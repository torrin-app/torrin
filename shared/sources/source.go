package sources

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/torrin-app/torrin/shared/blob"
	"github.com/torrin-app/torrin/shared/blobstore"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/video"
)

type Store interface {
	Has(ctx context.Context, key string) (bool, error)
	Head(ctx context.Context, key string) (*storage.Object, error)
	StreamUpload(ctx context.Context, key string, body io.Reader, contentType string) error
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
}

type File struct {
	Name     string
	Path     string
	Size     int64
	CacheKey string
	Source   jobs.Source
}

func Ingest(ctx context.Context, store Store, blobs blobstore.Index, cipher *crypto.Stream, repo jobs.Repository, f File, job *jobs.Job) error {
	if f.CacheKey == "" || f.Name == "" || f.Path == "" {
		return fmt.Errorf("sources: file needs CacheKey, Name and Path")
	}
	ck, err := blob.ContentKey(f.Path, f.Size)
	if err != nil {
		return fmt.Errorf("sources: content key: %w", err)
	}
	key := blob.StorageKey(ck)

	_, enc, _, err := blobstore.New(store, blobs, cipher, 0).Put(ctx, f.CacheKey, 0, f.Path, ck, key, video.ContentType(f.Name), f.Size)
	if err != nil {
		return err
	}

	data, _ := manifest.Manifest{
		InfoHash: f.CacheKey, Name: f.Name, CreatedAt: time.Now().UTC(),
		Files: []manifest.File{{FileName: f.Name, DirectURL: key, FileSize: f.Size, Enc: enc}},
	}.Marshal()
	if err := store.Put(ctx, manifest.Path(f.CacheKey), bytes.NewReader(data), "application/json"); err != nil {
		return fmt.Errorf("sources: manifest: %w", err)
	}

	job.Files = []jobs.File{{Index: 0, Name: f.Name, Size: f.Size, Key: key, Enc: enc}}
	job.FileSize = f.Size
	job.Status = jobs.StatusComplete
	return repo.Update(ctx, job)
}
