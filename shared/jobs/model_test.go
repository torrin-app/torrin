package jobs

import "testing"

func TestStatusActive(t *testing.T) {
	for _, s := range []Status{StatusPending, StatusQueued, StatusDownloading, StatusProcessing, StatusPublishing} {
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
	if activeStates != "('pending','queued','downloading','processing','publishing')" {
		t.Errorf("activeStates drifted: %s", activeStates)
	}
}

// Postgres is the Repository implementation; this fails to compile if it drifts.
var _ Repository = (*Postgres)(nil)
