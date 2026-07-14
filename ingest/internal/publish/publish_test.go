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

type fakeStore struct{ puts map[string]bool }

func (f *fakeStore) StreamUpload(_ context.Context, key string, body io.Reader, _ string) error {
	io.Copy(io.Discard, body)
	f.puts[key] = true
	return nil
}
func (f *fakeStore) Put(_ context.Context, key string, body io.Reader, _ string) error {
	io.Copy(io.Discard, body)
	f.puts[key] = true
	return nil
}
func (f *fakeStore) Head(_ context.Context, _ string) (*storage.Object, error) {
	return nil, fmt.Errorf("not found")
}

type memRepo struct{ jobs map[string]*jobs.Job }

func (m *memRepo) Create(_ context.Context, j *jobs.Job) error { m.jobs[j.ID] = j; return nil }
func (m *memRepo) Update(_ context.Context, j *jobs.Job) error { m.jobs[j.ID] = j; return nil }
func (m *memRepo) Get(_ context.Context, id string) (*jobs.Job, error) {
	return m.jobs[id], nil
}
func (m *memRepo) GetByInfoHash(_ context.Context, h string) (*jobs.Job, error) { return nil, nil }
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

	err := New(repo, store, "").Publish(context.Background(), job,
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
	if !store.puts["hash1/file_0/movie.mkv"] || !store.puts["hash1/manifest.json"] {
		t.Errorf("missing uploads: %v", store.puts)
	}
}

type stallStore struct{}

func (stallStore) StreamUpload(ctx context.Context, _ string, body io.Reader, _ string) error {
	io.CopyN(io.Discard, body, 1)
	<-ctx.Done()
	return ctx.Err()
}
func (stallStore) Put(context.Context, string, io.Reader, string) error { return nil }
func (stallStore) Head(context.Context, string) (*storage.Object, error) {
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

	err := New(repo, stallStore{}, "").Publish(context.Background(), job,
		[]File{{Name: "movie.mkv", Path: path, Size: 2_000_000}})
	if err == nil {
		t.Fatal("expected upload to abort on stall")
	}
}

func TestPublishRejectsTinyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "x.mkv", 4)
	job := &jobs.Job{ID: "j1", InfoHash: "h", Name: "x"}
	repo := &memRepo{jobs: map[string]*jobs.Job{"j1": job}}

	err := New(repo, &fakeStore{puts: map[string]bool{}}, "").Publish(context.Background(), job,
		[]File{{Name: "x.mkv", Path: path, Size: 4}})
	if err == nil {
		t.Fatal("expected tiny file to be rejected")
	}
}
