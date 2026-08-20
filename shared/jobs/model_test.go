package jobs

import (
	"context"
	"testing"
)

func TestColdPullAllowedUnlimited(t *testing.T) {
	p := &Postgres{}
	if ok, err := p.ColdPullAllowed(context.Background(), "u1", 0); !ok || err != nil {
		t.Errorf("perHour=0 should be unlimited (true, nil); got %v, %v", ok, err)
	}
}

func TestStatusActive(t *testing.T) {
	for _, s := range []Status{StatusPending, StatusQueued, StatusDownloading, StatusProcessing, StatusPublishing, StatusSeeding} {
		if !s.Active() {
			t.Errorf("%s should be active", s)
		}
	}
	for _, s := range []Status{StatusComplete, StatusFailed, StatusEvicted} {
		if s.Active() {
			t.Errorf("%s should not be active", s)
		}
	}
}

func TestActiveStatesSQL(t *testing.T) {
	if activeStates != "('pending','queued','downloading','processing','publishing','seeding')" {
		t.Errorf("activeStates drifted: %s", activeStates)
	}
	if downloadingStates != "('pending','downloading','processing','publishing')" {
		t.Errorf("downloadingStates drifted: %s", downloadingStates)
	}
}

// Postgres is the Repository implementation; this fails to compile if it drifts.
var _ Repository = (*Postgres)(nil)
