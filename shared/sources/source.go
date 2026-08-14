package sources

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
)

type Store interface {
	Has(ctx context.Context, key string) (bool, error)
	StreamUpload(ctx context.Context, key string, body io.Reader, contentType string) error
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
}

type File struct {
	Name     string
	Size     int64
	CacheKey string
	Source   jobs.Source
	Open     func(ctx context.Context) (io.ReadCloser, error)
}

func Ingest(ctx context.Context, store Store, repo jobs.Repository, f File, job *jobs.Job) error {
	if f.CacheKey == "" || f.Name == "" {
		return fmt.Errorf("sources: file needs CacheKey and Name")
	}
	key := manifest.Key(f.CacheKey, 0, f.Name)

	if cached, _ := store.Has(ctx, manifest.Path(f.CacheKey)); !cached {
		rc, err := f.Open(ctx)
		if err != nil {
			return fmt.Errorf("sources: open %s: %w", f.Source, err)
		}
		defer rc.Close()
		if err := store.StreamUpload(ctx, key, rc, "application/octet-stream"); err != nil {
			return fmt.Errorf("sources: upload: %w", err)
		}
		data, _ := manifest.Manifest{
			InfoHash: f.CacheKey, Name: f.Name, CreatedAt: time.Now().UTC(),
			Files: []manifest.File{{FileName: f.Name, DirectURL: key, FileSize: f.Size}},
		}.Marshal()
		if err := store.Put(ctx, manifest.Path(f.CacheKey), bytes.NewReader(data), "application/json"); err != nil {
			return fmt.Errorf("sources: manifest: %w", err)
		}
	}

	job.Files = []jobs.File{{Index: 0, Name: f.Name, Size: f.Size}}
	job.FileSize = f.Size
	job.Status = jobs.StatusComplete
	return repo.Update(ctx, job)
}
