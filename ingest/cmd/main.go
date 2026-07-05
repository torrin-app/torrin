package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/torrin-app/torrin/ingest/internal/debrid"
	"github.com/torrin-app/torrin/ingest/internal/hoster"
	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/ingest/internal/release"
	"github.com/torrin-app/torrin/ingest/internal/screen"
	"github.com/torrin-app/torrin/ingest/internal/torrent"
	"github.com/torrin-app/torrin/ingest/internal/usenet"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/events"
	hdclient "github.com/torrin-app/torrin/shared/hdencode"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/providers"
	"github.com/torrin-app/torrin/shared/qbit"
	"github.com/torrin-app/torrin/shared/safety"
	"github.com/torrin-app/torrin/shared/scenerls"
	"github.com/torrin-app/torrin/shared/service"
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/usenet/download"
)

func main() {
	ctx := context.Background()
	dsn := mustEnv("DATABASE_URL")

	repo, err := jobs.NewPostgres(ctx, dsn)
	if err != nil {
		fatal("postgres", err)
	}
	if err := providers.SetRateLimitDB(ctx, repo.Pool()); err != nil {
		slog.Warn("rate-limit db init failed, using per-process limiter", "err", err)
	}
	users, err := auth.NewPostgres(ctx, dsn)
	if err != nil {
		fatal("auth db", err)
	}
	if err := users.SetCredsKey(env.Get("CREDS_KEY", "")); err != nil {
		fatal("creds key", err)
	}
	safety.Refresh(ctx, users.GetBlocklist, time.Hour)
	ban := screen.BanFunc(func(ctx context.Context, userID, reason string) {
		users.BanUser(ctx, userID, reason)
		slog.Warn("user banned for blocked content", "user", userID, "reason", reason)
	})

	store := storage.NewClient(
		mustEnv("S3_ENDPOINT"), env.Get("S3_REGION", ""),
		mustEnv("S3_ACCESS_KEY"), mustEnv("S3_SECRET_KEY"),
		mustEnv("S3_BUCKET"), env.Get("PUBLIC_URL", ""), mustEnv("SIGNING_KEY"),
	)

	b, err := bus.Connect(mustEnv("NATS_URL"))
	if err != nil {
		fatal("nats", err)
	}
	defer b.Close()

	pub := publish.New(repo, store, env.Get("NODE_ID", ""))
	sysRD, sysAD := os.Getenv("RD_API_KEY"), os.Getenv("AD_API_KEY")
	providersFor := func(ctx context.Context, job *jobs.Job) []providers.Provider {
		var list []providers.Provider
		userRD, _ := users.GetRDKey(ctx, job.UserID)
		if userRD != "" {
			list = append(list, providers.NewRealDebrid(userRD))
		}
		if k, _ := users.GetTBKey(ctx, job.UserID); k != "" {
			list = append(list, providers.NewTorBox(k))
		}
		if k, _ := users.GetPMKey(ctx, job.UserID); k != "" {
			list = append(list, providers.NewPremiumize(k))
		}
		if sysRD != "" && sysRD != userRD {
			list = append(list, providers.NewRealDebrid(sysRD))
		}
		if sysAD != "" {
			list = append(list, providers.NewAllDebrid(sysAD))
		}
		return list
	}
	dlConns := atoiOr(os.Getenv("DOWNLOAD_CONNS"), 6)
	debridRunner := debrid.NewRunner(providersFor, pub, repo, env.Get("SCRATCH_DIR", "/scratch"), ban, dlConns)

	var torrentRunner *torrent.Runner
	if qbURL := os.Getenv("QBIT_URL"); qbURL != "" {
		qb := qbit.NewClient(qbURL, env.Get("QBIT_USER", "admin"), env.Get("QBIT_PASS", ""))
		torrentRunner = torrent.NewRunner(qb, repo, pub, b, ban)
		go torrentRunner.Run(ctx)
		go torrent.StartTrackerRefresh(ctx, qb)
		slog.Info("torrent engine enabled")
	}

	hosterRunner := hoster.NewRunner(func(ctx context.Context, j *jobs.Job) string {
		return sysAD
	}, repo, pub, b, ban, env.Get("SCRATCH_DIR", "/scratch"), dlConns)

	usenetRunner := usenet.NewRunner(repo, store, users, pub, b, ban, env.Get("SCRATCH_DIR", "/scratch"), download.Credentials{
		Host:     os.Getenv("USENET_HOST"),
		Port:     atoiOr(os.Getenv("USENET_PORT"), 563),
		Username: os.Getenv("USENET_USER"),
		Password: os.Getenv("USENET_PASS"),
		SSL:      os.Getenv("USENET_SSL") != "false",
		MaxConns: atoiOr(os.Getenv("USENET_MAXCONNS"), 20),
	})

	usenetFallback := release.NewUsenetFallback(users, usenetRunner, env.Get("USENET_INDEXER_URL", ""), env.Get("USENET_INDEXER_KEY", ""))

	releaseRunner := release.NewRunner(func(ctx context.Context, j *jobs.Job) string {
		return sysAD
	}, map[jobs.Source]release.Resolver{
		jobs.SourceHDEncode: hdclient.NewClient(),
		jobs.SourceScenerls: scenerls.NewClient(),
	}, repo, pub, b, ban, env.Get("SCRATCH_DIR", "/scratch"), dlConns, usenetFallback.Try)

	cancels := newCancelRegistry()
	if _, err := bus.Subscribe(b, events.JobAssigned, func(a events.Assigned) {
		go process(ctx, repo, debridRunner, torrentRunner, usenetRunner, hosterRunner, releaseRunner, usenetFallback, b, ban, cancels, a)
	}); err != nil {
		fatal("subscribe", err)
	}
	if _, err := bus.Subscribe(b, events.JobDeleted, func(d events.Deleted) {
		cancels.cancel(d.JobID)
	}); err != nil {
		fatal("subscribe deleted", err)
	}

	go func() {
		t := time.NewTimer(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				reconcile(ctx, repo, b, cancels)
				if torrentRunner != nil {
					torrentRunner.ReapOrphans(ctx)
				}
				t.Reset(5 * time.Minute)
			}
		}
	}()

	go sweepScratch(ctx, repo, env.Get("SCRATCH_DIR", "/scratch"))

	slog.Info("ingest worker started")
	service.RunHealth("ingest", "8083")
}

