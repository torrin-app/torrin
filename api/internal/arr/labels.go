package arr

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type labelStore struct{ pool *pgxpool.Pool }

func newLabelStore(ctx context.Context, pool *pgxpool.Pool) *labelStore {
	if pool != nil {
		pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS arr_labels (
			user_id text NOT NULL,
			info_hash text NOT NULL,
			category text NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, info_hash)
		)`)
	}
	return &labelStore{pool: pool}
}

func (s *labelStore) set(ctx context.Context, userID, infoHash, category string) {
	if s.pool == nil || category == "" {
		return
	}
	s.pool.Exec(ctx, `INSERT INTO arr_labels (user_id, info_hash, category)
		VALUES ($1, lower($2), $3)
		ON CONFLICT (user_id, info_hash) DO UPDATE SET category = EXCLUDED.category`,
		userID, infoHash, category)
}

func (s *labelStore) byUser(ctx context.Context, userID string) map[string]string {
	out := map[string]string{}
	if s.pool == nil {
		return out
	}
	rows, err := s.pool.Query(ctx, `SELECT info_hash, category FROM arr_labels WHERE user_id=$1`, userID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var h, c string
		if rows.Scan(&h, &c) == nil {
			out[h] = c
		}
	}
	return out
}

func (s *labelStore) del(ctx context.Context, userID, infoHash string) {
	if s.pool == nil {
		return
	}
	s.pool.Exec(ctx, `DELETE FROM arr_labels WHERE user_id=$1 AND info_hash=lower($2)`, userID, infoHash)
}
