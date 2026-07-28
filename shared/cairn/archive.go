package cairn

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"sync"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
	"github.com/torrin-app/torrin/shared/usenet/post"
)

type Store interface {
	GetBytes(ctx context.Context, key string) ([]byte, error)
	GetReader(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
}

type Archiver struct {
	store  Store
	users  *auth.Store
	poster *post.Poster
	mu     sync.Mutex
	active map[string]bool
}

func NewArchiver(store Store, users *auth.Store, poster *post.Poster) *Archiver {
	return &Archiver{store: store, users: users, poster: poster, active: map[string]bool{}}
}

func (a *Archiver) begin(hash string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active[hash] {
		return false
	}
	a.active[hash] = true
	return true
}

func (a *Archiver) end(hash string) {
	a.mu.Lock()
	delete(a.active, hash)
	a.mu.Unlock()
}

func (a *Archiver) Archive(ctx context.Context, hash string) {
	if !a.begin(hash) {
		return
	}
	defer a.end(hash)
	if _, _, ok := a.users.GetCairnArchive(ctx, hash); ok {
		return
	}
	manData, err := a.store.GetBytes(ctx, manifest.Path(hash))
	if err != nil {
		slog.Warn("cairn: manifest read", "hash", hash, "err", err)
		return
	}
	man, err := manifest.Parse(manData)
	if err != nil {
		slog.Warn("cairn: manifest parse", "hash", hash, "err", err)
		return
	}
	var files []post.FileInput
	var total int64
	for i, f := range man.Files {
		key := manifest.Key(hash, i, f.FileName)
		files = append(files, post.FileInput{
			Name: f.FileName, Size: f.FileSize,
			Open: func() (io.ReadCloser, error) { return a.store.GetReader(ctx, key) },
		})
		total += f.FileSize
	}
	slog.Info("cairn: streaming", "hash", hash, "files", len(files), "bytes", total)
	nzbBytes, err := a.poster.Post(ctx, files)
	if err != nil {
		slog.Warn("cairn: post failed", "hash", hash, "err", err)
		return
	}
	if err := a.store.Put(ctx, nzb.StorageKey(hash), bytes.NewReader(nzbBytes), "application/x-nzb"); err != nil {
		slog.Warn("cairn: store nzb", "hash", hash, "err", err)
		return
	}
	if err := a.users.SetCairnArchive(ctx, hash, nzb.StorageKey(hash), man.Name, total, nzbBytes); err != nil {
		slog.Warn("cairn: record", "hash", hash, "err", err)
		return
	}
	slog.Info("cairn: done", "hash", hash, "key", nzb.StorageKey(hash), "bytes", total)
}
