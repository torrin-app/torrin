package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
)

type GumroadHandler struct {
	secret    string
	sellerID  string
	userStore *auth.Store
}

func NewGumroadHandler(secret, sellerID string, userStore *auth.Store) *GumroadHandler {
	return &GumroadHandler{secret: secret, sellerID: sellerID, userStore: userStore}
}

func (g *GumroadHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if !g.verifyWebhook(body, r.Header.Get("X-Gumroad-Signature")) {
		slog.Warn("invalid gumroad signature")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	v, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, "parse body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	event := v.Get("resource_name")
	slog.Info("gumroad webhook", "event", event, "email", v.Get("email"))

	if v.Get("disputed") == "true" {
		g.handleDispute(ctx, v)
	}

	switch event {
	case "sale", "subscription_restarted":
		g.handleSale(ctx, v)
	case "subscription_ended", "refund":
		if sub := v.Get("subscription_id"); sub != "" {
			g.userStore.Downgrade(ctx, sub)
			slog.Info("subscription downgraded", "event", event, "subscription_id", sub)
		}
	case "cancellation":
		slog.Info("subscription cancelled, ends at period end", "subscription_id", v.Get("subscription_id"))
	default:
		slog.Info("unhandled gumroad event", "event", event)
	}
	w.WriteHeader(http.StatusOK)
}

func (g *GumroadHandler) handleSale(ctx context.Context, v url.Values) {
	email, permalink, saleID := v.Get("email"), v.Get("permalink"), v.Get("sale_id")
	if email == "" {
		slog.Error("sale missing email")
		return
	}
	if saleID != "" && g.userStore.HasProcessedSale(ctx, saleID) {
		slog.Info("duplicate sale event, skipping", "sale_id", saleID)
		return
	}

	planID, ok := plans.ByGumroadProduct[permalink]
	if !ok {
		slog.Warn("unknown gumroad product, rejecting", "permalink", permalink)
		return
	}
	period := plans.RecurrenceFromProduct(permalink)
	if price, _ := strconv.Atoi(v.Get("price")); price > 0 && !plans.ValidatePrice(planID, period, price) {
		slog.Warn("price mismatch, rejecting", "permalink", permalink, "plan", planID)
		return
	}

	user, err := g.userStore.GetByEmail(ctx, email)
	if err != nil || user == nil {
		if user, err = g.userStore.CreateUser(ctx, email); err != nil {
			slog.Error("create user", "err", err)
			return
		}
		slog.Info("new user created via gumroad", "email", email)
	}

	recurrence := v.Get("recurrence")
	if recurrence == "" {
		recurrence = period
	}
	expiresAt := saleExpiry(period, permalink, &recurrence)

	if user.ExpiresAt.After(time.Now()) && !user.IsPaused() && user.PlanID != "free" && period != "lifetime" {
		if remaining := time.Until(user.ExpiresAt); remaining > 0 {
			expiresAt = expiresAt.Add(remaining)
		}
	}

	if err := g.userStore.UpdatePlan(ctx, user.ID, planID, v.Get("subscription_id"), v.Get("license_key"), recurrence, expiresAt); err != nil {
		slog.Error("update plan", "err", err)
		return
	}
	if saleID != "" {
		saleCents, _ := strconv.Atoi(v.Get("price"))
		g.userStore.RecordProcessedSale(ctx, saleID, user.ID, saleCents, v.Get("currency"))
	}
	slog.Info("plan activated", "email", email, "plan", planID, "recurrence", recurrence, "expires", expiresAt.Format("2006-01-02"))

	if recurrence == "monthly" || recurrence == "yearly" {
		g.userStore.CreditReferral(ctx, user.ID)
	}
}

func saleExpiry(period, permalink string, recurrence *string) time.Time {
	switch period {
	case "lifetime":
		*recurrence = "lifetime"
		return time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC)
	case "yearly":
		return time.Now().Add(365 * 24 * time.Hour)
	case "days":
		days := plans.DaysFromProduct(permalink)
		if days <= 0 {
			days = 7
		}
		*recurrence = "days"
		return time.Now().Add(time.Duration(days) * 24 * time.Hour)
	default:
		return time.Now().Add(35 * 24 * time.Hour)
	}
}

func (g *GumroadHandler) handleDispute(ctx context.Context, v url.Values) {
	email := v.Get("email")
	if email == "" {
		return
	}
	user, err := g.userStore.GetByEmail(ctx, email)
	if err != nil || user == nil {
		slog.Warn("dispute for unknown user", "email", email)
		return
	}
	g.userStore.UpdatePlan(ctx, user.ID, "free", "", "", "", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	g.userStore.AuditLog(ctx, user.ID, "dispute_auto_suspended", "email="+email, "")
	slog.Warn("DISPUTE: account auto-suspended", "email", email, "user_id", user.ID)
}

func (g *GumroadHandler) verifyWebhook(body []byte, signature string) bool {
	if g.secret != "" {
		if signature == "" {
			return false
		}
		mac := hmac.New(sha256.New, []byte(g.secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(expected), []byte(signature))
	}
	if g.sellerID != "" {
		v, err := url.ParseQuery(string(body))
		if err != nil {
			return false
		}
		return v.Get("seller_id") == g.sellerID
	}
	return false
}
