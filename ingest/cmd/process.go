package main

import (
	"context"
	"log/slog"

	"github.com/torrin-app/torrin/ingest/internal/debrid"
	"github.com/torrin-app/torrin/ingest/internal/hoster"
	"github.com/torrin-app/torrin/ingest/internal/jobrun"
	"github.com/torrin-app/torrin/ingest/internal/release"
	"github.com/torrin-app/torrin/ingest/internal/screen"
	"github.com/torrin-app/torrin/ingest/internal/torrent"
	"github.com/torrin-app/torrin/ingest/internal/usenet"
	"github.com/torrin-app/torrin/ingest/internal/ytdlp"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/failure"
	"github.com/torrin-app/torrin/shared/jobs"
)

func process(ctx context.Context, repo jobs.Repository, dr *debrid.Runner, tr, sr *torrent.Runner, ur *usenet.Runner, hr *hoster.Runner, rel *release.Runner, yr *ytdlp.Runner, uf *release.UsenetFallback, b *bus.Bus, ban screen.BanFunc, cancels *cancelRegistry, a events.Assigned) {
	job, err := repo.Get(ctx, a.JobID)
	if err != nil {
		slog.Error("load job", "id", a.JobID, "err", err)
		return
	}

	jobCtx, cancel := context.WithCancel(ctx)
	if !cancels.trackIfAbsent(job.ID, cancel) {
		cancel()
		return
	}
	done := func() { cancels.untrack(job.ID); cancel() }

	if job.Source == jobs.SourceCrossSeed {
		if sr == nil {
			done()
			jobrun.Fail(ctx, repo, b, job, failure.EngineDown)
			return
		}
		job.Status = jobs.StatusDownloading
		repo.Update(ctx, job)
		sr.CrossSeed(jobCtx, job, done)
		return
	}
	if job.Source == jobs.SourceUsenet {
		job.Status = jobs.StatusDownloading
		repo.Update(ctx, job)
		ur.Run(jobCtx, job, done)
		return
	}
	if job.Source == jobs.SourceYtdlp {
		if yr == nil {
			done()
			jobrun.Fail(ctx, repo, b, job, failure.EngineDown)
			return
		}
		job.Status = jobs.StatusDownloading
		repo.Update(ctx, job)
		yr.Run(jobCtx, job, done)
		return
	}
	if job.Source == jobs.SourceHoster {
		job.Status = jobs.StatusDownloading
		repo.Update(ctx, job)
		hr.Run(jobCtx, job, done)
		return
	}
	if rel.Handles(job.Source) {
		job.Status = jobs.StatusDownloading
		repo.Update(ctx, job)
		rel.Run(jobCtx, job, done)
		return
	}

	defer done()
	if tr != nil && !tr.Hold(job.InfoHash) {
		return
	}
	job.Status = jobs.StatusDownloading
	repo.Update(ctx, job)

	if screen.Blocked(ctx, ban, job) {
		if tr != nil {
			tr.Release(job.InfoHash)
		}
		jobrun.Fail(ctx, repo, b, job, failure.Blocked)
		return
	}

	handled, err := dr.Run(jobCtx, job)
	if tr != nil {
		tr.Release(job.InfoHash)
	}

	canQbit := tr != nil && job.Source == jobs.SourceTorrent && job.UserID != "prewarm"

	debridMiss := !handled && !(err != nil && debrid.IsTerminal(err))
	if uf != nil && debridMiss && !job.Seed && job.Source == jobs.SourceTorrent && job.UserID != "prewarm" {
		if ferr := uf.Try(jobCtx, job); ferr == nil {
			return
		} else {
			slog.Info("usenet layer unavailable, continuing", "job", job.ID, "err", ferr)
		}
	}

	switch {
	case err != nil && debrid.IsTerminal(err):
		jobrun.Fail(ctx, repo, b, job, err)
	case err == nil && handled:
		b.Publish(events.JobComplete, events.Complete{JobID: job.ID, InfoHash: job.InfoHash})
	case canQbit:
		if err != nil {
			slog.Info("debrid unavailable, falling through to qbit", "job", job.ID, "err", err)
		}
		if e := tr.Start(job); e != nil {
			jobrun.Fail(ctx, repo, b, job, failure.Wrap(failure.AddFailed, "add torrent: %v", e))
		}
	default:
		fail := error(failure.NoSources)
		if err != nil {
			fail = failure.Wrap(failure.NoSources, "no provider: %v", err)
		}
		jobrun.Fail(ctx, repo, b, job, fail)
	}
}
