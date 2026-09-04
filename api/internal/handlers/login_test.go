package handlers

import (
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
