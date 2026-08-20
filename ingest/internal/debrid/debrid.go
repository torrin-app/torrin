package debrid

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/ingest/internal/screen"
	"github.com/torrin-app/torrin/shared/failure"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/providers"
)

type terminalErr struct{ error }

func (e terminalErr) Unwrap() error { return e.error }

func terminal(format string, a ...any) error { return terminalErr{fmt.Errorf(format, a...)} }

func IsTerminal(err error) bool {
	var t terminalErr
	return errors.As(err, &t)
}

type Publisher interface {
	Publish(ctx context.Context, job *jobs.Job, files []publish.File) error
}

type jobStore interface {
	SetProgress(ctx context.Context, id string, pct float64, speed int64) error
	Update(ctx context.Context, job *jobs.Job) error
}

type ProvidersFor func(ctx context.Context, job *jobs.Job) []providers.Provider

type UsageRecorder func(ctx context.Context, userID, provider string, bytes int64) error

type Runner struct {
	providersFor ProvidersFor
	pub          Publisher
	repo         jobStore
	scratch      string
	ban          screen.BanFunc
	conns        int
	http         *http.Client
	usage        UsageRecorder
}

func NewRunner(providersFor ProvidersFor, pub Publisher, repo jobStore, scratch string, ban screen.BanFunc, conns int, usage UsageRecorder) *Runner {
	if conns < 1 {
		conns = 1
	}
	return &Runner{providersFor: providersFor, pub: pub, repo: repo, scratch: scratch, ban: ban, conns: conns, http: &http.Client{}, usage: usage}
}

func (r *Runner) Run(ctx context.Context, job *jobs.Job) (bool, error) {
	dir := filepath.Join(r.scratch, job.InfoHash)
	defer os.RemoveAll(dir)

	var cached bool
	var lastErr error
	for _, prov := range r.providersFor(ctx, job) {
		res, err := prov.Fetch(ctx, job.Magnet, job.InfoHash)
		if err != nil {
			slog.Info("debrid miss", "provider", prov.Name(), "reason", err)
			if lastErr == nil {
				lastErr = err
			}
			continue
		}
		if res == nil {
			slog.Info("debrid miss", "provider", prov.Name(), "reason", "not cached")
			continue
		}
		cached = true
		files, err := r.attempt(ctx, job, dir, prov, res)
		if err != nil {
			if IsTerminal(err) {
				return true, err
			}
			os.RemoveAll(dir)
			slog.Warn("debrid provider failed, trying next", "job", job.ID, "provider", prov.Name(), "err", err)
			lastErr = err
			continue
		}
		r.recordUsage(ctx, job, prov.Name(), files)
		slog.Info("debrid download complete, publishing", "job", job.ID, "files", len(files))
		return true, r.pub.Publish(ctx, job, files)
	}
	return cached, lastErr
}

func (r *Runner) attempt(ctx context.Context, job *jobs.Job, dir string, prov providers.Provider, res *providers.Result) ([]publish.File, error) {
	defer prov.Release(context.WithoutCancel(ctx), res.Handle)
	slog.Info("debrid cache hit", "job", job.ID, "provider", prov.Name(), "name", res.Name, "files", len(res.Files))
	if job.Name == "" && res.Name != "" {
		job.Name = res.Name
	}
	var total int64
	for _, f := range res.Files {
		total += f.Size
	}
	job.FileSize = total
	r.repo.Update(ctx, job)

	names := make([]string, len(res.Files))
	for i, f := range res.Files {
		names[i] = f.Name
	}
	if screen.Blocked(ctx, r.ban, job, names...) {
		return nil, terminalErr{failure.Blocked}
	}
	if over := overPlanLimit(res, job.MaxBytes); over > 0 {
		return nil, terminalErr{failure.Newf("too_large", "this is %dGB, over your plan limit of %dGB", over/1e9, job.MaxBytes/1e9)}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return r.download(ctx, dir, job.ID, res.Files)
}

func (r *Runner) recordUsage(ctx context.Context, job *jobs.Job, provName string, files []publish.File) {
	if r.usage == nil {
		return
	}
	var got int64
	for _, f := range files {
		got += f.Size
	}
	if err := r.usage(context.WithoutCancel(ctx), job.UserID, provName, got); err != nil {
		slog.Warn("debrid usage record failed", "job", job.ID, "err", err)
	}
}

const renewAttempts = 3

func downloadWithRenew(ctx context.Context, link providers.Link, dl func(url string) error, maxRenew int) error {
	url := link.URL
	for attempt := 0; ; attempt++ {
		err := dl(url)
		if err == nil {
			return nil
		}
		if link.Renew == nil || attempt >= maxRenew || ctx.Err() != nil {
			return err
		}
		slog.Warn("debrid download failed, re-unrestricting for a fresh link", "file", link.Name, "attempt", attempt+1, "err", err)
		fresh, rErr := link.Renew(ctx)
		if rErr != nil || fresh == "" || fresh == url {
			return err
		}
		url = fresh
	}
}

func (r *Runner) download(ctx context.Context, dir, jobID string, links []providers.Link) ([]publish.File, error) {
	var files []publish.File
	var grandTotal, done int64
	for _, l := range links {
		grandTotal += l.Size
	}
	rep := jobs.ProgressReporter(ctx, r.repo, jobID)
	report := func(written, _ int64) { rep(done+written, grandTotal) }
	for _, link := range links {
		slog.Info("debrid downloading", "job", jobID, "file", link.Name, "size_mb", link.Size/1e6)
		path := filepath.Join(dir, filepath.Base(link.Name))
		if !providers.OnDisk(path, link.Size) {
			dl := func(url string) error {
				if err := providers.FetchFileStalled(ctx, r.http, url, path, report, r.conns); err != nil {
					return err
				}
				info, statErr := os.Stat(path)
				if link.Size > 0 && (statErr != nil || info.Size() != link.Size) {
					got := int64(0)
					if statErr == nil {
						got = info.Size()
					}
					return fmt.Errorf("incomplete: got %d of %d bytes", got, link.Size)
				}
				return nil
			}
			if err := downloadWithRenew(ctx, link, dl, renewAttempts); err != nil {
				return nil, fmt.Errorf("download %s: %w", link.Name, err)
			}
		}
		size := link.Size
		if info, err := os.Stat(path); err == nil {
			size = info.Size()
		}
		done += size
		files = append(files, publish.File{Name: link.Name, Path: path, Size: size})
	}
	return files, nil
}

func overPlanLimit(res *providers.Result, maxBytes int64) int64 {
	if maxBytes <= 0 {
		return 0
	}
	var total int64
	for _, f := range res.Files {
		total += f.Size
	}
	if total > maxBytes {
		return total
	}
	return 0
}
