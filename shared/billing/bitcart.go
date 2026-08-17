package billing

import (
	"bytes"
	"context"
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

const bitcartSettled = "complete"

type BitcartHandler struct {
	apiURL      string
	checkoutURL string
	storeID     string
	apiBase     string
	webBase     string
	users       *auth.Store
	http        *http.Client
}

func NewBitcartHandler(apiURL, checkoutURL, storeID, apiBase, webBase string, users *auth.Store) *BitcartHandler {
	return &BitcartHandler{
		apiURL:      strings.TrimRight(apiURL, "/"),
		checkoutURL: strings.TrimRight(checkoutURL, "/"),
		storeID:     storeID,
		apiBase:     strings.TrimRight(apiBase, "/"),
		webBase:     strings.TrimRight(webBase, "/"),
		users:       users,
		http:        &http.Client{Timeout: 15 * time.Second},
	}
}

func (b *BitcartHandler) Enabled() bool { return b.apiURL != "" && b.storeID != "" }

type bitcartInvoice struct {
	ID       string            `json:"id"`
	Status   string            `json:"status"`
	StoreID  string            `json:"store_id"`
	Currency string            `json:"currency"`
	Metadata map[string]string `json:"metadata"`
}

func (b *BitcartHandler) CreateInvoice(ctx context.Context, email, planID, period string, days int) (string, error) {
	cents, ok := plans.PriceCents(planID, period, days)
	if !ok || cents <= 0 {
		return "", fmt.Errorf("no price for plan %q period %q days %d", planID, period, days)
	}
	return b.createInvoice(ctx, email, cents, map[string]string{
		"email": email, "plan": planID, "period": period, "days": fmt.Sprintf("%d", days),
	})
}

func (b *BitcartHandler) CreateTopupInvoice(ctx context.Context, email string, cents int) (string, error) {
	return b.createInvoice(ctx, email, cents, map[string]string{
		"email": email, "type": "topup", "credits": fmt.Sprintf("%d", cents),
	})
}

func (b *BitcartHandler) createInvoice(ctx context.Context, email string, cents int, meta map[string]string) (string, error) {
	payload := map[string]any{
		"price":            float64(cents) / 100,
		"store_id":         b.storeID,
		"currency":         "USD",
		"buyer_email":      email,
		"order_id":         fmt.Sprintf("torrin:%s:%d", email, time.Now().UnixNano()),
		"notification_url": b.apiBase + "/webhooks/bitcart",
		"redirect_url":     b.webBase + "/?paid=1",
		"metadata":         meta,
	}
	body, _ := json.Marshal(payload)

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(400 * time.Millisecond)
		}
		id, retryable, err := b.postInvoice(ctx, body)
		if err == nil {
			return id, nil
		}
		lastErr = err
		if !retryable {
			return "", err
		}
	}
	return "", lastErr
}

func (b *BitcartHandler) postInvoice(ctx context.Context, body []byte) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.apiURL+"/invoices", bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.http.Do(req)
	if err != nil {
		return "", true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 5 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", true, fmt.Errorf("bitcart create invoice: %d %s", resp.StatusCode, msg)
	}
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", false, fmt.Errorf("bitcart create invoice: %d %s", resp.StatusCode, msg)
	}

	var inv bitcartInvoice
	if err := json.NewDecoder(resp.Body).Decode(&inv); err != nil {
		return "", true, err
	}
	if inv.ID == "" {
		return "", true, fmt.Errorf("bitcart create invoice: empty id")
	}
	return inv.ID, false, nil
}

type InvoicePayment struct {
	ID            string `json:"id"`
	Currency      string `json:"currency"`
	Name          string `json:"name"`
	Symbol        string `json:"symbol"`
	Amount        string `json:"amount"`
	Address       string `json:"address"`
	URL           string `json:"url"`
	Rate          string `json:"rate"`
	Confirmations int    `json:"confirmations"`
}

type InvoiceView struct {
	ID       string           `json:"id"`
	Status   string           `json:"status"`
	Settled  bool             `json:"settled"`
	PriceUSD string           `json:"price_usd"`
	TimeLeft int              `json:"time_left"`
	Payments []InvoicePayment `json:"payments"`
}

