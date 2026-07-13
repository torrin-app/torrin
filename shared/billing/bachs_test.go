package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBachsCreateCheckout(t *testing.T) {
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/checkout-sessions" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"checkout_id": "chk1", "checkout_url": "https://checkout.bachs.io/c/chk1", "status": "OPEN"})
	}))
	defer srv.Close()

	b := NewBachsHandler(srv.URL, "sk_sandbox_x", "whsec", "prod_1", "https://api.torrin.app", "https://torrin.app", nil)
	url, err := b.CreateCheckout(context.Background(), "a@b.com", "starter", "monthly", 0)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://checkout.bachs.io/c/chk1" {
		t.Errorf("checkout_url = %q", url)
	}
	if auth != "Bearer sk_sandbox_x" {
		t.Errorf("auth = %q", auth)
	}
	cart, _ := got["product_cart"].([]any)
	if len(cart) != 1 {
		t.Fatalf("product_cart = %v", got["product_cart"])
	}
	item := cart[0].(map[string]any)
	if item["product_id"] != "prod_1" || item["amount"] != "2.99" {
		t.Errorf("cart item = %v", item)
	}
	meta, _ := got["metadata"].(map[string]any)
	if meta["plan"] != "starter" || meta["period"] != "monthly" {
		t.Errorf("metadata = %v", meta)
	}
}

func TestBachsCreateCheckoutUnknownPlan(t *testing.T) {
	b := NewBachsHandler("http://x", "sk", "whsec", "prod_1", "", "", nil)
	if _, err := b.CreateCheckout(context.Background(), "a@b.com", "nope", "monthly", 0); err == nil {
		t.Error("expected error for unknown plan")
	}
}

func signBachs(secret string, body string) (string, string) {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s.%s", ts, body)
	return ts, hex.EncodeToString(mac.Sum(nil))
}

func TestBachsWebhookBadSignature(t *testing.T) {
	b := NewBachsHandler("http://x", "sk", "whsec", "prod_1", "", "", nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/bachs", strings.NewReader(`{"id":"evt_1","type":"collection.succeeded"}`))
	req.Header.Set("X-Bachs-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))
	req.Header.Set("X-Bachs-Signature", "deadbeef")
	b.HandleWebhook(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestBachsWebhookIgnoresNonPaid(t *testing.T) {
	// users is nil: a non-paid event must return before any store access.
	b := NewBachsHandler("http://x", "sk", "whsec", "prod_1", "", "", nil)
	body := `{"id":"evt_2","type":"collection.abandoned","data":{}}`
	ts, sig := signBachs("whsec", body)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/bachs", strings.NewReader(body))
	req.Header.Set("X-Bachs-Timestamp", ts)
	req.Header.Set("X-Bachs-Signature", sig)
	b.HandleWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
