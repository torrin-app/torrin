package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLargeUpload(t *testing.T) {
	for _, p := range []string{"/api/jobs/nzb", "/api/torrents/upload", "/api/add"} {
		if !largeUpload(p) {
			t.Errorf("%s must skip the 1MB body cap (uploads set their own)", p)
		}
	}
	for _, p := range []string{"/api/jobs", "/api/me", "/api/usenet/grab"} {
		if largeUpload(p) {
			t.Errorf("%s should keep the 1MB cap", p)
		}
	}
}

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

func TestRequireLogin(t *testing.T) {
	h := RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	cases := []struct {
		name    string
		set     bool
		allowed bool
		want    int
	}{
		{"login-allowed key passes", true, true, 200},
		{"api-only key blocked", true, false, 403},
		{"missing flag defaults to blocked", false, false, 403},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/api/me", nil)
		if c.set {
			req = req.WithContext(context.WithValue(req.Context(), loginAllowedKey, c.allowed))
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: got %d, want %d", c.name, rec.Code, c.want)
		}
	}
}
