package auth

import (
	"context"
	"time"

	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/usenet/indexer"
)

type IndexerEntry struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Label     string    `json:"label"`
	URL       string    `json:"url"`
	APIKey    string    `json:"api_key,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) queryIndexers(ctx context.Context, sql string, args ...any) ([]*IndexerEntry, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*IndexerEntry
	for rows.Next() {
		e := &IndexerEntry{}
		if rows.Scan(&e.ID, &e.UserID, &e.Label, &e.URL, &e.APIKey, &e.Enabled, &e.CreatedAt) == nil {
			e.APIKey = s.dec(e.APIKey)
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

func (s *Store) ListIndexers(ctx context.Context, userID string) ([]*IndexerEntry, error) {
	return s.queryIndexers(ctx,
		`SELECT id, user_id, label, url, api_key, enabled, created_at
		 FROM usenet_indexer_list WHERE user_id=$1 ORDER BY created_at`, userID)
}

func (s *Store) ListEnabledIndexers(ctx context.Context, userID string) ([]*IndexerEntry, error) {
	return s.queryIndexers(ctx,
		`SELECT id, user_id, label, url, api_key, enabled, created_at
		 FROM usenet_indexer_list WHERE user_id=$1 AND enabled=true ORDER BY created_at`, userID)
}

func (s *Store) GetIndexer(ctx context.Context, id, userID string) (*IndexerEntry, error) {
	list, err := s.queryIndexers(ctx,
		`SELECT id, user_id, label, url, api_key, enabled, created_at
		 FROM usenet_indexer_list WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return list[0], nil
}

func (s *Store) CountIndexers(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM usenet_indexer_list WHERE user_id=$1`, userID).Scan(&n)
	return n, err
}

func (s *Store) AddIndexer(ctx context.Context, e *IndexerEntry) error {
	if e.ID == "" {
		e.ID = newID()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO usenet_indexer_list (id, user_id, label, url, api_key, enabled) VALUES ($1,$2,$3,$4,$5,true)`,
		e.ID, e.UserID, e.Label, e.URL, s.enc(e.APIKey))
	return err
}

func (s *Store) UpdateIndexer(ctx context.Context, id, userID, label, url, apiKey string) error {
	if apiKey == "" {
		_, err := s.pool.Exec(ctx,
			`UPDATE usenet_indexer_list SET label=$1, url=$2, updated_at=now() WHERE id=$3 AND user_id=$4`,
			label, url, id, userID)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE usenet_indexer_list SET label=$1, url=$2, api_key=$3, updated_at=now() WHERE id=$4 AND user_id=$5`,
		label, url, s.enc(apiKey), id, userID)
	return err
}

func (s *Store) ToggleIndexer(ctx context.Context, id, userID string, enabled bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE usenet_indexer_list SET enabled=$1, updated_at=now() WHERE id=$2 AND user_id=$3`, enabled, id, userID)
	return err
}

func (s *Store) DeleteIndexer(ctx context.Context, id, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM usenet_indexer_list WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

func (s *Store) IndexerSources(ctx context.Context, userID string, plan plans.Plan, sysURL, sysKey string) []indexer.Source {
	sysOn, _ := s.SystemIndexerEnabled(ctx, userID)
	allowBYO, allowSystem := plans.IndexerPolicy(plan, sysOn)
	var sources []indexer.Source
	if allowBYO {
		list, _ := s.ListEnabledIndexers(ctx, userID)
		for _, e := range list {
			if e.URL == "" {
				continue
			}
			sources = append(sources, indexer.Source{ID: e.ID, Label: e.Label, Client: indexer.NewClient(e.URL, e.APIKey)})
		}
	}
	if allowSystem && sysURL != "" {
		sources = append(sources, indexer.Source{ID: "system", Label: "Torrin", Client: indexer.NewClient(sysURL, sysKey)})
	}
	return sources
}

func (s *Store) SystemIndexerEnabled(ctx context.Context, userID string) (bool, error) {
	var on bool
	err := s.pool.QueryRow(ctx, `SELECT system_indexer_enabled FROM users WHERE id=$1`, userID).Scan(&on)
	return on, err
}

func (s *Store) SetSystemIndexerEnabled(ctx context.Context, userID string, on bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET system_indexer_enabled=$1, updated_at=now() WHERE id=$2`, on, userID)
	return err
}
