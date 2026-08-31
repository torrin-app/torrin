package eviction

import (
	"context"
	"log/slog"
	"time"

	"github.com/torrin-app/torrin/shared/diskfree"
)

func (e *Engine) StartDiskWatch(ctx context.Context, path string, minFree, reclaimTo int64, interval time.Duration, fleetHasRoom func(context.Context) bool) {
	if path == "" || minFree <= 0 || interval <= 0 {
		return
	}
	if reclaimTo < minFree {
		reclaimTo = minFree
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				free, _, ok := diskfree.Stat(path)
				if !ok || free >= minFree {
					continue
				}
				if fleetHasRoom != nil && fleetHasRoom(ctx) {
					continue
				}
				evicted, freed := e.reclaimToFree(ctx, path, reclaimTo)
				slog.Warn("eviction: fleet full, reclaimed coldest", "node", e.node,
					"free_gb", free/1e9, "high_gb", minFree/1e9, "low_gb", reclaimTo/1e9, "evicted", evicted, "freed_gb", freed/1e9)
			}
		}
	}()
}

func (e *Engine) reclaimToFree(ctx context.Context, path string, target int64) (int64, int64) {
	var evicted, freed int64
	e.RunGC(ctx)
	if free, _, ok := diskfree.Stat(path); ok && free >= target {
		return evicted, freed
	}
	candidates, err := e.repo.GetEvictionCandidates(ctx, e.node)
	if err != nil {
		return evicted, freed
	}
	for _, c := range orderForReclaim(candidates) {
		if free, _, ok := diskfree.Stat(path); ok && free >= target {
			break
		}
		if c.DaysSinceAccess < e.policy.BudgetGraceDays {
			continue
		}
		if e.evict(ctx, c.InfoHash) != nil {
			continue
		}
		evicted++
		freed += c.FileSize
	}
	return evicted, freed
}
