package server

import (
	"net/http"
	"testing"
	"time"
)

func TestSpeedtestClientIP(t *testing.T) {
	cases := []struct{ xff, remote, want string }{
		{"1.2.3.4", "", "1.2.3.4"},
		{"1.2.3.4, 5.6.7.8", "", "1.2.3.4"},
		{"  1.1.1.1 , 2.2.2.2", "", "1.1.1.1"},
		{"", "9.9.9.9:443", "9.9.9.9"},
	}
	for _, c := range cases {
		r := &http.Request{Header: http.Header{}, RemoteAddr: c.remote}
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := speedtestClientIP(r); got != c.want {
			t.Errorf("xff=%q remote=%q => %q, want %q", c.xff, c.remote, got, c.want)
		}
	}
}

func TestSpeedtestBudget(t *testing.T) {
	l := &speedtestLimiter{sem: make(chan struct{}, 1), used: map[string]*stBudget{}}

	if !l.allowIP("1.2.3.4", speedtestBytesPerMin) {
		t.Fatal("first request is checked before adding, so it should pass")
	}
	if l.allowIP("1.2.3.4", 1) {
		t.Fatal("second request after the budget is spent must be denied")
	}
	if !l.allowIP("5.6.7.8", 1) {
		t.Fatal("a different IP has its own budget and should be allowed")
	}

	l.used["1.2.3.4"].reset = time.Now().Add(-time.Second)
	if !l.allowIP("1.2.3.4", 1) {
		t.Fatal("after the window resets the IP should be allowed again")
	}
}
