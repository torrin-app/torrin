package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
)

const bachsPaidEvent = "collection.succeeded"

type BachsHandler struct {
	apiURL        string
	secretKey     string
	webhookSecret string
	productID     string
	apiBase       string
	webBase       string
	users         *auth.Store
	http          *http.Client
}

func NewBachsHandler(apiURL, secretKey, webhookSecret, productID, apiBase, webBase string, users *auth.Store) *BachsHandler {
	if apiURL == "" {
		apiURL = "https://sandbox-api.bachs.io"
	}
	return &BachsHandler{
		apiURL:        strings.TrimRight(apiURL, "/"),
		secretKey:     secretKey,
		webhookSecret: webhookSecret,
		productID:     productID,
		apiBase:       strings.TrimRight(apiBase, "/"),
		webBase:       strings.TrimRight(webBase, "/"),
		users:         users,
		http:          &http.Client{Timeout: 15 * time.Second},
	}
}

func (b *BachsHandler) Enabled() bool { return b.secretKey != "" && b.productID != "" }

func (b *BachsHandler) CreateCheckout(ctx context.Context, email, planID, period string, days int) (string, error) {
	cents, ok := plans.PriceCents(planID, period, days)
	if !ok || cents <= 0 {
		return "", fmt.Errorf("no price for plan %q period %q days %d", planID, period, days)
	}

	payload := map[string]any{
		"customer": map[string]string{"email": email, "name": email},
		"product_cart": []map[string]any{{
			"product_id": b.productID,
			"quantity":   1,
			"amount":     fmt.Sprintf("%.2f", float64(cents)/100),
		}},
		"return_url": b.webBase + "/?paid=1",
		"metadata": map[string]string{
			"email":  email,
			"plan":   planID,
			"period": period,
			"days":   strconv.Itoa(days),
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.apiURL+"/v1/checkout-sessions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.secretKey)

	resp, err := b.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("bachs create checkout: %d %s", resp.StatusCode, msg)
	}

	var out struct {
		CheckoutURL string `json:"checkout_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.CheckoutURL == "" {
		return "", fmt.Errorf("bachs create checkout: empty checkout_url")
	}
	return out.CheckoutURL, nil
}

func (b *BachsHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if !b.verify(raw, r.Header.Get("X-Bachs-Timestamp"), r.Header.Get("X-Bachs-Signature")) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}

	var evt struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Metadata map[string]string `json:"metadata"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &evt); err != nil || evt.ID == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	slog.Info("bachs webhook", "id", evt.ID, "type", evt.Type)
	if evt.Type != bachsPaidEvent {
		w.WriteHeader(http.StatusOK)
		return
	}
	b.activate(r.Context(), evt.ID, evt.Data.Metadata)
	w.WriteHeader(http.StatusOK)
}

func (b *BachsHandler) verify(rawBody []byte, timestampHeader, signatureHeader string) bool {
	if b.webhookSecret == "" || signatureHeader == "" {
		return false
	}
	ts, err := strconv.ParseInt(timestampHeader, 10, 64)
	if err != nil {
		return false
	}
	if math.Abs(float64(time.Now().Unix()-ts)) > 300 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(b.webhookSecret))
	fmt.Fprintf(mac, "%d.%s", ts, rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signatureHeader))
}

func (b *BachsHandler) activate(ctx context.Context, eventID string, meta map[string]string) {
	email := meta["email"]
	planID := meta["plan"]
	period := meta["period"]
	if email == "" || planID == "" {
		slog.Error("bachs event missing metadata", "id", eventID)
		return
	}
	if _, ok := plans.Get(planID); !ok {
		slog.Warn("bachs unknown plan, rejecting", "id", eventID, "plan", planID)
		return
	}
	if b.users.HasProcessedSale(ctx, eventID) {
		slog.Info("bachs duplicate event, skipping", "id", eventID)
		return
	}

	user, err := b.users.GetByEmail(ctx, email)
	if err != nil || user == nil {
		if user, err = b.users.CreateUser(ctx, email); err != nil {
			slog.Error("bachs create user", "err", err)
			return
		}
		slog.Info("new user created via bachs", "email", email)
	}

	days := 0
	fmt.Sscanf(meta["days"], "%d", &days)
	expiresAt := cryptoExpiry(period, days)
	if user.ExpiresAt.After(time.Now()) && !user.IsPaused() && user.PlanID != "free" && period != "lifetime" {
		if remaining := time.Until(user.ExpiresAt); remaining > 0 {
			expiresAt = expiresAt.Add(remaining)
		}
	}

	if err := b.users.UpdatePlan(ctx, user.ID, planID, "", eventID, period, expiresAt); err != nil {
		slog.Error("bachs update plan", "err", err)
		return
	}
	cents, _ := plans.PriceCents(planID, period, days)
	b.users.RecordProcessedSale(ctx, eventID, user.ID, cents, "USD")
	slog.Info("plan activated via bachs", "email", email, "plan", planID, "period", period, "expires", expiresAt.Format("2006-01-02"))
}
