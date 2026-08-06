package bot

import (
	"testing"
	"time"
)

func TestDedupe(t *testing.T) {
	d := newDedupe(time.Minute)
	if !d.firstSight(1, 100) {
		t.Fatal("first sight should be true")
	}
	if d.firstSight(1, 100) {
		t.Error("same user+msg should be a duplicate")
	}
	if !d.firstSight(1, 101) {
		t.Error("different msg id should be a first sight")
	}
	if !d.firstSight(2, 100) {
		t.Error("different user should be a first sight")
	}
}

func TestDedupeExpiry(t *testing.T) {
	d := newDedupe(10 * time.Millisecond)
	d.firstSight(1, 100)
	time.Sleep(20 * time.Millisecond)
	if !d.firstSight(1, 100) {
		t.Error("should be a first sight again after ttl")
	}
}

func TestDocCacheKey(t *testing.T) {
	// Deterministic 40-hex (sha1) per Telegram doc id — the dedup key.
	a := docCacheKey(12345)
	if len(a) != 40 {
		t.Fatalf("want 40-hex, got %d chars", len(a))
	}
	if docCacheKey(12345) != a {
		t.Error("not deterministic")
	}
	if docCacheKey(54321) == a {
		t.Error("different ids collided")
	}
}

func TestUserLimiter(t *testing.T) {
	l := newUserLimiter()
	first := l.tryAcquire("u", 2)
	second := l.tryAcquire("u", 2)
	if !first || !second {
		t.Fatal("should allow 2")
	}
	if l.tryAcquire("u", 2) {
		t.Error("3rd should be denied")
	}
	l.release("u")
	if !l.tryAcquire("u", 2) {
		t.Error("should allow after release")
	}
}
