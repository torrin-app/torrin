package auth

import (
	"context"
	"time"
)

type DebridUsage struct {
	Provider string `json:"provider"`
	Bytes    int64  `json:"bytes"`
}

func usageMonth() string { return time.Now().UTC().Format("2006-01") }

func (s *Store) AddDebridUsage(ctx context.Context, userID, provider string, bytes int64) error {
	if bytes <= 0 || userID == "" || provider == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO debrid_usage (user_id, provider, month, bytes) VALUES ($1,$2,$3,$4)
		 ON CONFLICT (user_id, provider, month) DO UPDATE SET bytes = debrid_usage.bytes + $4`,
		userID, provider, usageMonth(), bytes)
	return err
}

func (s *Store) GetDebridUsage(ctx context.Context, userID string) ([]DebridUsage, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT provider, bytes FROM debrid_usage WHERE user_id=$1 AND month=$2 ORDER BY provider`,
		userID, usageMonth())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DebridUsage
	for rows.Next() {
		var u DebridUsage
		if rows.Scan(&u.Provider, &u.Bytes) == nil {
			out = append(out, u)
		}
	}
	return out, rows.Err()
}
