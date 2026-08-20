package jobs

import (
	"context"
	"time"
)

type WrappedContent struct {
	Name     string `json:"name"`
	InfoHash string `json:"info_hash"`
	ImdbID   string `json:"imdb_id,omitempty"`
	Views    int64  `json:"views"`
	Size     int64  `json:"size"`
}

type UserWrapped struct {
	TotalDownloads int              `json:"total_downloads"`
	TotalCached    int              `json:"total_cached"`
	TotalStreams   int64            `json:"total_streams"`
	TotalBytes     int64            `json:"total_bytes"`
	ActiveDays     int              `json:"active_days"`
	Streak         int              `json:"streak_days"`
	BiggestFile    int64            `json:"biggest_file"`
	TopContent     []WrappedContent `json:"top_content"`
	BySource       map[string]int   `json:"by_source"`
	MemberSince    string           `json:"member_since"`
}

type PlatformWrapped struct {
	TotalUsers     int              `json:"total_users"`
	TotalDownloads int              `json:"total_downloads"`
	TotalCached    int              `json:"total_cached"`
	TotalStreams   int64            `json:"total_streams"`
	TotalBytes     int64            `json:"total_bytes"`
	UniqueHashes   int              `json:"unique_hashes"`
	TopContent     []WrappedContent `json:"top_content"`
	BySource       map[string]int   `json:"by_source"`
	ActiveToday    int              `json:"active_today"`
}

type MetricsSnapshot struct {
	Date        string `json:"date"`
	CachedCount int    `json:"cached_count"`
	CachedSize  int64  `json:"cached_size"`
	TotalViews  int    `json:"total_views"`
	TotalUsers  int    `json:"total_users"`
}

func (p *Postgres) GetUserWrapped(ctx context.Context, userID string) (*UserWrapped, error) {
	w := &UserWrapped{BySource: make(map[string]int)}
	p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE user_id=$1`, userID).Scan(&w.TotalDownloads)
	p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE user_id=$1 AND status='complete'`, userID).Scan(&w.TotalCached)
	p.pool.QueryRow(ctx, `SELECT COALESCE(SUM(access_count),0) FROM jobs WHERE user_id=$1`, userID).Scan(&w.TotalStreams)
	w.TotalBytes, _ = p.CachedSizeByUser(ctx, userID)
	p.pool.QueryRow(ctx, `SELECT COALESCE(MAX(file_size),0) FROM jobs WHERE user_id=$1 AND status='complete'`, userID).Scan(&w.BiggestFile)
	p.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT created_at::date) FROM jobs WHERE user_id=$1`, userID).Scan(&w.ActiveDays)

	w.TopContent = p.topContent(ctx, `
		SELECT name, info_hash, imdb_id, access_count, file_size
		FROM jobs WHERE user_id=$1 AND status='complete' AND name != ''
		ORDER BY access_count DESC LIMIT 5`, userID)
	p.bySource(ctx, w.BySource, `SELECT COALESCE(NULLIF(source,''),'torrent'), COUNT(*) FROM jobs WHERE user_id=$1 GROUP BY source`, userID)

	rows, err := p.pool.Query(ctx, `SELECT DISTINCT created_at::date d FROM jobs WHERE user_id=$1 ORDER BY d DESC`, userID)
	if err == nil {
		defer rows.Close()
		var days []time.Time
		for rows.Next() {
			var d time.Time
			if rows.Scan(&d) == nil {
				days = append(days, d)
			}
		}
		w.Streak = calcStreak(days)
	}
	return w, nil
}

func (p *Postgres) GetPlatformWrapped(ctx context.Context) (*PlatformWrapped, error) {
	pw := &PlatformWrapped{BySource: make(map[string]int)}
	const realUsers = `user_id NOT IN ('', 'system', 'prewarm')`
	p.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT user_id) FROM jobs WHERE `+realUsers).Scan(&pw.TotalUsers)
	p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs`).Scan(&pw.TotalDownloads)
	p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE status='complete'`).Scan(&pw.TotalCached)
	p.pool.QueryRow(ctx, `SELECT COALESCE(SUM(access_count),0) FROM jobs`).Scan(&pw.TotalStreams)
	p.pool.QueryRow(ctx, `SELECT COALESCE(SUM(file_size),0) FROM jobs WHERE status='complete'`).Scan(&pw.TotalBytes)
	p.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT info_hash) FROM jobs WHERE status='complete'`).Scan(&pw.UniqueHashes)
	p.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT user_id) FROM jobs WHERE created_at::date = CURRENT_DATE AND `+realUsers).Scan(&pw.ActiveToday)

	pw.TopContent = p.topContent(ctx, `
		SELECT MAX(name), info_hash, MAX(imdb_id), SUM(access_count) AS views, MAX(file_size) AS size
		FROM jobs WHERE status='complete' AND name != ''
		GROUP BY info_hash ORDER BY views DESC LIMIT 10`)
	p.bySource(ctx, pw.BySource, `SELECT COALESCE(NULLIF(source,''),'torrent'), COUNT(*) FROM jobs GROUP BY source`)
	return pw, nil
}

