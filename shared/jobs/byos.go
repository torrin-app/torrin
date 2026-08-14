package jobs

import (
	"context"
	"encoding/json"
)

type BYOSQueueItem struct {
	JobID  string
	UserID string
}

func (p *Postgres) EnqueueBYOS(ctx context.Context, jobID, userID string) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO byos_queue (job_id, user_id) VALUES ($1,$2) ON CONFLICT (job_id) DO NOTHING`, jobID, userID)
	return err
}

func (p *Postgres) ListBYOSQueue(ctx context.Context, nodeID string) ([]BYOSQueueItem, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT q.job_id, q.user_id FROM byos_queue q
		LEFT JOIN jobs j ON j.id = q.job_id
		WHERE COALESCE(j.node, '') = $1
		ORDER BY q.created_at ASC`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BYOSQueueItem
	for rows.Next() {
		var it BYOSQueueItem
		if rows.Scan(&it.JobID, &it.UserID) == nil {
			out = append(out, it)
		}
	}
	return out, rows.Err()
}

func (p *Postgres) DeleteBYOSQueue(ctx context.Context, jobID string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM byos_queue WHERE job_id=$1`, jobID)
	return err
}

func (p *Postgres) IncrementBYOSAttempt(ctx context.Context, jobID string) int {
	var n int
	p.pool.QueryRow(ctx, `UPDATE byos_queue SET attempts=attempts+1 WHERE job_id=$1 RETURNING attempts`, jobID).Scan(&n)
	return n
}

type BYOSObject struct {
	UserID   string `json:"user_id"`
	Bucket   string `json:"bucket"`
	InfoHash string `json:"info_hash"`
	Name     string `json:"name"`
	Files    []File `json:"files"`
}

func (p *Postgres) MarkBYOSObject(ctx context.Context, jobID, userID, infoHash, bucket, name string, files []File) error {
	data, _ := json.Marshal(files)
	_, err := p.pool.Exec(ctx, `
		INSERT INTO byos_objects (job_id, user_id, bucket, info_hash, name, streams_json)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (job_id) DO UPDATE SET user_id=EXCLUDED.user_id, bucket=EXCLUDED.bucket,
			info_hash=EXCLUDED.info_hash, name=EXCLUDED.name, streams_json=EXCLUDED.streams_json`,
		jobID, userID, bucket, infoHash, name, data)
	return err
}

func (p *Postgres) HasBYOSObject(ctx context.Context, jobID string) bool {
	var x int
	p.pool.QueryRow(ctx, `SELECT 1 FROM byos_objects WHERE job_id=$1`, jobID).Scan(&x)
	return x == 1
}

func (p *Postgres) ListUserByosByIMDB(ctx context.Context, userID, imdbID string) ([]*Job, error) {
	return p.query(ctx, `SELECT `+cols+` FROM jobs
		WHERE imdb_id=$1 AND info_hash IN (SELECT info_hash FROM byos_objects WHERE user_id=$2)`, imdbID, userID)
}

type BYOSEvictCandidate struct {
	JobID           string
	InfoHash        string
	Name            string
	FileSize        int64
	DaysSinceAccess int
}

func (p *Postgres) BYOSEvictionCandidates(ctx context.Context, userID string) ([]BYOSEvictCandidate, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT o.job_id, o.info_hash, o.name, COALESCE(j.file_size, 0),
			CAST(EXTRACT(EPOCH FROM (now() - COALESCE(j.last_accessed_at, j.created_at))) / 86400 AS INTEGER)
		FROM byos_objects o JOIN jobs j ON j.id = o.job_id
		WHERE o.user_id = $1
		ORDER BY COALESCE(j.last_accessed_at, j.created_at) ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BYOSEvictCandidate
	for rows.Next() {
		var c BYOSEvictCandidate
		if rows.Scan(&c.JobID, &c.InfoHash, &c.Name, &c.FileSize, &c.DaysSinceAccess) == nil {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

func (p *Postgres) DeleteBYOSObject(ctx context.Context, jobID string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM byos_objects WHERE job_id=$1`, jobID)
	return err
}

func (p *Postgres) GetBYOSObject(ctx context.Context, jobID string) (*BYOSObject, bool) {
	return p.scanBYOS(p.pool.QueryRow(ctx,
		`SELECT user_id, bucket, info_hash, name, streams_json FROM byos_objects WHERE job_id=$1`, jobID))
}

func (p *Postgres) GetBYOSObjectByUserHash(ctx context.Context, userID, infoHash string) (*BYOSObject, bool) {
	return p.scanBYOS(p.pool.QueryRow(ctx,
		`SELECT user_id, bucket, info_hash, name, streams_json FROM byos_objects
		 WHERE user_id=$1 AND info_hash=$2 ORDER BY created_at DESC LIMIT 1`, userID, infoHash))
}

type rowScanner interface{ Scan(...any) error }

func (p *Postgres) scanBYOS(row rowScanner) (*BYOSObject, bool) {
	o := &BYOSObject{}
	var files []byte
	if row.Scan(&o.UserID, &o.Bucket, &o.InfoHash, &o.Name, &files) != nil {
		return nil, false
	}
	json.Unmarshal(files, &o.Files)
	return o, true
}
