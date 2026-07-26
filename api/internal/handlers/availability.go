package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/georoute"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
)

func (s *Server) registerAvailabilityRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	mux.Handle("POST /api/jobs/{id}/sign", authMW(http.HandlerFunc(s.signJob)))
	mux.Handle("GET /api/availability/{hash}", authMW(http.HandlerFunc(s.availabilityOne)))
	mux.Handle("POST /api/availability", authMW(http.HandlerFunc(s.availabilityBatch)))
}

func (s *Server) signJob(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	job, err := s.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job == nil || job.UserID != user.ID {
		web.WriteError(w, 404, "job not found")
		return
	}
	if job.Status != jobs.StatusComplete {
		web.WriteError(w, 400, "job not complete")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"job_id": job.ID, "streams": s.signStreams(job, r)})
}

func (s *Server) availabilityOne(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	files, ok := s.cachedFiles(r, hash)
	if !ok {
		web.WriteJSON(w, 200, map[string]any{"available": false, "info_hash": hash})
		return
	}
	web.WriteJSON(w, 200, map[string]any{"available": true, "info_hash": hash, "files": files})
}

func (s *Server) availabilityBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hashes []string `json:"hashes"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Hashes) == 0 {
		web.WriteError(w, 400, "hashes required")
		return
	}
	if len(req.Hashes) > 100 {
		web.WriteError(w, 400, "max 100 hashes")
		return
	}
	out := make(map[string]any, len(req.Hashes))
	for _, h := range req.Hashes {
		files, ok := s.cachedFiles(r, h)
		out[h] = map[string]any{"available": ok, "files": files}
	}
	web.WriteJSON(w, 200, out)
}

func (s *Server) cachedFiles(r *http.Request, infoHash string) ([]map[string]any, bool) {
	data, err := s.Store.GetBytes(r.Context(), manifest.Path(infoHash))
	if err != nil {
		return nil, false
	}
	m, err := manifest.Parse(data)
	if err != nil || len(m.Files) == 0 {
		return nil, false
	}
	out := make([]map[string]any, len(m.Files))
	for i, f := range m.Files {
		out[i] = map[string]any{
			"file_name": f.FileName, "size": f.FileSize,
			"url": georoute.URL(r, s.Store.SignURL(manifest.Key(infoHash, i, f.FileName), 24*time.Hour)),
		}
	}
	return out, true
}
