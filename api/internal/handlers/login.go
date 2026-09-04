package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
)

const (
	loginChallengeTTL = 10 * time.Minute
	loginWindow       = 15 * time.Minute
	loginMaxHits      = 10
)

type loginLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

var loginLim = &loginLimiter{hits: map[string][]time.Time{}}

func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-loginWindow)
	kept := l.hits[ip][:0]
	for _, t := range l.hits[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= loginMaxHits {
		l.hits[ip] = kept
		return false
	}
	l.hits[ip] = append(kept, time.Now())
	return true
}

func (s *Server) loginPassword(w http.ResponseWriter, r *http.Request) {
	if !loginLim.allow(clientIP(r)) {
		web.WriteError(w, 429, "too many attempts, try again later")
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		web.WriteError(w, 400, "email and password required")
		return
	}
	user, err := s.Users.CheckPassword(r.Context(), req.Email, req.Password)
	if err != nil {
		web.WriteError(w, 401, "invalid email or password")
		return
	}
	s.startLoginChallenge(w, r, user)
}

func (s *Server) loginKey(w http.ResponseWriter, r *http.Request) {
	if !loginLim.allow(clientIP(r)) {
		web.WriteError(w, 429, "too many attempts, try again later")
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Key == "" {
		web.WriteError(w, 400, "key required")
		return
	}
	user, loginAllowed, err := s.Users.ResolveAPIKey(r.Context(), strings.TrimSpace(req.Key))
	if err != nil || user == nil || !loginAllowed {
		web.WriteError(w, 401, "invalid key")
		return
	}
	s.startLoginChallenge(w, r, user)
}

func (s *Server) startLoginChallenge(w http.ResponseWriter, r *http.Request, user *auth.User) {
	if user.Banned {
		web.WriteError(w, 403, "account suspended")
		return
	}
	if user.TOTPEnabled {
		web.WriteJSON(w, 200, map[string]string{"factor": "totp", "challenge": auth.SignChallenge(s.SignKey, user.ID, "totp", loginChallengeTTL)})
		return
	}
	code, err := s.Users.NewLoginOTP(r.Context(), user.ID)
	if err != nil {
		web.WriteError(w, 500, "could not start login")
		return
	}
	if s.Mailer != nil {
		go s.Mailer.Send(user.Email, "Your Torrin login code", otpEmailHTML(code))
	}
	web.WriteJSON(w, 200, map[string]string{"factor": "email", "challenge": auth.SignChallenge(s.SignKey, user.ID, "email", loginChallengeTTL)})
}

func (s *Server) loginVerify(w http.ResponseWriter, r *http.Request) {
	if !loginLim.allow(clientIP(r)) {
		web.WriteError(w, 429, "too many attempts, try again later")
		return
	}
	var req struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		web.WriteError(w, 400, "challenge and code required")
		return
	}
	uid, factor, ok := auth.VerifyChallenge(s.SignKey, req.Challenge)
	if !ok {
		web.WriteError(w, 401, "login expired, start again")
		return
	}
	code := strings.TrimSpace(req.Code)
	valid := false
	switch factor {
	case "totp":
		valid = s.Users.VerifyTOTP(r.Context(), uid, code)
	case "email":
		valid = s.Users.CheckLoginOTP(r.Context(), uid, code)
	}
	if !valid {
		web.WriteError(w, 401, "invalid code")
		return
	}
	s.Users.AuditLog(r.Context(), uid, "login", factor, clientIP(r))
	web.WriteJSON(w, 200, map[string]string{"session": s.issueSession(w, uid)})
}

func (s *Server) setPassword(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var req struct {
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		web.WriteError(w, 400, "password required")
		return
	}
	if err := s.Users.SetPassword(r.Context(), user.ID, req.Password); err != nil {
		web.WriteError(w, 400, err.Error())
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "password_set", "", clientIP(r))
	web.WriteJSON(w, 200, map[string]string{"status": "set"})
}

func otpEmailHTML(code string) string {
	return fmt.Sprintf(`<div style="font-family:sans-serif;max-width:420px;margin:0 auto;padding:24px;text-align:center">
<p style="color:#666">Your Torrin login code:</p>
<p style="font-size:32px;font-weight:700;letter-spacing:6px;margin:16px 0">%s</p>
<p style="color:#999;font-size:13px">It expires in 10 minutes. If you didn't try to log in, ignore this email.</p>
</div>`, code)
}
