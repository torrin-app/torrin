package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
)

func sweepScratch(ctx context.Context, repo jobs.Repository, scratch, node string) {
	run := func() {
		active := map[string]bool{}
		for _, st := range []jobs.Status{jobs.StatusPending, jobs.StatusDownloading, jobs.StatusProcessing, jobs.StatusPublishing, jobs.StatusSeeding} {
			list, _ := repo.ListByStatus(ctx, st)
			for _, j := range list {
				if j.Node != node {
					continue
				}
				active[j.InfoHash] = true
			}
		}
		entries, err := os.ReadDir(scratch)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || active[e.Name()] {
				continue
			}
			dir := filepath.Join(scratch, e.Name())
			if info, err := os.Stat(dir); err == nil && time.Since(info.ModTime()) > 30*time.Minute {
				slog.Info("sweep: removing orphan scratch", "hash", e.Name())
				os.RemoveAll(dir)
			}
		}
	}
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

func reconcile(ctx context.Context, repo jobs.Repository, pub *publish.Publisher, b *bus.Bus, cancels *cancelRegistry, node string) {
	for _, st := range []jobs.Status{jobs.StatusPending, jobs.StatusDownloading, jobs.StatusProcessing, jobs.StatusPublishing} {
		list, err := repo.ListByStatus(ctx, st)
		if err != nil {
			continue
		}
		for _, job := range list {
			if job.Node != node {
				continue
			}
			if cancels.has(job.ID) {
				continue
			}
			if ok, _ := pub.CompleteFromCache(ctx, job.InfoHash); ok {
				continue
			}
			if st == jobs.StatusDownloading && job.Source == jobs.SourceTorrent {
				continue
			}
			slog.Info("reconcile: re-assigning stranded job", "job", job.ID, "status", st, "source", job.Source)
			b.Publish(events.JobAssigned, events.Assigned{
				JobID: job.ID, InfoHash: job.InfoHash, Magnet: job.Magnet,
				Source: string(job.Source), MaxBytes: job.MaxBytes,
				Node: job.Node,
			})
		}
	}
}
