package auth

import (
	"context"
	"strconv"
	"time"
)

type AdminStats struct {
	TotalUsers   int            `json:"total_users"`
	ActiveUsers  int            `json:"active_users"`
	FreeActive   int            `json:"free_active"`
	Expired      int            `json:"expired"`
	PaidByPlan   map[string]int `json:"paid_by_plan"`
	ByRecurrence map[string]int `json:"by_recurrence"`
	CredCounts   map[string]int `json:"cred_counts"`
}

var credTables = map[string]string{
	"rd": "rd_credentials", "ad": "ad_credentials", "pm": "pm_credentials",
	"tb": "tb_credentials", "oc": "oc_credentials", "usenet": "usenet_credentials",
	"indexers": "usenet_indexers", "rss": "rss_feeds",
}

func (s *Store) AdminStats(ctx context.Context) (AdminStats, error) {
	st := AdminStats{PaidByPlan: map[string]int{}, ByRecurrence: map[string]int{}, CredCounts: map[string]int{}}
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&st.TotalUsers); err != nil {
		return st, err
	}
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE plan_id != 'free' AND expires_at > now()`).Scan(&st.ActiveUsers)
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE plan_id = 'free' AND expires_at > now()`).Scan(&st.FreeActive)
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE expires_at <= now()`).Scan(&st.Expired)

	rows, err := s.pool.Query(ctx,
		`SELECT plan_id, COUNT(*) FROM users WHERE plan_id != 'free' AND expires_at > now() GROUP BY plan_id`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var plan string
			var n int
			if rows.Scan(&plan, &n) == nil {
				st.PaidByPlan[plan] = n
			}
		}
	}

	rrows, err := s.pool.Query(ctx,
		`SELECT COALESCE(NULLIF(recurrence,''),'comp'), COUNT(*) FROM users WHERE plan_id != 'free' AND expires_at > now() GROUP BY 1`)
	if err == nil {
		defer rrows.Close()
		for rrows.Next() {
			var rec string
			var n int
			if rrows.Scan(&rec, &n) == nil {
				st.ByRecurrence[rec] = n
			}
		}
	}

	for label, table := range credTables {
		var n int
		s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n)
		st.CredCounts[label] = n
	}
	return st, nil
}

type AdminUser struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	PlanID        string    `json:"plan_id"`
	ExpiresAt     time.Time `json:"expires_at"`
	CreatedAt     time.Time `json:"created_at"`
	Banned        bool      `json:"banned"`
	ActiveSlots   int       `json:"active_slots"`
	ReferralCount int       `json:"referral_count"`
	HasRDKey      bool      `json:"has_rd_key"`
	HasADKey      bool      `json:"has_ad_key"`
	HasPMKey      bool      `json:"has_pm_key"`
	HasTBKey      bool      `json:"has_tb_key"`
	HasOCKey      bool      `json:"has_oc_key"`
}

func (s *Store) AdminListUsers(ctx context.Context, q, plan string, limit, offset int) ([]AdminUser, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `
		SELECT u.id, u.email, u.plan_id, u.expires_at, u.created_at, u.banned,
			(SELECT COUNT(*) FROM jobs j WHERE j.user_id=u.id AND j.status IN ('pending','queued','downloading','processing','publishing')),
			(SELECT COUNT(*) FROM referrals r WHERE r.referrer_id=u.id AND r.paid=TRUE),
			EXISTS(SELECT 1 FROM rd_credentials WHERE user_id=u.id),
			EXISTS(SELECT 1 FROM ad_credentials WHERE user_id=u.id),
			EXISTS(SELECT 1 FROM pm_credentials WHERE user_id=u.id),
			EXISTS(SELECT 1 FROM tb_credentials WHERE user_id=u.id),
			EXISTS(SELECT 1 FROM oc_credentials WHERE user_id=u.id)
		FROM users u WHERE 1=1`
	args := []any{}
	i := 1
	if q != "" {
		query += ` AND u.email ILIKE $` + strconv.Itoa(i)
		args = append(args, "%"+q+"%")
		i++
	}
	if plan != "" {
		query += ` AND u.plan_id = $` + strconv.Itoa(i)
		args = append(args, plan)
		i++
	}
	query += ` ORDER BY u.created_at DESC LIMIT $` + strconv.Itoa(i) + ` OFFSET $` + strconv.Itoa(i+1)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminUser{}
	for rows.Next() {
		var u AdminUser
		if rows.Scan(&u.ID, &u.Email, &u.PlanID, &u.ExpiresAt, &u.CreatedAt, &u.Banned,
			&u.ActiveSlots, &u.ReferralCount, &u.HasRDKey, &u.HasADKey, &u.HasPMKey, &u.HasTBKey, &u.HasOCKey) == nil {
			out = append(out, u)
		}
	}
	return out, rows.Err()
}

type AdminReferrer struct {
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
	ReferralCode string `json:"referral_code"`
	PaidCount    int    `json:"paid_count"`
	TotalCount   int    `json:"total_count"`
}

func (s *Store) AdminReferrers(ctx context.Context, limit int) ([]AdminReferrer, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.email, u.referral_code,
			(SELECT COUNT(*) FROM referrals WHERE referrer_id=u.id AND paid=TRUE),
			(SELECT COUNT(*) FROM referrals WHERE referrer_id=u.id)
		FROM users u
		WHERE u.id IN (SELECT DISTINCT referrer_id FROM referrals)
		ORDER BY 4 DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminReferrer{}
	for rows.Next() {
		var r AdminReferrer
		if rows.Scan(&r.UserID, &r.Email, &r.ReferralCode, &r.PaidCount, &r.TotalCount) == nil {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

type PaidKey struct {
	Email  string
	APIKey string
}

func (s *Store) PaidUserKeys(ctx context.Context) ([]PaidKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT email, api_key FROM users WHERE plan_id != 'free' AND expires_at > now()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PaidKey
	for rows.Next() {
		var k PaidKey
		if rows.Scan(&k.Email, &k.APIKey) == nil {
			out = append(out, k)
		}
	}
	return out, rows.Err()
}
