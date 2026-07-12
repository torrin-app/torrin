package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBitcartCreateInvoice(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/invoices" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"id": "inv123", "status": "pending", "store_id": "s1"})
	}))
	defer srv.Close()

	b := NewBitcartHandler(srv.URL, "https://pay.torrin.app", "s1", "https://api.torrin.app", "https://torrin.app", nil)
	id, err := b.CreateInvoice(context.Background(), "a@b.com", "starter", "monthly", 0)
	if err != nil {
		t.Fatal(err)
	}
	if id != "inv123" {
		t.Errorf("invoice id = %q", id)
	}
	if got["store_id"] != "s1" {
		t.Errorf("store_id = %v", got["store_id"])
	}
	if got["price"].(float64) != 2.99 {
		t.Errorf("price = %v, want 2.99", got["price"])
	}
	meta, _ := got["metadata"].(map[string]any)
	if meta["plan"] != "starter" || meta["period"] != "monthly" {
		t.Errorf("metadata = %v", meta)
	}
	if got["notification_url"] != "https://api.torrin.app/webhooks/bitcart" {
		t.Errorf("notification_url = %v", got["notification_url"])
	}
}

func TestBitcartCreateInvoiceUnknownPlan(t *testing.T) {
	b := NewBitcartHandler("http://x", "http://x", "s1", "", "", nil)
	if _, err := b.CreateInvoice(context.Background(), "a@b.com", "nope", "monthly", 0); err == nil {
		t.Error("expected error for unknown plan")
	}
}

func TestBitcartWebhookIgnoresUnsettled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "inv123", "status": "pending"})
	}))
	defer srv.Close()

	// users is nil: a pending invoice must return before any store access.
	b := NewBitcartHandler(srv.URL, "http://x", "s1", "", "", nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/bitcart", strings.NewReader(`{"id":"inv123","status":"pending"}`))
	b.HandleWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestBitcartWebhookBadBody(t *testing.T) {
	b := NewBitcartHandler("http://x", "http://x", "s1", "", "", nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/bitcart", strings.NewReader(`{}`))
	b.HandleWebhook(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestCryptoExpiry(t *testing.T) {
	if got := cryptoExpiry("lifetime", 0); got.Year() != 2099 {
		t.Errorf("lifetime year = %d", got.Year())
	}
	if got := cryptoExpiry("days", 15); got.IsZero() {
		t.Error("days expiry zero")
	}
	if cryptoExpiry("yearly", 0).Before(cryptoExpiry("monthly", 0)) {
		t.Error("yearly should be later than monthly")
	}
}
