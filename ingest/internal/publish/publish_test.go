package publish

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/storage"
)

func TestUsableFilesDropsUndersized(t *testing.T) {
	job := &jobs.Job{Seed: true} // seed avoids os.Remove on the dropped path
	kept := usableFiles(job, []File{
		{Name: "S01E01.mkv", Size: 2_000_000_000},
		{Name: "Deleted scenes 4.mkv", Size: 820_583}, // the junk extra that nuked a 134GB pack
		{Name: "S01E02.mkv", Size: 1_500_000_000},
	})
	if len(kept) != 2 {
		t.Fatalf("expected 2 kept after dropping the junk extra, got %d", len(kept))
	}
	for _, f := range kept {
		if f.Size < minVideoFileSize {
			t.Errorf("kept an undersized file %q", f.Name)
		}
	}
	if got := usableFiles(job, []File{{Name: "truncated.mkv", Size: 500}}); len(got) != 0 {
		t.Errorf("a single undersized file should leave nothing, got %d", len(got))
	}
}

func TestMain(m *testing.M) {
	VerifyVideo = false // these tests publish synthetic byte files, not real videos
	os.Exit(m.Run())
}

type fakeStore struct {
	puts map[string]bool
	data map[string][]byte
}

func (f *fakeStore) StreamUpload(_ context.Context, key string, body io.Reader, _ string) error {
	io.Copy(io.Discard, body)
	f.puts[key] = true
	return nil
}
func (f *fakeStore) PutSized(_ context.Context, key string, body io.Reader, _ int64, _ string) error {
	io.Copy(io.Discard, body)
	f.puts[key] = true
	return nil
}
func (f *fakeStore) Put(_ context.Context, key string, body io.Reader, _ string) error {
	b, _ := io.ReadAll(body)
	f.puts[key] = true
	if f.data == nil {
		f.data = map[string][]byte{}
	}
	f.data[key] = b
	return nil
}
func (f *fakeStore) Head(_ context.Context, key string) (*storage.Object, error) {
	if f.puts[key] {
		return &storage.Object{}, nil
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeStore) GetBytes(_ context.Context, key string) ([]byte, error) {
	if b, ok := f.data[key]; ok {
		return b, nil
	}
	return nil, fmt.Errorf("not found")
}

type memRepo struct{ jobs map[string]*jobs.Job }

func (m *memRepo) Create(_ context.Context, j *jobs.Job) error { m.jobs[j.ID] = j; return nil }
func (m *memRepo) Update(_ context.Context, j *jobs.Job) error { m.jobs[j.ID] = j; return nil }
func (m *memRepo) Get(_ context.Context, id string) (*jobs.Job, error) {
	return m.jobs[id], nil
}
func (m *memRepo) GetByInfoHash(_ context.Context, h string) (*jobs.Job, error) { return nil, nil }
func (m *memRepo) NodeForInfoHash(_ context.Context, h string) string           { return "" }
func (m *memRepo) ListByInfoHash(_ context.Context, h string) ([]*jobs.Job, error) {
	var out []*jobs.Job
	for _, j := range m.jobs {
		if j.InfoHash == h {
			out = append(out, j)
		}
	}
	return out, nil
}
func (m *memRepo) ListByUser(context.Context, string, int) ([]*jobs.Job, error) { return nil, nil }
func (m *memRepo) ListByUserBefore(context.Context, string, time.Time, string, int) ([]*jobs.Job, error) {
	return nil, nil
}
func (m *memRepo) ListByStatus(context.Context, jobs.Status) ([]*jobs.Job, error) { return nil, nil }
func (m *memRepo) Delete(context.Context, string) error                           { return nil }
func (m *memRepo) ActiveCount(context.Context, string) (int, error)               { return 0, nil }
func (m *memRepo) DownloadingCount(context.Context, string) (int, error)          { return 0, nil }
func (m *memRepo) BudgetUsed(context.Context) (int64, error)                      { return 0, nil }
func (m *memRepo) RecordView(context.Context, string, string) (bool, error)       { return false, nil }
func (m *memRepo) SetProgress(context.Context, string, float64, int64) error      { return nil }

type fakeBlobs struct {
	blobs map[string]*jobs.Blob
	refs  map[string]string
}

func newFakeBlobs() *fakeBlobs {
	return &fakeBlobs{blobs: map[string]*jobs.Blob{}, refs: map[string]string{}}
}
func (b *fakeBlobs) LookupBlob(_ context.Context, ck string) (*jobs.Blob, error) {
	if bl, ok := b.blobs[ck]; ok {
		return bl, nil
	}
	return nil, fmt.Errorf("not found")
}
func (b *fakeBlobs) AddBlobRef(_ context.Context, infoHash string, idx int, ck string, size int64, enc bool) error {
	b.blobs[ck] = &jobs.Blob{ContentKey: ck, Size: size, Encrypted: enc}
	b.refs[fmt.Sprintf("%s/%d", infoHash, idx)] = ck
	return nil
}

func newPub(repo jobs.Repository, store Store) *Publisher {
	return New(repo, store, "", newFakeBlobs(), nil, nil)
}

func blobPut(puts map[string]bool) bool {
	for k := range puts {
		if len(k) > 6 && k[:6] == "blobs/" {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, dir, name string, size int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPublishMarksComplete(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "movie.mkv", 2_000_000)

	job := &jobs.Job{ID: "j1", InfoHash: "hash1", Name: "Movie"}
	repo := &memRepo{jobs: map[string]*jobs.Job{"j1": job}}
	store := &fakeStore{puts: map[string]bool{}}

	err := newPub(repo, store).Publish(context.Background(), job,
		[]File{{Name: "movie.mkv", Path: path, Size: 2_000_000}})
	if err != nil {
		t.Fatal(err)
	}

	got := repo.jobs["j1"]
	if got.Status != jobs.StatusComplete {
		t.Errorf("status = %s, want complete", got.Status)
	}
	if len(got.Files) != 1 || got.FileSize != 2_000_000 {
		t.Errorf("files=%d size=%d", len(got.Files), got.FileSize)
	}
	if !blobPut(store.puts) || !store.puts["hash1/manifest.json"] {
		t.Errorf("missing uploads: %v", store.puts)
	}
}

func TestPublishRoutesToAssignedNode(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "movie.mkv", 2_000_000)

	job := &jobs.Job{ID: "j1", InfoHash: "hash1", Name: "Movie", Node: "box2"}
	repo := &memRepo{jobs: map[string]*jobs.Job{"j1": job}}
	local := &fakeStore{puts: map[string]bool{}}
	box2 := &fakeStore{puts: map[string]bool{}}

	pub := New(repo, local, "box1", newFakeBlobs(), nil, nil)
	pub.SetNodeStores(map[string]Store{"box2": box2})

	if err := pub.Publish(context.Background(), job, []File{{Name: "movie.mkv", Path: path, Size: 2_000_000}}); err != nil {
		t.Fatal(err)
	}
	if job.Node != "box2" {
		t.Fatalf("assigned node clobbered to %q, want box2", job.Node)
	}
	if !blobPut(box2.puts) || !box2.puts["hash1/manifest.json"] {
		t.Errorf("box2 store missing uploads: %v", box2.puts)
	}
	if len(local.puts) != 0 {
		t.Errorf("local store should be untouched, got %v", local.puts)
	}
}

func TestPublishDedupCrossInfohash(t *testing.T) {
	dir := t.TempDir()
	content := make([]byte, 3_000_000)
	for i := range content {
		content[i] = byte(i * 7)
	}
	p1 := filepath.Join(dir, "a.mkv")
	p2 := filepath.Join(dir, "b.mkv")
	os.WriteFile(p1, content, 0o644)
	os.WriteFile(p2, content, 0o644)

	store := &fakeStore{puts: map[string]bool{}}
	blobs := newFakeBlobs()
	pub := New(&memRepo{jobs: map[string]*jobs.Job{}}, store, "", blobs, nil, nil)

	j1 := &jobs.Job{ID: "j1", InfoHash: "packA", Name: "A"}
	if err := pub.Publish(context.Background(), j1, []File{{Name: "a.mkv", Path: p1, Size: int64(len(content))}}); err != nil {
		t.Fatal(err)
	}
	j2 := &jobs.Job{ID: "j2", InfoHash: "releaseB", Name: "B"}
	if err := pub.Publish(context.Background(), j2, []File{{Name: "b.mkv", Path: p2, Size: int64(len(content))}}); err != nil {
		t.Fatal(err)
	}

	blobKeys := 0
	for k := range store.puts {
		if len(k) > 6 && k[:6] == "blobs/" {
			blobKeys++
		}
	}
	if blobKeys != 1 {
		t.Fatalf("same content under 2 infohashes should upload one blob, got %d", blobKeys)
	}
	if len(blobs.refs) != 2 {
		t.Fatalf("both infohashes should reference the blob, refs=%d", len(blobs.refs))
	}
}
