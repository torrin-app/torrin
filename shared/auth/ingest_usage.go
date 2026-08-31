package auth

import (
	"context"
	"time"
)

var QuotaEnforceMonth string

func ingestMonth() string { return time.Now().UTC().Format("2006-01") }

func (s *Store) AddIngestUsage(ctx context.Context, userID string, bytes int64) error {
	if bytes <= 0 || userID == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO ingest_usage (user_id, month, bytes) VALUES ($1,$2,$3)
		 ON CONFLICT (user_id, month) DO UPDATE SET bytes = ingest_usage.bytes + $3`,
		userID, ingestMonth(), bytes)
	return err
}

func (s *Store) MonthlyIngestBytes(ctx context.Context, userID string) (int64, error) {
	var b int64
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(bytes),0) FROM ingest_usage WHERE user_id=$1 AND month=$2`,
		userID, ingestMonth()).Scan(&b)
	return b, err
}

func (s *Store) MonthlyQuotaExceeded(ctx context.Context, userID string, capBytes int64) (bool, error) {
	if capBytes <= 0 || QuotaEnforceMonth == "" || ingestMonth() < QuotaEnforceMonth {
		return false, nil
	}
	used, err := s.MonthlyIngestBytes(ctx, userID)
	if err != nil {
		return false, err
	}
	return used >= capBytes, nil
}
