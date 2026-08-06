package auth

import (
	"context"
	"time"
)

func (s *Store) CreateTelegramLinkCode(ctx context.Context, code, userID string, ttl time.Duration) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO telegram_link_codes (code, user_id, expires_at) VALUES ($1,$2,$3)
		 ON CONFLICT (code) DO UPDATE SET user_id=EXCLUDED.user_id, expires_at=EXCLUDED.expires_at`,
		code, userID, time.Now().Add(ttl))
	return err
}
func (s *Store) RedeemTelegramLinkCode(ctx context.Context, code string, tgUserID int64) (string, bool) {
	var userID string
	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM telegram_link_codes WHERE code=$1 AND expires_at > now()`, code).Scan(&userID)
	if err != nil || userID == "" {
		return "", false
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO telegram_links (tg_user_id, user_id) VALUES ($1,$2)
		 ON CONFLICT (tg_user_id) DO UPDATE SET user_id=EXCLUDED.user_id`, tgUserID, userID); err != nil {
		return "", false
	}
	s.pool.Exec(ctx, `DELETE FROM telegram_link_codes WHERE code=$1`, code)
	return userID, true
}

func (s *Store) TelegramUserID(ctx context.Context, tgUserID int64) (string, bool) {
	var userID string
	err := s.pool.QueryRow(ctx, `SELECT user_id FROM telegram_links WHERE tg_user_id=$1`, tgUserID).Scan(&userID)
	if err != nil || userID == "" {
		return "", false
	}
	return userID, true
}

func (s *Store) TelegramLinked(ctx context.Context, userID string) (int64, bool) {
	var tgUserID int64
	err := s.pool.QueryRow(ctx, `SELECT tg_user_id FROM telegram_links WHERE user_id=$1 LIMIT 1`, userID).Scan(&tgUserID)
	if err != nil {
		return 0, false
	}
	return tgUserID, true
}

func (s *Store) UnlinkTelegram(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM telegram_links WHERE user_id=$1`, userID)
	return err
}
