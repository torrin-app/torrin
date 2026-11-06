package eviction

import (
	"context"
	"fmt"
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
)

func TestReclaimToFreeBoundedByDeficit(t *testing.T) {
	orig := statFree
	defer func() { statFree = orig }()
	statFree = func(string) (int64, int64, bool) { return 500_000_000_000, 28_000_000_000_000, true }

	var cands []jobs.EvictionCandidate
	for i := 0; i < 20; i++ {
		cands = append(cands, jobs.EvictionCandidate{InfoHash: fmt.Sprintf("h%d", i), FileSize: 400_000_000_000, DaysSinceAccess: 30})
	}
	repo := &fakeRepo{evicted: map[string]bool{}, candidates: cands}
	store := &fakeStorage{deleted: map[string]bool{}}

	evicted, freed := New(repo, store, DefaultPolicy, "").reclaimToFree(context.Background(), "/mnt/cache", 2_000_000_000_000)

	if evicted == 0 {
		t.Fatal("should evict enough to cover the 1.5TB deficit")
	}
	if evicted > 5 {
		t.Errorf("over-evicted: %d items / %dGB for a 1.5TB deficit; a laggy free-space reading must not nuke the whole cache", evicted, freed/1e9)
	}
}
