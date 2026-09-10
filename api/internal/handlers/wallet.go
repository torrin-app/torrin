package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/billing"
	"github.com/torrin-app/torrin/shared/plans"
)

func (s *Server) walletGet(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	bal, _ := s.Users.WalletBalance(r.Context(), user.ID)
	hist, _ := s.Users.WalletHistory(r.Context(), user.ID, 50)
	web.WriteJSON(w, 200, map[string]any{"balance_cents": bal, "history": hist})
}

func (s *Server) walletBuyPlan(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var req struct {
		Plan           string `json:"plan"`
		Period         string `json:"period"`
		Recurring      bool   `json:"recurring"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&req); err != nil {
		web.WriteError(w, 400, "invalid json")
		return
	}
	if _, ok := plans.Get(req.Plan); !ok || req.Plan == "free" {
		web.WriteError(w, 400, "invalid plan")
		return
	}
	if req.Period != "monthly" && req.Period != "yearly" && req.Period != "lifetime" {
		web.WriteError(w, 400, "invalid period")
		return
	}
	paid, err := billing.BuyPlanFromWallet(r.Context(), s.Users, user, req.Plan, req.Period, req.Recurring, req.IdempotencyKey)
	if err != nil {
		web.WriteError(w, 500, "could not complete purchase")
		return
	}
	if !paid {
		web.WriteError(w, 402, "insufficient balance, top up your wallet")
		return
	}
	s.Slots.Wake()
	web.WriteJSON(w, 200, map[string]any{"status": "ok"})
}

func (s *Server) walletTopup(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var req struct {
		AmountCents int    `json:"amount_cents"`
		Provider    string `json:"provider"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&req); err != nil || req.AmountCents < 100 {
		web.WriteError(w, 400, "minimum top-up is $1")
		return
	}
	switch req.Provider {
	case "bachs":
		if s.Bachs == nil || !s.Bachs.Enabled() {
			web.WriteError(w, 503, "card payments not available")
			return
		}
		url, err := s.Bachs.CreateTopup(r.Context(), user.Email, req.AmountCents)
		if err != nil {
			web.WriteError(w, 502, "could not create checkout, please retry")
			return
		}
		web.WriteJSON(w, 200, map[string]any{"checkout_url": url})
	case "crypto":
		if s.NowPay == nil || !s.NowPay.Enabled() {
			web.WriteError(w, 503, "crypto payments not available")
			return
		}
		url, err := s.NowPay.CreateTopupInvoice(r.Context(), user.Email, req.AmountCents)
		if err != nil {
			web.WriteError(w, 502, "could not create invoice, please retry")
			return
		}
		web.WriteJSON(w, 200, map[string]any{"checkout_url": url})
	default:
		web.WriteError(w, 400, "unknown provider")
	}
}
