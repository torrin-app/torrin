package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
)

func validIMDB(s string) bool {
	if !strings.HasPrefix(s, "tt") {
		return false
	}
	d := s[2:]
	if len(d) < 5 || len(d) > 9 {
		return false
	}
	for _, c := range d {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (s *Server) reportIMDB(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	job, err := s.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job == nil || job.UserID != user.ID {
		web.WriteError(w, 404, "job not found")
		return
	}
	var req struct {
		IMDBID string `json:"imdb_id"`
		Reason string `json:"reason"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		web.WriteError(w, 400, "bad request")
		return
	}
	imdb := strings.TrimSpace(req.IMDBID)
	if !validIMDB(imdb) {
		web.WriteError(w, 400, "invalid imdb id, expected ttNNNNNNN")
		return
	}
	if job.InfoHash == "" {
		web.WriteError(w, 422, "this job has no content to correct")
		return
	}
	note := strings.TrimSpace(req.Reason)
	if len(note) > 500 {
		note = note[:500]
	}
	if err := s.JobsPG.CreateIMDBCorrection(r.Context(), user.ID, job.InfoHash, imdb, note); err != nil {
		web.WriteError(w, 500, "could not save your report")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"ok": true})
}
