package torrent

import (
	"context"
	"log/slog"
	"strings"

	"github.com/torrin-app/torrin/shared/jobs"
)

func (r *Runner) ReapOrphans(ctx context.Context) {
	if r.qb.Login() != nil {
		return
	}
	torrents, err := r.qb.ListTorrents(r.category)
	if err != nil || len(torrents) == 0 {
		return
	}
	keep := r.activeHashes(ctx)
	for i := range torrents {
		t := &torrents[i]
		h := strings.ToLower(t.Hash)
		if h == "" || keep[h] {
			continue
		}
		if _, busy := r.inflight.Load(t.Hash); busy {
			continue
		}
		slog.Info("reaping orphan torrent", "hash", h, "name", t.Name, "state", t.State)
		r.qb.Delete(t.Hash)
	}
}

func (r *Runner) activeHashes(ctx context.Context) map[string]bool {
	keep := map[string]bool{}
	for _, st := range []jobs.Status{jobs.StatusPending, jobs.StatusQueued, jobs.StatusDownloading, jobs.StatusProcessing, jobs.StatusPublishing, jobs.StatusSeeding} {
		list, _ := r.repo.ListByStatus(ctx, st)
		for _, j := range list {
			if !r.mine(j.Node) {
				continue
			}
			if j.InfoHash != "" {
				keep[strings.ToLower(j.InfoHash)] = true
			}
			if v2 := extractV2Hash(j.Magnet); v2 != "" {
				keep[strings.ToLower(v2)] = true
			}
		}
	}
	return keep
}