func (p *Postgres) topContent(ctx context.Context, sql string, args ...any) []WrappedContent {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []WrappedContent
	for rows.Next() {
		var c WrappedContent
		if rows.Scan(&c.Name, &c.InfoHash, &c.ImdbID, &c.Views, &c.Size) == nil {
			out = append(out, c)
		}
	}
	return out
}

func (p *Postgres) bySource(ctx context.Context, dst map[string]int, sql string, args ...any) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var src string
		var n int
		if rows.Scan(&src, &n) == nil {
			dst[src] = n
		}
	}
}

func calcStreak(days []time.Time) int {
	if len(days) == 0 {
		return 0
	}
	streak := 1
	for i := 1; i < len(days); i++ {
		if days[i-1].Sub(days[i]).Hours() == 24 {
			streak++
			continue
		}
		break
	}
	return streak
}

func (p *Postgres) CurrentMetrics(ctx context.Context, totalUsers int) MetricsSnapshot {
	m := MetricsSnapshot{Date: time.Now().UTC().Format("2006-01-02"), TotalUsers: totalUsers}
	p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE status='complete'`).Scan(&m.CachedCount)
	p.pool.QueryRow(ctx, `SELECT COALESCE(SUM(mx),0) FROM (SELECT MAX(file_size) mx FROM jobs WHERE status='complete' GROUP BY info_hash) t`).Scan(&m.CachedSize)
	p.pool.QueryRow(ctx, `SELECT COALESCE(SUM(access_count),0) FROM jobs`).Scan(&m.TotalViews)
	return m
}

func (p *Postgres) RecordDailySnapshot(ctx context.Context, totalUsers int) error {
	m := p.CurrentMetrics(ctx, totalUsers)
	_, err := p.pool.Exec(ctx, `
		INSERT INTO metrics_snapshots (date, cached_count, cached_size, total_views, total_users)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (date) DO UPDATE SET cached_count=EXCLUDED.cached_count,
			cached_size=EXCLUDED.cached_size, total_views=EXCLUDED.total_views, total_users=EXCLUDED.total_users`,
		m.Date, m.CachedCount, m.CachedSize, m.TotalViews, m.TotalUsers)
	return err
}

func (p *Postgres) GetMetricsHistory(ctx context.Context, days int) ([]MetricsSnapshot, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT to_char(date,'YYYY-MM-DD'), cached_count, cached_size, total_views, total_users
		FROM metrics_snapshots ORDER BY date DESC LIMIT $1`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetricsSnapshot{}
	for rows.Next() {
		var m MetricsSnapshot
		if err := rows.Scan(&m.Date, &m.CachedCount, &m.CachedSize, &m.TotalViews, &m.TotalUsers); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
