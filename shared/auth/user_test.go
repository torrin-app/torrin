package auth

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

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
