package auth

import (
	"context"
	"errors"

	"github.com/torrin-app/torrin/shared/rclonerc"
	"github.com/torrin-app/torrin/shared/storage"
)

func PurgeRelease(ctx context.Context, rc *rclonerc.Client, userID string, c *StorageCreds, infoHash string) error {
	dir := c.Prefix + infoHash
	if c.IsRclone() {
		if rc == nil {
			return errors.New("storage backend unavailable")
		}
		remote, err := EnsureRemote(ctx, rc, userID, c)
		if err != nil {
			return err
		}
		return rc.Purge(ctx, remote+":", dir)
	}
	dst := storage.NewClient(c.Endpoint, c.Region, c.AccessKey, c.SecretKey, c.Bucket, "", "")
	return dst.DeletePrefix(ctx, dir+"/")
}

type EvictPolicy struct {
	UserID    string
	AfterDays int
	MaxBytes  int64
}

func (s *Store) ListStorageEvictPolicies(ctx context.Context) ([]EvictPolicy, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, evict_after_days, evict_max_bytes FROM storage_credentials
		WHERE enabled AND (evict_after_days > 0 OR evict_max_bytes > 0)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvictPolicy
	for rows.Next() {
		var p EvictPolicy
		if rows.Scan(&p.UserID, &p.AfterDays, &p.MaxBytes) == nil {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}
