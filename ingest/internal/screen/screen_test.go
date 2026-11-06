package screen

import (
	"context"
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/safety"
)

func TestBlockedAllowOverridesSoftOnly(t *testing.T) {
	safety.SetTerms([]string{"hardbad"}, []string{"lolita"})
	allow := func(context.Context, string) bool { return true }

	soft := &jobs.Job{Name: "Lolita.1962.2160p.BluRay", InfoHash: "abc", UserID: "u1"}
	if BlockedAllow(context.Background(), nil, allow, soft) {
		t.Error("a soft flag on an allowlisted hash should be overridden")
	}
	if !BlockedAllow(context.Background(), nil, nil, soft) {
		t.Error("a soft flag with no allowlist should still block")
	}

	hard := &jobs.Job{Name: "hardbad release", InfoHash: "abc", UserID: "u1"}
	if !BlockedAllow(context.Background(), nil, allow, hard) {
		t.Error("a hard term must never be overridden by the allowlist")
	}

	clean := &jobs.Job{Name: "Interstellar.2014", InfoHash: "abc", UserID: "u1"}
	if BlockedAllow(context.Background(), nil, nil, clean) {
		t.Error("clean content should not be blocked")
	}
}
