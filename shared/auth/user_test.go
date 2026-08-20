package auth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNormalizeEmail(t *testing.T) {
	cases := map[string]string{
		"You+promo@Gmail.com":  "you@gmail.com",
		"y.o.u@googlemail.com": "you@gmail.com",
		" Ab@Proton.me ":       "ab@proton.me",
		"tag+x@outlook.com":    "tag@outlook.com",
		"no.dots@fastmail.com": "no.dots@fastmail.com",
		"weird":                "weird",
	}
	for in, want := range cases {
		if got := NormalizeEmail(in); got != want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateSignupEmail(t *testing.T) {
	ctx := context.Background()
	for _, bad := range []string{"notanemail", "x@", "@nodomain.com", "x@no-such-domain-torrin-test.invalid"} {
		if err := ValidateSignupEmail(ctx, bad); err == nil {
			t.Errorf("ValidateSignupEmail(%q) = nil, want error", bad)
		}
	}
	if err := ValidateSignupEmail(ctx, "someone@gmail.com"); err != nil {
		t.Errorf("gmail.com has MX, want nil, got %v", err)
	}
}

func TestIsPaused(t *testing.T) {
	var u User
	if u.IsPaused() {
		t.Error("zero PausedAt should not count as paused")
	}
	u.PausedAt = time.Now()
	if !u.IsPaused() {
		t.Error("set PausedAt should count as paused")
	}
}

func TestResellerCodeJSONHasRedeemed(t *testing.T) {
	b, _ := json.Marshal(ResellerCode{Code: "TRN-X", RedeemedBy: "u1", Redeemed: true})
	if !strings.Contains(string(b), `"redeemed":true`) {
		t.Errorf("redeemed flag missing from JSON: %s", b)
	}
	b2, _ := json.Marshal(ResellerCode{Code: "TRN-Y"})
	if !strings.Contains(string(b2), `"redeemed":false`) {
		t.Errorf("unredeemed code should serialize redeemed:false: %s", b2)
	}
}