func (b *BitcartHandler) GetInvoiceView(ctx context.Context, id string) (*InvoiceView, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.apiURL+"/invoices/"+id, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var raw struct {
		ID       string          `json:"id"`
		Status   string          `json:"status"`
		Price    json.RawMessage `json:"price"`
		TimeLeft int             `json:"time_left"`
		Payments []struct {
			ID             string `json:"id"`
			Currency       string `json:"currency"`
			Name           string `json:"name"`
			Symbol         string `json:"symbol"`
			Amount         string `json:"amount"`
			PaymentAddress string `json:"payment_address"`
			PaymentURL     string `json:"payment_url"`
			RateStr        string `json:"rate_str"`
			Confirmations  int    `json:"confirmations"`
		} `json:"payments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	view := &InvoiceView{
		ID: raw.ID, Status: raw.Status, Settled: raw.Status == bitcartSettled,
		PriceUSD: strings.Trim(string(raw.Price), `"`), TimeLeft: raw.TimeLeft,
	}
	for _, p := range raw.Payments {
		view.Payments = append(view.Payments, InvoicePayment{
			ID: p.ID, Currency: p.Currency, Name: p.Name, Symbol: p.Symbol, Amount: p.Amount,
			Address: p.PaymentAddress, URL: p.PaymentURL, Rate: p.RateStr,
			Confirmations: p.Confirmations,
		})
	}
	return view, nil
}

func (b *BitcartHandler) SubmitSenderAddress(ctx context.Context, invoiceID, methodID, address string) error {
	payload, _ := json.Marshal(map[string]string{"id": methodID, "address": address})
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, b.apiURL+"/invoices/"+invoiceID+"/details", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("bitcart set address: %d %s", resp.StatusCode, msg)
	}
	return nil
}

func (b *BitcartHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var ipn struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&ipn); err != nil || ipn.ID == "" {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}

	inv, err := b.fetchInvoice(r.Context(), ipn.ID)
	if err != nil {
		slog.Error("bitcart fetch invoice", "id", ipn.ID, "err", err)
		http.Error(w, "fetch failed", http.StatusBadGateway)
		return
	}

	slog.Info("bitcart ipn", "id", inv.ID, "status", inv.Status)
	if inv.Status != bitcartSettled {
		w.WriteHeader(http.StatusOK)
		return
	}
	if inv.StoreID != "" && inv.StoreID != b.storeID {
		slog.Warn("bitcart invoice store mismatch", "id", inv.ID, "store", inv.StoreID)
		w.WriteHeader(http.StatusOK)
		return
	}
	b.activate(r.Context(), inv)
	w.WriteHeader(http.StatusOK)
}

func (b *BitcartHandler) fetchInvoice(ctx context.Context, id string) (*bitcartInvoice, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.apiURL+"/invoices/"+id, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var inv bitcartInvoice
	if err := json.NewDecoder(resp.Body).Decode(&inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

func (b *BitcartHandler) activate(ctx context.Context, inv *bitcartInvoice) {
	email := inv.Metadata["email"]
	if email == "" {
		slog.Error("bitcart invoice missing email", "id", inv.ID)
		return
	}
	user, created, err := getOrCreateUser(ctx, b.users, email)
	if err != nil {
		slog.Error("bitcart create user", "err", err)
		return
	}
	if created {
		slog.Info("new user created via bitcart", "email", email)
	}

	if inv.Metadata["type"] == "topup" {
		creditWallet(ctx, b.users, user.ID, inv.Metadata["credits"], "topup:crypto", inv.ID)
		return
	}

	planID := inv.Metadata["plan"]
	if planID == "" {
		slog.Error("bitcart invoice missing plan", "id", inv.ID)
		return
	}
	if _, ok := plans.Get(planID); !ok {
		slog.Warn("bitcart unknown plan, rejecting", "id", inv.ID, "plan", planID)
		return
	}
	if b.users.HasProcessedSale(ctx, inv.ID) {
		slog.Info("bitcart duplicate ipn, skipping", "id", inv.ID)
		return
	}
	days := 0
	fmt.Sscanf(inv.Metadata["days"], "%d", &days)
	applyCryptoPlan(ctx, b.users, user, email, inv.ID, planID, inv.Metadata["period"], "bitcart", days)
}

func cryptoExpiry(period string, days int) time.Time {
	switch period {
	case "lifetime":
		return time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC)
	case "yearly":
		return time.Now().Add(365 * 24 * time.Hour)
	case "days":
		if days <= 0 {
			days = 7
		}
		return time.Now().Add(time.Duration(days) * 24 * time.Hour)
	default:
		return time.Now().Add(35 * 24 * time.Hour)
	}
}
