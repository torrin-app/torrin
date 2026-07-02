package usenet

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/Tensai75/nntpPool"
	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/ingest/internal/screen"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/usenet/download"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
	"github.com/torrin-app/torrin/shared/usenet/postproc"
)

type Runner struct {
	repo     jobs.Repository
	store    *storage.Client
	users    *auth.Store
	pub      *publish.Publisher
	bus      *bus.Bus
	ban      screen.BanFunc
	scratch  string
	sysCreds download.Credentials
}

func NewRunner(repo jobs.Repository, store *storage.Client, users *auth.Store, pub *publish.Publisher, b *bus.Bus, ban screen.BanFunc, scratch string, sysCreds download.Credentials) *Runner {
	return &Runner{repo: repo, store: store, users: users, pub: pub, bus: b, ban: ban, scratch: scratch, sysCreds: sysCreds}
}

func (r *Runner) Run(ctx context.Context, job *jobs.Job, done func()) {
	go func() {
		defer done()
		if err := r.process(ctx, job); err != nil {
			r.fail(ctx, job, err.Error())
		}
	}()
}

func (r *Runner) process(ctx context.Context, job *jobs.Job) error {
	slog.Info("usenet job started", "job", job.ID)
	data, err := r.store.GetBytes(ctx, nzb.StorageKey(job.InfoHash))
	if err != nil {
		return fmt.Errorf("fetch nzb: %w", err)
	}
	parsed, err := nzb.ParseBytes(data)
	if err != nil {
		return fmt.Errorf("parse nzb: %w", err)
	}

	var names []string
	for _, f := range parsed.Files {
		names = append(names, f.Filename, f.Subject)
	}
	if screen.Blocked(ctx, r.ban, job, names...) {
		return fmt.Errorf("content blocked by safety policy")
	}
	if job.MaxBytes > 0 && parsed.TotalSize() > job.MaxBytes {
		return fmt.Errorf("size %dGB exceeds your plan limit of %dGB", parsed.TotalSize()/1e9, job.MaxBytes/1e9)
	}

	slog.Info("usenet nzb ok", "job", job.ID, "name", parsed.Name(), "files", len(parsed.Files), "size_gb", parsed.TotalSize()/1e9)

	creds, system := r.creds(ctx, job.UserID)
	if creds.Host == "" {
		return fmt.Errorf("no usenet provider configured")
	}
	slog.Info("usenet connecting", "job", job.ID, "host", creds.Host, "conns", creds.MaxConns, "shared", system)
	var pool nntpPool.ConnectionPool
	if system {
		pool, err = download.SharedPool(creds)
	} else {
		pool, err = download.NewPool(creds)
	}
	if err != nil {
		return fmt.Errorf("connect usenet: %w", err)
	}
	if !system {
		defer pool.Close()
	}

	if job.Name == "" {
		job.Name = parsed.Name()
	}
	job.FileSize = parsed.TotalSize()
	r.repo.Update(ctx, job)

	outDir := filepath.Join(r.scratch, job.InfoHash)
	defer os.RemoveAll(outDir)

	dlCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var lastAt atomic.Int64
	lastAt.Store(time.Now().UnixNano())
	rep := jobs.ProgressReporter(ctx, r.repo, job.ID)
	onProgress := func(doneB, totalB int64) {
		lastAt.Store(time.Now().UnixNano())
		rep(doneB, totalB)
	}
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-dlCtx.Done():
				return
			case <-t.C:
				if time.Since(time.Unix(0, lastAt.Load())) > 30*time.Minute {
					slog.Warn("usenet download stalled, cancelling", "job", job.ID)
					cancel()
					return
				}
			}
		}
	}()

	slog.Info("usenet downloading", "job", job.ID, "name", job.Name)
	if _, err := download.Download(dlCtx, pool, parsed, outDir, creds.MaxConns, onProgress); err != nil {
		if dlCtx.Err() != nil && ctx.Err() == nil {
			return fmt.Errorf("download stalled — no progress for 30 minutes")
		}
		return fmt.Errorf("download: %w", err)
	}

	job.Status = jobs.StatusProcessing
	r.repo.Update(ctx, job)

	pwds := postproc.PasswordCandidates(parsed.Meta["password"], job.Name, parsed.Name())
	files, err := postproc.Process(outDir, pwds, job.Name)
	if err != nil {
		return fmt.Errorf("postproc: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no video files found")
	}

	pubFiles := make([]publish.File, len(files))
	for i, f := range files {
		pubFiles[i] = publish.File{Name: f.Name, Path: f.Path, Size: f.Size}
	}
	if err := r.pub.Publish(ctx, job, pubFiles); err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	r.bus.Publish(events.JobComplete, events.Complete{JobID: job.ID, InfoHash: job.InfoHash})
	slog.Info("usenet complete", "job", job.ID, "files", len(pubFiles))
	return nil
}

func (r *Runner) creds(ctx context.Context, userID string) (download.Credentials, bool) {
	if c, err := r.users.GetUsenetCreds(ctx, userID); err == nil && c.Host != "" {
		return download.Credentials{Host: c.Host, Port: c.Port, Username: c.Username, Password: c.Password, SSL: c.SSL, MaxConns: c.MaxConns}, false
	}
	if u, err := r.users.GetByID(ctx, userID); err == nil && u != nil {
		if p, ok := plans.Get(u.PlanID); ok && p.SystemUsenet {
			return r.sysCreds, true
		}
	}
	return download.Credentials{}, false
}

func (r *Runner) fail(ctx context.Context, job *jobs.Job, reason string) {
	job.Status = jobs.StatusFailed
	job.Error = reason
	r.repo.Update(ctx, job)
	r.bus.Publish(events.JobFailed, events.Failed{JobID: job.ID, Reason: reason})
}
