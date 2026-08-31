package handlers

import "testing"

func TestTrustedPartner(t *testing.T) {
	if !trustedPartner("secret", "secret") {
		t.Error("matching key should be trusted")
	}
	if trustedPartner("wrong", "secret") {
		t.Error("wrong key must not be trusted")
	}
	if trustedPartner("", "secret") {
		t.Error("empty header must not be trusted")
	}
	if trustedPartner("anything", "") {
		t.Error("unconfigured key must fail closed (never bypass)")
	}
	if trustedPartner("", "") {
		t.Error("empty/empty must not be trusted")
	}
}
