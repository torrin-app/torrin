package jobs

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/torrin-app/torrin/shared/pg"
)

//go:embed schema.sql
var schema string

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	pool, err := pg.Open(ctx, dsn, schema)
	if err != nil {
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() { p.pool.Close() }

func (p *Postgres) Pool() *pgxpool.Pool { return p.pool }

func (p *Postgres) ColdPullAllowed(ctx context.Context, userID string, perHour int) (bool, error) {
	if perHour <= 0 {
		return true, nil
	}
	var count int
	if err := p.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM cold_pulls WHERE user_id=$1 AND created_at > now() - interval '1 hour'`,
		userID).Scan(&count); err != nil {
		return true, err
	}
	if count >= perHour {
		return false, nil
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO cold_pulls (user_id) VALUES ($1)`, userID)
	return true, err
}

func (p *Postgres) SweepColdPulls(ctx context.Context) {
	p.pool.Exec(ctx, `DELETE FROM cold_pulls WHERE created_at < now() - interval '2 hours'`)
}

func (p *Postgres) StoreReleaseLink(ctx context.Context, infoHash, postURL, title, source string, size int64) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO release_links (info_hash, post_url, title, source, size) VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (info_hash) DO UPDATE SET post_url=$2, title=$3, source=$4, size=$5`,
		infoHash, postURL, title, source, size)
	return err
}

func (p *Postgres) ReleaseLink(ctx context.Context, infoHash string) (postURL, title, source string, size int64) {
	p.pool.QueryRow(ctx, `SELECT post_url, COALESCE(title, ''), COALESCE(source, 'hdencode'), COALESCE(size, 0) FROM release_links WHERE info_hash=$1`, infoHash).Scan(&postURL, &title, &source, &size)
	return
}

func (p *Postgres) Create(ctx context.Context, j *Job) error {
	_, err := p.CreateOnce(ctx, j)
	return err
}

func (p *Postgres) Update(ctx context.Context, j *Job) error {
	j.UpdatedAt = time.Now().UTC()
	files, _ := json.Marshal(j.Files)
	idxs, _ := json.Marshal(j.SelectedIdxs)
	ct, err := p.pool.Exec(ctx, `
		UPDATE jobs SET user_id=$2, info_hash=$3, name=$4, magnet=$5, source=$6,
			status=$7, error=$8, files=$9, selected_idxs=$10, imdb_id=$11,
			title_norm=$12, season=$13, episode=$14, file_size=$15, max_bytes=$16, priority=$17, node=$18, updated_at=$19, seed=$20,
			budget_gated=$21, input_key=$22
		WHERE id=$1`,
		j.ID, j.UserID, j.InfoHash, j.Name, j.Magnet, string(j.Source), string(j.Status),
		j.Error, files, idxs, j.IMDBID, titleNormFromName(j.Name), j.Season, j.Episode, j.FileSize, j.MaxBytes, j.Priority, j.Node, j.UpdatedAt, j.Seed,
		j.BudgetGated, j.InputKey)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const cols = `id, user_id, info_hash, name, magnet, source, status, error,
	files, selected_idxs, imdb_id, season, episode, file_size, max_bytes, priority,
	created_at, updated_at, progress, dl_speed, node, seed, budget_gated, input_key`

func (p *Postgres) Requeue(ctx context.Context, id string) error {
	ct, err := p.pool.Exec(ctx,
		`UPDATE jobs SET status='pending', error='', created_at=now(), updated_at=now() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) SetProgress(ctx context.Context, id string, pct float64, speed int64) error {
	_, err := p.pool.Exec(ctx, `UPDATE jobs SET progress=$2, dl_speed=$3 WHERE id=$1`, id, pct, speed)
	return err
}

func (p *Postgres) Get(ctx context.Context, id string) (*Job, error) {
	return scanOne(p.pool.QueryRow(ctx, `SELECT `+cols+` FROM jobs WHERE id=$1`, id))
}

func (p *Postgres) GetByInfoHash(ctx context.Context, infoHash string) (*Job, error) {
	return scanOne(p.pool.QueryRow(ctx,
		`SELECT `+cols+` FROM jobs WHERE lower(info_hash)=lower($1) ORDER BY created_at DESC LIMIT 1`, infoHash))
}

func (p *Postgres) ListByInfoHash(ctx context.Context, infoHash string) ([]*Job, error) {
	return p.query(ctx, `SELECT `+cols+` FROM jobs WHERE lower(info_hash)=lower($1)`, infoHash)
}

func (p *Postgres) CachedByHashes(ctx context.Context, hashes []string) (map[string]*Job, error) {
	rows, err := p.query(ctx, `SELECT DISTINCT ON (info_hash) `+cols+` FROM jobs
		WHERE info_hash = ANY($1) AND status IN ('complete','seeding')
		AND jsonb_typeof(files)='array' AND files<>'[]'::jsonb
		AND (COALESCE(node,'')<>'' OR NOT EXISTS (
			SELECT 1 FROM jsonb_array_elements(
				CASE WHEN jsonb_typeof(files)='array' THEN files ELSE '[]'::jsonb END
			) AS file
			WHERE COALESCE(file->>'key','') LIKE '%/cairn/%'
		))
		ORDER BY info_hash, created_at DESC`, hashes)
	if err != nil {
		return nil, err
	}
	m := make(map[string]*Job, len(rows))
	for _, j := range rows {
		m[j.InfoHash] = j
	}
	return m, nil
}

