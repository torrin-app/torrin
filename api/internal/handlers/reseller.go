package handlers

import (
	crand "crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
)

var validPeriods = map[string]bool{"monthly": true, "yearly": true, "lifetime": true, "days": true}

func dayCodeRetired(period string) bool {
	return !plans.DayPlansEnabled && period == "days"
}

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func secretEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

var redeemLimiter = newAttemptLimiter(8, time.Minute)

type attemptLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newAttemptLimiter(max int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{max: max, window: window, hits: map[string][]time.Time{}}
}

func (l *attemptLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, time.Now())
	return true
}

func (s *Server) registerResellerRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	mux.Handle("POST /api/redeem", authMW(http.HandlerFunc(s.redeemCode)))
	mux.HandleFunc("POST /api/reseller/codes", s.mintCodes) // partner-keyed
	mux.HandleFunc("GET /api/reseller/codes", s.listCodes)
	mux.HandleFunc("POST /api/reseller/settle", s.settleCodes)
}

func (s *Server) settleCodes(w http.ResponseWriter, r *http.Request) {
	if s.ResellerKey == "" || !secretEqual(r.Header.Get("X-Reseller-Key"), s.ResellerKey) {
		web.WriteError(w, 401, "invalid reseller key")
		return
	}
	var req struct {
		Reseller string `json:"reseller"`
		Ref      string `json:"ref"`
		Undo     bool   `json:"undo"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		web.WriteError(w, 400, "invalid json")
		return
	}
	reseller := strings.TrimSpace(req.Reseller)
	if reseller == "" {
		web.WriteError(w, 400, "reseller required")
		return
	}
	var n int64
	var err error
	if req.Undo {
		n, err = s.Users.UnsettleResellerCodes(r.Context(), reseller)
	} else {
		n, err = s.Users.MarkResellerCodesSettled(r.Context(), reseller, strings.TrimSpace(req.Ref))
	}
	if err != nil {
		web.WriteError(w, 500, "could not update settlement")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"updated": n})
}

func redeemExpiry(period string, days int, user *auth.User) time.Time {
	var exp time.Time
	switch period {
	case "lifetime":
		return time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC)
	case "yearly":
		exp = time.Now().Add(365 * 24 * time.Hour)
	case "days":
		exp = time.Now().Add(time.Duration(days) * 24 * time.Hour)
	default:
		exp = time.Now().Add(35 * 24 * time.Hour)
	}
	if user.ExpiresAt.After(time.Now()) && !user.IsPaused() && user.PlanID != "free" {
		if remaining := time.Until(user.ExpiresAt); remaining > 0 {
			exp = exp.Add(remaining)
		}
	}
	return exp
}

func (s *Server) redeemCode(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !redeemLimiter.allow(clientIP(r)) {
		web.WriteError(w, 429, "too many attempts, slow down and try again")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		web.WriteError(w, 400, "invalid json")
		return
	}
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	if code == "" {
		web.WriteError(w, 400, "code required")
		return
	}
	rc, err := s.Users.GetResellerCode(r.Context(), code)
	if err != nil {
		web.WriteError(w, 400, "invalid code")
		return
	}
	if rc.Redeemed {
		web.WriteError(w, 409, "this code has already been used")
		return
	}
	if _, ok := plans.Get(rc.PlanID); !ok {
		web.WriteError(w, 400, "this code is no longer valid")
		return
	}
	if dayCodeRetired(rc.Period) {
		web.WriteError(w, 422, "day passes are no longer offered")
		return
	}
	claimed, err := s.Users.MarkResellerCodeRedeemed(r.Context(), code, user.ID)
	if err != nil {
		web.WriteError(w, 500, "could not redeem")
		return
	}
	if !claimed {
		web.WriteError(w, 409, "this code has already been used")
		return
	}
	expiresAt := redeemExpiry(rc.Period, rc.Days, user)
	if err := s.Users.UpdatePlan(r.Context(), user.ID, rc.PlanID, "", "", rc.Period, expiresAt); err != nil {
		web.WriteError(w, 500, "could not activate plan")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "code_redeemed", "code="+code, clientIP(r))
	web.WriteJSON(w, 200, map[string]any{"plan_id": rc.PlanID, "expires_at": expiresAt})
}

func (s *Server) mintCodes(w http.ResponseWriter, r *http.Request) {
	if s.ResellerKey == "" || !secretEqual(r.Header.Get("X-Reseller-Key"), s.ResellerKey) {
		web.WriteError(w, 401, "invalid reseller key")
		return
	}
	var req struct {
		PlanID   string `json:"plan_id"`
		Period   string `json:"period"`
		Days     int    `json:"days"`
		Quantity int    `json:"quantity"`
		Reseller string `json:"reseller"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		web.WriteError(w, 400, "invalid json")
		return
	}
	if _, ok := plans.Get(req.PlanID); !ok {
		web.WriteError(w, 400, "unknown plan")
		return
	}
	if !validPeriods[req.Period] {
		web.WriteError(w, 400, "period must be one of: monthly, yearly, lifetime, days")
		return
	}
	if dayCodeRetired(req.Period) {
		web.WriteError(w, 422, "day passes are retired and can no longer be minted")
		return
	}
	if req.Period == "days" && req.Days <= 0 {
		web.WriteError(w, 400, "days must be > 0 when period is 'days'")
		return
	}
	qty := req.Quantity
	if qty <= 0 {
		qty = 1
	}
	if qty > 100 {
		web.WriteError(w, 400, "quantity capped at 100 per call")
		return
	}
	reseller := req.Reseller
	if reseller == "" {
		reseller = "reseller"
	}
	retail, _ := plans.PriceCents(req.PlanID, req.Period, req.Days)
	codes := make([]string, 0, qty)
	for i := 0; i < qty; i++ {
		var code string
		for attempt := 0; attempt < 5; attempt++ {
			code = generateResellerCode()
			if s.Users.CreateResellerCode(r.Context(), &auth.ResellerCode{
				Code: code, PlanID: req.PlanID, Period: req.Period, Days: req.Days, Reseller: reseller, RetailCents: retail}) == nil {
				break
			}
			code = ""
		}
		if code == "" {
			web.WriteError(w, 500, "could not generate code")
			return
		}
		codes = append(codes, code)
	}
	web.WriteJSON(w, 200, map[string]any{"codes": codes, "plan_id": req.PlanID, "period": req.Period, "days": req.Days})
}

func (s *Server) listCodes(w http.ResponseWriter, r *http.Request) {
	if s.ResellerKey == "" || !secretEqual(r.Header.Get("X-Reseller-Key"), s.ResellerKey) {
		web.WriteError(w, 401, "invalid reseller key")
		return
	}
	redeemedOnly := r.URL.Query().Get("redeemed") == "true"
	var since *time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			since = &t
		}
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	codes, err := s.Users.ListResellerCodes(r.Context(), redeemedOnly, since, limit, offset)
	if err != nil {
		web.WriteError(w, 500, "could not list codes")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"codes": codes, "count": len(codes)})
}

func randCode(n int) string {
	b := make([]byte, n)
	crand.Read(b)
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b)
}

func generateResellerCode() string {
	s := randCode(12)
	return "TRN-" + s[0:4] + "-" + s[4:8] + "-" + s[8:12]
}
