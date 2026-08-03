package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/safety"
)

func (s *Server) reloadBlocklist(ctx context.Context) {
	if hard, soft, err := s.Users.GetBlocklist(ctx); err == nil {
		safety.SetTerms(hard, soft)
	}
}

func (s *Server) adminGetBlocklist(w http.ResponseWriter, r *http.Request) {
	hard, soft, err := s.Users.GetBlocklist(r.Context())
	if err != nil {
		web.WriteError(w, 500, "blocklist operation failed")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"hard": hard, "soft": soft})
}

func (s *Server) adminAddBlocklist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Term string `json:"term"`
		Tier string `json:"tier"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&body); err != nil || body.Term == "" {
		web.WriteError(w, 400, "term required")
		return
	}
	if err := s.Users.AddBlocklistTerm(r.Context(), body.Term, body.Tier); err != nil {
		web.WriteError(w, 500, "blocklist operation failed")
		return
	}
	s.reloadBlocklist(r.Context())
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) adminDelBlocklist(w http.ResponseWriter, r *http.Request) {
	if err := s.Users.RemoveBlocklistTerm(r.Context(), r.PathValue("term")); err != nil {
		web.WriteError(w, 500, "blocklist operation failed")
		return
	}
	s.reloadBlocklist(r.Context())
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) adminReferrals(w http.ResponseWriter, r *http.Request) {
	refs, err := s.Users.AdminReferrers(r.Context(), 50)
	if err != nil {
		web.WriteError(w, 500, "blocklist operation failed")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"referrers": refs})
}

func (s *Server) adminPartnerReport(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" {
		web.WriteError(w, 400, "code required")
		return
	}
	pct := 30
	if v, err := strconv.Atoi(r.URL.Query().Get("pct")); err == nil && v > 0 && v <= 100 {
		pct = v
	}
	rep, err := s.Users.PartnerReport(r.Context(), code, pct)
	if err != nil {
		web.WriteError(w, 500, "blocklist operation failed")
		return
	}
	web.WriteJSON(w, 200, rep)
}

func (s *Server) adminSendKeys(w http.ResponseWriter, r *http.Request) {
	if s.Mailer == nil {
		web.WriteError(w, 500, "email not configured")
		return
	}
	recipients, err := s.Users.PaidUserKeys(r.Context())
	if err != nil {
		web.WriteError(w, 500, "blocklist operation failed")
		return
	}

	go func() {
		sent := 0
		for _, k := range recipients {
			if err := s.Mailer.SendWelcome(k.Email, k.APIKey); err != nil {
				slog.Warn("send-keys: failed", "email", k.Email, "err", err)
			} else {
				sent++
			}
			time.Sleep(200 * time.Millisecond)
		}
		slog.Info("send-keys complete", "sent", sent, "total", len(recipients))
	}()

	web.WriteJSON(w, 200, map[string]any{"queued": len(recipients)})
}
