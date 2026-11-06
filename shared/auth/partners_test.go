package auth

import (
	"context"
	"testing"
)

// The idempotent partner-register must never recover an account for an
// unauthenticated / operator request (empty partner id), so it returns early
// before touching the DB.
func TestAccountForPartnerRequiresPartner(t *testing.T) {
	var s Store // nil pool: an empty partnerID must not reach it
	if _, _, ok := s.AccountForPartner(context.Background(), "someone@example.com", ""); ok {
		t.Fatal("empty partner id must never match an account")
	}
}

func TestHashPartnerKey(t *testing.T) {
	a := HashPartnerKey("trn_rsl_montage_abc")
	if a != HashPartnerKey("trn_rsl_montage_abc") {
		t.Fatal("hash must be stable for the same key")
	}
	if a == HashPartnerKey("trn_rsl_other_xyz") {
		t.Fatal("distinct keys must hash distinctly")
	}
	if len(a) != 64 {
		t.Fatalf("sha256 hex should be 64 chars, got %d", len(a))
	}
	if HashPartnerKey("") == a {
		t.Fatal("empty key must not collide with a real key")
	}
}
