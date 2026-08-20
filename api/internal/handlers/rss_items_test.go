package handlers

import "testing"

func TestRSSExhausted(t *testing.T) {
	for n := 1; n < rssMaxAttempts; n++ {
		if rssExhausted(n) {
			t.Errorf("attempt %d should still retry (cap %d)", n, rssMaxAttempts)
		}
	}
	if !rssExhausted(rssMaxAttempts) {
		t.Errorf("attempt %d should give up (cap %d)", rssMaxAttempts, rssMaxAttempts)
	}
	if !rssExhausted(rssMaxAttempts + 5) {
		t.Error("past the cap should give up")
	}
}
