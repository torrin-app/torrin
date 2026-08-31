package providers

import (
	"context"
	"testing"
)

func TestByteTally(t *testing.T) {
	ctx := context.Background()
	if TallyFrom(ctx) != nil {
		t.Fatal("bare ctx should have no tally")
	}
	bt := &ByteTally{}
	ctx = WithByteTally(ctx, bt)
	addBytes(ctx, 1000)
	addBytes(ctx, 500)
	addBytes(ctx, 0)
	addBytes(ctx, -5)
	if got := bt.Downloaded.Load(); got != 1500 {
		t.Fatalf("downloaded = %d, want 1500", got)
	}
	if !bt.Unpublished() {
		t.Error("should start unpublished")
	}
	bt.MarkPublished()
	if bt.Unpublished() {
		t.Error("should be published after MarkPublished")
	}
}
