package publish

import (
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/torrin-app/torrin/shared/blob"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/failure"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/mediainfo"
	"github.com/torrin-app/torrin/shared/rclonerc"
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/video"
)

const minVideoFileSize = 1_000_000

var uploadStallTimeout = 2 * time.Minute

var VerifyVideo = true

type File struct {
	Name string
	Path string
	Size int64
}

type Store interface {
	StreamUpload(ctx context.Context, key string, body io.Reader, contentType string) error
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
	Head(ctx context.Context, key string) (*storage.Object, error)
	GetBytes(ctx context.Context, key string) ([]byte, error)
}

type BlobIndex interface {
	LookupBlob(ctx context.Context, contentKey string) (*jobs.Blob, error)
	AddBlobRef(ctx context.Context, infoHash string, idx int, contentKey string, size int64, encrypted bool) error
}

type Publisher struct {
	repo   jobs.Repository
	store  Store
	node   string
	blobs  BlobIndex
	cipher *crypto.Stream
	caches []*rclonerc.Client
}

func New(repo jobs.Repository, store Store, node string, blobs BlobIndex, cipher *crypto.Stream, caches []*rclonerc.Client) *Publisher {
	return &Publisher{repo: repo, store: store, node: node, blobs: blobs, cipher: cipher, caches: caches}
}

func (p *Publisher) Publish(ctx context.Context, job *jobs.Job, files []File) error {
	for _, f := range files {
		if f.Size < minVideoFileSize {
			return failure.Newf("incomplete", "file %q too small (%d bytes), likely corrupt", f.Name, f.Size)
		}
	}

	job.Status = jobs.StatusPublishing
	job.Node = p.node
	p.repo.Update(ctx, job)

	var mfFiles []manifest.File
	var total int64
	wroteBlob := false
	for i, f := range files {
		ck, err := blob.ContentKey(f.Path, f.Size)
		if err != nil {
			return err
		}
		key := blob.StorageKey(ck)
		crc, enc, wrote, err := p.store2blob(ctx, job.InfoHash, i, key, ck, f)
		if err != nil {
			return err
		}
		wroteBlob = wroteBlob || wrote
		var mi *mediainfo.Info
		if video.IsVideo(f.Name) {
			pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			info, err := mediainfo.Probe(pctx, f.Path)
			cancel()
			if err != nil || !mediainfo.Playable(info) {
				if VerifyVideo {
					return failure.Newf("incomplete", "file %q is not a playable video, likely a partial or corrupt download", f.Name)
				}
				slog.Warn("mediainfo probe failed", "file", f.Name, "err", err)
			} else {
				mi = info
			}
		}
		if !job.Seed {
			os.Remove(f.Path)
		}
		total += f.Size
		mfFiles = append(mfFiles, manifest.File{FileName: f.Name, DirectURL: key, FileSize: f.Size, Crc32: crc, Enc: enc, MediaInfo: mi})
	}

	name := job.Name
	if name == "" && len(files) > 0 {
		name = files[0].Name
	}

	data, err := manifest.Manifest{
		InfoHash:  job.InfoHash,
		Name:      name,
		Files:     mfFiles,
		CreatedAt: time.Now(),
	}.Marshal()
	if err != nil {
		return err
	}
	if err := p.store.Put(ctx, manifest.Path(job.InfoHash), bytes.NewReader(data), "application/json"); err != nil {
		return err
	}

	if wroteBlob {
		p.refreshCaches(ctx)
	}
	if err := p.complete(ctx, job.InfoHash, name, mfFiles, total); err != nil {
		return err
	}
	return nil
}

func (p *Publisher) refreshCaches(ctx context.Context) {
	var wg sync.WaitGroup
	for _, c := range p.caches {
		wg.Add(1)
		go func(c *rclonerc.Client) {
			defer wg.Done()
			rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := c.VFSRefresh(rctx, "blobs"); err != nil {
				slog.Warn("publish: cache refresh failed", "err", err)
			}
		}(c)
	}
	wg.Wait()
}

func (p *Publisher) store2blob(ctx context.Context, infoHash string, idx int, key, ck string, f File) (uint32, bool, bool, error) {
	var crc uint32
	enc := false
	reuse := false
	if existing, err := p.blobs.LookupBlob(ctx, ck); err == nil && existing != nil {
		if _, herr := p.store.Head(ctx, key); herr == nil {
			reuse, enc = true, existing.Encrypted
		}
	}
	if reuse {
		c, err := crcFile(f.Path)
		if err != nil {
			return 0, false, false, err
		}
		crc = c
	} else {
		enc = p.cipher != nil
		c, err := p.uploadBlob(ctx, key, f.Path, enc)
		if err != nil {
			return 0, false, false, err
		}
		crc = c
	}
	if err := p.blobs.AddBlobRef(ctx, infoHash, idx, ck, f.Size, enc); err != nil {
		return 0, false, false, err
	}
	return crc, enc, !reuse, nil
}

func (p *Publisher) uploadBlob(ctx context.Context, key, path string, enc bool) (uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchdog := time.AfterFunc(uploadStallTimeout, cancel)
	defer watchdog.Stop()
	h := crc32.NewIEEE()
	var body io.Reader = &stallReader{r: io.TeeReader(f, h), timer: watchdog, timeout: uploadStallTimeout}
	if enc {
		body, err = p.cipher.EncryptReader(body)
		if err != nil {
			return 0, err
		}
	}

	if err := p.store.StreamUpload(ctx, key, body, video.ContentType(path)); err != nil {
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

func (p *Publisher) CompleteFromCache(ctx context.Context, infoHash string) (bool, error) {
	data, err := p.store.GetBytes(ctx, manifest.Path(infoHash))
	if err != nil {
		return false, nil
	}
	man, err := manifest.Parse(data)
	if err != nil {
		return false, err
	}
	var total int64
	for _, f := range man.Files {
		total += f.FileSize
	}
	return true, p.complete(ctx, infoHash, man.Name, man.Files, total)
}

func (p *Publisher) complete(ctx context.Context, infoHash, name string, files []manifest.File, total int64) error {
	jobFiles := make([]jobs.File, len(files))
	for i, f := range files {
		jobFiles[i] = jobs.File{Index: i, Name: f.FileName, Size: f.FileSize, Key: f.DirectURL, Enc: f.Enc, MediaInfo: f.MediaInfo}
	}
	siblings, err := p.repo.ListByInfoHash(ctx, infoHash)
	if err != nil {
		return err
	}
	for _, sib := range siblings {
		if sib.Node != p.node {
			continue
		}
		sib.Status = jobs.StatusComplete
		sib.Error = ""
		sib.Name = name
		sib.Files = jobFiles
		sib.FileSize = total
		if err := p.repo.Update(ctx, sib); err != nil {
			return err
		}
	}
	return nil
}
