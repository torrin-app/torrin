package plans

import "testing"

func TestGet(t *testing.T) {
	p, ok := Get("pro")
	if !ok || p.MaxConcurrent != 8 || p.MaxTorrentBytes != 1_000_000_000_000 {
		t.Fatalf("pro plan wrong: %+v", p)
	}
	if _, ok := Get("nope"); ok {
		t.Error("unknown plan should not resolve")
	}
}

func TestCanBYOK(t *testing.T) {
	if CanBYOK("free") || CanBYOK("") {
		t.Error("free/empty must not allow BYOK")
	}
	for _, id := range []string{"starter", "standard", "pro"} {
		if !CanBYOK(id) {
			t.Errorf("%s should allow BYOK", id)
		}
	}
}

func TestValidatePrice(t *testing.T) {
	if !ValidatePrice("standard", "monthly", 599) {
		t.Error("exact monthly price should pass")
	}
	if ValidatePrice("standard", "monthly", 100) {
		t.Error("underpayment should fail")
	}
	if !ValidatePrice("pro", "yearly", 12000) {
		t.Error("overpayment should pass")
	}
	if !ValidatePrice("starter", "days", 149) {
		t.Error("valid day price should pass")
	}
}
