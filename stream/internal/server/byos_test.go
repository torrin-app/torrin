package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
)

type fakeByosJobs struct {
	job *jobs.Job
	err error
}

func (f *fakeByosJobs) GetByInfoHash(context.Context, string) (*jobs.Job, error) {
	return f.job, f.err
}

func TestResolveStoreKey(t *testing.T) {
	ih := strings.Repeat("a", 40)
	ctx := context.Background()

	// non-blob key (unencrypted) is already the stored path
	b := &byosBackend{}
	if got := b.resolveStoreKey(ctx, ih, "hash/file_0/movie.mkv"); got != "hash/file_0/movie.mkv" {
		t.Fatalf("plain key must pass through, got %q", got)
	}

	// blob key: resolve name from the jobs DB (manifest is evicted by the time byos=1 is set)
	b = &byosBackend{jobs: &fakeByosJobs{job: &jobs.Job{Files: []jobs.File{
		{Index: 0, Name: "Arne no Jikenbo S01E12.mkv", Key: "blobs/b_abc", Enc: true},
	}}}}
	if got, want := b.resolveStoreKey(ctx, ih, "blobs/b_abc"), manifest.Key(ih, 0, "Arne no Jikenbo S01E12.mkv"); got != want {
		t.Fatalf("blob key must resolve to the stored path %q, got %q", want, got)
	}

	// no jobs backend or unknown hash -> empty (serveBYOS bails gracefully)
	b = &byosBackend{jobs: &fakeByosJobs{err: errors.New("no rows")}}
	if got := b.resolveStoreKey(ctx, ih, "blobs/b_abc"); got != "" {
		t.Fatalf("unresolved blob must return empty, got %q", got)
	}
}