type cancelRegistry struct {
	mu sync.Mutex
	m  map[string]context.CancelFunc
}

func newCancelRegistry() *cancelRegistry { return &cancelRegistry{m: map[string]context.CancelFunc{}} }

func (c *cancelRegistry) trackIfAbsent(id string, cancel context.CancelFunc) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[id]; ok {
		return false
	}
	c.m[id] = cancel
	return true
}

func (c *cancelRegistry) untrack(id string) {
	c.mu.Lock()
	delete(c.m, id)
	c.mu.Unlock()
}

func (c *cancelRegistry) has(id string) bool {
	c.mu.Lock()
	_, ok := c.m[id]
	c.mu.Unlock()
	return ok
}

func (c *cancelRegistry) cancel(id string) {
	c.mu.Lock()
	cancel := c.m[id]
	delete(c.m, id)
	c.mu.Unlock()
	if cancel != nil {
		slog.Info("cancelling deleted job", "job", id)
		cancel()
	}
}

func process(ctx context.Context, repo jobs.Repository, dr *debrid.Runner, tr *torrent.Runner, ur *usenet.Runner, hr *hoster.Runner, rel *release.Runner, uf *release.UsenetFallback, b *bus.Bus, ban screen.BanFunc, cancels *cancelRegistry, a events.Assigned) {
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

	if job.Source == jobs.SourceUsenet {
		job.Status = jobs.StatusDownloading
		repo.Update(ctx, job)
		ur.Run(jobCtx, job, done)
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
		fail(ctx, repo, b, job, "content blocked by safety policy")
		return
	}

	handled, err := dr.Run(jobCtx, job)
	if tr != nil {
		tr.Release(job.InfoHash)
	}

	canQbit := tr != nil && job.Source == jobs.SourceTorrent && job.UserID != "prewarm"

	debridMiss := !handled && !(err != nil && debrid.IsTerminal(err))
	if uf != nil && debridMiss && job.Source == jobs.SourceTorrent && job.UserID != "prewarm" {
		if ferr := uf.Try(jobCtx, job); ferr == nil {
			return
		} else {
			slog.Info("usenet layer unavailable, continuing", "job", job.ID, "err", ferr)
		}
	}

	switch {
	case err != nil && debrid.IsTerminal(err):
		fail(ctx, repo, b, job, err.Error())
	case err == nil && handled:
		b.Publish(events.JobComplete, events.Complete{JobID: job.ID, InfoHash: job.InfoHash})
	case canQbit:
		if err != nil {
			slog.Info("debrid unavailable, falling through to qbit", "job", job.ID, "err", err)
		}
		if e := tr.Start(job); e != nil {
			fail(ctx, repo, b, job, "failed to add torrent: "+e.Error())
		}
	default:
		msg := "not cached on any debrid provider"
		if err != nil {
			msg = err.Error()
		}
		fail(ctx, repo, b, job, msg)
	}
}

func fail(ctx context.Context, repo jobs.Repository, b *bus.Bus, job *jobs.Job, reason string) {
	clean := jobs.UserError(reason)
	job.Status = jobs.StatusFailed
	job.Error = clean
	repo.Update(ctx, job)
	b.Publish(events.JobFailed, events.Failed{JobID: job.ID, Reason: clean})
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fatal("missing env "+k, nil)
	}
	return v
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
