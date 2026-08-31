package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/torrin-app/torrin/shared/diskfree"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/eviction"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/nodestatus"
	"github.com/torrin-app/torrin/shared/service"
	"github.com/torrin-app/torrin/shared/storage"
)

const noCap int64 = 1 << 62

func main() {
	ctx := context.Background()
	node := env.Get("NODE_ID", "")
	dir := env.Get("STORE_DIR", "/mnt/cache/store")

	repo, err := jobs.NewPostgres(ctx, mustEnv("DATABASE_URL"))
	if err != nil {
		fatal("postgres", err)
	}
	store := storage.NewFSClient(dir, env.Get("PUBLIC_URL", ""), mustEnv("SIGNING_KEY"))
	if err := store.SetStorageKey(env.Get("STORAGE_KEY", "")); err != nil {
		fatal("storage key", err)
	}

	ns := nodestatus.New(3 * time.Minute)
	if err := ns.SetDB(ctx, repo.Pool()); err != nil {
		fatal("node status db", err)
	}

	pol := policyFromEnv()

	minFree := env.Int("STORE_MIN_FREE_BYTES", 0)
	fleetHasRoom := func(ctx context.Context) bool { return ns.OtherHasRoom(ctx, node, minFree) }

	eng := eviction.New(repo, store, pol, node)
	eng.StartSchedule(ctx, int(env.Int("EVICTION_HOUR", 4)))
	eng.StartDiskWatch(ctx, dir, minFree, env.Int("STORE_RECLAIM_BYTES", 0), 5*time.Minute, fleetHasRoom)

	go reportLoop(ctx, ns, node, dir, minFree)
	slog.Info("evictor started", "node", node, "store", dir)
	service.RunHealth("evictor", env.Get("PORT", "8089"))
}

func policyFromEnv() eviction.Policy {
	pol := eviction.DefaultPolicy
	pol.StorageCapBytes = noCap
	if c := env.Int("EVICTION_CAP_BYTES", 0); c > 0 {
		pol.StorageCapBytes = c
	}
	pol.NeverAccessedTTL = int(env.Int("EVICTION_NEVER_TTL", int64(pol.NeverAccessedTTL)))
	pol.StandardTTL = int(env.Int("EVICTION_STANDARD_TTL", int64(pol.StandardTTL)))
	pol.PopularTTL = int(env.Int("EVICTION_POPULAR_TTL", int64(pol.PopularTTL)))
	pol.PrewarmColdTTL = int(env.Int("EVICTION_PREWARM_TTL", int64(pol.PrewarmColdTTL)))
	pol.BudgetGraceDays = int(env.Int("EVICTION_BUDGET_GRACE", int64(pol.BudgetGraceDays)))
	return pol
}

func reportLoop(ctx context.Context, ns *nodestatus.Store, node, dir string, minFree int64) {
	every := time.Duration(env.Int("HEARTBEAT_SECONDS", 45)) * time.Second
	if every < time.Second {
		every = 45 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	reportOnce(ctx, ns, node, dir, minFree)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			reportOnce(ctx, ns, node, dir, minFree)
		}
	}
}

func reportOnce(ctx context.Context, ns *nodestatus.Store, node, dir string, minFree int64) {
	free, total, ok := diskfree.Stat(dir)
	if !ok {
		return
	}
	if err := ns.Report(ctx, node, free, total, minFree); err != nil {
		slog.Warn("evictor: report failed", "err", err)
	}
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fatal("missing env "+k, nil)
	}
	return v
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
