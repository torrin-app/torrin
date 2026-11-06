package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/torrin-app/torrin/api/internal/web"
)

func (s *Server) adminGetAllowlist(w http.ResponseWriter, r *http.Request) {
	list, err := s.Users.ListAllowlistHashes(r.Context())
	if err != nil {
		web.WriteError(w, 500, "allowlist operation failed")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"hashes": list})
}

func (s *Server) adminAddAllowlist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hash string `json:"hash"`
		Note string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&body); err != nil || strings.TrimSpace(body.Hash) == "" {
		web.WriteError(w, 400, "hash required")
		return
	}
	if err := s.Users.AddAllowlistHash(r.Context(), strings.TrimSpace(body.Hash), body.Note); err != nil {
		web.WriteError(w, 500, "allowlist operation failed")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) adminDelAllowlist(w http.ResponseWriter, r *http.Request) {
	if err := s.Users.RemoveAllowlistHash(r.Context(), r.PathValue("hash")); err != nil {
		web.WriteError(w, 500, "allowlist operation failed")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}
