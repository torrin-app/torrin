package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
)

type checkoutReq struct {
	Plan   string `json:"plan"`
	Period string `json:"period"`
	Days   int    `json:"days"`
}

func checkoutRequest(w http.ResponseWriter, r *http.Request) (*auth.User, checkoutReq, bool) {
	user, _ := r.Context().Value(middleware.UserContextKey).(*auth.User)
	if user == nil || user.Email == "" {
		web.WriteError(w, 401, "login required")
		return nil, checkoutReq{}, false
	}
	var req checkoutReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		web.WriteError(w, 400, "invalid json")
		return nil, checkoutReq{}, false
	}
	if !validPeriods[req.Period] {
		web.WriteError(w, 400, "invalid period")
		return nil, checkoutReq{}, false
	}
	if !plans.DayPlansEnabled && (req.Period == "days" || req.Days > 0) {
		web.WriteError(w, 422, "day passes are no longer offered")
		return nil, checkoutReq{}, false
	}
	return user, req, true
}

func (s *Server) cryptoCheckout(w http.ResponseWriter, r *http.Request) {
	if s.NowPay == nil || !s.NowPay.Enabled() {
		web.WriteError(w, 503, "crypto payments not available")
		return
	}
	user, req, ok := checkoutRequest(w, r)
	if !ok {
		return
	}
	url, err := s.NowPay.CreateInvoice(r.Context(), user.Email, req.Plan, req.Period, req.Days)
	if err != nil {
		web.WriteError(w, 502, "could not create invoice, please retry")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"checkout_url": url})
}

func (s *Server) bachsCheckout(w http.ResponseWriter, r *http.Request) {
	if s.Bachs == nil || !s.Bachs.Enabled() {
		web.WriteError(w, 503, "card payments not available")
		return
	}
	user, req, ok := checkoutRequest(w, r)
	if !ok {
		return
	}
	url, err := s.Bachs.CreateCheckout(r.Context(), user.Email, req.Plan, req.Period, req.Days)
	if err != nil {
		web.WriteError(w, 502, "could not create checkout, please retry")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"checkout_url": url})
}

func (s *Server) bachsPortal(w http.ResponseWriter, r *http.Request) {
	if s.Bachs == nil || !s.Bachs.Enabled() {
		web.WriteError(w, 503, "subscription management is unavailable right now")
		return
	}
	user, _ := r.Context().Value(middleware.UserContextKey).(*auth.User)
	if user == nil {
		web.WriteError(w, 401, "login required")
		return
	}
	custID, err := s.Users.BachsCustomerID(r.Context(), user.ID)
	if err != nil || custID == "" {
		web.WriteError(w, 404, "no subscription to manage")
		return
	}
	url, err := s.Bachs.CreatePortalSession(r.Context(), custID)
	if err != nil {
		web.WriteError(w, 502, "could not open subscription management, please retry")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"url": url})
}

func (s *Server) cryptoInvoice(w http.ResponseWriter, r *http.Request) {
	if s.Bitcart == nil || !s.Bitcart.Enabled() {
		web.WriteError(w, 503, "crypto payments not available")
		return
	}
	inv, err := s.Bitcart.GetInvoiceView(r.Context(), r.PathValue("id"))
	if err != nil {
		web.WriteError(w, 502, "could not load invoice")
		return
	}
	web.WriteJSON(w, 200, inv)
}

func (s *Server) cryptoInvoiceAddress(w http.ResponseWriter, r *http.Request) {
	if s.Bitcart == nil || !s.Bitcart.Enabled() {
		web.WriteError(w, 503, "crypto payments not available")
		return
	}
	var req struct {
		MethodID string `json:"method_id"`
		Address  string `json:"address"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&req); err != nil || req.MethodID == "" || req.Address == "" {
		web.WriteError(w, 400, "method_id and address required")
		return
	}
	if err := s.Bitcart.SubmitSenderAddress(r.Context(), r.PathValue("id"), req.MethodID, req.Address); err != nil {
		web.WriteError(w, 502, "could not submit address")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"ok": true})
}
