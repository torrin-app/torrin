package auth

import (
	"context"
	"time"
)

const (
	ReferralDaysPerPayment = 7
	ReferralMaxDays        = 30
)

func (s *Store) Downgrade(ctx context.Context, subscriptionID string) error {
	var userID string
	s.pool.QueryRow(ctx, `SELECT id FROM users WHERE subscription_id=$1`, subscriptionID).Scan(&userID)
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET plan_id='free', subscription_id='', recurrence='',
			paused_at=NULL, remaining_days=0, expires_at=$1, updated_at=$2
		WHERE subscription_id=$3`,
		time.Now().Add(24*time.Hour), time.Now(), subscriptionID)
	if userID != "" {
		s.AuditLog(ctx, userID, "subscription_downgraded", "sub_id="+subscriptionID, "")
	}
	return err
}

func (s *Store) HasProcessedSale(ctx context.Context, saleID string) bool {
	var n int
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM processed_sales WHERE sale_id=$1`, saleID).Scan(&n)
	return n > 0
}

func (s *Store) RecordProcessedSale(ctx context.Context, saleID, userID string, amountCents int, currency string) {
	s.pool.Exec(ctx, `INSERT INTO processed_sales (sale_id, user_id, amount_cents, currency)
		VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, saleID, userID, amountCents, currency)
}

func (s *Store) GetReferrerID(ctx context.Context, refereeID string) string {
	var id string
	s.pool.QueryRow(ctx, `SELECT referred_by FROM users WHERE id=$1`, refereeID).Scan(&id)
	return id
}

func (s *Store) MarkReferralPaid(ctx context.Context, refereeID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE referrals SET paid=TRUE WHERE referee_id=$1 AND paid=FALSE`, refereeID)
	return err
}

func (s *Store) GetReferralCode(ctx context.Context, userID string) string {
	var code string
	s.pool.QueryRow(ctx, `SELECT referral_code FROM users WHERE id=$1`, userID).Scan(&code)
	return code
}

func (s *Store) GetTotalReferralCount(ctx context.Context, userID string) int {
	var n int
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM referrals WHERE referrer_id=$1`, userID).Scan(&n)
	return n
}

func (s *Store) GetPayingReferralCount(ctx context.Context, userID string) int {
	var n int
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM referrals WHERE referrer_id=$1 AND paid=TRUE`, userID).Scan(&n)
	return n
}

func (s *Store) GetReferralCredit(ctx context.Context, userID string) int {
	u, err := s.GetByID(ctx, userID)
	if err != nil {
		return 0
	}
	if d := int(time.Until(u.ExpiresAt).Hours() / 24); d > 0 {
		return d
	}
	return 0
}

func (s *Store) ApplyReferralReward(ctx context.Context, userID string) {
	user, err := s.GetByID(ctx, userID)
	if err != nil || user == nil {
		return
	}
	now := time.Now()
	currentExpiry := now
	if user.ExpiresAt.After(now) {
		currentExpiry = user.ExpiresAt
	}
	newExpiry := currentExpiry.Add(ReferralDaysPerPayment * 24 * time.Hour)
	if maxExpiry := now.Add(ReferralMaxDays * 24 * time.Hour); newExpiry.After(maxExpiry) {
		newExpiry = maxExpiry
	}
	s.pool.Exec(ctx, `UPDATE users SET expires_at=$1, updated_at=$2 WHERE id=$3`, newExpiry, now, userID)
}
