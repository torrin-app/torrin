package auth

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const passwordCost = 12

var ErrBadCredentials = errors.New("invalid email or password")

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("timing-equalizer"), passwordCost)

func (s *Store) SetPassword(ctx context.Context, userID, plain string) error {
	if len(plain) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), passwordCost)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, userID, string(hash))
	return err
}

func (s *Store) HasPassword(ctx context.Context, userID string) bool {
	return s.passwordHash(ctx, userID) != ""
}

func (s *Store) CheckPassword(ctx context.Context, email, plain string) (*User, error) {
	u, err := s.GetByEmail(ctx, NormalizeEmail(email))
	if err != nil || u == nil {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(plain))
		return nil, ErrBadCredentials
	}
	hash := s.passwordHash(ctx, u.ID)
	if hash == "" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) != nil {
		return nil, ErrBadCredentials
	}
	return u, nil
}

func (s *Store) passwordHash(ctx context.Context, userID string) string {
	var h string
	s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, userID).Scan(&h)
	return strings.TrimSpace(h)
}
