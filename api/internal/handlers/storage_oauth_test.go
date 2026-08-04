package handlers

import (
	"testing"
	"time"
)

func TestOAuthState(t *testing.T) {
	key := []byte("k")
	st := signOAuthState(key, "user1", "dropbox", "pool", time.Now().Add(time.Minute).Unix())
	uid, prov, mode, ok := verifyOAuthState(key, st)
	if !ok || uid != "user1" || prov != "dropbox" || mode != "pool" {
		t.Fatalf("roundtrip failed: %q %q %q %v", uid, prov, mode, ok)
	}
	if _, _, _, ok := verifyOAuthState([]byte("wrong"), st); ok {
		t.Error("tampered key should fail")
	}
	expired := signOAuthState(key, "u", "dropbox", "", time.Now().Add(-time.Minute).Unix())
	if _, _, _, ok := verifyOAuthState(key, expired); ok {
		t.Error("expired state should fail")
	}
	primary := signOAuthState(key, "u2", "gdrive", "", time.Now().Add(time.Minute).Unix())
	if _, _, mode, ok := verifyOAuthState(key, primary); !ok || mode != "" {
		t.Errorf("primary mode should be empty: %q %v", mode, ok)
	}
}
