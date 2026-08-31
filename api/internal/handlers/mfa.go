package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
)

const sessionTTL = 30 * 24 * time.Hour

func (s *Server) mfaEnroll(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user.TOTPEnabled {
		web.WriteError(w, 409, "two-factor is already enabled")
		return
	}
	secret, uri, err := s.Users.EnrollTOTP(r.Context(), user.ID, user.Email)
	if err != nil {
		web.WriteError(w, 500, "could not start two-factor setup")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"secret": secret, "otpauth_uri": uri})
}

func (s *Server) mfaConfirm(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	backup, err := s.Users.ConfirmTOTP(r.Context(), user.ID, mfaCode(r))
	if err != nil {
		web.WriteError(w, 400, "invalid code, check your authenticator app")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "2fa_enabled", "", clientIP(r))
	web.WriteJSON(w, 200, map[string]any{"backup_codes": backup, "session": s.issueSession(w, user.ID)})
}

func (s *Server) mfaVerify(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !s.Users.VerifyTOTP(r.Context(), user.ID, mfaCode(r)) {
		web.WriteError(w, 401, "invalid code")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"session": s.issueSession(w, user.ID)})
}

func (s *Server) issueSession(w http.ResponseWriter, userID string) string {
	token := auth.SignSession(s.SignKey, userID, sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     "tr_session",
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	return token
}

func (s *Server) mfaDisable(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !s.Users.VerifyTOTP(r.Context(), user.ID, mfaCode(r)) {
		web.WriteError(w, 401, "invalid code")
		return
	}
	if err := s.Users.DisableTOTP(r.Context(), user.ID); err != nil {
		web.WriteError(w, 500, "could not disable two-factor")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "2fa_disabled", "", clientIP(r))
	web.WriteJSON(w, 200, map[string]string{"status": "disabled"})
}

func mfaCode(r *http.Request) string {
	var req struct {
		Code string `json:"code"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	return strings.TrimSpace(req.Code)
}
