package jobs

import (
	"context"
	"strconv"
	"time"
)

func (p *Postgres) JobStatusCounts(ctx context.Context) (map[string]int, error) {
	rows, err := p.pool.Query(ctx, `SELECT status, COUNT(*) FROM jobs GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if rows.Scan(&st, &n) == nil {
			out[st] = n
		}
	}
	return out, rows.Err()
}

type AdminJob struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	InfoHash  string    `json:"info_hash"`
	Name      string    `json:"name"`
	Source    string    `json:"source"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	FileSize  int64     `json:"file_size"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (p *Postgres) AdminListJobs(ctx context.Context, status, q string, limit, offset int) ([]AdminJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT j.id, j.user_id, COALESCE(u.email,''), j.info_hash, j.name, j.source, j.status,
			COALESCE(j.error,''), j.file_size, j.created_at, j.updated_at
		FROM jobs j LEFT JOIN users u ON u.id=j.user_id WHERE 1=1`
	args := []any{}
	i := 1
	if status != "" {
		query += ` AND j.status = $` + strconv.Itoa(i)
		args = append(args, status)
		i++
	}
	if q != "" {
		query += ` AND j.name ILIKE $` + strconv.Itoa(i)
		args = append(args, "%"+q+"%")
		i++
	}
	query += ` ORDER BY j.created_at DESC LIMIT $` + strconv.Itoa(i) + ` OFFSET $` + strconv.Itoa(i+1)
	args = append(args, limit, offset)

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminJob{}
	for rows.Next() {
		var j AdminJob
		if rows.Scan(&j.ID, &j.UserID, &j.Email, &j.InfoHash, &j.Name, &j.Source, &j.Status,
			&j.Error, &j.FileSize, &j.CreatedAt, &j.UpdatedAt) == nil {
			out = append(out, j)
		}
	}
	return out, rows.Err()
}

type CacheItem struct {
	ID           string    `json:"id"`
	InfoHash     string    `json:"info_hash"`
	Name         string    `json:"name"`
	FileSize     int64     `json:"file_size"`
	AccessCount  int64     `json:"access_count"`
	LastAccessed time.Time `json:"last_accessed"`
	CreatedAt    time.Time `json:"created_at"`
}

func (p *Postgres) CacheTopItems(ctx context.Context, limit int) ([]CacheItem, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, info_hash, name, file_size, access_count,
			COALESCE(last_accessed_at, created_at), created_at
		FROM jobs WHERE status='complete'
		ORDER BY access_count DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CacheItem{}
	for rows.Next() {
		var c CacheItem
		if rows.Scan(&c.ID, &c.InfoHash, &c.Name, &c.FileSize, &c.AccessCount, &c.LastAccessed, &c.CreatedAt) == nil {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

func (p *Postgres) CacheEvictionCandidates(ctx context.Context, limit int) ([]CacheItem, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, info_hash, name, file_size, access_count,
			COALESCE(last_accessed_at, created_at), created_at
		FROM jobs WHERE status='complete'
		ORDER BY
			CASE WHEN access_count=0 THEN 0 WHEN access_count<10 THEN 1 ELSE 2 END ASC,
			COALESCE(last_accessed_at, created_at) ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CacheItem{}
	for rows.Next() {
		var c CacheItem
		if rows.Scan(&c.ID, &c.InfoHash, &c.Name, &c.FileSize, &c.AccessCount, &c.LastAccessed, &c.CreatedAt) == nil {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

func (p *Postgres) DeleteCachedByHash(ctx context.Context, infoHash string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM jobs WHERE info_hash=$1 AND status='complete'`, infoHash)
	return err
}
