package blobstore

import (
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"time"

	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/storage"
)

type Store interface {
	Head(ctx context.Context, key string) (*storage.Object, error)
	StreamUpload(ctx context.Context, key string, body io.Reader, contentType string) error
}

type Index interface {
	LookupBlob(ctx context.Context, contentKey string) (*jobs.Blob, error)
	AddBlobRef(ctx context.Context, infoHash string, idx int, contentKey string, size int64, encrypted bool) error
}

type Uploader struct {
	store  Store
	index  Index
	cipher *crypto.Stream
	stall  time.Duration
}

func New(store Store, index Index, cipher *crypto.Stream, stall time.Duration) *Uploader {
	if stall <= 0 {
		stall = 2 * time.Minute
	}
	return &Uploader{store: store, index: index, cipher: cipher, stall: stall}
}

func (u *Uploader) Put(ctx context.Context, infoHash string, idx int, path, contentKey, key, contentType string, size int64) (crc uint32, enc bool, wrote bool, err error) {
	reuse := false
	if existing, e := u.index.LookupBlob(ctx, contentKey); e == nil && existing != nil {
		if _, he := u.store.Head(ctx, key); he == nil {
			reuse, enc = true, existing.Encrypted
		}
	}
	if reuse {
		if crc, err = crcFile(path); err != nil {
			return
		}
	} else {
		enc = u.cipher != nil
		if crc, err = u.upload(ctx, key, path, contentType, enc); err != nil {
			return
		}
	}
	if err = u.index.AddBlobRef(ctx, infoHash, idx, contentKey, size, enc); err != nil {
		return
	}
	wrote = !reuse
	return
}

func (u *Uploader) upload(ctx context.Context, key, path, contentType string, enc bool) (uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchdog := time.AfterFunc(u.stall, cancel)
	defer watchdog.Stop()
	h := crc32.NewIEEE()
	var body io.Reader = &stallReader{r: io.TeeReader(f, h), timer: watchdog, timeout: u.stall}
	if enc {
		if body, err = u.cipher.EncryptReader(body); err != nil {
			return 0, err
		}
	}
	if err := u.store.StreamUpload(ctx, key, body, contentType); err != nil {
		return 0, fmt.Errorf("upload %s: %w", key, err)
	}
	return h.Sum32(), nil
}

func crcFile(path string) (uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	h := crc32.NewIEEE()
	if _, err := io.Copy(h, f); err != nil {
		return 0, err
	}
	return h.Sum32(), nil
}

type stallReader struct {
	r       io.Reader
	timer   *time.Timer
	timeout time.Duration
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.timer.Reset(s.timeout)
	}
	return n, err
}
