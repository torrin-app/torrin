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
	donations     *Discord
	http          *http.Client
}

func NewBachsHandler(apiURL, secretKey, webhookSecret, productID, apiBase, webBase, donationWebhook string, users *auth.Store) *BachsHandler {
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
		donations:     NewDiscord(donationWebhook),
		http:          &http.Client{Timeout: 15 * time.Second},
	}
}

func (b *BachsHandler) Enabled() bool { return b.secretKey != "" && b.productID != "" }

func (b *BachsHandler) CreateCheckout(ctx context.Context, email, planID, period string, days int) (string, error) {
	cents, ok := plans.PriceCents(planID, period, days)
	if !ok || cents <= 0 {
		return "", fmt.Errorf("no price for plan %q period %q days %d", planID, period, days)
	}
	return b.createSession(ctx, email, cents, map[string]string{
		"email": email, "plan": planID, "period": period, "days": strconv.Itoa(days),
	})
}

func (b *BachsHandler) CreateTopup(ctx context.Context, email string, cents int) (string, error) {
	return b.createSession(ctx, email, cents, map[string]string{
		"email": email, "type": "topup", "credits": strconv.Itoa(cents),
	})
}

func (b *BachsHandler) createSession(ctx context.Context, email string, cents int, meta map[string]string) (string, error) {
	payload := map[string]any{
		"customer": map[string]string{"email": email, "name": email},
		"product_cart": []map[string]any{{
			"product_id": b.productID,
			"quantity":   1,
			"amount":     fmt.Sprintf("%.2f", float64(cents)/100),
		}},
		"return_url": b.webBase + "/?paid=1",
		"metadata":   meta,
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
			Metadata map[string]any `json:"metadata"`
			Amount   string         `json:"amount"`
			Currency string         `json:"currency"`
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
	meta := stringMap(evt.Data.Metadata)
	if isDonation(meta) {
		b.notifyDonation(donationMessage(evt.Data.Amount, evt.Data.Currency))
		w.WriteHeader(http.StatusOK)
		return
	}
	b.activate(r.Context(), evt.ID, meta)
	w.WriteHeader(http.StatusOK)
}

func isDonation(meta map[string]string) bool {
	return meta["plan"] == "" && meta["type"] != "topup"
}

func stringMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func (b *BachsHandler) notifyDonation(msg string) {
	if b.donations == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		b.donations.Post(ctx, msg)
	}()
}

func donationMessage(amount, currency string) string {
	return fmt.Sprintf("\U0001F49C New donation: %s\nThank you for supporting Torrin!", formatMoney(amount, currency))
}

func formatMoney(amount, currency string) string {
	switch {
	case amount == "":
		return "a contribution"
	case currency == "USD":
		return "$" + amount
	case currency == "":
		return amount
	default:
		return amount + " " + currency
	}
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
	if email == "" {
		slog.Error("bachs event missing email", "id", eventID)
		return
	}
	user, created, err := getOrCreateUser(ctx, b.users, email)
	if err != nil {
		slog.Error("bachs create user", "err", err)
		return
	}
	if created {
		slog.Info("new user created via bachs", "email", email)
	}

	if meta["type"] == "topup" {
		creditWallet(ctx, b.users, user.ID, meta["credits"], "topup:bachs", eventID)
		return
	}

	planID := meta["plan"]
	if planID == "" {
		slog.Error("bachs event missing plan", "id", eventID)
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
	days := 0
	fmt.Sscanf(meta["days"], "%d", &days)
	applyCryptoPlan(ctx, b.users, user, email, eventID, planID, meta["period"], "bachs", days)
}
