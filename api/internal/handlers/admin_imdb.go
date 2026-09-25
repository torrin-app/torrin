package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/torrin-app/torrin/api/internal/web"
)

func (s *Server) adminIMDBCorrections(w http.ResponseWriter, r *http.Request) {
	list, err := s.JobsPG.ListIMDBCorrections(r.Context(), "pending", 200)
	if err != nil {
		web.WriteError(w, 500, "could not load corrections")
		return
	}
	web.WriteJSON(w, 200, list)
}

func (s *Server) adminResolveIMDB(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Approve bool `json:"approve"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req) != nil {
		web.WriteError(w, 400, "bad request")
		return
	}
	hash, imdb, err := s.JobsPG.ResolveIMDBCorrection(r.Context(), r.PathValue("id"), req.Approve)
	if err != nil {
		web.WriteError(w, 404, "correction not found or already resolved")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"info_hash": hash, "imdb_id": imdb, "approved": req.Approve})
}