func (p *Postgres) NodeForInfoHash(ctx context.Context, infoHash string) string {
	if p == nil {
		return ""
	}
	var node string
	p.pool.QueryRow(ctx,
		`SELECT COALESCE(node,'') FROM jobs WHERE lower(info_hash)=lower($1) AND status='complete' ORDER BY updated_at DESC LIMIT 1`,
		infoHash).Scan(&node)
	return node
}

func (p *Postgres) ListByUser(ctx context.Context, userID string, limit int) ([]*Job, error) {
	return p.query(ctx, `SELECT `+cols+` FROM jobs WHERE user_id=$1 ORDER BY created_at DESC, id DESC LIMIT $2`, userID, limit)
}

func (p *Postgres) ListByUserBefore(ctx context.Context, userID string, before time.Time, beforeID string, limit int) ([]*Job, error) {
	return p.query(ctx, `SELECT `+cols+` FROM jobs WHERE user_id=$1 AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4`, userID, before, beforeID, limit)
}

func (p *Postgres) ListByStatus(ctx context.Context, status Status) ([]*Job, error) {
	return p.query(ctx, `SELECT `+cols+` FROM jobs WHERE status=$1 ORDER BY priority DESC, created_at ASC, id ASC`, string(status))
}

func (p *Postgres) ListByIMDB(ctx context.Context, imdbID string) ([]*Job, error) {
	return p.query(ctx, `SELECT `+cols+` FROM jobs WHERE imdb_id=$1 AND status='complete'`, imdbID)
}

func (p *Postgres) ListByTitleNorm(ctx context.Context, norm string) ([]*Job, error) {
	return p.query(ctx, `SELECT `+cols+` FROM jobs WHERE title_norm=$1 AND status='complete' ORDER BY created_at DESC`, norm)
}

func (p *Postgres) SetIMDB(ctx context.Context, infoHash, imdbID string) error {
	if infoHash == "" || imdbID == "" {
		return nil
	}
	_, err := p.pool.Exec(ctx, `UPDATE jobs SET imdb_id=$2 WHERE info_hash=$1 AND COALESCE(imdb_id,'')=''`, infoHash, imdbID)
	return err
}

func (p *Postgres) UntaggedComplete(ctx context.Context, limit int) ([]*Job, error) {
	return p.query(ctx, `SELECT `+cols+` FROM jobs
		WHERE status='complete' AND COALESCE(imdb_id,'')='' AND COALESCE(info_hash,'')<>''
		ORDER BY random() LIMIT $1`, limit)
}

func (p *Postgres) BackfillTitleNorm(ctx context.Context) (int, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, name, COALESCE(title_norm,'') FROM jobs WHERE status IN ('complete','seeding')`)
	if err != nil {
		return 0, err
	}
	type fix struct{ id, norm string }
	var fixes []fix
	for rows.Next() {
		var id, name, old string
		if rows.Scan(&id, &name, &old) != nil {
			continue
		}
		want := titleNormFromName(name)
		if want == "" || want == old {
			continue
		}
		if old != "" && strings.HasPrefix(want, old) && len(want) > len(old) {
			continue
		}
		fixes = append(fixes, fix{id, want})
	}
	rows.Close()
	n := 0
	for _, f := range fixes {
		if _, err := p.pool.Exec(ctx, `UPDATE jobs SET title_norm=$2 WHERE id=$1`, f.id, f.norm); err == nil {
			n++
		}
	}
	return n, nil
}

func (p *Postgres) Delete(ctx context.Context, id string) error {
	var infoHash string
	err := p.pool.QueryRow(ctx, `DELETE FROM jobs WHERE id=$1 RETURNING info_hash`, id).Scan(&infoHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var n int
	if err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE info_hash=$1`, infoHash).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		p.DropBlobRefs(ctx, infoHash)
	}
	return nil
}

func (p *Postgres) ActiveCount(ctx context.Context, userID string) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM jobs WHERE user_id=$1 AND seed=false AND status IN `+concurrencyStates, userID).Scan(&n)
	return n, err
}

func (p *Postgres) DownloadingCount(ctx context.Context, userID string) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM jobs WHERE user_id=$1 AND seed=false AND status IN `+downloadingStates, userID).Scan(&n)
	return n, err
}

func (p *Postgres) BudgetUsed(ctx context.Context) (int64, error) {
	var total int64
	err := p.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(CASE WHEN file_size > 0 THEN file_size ELSE 5000000000 END), 0)
		 FROM jobs WHERE status IN `+budgetStates).Scan(&total)
	return total, err
}

func (p *Postgres) query(ctx context.Context, sql string, args ...any) ([]*Job, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanOne(row pgx.Row) (*Job, error) {
	j, err := scan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return j, err
}

func scan(s scanner) (*Job, error) {
	var j Job
	var source, status string
	var files, idxs []byte
	err := s.Scan(&j.ID, &j.UserID, &j.InfoHash, &j.Name, &j.Magnet, &source, &status,
		&j.Error, &files, &idxs, &j.IMDBID, &j.Season, &j.Episode, &j.FileSize, &j.MaxBytes, &j.Priority,
		&j.CreatedAt, &j.UpdatedAt, &j.Progress, &j.Speed, &j.Node, &j.Seed, &j.BudgetGated, &j.InputKey)
	if err != nil {
		return nil, err
	}
	j.Source, j.Status = Source(source), Status(status)
	if len(files) > 0 {
		json.Unmarshal(files, &j.Files)
	}
	if len(idxs) > 0 {
		json.Unmarshal(idxs, &j.SelectedIdxs)
	}
	return &j, nil
}
