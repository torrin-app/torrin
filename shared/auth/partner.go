package auth

import "context"

type PartnerReport struct {
	Code            string `json:"code"`
	Clicks          int    `json:"clicks"`
	Signups         int    `json:"signups"`
	Conversions     int    `json:"conversions"`
	GrossCents      int64  `json:"gross_cents"`
	CommissionPct   int    `json:"commission_pct"`
	CommissionCents int64  `json:"commission_cents"`
}

func (s *Store) RecordReferralClick(ctx context.Context, code, ipHash string) {
	s.pool.Exec(ctx, `INSERT INTO referral_clicks (code, ip_hash) VALUES ($1,$2)`, code, ipHash)
}

func (s *Store) MintPartnerToken(ctx context.Context, code, token string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO partner_tokens (token, code) VALUES ($1,$2)`, token, code)
	return err
}

func (s *Store) PartnerCodeForToken(ctx context.Context, token string) (string, bool) {
	var code string
	err := s.pool.QueryRow(ctx, `SELECT code FROM partner_tokens WHERE token=$1 AND NOT revoked`, token).Scan(&code)
	return code, err == nil && code != ""
}

func (s *Store) PartnerReport(ctx context.Context, code string, commissionPct int) (PartnerReport, error) {
	rep := PartnerReport{Code: code, CommissionPct: commissionPct}
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM referral_clicks WHERE code=$1`, code).Scan(&rep.Clicks)
	err := s.pool.QueryRow(ctx, `
		WITH partner AS (SELECT id FROM users WHERE referral_code=$1),
		refs AS (SELECT u.id AS uid FROM users u JOIN partner p ON u.referred_by=p.id),
		firsts AS (
			SELECT user_id, MIN(created_at) AS first_sale
			FROM processed_sales
			WHERE user_id IN (SELECT uid FROM refs)
			GROUP BY user_id
		)
		SELECT
			(SELECT COUNT(*) FROM refs),
			(SELECT COUNT(*) FROM firsts),
			COALESCE((
				SELECT SUM(ps.amount_cents)
				FROM processed_sales ps JOIN firsts f ON ps.user_id=f.user_id
				WHERE ps.created_at <= f.first_sale + INTERVAL '12 months'
			), 0)`,
		code).Scan(&rep.Signups, &rep.Conversions, &rep.GrossCents)
	if err != nil {
		return rep, err
	}
	rep.CommissionCents = rep.GrossCents * int64(commissionPct) / 100
	return rep, nil
}
