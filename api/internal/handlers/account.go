package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
)

func (s *Server) registerAccountRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	mux.Handle("GET /api/referral", authMW(http.HandlerFunc(s.referral)))
	mux.Handle("POST /api/me/pause", authMW(http.HandlerFunc(s.pauseSub)))
	mux.Handle("POST /api/me/resume", authMW(http.HandlerFunc(s.resumeSub)))
	mux.Handle("PUT /api/me", authMW(http.HandlerFunc(s.updateEmail)))
	mux.Handle("DELETE /api/me", authMW(http.HandlerFunc(s.deleteAccount)))
	mux.Handle("POST /api/auth/regenerate-key", authMW(http.HandlerFunc(s.regenerateKey)))
	mux.Handle("GET /api/me/audit", authMW(http.HandlerFunc(s.auditLog)))
}

func (s *Server) referral(w http.ResponseWriter, r *http.Request) {
	u := middleware.GetUser(r)
	code := s.Users.GetReferralCode(r.Context(), u.ID)
	web.WriteJSON(w, 200, map[string]any{
		"code":              code,
		"total":             s.Users.GetTotalReferralCount(r.Context(), u.ID),
		"paying":            s.Users.GetPayingReferralCount(r.Context(), u.ID),
		"credit_days":       s.Users.GetReferralCredit(r.Context(), u.ID),
		"days_per_referral": auth.ReferralDaysPerPayment,
		"max_days":          auth.ReferralMaxDays,
		"referral_link":     "https://torrin.app/login?ref=" + code,
	})
}

func (s *Server) pauseSub(w http.ResponseWriter, r *http.Request) {
	remaining, err := s.Users.PauseSub(r.Context(), middleware.GetUser(r).ID)
	if err != nil {
		web.WriteError(w, 400, err.Error())
		return
	}
	web.WriteJSON(w, 200, map[string]any{"paused": true, "remaining_days": remaining})
}

func (s *Server) resumeSub(w http.ResponseWriter, r *http.Request) {
	newExpiry, err := s.Users.ResumeSub(r.Context(), middleware.GetUser(r).ID)
	if err != nil {
		web.WriteError(w, 400, err.Error())
		return
	}
	web.WriteJSON(w, 200, map[string]any{"paused": false, "expires_at": newExpiry})
}

func (s *Server) updateEmail(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var req struct {
		Email string `json:"email"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Email == "" {
		web.WriteError(w, 400, "email required")
		return
	}
	if err := s.Users.UpdateEmail(r.Context(), user.ID, req.Email); err != nil {
		web.WriteError(w, 400, err.Error())
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "email_changed", "", clientIP(r))
	web.WriteJSON(w, 200, map[string]any{"email": req.Email})
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	userJobs, _ := s.Jobs.ListByUser(r.Context(), user.ID, 1000)
	for _, j := range userJobs {
		qb := s.Qbit
		if j.Seed {
			qb = s.QbitSeed
		}
		if j.Status.Active() && j.Source == jobs.SourceTorrent && qb != nil && qb.Login() == nil {
			qb.Delete(j.InfoHash)
		}
		s.Jobs.Delete(r.Context(), j.ID)
	}
	s.Users.AuditLog(r.Context(), user.ID, "account_deleted", "", clientIP(r))
	s.Users.DeleteUser(r.Context(), user.ID)
	slog.Info("account deleted", "user", user.ID)
	web.WriteJSON(w, 200, map[string]any{"status": "account deleted"})
}

func (s *Server) regenerateKey(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	newKey, err := s.Users.RegenerateAPIKey(r.Context(), user.ID)
	if err != nil {
		web.WriteError(w, 500, "failed to regenerate key")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "key_regenerated", "", clientIP(r))
	web.WriteJSON(w, 200, map[string]any{"api_key": newKey})
}

func (s *Server) auditLog(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.Users.GetAuditLog(r.Context(), middleware.GetUser(r).ID, 50)
	web.WriteJSON(w, 200, map[string]any{"events": entries})
}
