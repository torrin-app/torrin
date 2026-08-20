package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lindell/go-burner-email-providers/burner"
)

func ValidateSignupEmail(ctx context.Context, email string) error {
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 {
		return errors.New("invalid email address")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	domain := email[at+1:]
	if mx, err := net.DefaultResolver.LookupMX(ctx, domain); err == nil && len(mx) > 0 {
		return nil
	}
	if ips, err := net.DefaultResolver.LookupHost(ctx, domain); err == nil && len(ips) > 0 {
		return nil
	}
	return errors.New("email domain can't receive mail, check for typos")
}

func NormalizeEmail(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 {
		return email
	}
	local, domain := email[:at], email[at+1:]
	if plus := strings.IndexByte(local, '+'); plus >= 0 {
		local = local[:plus]
	}
	if domain == "googlemail.com" {
		domain = "gmail.com"
	}
	if domain == "gmail.com" {
		local = strings.ReplaceAll(local, ".", "")
	}
	return local + "@" + domain
}

func (s *Store) CreateUser(ctx context.Context, email string, referralCode ...string) (*User, error) {
	email = NormalizeEmail(email)
	if s.IsEmailDeleted(ctx, email) {
		return nil, fmt.Errorf("account previously deleted")
	}
	if burner.IsBurnerEmail(email) {
		return nil, fmt.Errorf("disposable email addresses are not allowed")
	}

	now := time.Now()
	u := &User{
		ID:        newID(),
		Email:     email,
		APIKey:    generateAPIKey(),
		PlanID:    "free",
		ExpiresAt: now.Add(72 * time.Hour),
		CreatedAt: now,
		UpdatedAt: now,
	}

	var referredBy string
	if len(referralCode) > 0 && referralCode[0] != "" {
		var referrerID string
		if err := s.pool.QueryRow(ctx, `SELECT id FROM users WHERE referral_code=$1`, referralCode[0]).Scan(&referrerID); err == nil {
			referredBy = referrerID
		}
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, email, api_key, plan_id, expires_at, referral_code, referred_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		u.ID, u.Email, u.APIKey, u.PlanID, u.ExpiresAt, generateRefCode(), referredBy, u.CreatedAt, u.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if referredBy != "" {
		s.pool.Exec(ctx, `INSERT INTO referrals (id, referrer_id, referee_id) VALUES ($1,$2,$3)`,
			newID(), referredBy, u.ID)
	}
	return u, nil
}

func (s *Store) RegenerateAPIKey(ctx context.Context, userID string) (string, error) {
	key := generateAPIKey()
	if _, err := s.pool.Exec(ctx, `UPDATE users SET api_key=$1, updated_at=$2 WHERE id=$3`, key, time.Now(), userID); err != nil {
		return "", err
	}
	s.AuditLog(ctx, userID, "key_regenerated", "", "")
	return key, nil
}

func (s *Store) IsEmailDeleted(ctx context.Context, email string) bool {
	var exists int
	s.pool.QueryRow(ctx, `SELECT 1 FROM deleted_emails WHERE email=$1`, email).Scan(&exists)
	return exists == 1
}

func newID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func generateAPIKey() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "tr_" + base64.RawURLEncoding.EncodeToString(b)
}

func generateRefCode() string {
	b := make([]byte, 6)
	rand.Read(b)
	return "tr_ref_" + hex.EncodeToString(b)
}
