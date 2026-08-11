package auth

import (
	"context"
	"time"
)

func (s *Store) AuditLog(ctx context.Context, userID, action, detail, ip string) {
	s.pool.Exec(ctx, `INSERT INTO audit_log (user_id, action, detail, ip) VALUES ($1,$2,$3,$4)`,
		userID, action, detail, ip)
}

func (s *Store) SignupsFromIP(ctx context.Context, ip string, since time.Time) int {
	var n int
	s.pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE action='account_created' AND ip=$1 AND created_at >= $2`,
		ip, since).Scan(&n)
	return n
}

type AuditEntry struct {
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	IP        string    `json:"ip,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) GetAuditLog(ctx context.Context, userID string, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT action, detail, ip, created_at FROM audit_log WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if rows.Scan(&e.Action, &e.Detail, &e.IP, &e.CreatedAt) == nil {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}
