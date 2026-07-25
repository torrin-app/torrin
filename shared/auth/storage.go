package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type StorageCreds struct {
	Provider   string `json:"provider"`
	Endpoint   string `json:"endpoint"`
	Region     string `json:"region"`
	Bucket     string `json:"bucket"`
	AccessKey  string `json:"access_key"`
	SecretKey  string `json:"secret_key"`
	Prefix     string `json:"prefix,omitempty"`
	Enabled    bool   `json:"enabled"`
	Backend    string `json:"backend,omitempty"`
	ConfigJSON string `json:"-"`
	Encrypted  bool   `json:"encrypted"`
	CryptPass  string `json:"-"`
}

func (c *StorageCreds) IsRclone() bool { return c.Backend != "" }

func (s *Store) GetStorageCreds(ctx context.Context, userID string) (*StorageCreds, error) {
	c := &StorageCreds{}
	err := s.pool.QueryRow(ctx, `
		SELECT provider, endpoint, region, bucket, access_key, secret_key, prefix, enabled, backend, config_json, encrypted, crypt_password
		FROM storage_credentials WHERE user_id=$1`, userID).
		Scan(&c.Provider, &c.Endpoint, &c.Region, &c.Bucket, &c.AccessKey, &c.SecretKey, &c.Prefix, &c.Enabled, &c.Backend, &c.ConfigJSON, &c.Encrypted, &c.CryptPass)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("no storage configured")
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) SaveStorageCreds(ctx context.Context, userID string, c *StorageCreds) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO storage_credentials (user_id, provider, endpoint, region, bucket, access_key, secret_key, prefix, enabled, backend, config_json, encrypted, crypt_password, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (user_id) DO UPDATE SET provider=EXCLUDED.provider, endpoint=EXCLUDED.endpoint,
			region=EXCLUDED.region, bucket=EXCLUDED.bucket, access_key=EXCLUDED.access_key,
			secret_key=EXCLUDED.secret_key, prefix=EXCLUDED.prefix, enabled=EXCLUDED.enabled,
			backend=EXCLUDED.backend, config_json=EXCLUDED.config_json, encrypted=EXCLUDED.encrypted,
			crypt_password=EXCLUDED.crypt_password, updated_at=EXCLUDED.updated_at`,
		userID, c.Provider, c.Endpoint, c.Region, c.Bucket, c.AccessKey, c.SecretKey, c.Prefix, c.Enabled, c.Backend, c.ConfigJSON, c.Encrypted, c.CryptPass, time.Now())
	return err
}

func (s *Store) DeleteStorageCreds(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM storage_credentials WHERE user_id=$1`, userID)
	return err
}
