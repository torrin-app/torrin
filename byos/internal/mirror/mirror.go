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
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/video"
)

type Source interface {
	Get(ctx context.Context, key, rng string) (*storage.Object, error)
}

func OpenDecrypted(ctx context.Context, src Source, cipher *crypto.Stream, key string, enc bool) (io.ReadCloser, error) {
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

type uploadFn func(ctx context.Context, i int, f jobs.File, body io.Reader) error

func mirrorFiles(ctx context.Context, src Source, cipher *crypto.Stream, job *jobs.Job, stall time.Duration, up uploadFn) error {
	for i, f := range job.Files {
		body, err := OpenDecrypted(ctx, src, cipher, manifest.ResolveKey(job.InfoHash, i, f.Key, f.Name), f.Enc)
		if err != nil {
			return fmt.Errorf("read %s: %w", f.Name, err)
		}
		pr := newProgressReader(body)
		uctx, stop := guardStall(ctx, pr, stall)
		err = up(uctx, i, f, pr)
		stop()
		body.Close()
		if err != nil {
			return fmt.Errorf("upload %s: %w", f.Name, err)
		}
	}
	return nil
}

func Mirror(ctx context.Context, src Source, cipher *crypto.Stream, job *jobs.Job, creds *auth.StorageCreds, stall time.Duration) error {
	dst := storage.NewClient(creds.Endpoint, creds.Region, creds.AccessKey, creds.SecretKey, creds.Bucket, "", "")
	return mirrorFiles(ctx, src, cipher, job, stall, func(uctx context.Context, i int, f jobs.File, body io.Reader) error {
		return dst.StreamUpload(uctx, creds.Prefix+manifest.Key(job.InfoHash, i, f.Name), body, video.ContentType(f.Name))
	})
}

func MirrorRclone(ctx context.Context, rc *rclonerc.Client, srcBase, srcToken string, job *jobs.Job, creds *auth.StorageCreds, stall time.Duration) error {
	remote, err := auth.EnsureRemote(ctx, rc, job.UserID, creds)
	if err != nil {
		return fmt.Errorf("create remote: %w", err)
	}
	srcFs := ":http,url=" + srcBase
	for i, f := range job.Files {
		srcRemote := fmt.Sprintf("src/%s/%s/%d", srcToken, job.ID, i)
		dst := creds.Prefix + manifest.Key(job.InfoHash, i, f.Name)
		group := fmt.Sprintf("byos/%s/%d", job.ID, i)
		jobID, err := rc.CopyFileAsync(ctx, srcFs, srcRemote, remote+":", dst, group)
		if err != nil {
			return fmt.Errorf("copy %s: %w", f.Name, err)
		}
		if err := waitCopy(ctx, rc, jobID, group, stall); err != nil {
			return fmt.Errorf("copy %s: %w", f.Name, err)
		}
		if ok, err := rc.Exists(ctx, remote+":", dst); err != nil || !ok {
			return fmt.Errorf("verify %s: not present after copy", f.Name)
		}
	}
	return nil
}

func waitCopy(ctx context.Context, rc *rclonerc.Client, jobID int64, group string, stall time.Duration) error {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	var last int64
	progress := time.Now()
	for {
		select {
		case <-ctx.Done():
			rc.JobStop(context.Background(), jobID)
			return ctx.Err()
		case <-t.C:
		}
		st, err := rc.JobStatus(ctx, jobID)
		if err != nil {
			continue
		}
		if st.Finished {
			if st.Success {
				return nil
			}
			return fmt.Errorf("rclone copy: %s", st.Error)
		}
		if b := rc.GroupBytes(ctx, group); b > last {
			last, progress = b, time.Now()
		} else if time.Since(progress) > stall {
			rc.JobStop(context.Background(), jobID)
			return fmt.Errorf("stalled: no progress for %s", stall)
		}
	}
}
