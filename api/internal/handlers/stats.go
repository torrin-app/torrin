package handlers

import (
	"net/http"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
)

func (s *Server) registerStatsRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	mux.HandleFunc("GET /api/stats/public", s.statsPublic)
	mux.Handle("GET /api/stats", authMW(http.HandlerFunc(s.stats)))
	mux.Handle("GET /api/history", authMW(http.HandlerFunc(s.history)))
	mux.Handle("GET /api/wrapped", authMW(http.HandlerFunc(s.wrapped)))
	mux.HandleFunc("GET /api/wrapped/platform", s.wrappedPlatform)
}

func (s *Server) statsPublic(w http.ResponseWriter, r *http.Request) {
	history, err := s.JobsPG.GetMetricsHistory(r.Context(), 90)
	if err != nil {
		web.WriteError(w, 500, "could not load stats")
		return
	}
	current := s.JobsPG.CurrentMetrics(r.Context(), s.Users.CountUsers(r.Context()))
	web.WriteJSON(w, 200, map[string]any{"current": current, "history": history})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)
	st, _ := s.JobsPG.GetUserStats(r.Context(), user.ID)
	web.WriteJSON(w, 200, map[string]any{
		"stats": st,
		"plan":  plan,
		"limits": map[string]any{
			"max_concurrent":    plan.MaxConcurrent,
			"max_torrent_bytes": plan.MaxTorrentBytes,
			"active_slots":      s.Slots.ActiveSlots(r.Context(), user.ID),
		},
	})
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	history, _ := s.JobsPG.GetUserHistory(r.Context(), middleware.GetUser(r).ID, 100)
	web.WriteJSON(w, 200, map[string]any{"history": history})
}

func (s *Server) wrapped(w http.ResponseWriter, r *http.Request) {
	if !wrappedAvailable(time.Now()) {
		web.WriteError(w, 403, "wrapped drops at the end of the month")
		return
	}
	user := middleware.GetUser(r)
	wr, _ := s.JobsPG.GetUserWrapped(r.Context(), user.ID)
	wr.MemberSince = user.CreatedAt.Format("2006-01-02")
	web.WriteJSON(w, 200, wr)
}

func (s *Server) wrappedPlatform(w http.ResponseWriter, r *http.Request) {
	if !wrappedAvailable(time.Now()) {
		web.WriteError(w, 403, "wrapped drops at the end of the month")
		return
	}
	wr, _ := s.JobsPG.GetPlatformWrapped(r.Context())
	web.WriteJSON(w, 200, wr)
}

func wrappedAvailable(now time.Time) bool {
	lastDay := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Day()
	return now.Day() >= lastDay-6
}
