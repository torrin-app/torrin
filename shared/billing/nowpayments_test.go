package billing

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestOrderEncodeDecode(t *testing.T) {
	meta := map[string]string{"email": "a@b.com", "plan": "pro", "period": "yearly", "days": "0"}
	got := decodeOrder(encodeOrder(meta))
	for k, v := range meta {
		if got[k] != v {
			t.Fatalf("roundtrip %s = %q, want %q", k, got[k], v)
		}
	}
	if decodeOrder("not-base64!!!") != nil {
		t.Error("malformed order id should decode to nil")
	}
}

func TestInvoicePayloadFloatingRate(t *testing.T) {
	n := &NowPaymentsHandler{apiBase: "https://torrin.app", webBase: "https://torrin.app"}
	p := n.invoicePayload(299, "starter", map[string]string{"email": "a@b.com", "plan": "starter"})
	if _, ok := p["is_fixed_rate"]; ok {
		t.Fatal("is_fixed_rate must not be set: fixed rate forces a high minimum and rejects small charges")
	}
	if p["price_amount"] != 2.99 {
		t.Fatalf("price_amount = %v, want 2.99", p["price_amount"])
	}
	if p["price_currency"] != "usd" {
		t.Fatalf("price_currency = %v, want usd", p["price_currency"])
	}
	if p["ipn_callback_url"] != "https://torrin.app/webhooks/nowpayments" {
		t.Fatalf("ipn_callback_url = %v", p["ipn_callback_url"])
	}
}

func sigFor(secret string, body []byte) string {
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	_ = dec.Decode(&m)
	sorted, _ := json.Marshal(m)
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(sorted)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestValidSignature(t *testing.T) {
	n := &NowPaymentsHandler{ipnSecret: "sekret"}
	body := []byte(`{"payment_status":"finished","payment_id":123,"order_id":"torrin:x"}`)
	good := sigFor("sekret", body)

	if !n.validSignature(good, body) {
		t.Fatal("valid signature rejected")
	}
	if n.validSignature("deadbeef", body) {
		t.Fatal("wrong signature accepted")
	}
	if n.validSignature("", body) {
		t.Fatal("empty signature accepted")
	}
	if n.validSignature(sigFor("wrong-secret", body), body) {
		t.Fatal("signature from wrong secret accepted")
	}
	if (&NowPaymentsHandler{ipnSecret: ""}).validSignature(good, body) {
		t.Fatal("handler with no ipn secret must reject all")
	}
}
