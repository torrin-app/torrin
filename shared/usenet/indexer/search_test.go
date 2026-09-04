package indexer

import (
	"testing"
	"time"
)

func TestFallbackQuery(t *testing.T) {
	if got := FallbackQuery("Silo", 1, 2); got != "Silo S01E02" {
		t.Errorf("tv episode: got %q", got)
	}
	if got := FallbackQuery("Avatar The Way of Water", 0, 0); got != "Avatar The Way of Water" {
		t.Errorf("movie: got %q", got)
	}
	if got := FallbackQuery("Show", 2, 0); got != "Show" {
		t.Errorf("partial se: got %q", got)
	}
}

func TestSearchCache(t *testing.T) {
	searchCacheMu.Lock()
	searchCache = map[string]searchCacheEntry{}
	searchCacheMu.Unlock()

	if _, ok := searchCacheGet("k"); ok {
		t.Fatal("empty cache should miss")
	}
	res := []Result{{ID: "1", Title: "X"}}
	searchCacheSet("k", res)
	got, ok := searchCacheGet("k")
	if !ok || len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("expected hit, got %v %v", got, ok)
	}

	searchCacheMu.Lock()
	searchCache["k"] = searchCacheEntry{results: res, exp: time.Now().Add(-time.Minute)}
	searchCacheMu.Unlock()
	if _, ok := searchCacheGet("k"); ok {
		t.Fatal("expired entry should miss and be evicted")
	}
}
