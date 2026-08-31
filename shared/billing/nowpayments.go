package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
)

const nowPayHost = "https://api.nowpayments.io/v1"

type NowPaymentsHandler struct {
	apiKey    string
	ipnSecret string
	apiBase   string
	webBase   string
	users     *auth.Store
	http      *http.Client
}

func NewNowPaymentsHandler(apiKey, ipnSecret, apiBase, webBase string, users *auth.Store) *NowPaymentsHandler {
	return &NowPaymentsHandler{
		apiKey:    apiKey,
		ipnSecret: ipnSecret,
		apiBase:   strings.TrimRight(apiBase, "/"),
		webBase:   strings.TrimRight(webBase, "/"),
		users:     users,
		http:      &http.Client{Timeout: 15 * time.Second},
	}
}

func (n *NowPaymentsHandler) Enabled() bool { return n.apiKey != "" }

func (n *NowPaymentsHandler) CreateInvoice(ctx context.Context, email, planID, period string, days int) (string, error) {
	cents, ok := plans.PriceCents(planID, period, days)
	if !ok || cents <= 0 {
		return "", fmt.Errorf("no price for plan %q period %q days %d", planID, period, days)
	}
	return n.createInvoice(ctx, cents, "torrin "+planID+" plan", map[string]string{
		"email": email, "plan": planID, "period": period, "days": fmt.Sprintf("%d", days),
	})
}

func (n *NowPaymentsHandler) CreateTopupInvoice(ctx context.Context, email string, cents int) (string, error) {
	return n.createInvoice(ctx, cents, "torrin wallet top-up", map[string]string{
		"email": email, "type": "topup", "credits": fmt.Sprintf("%d", cents),
	})
}

func encodeOrder(meta map[string]string) string {
	b, _ := json.Marshal(meta)
	return "torrin:" + base64.RawURLEncoding.EncodeToString(b)
}

func decodeOrder(orderID string) map[string]string {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(orderID, "torrin:"))
	if err != nil {
		return nil
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

func (n *NowPaymentsHandler) invoicePayload(cents int, desc string, meta map[string]string) map[string]any {
	return map[string]any{
		"price_amount":      float64(cents) / 100,
		"price_currency":    "usd",
		"order_id":          encodeOrder(meta),
		"order_description": desc,
		"ipn_callback_url":  n.apiBase + "/webhooks/nowpayments",
		"success_url":       n.webBase + "/?paid=1",
		"cancel_url":        n.webBase + "/?paid=0",
	}
}

func (n *NowPaymentsHandler) createInvoice(ctx context.Context, cents int, desc string, meta map[string]string) (string, error) {
	payload, _ := json.Marshal(n.invoicePayload(cents, desc, meta))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nowPayHost+"/invoice", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", n.apiKey)
	resp, err := n.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("nowpayments create invoice: %d %s", resp.StatusCode, msg)
	}
	var inv struct {
		InvoiceURL string `json:"invoice_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&inv); err != nil {
		return "", err
	}
	if inv.InvoiceURL == "" {
		return "", fmt.Errorf("nowpayments create invoice: empty invoice_url")
	}
	return inv.InvoiceURL, nil
}

func (n *NowPaymentsHandler) validSignature(sig string, body []byte) bool {
	if n.ipnSecret == "" || sig == "" {
		return false
	}
	var payload map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if dec.Decode(&payload) != nil {
		return false
	}
	sorted, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	mac := hmac.New(sha512.New, []byte(n.ipnSecret))
	mac.Write(sorted)
	return hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(sig))
}

func (n *NowPaymentsHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32*1024))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if !n.validSignature(r.Header.Get("x-nowpayments-sig"), body) {
		slog.Warn("nowpayments ipn bad signature")
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	var ipn struct {
		PaymentID     json.Number `json:"payment_id"`
		PaymentStatus string      `json:"payment_status"`
		OrderID       string      `json:"order_id"`
	}
	if json.Unmarshal(body, &ipn) != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	slog.Info("nowpayments ipn", "status", ipn.PaymentStatus, "payment", ipn.PaymentID.String())
	if ipn.PaymentStatus != "finished" {
		w.WriteHeader(http.StatusOK)
		return
	}
	n.activate(r.Context(), decodeOrder(ipn.OrderID), "nowpay:"+ipn.PaymentID.String())
	w.WriteHeader(http.StatusOK)
}

func (n *NowPaymentsHandler) activate(ctx context.Context, meta map[string]string, saleID string) {
	email := meta["email"]
	if email == "" {
		slog.Error("nowpayments ipn missing email", "sale", saleID)
		return
	}
	user, created, err := getOrCreateUser(ctx, n.users, email)
	if err != nil {
		slog.Error("nowpayments create user", "err", err)
		return
	}
	if created {
		slog.Info("new user created via nowpayments", "email", email)
	}
	if meta["type"] == "topup" {
		creditWallet(ctx, n.users, user.ID, meta["credits"], "topup:crypto", saleID)
		return
	}
	planID := meta["plan"]
	if _, ok := plans.Get(planID); !ok {
		slog.Warn("nowpayments unknown plan, rejecting", "sale", saleID, "plan", planID)
		return
	}
	if n.users.HasProcessedSale(ctx, saleID) {
		slog.Info("nowpayments duplicate ipn, skipping", "sale", saleID)
		return
	}
	days := 0
	fmt.Sscanf(meta["days"], "%d", &days)
	applyCryptoPlan(ctx, n.users, user, email, saleID, planID, meta["period"], "nowpayments", days)
}
