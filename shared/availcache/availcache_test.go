package availcache

import (
	"context"
	"testing"
	"time"
)

func TestCheckCachedMemoizes(t *testing.T) {
	c := New(time.Minute)
	calls := 0
	q := func(_ context.Context, _ []string) map[string]bool {
		calls++
		return map[string]bool{"aabb": true}
	}
	r1 := c.CheckCached(context.Background(), "p:", []string{"AABB", "CCDD"}, q)
	if !r1["aabb"] || r1["ccdd"] {
		t.Fatalf("r1 = %v", r1)
	}
	r2 := c.CheckCached(context.Background(), "p:", []string{"aabb", "ccdd"}, q)
	if !r2["aabb"] || r2["ccdd"] {
		t.Fatalf("r2 = %v", r2)
	}
	if calls != 1 {
		t.Fatalf("query ran %d times, want 1 (the negative for ccdd should be cached too)", calls)
	}
}

func TestCheckCachedNoCacheOnFailure(t *testing.T) {
	c := New(time.Minute)
	calls := 0
	q := func(_ context.Context, _ []string) map[string]bool { calls++; return nil }
	c.CheckCached(context.Background(), "p:", []string{"aabb"}, q)
	c.CheckCached(context.Background(), "p:", []string{"aabb"}, q)
	if calls != 2 {
		t.Fatalf("query ran %d times, want 2 (failed lookups must not be cached)", calls)
	}
}
