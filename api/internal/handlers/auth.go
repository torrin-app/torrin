package handlers

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/plans"
)

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email     string `json:"email"`
		Ref       string `json:"ref"`
		Turnstile string `json:"turnstile"`
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
	trusted := trustedPartner(r.Header.Get("X-Partner-Key"), s.PartnerKey)
	if !trusted && s.TurnstileSecret != "" && !verifyTurnstile(r.Context(), s.TurnstileSecret, req.Turnstile, clientIP(r)) {
		web.WriteError(w, 403, "captcha verification failed, please retry")
		return
	}
	if err := auth.ValidateSignupEmail(r.Context(), req.Email); err != nil {
		web.WriteError(w, 400, err.Error())
		return
	}
	if existing, err := s.Users.GetByEmail(r.Context(), req.Email); err == nil && existing != nil {
		web.WriteError(w, 409, "account already exists - use your API key to log in")
		return
	}
	if limit := int(env.Int("FREE_SIGNUPS_PER_IP", 5)); !trusted && limit > 0 {
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
		"seed_used":  s.seedUsage(r.Context(), user), "seed_cap": seedCap(plan, user),
		"seed_packs": user.SeedSlotPacks, "seed_max_packs": 2,
		"seeding_allowed": s.seedingAllowed(user),
		"totp_enabled":    user.TOTPEnabled,
	})
}

func (s *Server) plans(w http.ResponseWriter, _ *http.Request) {
	web.WriteJSON(w, 200, plans.Listed())
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request) {
	web.WriteJSON(w, 200, map[string]any{"turnstile_site_key": s.TurnstileSiteKey})
}

func trustedPartner(header, key string) bool {
	return key != "" && subtle.ConstantTimeCompare([]byte(header), []byte(key)) == 1
}

var turnstileClient = &http.Client{Timeout: 10 * time.Second}

func verifyTurnstile(ctx context.Context, secret, token, ip string) bool {
	if token == "" {
		return false
	}
	form := url.Values{"secret": {secret}, "response": {token}}
	if ip != "" {
		form.Set("remoteip", ip)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(form.Encode()))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := turnstileClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var out struct {
		Success bool `json:"success"`
	}
	return json.NewDecoder(resp.Body).Decode(&out) == nil && out.Success
}
