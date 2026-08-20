package jobs

import (
	"context"
	"encoding/json"
)

type PrewarmFallback struct {
	JobID     string
	IMDBID    string
	Name      string
	Remaining []string
}

func (p *Postgres) CachedSizeByUser(ctx context.Context, userID string) (int64, error) {
	var n int64
	err := p.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(file_size),0) FROM (
			SELECT MAX(file_size) AS file_size FROM jobs
			WHERE user_id=$1 AND status='complete' GROUP BY info_hash) t`, userID).Scan(&n)
	return n, err
}

func (p *Postgres) StorePrewarmFallback(ctx context.Context, jobID, imdbID, name string, remaining []string) error {
	if len(remaining) == 0 {
		return nil
	}
	b, _ := json.Marshal(remaining)
	_, err := p.pool.Exec(ctx,
		`INSERT INTO prewarm_fallbacks (job_id, imdb_id, name, remaining, updated_at)
		 VALUES ($1,$2,$3,$4,now())
		 ON CONFLICT (job_id) DO UPDATE SET imdb_id=EXCLUDED.imdb_id, name=EXCLUDED.name,
		 remaining=EXCLUDED.remaining, updated_at=now()`,
		jobID, imdbID, name, b)
	return err
}

func (p *Postgres) ListPrewarmFallbacks(ctx context.Context) ([]PrewarmFallback, error) {
	rows, err := p.pool.Query(ctx, `SELECT job_id, imdb_id, name, remaining FROM prewarm_fallbacks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrewarmFallback
	for rows.Next() {
		var f PrewarmFallback
		var rem []byte
		if rows.Scan(&f.JobID, &f.IMDBID, &f.Name, &rem) != nil {
			continue
		}
		json.Unmarshal(rem, &f.Remaining)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (p *Postgres) DeletePrewarmFallback(ctx context.Context, jobID string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM prewarm_fallbacks WHERE job_id=$1`, jobID)
	return err
}
