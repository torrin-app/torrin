package publish

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/storage"
)

type stallStore struct{}

func (stallStore) StreamUpload(ctx context.Context, _ string, body io.Reader, _ string) error {
	io.CopyN(io.Discard, body, 1)
	<-ctx.Done()
	return ctx.Err()
}
func (stallStore) PutSized(ctx context.Context, _ string, body io.Reader, _ int64, _ string) error {
	io.CopyN(io.Discard, body, 1)
	<-ctx.Done()
	return ctx.Err()
}
func (stallStore) Put(context.Context, string, io.Reader, string) error { return nil }
func (stallStore) Head(context.Context, string) (*storage.Object, error) {
	return nil, fmt.Errorf("not found")
}
func (stallStore) GetBytes(context.Context, string) ([]byte, error) {
	return nil, fmt.Errorf("not found")
}

func TestUploadAbortsOnStall(t *testing.T) {
	old := uploadStallTimeout
	uploadStallTimeout = 50 * time.Millisecond
	defer func() { uploadStallTimeout = old }()

	dir := t.TempDir()
	path := writeFile(t, dir, "movie.mkv", 2_000_000)
	job := &jobs.Job{ID: "j1", InfoHash: "h", Name: "Movie"}
	repo := &memRepo{jobs: map[string]*jobs.Job{"j1": job}}

	err := newPub(repo, stallStore{}).Publish(context.Background(), job,
		[]File{{Name: "movie.mkv", Path: path, Size: 2_000_000}})
	if err == nil {
		t.Fatal("expected upload to abort on stall")
	}
}

func TestCompleteFromCache(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "movie.mkv", 2_000_000)

	main := &jobs.Job{ID: "j1", InfoHash: "hashX", Name: "Movie"}
	repo := &memRepo{jobs: map[string]*jobs.Job{"j1": main}}
	store := &fakeStore{puts: map[string]bool{}}
	pub := newPub(repo, store)
	if err := pub.Publish(context.Background(), main, []File{{Name: "movie.mkv", Path: path, Size: 2_000_000}}); err != nil {
		t.Fatal(err)
	}

	follower := &jobs.Job{ID: "j2", InfoHash: "hashX", Status: jobs.StatusDownloading}
	repo.jobs["j2"] = follower

	ok, err := pub.CompleteFromCache(context.Background(), "", "hashX")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if repo.jobs["j2"].Status != jobs.StatusComplete {
		t.Errorf("follower status = %s, want complete", repo.jobs["j2"].Status)
	}
	if len(repo.jobs["j2"].Files) != 1 {
		t.Errorf("follower files = %d, want 1", len(repo.jobs["j2"].Files))
	}

	if ok, _ := pub.CompleteFromCache(context.Background(), "", "nope"); ok {
		t.Error("unknown hash should report not cached")
	}
}

func TestPublishRejectsTinyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "x.mkv", 4)
	job := &jobs.Job{ID: "j1", InfoHash: "h", Name: "x"}
	repo := &memRepo{jobs: map[string]*jobs.Job{"j1": job}}

	err := newPub(repo, &fakeStore{puts: map[string]bool{}}).Publish(context.Background(), job,
		[]File{{Name: "x.mkv", Path: path, Size: 4}})
	if err == nil {
		t.Fatal("expected tiny file to be rejected")
	}
}

func TestCompleteOnlyTouchesOwnNode(t *testing.T) {
	mine := &jobs.Job{ID: "jb", InfoHash: "h", Node: "box2", Status: jobs.StatusDownloading}
	other := &jobs.Job{ID: "j1", InfoHash: "h", Node: "", Status: jobs.StatusDownloading}
	repo := &memRepo{jobs: map[string]*jobs.Job{"jb": mine, "j1": other}}
	p := New(repo, &fakeStore{puts: map[string]bool{}}, "box2", newFakeBlobs(), nil, nil)

	files := []manifest.File{{FileName: "m.mkv", FileSize: 2_000_000}}
	if err := p.complete(context.Background(), "box2", "h", "Movie", files, 2_000_000); err != nil {
		t.Fatal(err)
	}
	if mine.Status != jobs.StatusComplete {
		t.Fatalf("own-node sibling status = %s, want complete", mine.Status)
	}
	if other.Status == jobs.StatusComplete {
		t.Fatal("other-node sibling must not be marked complete")
	}
	if other.Node != "" {
		t.Fatalf("other-node sibling node = %q, must stay empty", other.Node)
	}
}

func TestPublishRepicksToNodeWithRoom(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "movie.mkv", 2_000_000)
	job := &jobs.Job{ID: "j1", InfoHash: "hashZ", Name: "Movie", Node: "box1", Source: jobs.SourceTorrent}
	repo := &memRepo{jobs: map[string]*jobs.Job{"j1": job}}
	store := &fakeStore{puts: map[string]bool{}}
	pub := newPub(repo, store)
	pub.SetNodeStores(map[string]Store{"box1": store, "box2": store})
	pub.SetTargeter(func(context.Context, string, int64) string { return "box2" })
	if err := pub.Publish(context.Background(), job,
		[]File{{Name: "movie.mkv", Path: path, Size: 2_000_000}}); err != nil {
		t.Fatal(err)
	}
	if job.Node != "box2" {
		t.Fatalf("node = %q, want box2 (targeter should re-point off a full node)", job.Node)
	}
}

type fakeRuntime struct{ min int }

func (f fakeRuntime) Runtime(context.Context, string) (int, error) { return f.min, nil }

func TestRuntimeMismatch(t *testing.T) {
	if !runtimeMismatch(104*60, 159) {
		t.Error("104 min vs expected 159 (the mislabel case) should be flagged")
	}
	if runtimeMismatch(159*60, 159) {
		t.Error("exact runtime should pass")
	}
	if runtimeMismatch(190*60, 159) {
		t.Error("a longer cut within tolerance should pass")
	}
	if !runtimeMismatch(320*60, 159) {
		t.Error("double-length should be flagged")
	}
}

func TestCheckRuntimeGate(t *testing.T) {
	p := &Publisher{rt: fakeRuntime{159}}
	if err := p.checkRuntime(context.Background(), &jobs.Job{IMDBID: "4987556"}, 104*60); err == nil {
		t.Error("mismatched movie runtime should reject")
	}
	if err := p.checkRuntime(context.Background(), &jobs.Job{IMDBID: "4987556"}, 158*60); err != nil {
		t.Errorf("matching runtime should pass, got %v", err)
	}
	if err := p.checkRuntime(context.Background(), &jobs.Job{IMDBID: "x", Season: 1, Episode: 3}, 20*60); err != nil {
		t.Error("episodes must be skipped by the gate")
	}
	if err := p.checkRuntime(context.Background(), &jobs.Job{}, 20*60); err != nil {
		t.Error("no imdb must be skipped")
	}
	if err := (&Publisher{}).checkRuntime(context.Background(), &jobs.Job{IMDBID: "x"}, 10*60); err != nil {
		t.Error("nil runtime source must be skipped")
	}
}
