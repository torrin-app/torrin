package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) GetRDKey(ctx context.Context, userID string) (string, error) {
	return s.getKey(ctx, "rd_credentials", userID)
}
func (s *Store) GetPMKey(ctx context.Context, userID string) (string, error) {
	return s.getKey(ctx, "pm_credentials", userID)
}
func (s *Store) GetTBKey(ctx context.Context, userID string) (string, error) {
	return s.getKey(ctx, "tb_credentials", userID)
}

func (s *Store) SetRDKey(ctx context.Context, userID, key string) error {
	return s.setKey(ctx, "rd_credentials", userID, key)
}
func (s *Store) SetPMKey(ctx context.Context, userID, key string) error {
	return s.setKey(ctx, "pm_credentials", userID, key)
}
func (s *Store) SetTBKey(ctx context.Context, userID, key string) error {
	return s.setKey(ctx, "tb_credentials", userID, key)
}

func (s *Store) DeleteRDKey(ctx context.Context, userID string) error {
	return s.delKey(ctx, "rd_credentials", userID)
}
func (s *Store) DeletePMKey(ctx context.Context, userID string) error {
	return s.delKey(ctx, "pm_credentials", userID)
}
func (s *Store) DeleteTBKey(ctx context.Context, userID string) error {
	return s.delKey(ctx, "tb_credentials", userID)
}

func (s *Store) getKey(ctx context.Context, table, userID string) (string, error) {
	var key string
	err := s.pool.QueryRow(ctx, `SELECT api_key FROM `+table+` WHERE user_id=$1`, userID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return s.dec(key), err
}

func (s *Store) setKey(ctx context.Context, table, userID, key string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO `+table+` (user_id, api_key, updated_at) VALUES ($1,$2,$3)
		 ON CONFLICT (user_id) DO UPDATE SET api_key=EXCLUDED.api_key, updated_at=EXCLUDED.updated_at`,
		userID, s.enc(key), time.Now())
	return err
}

func (s *Store) delKey(ctx context.Context, table, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE user_id=$1`, userID)
	return err
}
