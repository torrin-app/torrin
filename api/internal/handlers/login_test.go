package handlers

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginLimiter(t *testing.T) {
	l := &loginLimiter{hits: map[string][]time.Time{}}
	for i := 0; i < loginMaxHits; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}
	if l.allow("1.2.3.4") {
		t.Error("over-limit attempt should be blocked")
	}
	if !l.allow("5.6.7.8") {
		t.Error("a different IP must not be rate-limited")
	}
}

func TestLogoutExpiresSessionCookie(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/auth/logout", nil)
	s.logout(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var found bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "tr_session" {
			found = true
			if c.MaxAge >= 0 || c.Value != "" {
				t.Errorf("tr_session not cleared: maxage=%d value=%q", c.MaxAge, c.Value)
			}
		}
	}
	if !found {
		t.Error("logout did not set a clearing tr_session cookie")
	}
}
