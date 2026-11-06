package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type UsenetCreds struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	SSL      bool   `json:"ssl"`
	MaxConns int    `json:"max_conns"`
}

func (s *Store) GetUsenetCreds(ctx context.Context, userID string) (*UsenetCreds, error) {
	var c UsenetCreds
	err := s.pool.QueryRow(ctx,
		`SELECT host, port, username, password, ssl, max_conns FROM usenet_credentials WHERE user_id=$1`, userID).
		Scan(&c.Host, &c.Port, &c.Username, &c.Password, &c.SSL, &c.MaxConns)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("no usenet credentials configured")
	}
	if err != nil {
		return nil, err
	}
	c.Password = s.dec(c.Password)
	return &c, nil
}

func (s *Store) SaveUsenetCreds(ctx context.Context, userID string, c *UsenetCreds) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO usenet_credentials (user_id, host, port, username, password, ssl, max_conns, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (user_id) DO UPDATE SET host=EXCLUDED.host, port=EXCLUDED.port,
			username=EXCLUDED.username, password=EXCLUDED.password, ssl=EXCLUDED.ssl,
			max_conns=EXCLUDED.max_conns, updated_at=EXCLUDED.updated_at`,
		userID, c.Host, c.Port, c.Username, s.enc(c.Password), c.SSL, c.MaxConns, time.Now())
	return err
}

func (s *Store) DeleteUsenetCreds(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM usenet_credentials WHERE user_id=$1`, userID)
	return err
}

func (s *Store) SetUsenetStreaming(ctx context.Context, userID string, on bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET usenet_streaming=$2 WHERE id=$1`, userID, on)
	return err
}

type UsenetIndexer struct {
	URL    string `json:"url"`
	APIKey string `json:"api_key,omitempty"`
}

func (s *Store) GetUsenetIndexer(ctx context.Context, userID string) (*UsenetIndexer, error) {
	var idx UsenetIndexer
	err := s.pool.QueryRow(ctx, `SELECT url, api_key FROM usenet_indexers WHERE user_id=$1`, userID).
		Scan(&idx.URL, &idx.APIKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("no usenet indexer configured")
	}
	if err != nil {
		return nil, err
	}
	idx.APIKey = s.dec(idx.APIKey)
	return &idx, nil
}

func (s *Store) SaveUsenetIndexer(ctx context.Context, userID string, idx *UsenetIndexer) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO usenet_indexers (user_id, url, api_key, updated_at) VALUES ($1,$2,$3,$4)
		ON CONFLICT (user_id) DO UPDATE SET url=EXCLUDED.url, api_key=EXCLUDED.api_key, updated_at=EXCLUDED.updated_at`,
		userID, idx.URL, s.enc(idx.APIKey), time.Now())
	return err
}

func (s *Store) DeleteUsenetIndexer(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM usenet_indexers WHERE user_id=$1`, userID)
	return err
}
