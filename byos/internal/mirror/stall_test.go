package mirror

import (
	"context"
	"io"
	"testing"
	"time"
)

type blockingReader struct{ ch chan struct{} }

func (b blockingReader) Read(p []byte) (int, error) {
	<-b.ch
	return 0, io.EOF
}

func TestGuardStallCancelsOnNoProgress(t *testing.T) {
	pr := newProgressReader(blockingReader{make(chan struct{})})
	ctx, stop := guardStall(context.Background(), pr, 40*time.Millisecond)
	defer stop()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("stall guard did not cancel a stuck transfer")
	}
}

func TestGuardStallStaysOpenWithProgress(t *testing.T) {
	pr := newProgressReader(nil)
	ctx, stop := guardStall(context.Background(), pr, 60*time.Millisecond)
	defer stop()
	for i := 0; i < 6; i++ {
		pr.last.Store(time.Now().UnixNano())
		time.Sleep(20 * time.Millisecond)
	}
	if ctx.Err() != nil {
		t.Fatal("stall guard cancelled a transfer that was making progress")
	}
}

func TestGuardStallDisabled(t *testing.T) {
	pr := newProgressReader(nil)
	ctx, stop := guardStall(context.Background(), pr, 0)
	defer stop()
	if ctx.Err() != nil {
		t.Fatal("zero window must disable the guard")
	}
}
