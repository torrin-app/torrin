package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
)

func (s *Server) webdavOverridesList(w http.ResponseWriter, r *http.Request) {
	user, _ := r.Context().Value(middleware.UserContextKey).(*auth.User)
	if user == nil {
		web.WriteError(w, 401, "login required")
		return
	}
	list, err := s.Users.ListWebdavOverrides(r.Context(), user.ID)
	if err != nil {
		web.WriteError(w, 500, "could not load your webdav overrides")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"overrides": list})
}

func (s *Server) webdavOverrideSet(w http.ResponseWriter, r *http.Request) {
	user, _ := r.Context().Value(middleware.UserContextKey).(*auth.User)
	if user == nil {
		web.WriteError(w, 401, "login required")
		return
	}
	var req struct {
		InfoHash  string `json:"info_hash"`
		FileIndex int    `json:"file_index"`
		Alias     string `json:"alias"`
		Excluded  bool   `json:"excluded"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		web.WriteError(w, 400, "invalid json")
		return
	}
	if req.InfoHash == "" {
		web.WriteError(w, 400, "info_hash required")
		return
	}
	if len(req.Alias) > 255 {
		web.WriteError(w, 422, "name is too long")
		return
	}
	if err := s.Users.SetWebdavOverride(r.Context(), user.ID, req.InfoHash, req.FileIndex, req.Alias, req.Excluded); err != nil {
		web.WriteError(w, 500, "could not save that change")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}
