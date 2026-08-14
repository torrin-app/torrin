package torrent

import (
	"context"
	"log/slog"
	"time"

	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/qbit"
)

const (
	seedRatioTarget = 1.0
	seedMinTime     = 7 * 24 * time.Hour
	seedMaxTime     = 30 * 24 * time.Hour
)

func (r *Runner) WatchSeeds(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.checkSeeds(ctx)
		}
	}
}

func (r *Runner) checkSeeds(ctx context.Context) {
	if r.qb.Login() != nil {
		return
	}
	seeds, _ := r.repo.ListByStatus(ctx, jobs.StatusSeeding)
	for _, job := range seeds {
		if job.Node != r.node {
			continue
		}
		t, err := r.qb.GetTorrent(job.InfoHash)
		if err != nil {
			r.completeSeed(ctx, job, job.InfoHash, nil)
			continue
		}
		if seedTargetMet(t) {
			slog.Info("seed target met, releasing", "job", job.ID, "ratio", t.Ratio, "seeding_time", t.SeedingTime)
			r.completeSeed(ctx, job, job.InfoHash, t)
		}
	}
}

func seedTargetMet(t *qbit.Torrent) bool {
	st := time.Duration(t.SeedingTime) * time.Second
	if st >= seedMaxTime {
		return true
	}
	return t.Ratio >= seedRatioTarget && st >= seedMinTime
}

func (r *Runner) completeSeed(ctx context.Context, job *jobs.Job, hash string, t *qbit.Torrent) {
	if t != nil {
		r.deleteAndVerify(hash, t)
	}
	job.Status = jobs.StatusComplete
	r.repo.Update(ctx, job)
	r.bus.Publish(events.JobComplete, events.Complete{JobID: job.ID, InfoHash: job.InfoHash, Node: job.Node})
}
