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
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/providers"
)

type terminalErr struct{ error }

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
	res, prov, err := providers.FirstAvailable(ctx, r.providersFor(ctx, job), job.Magnet, job.InfoHash)
	if err != nil {
		return false, err
	}
	if res == nil {
		return false, nil
	}
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
	defer prov.Release(context.WithoutCancel(ctx), res.Handle)

	names := make([]string, len(res.Files))
	for i, f := range res.Files {
		names[i] = f.Name
	}
	if screen.Blocked(ctx, r.ban, job, names...) {
		return true, terminal("content blocked by safety policy")
	}

	if over := overPlanLimit(res, job.MaxBytes); over > 0 {
		return true, terminal("size %d exceeds plan limit %d", over, job.MaxBytes)
	}

	dir := filepath.Join(r.scratch, job.InfoHash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return true, err
	}
	defer os.RemoveAll(dir)

	files, err := r.download(ctx, dir, job.ID, res.Files)
	if err != nil {
		return true, err
	}

	if r.usage != nil {
		var got int64
		for _, f := range files {
			got += f.Size
		}
		if err := r.usage(context.WithoutCancel(ctx), job.UserID, prov.Name(), got); err != nil {
			slog.Warn("debrid usage record failed", "job", job.ID, "err", err)
		}
	}

	slog.Info("debrid download complete, publishing", "job", job.ID, "files", len(files))
	return true, r.pub.Publish(ctx, job, files)
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
				return providers.FetchFile(ctx, r.http, url, path, report, r.conns)
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
