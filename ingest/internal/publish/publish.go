package publish

import (
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/mediainfo"
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/video"
)

const minVideoFileSize = 1_000_000

var uploadStallTimeout = 2 * time.Minute

type File struct {
	Name string
	Path string
	Size int64
}

type Store interface {
	StreamUpload(ctx context.Context, key string, body io.Reader, contentType string) error
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
	Head(ctx context.Context, key string) (*storage.Object, error)
}

type Publisher struct {
	repo  jobs.Repository
	store Store
	node  string
}

func New(repo jobs.Repository, store Store, node string) *Publisher {
	return &Publisher{repo: repo, store: store, node: node}
}

func (p *Publisher) Publish(ctx context.Context, job *jobs.Job, files []File) error {
	for _, f := range files {
		if f.Size < minVideoFileSize {
			return fmt.Errorf("file %q too small (%d bytes), likely corrupt", f.Name, f.Size)
		}
	}

	job.Status = jobs.StatusPublishing
	job.Node = p.node
	p.repo.Update(ctx, job)

	var mfFiles []manifest.File
	var total int64
	for i, f := range files {
		key := manifest.Key(job.InfoHash, i, f.Name)
		var crc uint32
		if obj, err := p.store.Head(ctx, key); err != nil || obj.Size != f.Size {
			c, err := p.upload(ctx, key, f.Path)
			if err != nil {
				return err
			}
			crc = c
		} else if c, err := crcFile(f.Path); err == nil {
			crc = c
		}
		var mi *mediainfo.Info
		if video.IsVideo(f.Name) {
			pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			info, err := mediainfo.Probe(pctx, f.Path)
			cancel()
			if err != nil {
				slog.Warn("mediainfo probe failed", "file", f.Name, "err", err)
			} else {
				mi = info
			}
		}
		os.Remove(f.Path)
		total += f.Size
		mfFiles = append(mfFiles, manifest.File{FileName: f.Name, DirectURL: key, FileSize: f.Size, Crc32: crc, MediaInfo: mi})
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

	return p.complete(ctx, job.InfoHash, name, mfFiles, total)
}

func (p *Publisher) upload(ctx context.Context, key, path string) (uint32, error) {
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
	r := &stallReader{r: io.TeeReader(f, h), timer: watchdog, timeout: uploadStallTimeout}

	if err := p.store.StreamUpload(ctx, key, r, video.ContentType(path)); err != nil {
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

func (p *Publisher) complete(ctx context.Context, infoHash, name string, files []manifest.File, total int64) error {
	jobFiles := make([]jobs.File, len(files))
	for i, f := range files {
		jobFiles[i] = jobs.File{Index: i, Name: f.FileName, Size: f.FileSize, MediaInfo: f.MediaInfo}
	}
	siblings, err := p.repo.ListByInfoHash(ctx, infoHash)
	if err != nil {
		return err
	}
	for _, sib := range siblings {
		sib.Status = jobs.StatusComplete
		sib.Error = ""
		sib.Name = name
		sib.Files = jobFiles
		sib.FileSize = total
		sib.Node = p.node
		if err := p.repo.Update(ctx, sib); err != nil {
			return err
		}
	}
	return nil
}
