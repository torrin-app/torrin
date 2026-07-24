package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidRefCode(t *testing.T) {
	valid := []string{"stremio-addons.net", "stremio", "abc_123", "A1", strings.Repeat("a", 64)}
	for _, c := range valid {
		if !validRefCode(c) {
			t.Errorf("%q should be valid", c)
		}
	}
	invalid := []string{"", "has space", "bad/slash", "semi;colon", "quote\"", strings.Repeat("a", 65), "emoji😀"}
	for _, c := range invalid {
		if validRefCode(c) {
			t.Errorf("%q should be invalid", c)
		}
	}
}

func TestReferralRedirectInvalidCode(t *testing.T) {
	s := &Server{Deps: Deps{WebBase: "https://torrin.app"}}
	r := httptest.NewRequest("GET", "/r/bad%20code", nil)
	r.SetPathValue("code", "bad code")
	w := httptest.NewRecorder()

	s.referralRedirect(w, r)

	if w.Code != 302 {
		t.Fatalf("want 302, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "https://torrin.app" {
		t.Errorf("invalid code should redirect to WebBase without ?ref, got %q", loc)
	}
	if w.Header().Get("Set-Cookie") != "" {
		t.Error("invalid code must not set a cookie")
	}
}
