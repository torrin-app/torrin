package publish

import (
	"context"
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
)

func TestCompleteQueuedSiblingAcrossNodesWithoutRevivingHistory(t *testing.T) {
	active := &jobs.Job{ID: "active", UserID: "u1", InfoHash: "h", Node: "box2", Status: jobs.StatusPublishing}
	queued := &jobs.Job{ID: "queued", UserID: "u2", InfoHash: "h", Status: jobs.StatusQueued,
		Season: 2, Episode: 3, InputKey: "torrent-input/retain-for-retry", BudgetGated: true}
	failed := &jobs.Job{ID: "failed", UserID: "u1", InfoHash: "h", Node: "box2", Status: jobs.StatusFailed}
	evicted := &jobs.Job{ID: "evicted", UserID: "u2", InfoHash: "h", Node: "box2", Status: jobs.StatusEvicted}
	other := &jobs.Job{ID: "other-active", UserID: "u3", InfoHash: "h", Node: "box3", Status: jobs.StatusDownloading}
	repo := &memRepo{jobs: map[string]*jobs.Job{}}
	for _, job := range []*jobs.Job{active, queued, failed, evicted, other} {
		repo.jobs[job.ID] = job
	}
	p := New(repo, &fakeStore{puts: map[string]bool{}}, "box2", newFakeBlobs(), nil, nil)
	files := []manifest.File{{FileName: "Show.S02E03.mkv", FileSize: 2_000_000}}
	if err := p.complete(context.Background(), "box2", "h", "Show", files, 2_000_000); err != nil {
		t.Fatal(err)
	}
	if active.Status != jobs.StatusComplete || queued.Status != jobs.StatusComplete || queued.Node != "box2" || len(queued.Files) != 1 {
		t.Fatalf("queued sibling did not reuse published node: %+v", queued)
	}
	if queued.Season != 2 || queued.Episode != 3 || !queued.BudgetGated || queued.InputKey != "torrent-input/retain-for-retry" {
		t.Fatalf("completion lost episode or retry metadata: %+v", queued)
	}
	if failed.Status != jobs.StatusFailed || evicted.Status != jobs.StatusEvicted {
		t.Fatal("completion revived non-canonical history")
	}
	if other.Status != jobs.StatusDownloading || other.Node != "box3" {
		t.Fatal("completion touched another node's active download")
	}
}
