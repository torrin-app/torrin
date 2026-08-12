package auth

import (
	"context"
	"time"
)

func (s *Store) TombstoneUsenet(ctx context.Context, userID, infoHash string, until time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO usenet_tombstones (user_id, info_hash, until) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, info_hash) DO UPDATE SET until=$3`,
		userID, infoHash, until)
	return err
}

func (s *Store) ClearUsenetTombstone(ctx context.Context, userID, infoHash string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM usenet_tombstones WHERE user_id=$1 AND info_hash=$2`,
		userID, infoHash)
	return err
}

func (s *Store) UsenetTombstoned(ctx context.Context, userID, infoHash string) bool {
	var until time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT until FROM usenet_tombstones WHERE user_id=$1 AND info_hash=$2`,
		userID, infoHash).Scan(&until); err != nil {
		return false
	}
	return until.After(time.Now())
}
