package sources

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/storage"
)

type fakeStore struct {
	uploaded map[string]bool
	puts     map[string]bool
	headOK   bool
}

func (f *fakeStore) Has(context.Context, string) (bool, error) { return false, nil }
func (f *fakeStore) Head(context.Context, string) (*storage.Object, error) {
	if f.headOK {
		return &storage.Object{}, nil
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeStore) StreamUpload(_ context.Context, key string, body io.Reader, _ string) error {
	io.Copy(io.Discard, body)
	f.uploaded[key] = true
	return nil
}
func (f *fakeStore) PutSized(_ context.Context, key string, body io.Reader, _ int64, _ string) error {
	io.Copy(io.Discard, body)
	f.uploaded[key] = true
	return nil
}
func (f *fakeStore) Put(_ context.Context, key string, body io.Reader, _ string) error {
	io.Copy(io.Discard, body)
	f.puts[key] = true
	return nil
}

type fakeBlobs struct {
	existing *jobs.Blob
	refs     int
}

func (b *fakeBlobs) LookupBlob(context.Context, string) (*jobs.Blob, error) { return b.existing, nil }
func (b *fakeBlobs) AddBlobRef(context.Context, string, int, string, int64, bool) error {
	b.refs++
	return nil
}

type captureRepo struct {
	jobs.Repository
	updated *jobs.Job
}

func (c *captureRepo) Update(_ context.Context, j *jobs.Job) error {
	c.updated = j
	return nil
}

func writeTmp(t *testing.T, fill byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(path, bytes.Repeat([]byte{fill}, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIngestContentAddresses(t *testing.T) {
	store := &fakeStore{uploaded: map[string]bool{}, puts: map[string]bool{}}
	blobs := &fakeBlobs{}
	repo := &captureRepo{}
	f := File{Name: "movie.mkv", Path: writeTmp(t, 'x'), Size: 4096,
		CacheKey: strings.Repeat("a", 40), Source: jobs.SourceTelegram}
	job := &jobs.Job{UserID: "user1", InfoHash: f.CacheKey, Node: "box2", Status: jobs.StatusDownloading}

	if err := Ingest(context.Background(), store, blobs, nil, repo, f, job); err != nil {
		t.Fatal(err)
	}
	if job.Status != jobs.StatusComplete || repo.updated == nil {
		t.Fatalf("job not completed/persisted: %+v", job)
	}
	if len(job.Files) != 1 || !strings.HasPrefix(job.Files[0].Key, "blobs/b_") {
		t.Fatalf("file must be content-addressed, got %+v", job.Files)
	}
	if job.Files[0].Enc {
		t.Fatal("nil cipher must produce a plaintext blob")
	}
	if blobs.refs != 1 || len(store.uploaded) != 1 {
		t.Fatalf("blob not uploaded/registered: refs=%d uploads=%v", blobs.refs, store.uploaded)
	}
	if !store.puts[manifest.Path(f.CacheKey)] {
		t.Fatal("manifest not written")
	}
	if manifest.StreamQuery(job.InfoHash, job.Files[0].Enc) == "" {
		t.Fatal("telegram content must now yield a node-routable stream query (ih)")
	}
}

func TestIngestDedupsExistingBlob(t *testing.T) {
	store := &fakeStore{uploaded: map[string]bool{}, puts: map[string]bool{}, headOK: true}
	blobs := &fakeBlobs{existing: &jobs.Blob{Encrypted: true}}
	repo := &captureRepo{}
	f := File{Name: "movie.mkv", Path: writeTmp(t, 'y'), Size: 4096,
		CacheKey: strings.Repeat("b", 40), Source: jobs.SourceTelegram}
	job := &jobs.Job{InfoHash: f.CacheKey, Status: jobs.StatusDownloading}

	if err := Ingest(context.Background(), store, blobs, nil, repo, f, job); err != nil {
		t.Fatal(err)
	}
	if len(store.uploaded) != 0 {
		t.Fatal("an existing blob must not be re-uploaded")
	}
	if !job.Files[0].Enc {
		t.Fatal("dedup must inherit the existing blob's enc flag")
	}
	if blobs.refs != 1 {
		t.Fatal("dedup must still register a ref for this infohash")
	}
}
