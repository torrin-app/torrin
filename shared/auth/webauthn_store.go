package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

type Passkey struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
}

type WebAuthnUser struct {
	user  *User
	creds []webauthn.Credential
}

func (w *WebAuthnUser) User() *User                                { return w.user }
func (w *WebAuthnUser) WebAuthnID() []byte                         { return []byte(w.user.ID) }
func (w *WebAuthnUser) WebAuthnName() string                       { return w.user.Email }
func (w *WebAuthnUser) WebAuthnDisplayName() string                { return w.user.Email }
func (w *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

func (s *Store) WebAuthnUserFor(ctx context.Context, u *User) (*WebAuthnUser, error) {
	creds, err := s.webAuthnCredentials(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return &WebAuthnUser{user: u, creds: creds}, nil
}

func (s *Store) WebAuthnUserByHandle(ctx context.Context, handle []byte) (*WebAuthnUser, error) {
	u, err := s.GetByID(ctx, string(handle))
	if err != nil {
		return nil, err
	}
	return s.WebAuthnUserFor(ctx, u)
}

func (s *Store) webAuthnCredentials(ctx context.Context, userID string) ([]webauthn.Credential, error) {
	rows, err := s.pool.Query(ctx, `SELECT data FROM webauthn_credentials WHERE user_id=$1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []webauthn.Credential
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil {
			continue
		}
		var c webauthn.Credential
		if json.Unmarshal(raw, &c) == nil {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

func (s *Store) AddWebAuthnCredential(ctx context.Context, userID, name string, c *webauthn.Credential) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO webauthn_credentials (id, user_id, credential_id, name, data) VALUES ($1,$2,$3,$4,$5)`,
		newID(), userID, base64.RawURLEncoding.EncodeToString(c.ID), name, raw)
	return err
}

func (s *Store) UpdateWebAuthnCredential(ctx context.Context, c *webauthn.Credential) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE webauthn_credentials SET data=$2, last_used_at=now() WHERE credential_id=$1`,
		base64.RawURLEncoding.EncodeToString(c.ID), raw)
	return err
}

func (s *Store) ListWebAuthnCredentials(ctx context.Context, userID string) ([]Passkey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, created_at, last_used_at FROM webauthn_credentials WHERE user_id=$1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Passkey{}
	for rows.Next() {
		var p Passkey
		var last *time.Time
		if rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &last) != nil {
			continue
		}
		if last != nil {
			p.LastUsedAt = *last
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

func (s *Store) DeleteWebAuthnCredential(ctx context.Context, userID, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM webauthn_credentials WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}
