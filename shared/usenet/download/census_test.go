package download

import (
	"context"
	"fmt"
	"testing"

	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

func censusSegs(n int) []nzb.Segment {
	segs := make([]nzb.Segment, n)
	for i := range segs {
		segs[i] = nzb.Segment{MessageID: fmt.Sprintf("m%d", i)}
	}
	return segs
}

func TestCensusVerdicts(t *testing.T) {
	ctx := context.Background()
	segs := censusSegs(100)

	if v := census(ctx, func(string) bool { return true }, segs, 24); v != CensusClean {
		t.Errorf("all present should be clean, got %v", v)
	}
	if v := census(ctx, func(string) bool { return false }, segs, 24); v != CensusUnstreamable {
		t.Errorf("all missing should be unstreamable, got %v", v)
	}
	miss := map[string]bool{"m0": true}
	if v := census(ctx, func(id string) bool { return !miss[id] }, segs, 24); v != CensusDegraded {
		t.Errorf("one missing should be degraded, got %v", v)
	}
	if v := census(ctx, func(string) bool { return true }, nil, 24); v != CensusUnstreamable {
		t.Errorf("empty should be unstreamable, got %v", v)
	}
}
