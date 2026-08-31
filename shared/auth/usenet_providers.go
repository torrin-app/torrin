package auth

import (
	"context"

	"github.com/google/uuid"
)

type UsenetProvider struct {
	ID       string `json:"id"`
	UserID   string `json:"user_id"`
	Label    string `json:"label"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	SSL      bool   `json:"ssl"`
	MaxConns int    `json:"max_conns"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
}

const usenetProviderCols = `id, user_id, label, host, port, username, password, ssl, max_conns, priority, enabled`

func (s *Store) queryUsenetProviders(ctx context.Context, sql string, args ...any) ([]*UsenetProvider, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*UsenetProvider{}
	for rows.Next() {
		p := &UsenetProvider{}
		if rows.Scan(&p.ID, &p.UserID, &p.Label, &p.Host, &p.Port, &p.Username, &p.Password, &p.SSL, &p.MaxConns, &p.Priority, &p.Enabled) == nil {
			p.Password = s.dec(p.Password)
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (s *Store) ListUsenetProviders(ctx context.Context, userID string) ([]*UsenetProvider, error) {
	return s.queryUsenetProviders(ctx,
		`SELECT `+usenetProviderCols+` FROM usenet_provider_list WHERE user_id=$1 ORDER BY priority, created_at`, userID)
}

func (s *Store) EnabledUsenetProviders(ctx context.Context, userID string) ([]*UsenetProvider, error) {
	return s.queryUsenetProviders(ctx,
		`SELECT `+usenetProviderCols+` FROM usenet_provider_list WHERE user_id=$1 AND enabled=true ORDER BY priority, created_at`, userID)
}

func (s *Store) AddUsenetProvider(ctx context.Context, userID string, p *UsenetProvider) (string, error) {
	if p.Port == 0 {
		p.Port = 563
	}
	if p.MaxConns == 0 {
		p.MaxConns = 10
	}
	id := uuid.NewString()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO usenet_provider_list (id, user_id, label, host, port, username, password, ssl, max_conns, priority, enabled)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,true)`,
		id, userID, p.Label, p.Host, p.Port, p.Username, s.enc(p.Password), p.SSL, p.MaxConns, p.Priority)
	return id, err
}

func (s *Store) UpdateUsenetProvider(ctx context.Context, id, userID string, p *UsenetProvider) error {
	if p.Password == "" {
		_, err := s.pool.Exec(ctx,
			`UPDATE usenet_provider_list SET label=$1, host=$2, port=$3, username=$4, ssl=$5, max_conns=$6, priority=$7, updated_at=now()
			 WHERE id=$8 AND user_id=$9`,
			p.Label, p.Host, p.Port, p.Username, p.SSL, p.MaxConns, p.Priority, id, userID)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE usenet_provider_list SET label=$1, host=$2, port=$3, username=$4, password=$5, ssl=$6, max_conns=$7, priority=$8, updated_at=now()
		 WHERE id=$9 AND user_id=$10`,
		p.Label, p.Host, p.Port, p.Username, s.enc(p.Password), p.SSL, p.MaxConns, p.Priority, id, userID)
	return err
}

func (s *Store) SetUsenetProviderEnabled(ctx context.Context, id, userID string, enabled bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE usenet_provider_list SET enabled=$1, updated_at=now() WHERE id=$2 AND user_id=$3`, enabled, id, userID)
	return err
}

func (s *Store) DeleteUsenetProvider(ctx context.Context, id, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM usenet_provider_list WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}
