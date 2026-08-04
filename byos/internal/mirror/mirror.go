package mirror

import (
	"context"
	"fmt"
	"io"
	"path"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/rclonerc"
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/video"
)

type Source interface {
	Get(ctx context.Context, key, rng string) (*storage.Object, error)
}

func openDecrypted(ctx context.Context, src Source, cipher *crypto.Stream, key string, enc bool) (io.ReadCloser, error) {
	obj, err := src.Get(ctx, key, "")
	if err != nil {
		return nil, err
	}
	if !enc || cipher == nil {
		return obj.Body, nil
	}
	pr, pw := io.Pipe()
	go func() {
		err := cipher.DecryptAll(pw, obj.Body)
		obj.Body.Close()
		pw.CloseWithError(err)
	}()
	return pr, nil
}

func Mirror(ctx context.Context, src Source, cipher *crypto.Stream, job *jobs.Job, creds *auth.StorageCreds) error {
	dst := storage.NewClient(creds.Endpoint, creds.Region, creds.AccessKey, creds.SecretKey, creds.Bucket, "", "")
	for i, f := range job.Files {
		body, err := openDecrypted(ctx, src, cipher, manifest.ResolveKey(job.InfoHash, i, f.Key, f.Name), f.Enc)
		if err != nil {
			return fmt.Errorf("read %s: %w", f.Name, err)
		}
		err = dst.StreamUpload(ctx, creds.Prefix+manifest.Key(job.InfoHash, i, f.Name), body, video.ContentType(f.Name))
		body.Close()
		if err != nil {
			return fmt.Errorf("upload %s: %w", f.Name, err)
		}
	}
	return nil
}

func MirrorRclone(ctx context.Context, rc *rclonerc.Client, src Source, cipher *crypto.Stream, job *jobs.Job, creds *auth.StorageCreds) error {
	remote, err := auth.EnsureRemote(ctx, rc, job.UserID, creds)
	if err != nil {
		return fmt.Errorf("create remote: %w", err)
	}
	for i, f := range job.Files {
		body, err := openDecrypted(ctx, src, cipher, manifest.ResolveKey(job.InfoHash, i, f.Key, f.Name), f.Enc)
		if err != nil {
			return fmt.Errorf("read %s: %w", f.Name, err)
		}
		dst := creds.Prefix + manifest.Key(job.InfoHash, i, f.Name)
		err = rc.UploadFile(ctx, remote+":", path.Dir(dst), path.Base(dst), body)
		body.Close()
		if err != nil {
			return fmt.Errorf("upload %s: %w", f.Name, err)
		}
	}
	return nil
}
