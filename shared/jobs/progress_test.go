package jobs

import (
	"context"
	"testing"
)

type fakeProg struct {
	pcts   []float64
	speeds []int64
}

func (f *fakeProg) SetProgress(_ context.Context, _ string, pct float64, speed int64) error {
	f.pcts = append(f.pcts, pct)
	f.speeds = append(f.speeds, speed)
	return nil
}

func TestProgressReporterFractional(t *testing.T) {
	f := &fakeProg{}
	rep := ProgressReporter(context.Background(), f, "j1")
	total := int64(306) * 1_000_000_000 // 306 GB

	rep(545_000_000, total)   // ~0.17% -> 0.1 (would be 0 under integer-percent)
	rep(600_000_000, total)   // still 0.1 tenths + within refresh window -> skipped
	rep(3_100_000_000, total) // ~1.01% -> 1.0 -> fires on change

	if len(f.pcts) != 2 {
		t.Fatalf("want 2 updates (first + tenth-percent change), got %d: %v", len(f.pcts), f.pcts)
	}
	if f.pcts[0] != 0.1 {
		t.Errorf("first pct = %v, want 0.1 (fractional, not a frozen 0)", f.pcts[0])
	}
	if f.pcts[1] != 1.0 {
		t.Errorf("second pct = %v, want 1.0", f.pcts[1])
	}
}

func TestProgressReporterIgnoresZeroTotal(t *testing.T) {
	f := &fakeProg{}
	rep := ProgressReporter(context.Background(), f, "j1")
	rep(100, 0)
	if len(f.pcts) != 0 {
		t.Errorf("zero total should not report, got %v", f.pcts)
	}
}
