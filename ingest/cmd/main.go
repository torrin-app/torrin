package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/torrin-app/torrin/ingest/internal/debrid"
	"github.com/torrin-app/torrin/ingest/internal/hoster"
	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/ingest/internal/release"
	"github.com/torrin-app/torrin/ingest/internal/screen"
	"github.com/torrin-app/torrin/ingest/internal/torrent"
	"github.com/torrin-app/torrin/ingest/internal/usenet"
	"github.com/torrin-app/torrin/ingest/internal/ytdlp"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/cinemeta"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/eviction"
	hdclient "github.com/torrin-app/torrin/shared/hdencode"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/providers"
	"github.com/torrin-app/torrin/shared/qbit"
	"github.com/torrin-app/torrin/shared/rclonerc"
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

	store := storage.NewFSClient(env.Get("STORE_DIR", "/mnt/cache/store"), env.Get("PUBLIC_URL", ""), mustEnv("SIGNING_KEY"))
	if err := store.SetStorageKey(env.Get("STORAGE_KEY", "")); err != nil {
		fatal("storage key", err)
	}

	b, err := bus.Connect(mustEnv("NATS_URL"))
	if err != nil {
		fatal("nats", err)
	}
	defer b.Close()

	cipher, err := crypto.NewStream(env.Get("STORAGE_KEY", ""))
	if err != nil {
		fatal("storage key", err)
	}
	var caches []*rclonerc.Client
	for _, u := range strings.Split(env.Get("CACHE_RC_URLS", ""), ",") {
		if u = strings.TrimSpace(u); u != "" {
			caches = append(caches, rclonerc.New(u))
		}
	}
	publish.VerifyVideo = env.Get("INGEST_VERIFY_VIDEO", "true") != "false"
	pub := publish.New(repo, store, env.Get("NODE_ID", ""), repo, cipher, caches)
	if env.Get("RUNTIME_GATE", "true") != "false" {
		pub.SetRuntimeSource(cinemeta.NewClient())
	}
	if node := env.Get("NODE_ID", ""); node != "" {
		pol := eviction.DefaultPolicy
		if c := env.Int("EVICTION_CAP_BYTES", 0); c > 0 {
			pol.StorageCapBytes = c
		}
		eviction.New(repo, store, pol, node).StartSchedule(ctx, int(env.Int("EVICTION_HOUR", 4)))
	}
	sysRD, sysAD := os.Getenv("RD_API_KEY"), os.Getenv("AD_API_KEY")
	providersFor := func(ctx context.Context, job *jobs.Job) []providers.Provider {
		var list []providers.Provider
		byok := false
		if u, _ := users.GetByID(ctx, job.UserID); u != nil {
			byok = plans.CanBYOK(u.PlanID)
		}
		userRD, _ := users.GetRDKey(ctx, job.UserID)
		if byok {
			if userRD != "" {
				list = append(list, providers.NewRealDebrid(userRD))
			}
			if k, _ := users.GetADKey(ctx, job.UserID); k != "" {
				list = append(list, providers.NewAllDebrid(k))
			}
			if k, _ := users.GetTBKey(ctx, job.UserID); k != "" {
				list = append(list, providers.NewTorBox(k))
			}
			if k, _ := users.GetPMKey(ctx, job.UserID); k != "" {
				list = append(list, providers.NewPremiumize(k))
			}
			if k, _ := users.GetOCKey(ctx, job.UserID); k != "" {
				list = append(list, providers.NewOffcloud(k))
			}
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
	scratch := env.Get("SCRATCH_DIR", "/scratch")
	debridRunner := debrid.NewRunner(providersFor, pub, repo, scratch, ban, dlConns, users.AddDebridUsage)

	var torrentRunner, seedRunner *torrent.Runner
	if qbURL := os.Getenv("QBIT_URL"); qbURL != "" {
		qb := qbit.NewClient(qbURL, env.Get("QBIT_USER", "admin"), env.Get("QBIT_PASS", ""))
		torrentRunner = torrent.NewRunner(qb, repo, pub, b, ban, store, env.Get("DOWNLOADS_DIR", "/downloads"), false, env.Get("NODE_ID", ""))
		go torrentRunner.Run(ctx)
		go torrent.StartTrackerRefresh(ctx, qb)

		seedURL := env.Get("QBIT_SEED_URL", qbURL)
		qbSeed := qb
		if seedURL != qbURL {
			qbSeed = qbit.NewClient(seedURL, env.Get("QBIT_SEED_USER", env.Get("QBIT_USER", "admin")), env.Get("QBIT_SEED_PASS", env.Get("QBIT_PASS", "")))
		}
		seedRunner = torrent.NewRunner(qbSeed, repo, pub, b, ban, store, env.Get("SEED_DOWNLOADS_DIR", env.Get("DOWNLOADS_DIR", "/downloads")), true, env.Get("NODE_ID", ""))
		go configureSeedQbit(ctx, qbSeed)
		go seedRunner.Run(ctx)
		go seedRunner.WatchSeeds(ctx)
		slog.Info("torrent engine enabled", "separate_seed_instance", seedURL != qbURL)
	}

	hosterRunner := hoster.NewRunner(func(ctx context.Context, j *jobs.Job) string {
		return sysAD
	}, repo, pub, b, ban, scratch, dlConns)

	usenetRunner := usenet.NewRunner(repo, store, service.CairnStore(), users, pub, b, ban, scratch, download.Credentials{
		Host:     os.Getenv("USENET_HOST"),
		Port:     atoiOr(os.Getenv("USENET_PORT"), 563),
		Username: os.Getenv("USENET_USER"),
		Password: os.Getenv("USENET_PASS"),
		SSL:      os.Getenv("USENET_SSL") != "false",
		MaxConns: atoiOr(os.Getenv("USENET_MAXCONNS"), 20),
	}, cipher)

	usenetFallback := release.NewUsenetFallback(users, usenetRunner, env.Get("USENET_INDEXER_URL", ""), env.Get("USENET_INDEXER_KEY", ""))

	releaseRunner := release.NewRunner(func(ctx context.Context, j *jobs.Job) string {
		return sysAD
	}, map[jobs.Source]release.Resolver{
		jobs.SourceHDEncode: hdclient.NewClient(os.Getenv("HDENCODE_SOLVER_URL")),
		jobs.SourceScenerls: scenerls.NewClient(),
	}, repo, pub, b, ban, scratch, dlConns, usenetFallback.Try)

	ytdlpRunner := ytdlp.NewRunner(repo, pub, b, ban, scratch, os.Getenv("YTDLP_BIN"), os.Getenv("YTDLP_PROXY"), os.Getenv("YTDLP_FORMAT"))
	go func() {
		data, err := ytdlpRunner.Extractors(ctx)
		if err != nil {
			slog.Warn("ytdlp extractors list failed", "err", err)
			return
		}
		if err := store.Put(ctx, "meta/extractors.json", bytes.NewReader(data), "application/json"); err != nil {
			slog.Warn("ytdlp extractors publish failed", "err", err)
		}
	}()

	cancels := newCancelRegistry()
	myNode := env.Get("NODE_ID", "")
	if _, err := bus.Subscribe(b, events.JobAssigned, func(a events.Assigned) {
		if a.Node != myNode {
			return
		}
		go process(ctx, repo, debridRunner, torrentRunner, seedRunner, usenetRunner, hosterRunner, releaseRunner, ytdlpRunner, usenetFallback, b, ban, cancels, a)
	}); err != nil {
		fatal("subscribe", err)
	}
	if _, err := bus.Subscribe(b, events.JobDeleted, func(d events.Deleted) {
		cancels.cancel(d.JobID)
		if d.Source == string(jobs.SourceTorrent) && d.Node == myNode && d.InfoHash != "" && torrentRunner != nil {
			torrentRunner.Remove(d.InfoHash)
			seedRunner.Remove(d.InfoHash)
		}
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
				reconcile(ctx, repo, pub, b, cancels, myNode)
				if torrentRunner != nil {
					torrentRunner.ReapOrphans(ctx)
					seedRunner.ReapOrphans(ctx)
				}
				t.Reset(5 * time.Minute)
			}
		}
	}()

	go sweepScratch(ctx, repo, scratch, myNode)

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

func configureSeedQbit(ctx context.Context, qb *qbit.Client) {
	for i := 0; i < 30; i++ {
		if qb.Login() == nil {
			if err := qb.ConfigureForSeeding(); err != nil {
				slog.Warn("seed qbit configure failed", "err", err)
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(8 * time.Second):
		}
	}
	slog.Warn("seed qbit unreachable after retries, not configured for seeding")
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
