package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/plans"
)

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Ref   string `json:"ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		web.WriteError(w, 400, "email required")
		return
	}
	req.Email = auth.NormalizeEmail(req.Email)
	if req.Ref == "" {
		req.Ref = r.URL.Query().Get("ref")
	}
	if req.Ref == "" {
		if c, err := r.Cookie("torrin_ref"); err == nil {
			req.Ref = c.Value
		}
	}
	if existing, err := s.Users.GetByEmail(r.Context(), req.Email); err == nil && existing != nil {
		web.WriteError(w, 409, "account already exists - use your API key to log in")
		return
	}
	if limit := int(env.Int("FREE_SIGNUPS_PER_IP", 5)); limit > 0 {
		if ip := clientIP(r); ip != "" && s.Users.SignupsFromIP(r.Context(), ip, time.Now().Add(-30*24*time.Hour)) >= limit {
			web.WriteError(w, 429, "too many accounts from your network, contact support if this is a mistake")
			return
		}
	}
	user, err := s.Users.CreateUser(r.Context(), req.Email, req.Ref)
	if err != nil {
		msg := err.Error()
		if msg == "disposable email addresses are not allowed" || msg == "account previously deleted" {
			web.WriteError(w, 400, msg)
		} else {
			web.WriteError(w, 500, "failed to create account")
		}
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "account_created", "email="+req.Email, clientIP(r))
	if s.Mailer != nil {
		go s.Mailer.SendWelcome(user.Email, user.APIKey)
	}
	web.WriteJSON(w, 201, map[string]any{"api_key": user.APIKey, "email": user.Email, "plan": user.PlanID})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)
	web.WriteJSON(w, 200, map[string]any{
		"user_id": user.ID, "email": user.Email, "plan": plan,
		"active_slots": s.Slots.ActiveSlots(r.Context(), user.ID), "expires_at": user.ExpiresAt,
		"paused": user.IsPaused(), "remaining_days": user.RemainingDays,
		"pause_count": user.PauseCount, "max_pauses": 3,
		"recurrence": user.Recurrence,
	})
}

func (s *Server) plans(w http.ResponseWriter, _ *http.Request) {
	web.WriteJSON(w, 200, plans.All)
}
