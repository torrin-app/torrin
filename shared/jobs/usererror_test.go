package jobs

import (
	"strings"
	"testing"
)

func TestUserErrorNoLeaks(t *testing.T) {
	leaky := []string{
		`nzbget history: Post "http://nzbget:6789/jsonrpc": dial tcp 172.18.0.23:6789: connect: connection refused`,
		`connect usenet: dial tcp news.eweka.nl:563: timeout`,
		`download: segment ac8349fc@nyuu after 3 attempts`,
		`part 1/10: alldebrid LINK_HOST_UNAVAILABLE`,
	}
	for _, in := range leaky {
		out := UserError(in)
		for _, secret := range []string{"http://", "dial tcp", "172.18", "nzbget", "eweka", "@nyuu", "jsonrpc", "6789"} {
			if strings.Contains(strings.ToLower(out), secret) {
				t.Fatalf("UserError leaked %q in output %q (from %q)", secret, out, in)
			}
		}
	}
}

func TestUserErrorKeepsSafe(t *testing.T) {
	if got := UserError("size 90GB exceeds your plan limit of 50GB"); !strings.Contains(got, "plan limit") {
		t.Fatalf("plan-size message should pass through, got %q", got)
	}
	if got := UserError("season incomplete: episode 5/10 missing on indexer"); strings.Contains(got, "indexer") {
		t.Fatalf("should not leak 'indexer', got %q", got)
	}
}
