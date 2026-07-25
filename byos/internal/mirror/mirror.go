package mirror

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/rclonerc"
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/video"
)

type Source interface {
	Get(ctx context.Context, key, rng string) (*storage.Object, error)
}

func Mirror(ctx context.Context, src Source, job *jobs.Job, creds *auth.StorageCreds) error {
	dst := storage.NewClient(creds.Endpoint, creds.Region, creds.AccessKey, creds.SecretKey, creds.Bucket, "", "")
	for i, f := range job.Files {
		key := manifest.Key(job.InfoHash, i, f.Name)
		obj, err := src.Get(ctx, key, "")
		if err != nil {
			return fmt.Errorf("read %s: %w", key, err)
		}
		err = dst.StreamUpload(ctx, creds.Prefix+key, obj.Body, video.ContentType(f.Name))
		obj.Body.Close()
		if err != nil {
			return fmt.Errorf("upload %s: %w", key, err)
		}
	}
	return nil
}

func MirrorRclone(ctx context.Context, rc *rclonerc.Client, srcFs string, job *jobs.Job, creds *auth.StorageCreds) error {
	var params map[string]string
	if err := json.Unmarshal([]byte(creds.ConfigJSON), &params); err != nil || len(params) == 0 {
		return fmt.Errorf("bad rclone config")
	}
	remote, err := rc.EnsureUserRemote(ctx, job.UserID, creds.Backend, params, true, creds.CryptPass, creds.Bucket)
	if err != nil {
		return fmt.Errorf("create remote: %w", err)
	}
	for i, f := range job.Files {
		key := manifest.Key(job.InfoHash, i, f.Name)
		if err := rc.CopyFile(ctx, srcFs, key, remote+":", creds.Prefix+key); err != nil {
			return fmt.Errorf("copy %s: %w", key, err)
		}
	}
	return nil
}
