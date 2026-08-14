package auth

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

const totpIssuer = "torrin"

func (s *Store) EnrollTOTP(ctx context.Context, userID, email string) (secret, uri string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: totpIssuer, AccountName: email})
	if err != nil {
		return "", "", err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET totp_secret=$2 WHERE id=$1`, userID, s.enc(key.Secret())); err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

func (s *Store) totpSecret(ctx context.Context, userID string) string {
	var enc string
	s.pool.QueryRow(ctx, `SELECT totp_secret FROM users WHERE id=$1`, userID).Scan(&enc)
	return s.dec(enc)
}

func (s *Store) ConfirmTOTP(ctx context.Context, userID, code string) ([]string, error) {
	secret := s.totpSecret(ctx, userID)
	if secret == "" || !totp.Validate(strings.TrimSpace(code), secret) {
		return nil, fmt.Errorf("invalid code")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET totp_enabled=TRUE WHERE id=$1`, userID); err != nil {
		return nil, err
	}
	return s.regenBackupCodes(ctx, userID)
}

func (s *Store) VerifyTOTP(ctx context.Context, userID, code string) bool {
	code = strings.TrimSpace(code)
	if secret := s.totpSecret(ctx, userID); secret != "" && totp.Validate(code, secret) {
		return true
	}
	return s.consumeBackupCode(ctx, userID, code)
}

func (s *Store) DisableTOTP(ctx context.Context, userID string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE users SET totp_secret='', totp_enabled=FALSE WHERE id=$1`, userID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM mfa_backup_codes WHERE user_id=$1`, userID)
	return err
}

func (s *Store) regenBackupCodes(ctx context.Context, userID string) ([]string, error) {
	s.pool.Exec(ctx, `DELETE FROM mfa_backup_codes WHERE user_id=$1`, userID)
	codes := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		c := backupCode()
		h, err := bcrypt.GenerateFromPassword([]byte(c), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		if _, err := s.pool.Exec(ctx, `INSERT INTO mfa_backup_codes (id, user_id, code_hash) VALUES ($1,$2,$3)`, newID(), userID, string(h)); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, nil
}

func (s *Store) consumeBackupCode(ctx context.Context, userID, code string) bool {
	rows, err := s.pool.Query(ctx, `SELECT id, code_hash FROM mfa_backup_codes WHERE user_id=$1 AND used_at IS NULL`, userID)
	if err != nil {
		return false
	}
	type bc struct{ id, hash string }
	var list []bc
	for rows.Next() {
		var b bc
		if rows.Scan(&b.id, &b.hash) == nil {
			list = append(list, b)
		}
	}
	rows.Close()
	for _, b := range list {
		if bcrypt.CompareHashAndPassword([]byte(b.hash), []byte(code)) == nil {
			s.pool.Exec(ctx, `UPDATE mfa_backup_codes SET used_at=now() WHERE id=$1`, b.id)
			return true
		}
	}
	return false
}

func backupCode() string {
	b := make([]byte, 6)
	rand.Read(b)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}
