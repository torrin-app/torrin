package publish

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/torrin-app/torrin/shared/blob"
	"github.com/torrin-app/torrin/shared/blobstore"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/failure"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/mediainfo"
	"github.com/torrin-app/torrin/shared/providers"
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
	PutSized(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
	Head(ctx context.Context, key string) (*storage.Object, error)
	GetBytes(ctx context.Context, key string) ([]byte, error)
}

type BlobIndex interface {
	LookupBlob(ctx context.Context, contentKey string) (*jobs.Blob, error)
	AddBlobRef(ctx context.Context, infoHash string, idx int, contentKey string, size int64, encrypted bool) error
}

type RuntimeSource interface {
	Runtime(ctx context.Context, imdbID string) (int, error)
}

type nodeStore struct {
	store Store
	bs    *blobstore.Uploader
}

type Publisher struct {
	repo   jobs.Repository
	store  Store
	node   string
	blobs  BlobIndex
	cipher *crypto.Stream
	caches []*rclonerc.Client
	bs     *blobstore.Uploader
	rt     RuntimeSource
	nodes  map[string]*nodeStore
	usage  func(context.Context, string, int64) error
	target func(context.Context, string, int64) string
}

func New(repo jobs.Repository, store Store, node string, blobs BlobIndex, cipher *crypto.Stream, caches []*rclonerc.Client) *Publisher {
	return &Publisher{
		repo: repo, store: store, node: node, blobs: blobs, cipher: cipher, caches: caches,
		bs: blobstore.New(store, blobs, cipher, uploadStallTimeout),
	}
}

func (p *Publisher) SetRuntimeSource(rt RuntimeSource) { p.rt = rt }

func (p *Publisher) SetUsageRecorder(fn func(context.Context, string, int64) error) { p.usage = fn }

func (p *Publisher) SetTargeter(fn func(context.Context, string, int64) string) { p.target = fn }

func (p *Publisher) SetNodeStores(stores map[string]Store) {
	if len(stores) == 0 {
		return
	}
	p.nodes = make(map[string]*nodeStore, len(stores))
	for node, st := range stores {
		p.nodes[node] = &nodeStore{store: st, bs: blobstore.New(st, p.blobs, p.cipher, uploadStallTimeout)}
	}
}

func (p *Publisher) storeFor(node string) (Store, *blobstore.Uploader) {
	if ns, ok := p.nodes[node]; ok {
		return ns.store, ns.bs
	}
	return p.store, p.bs
}

func (p *Publisher) remote() bool { return len(p.nodes) > 0 }

func (p *Publisher) MeterBytes(ctx context.Context, userID string, seed bool, bytes int64) {
	if p.usage == nil || userID == "" || userID == "prewarm" || seed || bytes <= 0 {
		return
	}
	if err := p.usage(context.WithoutCancel(ctx), userID, bytes); err != nil {
		slog.Warn("ingest usage record failed", "user", userID, "err", err)
	}
}

func (p *Publisher) meterDownload(ctx context.Context, job *jobs.Job, files []File) {
	if t := providers.TallyFrom(ctx); t != nil {
		t.MarkPublished()
	}
	var dl int64
	for _, f := range files {
		dl += f.Size
	}
	p.MeterBytes(ctx, job.UserID, job.Seed, dl)
}

func usableFiles(job *jobs.Job, files []File) []File {
	var kept []File
	for _, f := range files {
		if f.Size < minVideoFileSize {
			slog.Warn("publish: dropping undersized file", "name", f.Name, "bytes", f.Size)
			if !job.Seed {
				os.Remove(f.Path)
			}
			continue
		}
		kept = append(kept, f)
	}
	return kept
}

