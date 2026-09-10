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
	if concurrencyStates != "('pending','downloading','processing','publishing')" {
		t.Errorf("concurrencyStates drifted: %s", concurrencyStates)
	}
	if downloadingStates != "('pending','downloading','processing','publishing')" {
		t.Errorf("downloadingStates drifted: %s", downloadingStates)
	}
	if budgetStates != "('pending','downloading','processing','publishing','seeding')" {
		t.Errorf("budgetStates drifted: %s", budgetStates)
	}
}

func TestConsumesDownloadSlot(t *testing.T) {
	for _, st := range []Status{StatusPending, StatusDownloading, StatusProcessing, StatusPublishing} {
		if !(&Job{Status: st}).ConsumesDownloadSlot() {
			t.Errorf("%s should consume a download slot", st)
		}
		if (&Job{Status: st, Seed: true}).ConsumesDownloadSlot() {
			t.Errorf("seed job in %s must not consume a download slot", st)
		}
	}
	for _, st := range []Status{StatusQueued, StatusSeeding, StatusComplete, StatusFailed, StatusEvicted} {
		if (&Job{Status: st}).ConsumesDownloadSlot() {
			t.Errorf("%s must not consume a download slot", st)
		}
	}
}

// Postgres is the Repository implementation; this fails to compile if it drifts.
var _ Repository = (*Postgres)(nil)
