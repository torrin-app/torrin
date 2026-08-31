package handlers

import (
	"net/http"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
)

func (s *Server) debridUsage(w http.ResponseWriter, r *http.Request) {
	usage, err := s.Users.GetDebridUsage(r.Context(), middleware.GetUser(r).ID)
	if err != nil {
		web.WriteError(w, 500, "could not load usage")
		return
	}
	if usage == nil {
		usage = []auth.DebridUsage{}
	}
	web.WriteJSON(w, 200, usage)
}

func (s *Server) ingestUsage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)
	used, err := s.Users.MonthlyIngestBytes(r.Context(), user.ID)
	if err != nil {
		web.WriteError(w, 500, "could not load usage")
		return
	}
	web.WriteJSON(w, 200, map[string]any{
		"used_bytes": used,
		"cap_bytes":  plan.MonthlyIngestBytes,
	})
}
