package jobs

import "context"

type EvictionCandidate struct {
	ID              string
	InfoHash        string
	Name            string
	FileSize        int64
	AccessCount     int
	DaysSinceAccess int
	IsPrewarm       bool
}

func (p *Postgres) RecordView(ctx context.Context, infoHash, userID string) (bool, error) {
	ct, err := p.pool.Exec(ctx,
		`INSERT INTO views (info_hash, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, infoHash, userID)
	if err != nil {
		return false, err
	}
	if ct.RowsAffected() == 0 {
		return false, nil
	}
	p.pool.Exec(ctx, `UPDATE jobs SET last_accessed_at=now(), access_count=access_count+1, updated_at=now()
		WHERE info_hash=$1 AND status IN ('complete','seeding')`, infoHash)
	return true, nil
}

func (p *Postgres) GetTotalCachedSize(ctx context.Context, node string) (int64, error) {
	var total int64
	err := p.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(max_size), 0) FROM (
			SELECT MAX(file_size) AS max_size FROM jobs WHERE status='complete' AND node=$1 GROUP BY info_hash
		) t`, node).Scan(&total)
	return total, err
}

func (p *Postgres) GetTotalCachedSizeAll(ctx context.Context) (int64, error) {
	var total int64
	err := p.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(max_size), 0) FROM (
			SELECT MAX(file_size) AS max_size FROM jobs WHERE status='complete' GROUP BY info_hash
		) t`).Scan(&total)
	return total, err
}

func (p *Postgres) GetEvictionCandidates(ctx context.Context, node string) ([]EvictionCandidate, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT MIN(id), info_hash, MAX(name), MAX(file_size), COALESCE(SUM(access_count), 0) AS access_total,
			CAST(EXTRACT(EPOCH FROM (now() - MAX(COALESCE(last_accessed_at, created_at)))) / 86400 AS INTEGER) AS days_inactive,
			MAX(CASE WHEN user_id='prewarm' THEN 1 ELSE 0 END) AS is_prewarm
		FROM jobs
		WHERE status='complete' AND node=$1
		GROUP BY info_hash
		ORDER BY
			CASE WHEN COALESCE(SUM(access_count),0)=0 THEN 0
			     WHEN COALESCE(SUM(access_count),0)<10 THEN 1
			     ELSE 2 END ASC,
			days_inactive DESC,
			MAX(file_size) DESC`, node)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EvictionCandidate
	for rows.Next() {
		var c EvictionCandidate
		var prewarm int
		if err := rows.Scan(&c.ID, &c.InfoHash, &c.Name, &c.FileSize, &c.AccessCount, &c.DaysSinceAccess, &prewarm); err != nil {
			continue
		}
		c.IsPrewarm = prewarm == 1
		out = append(out, c)
	}
	return out, rows.Err()
}
