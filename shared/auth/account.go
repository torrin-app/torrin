package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lindell/go-burner-email-providers/burner"
)

// ErrEmailTaken means the requested email already belongs to another account.
var ErrEmailTaken = errors.New("this email is already in use")

// internalErr logs a raw error and returns a safe, generic message so internal
// details (db constraints, driver errors) never reach the client. Nil passes through.
func internalErr(err error, msg string) error {
	if err == nil {
		return nil
	}
	slog.Warn("internal error", "op", msg, "err", err)
	return errors.New(msg)
}

func (s *Store) PauseSub(ctx context.Context, userID string) (int, error) {
	u, err := s.GetByID(ctx, userID)
	if err != nil {
		return 0, internalErr(err, "could not pause your plan, try again")
	}
	if u.PlanID == "free" || u.Recurrence == "lifetime" {
		return 0, errors.New("cannot pause this plan")
	}
	if u.IsPaused() {
		return u.RemainingDays, nil
	}
	if u.PauseCount >= 3 {
		return 0, errors.New("pause limit reached (3 per billing cycle)")
	}
	if !u.LastPausedAt.IsZero() && time.Since(u.LastPausedAt) < 24*time.Hour {
		return 0, errors.New("can only pause once every 24 hours")
	}
	remaining := int(math.Ceil(time.Until(u.ExpiresAt).Hours() / 24))
	if remaining < 1 {
		remaining = 1
	}
	now := time.Now()
	_, err = s.pool.Exec(ctx,
		`UPDATE users SET paused_at=$1, remaining_days=$2, pause_count=pause_count+1, last_paused_at=$3, updated_at=$4 WHERE id=$5`,
		now, remaining, now, now, userID)
	s.AuditLog(ctx, userID, "subscription_paused", fmt.Sprintf("remaining_days=%d", remaining), "")
	return remaining, internalErr(err, "could not pause your plan, try again")
}

func (s *Store) ResumeSub(ctx context.Context, userID string) (time.Time, error) {
	u, err := s.GetByID(ctx, userID)
	if err != nil {
		return time.Time{}, internalErr(err, "could not resume your plan, try again")
	}
	if !u.IsPaused() {
		return u.ExpiresAt, nil
	}
	newExpiry := time.Now().Add(time.Duration(u.RemainingDays) * 24 * time.Hour)
	_, err = s.pool.Exec(ctx,
		`UPDATE users SET expires_at=$1, paused_at=NULL, remaining_days=0, updated_at=$2 WHERE id=$3`,
		newExpiry, time.Now(), userID)
	s.AuditLog(ctx, userID, "subscription_resumed", "new_expiry="+newExpiry.Format(time.RFC3339), "")
	return newExpiry, internalErr(err, "could not resume your plan, try again")
}

func (s *Store) UpdateEmail(ctx context.Context, userID, email string) error {
	if burner.IsBurnerEmail(email) {
		return errors.New("disposable email addresses are not allowed")
	}
	var changes int
	s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE user_id=$1 AND action='email_changed' AND created_at > now() - interval '30 days'`,
		userID).Scan(&changes)
	if changes >= 2 {
		return errors.New("email change limit reached (max 2 per month)")
	}
	_, err := s.pool.Exec(ctx, `UPDATE users SET email=$1, updated_at=$2 WHERE id=$3`, email, time.Now(), userID)
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrEmailTaken
	}
	slog.Warn("update email failed", "user", userID, "err", err)
	return errors.New("could not update your email, try again")
}

func (s *Store) DeleteUser(ctx context.Context, userID string) error {
	var email string
	s.pool.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, userID).Scan(&email)
	if email != "" {
		s.pool.Exec(ctx, `INSERT INTO deleted_emails (email) VALUES ($1) ON CONFLICT DO NOTHING`, email)
	}
	s.AuditLog(ctx, userID, "account_deleted", "email="+email, "")
	_, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID)
	return internalErr(err, "could not delete your account, try again")
}
