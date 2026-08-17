package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type LedgerEntry struct {
	Delta     int64     `json:"delta_cents"`
	Reason    string    `json:"reason"`
	Balance   int64     `json:"balance_after"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) WalletBalance(ctx context.Context, userID string) (int64, error) {
	var bal int64
	err := s.pool.QueryRow(ctx, `SELECT balance_cents FROM wallets WHERE user_id=$1`, userID).Scan(&bal)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return bal, err
}

func (s *Store) Credit(ctx context.Context, userID string, cents int64, reason, ref string) error {
	_, err := s.move(ctx, userID, cents, reason, ref, false)
	return err
}

func (s *Store) Debit(ctx context.Context, userID string, cents int64, reason, ref string) (bool, error) {
	return s.move(ctx, userID, -cents, reason, ref, true)
}

func (s *Store) move(ctx context.Context, userID string, delta int64, reason, ref string, guard bool) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var bal int64
	err = tx.QueryRow(ctx, `INSERT INTO wallets (user_id) VALUES ($1)
		ON CONFLICT (user_id) DO UPDATE SET balance_cents = wallets.balance_cents
		RETURNING balance_cents`, userID).Scan(&bal)
	if err != nil {
		return false, err
	}
	if guard && bal+delta < 0 {
		return false, nil
	}
	newBal := bal + delta

	tag, err := tx.Exec(ctx, `INSERT INTO wallet_ledger (user_id, delta_cents, reason, ref, balance_after)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (ref) DO NOTHING`, userID, delta, reason, ref, newBal)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return true, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE wallets SET balance_cents=$1, updated_at=now() WHERE user_id=$2`, newBal, userID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *Store) WalletBuyPlan(ctx context.Context, userID string, cents int64, reason, ref, planID, subID, license, recurrence string, expiresAt time.Time) (ok, applied bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback(ctx)

	var bal int64
	err = tx.QueryRow(ctx, `INSERT INTO wallets (user_id) VALUES ($1)
		ON CONFLICT (user_id) DO UPDATE SET balance_cents = wallets.balance_cents
		RETURNING balance_cents`, userID).Scan(&bal)
	if err != nil {
		return false, false, err
	}
	if bal-cents < 0 {
		return false, false, nil
	}
	newBal := bal - cents

	tag, err := tx.Exec(ctx, `INSERT INTO wallet_ledger (user_id, delta_cents, reason, ref, balance_after)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (ref) DO NOTHING`, userID, -cents, reason, ref, newBal)
	if err != nil {
		return false, false, err
	}
	if tag.RowsAffected() == 0 {
		return true, false, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE wallets SET balance_cents=$1, updated_at=now() WHERE user_id=$2`, newBal, userID); err != nil {
		return false, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET plan_id=$2, subscription_id=$3, license_key=$4, recurrence=$5,
			expires_at=$6, paused_at=NULL, remaining_days=0, pause_count=0, updated_at=now()
		WHERE id=$1`, userID, planID, subID, license, recurrence, expiresAt); err != nil {
		return false, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, false, err
	}
	s.AuditLog(ctx, userID, "plan_updated",
		fmt.Sprintf("plan=%s recurrence=%s expires=%s via=wallet", planID, recurrence, expiresAt.Format(time.RFC3339)), "")
	return true, true, nil
}

func (s *Store) WalletRecurringDue(ctx context.Context) ([]*User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM users
		WHERE subscription_id LIKE 'wallet:%' AND plan_id != 'free'
		  AND recurrence NOT IN ('lifetime','') AND expires_at < now() + interval '1 day'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		if u, err := scanOne(rows); err == nil && u != nil {
			out = append(out, u)
		}
	}
	return out, rows.Err()
}

func (s *Store) WalletHistory(ctx context.Context, userID string, limit int) ([]LedgerEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT delta_cents, reason, balance_after, created_at
		FROM wallet_ledger WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LedgerEntry{}
	for rows.Next() {
		var e LedgerEntry
		if rows.Scan(&e.Delta, &e.Reason, &e.Balance, &e.CreatedAt) == nil {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}
