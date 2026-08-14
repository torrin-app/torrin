package hoster

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/ingest/internal/screen"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/providers"
	"github.com/torrin-app/torrin/shared/video"
)

type ADKeyFor func(context.Context, *jobs.Job) string

type Runner struct {
	adKeyFor ADKeyFor
	repo     jobs.Repository
	pub      *publish.Publisher
	bus      *bus.Bus
	ban      screen.BanFunc
	scratch  string
	conns    int
	http     *http.Client
}

func NewRunner(adKeyFor ADKeyFor, repo jobs.Repository, pub *publish.Publisher, b *bus.Bus, ban screen.BanFunc, scratch string, conns int) *Runner {
	if conns < 1 {
		conns = 1
	}
	return &Runner{adKeyFor: adKeyFor, repo: repo, pub: pub, bus: b, ban: ban, scratch: scratch, conns: conns, http: &http.Client{}}
}

func (r *Runner) Run(ctx context.Context, job *jobs.Job, done func()) {
	go func() {
		defer done()
		if err := r.process(ctx, job); err != nil {
			job.Status = jobs.StatusFailed
			job.Error = err.Error()
			r.repo.Update(ctx, job)
			r.bus.Publish(events.JobFailed, events.Failed{JobID: job.ID, Reason: err.Error()})
		}
	}()
}

func (r *Runner) process(ctx context.Context, job *jobs.Job) error {
	adKey := r.adKeyFor(ctx, job)
	if adKey == "" {
		return fmt.Errorf("no AllDebrid key configured")
	}
	name, link, size, err := providers.HosterUnlock(ctx, adKey, job.Magnet)
	if err != nil {
		return fmt.Errorf("unlock: %w", err)
	}
	if !video.IsVideo(name) {
		return fmt.Errorf("unsupported file type (video only)")
	}
	if screen.Blocked(ctx, r.ban, job, name) {
		return fmt.Errorf("content blocked by safety policy")
	}
	if job.MaxBytes > 0 && size > job.MaxBytes {
		return fmt.Errorf("file too large (%dMB, max %dMB)", size/1e6, job.MaxBytes/1e6)
	}

	if job.Name == "" || job.Name == job.Magnet {
		job.Name = name
	}
	job.FileSize = size
	r.repo.Update(ctx, job)

	dir := filepath.Join(r.scratch, job.InfoHash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, filepath.Base(name))
	if !providers.OnDisk(path, size) {
		if err := providers.FetchFile(ctx, r.http, link, path, jobs.ProgressReporter(ctx, r.repo, job.ID), r.conns); err != nil {
			return fmt.Errorf("download: %w", err)
		}
	}
	sz := size
	if info, e := os.Stat(path); e == nil {
		sz = info.Size()
	}

	if err := r.pub.Publish(ctx, job, []publish.File{{Name: filepath.Base(name), Path: path, Size: sz}}); err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	r.bus.Publish(events.JobComplete, events.Complete{JobID: job.ID, InfoHash: job.InfoHash, Node: job.Node})
	slog.Info("hoster complete", "job", job.ID, "name", name)
	return nil
}
