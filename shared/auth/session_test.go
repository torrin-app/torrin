package auth

import (
	"testing"
	"time"
)

func TestSession(t *testing.T) {
	key := []byte("test-signing-key")
	tok := SignSession(key, "user-1", time.Hour)

	if uid, ok := VerifySession(key, tok); !ok || uid != "user-1" {
		t.Fatalf("valid token should verify to user-1, got %q ok=%v", uid, ok)
	}
	if _, ok := VerifySession([]byte("wrong-key"), tok); ok {
		t.Error("wrong key must not verify")
	}
	if _, ok := VerifySession(key, tok+"x"); ok {
		t.Error("tampered token must not verify")
	}
	if _, ok := VerifySession(key, ""); ok {
		t.Error("empty token must not verify")
	}
	if _, ok := VerifySession(key, SignSession(key, "user-1", -time.Minute)); ok {
		t.Error("expired token must not verify")
	}
}
