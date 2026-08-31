package release

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/torrin-app/torrin/ingest/internal/jobrun"
	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/ingest/internal/screen"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/failure"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/providers"
	"github.com/torrin-app/torrin/shared/usenet/postproc"
)

const partAttempts = 3

var ErrSourceUnavailable = errors.New("release source unavailable")

type Resolver interface {
	Resolve(ctx context.Context, postURL, imdbID, want string) ([][][]string, error)
}

type ADKeyFor func(context.Context, *jobs.Job) string

type Runner struct {
	adKeyFor  ADKeyFor
	resolvers map[jobs.Source]Resolver
	repo      jobs.Repository
	pub       *publish.Publisher
	bus       *bus.Bus
	ban       screen.BanFunc
	scratch   string
	conns     int
	http      *http.Client
	fallback  func(context.Context, *jobs.Job) error
}

func NewRunner(adKeyFor ADKeyFor, resolvers map[jobs.Source]Resolver, repo jobs.Repository, pub *publish.Publisher, b *bus.Bus, ban screen.BanFunc, scratch string, conns int, fallback func(context.Context, *jobs.Job) error) *Runner {
	if conns < 1 {
		conns = 1
	}
	return &Runner{adKeyFor: adKeyFor, resolvers: resolvers, repo: repo, pub: pub, bus: b, ban: ban, scratch: scratch, conns: conns, http: &http.Client{}, fallback: fallback}
}

func (r *Runner) Handles(src jobs.Source) bool { return r.resolvers[src] != nil }

func (r *Runner) Run(ctx context.Context, job *jobs.Job, done func()) {
	go func() {
		defer done()
		err := r.process(ctx, job)
		if err != nil && errors.Is(err, ErrSourceUnavailable) && r.fallback != nil {
			slog.Info("release source dead, trying usenet fallback", "job", job.ID, "err", err)
			if ferr := r.fallback(ctx, job); ferr == nil {
				return
			} else {
				slog.Warn("release usenet fallback failed", "job", job.ID, "err", ferr)
				err = fmt.Errorf("%w; usenet fallback: %v", err, ferr)
			}
		}
		if err != nil {
			slog.Warn("release failed", "job", job.ID, "err", err)
			jobrun.Fail(ctx, r.repo, r.bus, job, err)
		}
	}()
}

func (r *Runner) process(ctx context.Context, job *jobs.Job) error {
	resolver := r.resolvers[job.Source]
	if resolver == nil {
		return fmt.Errorf("no resolver for source %q", job.Source)
	}
	adKey := r.adKeyFor(ctx, job)
	if adKey == "" {
		return fmt.Errorf("no AllDebrid key configured")
	}
	archives, err := resolver.Resolve(ctx, job.Magnet, job.IMDBID, job.Name)
	if err != nil {
		return failure.Newf("resolve_failed", "%s", err)
	}
	if len(archives) == 0 {
		return failure.Wrap(failure.NoSources, "no usable links found")
	}

	if job.Name == "" {
		if n := releaseName(archives); n != "" {
			job.Name = n
			r.repo.Update(ctx, job)
		}
	}

	dir := filepath.Join(r.scratch, job.InfoHash)
	defer os.RemoveAll(dir)

	total, err := r.download(ctx, adKey, archives, dir, job)
	if err != nil {
		return err
	}

	job.FileSize = total
	job.Status = jobs.StatusPublishing
	r.repo.Update(ctx, job)

	files, err := postproc.Process(dir, postproc.PasswordCandidates("", job.Name), job.Name)
	if err != nil {
		return fmt.Errorf("postproc: %w", err)
	}
	if len(files) == 0 {
		return failure.NoVideo
	}
	if screen.Blocked(ctx, r.ban, job, files[0].Name) {
		return fmt.Errorf("content blocked by safety policy")
	}

	pubFiles := make([]publish.File, len(files))
	for i, f := range files {
		pubFiles[i] = publish.File{Name: f.Name, Path: f.Path, Size: f.Size}
	}
	if err := r.pub.Publish(ctx, job, pubFiles); err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	r.bus.Publish(events.JobComplete, events.Complete{JobID: job.ID, InfoHash: job.InfoHash, Node: job.Node})
	slog.Info("release complete", "job", job.ID, "source", job.Source, "files", len(pubFiles))
	return nil
}

