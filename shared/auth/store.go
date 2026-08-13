package auth

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/pg"
)

//go:embed schema.sql
var schema string

var ErrNotFound = errors.New("user not found")

const userColumns = `id, email, api_key, plan_id, subscription_id, license_key, recurrence,
	expires_at, paused_at, remaining_days, pause_count, last_paused_at, banned, ban_reason,
	created_at, updated_at, seed_slot_packs, seed_slot_sub, system_indexer_enabled`

type Store struct {
	pool  *pgxpool.Pool
	creds *crypto.Cipher
}

func NewPostgres(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pg.Open(ctx, dsn, schema)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) SetCredsKey(hexKey string) error {
	c, err := crypto.New(hexKey)
	if err != nil {
		return err
	}
	s.creds = c
	return nil
}

func (s *Store) enc(v string) string { return s.creds.Encrypt(v) }
func (s *Store) dec(v string) string { return s.creds.Decrypt(v) }

func (s *Store) GetByID(ctx context.Context, id string) (*User, error) {
	return scanOne(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, id))
}

func (s *Store) GetByAPIKey(ctx context.Context, apiKey string) (*User, error) {
	return scanOne(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE api_key=$1`, apiKey))
}

func (s *Store) GetByEmail(ctx context.Context, email string) (*User, error) {
	return scanOne(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE email=$1`, email))
}

func (s *Store) CountUsers(ctx context.Context) int {
	var n int
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n
}

func (s *Store) UpdatePlan(ctx context.Context, userID, planID, subscriptionID, licenseKey, recurrence string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET plan_id=$2, subscription_id=$3, license_key=$4, recurrence=$5,
			expires_at=$6, paused_at=NULL, remaining_days=0, pause_count=0, updated_at=$7
		WHERE id=$1`,
		userID, planID, subscriptionID, licenseKey, recurrence, expiresAt, time.Now())
	if err != nil {
		return err
	}
	s.AuditLog(ctx, userID, "plan_updated",
		fmt.Sprintf("plan=%s recurrence=%s expires=%s", planID, recurrence, expiresAt.Format(time.RFC3339)), "")
	return nil
}

func (s *Store) SetSeedSlots(ctx context.Context, userID string, packs int, subID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET seed_slot_packs=$2, seed_slot_sub=$3, updated_at=$4 WHERE id=$1`,
		userID, packs, subID, time.Now())
	return err
}

func (s *Store) ClearSeedSlotsBySub(ctx context.Context, subID string) error {
	if subID == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET seed_slot_packs=0, seed_slot_sub='', updated_at=$2 WHERE seed_slot_sub=$1`,
		subID, time.Now())
	return err
}

func scanOne(row pgx.Row) (*User, error) {
	var u User
	var pausedAt, lastPausedAt *time.Time
	err := row.Scan(&u.ID, &u.Email, &u.APIKey, &u.PlanID, &u.SubscriptionID, &u.LicenseKey,
		&u.Recurrence, &u.ExpiresAt, &pausedAt, &u.RemainingDays, &u.PauseCount, &lastPausedAt,
		&u.Banned, &u.BanReason, &u.CreatedAt, &u.UpdatedAt, &u.SeedSlotPacks, &u.SeedSlotSub,
		&u.SystemIndexerEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if pausedAt != nil {
		u.PausedAt = *pausedAt
	}
	if lastPausedAt != nil {
		u.LastPausedAt = *lastPausedAt
	}
	return &u, nil
}
