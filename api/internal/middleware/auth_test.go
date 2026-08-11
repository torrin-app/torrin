package middleware

import "testing"

func TestExpiredFreeAllowed(t *testing.T) {
	allow := []string{
		"/api/me", "/api/plans", "/api/stats", "/api/redeem",
		"/api/billing/crypto/checkout", "/api/billing/bachs/checkout",
	}
	for _, p := range allow {
		if !expiredFreeAllowed(p) {
			t.Errorf("expired free user should reach %s (to pay)", p)
		}
	}
	deny := []string{"/api/jobs", "/api/usenet/search", "/api/storage/connect", "/api/billing"}
	for _, p := range deny {
		if expiredFreeAllowed(p) {
			t.Errorf("%s should be blocked for expired free user", p)
		}
	}
}
