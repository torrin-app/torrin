package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
)

func (s *Server) adminListPartners(w http.ResponseWriter, r *http.Request) {
	list, err := s.Users.ListResellerPartners(r.Context())
	if err != nil {
		web.WriteError(w, 500, "partner operation failed")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"partners": list})
}

// adminCreatePartner mints a reseller partner and returns its key ONCE. The key
// doubles as the partner's X-Partner-Key (account creation) and X-Reseller-Key
// (code mint/list/settle, scoped to this partner's own codes). Only the hash is
// stored.
func (s *Server) adminCreatePartner(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		SuppressWelcome bool   `json:"suppress_welcome"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		web.WriteError(w, 400, "id required")
		return
	}
	key := "trn_rsl_" + randCode(40)
	if err := s.Users.CreateResellerPartner(r.Context(), strings.TrimSpace(body.ID), body.Name, auth.HashPartnerKey(key), body.SuppressWelcome); err != nil {
		web.WriteError(w, 500, "could not create partner (id may already exist)")
		return
	}
	web.WriteJSON(w, 200, map[string]any{
		"id":   strings.TrimSpace(body.ID),
		"key":  key,
		"note": "store this key now; it is hashed and cannot be shown again",
	})
}

func (s *Server) adminRevokePartner(w http.ResponseWriter, r *http.Request) {
	if err := s.Users.SetResellerPartnerActive(r.Context(), r.PathValue("id"), false); err != nil {
		web.WriteError(w, 500, "partner operation failed")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}
