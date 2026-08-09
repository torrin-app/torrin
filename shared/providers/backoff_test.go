package providers

import (
	"context"
	"testing"
	"time"
)

func TestBackoffRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	Backoff(ctx, 6)
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("backoff should return immediately on a cancelled context")
	}
}

func TestBackoffWaits(t *testing.T) {
	start := time.Now()
	Backoff(context.Background(), 0)
	if d := time.Since(start); d < 900*time.Millisecond {
		t.Fatalf("expected ~1s backoff, got %v", d)
	}
}
