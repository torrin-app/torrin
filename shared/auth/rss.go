package auth

import (
	"context"
	"encoding/json"
	"time"
)

type RSSFeed struct {
	ID        string          `json:"id"`
	UserID    string          `json:"user_id"`
	URL       string          `json:"url"`
	Name      string          `json:"name"`
	Filter    string          `json:"filter"`
	Criteria  json.RawMessage `json:"criteria"`
	Enabled   bool            `json:"enabled"`
	LastCheck time.Time       `json:"last_check"`
	CreatedAt time.Time       `json:"created_at"`
}

func (s *Store) AddRSSFeed(ctx context.Context, f *RSSFeed) error {
	if len(f.Criteria) == 0 {
		f.Criteria = json.RawMessage("{}")
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO rss_feeds (id, user_id, url, name, filter, criteria, enabled) VALUES ($1,$2,$3,$4,$5,$6,true)`,
		f.ID, f.UserID, f.URL, f.Name, f.Filter, f.Criteria)
	return err
}

func (s *Store) UpdateRSSFeed(ctx context.Context, id, userID, url, name, filter string, criteria json.RawMessage) error {
	if len(criteria) == 0 {
		criteria = json.RawMessage("{}")
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE rss_feeds SET url=$1, name=$2, filter=$3, criteria=$4 WHERE id=$5 AND user_id=$6`,
		url, name, filter, criteria, id, userID)
	return err
}

func (s *Store) ListRSSFeeds(ctx context.Context, userID string) ([]*RSSFeed, error) {
	return s.queryFeeds(ctx,
		`SELECT id, user_id, url, name, filter, criteria, enabled, COALESCE(last_check, to_timestamp(0)), created_at
		 FROM rss_feeds WHERE user_id=$1 ORDER BY created_at DESC`, userID)
}

func (s *Store) GetAllEnabledFeeds(ctx context.Context) ([]*RSSFeed, error) {
	return s.queryFeeds(ctx,
		`SELECT id, user_id, url, name, filter, criteria, enabled, COALESCE(last_check, to_timestamp(0)), created_at
		 FROM rss_feeds WHERE enabled=true`)
}

func (s *Store) queryFeeds(ctx context.Context, sql string, args ...any) ([]*RSSFeed, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var feeds []*RSSFeed
	for rows.Next() {
		f := &RSSFeed{}
		if rows.Scan(&f.ID, &f.UserID, &f.URL, &f.Name, &f.Filter, &f.Criteria, &f.Enabled, &f.LastCheck, &f.CreatedAt) == nil {
			feeds = append(feeds, f)
		}
	}
	return feeds, rows.Err()
}

func (s *Store) GetRSSFeed(ctx context.Context, id, userID string) (*RSSFeed, error) {
	feeds, err := s.queryFeeds(ctx,
		`SELECT id, user_id, url, name, filter, criteria, enabled, COALESCE(last_check, to_timestamp(0)), created_at
		 FROM rss_feeds WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil || len(feeds) == 0 {
		return nil, err
	}
	return feeds[0], nil
}

func (s *Store) ClearRSSSeen(ctx context.Context, feedID string) {
	s.pool.Exec(ctx, `DELETE FROM rss_seen WHERE feed_id=$1`, feedID)
}

func (s *Store) DeleteRSSFeed(ctx context.Context, id, userID string) error {
	s.ClearRSSSeen(ctx, id)
	_, err := s.pool.Exec(ctx, `DELETE FROM rss_feeds WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

func (s *Store) ToggleRSSFeed(ctx context.Context, id, userID string, enabled bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE rss_feeds SET enabled=$1 WHERE id=$2 AND user_id=$3`, enabled, id, userID)
	return err
}

func (s *Store) MarkFeedChecked(ctx context.Context, feedID string) {
	s.pool.Exec(ctx, `UPDATE rss_feeds SET last_check=now() WHERE id=$1`, feedID)
}

func (s *Store) IsRSSSeen(ctx context.Context, feedID, guid string) bool {
	var n int
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM rss_seen WHERE feed_id=$1 AND guid=$2 AND done`, feedID, guid).Scan(&n)
	return n > 0
}

func (s *Store) MarkRSSSeen(ctx context.Context, feedID, guid string) {
	s.pool.Exec(ctx, `INSERT INTO rss_seen (feed_id, guid, done) VALUES ($1,$2,true)
		ON CONFLICT (feed_id, guid) DO UPDATE SET done=true`, feedID, guid)
}

func (s *Store) RSSAttempt(ctx context.Context, feedID, guid string) int {
	var n int
	s.pool.QueryRow(ctx, `INSERT INTO rss_seen (feed_id, guid, attempts, done) VALUES ($1,$2,1,false)
		ON CONFLICT (feed_id, guid) DO UPDATE SET attempts = rss_seen.attempts + 1
		RETURNING attempts`, feedID, guid).Scan(&n)
	return n
}
