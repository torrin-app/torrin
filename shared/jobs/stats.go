package jobs

import (
	"context"
	"sort"
	"time"
)

type UserStats struct {
	TotalDownloads int   `json:"total_downloads"`
	ActiveJobs     int   `json:"active_jobs"`
	CompletedJobs  int   `json:"completed_jobs"`
	FailedJobs     int   `json:"failed_jobs"`
	TotalBytes     int64 `json:"total_bytes"`
	TotalAccesses  int64 `json:"total_accesses"`
}

type HistoryEntry struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	InfoHash     string `json:"info_hash"`
	Status       Status `json:"status"`
	FileSize     int64  `json:"file_size"`
	AccessCount  int64  `json:"access_count"`
	CreatedAt    string `json:"created_at"`
	LastAccessed string `json:"last_accessed,omitempty"`
}

type NodeStat struct {
	Node        string         `json:"node"`
	CachedBytes int64          `json:"cached_bytes"`
	ByStatus    map[string]int `json:"by_status"`
}

type nodeStatusRow struct {
	Node   string
	Status string
	Count  int
}

type nodeCacheRow struct {
	Node  string
	Bytes int64
}

func buildNodeStats(status []nodeStatusRow, cache []nodeCacheRow) []NodeStat {
	nodes := map[string]*NodeStat{}
	pick := func(n string) *NodeStat {
		if n == "" {
			n = "box1"
		}
		if nodes[n] == nil {
			nodes[n] = &NodeStat{Node: n, ByStatus: map[string]int{}}
		}
		return nodes[n]
	}
	for _, r := range status {
		pick(r.Node).ByStatus[r.Status] = r.Count
	}
	for _, c := range cache {
		pick(c.Node).CachedBytes = c.Bytes
	}
	out := make([]NodeStat, 0, len(nodes))
	for _, v := range nodes {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	return out
}

func (p *Postgres) NodeSummary(ctx context.Context) ([]NodeStat, error) {
	var status []nodeStatusRow
	rows, err := p.pool.Query(ctx, `SELECT COALESCE(node,''), status, COUNT(*) FROM jobs GROUP BY node, status`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r nodeStatusRow
		if rows.Scan(&r.Node, &r.Status, &r.Count) == nil {
			status = append(status, r)
		}
	}
	rows.Close()

	var cache []nodeCacheRow
	crows, err := p.pool.Query(ctx, `
		SELECT COALESCE(node,''), COALESCE(SUM(m),0) FROM (
			SELECT node, info_hash, MAX(file_size) m FROM jobs WHERE status='complete' GROUP BY node, info_hash
		) t GROUP BY node`)
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var c nodeCacheRow
		if crows.Scan(&c.Node, &c.Bytes) == nil {
			cache = append(cache, c)
		}
	}
	crows.Close()

	return buildNodeStats(status, cache), nil
}

func (p *Postgres) GetUserStats(ctx context.Context, userID string) (*UserStats, error) {
	st := &UserStats{}
	p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE user_id=$1`, userID).Scan(&st.TotalDownloads)
	p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE user_id=$1 AND status IN `+activeStates, userID).Scan(&st.ActiveJobs)
	p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE user_id=$1 AND status='complete'`, userID).Scan(&st.CompletedJobs)
	p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE user_id=$1 AND status='failed'`, userID).Scan(&st.FailedJobs)
	p.pool.QueryRow(ctx, `SELECT COALESCE(SUM(file_size),0) FROM jobs WHERE user_id=$1 AND status='complete'`, userID).Scan(&st.TotalBytes)
	p.pool.QueryRow(ctx, `SELECT COALESCE(SUM(access_count),0) FROM jobs WHERE user_id=$1`, userID).Scan(&st.TotalAccesses)
	return st, nil
}

func (p *Postgres) GetUserHistory(ctx context.Context, userID string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, name, info_hash, status, file_size, access_count, created_at, last_accessed_at
		FROM jobs WHERE user_id=$1 AND status IN ('complete','evicted')
		ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []HistoryEntry{}
	for rows.Next() {
		var h HistoryEntry
		var status string
		var created time.Time
		var last *time.Time
		if err := rows.Scan(&h.ID, &h.Name, &h.InfoHash, &status, &h.FileSize, &h.AccessCount, &created, &last); err != nil {
			return nil, err
		}
		h.Status = Status(status)
		h.CreatedAt = created.UTC().Format(time.RFC3339)
		if last != nil {
			h.LastAccessed = last.UTC().Format(time.RFC3339)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
