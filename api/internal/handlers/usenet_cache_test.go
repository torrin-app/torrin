package handlers

import (
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/usenet/indexer"
)

func TestUserJobForHash(t *testing.T) {
	sibs := []*jobs.Job{
		{UserID: "other", Status: jobs.StatusComplete},
		{UserID: "u1", Status: jobs.StatusFailed},
		{UserID: "u1", Status: jobs.StatusComplete},
	}
	if j := userJobForHash(sibs, "u1"); j == nil || j.Status != jobs.StatusComplete {
		t.Fatalf("should return u1's non-failed job, got %+v", j)
	}
	if userJobForHash(sibs, "nobody") != nil {
		t.Error("no job for unknown user")
	}
	if userJobForHash([]*jobs.Job{{UserID: "u1", Status: jobs.StatusFailed}}, "u1") != nil {
		t.Error("a failed job should not count (allows retry)")
	}
}

func TestLockGrabSerializes(t *testing.T) {
	unlock := lockGrab("h1")
	done := make(chan struct{})
	go func() { u := lockGrab("h1"); u(); close(done) }()
	select {
	case <-done:
		t.Fatal("second grab of same hash should block until the first releases")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second grab should proceed after release")
	}
	// different hashes must not block each other
	a := lockGrab("a")
	b := lockGrab("b")
	a()
	b()
}

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
