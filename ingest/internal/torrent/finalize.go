package torrent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/ingest/internal/screen"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/qbit"
	"github.com/torrin-app/torrin/shared/video"
)

func extractV2Hash(magnet string) string {
	for _, part := range strings.Split(magnet, "&") {
		if strings.HasPrefix(part, "xt=urn:btmh:") {
			mh := strings.ToLower(strings.TrimPrefix(part, "xt=urn:btmh:"))
			if strings.HasPrefix(mh, "1220") && len(mh) >= 44 {
				return mh[4:44]
			}
		}
	}
	return ""
}

func (r *Runner) finalize(ctx context.Context, job *jobs.Job, hash string, t *qbit.Torrent) {
	if _, already := r.inflight.LoadOrStore(job.InfoHash, true); already {
		return
	}
	go func() {
		defer r.inflight.Delete(job.InfoHash)
		select {
		case r.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-r.sem }()
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic finalizing torrent", "job", job.ID, "err", rec)
				r.fail(ctx, job, "internal error during upload")
			}
		}()

		files, err := r.qb.GetFiles(hash)
		if err != nil {
			r.fail(ctx, job, "could not list torrent files")
			return
		}
		names := make([]string, len(files))
		for i, f := range files {
			names[i] = f.Name
		}
		if screen.Blocked(ctx, r.ban, job, names...) {
			r.failDelete(ctx, job, hash, t, "content blocked by safety policy")
			return
		}
		var pubFiles []publish.File
		for _, f := range files {
			if !video.IsVideo(f.Name) {
				continue
			}
			pubFiles = append(pubFiles, publish.File{
				Name: filepath.Base(f.Name),
				Path: filepath.Join(t.SavePath, f.Name),
				Size: f.Size,
			})
		}
		if len(pubFiles) == 0 {
			r.failDelete(ctx, job, hash, t, "no video files found")
			return
		}

		slog.Info("torrent complete, publishing", "job", job.ID, "name", t.Name, "files", len(pubFiles))
		if err := r.pub.Publish(ctx, job, pubFiles); err != nil {
			r.fail(ctx, job, "publish failed: "+err.Error())
			return
		}
		if job.Seed {
			job.Status = jobs.StatusSeeding
			r.repo.Update(ctx, job)
			slog.Info("published, now seeding for ratio", "job", job.ID, "name", t.Name)
			return
		}
		r.deleteAndVerify(hash, t)
		r.bus.Publish(events.JobComplete, events.Complete{JobID: job.ID, InfoHash: job.InfoHash, Node: job.Node})
	}()
}

func (r *Runner) fail(ctx context.Context, job *jobs.Job, reason string) {
	job.Status = jobs.StatusFailed
	job.Error = reason
	r.repo.Update(ctx, job)
}

func (r *Runner) failDelete(ctx context.Context, job *jobs.Job, hash string, t *qbit.Torrent, reason string) {
	r.fail(ctx, job, reason)
	r.deleteAndVerify(hash, t)
}

func (r *Runner) deleteAndVerify(hash string, t *qbit.Torrent) {
	if err := r.qb.Delete(hash); err != nil {
		slog.Error("qbit delete failed", "hash", hash, "err", err)
	}
	time.Sleep(2 * time.Second)
	if _, err := r.qb.GetTorrent(hash); err == nil {
		r.qb.Delete(hash)
		time.Sleep(1 * time.Second)
	}
	contentPath := t.ContentPath
	if contentPath == "" {
		contentPath = filepath.Join(t.SavePath, t.Name)
	}
	if _, err := os.Stat(contentPath); err == nil {
		if err := os.RemoveAll(contentPath); err != nil {
			slog.Error("manual cleanup failed", "path", contentPath, "err", err)
		}
	}
}
