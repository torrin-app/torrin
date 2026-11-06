package main

import (
	"testing"

	"github.com/torrin-app/torrin/shared/plans"
)

func TestUsenetJobEntitled(t *testing.T) {
	starter := plans.Plan{ID: "starter"} // paid, CanBYOK, no system usenet
	pro := plans.Plan{ID: "pro", SystemUsenet: true}
	free := plans.Plan{ID: "free"}
	cases := []struct {
		name     string
		plan     plans.Plan
		hasCairn bool
		hasCreds bool
		want     bool
	}{
		{"starter byok provider", starter, false, true, true},
		{"starter no creds", starter, false, false, false},
		{"starter via cairn", starter, true, false, true},
		{"pro system usenet", pro, false, false, true},
		{"free no creds", free, false, false, false},
		{"free with creds (not byok)", free, false, true, false},
	}
	for _, c := range cases {
		if got := usenetJobEntitled(c.plan, c.hasCairn, c.hasCreds); got != c.want {
			t.Errorf("%s: usenetJobEntitled=%v want %v", c.name, got, c.want)
		}
	}
}
