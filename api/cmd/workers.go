package main

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/cluster"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/providers"
)

type queueScheduler struct {
	repo   *jobs.Postgres
	users  *auth.Store
	bus    *bus.Bus
	budget int64
	wake   chan struct{}
}

func newQueueScheduler(repo *jobs.Postgres, users *auth.Store, b *bus.Bus, budget int64) *queueScheduler {
	return &queueScheduler{repo: repo, users: users, bus: b, budget: budget, wake: make(chan struct{}, 1)}
}

func (q *queueScheduler) Wake() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *queueScheduler) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	q.Wake()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-q.wake:
		}
		q.drain(ctx)
	}
}

func (q *queueScheduler) drain(ctx context.Context) {
	queued, err := q.repo.ListByStatus(ctx, jobs.StatusQueued)
	if err != nil {
		return
	}
	if q.refreshPriorities(ctx, queued) {
		queued, err = q.repo.ListByStatus(ctx, jobs.StatusQueued)
		if err != nil {
			return
		}
	}
	for _, candidate := range queued {
		user, plan, ok := q.eligibleUser(ctx, candidate)
		if !ok {
			continue
		}
		if !queuedSourceEntitled(ctx, q.users, user, plan, candidate) {
			reason := "your current plan no longer supports this download source"
			if changed, err := q.repo.FailQueued(ctx, candidate.ID, reason); err == nil && changed {
				q.bus.Publish(events.JobFailed, events.Failed{JobID: candidate.ID, Reason: reason})
			}
			continue
		}
		job, err := q.repo.PromoteQueued(ctx, candidate.ID, plan.MaxConcurrent, q.budget)
		if err != nil || job == nil {
			continue
		}
		cluster.Assign(ctx, q.bus, q.repo, q.repo, job)
		slog.Info("queue: promoted download", "job", job.ID, "user", job.UserID, "source", job.Source,
			"wait", time.Since(job.CreatedAt).Round(time.Second))
	}
}

// refreshPriorities makes upgrades and downgrades affect already queued work
// before anything is promoted from the persisted ordering.
func (q *queueScheduler) refreshPriorities(ctx context.Context, queued []*jobs.Job) bool {
	changed := false
	for _, job := range queued {
		user, err := q.users.GetByID(ctx, job.UserID)
		if err != nil || user == nil {
			continue
		}
		plan := planForUser(user)
		if job.Priority == plan.Priority {
			continue
		}
		if updated, err := q.repo.SetQueuedPriority(ctx, job.ID, plan.Priority); err == nil && updated {
			changed = true
		}
	}
	return changed
}

func (q *queueScheduler) eligibleUser(ctx context.Context, job *jobs.Job) (*auth.User, plans.Plan, bool) {
	user, err := q.users.GetByID(ctx, job.UserID)
	if err != nil || user == nil || user.Banned || user.IsPaused() || time.Now().After(user.ExpiresAt) {
		return nil, plans.Plan{}, false
	}
	plan := planForUser(user)
	if over, err := q.users.MonthlyQuotaExceeded(ctx, user.ID, plan.MonthlyIngestBytes); err != nil || over {
		return nil, plans.Plan{}, false
	}
	return user, plan, true
}

func planForUser(user *auth.User) plans.Plan {
	plan, ok := plans.Get(user.PlanID)
	if !ok {
		return plans.Free
	}
	return plan
}

func queuedSourceEntitled(ctx context.Context, users *auth.Store, user *auth.User, plan plans.Plan, job *jobs.Job) bool {
	switch job.Source {
	case jobs.SourceHoster, jobs.SourceHDEncode, jobs.SourceScenerls:
		return plan.ID != "free"
	case jobs.SourceUsenet:
		if users.HasUserCairn(ctx, user.ID, job.InfoHash) {
			return true
		}
		_, err := users.GetUsenetCreds(ctx, user.ID)
		return (err == nil && plans.CanBYOK(plan.ID)) || plan.SystemUsenet
	default:
		return true
	}
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
