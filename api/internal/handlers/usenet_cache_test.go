package handlers

import (
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/usenet/indexer"
)

func TestUsenetCache(t *testing.T) {
	usenetCacheMu.Lock()
	usenetCache = map[string]usenetCacheEntry{}
	usenetCacheMu.Unlock()

	if _, ok := usenetCacheGet("k"); ok {
		t.Fatal("empty cache should miss")
	}
	res := []indexer.Result{{ID: "1", Title: "X"}}
	usenetCacheSet("k", res)
	got, ok := usenetCacheGet("k")
	if !ok || len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("expected hit, got %v %v", got, ok)
	}

	usenetCacheMu.Lock()
	usenetCache["k"] = usenetCacheEntry{results: res, exp: time.Now().Add(-time.Minute)}
	usenetCacheMu.Unlock()
	if _, ok := usenetCacheGet("k"); ok {
		t.Fatal("expired entry should miss and be evicted")
	}
}
