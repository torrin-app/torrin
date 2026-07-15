package handlers

import (
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
)

func TestCanCairn(t *testing.T) {
	active := &auth.User{ExpiresAt: time.Now().Add(time.Hour)}
	cases := []struct {
		name string
		plan plans.Plan
		user *auth.User
		want bool
	}{
		{"free excluded", plans.Free, active, false},
		{"paid monthly ok", plans.Plan{ID: "standard"}, &auth.User{Recurrence: "monthly"}, true},
		{"paid lifetime ok", plans.Plan{ID: "pro"}, &auth.User{Recurrence: "lifetime"}, true},
		{"day plan excluded", plans.Plan{ID: "standard"}, &auth.User{Recurrence: "days"}, false},
	}
	for _, c := range cases {
		if got := canCairn(c.plan, c.user); got != c.want {
			t.Errorf("%s: canCairn = %v, want %v", c.name, got, c.want)
		}
	}
}
