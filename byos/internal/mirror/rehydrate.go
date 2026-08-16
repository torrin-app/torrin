package mirror

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/rclonerc"
	"github.com/torrin-app/torrin/shared/video"
)

type Dest interface {
	StreamUpload(ctx context.Context, key string, body io.Reader, contentType string) error
}

func Rehydrate(ctx context.Context, rc *rclonerc.Client, dst Dest, cipher *crypto.Stream, obj *jobs.BYOSObject, creds *auth.StorageCreds, stall time.Duration) error {
	remote, err := auth.EnsureRemote(ctx, rc, obj.UserID, creds)
	if err != nil {
		return fmt.Errorf("create remote: %w", err)
	}
	for i, f := range obj.Files {
		body, err := rc.ReadFile(ctx, remote, creds.Prefix+manifest.Key(obj.InfoHash, i, f.Name))
		if err != nil {
			return fmt.Errorf("read %s: %w", f.Name, err)
		}
		pr := newProgressReader(body)
		uctx, stop := guardStall(ctx, pr, stall)
		var src io.Reader = pr
		if f.Enc && cipher != nil {
			src, err = cipher.EncryptReader(pr)
		}
		if err == nil {
			err = dst.StreamUpload(uctx, f.Key, src, video.ContentType(f.Name))
		}
		stop()
		body.Close()
		if err != nil {
			return fmt.Errorf("store %s: %w", f.Name, err)
		}
	}
	return nil
}
