package handlers

import (
	"context"
	"net/http"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/qbit"
)

func (s *Server) progressJob(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	job, err := s.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job == nil || job.UserID != user.ID {
		web.WriteError(w, 404, "job not found")
		return
	}
	resp := map[string]any{"status": job.Status, "name": job.Name}
	switch {
	case job.Status == jobs.StatusDownloading && job.Source == jobs.SourceTorrent:
		fromQbit := false
		if s.Qbit != nil && s.Qbit.Login() == nil {
			if t, err := s.Qbit.GetTorrent(job.InfoHash); err == nil {
				resp["progress"] = t.Progress * 100
				resp["speed"] = t.DlSpeed
				resp["state"] = torrentState(t)
				fromQbit = true
			}
		}
		if !fromQbit {
			p, sp, st := s.liveProgress(r.Context(), job)
			resp["progress"] = p
			resp["speed"] = sp
			resp["status"] = st
		}
	case job.Status == jobs.StatusDownloading && (job.Source == jobs.SourceUsenet || job.Source == jobs.SourceHDEncode || job.Source == jobs.SourceScenerls || job.Source == jobs.SourceHoster || job.Source == jobs.SourceYtdlp):
		p, sp, st := s.liveProgress(r.Context(), job)
		resp["progress"] = p
		resp["speed"] = sp
		resp["status"] = st
	case job.Status == jobs.StatusProcessing || job.Status == jobs.StatusPublishing:
		resp["progress"] = job.Progress
	}
	web.WriteJSON(w, 200, resp)
}

func torrentState(t *qbit.Torrent) string {
	switch {
	case qbit.IsFetchingMetadata(t):
		return "fetching metadata"
	case qbit.IsStalled(t):
		return "stalled"
	default:
		return "downloading"
	}
}

func (s *Server) liveProgress(ctx context.Context, job *jobs.Job) (float64, int64, jobs.Status) {
	if job.Progress > 0 {
		return job.Progress, job.Speed, job.Status
	}
	sibs, err := s.Jobs.ListByInfoHash(ctx, job.InfoHash)
	if err != nil {
		return job.Progress, job.Speed, job.Status
	}
	for _, sib := range sibs {
		if sib.ID != job.ID && sib.Status.Active() && sib.Progress > 0 {
			return sib.Progress, sib.Speed, sib.Status
		}
	}
	return job.Progress, job.Speed, job.Status
}
