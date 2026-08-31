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

	Settled       bool       `json:"settled"`
	SettledAt     *time.Time `json:"settled_at,omitempty"`
	SettlementRef string     `json:"settlement_ref,omitempty"`

	RetailCents int `json:"retail_cents"`
}

func (s *Store) CreateResellerCode(ctx context.Context, c *ResellerCode) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO reseller_codes (code, plan_id, period, days, reseller, retail_cents) VALUES ($1,$2,$3,$4,$5,$6)`,
		c.Code, c.PlanID, c.Period, c.Days, c.Reseller, c.RetailCents)
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

func (s *Store) BackfillResellerRetail(ctx context.Context, price func(planID, period string, days int, createdAt time.Time) int) (int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT code, plan_id, period, days, created_at FROM reseller_codes WHERE retail_cents = 0`)
	if err != nil {
		return 0, err
	}
	type pending struct {
		code, planID, period string
		days                 int
		createdAt            time.Time
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if rows.Scan(&p.code, &p.planID, &p.period, &p.days, &p.createdAt) == nil {
			todo = append(todo, p)
		}
	}
	rows.Close()

	n := 0
	for _, p := range todo {
		cents := price(p.planID, p.period, p.days, p.createdAt)
		if cents <= 0 {
			continue
		}
		if _, err := s.pool.Exec(ctx,
			`UPDATE reseller_codes SET retail_cents=$1 WHERE code=$2 AND retail_cents=0`, cents, p.code); err == nil {
			n++
		}
	}
	return n, nil
}

func (s *Store) ListResellerCodes(ctx context.Context, redeemedOnly bool, since *time.Time, limit, offset int) ([]*ResellerCode, error) {
	dateCol := "created_at"
	if redeemedOnly {
		dateCol = "redeemed_at"
	}
	q := `SELECT code, plan_id, period, days, reseller, redeemed_by, redeemed_at, created_at, settled_at, settlement_ref, retail_cents FROM reseller_codes WHERE TRUE`
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
		var redeemedAt, settledAt *time.Time
		if rows.Scan(&c.Code, &c.PlanID, &c.Period, &c.Days, &c.Reseller, &c.RedeemedBy, &redeemedAt, &c.CreatedAt, &settledAt, &c.SettlementRef, &c.RetailCents) != nil {
			continue
		}
		c.RedeemedAt = redeemedAt
		c.Redeemed = c.RedeemedBy != ""
		c.SettledAt = settledAt
		c.Settled = settledAt != nil
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) MarkResellerCodesSettled(ctx context.Context, reseller, ref string) (int64, error) {
	ct, err := s.pool.Exec(ctx,
		`UPDATE reseller_codes SET settled_at=now(), settlement_ref=$2 WHERE reseller=$1 AND settled_at IS NULL`,
		reseller, ref)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

func (s *Store) UnsettleResellerCodes(ctx context.Context, reseller string) (int64, error) {
	ct, err := s.pool.Exec(ctx,
		`UPDATE reseller_codes SET settled_at=NULL, settlement_ref='' WHERE reseller=$1`,
		reseller)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}
