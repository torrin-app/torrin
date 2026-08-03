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
