package main

import (
	"context"
	"testing"
	"time"
)

func TestGateNilIsUnlimited(t *testing.T) {
	var g *gate
	if !g.acquire(context.Background()) {
		t.Fatal("nil gate should always acquire")
	}
	g.release()
}

func TestGateLimitsAndReleases(t *testing.T) {
	g := newGate(2)
	ctx := context.Background()
	if !g.acquire(ctx) {
		t.Fatal("first acquire should succeed")
	}
	if !g.acquire(ctx) {
		t.Fatal("second acquire should succeed")
	}

	cctx, cancel := context.WithCancel(ctx)
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if g.acquire(cctx) {
		t.Fatal("acquire past capacity must fail when ctx is canceled")
	}

	g.release()
	if !g.acquire(ctx) {
		t.Fatal("should acquire after a release frees a slot")
	}
}
