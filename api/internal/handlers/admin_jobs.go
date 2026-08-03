package handlers

import (
	"net/http"
	"strconv"

	"github.com/torrin-app/torrin/api/internal/web"
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
	if err := s.JobsPG.Delete(r.Context(), r.PathValue("id")); err != nil {
		web.WriteError(w, 500, "job lookup failed")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "deleted"})
}
