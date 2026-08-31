package nodestatus

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Store struct {
	db     DB
	maxAge time.Duration
}

func New(maxAge time.Duration) *Store {
	if maxAge <= 0 {
		maxAge = 3 * time.Minute
	}
	return &Store{maxAge: maxAge}
}

func (s *Store) SetDB(ctx context.Context, db DB) error {
	s.db = db
	if _, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS node_status (
		node text PRIMARY KEY,
		free_bytes bigint NOT NULL,
		total_bytes bigint NOT NULL,
		updated_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `ALTER TABLE node_status ADD COLUMN IF NOT EXISTS min_free_bytes bigint NOT NULL DEFAULT 0`)
	return err
}

func (s *Store) Report(ctx context.Context, node string, free, total, minFree int64) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(ctx, `INSERT INTO node_status (node, free_bytes, total_bytes, min_free_bytes, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (node) DO UPDATE SET free_bytes = $2, total_bytes = $3, min_free_bytes = $4, updated_at = now()`, node, free, total, minFree)
	return err
}

func (s *Store) MinFree(ctx context.Context, node string) (int64, bool) {
	if s.db == nil {
		return 0, false
	}
	var minFree int64
	var ageSec float64
	err := s.db.QueryRow(ctx,
		`SELECT min_free_bytes, EXTRACT(EPOCH FROM (now()-updated_at)) FROM node_status WHERE node = $1`, node).
		Scan(&minFree, &ageSec)
	if err != nil || !fresh(ageSec, s.maxAge) {
		return 0, false
	}
	return minFree, true
}

func (s *Store) Free(ctx context.Context, node string) (int64, bool) {
	if s.db == nil {
		return 0, false
	}
	var free int64
	var ageSec float64
	err := s.db.QueryRow(ctx,
		`SELECT free_bytes, EXTRACT(EPOCH FROM (now()-updated_at)) FROM node_status WHERE node = $1`, node).
		Scan(&free, &ageSec)
	if err != nil || !fresh(ageSec, s.maxAge) {
		return 0, false
	}
	return free, true
}

func (s *Store) OtherHasRoom(ctx context.Context, self string, minFree int64) bool {
	if s.db == nil || minFree <= 0 {
		return false
	}
	var n int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM node_status
		WHERE node <> $1 AND free_bytes >= $2 AND EXTRACT(EPOCH FROM (now()-updated_at)) <= $3`,
		self, minFree, s.maxAge.Seconds()).Scan(&n)
	return err == nil && n > 0
}

func fresh(ageSec float64, maxAge time.Duration) bool {
	return ageSec >= 0 && ageSec <= maxAge.Seconds()
}
