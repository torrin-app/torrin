package jobs

import "testing"

func TestBuildNodeStats(t *testing.T) {
	out := buildNodeStats(
		[]nodeStatusRow{
			{Node: "", Status: "complete", Count: 2695},
			{Node: "", Status: "downloading", Count: 8},
			{Node: "box2", Status: "complete", Count: 13},
			{Node: "box2", Status: "downloading", Count: 2},
		},
		[]nodeCacheRow{
			{Node: "", Bytes: 21_470_000_000_000},
			{Node: "box2", Bytes: 230_000_000_000},
		},
	)

	if len(out) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(out))
	}
	if out[0].Node != "box1" {
		t.Fatalf("empty node should normalize to box1 and sort first, got %q", out[0].Node)
	}
	if out[0].CachedBytes != 21_470_000_000_000 || out[0].ByStatus["downloading"] != 8 {
		t.Fatalf("box1 stats wrong: %+v", out[0])
	}
	if out[1].Node != "box2" || out[1].CachedBytes != 230_000_000_000 || out[1].ByStatus["complete"] != 13 || out[1].ByStatus["downloading"] != 2 {
		t.Fatalf("box2 stats wrong: %+v", out[1])
	}
}
