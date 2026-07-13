package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
)

func (s *Server) cryptoCheckout(w http.ResponseWriter, r *http.Request) {
	if s.Bitcart == nil || !s.Bitcart.Enabled() {
		web.WriteError(w, 503, "crypto payments not available")
		return
	}
	user, _ := r.Context().Value(middleware.UserContextKey).(*auth.User)
	if user == nil || user.Email == "" {
		web.WriteError(w, 401, "login required")
		return
	}

	var req struct {
		Plan   string `json:"plan"`
		Period string `json:"period"`
		Days   int    `json:"days"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		web.WriteError(w, 400, "invalid json")
		return
	}
	if !validPeriods[req.Period] {
		web.WriteError(w, 400, "invalid period")
		return
	}

	id, err := s.Bitcart.CreateInvoice(r.Context(), user.Email, req.Plan, req.Period, req.Days)
	if err != nil {
		web.WriteError(w, 400, "could not create invoice")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"invoice_id": id})
}

func (s *Server) bachsCheckout(w http.ResponseWriter, r *http.Request) {
	if s.Bachs == nil || !s.Bachs.Enabled() {
		web.WriteError(w, 503, "card payments not available")
		return
	}
	user, _ := r.Context().Value(middleware.UserContextKey).(*auth.User)
	if user == nil || user.Email == "" {
		web.WriteError(w, 401, "login required")
		return
	}

	var req struct {
		Plan   string `json:"plan"`
		Period string `json:"period"`
		Days   int    `json:"days"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		web.WriteError(w, 400, "invalid json")
		return
	}
	if !validPeriods[req.Period] {
		web.WriteError(w, 400, "invalid period")
		return
	}

	url, err := s.Bachs.CreateCheckout(r.Context(), user.Email, req.Plan, req.Period, req.Days)
	if err != nil {
		web.WriteError(w, 400, "could not create checkout")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"checkout_url": url})
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
