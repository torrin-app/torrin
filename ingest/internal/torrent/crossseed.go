package torrent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/qbit"
	"github.com/torrin-app/torrin/shared/torrentfile"
)

type diskFile struct {
	path string
	size int64
}

func (r *Runner) CrossSeed(ctx context.Context, job *jobs.Job, done func()) {
	defer done()
	data, err := r.store.GetBytes(ctx, torrentfile.XSeedKey(job.InfoHash))
	if err != nil {
		r.fail(ctx, job, "cross-seed torrent not found")
		return
	}
	meta, err := torrentfile.Parse(data)
	if err != nil {
		r.fail(ctx, job, "invalid cross-seed torrent")
		return
	}

	src, srcFiles := r.crossSeedSource(ctx, meta)
	if src == nil {
		r.fail(ctx, job, "no matching local content to cross-seed from")
		return
	}
	if err := r.linkCrossSeed(meta, srcFiles); err != nil {
		r.cleanupLinks(meta)
		r.fail(ctx, job, "cross-seed link failed: "+err.Error())
		return
	}

	if err := r.qb.Login(); err != nil {
		r.fail(ctx, job, "torrent engine unavailable")
		return
	}
	if err := r.qb.AddTorrentFile(data, "torrin-seed"); err != nil {
		r.cleanupLinks(meta)
		r.fail(ctx, job, "cross-seed inject failed")
		return
	}
	if !r.verifyCrossSeed(ctx, job.InfoHash) {
		r.qb.Delete(job.InfoHash)
		r.cleanupLinks(meta)
		r.fail(ctx, job, "cross-seed recheck failed, files did not match")
		return
	}

	job.Status = jobs.StatusSeeding
	r.repo.Update(ctx, job)
	slog.Info("cross-seed injected, seeding", "job", job.ID, "source", src.InfoHash, "name", meta.Name)
}

func (r *Runner) crossSeedSource(ctx context.Context, meta *torrentfile.Meta) (*jobs.Job, []diskFile) {
	want := meta.Sizes()
	seeds, _ := r.repo.ListByStatus(ctx, jobs.StatusSeeding)
	for _, s := range seeds {
		if s.Node != r.node || s.InfoHash == "" || s.InfoHash == meta.InfoHash || !torrentfile.SubsetBySize(want, s.FileSizes()) {
			continue
		}
		if files := r.diskFiles(s.InfoHash); len(files) > 0 {
			return s, files
		}
	}
	return nil, nil
}

func (r *Runner) diskFiles(hash string) []diskFile {
	t, err := r.qb.GetTorrent(hash)
	if err != nil {
		return nil
	}
	files, err := r.qb.GetFiles(hash)
	if err != nil {
		return nil
	}
	out := make([]diskFile, 0, len(files))
	for _, f := range files {
		out = append(out, diskFile{path: filepath.Join(t.SavePath, f.Name), size: f.Size})
	}
	return out
}

func (r *Runner) linkCrossSeed(meta *torrentfile.Meta, src []diskFile) error {
	used := make([]bool, len(src))
	for _, bf := range meta.Files {
		si := -1
		for i, sf := range src {
			if !used[i] && sf.size == bf.Size {
				si = i
				break
			}
		}
		if si < 0 {
			return fmt.Errorf("no source file for %s (%d bytes)", bf.Path, bf.Size)
		}
		used[si] = true
		dst := filepath.Join(r.downloads, bf.Path)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		os.Remove(dst)
		if err := os.Link(src[si].path, dst); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) cleanupLinks(meta *torrentfile.Meta) {
	if meta.Name == "" {
		return
	}
	os.RemoveAll(filepath.Join(r.downloads, meta.Name))
}

func (r *Runner) verifyCrossSeed(ctx context.Context, hash string) bool {
	for i := 0; i < 20; i++ {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(3 * time.Second):
		}
		t, err := r.qb.GetTorrent(hash)
		if err != nil {
			continue
		}
		if qbit.IsError(t) {
			return false
		}
		if t.Progress >= 0.999 {
			return true
		}
	}
	return false
}
