package jobrun

import (
	"context"
	"log/slog"

	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/failure"
	"github.com/torrin-app/torrin/shared/jobs"
)

func Fail(ctx context.Context, repo jobs.Repository, b *bus.Bus, job *jobs.Job, err error) {
	msg := failure.Message(err)
	slog.Warn("job failed", "job", job.ID, "source", job.Source, "err", err)
	job.Status, job.Error = jobs.StatusFailed, msg
	repo.Update(ctx, job)
	b.Publish(events.JobFailed, events.Failed{JobID: job.ID, Reason: msg})
}

func Complete(ctx context.Context, repo jobs.Repository, b *bus.Bus, pub *publish.Publisher, job *jobs.Job, files []publish.File) error {
	job.Status = jobs.StatusProcessing
	repo.Update(ctx, job)
	if err := pub.Publish(ctx, job, files); err != nil {
		return err
	}
	b.Publish(events.JobComplete, events.Complete{JobID: job.ID, InfoHash: job.InfoHash, Node: job.Node})
	return nil
}
