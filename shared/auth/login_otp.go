package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	loginOTPTTL         = 10 * time.Minute
	loginOTPMaxAttempts = 5
)

func (s *Store) NewLoginOTP(ctx context.Context, userID string) (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO login_otps (user_id, code_hash, attempts, expires_at)
		VALUES ($1,$2,0,$3)
		ON CONFLICT (user_id) DO UPDATE SET code_hash=EXCLUDED.code_hash, attempts=0, expires_at=EXCLUDED.expires_at`,
		userID, string(hash), time.Now().Add(loginOTPTTL))
	return code, err
}

func (s *Store) CheckLoginOTP(ctx context.Context, userID, code string) bool {
	var hash string
	var attempts int
	var exp time.Time
	if s.pool.QueryRow(ctx, `SELECT code_hash, attempts, expires_at FROM login_otps WHERE user_id=$1`, userID).Scan(&hash, &attempts, &exp) != nil {
		return false
	}
	if time.Now().After(exp) || attempts >= loginOTPMaxAttempts {
		s.pool.Exec(ctx, `DELETE FROM login_otps WHERE user_id=$1`, userID)
		return false
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(code)) != nil {
		s.pool.Exec(ctx, `UPDATE login_otps SET attempts=attempts+1 WHERE user_id=$1`, userID)
		return false
	}
	s.pool.Exec(ctx, `DELETE FROM login_otps WHERE user_id=$1`, userID)
	return true
}
