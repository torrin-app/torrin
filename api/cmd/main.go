package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/handlers"
	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/rdapi"
	"github.com/torrin-app/torrin/api/internal/stremthru"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/availcache"
	"github.com/torrin-app/torrin/shared/billing"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/cinemeta"
	"github.com/torrin-app/torrin/shared/cluster"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/email"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/episodes"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/nodestatus"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/providers"
	"github.com/torrin-app/torrin/shared/qbit"
	"github.com/torrin-app/torrin/shared/rclonerc"
	"github.com/torrin-app/torrin/shared/safety"
	"github.com/torrin-app/torrin/shared/scrape"
	"github.com/torrin-app/torrin/shared/service"
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/torrentclaw"
)

func main() {
	ctx := context.Background()
	dsn := mustEnv("DATABASE_URL")

	jobsRepo, err := jobs.NewPostgres(ctx, dsn)
	if err != nil {
		fatal("jobs db", err)
	}
	if err := providers.SetRateLimitDB(ctx, jobsRepo.Pool()); err != nil {
		slog.Warn("rate-limit db init failed, using per-process limiter", "err", err)
	}
	users, err := auth.NewPostgres(ctx, dsn)
	if err != nil {
		fatal("auth db", err)
	}
	if err := users.SetCredsKey(env.Get("CREDS_KEY", "")); err != nil {
		fatal("creds key", err)
	}

	plans.DayPlansEnabled = env.Get("DAY_PLANS_ENABLED", "true") != "false"
	auth.QuotaEnforceMonth = env.Get("INGEST_QUOTA_ENFORCE_MONTH", "")
	middleware.SetProxySecret(env.Get("TRUSTED_PROXY_SECRET", ""))

	if n, err := users.BackfillResellerRetail(ctx, func(planID, period string, days int, createdAt time.Time) int {
		c, _ := plans.PriceCentsAt(planID, period, days, createdAt)
		return c
	}); err != nil {
		slog.Warn("reseller retail backfill", "err", err)
	} else if n > 0 {
		slog.Info("reseller retail backfilled", "rows", n)
	}

	store := storage.NewFSClient(env.Get("STORE_DIR", "/mnt/cache/store"), env.Get("PUBLIC_URL", ""), mustEnv("SIGNING_KEY"))
	if err := store.SetStorageKey(env.Get("STORAGE_KEY", "")); err != nil {
		fatal("storage key", err)
	}

	store.SetNodeBases(parseNodeBases(env.Get("NODE_STREAM_BASES", "")))
	cairnStore := service.CairnStore()
	cairnCipher, err := crypto.NewStream(env.Get("STORAGE_KEY", ""))
	if err != nil {
		fatal("cairn stream key", err)
	}

	nodeStores := map[string]handlers.Storage{}
	for node, c := range storage.ParseNodes(env.Get("STORE_NODES", ""), env.Get("STORE_S3_REGION", "auto"),
		env.Get("STORE_S3_ACCESS_KEY", ""), env.Get("STORE_S3_SECRET_KEY", ""),
		env.Get("STORE_S3_BUCKET", "torrin"), env.Get("PUBLIC_URL", ""), mustEnv("SIGNING_KEY")) {
		if err := c.SetStorageKey(env.Get("STORAGE_KEY", "")); err != nil {
			fatal("store node key", err)
		}
		nodeStores[node] = c
	}

	b, err := bus.Connect(mustEnv("NATS_URL"))
	if err != nil {
		fatal("nats", err)
	}
	defer b.Close()

	budget := env.Int("BUDGET_MAX_BYTES", 1_000_000_000_000)

	var qb, qbSeed *qbit.Client
	if u := os.Getenv("QBIT_URL"); u != "" {
		qb = qbit.NewClient(u, env.Get("QBIT_USER", "admin"), env.Get("QBIT_PASS", ""))
		if su := env.Get("QBIT_SEED_URL", u); su != u {
			qbSeed = qbit.NewClient(su, env.Get("QBIT_SEED_USER", env.Get("QBIT_USER", "admin")), env.Get("QBIT_SEED_PASS", env.Get("QBIT_PASS", "")))
		} else {
			qbSeed = qb
		}
	}

	pwMaxBytes := env.Int("PREWARM_MAX_BYTES_GB", 25) * 1_000_000_000
	pwMaxActive := int(env.Int("PREWARM_MAX_ACTIVE", 3))
	pwCapBytes := env.Int("PREWARM_CACHE_CAP_GB", 150) * 1_000_000_000

	var mailer *email.Client
	if k := env.Get("EMAIL_API_KEY", ""); k != "" {
		mailer = email.NewClient(k, env.Get("EMAIL_FROM", "torrin <hi@torrin.app>"))
	}
	var rc *rclonerc.Client
	if u := env.Get("RCLONE_RC_URL", ""); u != "" {
		rc = rclonerc.New(u)
	}

	slots := middleware.NewSlotTracker(jobsRepo)
	bitcart := billing.NewBitcartHandler(
		env.Get("BITCART_API_URL", ""), env.Get("BITCART_CHECKOUT_URL", ""),
		env.Get("BITCART_STORE_ID", ""),
		env.Get("API_PUBLIC_URL", ""), env.Get("WEB_URL", ""), users)
	bachs := billing.NewBachsHandler(
		env.Get("BACHS_API_URL", ""), env.Get("BACHS_SECRET_KEY", ""),
		env.Get("BACHS_WEBHOOK_SECRET", ""), env.Get("BACHS_PRODUCT_ID", ""),
		env.Get("BACHS_SUB_PRODUCTS", ""),
		env.Get("API_PUBLIC_URL", ""), env.Get("WEB_URL", ""),
		env.Get("DONATION_DISCORD_WEBHOOK", ""), users)
	nowpay := billing.NewNowPaymentsHandler(
		env.Get("NOWPAYMENTS_API_KEY", ""), env.Get("NOWPAYMENTS_IPN_SECRET", ""),
		env.Get("API_PUBLIC_URL", ""), env.Get("WEB_URL", ""), users)
	nodeStat := nodestatus.New(3 * time.Minute)
	if err := nodeStat.SetDB(ctx, jobsRepo.Pool()); err != nil {
		slog.Warn("node status db init failed", "err", err)
	}
	cluster.SetStatuser(nodeStat)
	episodeResolver := episodes.New(cinemeta.NewClient())
	srv := handlers.New(handlers.Deps{
		EpisodeResolver: episodeResolver,
		Jobs:            jobsRepo, JobsPG: jobsRepo, Users: users, Store: store, NodeStores: nodeStores,
		CairnStore: cairnStore, CairnCipher: cairnCipher, CairnDirect: env.Get("USENET_HOST", "") != "", Bus: b,
		Slots: slots, Qbit: qb, QbitSeed: qbSeed, Scrape: scrape.New(), Mailer: mailer, Budget: budget,
		RClone: rc, Bitcart: bitcart, NowPay: nowpay, Bachs: bachs, SignKey: []byte(env.Get("SIGNING_KEY", "")),
		SeedingEnabled:    env.Get("SEEDING_ENABLED", "") == "true",
		SeedingAllowUsers: parseAllowUsers(env.Get("SEEDING_ALLOW_USERS", "")),
		Internal:          env.Get("SIGNING_KEY", ""),
		AdminKey:          env.Get("ADMIN_KEY", ""),
		IndexerURL:        env.Get("USENET_INDEXER_URL", ""),
		IndexerKey:        env.Get("USENET_INDEXER_KEY", ""),
		TGBotUsername:     env.Get("TELEGRAM_BOT_USERNAME", ""),
		ResellerKey:       env.Get("RESELLER_KEY", ""),
		APIBase:           env.Get("API_PUBLIC_URL", ""),
		WebBase:           env.Get("WEB_URL", ""),
		TurnstileSecret:   env.Get("TURNSTILE_SECRET", ""),
		TurnstileSiteKey:  env.Get("TURNSTILE_SITE_KEY", ""),
		PartnerKey:        env.Get("PARTNER_REGISTER_KEY", ""),
		PrewarmMaxBytes:   pwMaxBytes, PrewarmMaxActive: pwMaxActive, PrewarmCapBytes: pwCapBytes,
	})

	mux := http.NewServeMux()
	srv.Register(mux, middleware.Auth(users, []byte(env.Get("SIGNING_KEY", ""))))

	avail := availcache.New(time.Duration(env.Int("AVAIL_CACHE_TTL_MIN", 120)) * time.Minute)
	if err := avail.SetDB(ctx, jobsRepo.Pool()); err != nil {
		slog.Warn("avail cache db init failed, using memory only", "err", err)
	}
	providers.SetAvailCache(avail)

	var tc *torrentclaw.Client
	if k := env.Get("TORRENTCLAW_API_KEY", ""); k != "" {
		tc = torrentclaw.New(k)
		tc.SetCache(avail)
	}
	stremthru.New(stremthru.Deps{
		EpisodeResolver: episodeResolver,
		Users:           users, Jobs: jobsRepo, Store: store, Cairns: users, CairnStore: cairnStore,
		CairnCipher: cairnCipher, CairnDirect: env.Get("USENET_HOST", "") != "", Slots: slots, Bus: b,
		TC: tc, Qbit: qb, SysADKey: env.Get("AD_API_KEY", ""), SysRDKey: env.Get("RD_API_KEY", ""),
	}).Register(mux)

	rdapi.New(rdapi.Deps{Users: users, Jobs: jobsRepo, Store: store, Qbit: qb, Slots: slots, Bus: b}).Register(mux)

	gumroad := billing.NewGumroadHandler(env.Get("GUMROAD_SECRET", ""), env.Get("GUMROAD_SELLER_ID", ""), users)
	mux.HandleFunc("POST /webhooks/gumroad", gumroad.HandleWebhook)

	if bitcart.Enabled() {
		mux.HandleFunc("POST /webhooks/bitcart", bitcart.HandleWebhook)
	}

	if nowpay.Enabled() {
		mux.HandleFunc("POST /webhooks/nowpayments", nowpay.HandleWebhook)
	}

	if bachs.Enabled() {
		mux.HandleFunc("POST /webhooks/bachs", bachs.HandleWebhook)
	}

	safety.Refresh(ctx, users.GetBlocklist, time.Hour)
	go walletRenewLoop(ctx, users)
	go promoteQueued(ctx, jobsRepo, users, b, budget)
	go prewarmRetry(ctx, jobsRepo, b, pwMaxBytes, pwMaxActive)
	go srv.RunRSSWorker(ctx)
	go metricsSnapshot(ctx, jobsRepo, users)
	startLibrarySync(ctx, users)
	go func() {
		t := time.NewTicker(30 * time.Minute)
		defer t.Stop()
		for {
			jobsRepo.SweepColdPulls(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	go func() {
		if n, err := jobsRepo.BackfillTitleNorm(ctx); err == nil && n > 0 {
			slog.Info("title_norm backfill", "updated", n)
		}
	}()
	startIMDBResolver(ctx, jobsRepo)
	if adKey := env.Get("AD_API_KEY", ""); adKey != "" {
		startADWorkers(ctx, users, adKey)
	}

	slog.Info("api started", "budget_gb", budget/1e9)
	exempt := append(strings.Split(env.Get("RATE_LIMIT_EXEMPT", ""), ","), env.Get("TORRIN_SEARCH_KEY", ""))
	rl := middleware.RateLimit(int(env.Int("RATE_LIMIT_RPS", 30)), int(env.Int("RATE_LIMIT_BURST", 60)), exempt)
	service.Run("api", "8080", middleware.CORS(rl(mux)))
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fatal("missing env "+k, nil)
	}
	return v
}

func parseAllowUsers(csv string) map[string]bool {
	m := map[string]bool{}
	for _, e := range strings.Split(csv, ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			m[e] = true
		}
	}
	return m
}

func parseNodeBases(s string) map[string]string {
	m := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		if k, v, ok := strings.Cut(strings.TrimSpace(pair), "="); ok && k != "" {
			m[strings.TrimSpace(k)] = strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	return m
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
