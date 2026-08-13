package auth

import (
	"context"
	"fmt"
	"time"
)

const maxAPIKeys = 20

type APIKey struct {
	ID           string     `json:"id"`
	Label        string     `json:"label"`
	Key          string     `json:"key,omitempty"`
	LoginAllowed bool       `json:"login_allowed"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
}

func (s *Store) CreateAPIKey(ctx context.Context, userID, label string, loginAllowed bool) (*APIKey, error) {
	var n int
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE user_id=$1 AND revoked_at IS NULL`, userID).Scan(&n)
	if n >= maxAPIKeys {
		return nil, fmt.Errorf("key limit reached (max %d)", maxAPIKeys)
	}
	k := &APIKey{ID: newID(), Label: label, Key: generateAPIKey(), LoginAllowed: loginAllowed, CreatedAt: time.Now()}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO api_keys (id, user_id, key, label, login_allowed, created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		k.ID, userID, k.Key, k.Label, k.LoginAllowed, k.CreatedAt)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (s *Store) ListAPIKeys(ctx context.Context, userID string) ([]APIKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, label, login_allowed, created_at, last_used_at FROM api_keys
		 WHERE user_id=$1 AND revoked_at IS NULL ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.Label, &k.LoginAllowed, &k.CreatedAt, &k.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) RevokeAPIKey(ctx context.Context, userID, id string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ResolveAPIKey(ctx context.Context, key string) (*User, bool, error) {
	if u, err := scanOne(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE api_key=$1`, key)); err == nil {
		return u, true, nil
	}
	var userID string
	var loginAllowed bool
	if err := s.pool.QueryRow(ctx,
		`SELECT user_id, login_allowed FROM api_keys WHERE key=$1 AND revoked_at IS NULL`, key,
	).Scan(&userID, &loginAllowed); err != nil {
		return nil, false, ErrNotFound
	}
	u, err := s.GetByID(ctx, userID)
	if err != nil {
		return nil, false, err
	}
	s.touchAPIKey(key)
	return u, loginAllowed, nil
}

func (s *Store) touchAPIKey(key string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at=now() WHERE key=$1`, key)
	}()
}
