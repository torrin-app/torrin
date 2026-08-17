package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
)

func TestDayCodeRetired(t *testing.T) {
	defer func() { plans.DayPlansEnabled = true }()

	plans.DayPlansEnabled = true
	if dayCodeRetired("days") {
		t.Error("enabled: day codes must be allowed")
	}
	plans.DayPlansEnabled = false
	if !dayCodeRetired("days") {
		t.Error("disabled: day codes must be blocked")
	}
	for _, p := range []string{"monthly", "yearly", "lifetime"} {
		if dayCodeRetired(p) {
			t.Errorf("%s must never be blocked", p)
		}
	}
}

func TestRedeemExpiry(t *testing.T) {
	fresh := &auth.User{PlanID: "free"}
	if got := redeemExpiry("lifetime", 0, fresh); got.Year() != 2099 {
		t.Errorf("lifetime year = %d", got.Year())
	}
	// 30-day code with no existing time → ~30 days out.
	d := time.Until(redeemExpiry("days", 30, fresh)).Hours() / 24
	if d < 29 || d > 31 {
		t.Errorf("days expiry ~%.1f, want ~30", d)
	}
	// Active paid user keeps remaining time (carry-over).
	paid := &auth.User{PlanID: "pro", ExpiresAt: time.Now().Add(10 * 24 * time.Hour)}
	carry := time.Until(redeemExpiry("days", 30, paid)).Hours() / 24
	if carry < 39 {
		t.Errorf("carry-over ~%.1f, want ~40", carry)
	}
}

func TestGenerateResellerCode(t *testing.T) {
	c := generateResellerCode()
	if !strings.HasPrefix(c, "TRN-") || len(c) != 18 { // TRN-XXXX-XXXX-XXXX
		t.Errorf("bad code format: %q", c)
	}
}

func TestSecretEqual(t *testing.T) {
	if !secretEqual("s3cr3t-key", "s3cr3t-key") {
		t.Error("equal secrets should match")
	}
	if secretEqual("s3cr3t-key", "s3cr3t-keX") || secretEqual("short", "s3cr3t-key") || secretEqual("", "x") {
		t.Error("unequal secrets must not match")
	}
}

func TestAttemptLimiter(t *testing.T) {
	l := newAttemptLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.allow("ip-a") {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}
	if l.allow("ip-a") {
		t.Error("4th attempt should be blocked")
	}
	if !l.allow("ip-b") {
		t.Error("a different key should be independent")
	}

	w := newAttemptLimiter(1, 20*time.Millisecond)
	if !w.allow("k") || w.allow("k") {
		t.Fatal("first allowed, second blocked within window")
	}
	time.Sleep(30 * time.Millisecond)
	if !w.allow("k") {
		t.Error("should allow again after the window passes")
	}
}
