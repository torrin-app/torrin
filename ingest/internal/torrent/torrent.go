package torrent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/ingest/internal/screen"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/qbit"
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/video"
)

const stalledMsg = "stalled, waiting for peers"

type Runner struct {
	qb        *qbit.Client
	repo      jobs.Repository
	pub       *publish.Publisher
	bus       *bus.Bus
	ban       screen.BanFunc
	store     *storage.Client
	downloads string
	seedOnly  bool
	node      string
	category  string
	interval  time.Duration
	inflight  sync.Map
	sem       chan struct{}
}

func NewRunner(qb *qbit.Client, repo jobs.Repository, pub *publish.Publisher, b *bus.Bus, ban screen.BanFunc, store *storage.Client, downloads string, seedOnly bool, node string) *Runner {
	category := "torrin"
	if seedOnly {
		category = "torrin-seed"
	}
	return &Runner{qb: qb, repo: repo, pub: pub, bus: b, ban: ban, store: store, downloads: downloads, seedOnly: seedOnly, node: node, category: category, interval: 15 * time.Second, sem: make(chan struct{}, 5)}
}

func (r *Runner) Hold(infoHash string) bool {
	_, busy := r.inflight.LoadOrStore(infoHash, true)
	return !busy
}

func (r *Runner) Release(infoHash string) { r.inflight.Delete(infoHash) }

func (r *Runner) Remove(infoHash string) {
	if r.qb.Login() != nil {
		return
	}
	r.inflight.Delete(infoHash)
	r.qb.Delete(infoHash)
}

func (r *Runner) Start(job *jobs.Job) error {
	if err := r.qb.Login(); err != nil {
		return err
	}
	return r.qb.AddMagnet(job.Magnet)
}

func (r *Runner) Run(ctx context.Context) {
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.poll(ctx)
		}
	}
}

func (r *Runner) poll(ctx context.Context) {
	if r.qb.Login() != nil {
		return
	}
	active, _ := r.repo.ListByStatus(ctx, jobs.StatusDownloading)
	for _, job := range active {
		if job.Node != r.node || job.Source != jobs.SourceTorrent || job.InfoHash == "" || job.Seed != r.seedOnly {
			continue
		}
		if _, busy := r.inflight.Load(job.InfoHash); busy {
			continue
		}
		r.drive(ctx, job)
	}
}

func (r *Runner) drive(ctx context.Context, job *jobs.Job) {
	hash := job.InfoHash
	t, err := r.qb.GetTorrent(hash)
	if err != nil && job.Magnet != "" {
		if v2 := extractV2Hash(job.Magnet); v2 != "" && v2 != job.InfoHash {
			if t2, e2 := r.qb.GetTorrent(v2); e2 == nil {
				t, err, hash = t2, nil, v2
			}
		}
	}
	if err != nil {
		if time.Since(job.UpdatedAt) > 30*time.Second {
			slog.Info("torrent missing from engine, re-assigning to recover", "job", job.ID, "hash", hash)
			r.bus.Publish(events.JobAssigned, events.Assigned{
				JobID: job.ID, InfoHash: job.InfoHash, Magnet: job.Magnet,
				Source: string(job.Source), MaxBytes: job.MaxBytes,
				Node: job.Node,
			})
		}
		return
	}

	if t.Size > 0 && t.Name != "" && len(job.Files) == 0 {
		if !r.onMetadata(ctx, job, hash, t) {
			return
		}
	} else if t.Name != "" && job.Name == "" {
		job.Name = t.Name
		r.repo.Update(ctx, job)
	}

	if t.DlSpeed > 0 && job.Error == stalledMsg {
		job.Error = ""
		r.repo.Update(ctx, job)
	}

	if qbit.IsError(t) {
		r.failDelete(ctx, job, hash, t, "torrent error: "+t.State)
		return
	}
	if qbit.IsQueued(t) {
		return
	}

	r.repo.SetProgress(ctx, job.ID, t.Progress*100, t.DlSpeed)

	if r.recoverStall(ctx, job, hash, t) {
		return
	}

	if qbit.IsComplete(t) {
		r.finalize(ctx, job, hash, t)
	}
}

func (r *Runner) onMetadata(ctx context.Context, job *jobs.Job, hash string, t *qbit.Torrent) bool {
	job.Name = t.Name
	if job.MaxBytes > 0 && t.Size > job.MaxBytes {
		r.failDelete(ctx, job, hash, t,
			fmt.Sprintf("torrent size %dGB exceeds your plan limit of %dGB", t.Size/1e9, job.MaxBytes/1e9))
		return false
	}
	if files, err := r.qb.GetFiles(hash); err == nil && len(files) > 0 {
		job.Files = make([]jobs.File, len(files))
		var skip []int
		for i, f := range files {
			job.Files[i] = jobs.File{Index: f.Index, Name: f.Name, Size: f.Size}
			if !video.IsVideo(f.Name) {
				skip = append(skip, f.Index)
			}
		}
		if !job.Seed && len(skip) > 0 && len(skip) < len(files) {
			r.qb.SetFilePriority(hash, skip, 0)
		}
	}
	r.qb.Resume(hash)
	job.FileSize = t.Size
	r.repo.Update(ctx, job)
	slog.Info("torrent metadata ok, downloading", "job", job.ID, "name", t.Name, "size_gb", t.Size/1e9)
	return true
}

func (r *Runner) recoverStall(ctx context.Context, job *jobs.Job, hash string, t *qbit.Torrent) bool {
	stalledFor := time.Since(job.UpdatedAt)
	stuck := qbit.IsStalled(t) || (t.DlSpeed == 0 && t.Progress > 0.95 && t.Progress < 1.0)
	if stuck {
		switch {
		case stalledFor > 4*time.Hour:
			r.failDelete(ctx, job, hash, t, "torrent stalled, no peers available")
			return true
		case stalledFor > 2*time.Hour && job.Error != "restarting stalled torrent":
			r.qb.Pause(hash)
			time.Sleep(2 * time.Second)
			r.qb.Resume(hash)
			job.Error = "restarting stalled torrent"
			r.repo.Update(ctx, job)
		case stalledFor > 1*time.Hour:
			if job.Error != stalledMsg && job.Error != "restarting stalled torrent" {
				job.Error = stalledMsg
				r.repo.Update(ctx, job)
			}
			r.qb.Reannounce(hash)
		case stalledFor > 5*time.Minute:
			r.qb.Reannounce(hash)
		}
	}

	if qbit.IsFetchingMetadata(t) {
		if stalledFor > 15*time.Minute {
			r.failDelete(ctx, job, hash, t, "could not find torrent metadata, invalid or dead magnet")
			return true
		} else if stalledFor > 5*time.Minute {
			r.qb.Reannounce(hash)
		}
	}
	return false
}
