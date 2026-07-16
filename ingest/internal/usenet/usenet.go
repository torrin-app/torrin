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
	"github.com/torrin-app/torrin/ingest/internal/jobrun"
	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/ingest/internal/screen"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
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
			jobrun.Fail(ctx, r.repo, r.bus, job, err.Error())
		}
	}()
}

func (r *Runner) process(ctx context.Context, job *jobs.Job) error {
	slog.Info("usenet job started", "job", job.ID)
	data, err := r.store.GetBytes(ctx, nzb.StorageKey(job.InfoHash))
	if err != nil {
		return fmt.Errorf("fetch nzb: %w", err)
	}
	return r.RunNZB(ctx, job, data)
}

func (r *Runner) RunNZB(ctx context.Context, job *jobs.Job, data []byte) error {
	parsed, err := nzb.ParseBytes(data)
	if err != nil {
		return fmt.Errorf("parse nzb: %w", err)
	}
	if job.MaxBytes > 0 && parsed.TotalSize() > job.MaxBytes {
		return fmt.Errorf("size %dGB exceeds your plan limit of %dGB", parsed.TotalSize()/1e9, job.MaxBytes/1e9)
	}
	slog.Info("usenet nzb ok", "job", job.ID, "name", parsed.Name(), "files", len(parsed.Files), "size_gb", parsed.TotalSize()/1e9)

	if job.Name == "" {
		job.Name = parsed.Name()
	}
	job.FileSize = parsed.TotalSize()
	r.repo.Update(ctx, job)

	outDir := filepath.Join(r.scratch, job.InfoHash)
	defer os.RemoveAll(outDir)

	files, err := r.fetchToFiles(ctx, job, parsed, outDir, jobs.ProgressReporter(ctx, r.repo, job.ID))
	if err != nil {
		return err
	}
	return jobrun.Complete(ctx, r.repo, r.bus, r.pub, job, files)
}

func (r *Runner) AssemblePack(ctx context.Context, job *jobs.Job, parts [][]byte) error {
	parsedAll := make([]*nzb.NZB, 0, len(parts))
	var total int64
	for _, p := range parts {
		pn, err := nzb.ParseBytes(p)
		if err != nil {
			return fmt.Errorf("parse episode nzb: %w", err)
		}
		parsedAll = append(parsedAll, pn)
		total += pn.TotalSize()
	}
	if job.MaxBytes > 0 && total > job.MaxBytes {
		return fmt.Errorf("season pack %dGB exceeds your plan limit of %dGB", total/1e9, job.MaxBytes/1e9)
	}
	job.FileSize = total
	r.repo.Update(ctx, job)

	baseDir := filepath.Join(r.scratch, job.InfoHash)
	defer os.RemoveAll(baseDir)

	var all []publish.File
	rep := jobs.ProgressReporter(ctx, r.repo, job.ID)
	var doneBytes int64
	for i, pn := range parsedAll {
		epDir := filepath.Join(baseDir, fmt.Sprintf("ep%02d", i+1))
		if err := os.MkdirAll(epDir, 0o755); err != nil {
			return err
		}
		slog.Info("usenet pack episode", "job", job.ID, "ep", i+1, "of", len(parsedAll), "name", pn.Name())
		base := doneBytes
		files, err := r.fetchToFiles(ctx, job, pn, epDir, func(done, _ int64) { rep(base+done, total) })
		if err != nil {
			return fmt.Errorf("episode %d/%d: %w", i+1, len(parsedAll), err)
		}
		doneBytes += pn.TotalSize()
		all = append(all, files...)
	}
	return jobrun.Complete(ctx, r.repo, r.bus, r.pub, job, all)
}

func (r *Runner) fetchToFiles(ctx context.Context, job *jobs.Job, parsed *nzb.NZB, dir string, onProgress func(int64, int64)) ([]publish.File, error) {
	var names []string
	for _, f := range parsed.Files {
		names = append(names, f.Filename, f.Subject)
	}
	if screen.Blocked(ctx, r.ban, job, names...) {
		return nil, fmt.Errorf("content blocked by safety policy")
	}

	creds, system := r.creds(ctx, job.UserID)
	if creds.Host == "" {
		return nil, fmt.Errorf("no usenet provider configured")
	}
	slog.Info("usenet connecting", "job", job.ID, "host", creds.Host, "conns", creds.MaxConns, "shared", system)
	var pool nntpPool.ConnectionPool
	var err error
	if system {
		pool, err = download.SharedPool(creds)
	} else {
		pool, err = download.NewPool(creds)
	}
	if err != nil {
		return nil, fmt.Errorf("connect usenet: %w", err)
	}
	if !system {
		defer pool.Close()
	}

	dlCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var lastAt atomic.Int64
	lastAt.Store(time.Now().UnixNano())
	prog := func(doneB, totalB int64) {
		lastAt.Store(time.Now().UnixNano())
		if onProgress != nil {
			onProgress(doneB, totalB)
		}
	}
	go stallWatch(dlCtx, cancel, &lastAt, job.ID)

	slog.Info("usenet downloading", "job", job.ID, "name", parsed.Name())
	if _, err := download.Download(dlCtx, pool, parsed, dir, creds.MaxConns, prog); err != nil {
		if dlCtx.Err() != nil && ctx.Err() == nil {
			return nil, fmt.Errorf("download stalled — no progress for 30 minutes")
		}
		return nil, fmt.Errorf("download: %w", err)
	}

	job.Status = jobs.StatusProcessing
	r.repo.Update(ctx, job)

	pwds := postproc.PasswordCandidates(parsed.Meta["password"], job.Name, parsed.Name())
	files, err := postproc.Process(dir, pwds, job.Name)
	if err != nil {
		return nil, fmt.Errorf("postproc: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no video files found")
	}
	pubFiles := make([]publish.File, len(files))
	for i, f := range files {
		pubFiles[i] = publish.File{Name: f.Name, Path: f.Path, Size: f.Size}
	}
	return pubFiles, nil
}

func stallWatch(ctx context.Context, cancel context.CancelFunc, lastAt *atomic.Int64, jobID string) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if time.Since(time.Unix(0, lastAt.Load())) > 30*time.Minute {
				slog.Warn("usenet download stalled, cancelling", "job", jobID)
				cancel()
				return
			}
		}
	}
}

func (r *Runner) HasProvider(ctx context.Context, userID string) bool {
	c, _ := r.creds(ctx, userID)
	return c.Host != ""
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
