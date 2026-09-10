package jobs

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Account identity must be checked before capacity: a replay never needs a
// second slot or queue entry, even when the queue is full.
func liveAccountJobTx(ctx context.Context, tx pgx.Tx, job *Job) (*Job, error) {
	if job.Seed || job.UserID == "" || job.UserID == "system" || job.UserID == "prewarm" || job.InfoHash == "" {
		return nil, nil
	}
	existing, err := scanOne(tx.QueryRow(ctx, `SELECT `+cols+` FROM jobs
		WHERE user_id=$1 AND lower(info_hash)=lower($2) AND seed=false
		AND status NOT IN ('failed','evicted') LIMIT 1`, job.UserID, job.InfoHash))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return existing, err
}

// Taken after admission's global/user locks, this serializes physical download
// claims across accounts without serializing unrelated hashes.
func lockDownloadHash(ctx context.Context, tx pgx.Tx, hash string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(lower($1), 1))`, hash)
	return err
}

func activeHashTx(ctx context.Context, tx pgx.Tx, hash, excludeID string) (bool, error) {
	var active bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE lower(info_hash)=lower($1)
		AND id<>$2 AND seed=false AND status IN `+concurrencyStates+`)`, hash, excludeID).Scan(&active)
	return active, err
}
