package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/torrin-app/torrin/byos/internal/mirror"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/rclonerc"
	"github.com/torrin-app/torrin/shared/service"
	"github.com/torrin-app/torrin/shared/storage"
)

const sourceRemote = "garage"

type deps struct {
	repo  *jobs.Postgres
	users *auth.Store
	src   *storage.Client
	rc    *rclonerc.Client
	srcFs string
}

func main() {
	ctx := context.Background()
	dsn := mustEnv("DATABASE_URL")

	repo, err := jobs.NewPostgres(ctx, dsn)
	if err != nil {
		fatal("jobs db", err)
	}
	users, err := auth.NewPostgres(ctx, dsn)
	if err != nil {
		fatal("auth db", err)
	}

	endpoint, region := mustEnv("S3_ENDPOINT"), env.Get("S3_REGION", "garage")
	access, secret, bucket := mustEnv("S3_ACCESS_KEY"), mustEnv("S3_SECRET_KEY"), mustEnv("S3_BUCKET")
	d := &deps{
		repo:  repo,
		users: users,
		src:   storage.NewFSClient(env.Get("STORE_DIR", "/mnt/cache/store"), "", ""),
		srcFs: sourceRemote + ":" + bucket,
	}
	d.rc = connectRclone(ctx, endpoint, region, access, secret)

	b, err := bus.Connect(mustEnv("NATS_URL"))
	if err != nil {
		fatal("nats", err)
	}
	defer b.Close()

	if _, err := bus.Subscribe(b, events.JobComplete, func(c events.Complete) {
		d.enqueueHash(ctx, c.InfoHash)
	}); err != nil {
		fatal("subscribe", err)
	}
	go d.runQueue(ctx)

	slog.Info("byos worker started")
	service.RunHealth("byos", "8085")
}

func connectRclone(ctx context.Context, endpoint, region, access, secret string) *rclonerc.Client {
	u := os.Getenv("RCLONE_RC_URL")
	if u == "" {
		return nil
	}
	rc := rclonerc.New(u)
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := rc.WaitReady(wctx); err != nil {
		slog.Warn("byos: rclone rcd not reachable — rclone targets skipped", "err", err)
		return nil
	}
	if err := rc.CreateRemote(ctx, sourceRemote, "s3", map[string]string{
		"provider":          "Other",
		"access_key_id":     access,
		"secret_access_key": secret,
		"endpoint":          endpoint,
		"region":            region,
		"force_path_style":  "true",
	}, false); err != nil {
		slog.Warn("byos: could not register source remote in rcd", "err", err)
		return nil
	}
	slog.Info("byos: rclone rcd ready, source remote registered")
	return rc
}

func (d *deps) enqueueHash(ctx context.Context, infoHash string) {
	sibs, _ := d.repo.ListByInfoHash(ctx, infoHash)
	for _, job := range sibs {
		if job.Status != jobs.StatusComplete || job.UserID == "" || job.UserID == "system" {
			continue
		}
		if d.repo.HasBYOSObject(ctx, job.ID) {
			continue
		}
		if creds, err := d.users.GetStorageCreds(ctx, job.UserID); err == nil && creds != nil && creds.Enabled {
			d.repo.EnqueueBYOS(ctx, job.ID, job.UserID)
		}
	}
}

func (d *deps) runQueue(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		items, _ := d.repo.ListBYOSQueue(ctx)
		for _, it := range items {
			d.processItem(ctx, it)
		}
	}
}

func (d *deps) processItem(ctx context.Context, it jobs.BYOSQueueItem) {
	job, err := d.repo.Get(ctx, it.JobID)
	if err != nil || job == nil || job.Status != jobs.StatusComplete || d.repo.HasBYOSObject(ctx, job.ID) {
		d.repo.DeleteBYOSQueue(ctx, it.JobID)
		return
	}
	creds, err := d.users.GetStorageCreds(ctx, job.UserID)
	if err != nil || creds == nil || !creds.Enabled {
		d.repo.DeleteBYOSQueue(ctx, it.JobID)
		return
	}

	var mErr error
	if creds.IsRclone() {
		if d.rc == nil {
			return
		}
		mErr = mirror.MirrorRclone(ctx, d.rc, d.srcFs, job, creds)
	} else {
		mErr = mirror.Mirror(ctx, d.src, job, creds)
	}
	switch {
	case mErr == nil:
		d.repo.MarkBYOSObject(ctx, job.ID, job.UserID, job.InfoHash, bucketOf(creds), job.Name, job.Files)
		d.repo.DeleteBYOSQueue(ctx, it.JobID)
		slog.Info("byos: mirrored", "job", job.ID, "user", job.UserID)
	case rcPermanent(mErr):
		slog.Warn("byos: permanent mirror failure, dropping", "job", job.ID, "err", mErr)
		d.repo.DeleteBYOSQueue(ctx, it.JobID)
	default:
		if n := d.repo.IncrementBYOSAttempt(ctx, it.JobID); n >= 6 {
			slog.Warn("byos: mirror gave up after retries", "job", job.ID, "attempts", n, "err", mErr)
			d.repo.DeleteBYOSQueue(ctx, it.JobID)
		} else {
			slog.Warn("byos: mirror failed, will retry", "job", job.ID, "attempt", n, "err", mErr)
		}
	}
}

func rcPermanent(err error) bool {
	var e *rclonerc.Error
	return errors.As(err, &e) && e.Permanent()
}

func bucketOf(c *auth.StorageCreds) string {
	if c.IsRclone() {
		return c.Backend
	}
	return c.Bucket
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fatal("missing env "+k, nil)
	}
	return v
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
