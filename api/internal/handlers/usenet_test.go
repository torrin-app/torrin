package handlers

import (
	"testing"

	"github.com/torrin-app/torrin/shared/plans"
)

func TestUsenetEntitled(t *testing.T) {
	cases := []struct {
		name     string
		ownCreds bool
		plan     plans.Plan
		want     bool
	}{
		{"paid byok plan, own creds", true, plans.Starter, true},
		{"paid byok plan, no creds", false, plans.Starter, false},
		{"system usenet plan, no creds", false, plans.Standard, true},
		{"free with own creds", true, plans.Free, false},
		{"free no creds", false, plans.Free, false},
	}
	for _, c := range cases {
		if got := usenetEntitled(c.ownCreds, c.plan); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
