package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
)

func (d *deps) runStorageEviction(ctx context.Context, hour int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(nextDailyRun(hour))):
		}
		d.evictExpiredStorage(ctx)
	}
}

func (d *deps) evictExpiredStorage(ctx context.Context) {
	policies, err := d.users.ListStorageEvictPolicies(ctx)
	if err != nil {
		slog.Warn("byos evict: list policies", "err", err)
		return
	}
	for _, p := range policies {
		d.evictUserStorage(ctx, p)
	}
}

func (d *deps) evictUserStorage(ctx context.Context, p auth.EvictPolicy) {
	creds, err := d.users.GetStorageCreds(ctx, p.UserID)
	if err != nil || creds == nil || !creds.Enabled {
		return
	}
	cands, err := d.repo.BYOSEvictionCandidates(ctx, p.UserID)
	if err != nil {
		return
	}
	var total int64
	for _, c := range cands {
		total += c.FileSize
	}
	for _, c := range cands {
		overAge := p.AfterDays > 0 && c.DaysSinceAccess >= p.AfterDays
		overSize := p.MaxBytes > 0 && total > p.MaxBytes
		if !overAge && !overSize {
			continue
		}
		if err := auth.PurgeRelease(ctx, d.rc, p.UserID, creds, c.InfoHash); err != nil {
			slog.Warn("byos evict: purge", "user", p.UserID, "hash", c.InfoHash, "err", err)
			continue
		}
		d.repo.DeleteBYOSObject(ctx, c.JobID)
		total -= c.FileSize
		slog.Info("byos evict: removed from user storage", "user", p.UserID, "name", c.Name, "idle_days", c.DaysSinceAccess, "size_mb", c.FileSize/1e6)
	}
}

func nextDailyRun(hour int) time.Time {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}
