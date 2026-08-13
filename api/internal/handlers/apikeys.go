package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
)

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Users.ListAPIKeys(r.Context(), middleware.GetUser(r).ID)
	if err != nil {
		web.WriteError(w, 500, "could not load your keys")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"keys": keys})
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var req struct {
		Label        string `json:"label"`
		LoginAllowed bool   `json:"login_allowed"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	label := strings.TrimSpace(req.Label)
	if len(label) > 64 {
		label = label[:64]
	}
	k, err := s.Users.CreateAPIKey(r.Context(), user.ID, label, req.LoginAllowed)
	if err != nil {
		web.WriteError(w, 422, err.Error())
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "api_key_created", "label="+label, clientIP(r))
	web.WriteJSON(w, 201, k)
}

func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if err := s.Users.RevokeAPIKey(r.Context(), user.ID, r.PathValue("id")); err != nil {
		web.WriteError(w, 404, "key not found")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "api_key_revoked", "id="+r.PathValue("id"), clientIP(r))
	web.WriteJSON(w, 200, map[string]string{"status": "revoked"})
}
