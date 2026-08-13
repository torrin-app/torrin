package main

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/cluster"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/eviction"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/providers"
	"github.com/torrin-app/torrin/shared/storage"
)

func promoteQueued(ctx context.Context, repo *jobs.Postgres, users *auth.Store, b *bus.Bus, budget int64) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for range t.C {
		queued, err := repo.ListByStatus(ctx, jobs.StatusQueued)
		if err != nil {
			continue
		}
		for _, job := range queued {
			if used, _ := repo.BudgetUsed(ctx); budget-used < 5_000_000_000 {
				break
			}
			if !userDownloadSlotFree(ctx, repo, users, job.UserID) {
				continue
			}
			job.Status = jobs.StatusPending
			job.Node = cluster.TargetNode(ctx, repo, string(job.Source), job.MaxBytes)
			if repo.Update(ctx, job) != nil {
				continue
			}
			b.Publish(events.JobAssigned, events.Assigned{
				JobID: job.ID, InfoHash: job.InfoHash, Magnet: job.Magnet,
				Source: string(job.Source), MaxBytes: job.MaxBytes,
				Node: job.Node,
			})
		}
	}
}

func userDownloadSlotFree(ctx context.Context, repo jobs.Repository, users *auth.Store, userID string) bool {
	user, err := users.GetByID(ctx, userID)
	if err != nil || user == nil {
		return false
	}
	planID := user.PlanID
	if time.Now().After(user.ExpiresAt) {
		planID = "free"
	}
	plan, ok := plans.Get(planID)
	if !ok {
		plan = plans.Free
	}
	dc, _ := repo.DownloadingCount(ctx, userID)
	return dc < plan.MaxConcurrent
}

func startLibrarySync(ctx context.Context, users *auth.Store) {
	go func() {
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			syncProvider(ctx, "rd", users.GetAllRDKeys, providers.RDLibrary, users.SyncRDLibrary)
			syncProvider(ctx, "tb", users.GetAllTBKeys, providers.TBLibrary, users.SyncTBLibrary)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func syncProvider(ctx context.Context, label string,
	getKeys func(context.Context) (map[string]string, error),
	list func(context.Context, string) ([]providers.LibraryItem, error),
	save func(context.Context, string, []auth.LibraryEntry) error) {

	keys, err := getKeys(ctx)
	if err != nil {
		slog.Warn("library sync: get keys failed", "provider", label, "err", err)
		return
	}
	for userID, key := range keys {
		if ctx.Err() != nil {
			return
		}
		items, err := list(ctx, key)
		if err != nil {
			slog.Warn("library sync: list failed", "provider", label, "user", userID, "err", err)
			continue
		}
		entries := make([]auth.LibraryEntry, len(items))
		for i, it := range items {
			entries[i] = auth.LibraryEntry{InfoHash: it.Hash, Filename: it.Filename, Filesize: it.Size}
		}
		if err := save(ctx, userID, entries); err != nil {
			slog.Warn("library sync: save failed", "provider", label, "user", userID, "err", err)
		} else {
			slog.Info("library synced", "provider", label, "user", userID, "entries", len(entries))
		}
	}
}

func prewarmRetry(ctx context.Context, repo *jobs.Postgres, b *bus.Bus, maxBytes int64, maxActive int) {
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		fbs, err := repo.ListPrewarmFallbacks(ctx)
		if err != nil {
			continue
		}
		for _, f := range fbs {
			job, _ := repo.Get(ctx, f.JobID)
			if job != nil && job.Status.Active() {
				continue
			}
			if job != nil && job.Status == jobs.StatusComplete {
				repo.DeletePrewarmFallback(ctx, f.JobID)
				continue
			}
			if len(f.Remaining) == 0 {
				repo.DeletePrewarmFallback(ctx, f.JobID)
				continue
			}
			if active, _ := repo.ActiveCount(ctx, "prewarm"); active >= maxActive {
				continue
			}
			next, rest := f.Remaining[0], f.Remaining[1:]
			nj := &jobs.Job{
				UserID: "prewarm", InfoHash: next, Name: f.Name, IMDBID: f.IMDBID,
				Magnet: magnet.Build(next, f.Name),
				Source: jobs.SourceTorrent, Status: jobs.StatusPending,
				Priority: -100, MaxBytes: maxBytes,
			}
			if err := repo.Create(ctx, nj); err != nil {
				continue
			}
			repo.StorePrewarmFallback(ctx, nj.ID, f.IMDBID, f.Name, rest)
			repo.DeletePrewarmFallback(ctx, f.JobID)
			repo.Delete(ctx, f.JobID)
			nj.Node = cluster.TargetNode(ctx, repo, string(nj.Source), nj.MaxBytes)
			repo.Update(ctx, nj)
			b.Publish(events.JobAssigned, events.Assigned{
				JobID: nj.ID, InfoHash: nj.InfoHash, Magnet: nj.Magnet,
				Source: string(nj.Source), MaxBytes: nj.MaxBytes,
				Node: nj.Node,
			})
			slog.Info("prewarm retry: advanced to next release", "imdb", f.IMDBID, "hash", next, "left", len(rest))
		}
	}
}

func metricsSnapshot(ctx context.Context, repo *jobs.Postgres, users *auth.Store) {
	record := func() {
		if err := repo.RecordDailySnapshot(ctx, users.CountUsers(ctx)); err != nil {
			slog.Warn("metrics snapshot failed", "err", err)
		}
	}
	record()
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 4, 0, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
			record()
		}
	}
}

func startADWorkers(ctx context.Context, users *auth.Store, adKey string) {
	go func() {
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			mags, err := providers.ADListMagnets(ctx, adKey, "ready")
			if err != nil {
				slog.Warn("ad library sync failed", "err", err)
			} else {
				entries := make([]auth.LibraryEntry, 0, len(mags))
				for _, m := range mags {
					if m.Hash != "" {
						entries = append(entries, auth.LibraryEntry{
							InfoHash: strings.ToLower(m.Hash), Filename: m.Filename, Filesize: m.Size})
					}
				}
				if users.SyncADLibrary(ctx, entries) == nil {
					slog.Info("ad library synced", "entries", len(entries))
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := providers.ADReapOrphans(ctx, adKey); err != nil {
					slog.Warn("ad reaper failed", "err", err)
				} else if n > 0 {
					slog.Info("ad reaper: deleted orphan magnets", "count", n)
				}
			}
		}
	}()
}

func startEviction(ctx context.Context, repo *jobs.Postgres, store *storage.Client) {
	policy := eviction.DefaultPolicy
	if c := env.Int("EVICTION_CAP_BYTES", 0); c > 0 {
		policy.StorageCapBytes = c
	}
	eviction.New(repo, store, policy, env.Get("NODE_ID", "")).StartSchedule(ctx, int(env.Int("EVICTION_HOUR", 4)))
}
