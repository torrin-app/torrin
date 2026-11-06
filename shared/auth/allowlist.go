package auth

import (
	"context"
	"time"
)

type AllowlistHash struct {
	InfoHash  string    `json:"info_hash"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// IsHashAllowlisted reports whether a release hash has been manually approved to
// bypass soft safety flags (false-positive handling). It never overrides hard
// terms. Returns false on any error so a DB blip fails closed.
func (s *Store) IsHashAllowlisted(ctx context.Context, infoHash string) bool {
	if infoHash == "" {
		return false
	}
	var one int
	err := s.pool.QueryRow(ctx, `SELECT 1 FROM allowlist_hashes WHERE info_hash=$1`, infoHash).Scan(&one)
	return err == nil
}

func (s *Store) AddAllowlistHash(ctx context.Context, infoHash, note string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO allowlist_hashes (info_hash, note) VALUES ($1,$2) ON CONFLICT (info_hash) DO UPDATE SET note=$2`,
		infoHash, note)
	return err
}

func (s *Store) RemoveAllowlistHash(ctx context.Context, infoHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM allowlist_hashes WHERE info_hash=$1`, infoHash)
	return err
}

func (s *Store) ListAllowlistHashes(ctx context.Context) ([]AllowlistHash, error) {
	rows, err := s.pool.Query(ctx, `SELECT info_hash, note, created_at FROM allowlist_hashes ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AllowlistHash{}
	for rows.Next() {
		var a AllowlistHash
		if rows.Scan(&a.InfoHash, &a.Note, &a.CreatedAt) == nil {
			out = append(out, a)
		}
	}
	return out, rows.Err()
}
