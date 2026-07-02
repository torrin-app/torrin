package auth

import (
	"context"
	"strconv"
	"time"
)

type ResellerCode struct {
	Code       string     `json:"code"`
	PlanID     string     `json:"plan_id"`
	Period     string     `json:"period"`
	Days       int        `json:"days"`
	Reseller   string     `json:"reseller"`
	Redeemed   bool       `json:"redeemed"`
	RedeemedBy string     `json:"redeemed_by,omitempty"`
	RedeemedAt *time.Time `json:"redeemed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (s *Store) CreateResellerCode(ctx context.Context, c *ResellerCode) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO reseller_codes (code, plan_id, period, days, reseller) VALUES ($1,$2,$3,$4,$5)`,
		c.Code, c.PlanID, c.Period, c.Days, c.Reseller)
	return err
}

func (s *Store) GetResellerCode(ctx context.Context, code string) (*ResellerCode, error) {
	c := &ResellerCode{}
	var redeemedAt *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT code, plan_id, period, days, reseller, redeemed_by, redeemed_at, created_at
		 FROM reseller_codes WHERE code=$1`, code).
		Scan(&c.Code, &c.PlanID, &c.Period, &c.Days, &c.Reseller, &c.RedeemedBy, &redeemedAt, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	c.RedeemedAt = redeemedAt
	c.Redeemed = c.RedeemedBy != ""
	return c, nil
}

func (s *Store) MarkResellerCodeRedeemed(ctx context.Context, code, userID string) (bool, error) {
	ct, err := s.pool.Exec(ctx,
		`UPDATE reseller_codes SET redeemed_by=$1, redeemed_at=$2 WHERE code=$3 AND redeemed_by=''`,
		userID, time.Now(), code)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

func (s *Store) ListResellerCodes(ctx context.Context, redeemedOnly bool, since *time.Time, limit, offset int) ([]*ResellerCode, error) {
	dateCol := "created_at"
	if redeemedOnly {
		dateCol = "redeemed_at"
	}
	q := `SELECT code, plan_id, period, days, reseller, redeemed_by, redeemed_at, created_at FROM reseller_codes WHERE TRUE`
	var args []any
	if redeemedOnly {
		q += ` AND redeemed_by != ''`
	}
	if since != nil {
		args = append(args, *since)
		q += ` AND ` + dateCol + ` >= $1`
	}
	q += ` ORDER BY ` + dateCol + ` DESC LIMIT $` + strconv.Itoa(len(args)+1) + ` OFFSET $` + strconv.Itoa(len(args)+2)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ResellerCode
	for rows.Next() {
		c := &ResellerCode{}
		var redeemedAt *time.Time
		if rows.Scan(&c.Code, &c.PlanID, &c.Period, &c.Days, &c.Reseller, &c.RedeemedBy, &redeemedAt, &c.CreatedAt) != nil {
			continue
		}
		c.RedeemedAt = redeemedAt
		c.Redeemed = c.RedeemedBy != ""
		out = append(out, c)
	}
	return out, nil
}
