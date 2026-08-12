package handlers

import (
	"testing"

	"github.com/torrin-app/torrin/shared/plans"
)

func TestTombstoneBlocks(t *testing.T) {
	cases := []struct {
		name                       string
		explicit, tombstoned, want bool
	}{
		{"addon add, no tombstone", false, false, false},
		{"addon auto-regrab of deleted item", false, true, true},
		{"explicit add, no tombstone", true, false, false},
		{"explicit re-add of deleted item", true, true, false},
	}
	for _, c := range cases {
		if got := tombstoneBlocks(c.explicit, c.tombstoned); got != c.want {
			t.Errorf("%s: tombstoneBlocks(%v, %v) = %v, want %v", c.name, c.explicit, c.tombstoned, got, c.want)
		}
	}
}

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