func releaseName(archives [][][]string) string {
	if len(archives) == 0 || len(archives[0]) == 0 || len(archives[0][0]) == 0 {
		return ""
	}
	name := filepath.Base(strings.TrimSuffix(archives[0][0][0], ".html"))
	if strings.HasSuffix(strings.ToLower(name), ".rar") {
		if i := strings.Index(strings.ToLower(name), ".part"); i > 0 {
			name = name[:i]
		}
	}
	return name
}

func (r *Runner) download(ctx context.Context, adKey string, archives [][][]string, dir string, job *jobs.Job) (int64, error) {
	var lastErr error
	for ai, parts := range archives {
		os.RemoveAll(dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return 0, err
		}
		total, err := r.fetchArchive(ctx, adKey, parts, dir, job)
		if err == nil {
			return total, nil
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		var f *failure.Failure
		if errors.As(err, &f) && f.Code == "too_large" {
			return 0, err
		}
		lastErr = err
		if ai < len(archives)-1 {
			slog.Warn("release packaging failed, trying alternative", "job", job.ID, "packaging", ai+1, "of", len(archives), "err", err)
		}
	}
	return 0, lastErr
}

func (r *Runner) fetchArchive(ctx context.Context, adKey string, parts [][]string, dir string, job *jobs.Job) (int64, error) {
	var total int64
	for i, mirrors := range parts {
		size, err := r.fetchPart(ctx, adKey, mirrors, i, len(parts), dir, job)
		if err != nil {
			return 0, fmt.Errorf("part %d/%d: %w", i+1, len(parts), err)
		}
		total += size
		if job.MaxBytes > 0 && total > job.MaxBytes {
			return 0, failure.Newf("too_large", "this release is over your plan limit of %dMB", job.MaxBytes/1e6)
		}
		r.repo.SetProgress(ctx, job.ID, float64(i+1)/float64(len(parts))*100, 0)
	}
	return total, nil
}

func (r *Runner) fetchPart(ctx context.Context, adKey string, mirrors []string, i, n int, dir string, job *jobs.Job) (int64, error) {
	var lastErr error
	for mi, srcLink := range mirrors {
		size, err := r.fetchMirror(ctx, adKey, srcLink, i, n, dir, job)
		if err == nil {
			return size, nil
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		lastErr = err
		if mi < len(mirrors)-1 {
			slog.Warn("release mirror failed, trying next", "job", job.ID, "part", i+1, "mirror", mi+1, "of", len(mirrors), "err", err)
		}
	}
	slog.Warn("release part dead, all mirrors exhausted", "job", job.ID, "part", i+1, "of", n, "mirrors", len(mirrors), "err", lastErr)
	return 0, deadLinksErr()
}

func deadLinksErr() error {
	return fmt.Errorf("%w: %w", failure.DeadLinks, ErrSourceUnavailable)
}

func (r *Runner) fetchMirror(ctx context.Context, adKey, srcLink string, i, n int, dir string, job *jobs.Job) (int64, error) {
	var lastErr error
	for attempt := 0; attempt < partAttempts; attempt++ {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		name, dl, size, err := providers.HosterUnlock(ctx, adKey, srcLink)
		if err != nil {
			lastErr = err
			if providers.DeadLink(err) {
				return 0, err
			}
			providers.Backoff(ctx, attempt)
			continue
		}
		path := filepath.Join(dir, filepath.Base(name))
		if providers.OnDisk(path, size) {
			r.repo.SetProgress(ctx, job.ID, float64(i+1)/float64(n)*100, 0)
			return size, nil
		}
		slog.Info("release downloading", "job", job.ID, "part", i+1, "of", n, "name", name, "attempt", attempt+1)

		var lastBytes int64
		lastT := time.Now()
		report := func(written, partTotal int64) {
			var speed int64
			if dt := time.Since(lastT).Seconds(); dt > 0 {
				speed = int64(float64(written-lastBytes) / dt)
			}
			lastBytes, lastT = written, time.Now()
			frac := 0.0
			if partTotal > 0 {
				frac = float64(written) / float64(partTotal)
			}
			r.repo.SetProgress(ctx, job.ID, (float64(i)+frac)/float64(n)*100, speed)
		}
		err = providers.FetchFileStalled(ctx, r.http, dl, path, report, r.conns)

		if err == nil {
			return size, nil
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		lastErr = err
		slog.Warn("release part failed, re-unlocking", "job", job.ID, "part", i+1, "err", err)
		providers.Backoff(ctx, attempt)
	}
	return 0, lastErr
}
