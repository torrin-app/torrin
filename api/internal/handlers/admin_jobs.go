package handlers

import (
	"net/http"
	"strconv"

	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
)

func (s *Server) adminJobs(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	jobList, err := s.JobsPG.AdminListJobs(r.Context(), status, q, limit, offset)
	if err != nil {
		web.WriteError(w, 500, "job lookup failed")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"jobs": jobList})
}

func (s *Server) adminDeleteJob(w http.ResponseWriter, r *http.Request) {
	job, _ := s.JobsPG.Get(r.Context(), r.PathValue("id"))
	if err := s.JobsPG.Delete(r.Context(), r.PathValue("id")); err != nil {
		web.WriteError(w, 500, "job lookup failed")
		return
	}
	if job != nil && job.InputKey != "" {
		s.Store.Delete(r.Context(), job.InputKey)
	}
	if job != nil && job.Status.Active() && job.Status != jobs.StatusQueued {
		if job.Seed && job.Source == jobs.SourceTorrent && s.QbitSeed != nil && s.QbitSeed.Login() == nil {
			s.QbitSeed.Delete(job.InfoHash)
		} else if !job.Seed {
			s.Bus.Publish(events.JobDeleted, events.Deleted{
				JobID: job.ID, InfoHash: job.InfoHash, Source: string(job.Source),
				Node: job.Node, UserID: job.UserID,
			})
		}
	}
	s.Slots.Wake()
	web.WriteJSON(w, 200, map[string]string{"status": "deleted"})
}