func (p *Publisher) Publish(ctx context.Context, job *jobs.Job, files []File) error {
	p.meterDownload(ctx, job, files)

	files = usableFiles(job, files)
	if len(files) == 0 {
		return failure.Newf("incomplete", "no valid files, likely a corrupt or partial download")
	}

	job.Status = jobs.StatusPublishing
	if !p.remote() {
		job.Node = p.node
	} else if p.target != nil {
		var total int64
		for _, f := range files {
			total += f.Size
		}
		if n := p.target(ctx, string(job.Source), total); n != "" {
			job.Node = n
		}
	}
	p.repo.Update(ctx, job)
	store, bs := p.storeFor(job.Node)

	var mfFiles []manifest.File
	var total int64
	var vidDur float64
	wroteBlob := false
	for i, f := range files {
		var mi *mediainfo.Info
		if video.IsVideo(f.Name) {
			pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			info, err := mediainfo.Probe(pctx, f.Path)
			cancel()
			if err != nil || !mediainfo.Playable(info) {
				if VerifyVideo {
					slog.Warn("publish: skipping unplayable file", "name", f.Name, "err", err)
					if !job.Seed {
						os.Remove(f.Path)
					}
					continue
				}
				slog.Warn("mediainfo probe failed", "file", f.Name, "err", err)
			} else {
				mi = info
				if info.DurationSec > vidDur {
					vidDur = info.DurationSec
				}
			}
		}
		ck, err := blob.ContentKey(f.Path, f.Size)
		if err != nil {
			return err
		}
		key := blob.StorageKey(ck)
		crc, enc, wrote, err := bs.Put(ctx, job.InfoHash, i, f.Path, ck, key, video.ContentType(f.Path), f.Size)
		if err != nil {
			return err
		}
		wroteBlob = wroteBlob || wrote
		if !job.Seed {
			os.Remove(f.Path)
		}
		total += f.Size
		mfFiles = append(mfFiles, manifest.File{FileName: f.Name, DirectURL: key, FileSize: f.Size, Crc32: crc, Enc: enc, MediaInfo: mi})
	}
	if len(mfFiles) == 0 {
		return failure.Newf("incomplete", "no playable files, likely a corrupt or partial download")
	}

	if err := p.checkRuntime(ctx, job, vidDur); err != nil {
		return err
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
	if err := store.Put(ctx, manifest.Path(job.InfoHash), bytes.NewReader(data), "application/json"); err != nil {
		return err
	}

	p.refreshCaches(ctx, job.Node, job.InfoHash, wroteBlob)
	if err := p.complete(ctx, job.Node, job.InfoHash, name, mfFiles, total); err != nil {
		return err
	}
	return nil
}

func (p *Publisher) checkRuntime(ctx context.Context, job *jobs.Job, durationSec float64) error {
	if p.rt == nil || job.IMDBID == "" || job.Season > 0 || job.Episode > 0 || durationSec <= 0 {
		return nil
	}
	expected, err := p.rt.Runtime(ctx, job.IMDBID)
	if err != nil || expected <= 0 {
		return nil
	}
	if runtimeMismatch(durationSec, expected) {
		slog.Warn("runtime gate rejected", "job", job.ID, "name", job.Name, "imdb", job.IMDBID, "got_min", int(durationSec/60), "want_min", expected)
		return failure.Newf("wrong_content", "runtime %d min doesn't match this title (~%d min), likely a mislabeled release", int(durationSec/60), expected)
	}
	return nil
}

func runtimeMismatch(durationSec float64, expectedMin int) bool {
	got := durationSec / 60
	exp := float64(expectedMin)
	return got < exp*0.7 || got > exp*1.45
}

func (p *Publisher) refreshCaches(ctx context.Context, node, infoHash string, wroteBlob bool) {
	prefix := node
	if prefix == "" {
		prefix = "box1"
	}
	dirs := []string{infoHash, prefix + "/" + infoHash}
	if wroteBlob {
		dirs = append(dirs, "blobs", prefix+"/blobs")
	}
	var wg sync.WaitGroup
	for _, c := range p.caches {
		wg.Add(1)
		go func(c *rclonerc.Client) {
			defer wg.Done()
			rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := c.VFSRefresh(rctx, dirs...); err != nil {
				slog.Warn("publish: cache refresh failed", "err", err)
			}
		}(c)
	}
	wg.Wait()
}

func (p *Publisher) CompleteFromCache(ctx context.Context, node, infoHash string) (bool, error) {
	store, _ := p.storeFor(node)
	data, err := store.GetBytes(ctx, manifest.Path(infoHash))
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
	return true, p.complete(ctx, node, infoHash, man.Name, man.Files, total)
}

func (p *Publisher) complete(ctx context.Context, node, infoHash, name string, files []manifest.File, total int64) error {
	jobFiles := make([]jobs.File, len(files))
	for i, f := range files {
		jobFiles[i] = jobs.File{Index: i, Name: f.FileName, Size: f.FileSize, Key: f.DirectURL, Enc: f.Enc, MediaInfo: f.MediaInfo}
	}
	siblings, err := p.repo.ListByInfoHash(ctx, infoHash)
	if err != nil {
		return err
	}
	for _, sib := range siblings {
		if sib.Node != node {
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
