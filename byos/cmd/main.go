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
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/rclonerc"
	"github.com/torrin-app/torrin/shared/service"
	"github.com/torrin-app/torrin/shared/storage"
)

type deps struct {
	repo   *jobs.Postgres
	users  *auth.Store
	src    *storage.Client
	rc     *rclonerc.Client
	cipher *crypto.Stream
	nodeID string
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

	var cipher *crypto.Stream
	if k := env.Get("STORAGE_KEY", ""); k != "" {
		c, err := crypto.NewStream(k)
		if err != nil {
			fatal("storage key", err)
		}
		cipher = c
	}
	d := &deps{
		repo:   repo,
		users:  users,
		src:    storage.NewFSClient(env.Get("STORE_DIR", "/mnt/cache/store"), "", ""),
		cipher: cipher,
		nodeID: env.Get("NODE_ID", ""),
	}
	d.rc = connectRclone(ctx)

	b, err := bus.Connect(mustEnv("NATS_URL"))
	if err != nil {
		fatal("nats", err)
	}
	defer b.Close()

	if _, err := bus.Subscribe(b, events.JobComplete, func(c events.Complete) {
		if c.Node != d.nodeID {
			return
		}
		d.enqueueHash(ctx, c.InfoHash)
	}); err != nil {
		fatal("subscribe", err)
	}
	go d.runQueue(ctx)
	go d.reconcile(ctx)
	go d.runStorageEviction(ctx, int(env.Int("BYOS_EVICT_HOUR", 5)))

	slog.Info("byos worker started")
	service.RunHealth("byos", "8085")
}

func connectRclone(ctx context.Context) *rclonerc.Client {
	u := os.Getenv("RCLONE_RC_URL")
	if u == "" {
		return nil
	}
	rc := rclonerc.New(u)
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := rc.WaitReady(wctx); err != nil {
		slog.Warn("byos: rclone rcd not reachable, rclone targets skipped", "err", err)
		return nil
	}
	slog.Info("byos: rclone rcd ready")
	return rc
}

func (d *deps) enqueueHash(ctx context.Context, infoHash string) {
	sibs, _ := d.repo.ListByInfoHash(ctx, infoHash)
	for _, job := range sibs {
		d.enqueueJob(ctx, job)
	}
}

func (d *deps) enqueueJob(ctx context.Context, job *jobs.Job) {
	if job.Node != d.nodeID || job.Status != jobs.StatusComplete || job.UserID == "" || job.UserID == "system" {
		return
	}
	if d.repo.HasBYOSObject(ctx, job.ID) {
		return
	}
	if creds, err := d.users.GetStorageCreds(ctx, job.UserID); err == nil && creds != nil && creds.Enabled {
		d.repo.EnqueueBYOS(ctx, job.ID, job.UserID)
	}
}

func (d *deps) reconcile(ctx context.Context) {
	all, err := d.repo.ListByStatus(ctx, jobs.StatusComplete)
	if err != nil {
		return
	}
	for _, job := range all {
		d.enqueueJob(ctx, job)
	}
	slog.Info("byos: reconcile done", "node", d.nodeID, "scanned", len(all))
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

type queueAction int

const (
	actionMirror queueAction = iota
	actionSkip
	actionDelete
)

func queueActionFor(job *jobs.Job, nodeID string, mirrored bool) queueAction {
	if job == nil || job.Status != jobs.StatusComplete || mirrored {
		return actionDelete
	}
	if job.Node != nodeID {
		return actionSkip
	}
	return actionMirror
}

func (d *deps) processItem(ctx context.Context, it jobs.BYOSQueueItem) {
	job, _ := d.repo.Get(ctx, it.JobID)
	mirrored := job != nil && d.repo.HasBYOSObject(ctx, job.ID)
	switch queueActionFor(job, d.nodeID, mirrored) {
	case actionDelete:
		d.repo.DeleteBYOSQueue(ctx, it.JobID)
		return
	case actionSkip:
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
		mErr = mirror.MirrorRclone(ctx, d.rc, d.src, d.cipher, job, creds)
	} else {
		mErr = mirror.Mirror(ctx, d.src, d.cipher, job, creds)
	}
	switch {
	case mErr == nil:
		d.repo.MarkBYOSObject(ctx, job.ID, job.UserID, job.InfoHash, bucketOf(creds), job.Name, job.Files)
		d.repo.DeleteBYOSQueue(ctx, it.JobID)
		slog.Info("byos: mirrored", "job", job.ID, "user", job.UserID)
	case rcAuth(mErr):
		slog.Warn("byos: storage auth rejected, disabling until reconnect", "job", job.ID, "user", job.UserID, "err", mErr)
		d.users.DisableStorage(ctx, job.UserID, "your storage rejected the connection (401/403); reconnect it in settings")
		d.repo.DeleteBYOSQueue(ctx, it.JobID)
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

func rcAuth(err error) bool {
	var e *rclonerc.Error
	return errors.As(err, &e) && e.Auth()
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
