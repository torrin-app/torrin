package jobs

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type jobExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func prepareCreate(j *Job) error {
	j.InfoHash = strings.ToLower(strings.TrimSpace(j.InfoHash))
	if j.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		j.ID = id.String()
	}
	now := time.Now().UTC()
	j.CreatedAt, j.UpdatedAt = now, now
	return nil
}

func insertJob(ctx context.Context, db jobExecer, j *Job) (bool, error) {
	files, _ := json.Marshal(j.Files)
	idxs, _ := json.Marshal(j.SelectedIdxs)
	ct, err := db.Exec(ctx, `
		INSERT INTO jobs (id, user_id, info_hash, name, magnet, source, status, error,
			files, selected_idxs, imdb_id, title_norm, season, episode, file_size, max_bytes, priority,
			node, created_at, updated_at, seed)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		ON CONFLICT (user_id, (lower(info_hash)))
		WHERE user_id NOT IN ('', 'system', 'prewarm') AND info_hash<>'' AND seed=false
			AND status NOT IN ('failed', 'evicted')
		DO NOTHING`,
		j.ID, j.UserID, j.InfoHash, j.Name, j.Magnet, string(j.Source), string(j.Status),
		j.Error, files, idxs, j.IMDBID, titleNormFromName(j.Name), j.Season, j.Episode, j.FileSize, j.MaxBytes, j.Priority,
		j.Node, j.CreatedAt, j.UpdatedAt, j.Seed)
	return ct.RowsAffected() == 1, err
}

func hasLiveUserIdentity(j *Job) bool {
	return j != nil && j.UserID != "" && j.UserID != "system" && j.UserID != "prewarm" &&
		j.InfoHash != "" && !j.Seed && j.Status != StatusFailed && j.Status != StatusEvicted
}

// GetByUserInfoHash returns the one live account row for this user and hash.
func (p *Postgres) GetByUserInfoHash(ctx context.Context, userID, infoHash string) (*Job, error) {
	return scanOne(p.pool.QueryRow(ctx, `SELECT `+cols+` FROM jobs
		WHERE user_id=$1 AND lower(info_hash)=lower($2) AND seed=false
		AND status NOT IN ('failed','evicted')
		ORDER BY created_at DESC, id DESC LIMIT 1`, userID, infoHash))
}

// GetReusableByInfoHash ignores failed/evicted history so a newer tombstone
// cannot hide another account's still-live reusable copy.
func (p *Postgres) GetReusableByInfoHash(ctx context.Context, infoHash string) (*Job, error) {
	return scanOne(p.pool.QueryRow(ctx, `SELECT `+cols+` FROM jobs
		WHERE lower(info_hash)=lower($1) AND seed=false
		AND status NOT IN ('failed','evicted')
		ORDER BY created_at DESC, id DESC LIMIT 1`, infoHash))
}

// CreateOnce inserts a job or returns the canonical live row that won a
// concurrent user/hash insert. created is false when an existing row won.
func (p *Postgres) CreateOnce(ctx context.Context, j *Job) (created bool, err error) {
	if err := prepareCreate(j); err != nil {
		return false, err
	}
	created, err = insertJob(ctx, p.pool, j)
	if err != nil || created || !hasLiveUserIdentity(j) {
		return created, err
	}
	existing, err := p.GetByUserInfoHash(ctx, j.UserID, j.InfoHash)
	if err != nil {
		return false, err
	}
	*j = *existing
	return false, nil
}

type createOnceRepository interface {
	CreateOnce(context.Context, *Job) (bool, error)
}

// CreateAccountOnce keeps handlers testable through Repository while using the
// PostgreSQL conflict result in production to suppress duplicate assignment.
func CreateAccountOnce(ctx context.Context, repo Repository, j *Job) (bool, error) {
	if once, ok := repo.(createOnceRepository); ok {
		return once.CreateOnce(ctx, j)
	}
	return true, repo.Create(ctx, j)
}
