package jobrun

import (
	"context"

	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
)

func Fail(ctx context.Context, repo jobs.Repository, b *bus.Bus, job *jobs.Job, reason string) {
	clean := jobs.UserError(reason)
	job.Status, job.Error = jobs.StatusFailed, clean
	repo.Update(ctx, job)
	b.Publish(events.JobFailed, events.Failed{JobID: job.ID, Reason: clean})
}

func Complete(ctx context.Context, repo jobs.Repository, b *bus.Bus, pub *publish.Publisher, job *jobs.Job, files []publish.File) error {
	job.Status = jobs.StatusProcessing
	repo.Update(ctx, job)
	if err := pub.Publish(ctx, job, files); err != nil {
		return err
	}
	b.Publish(events.JobComplete, events.Complete{JobID: job.ID, InfoHash: job.InfoHash})
	return nil
}
