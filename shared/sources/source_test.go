package sources

import (
	"context"
	"io"
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
)

type cachedStore struct{}

func (cachedStore) Has(context.Context, string) (bool, error)                     { return true, nil }
func (cachedStore) StreamUpload(context.Context, string, io.Reader, string) error { return nil }
func (cachedStore) Put(context.Context, string, io.Reader, string) error          { return nil }

type captureRepo struct {
	jobs.Repository
	updated *jobs.Job
}

func (c *captureRepo) Update(_ context.Context, j *jobs.Job) error {
	c.updated = j
	return nil
}

func TestIngestCompletesJob(t *testing.T) {
	repo := &captureRepo{}
	f := File{Name: "movie.mkv", Size: 10, CacheKey: "abc123", Source: jobs.SourceTelegram}
	job := &jobs.Job{UserID: "user1", InfoHash: "abc123", Name: "movie.mkv",
		Source: jobs.SourceTelegram, Node: "box2", Status: jobs.StatusDownloading}
	if err := Ingest(context.Background(), cachedStore{}, repo, f, job); err != nil {
		t.Fatal(err)
	}
	if job.Status != jobs.StatusComplete {
		t.Fatalf("ingest must complete the job, got %q", job.Status)
	}
	if job.Node != "box2" || job.UserID != "user1" {
		t.Fatalf("job identity lost: %+v", job)
	}
	if job.FileSize != 10 || len(job.Files) != 1 || job.Files[0].Name != "movie.mkv" {
		t.Fatalf("files/size not set: %+v", job)
	}
	if repo.updated == nil {
		t.Fatal("job not persisted via Update")
	}
}
