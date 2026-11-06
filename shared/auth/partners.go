package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type ResellerPartner struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	SuppressWelcome bool      `json:"suppress_welcome"`
	Active          bool      `json:"active"`
	CreatedAt       time.Time `json:"created_at"`
}

// HashPartnerKey is the stable one-way hash a partner key is stored and looked
// up by, so plaintext keys never touch the database.
func HashPartnerKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (s *Store) CreateResellerPartner(ctx context.Context, id, name, keyHash string, suppressWelcome bool) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO reseller_partners (id, name, key_hash, suppress_welcome) VALUES ($1,$2,$3,$4)`,
		id, name, keyHash, suppressWelcome)
	return err
}

// GetPartnerByKeyHash returns the active partner that owns a key hash, or nil.
func (s *Store) GetPartnerByKeyHash(ctx context.Context, keyHash string) (*ResellerPartner, error) {
	p := &ResellerPartner{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, suppress_welcome, active, created_at FROM reseller_partners WHERE key_hash=$1 AND active`,
		keyHash).Scan(&p.ID, &p.Name, &p.SuppressWelcome, &p.Active, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) ListResellerPartners(ctx context.Context) ([]*ResellerPartner, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, suppress_welcome, active, created_at FROM reseller_partners ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ResellerPartner{}
	for rows.Next() {
		p := &ResellerPartner{}
		if rows.Scan(&p.ID, &p.Name, &p.SuppressWelcome, &p.Active, &p.CreatedAt) == nil {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (s *Store) SetResellerPartnerActive(ctx context.Context, id string, active bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE reseller_partners SET active=$2 WHERE id=$1`, id, active)
	return err
}

// SetUserPartner records which partner provisioned an account, so a partner can
// later recover it idempotently (see AccountForPartner).
func (s *Store) SetUserPartner(ctx context.Context, userID, partnerID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET partner_id=$2 WHERE id=$1`, userID, partnerID)
	return err
}

// AccountForPartner returns an account's api key + plan only when it exists AND
// was created by this same partner. This lets a partner safely retry a lost
// registration without ever exposing an account it did not create.
func (s *Store) AccountForPartner(ctx context.Context, email, partnerID string) (apiKey, plan string, ok bool) {
	if partnerID == "" {
		return "", "", false
	}
	err := s.pool.QueryRow(ctx,
		`SELECT api_key, plan_id FROM users WHERE email=$1 AND partner_id=$2`,
		NormalizeEmail(email), partnerID).Scan(&apiKey, &plan)
	return apiKey, plan, err == nil
}
