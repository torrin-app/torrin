package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/torrin-app/torrin/api/internal/web"
)

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	plan := r.URL.Query().Get("plan")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	users, err := s.Users.AdminListUsers(r.Context(), q, plan, limit, offset)
	if err != nil {
		web.WriteError(w, 500, "internal error")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"users": users})
}

func (s *Server) adminSetPlan(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")

	var body struct {
		PlanID    string `json:"plan_id"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		web.WriteError(w, 400, "invalid json")
		return
	}
	if body.PlanID == "" {
		web.WriteError(w, 400, "plan_id required")
		return
	}

	expiresAt := time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC)
	if body.ExpiresAt != "" {
		parsed, err := time.Parse("2006-01-02", body.ExpiresAt)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, body.ExpiresAt)
		}
		if err != nil {
			web.WriteError(w, 400, "invalid expires_at format")
			return
		}
		expiresAt = parsed
	}

	if err := s.Users.UpdatePlan(r.Context(), userID, body.PlanID, "", "", "", expiresAt); err != nil {
		web.WriteError(w, 500, "internal error")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) adminBanUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&body)
	if err := s.Users.BanUser(r.Context(), r.PathValue("id"), body.Reason); err != nil {
		web.WriteError(w, 500, "internal error")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "banned"})
}

func (s *Server) adminUnbanUser(w http.ResponseWriter, r *http.Request) {
	if err := s.Users.UnbanUser(r.Context(), r.PathValue("id")); err != nil {
		web.WriteError(w, 500, "internal error")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "unbanned"})
}

func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	logs, err := s.Users.GetAuditLog(r.Context(), r.PathValue("id"), 100)
	if err != nil {
		web.WriteError(w, 500, "internal error")
		return
	}
	web.WriteJSON(w, 200, logs)
}
