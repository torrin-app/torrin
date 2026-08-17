package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/torrin-app/torrin/api/internal/web"
)

func (s *Server) adminWalletGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	bal, err := s.Users.WalletBalance(r.Context(), id)
	if err != nil {
		web.WriteError(w, 500, "wallet lookup failed")
		return
	}
	hist, _ := s.Users.WalletHistory(r.Context(), id, 100)
	web.WriteJSON(w, 200, map[string]any{"balance_cents": bal, "history": hist})
}

func (s *Server) adminWalletAdjust(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		AmountCents int64  `json:"amount_cents"`
		Reason      string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&body); err != nil || body.AmountCents == 0 {
		web.WriteError(w, 400, "amount_cents required (non-zero)")
		return
	}
	reason := "admin"
	if body.Reason != "" {
		reason = "admin:" + body.Reason
	}
	ref := "admin:" + uuid.NewString()
	if body.AmountCents > 0 {
		if err := s.Users.Credit(r.Context(), id, body.AmountCents, reason, ref); err != nil {
			web.WriteError(w, 500, "credit failed")
			return
		}
	} else {
		paid, err := s.Users.Debit(r.Context(), id, -body.AmountCents, reason, ref)
		if err != nil {
			web.WriteError(w, 500, "debit failed")
			return
		}
		if !paid {
			web.WriteError(w, 402, "insufficient balance to deduct")
			return
		}
	}
	bal, _ := s.Users.WalletBalance(r.Context(), id)
	web.WriteJSON(w, 200, map[string]any{"balance_cents": bal})
}
