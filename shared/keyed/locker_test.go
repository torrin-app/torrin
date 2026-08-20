package keyed

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSameKeySerializes(t *testing.T) {
	l := NewLocker()
	var running, maxSeen int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := l.Lock("h")
			defer unlock()
			n := atomic.AddInt32(&running, 1)
			if n > atomic.LoadInt32(&maxSeen) {
				atomic.StoreInt32(&maxSeen, n)
			}
			time.Sleep(time.Millisecond)
			atomic.AddInt32(&running, -1)
		}()
	}
	wg.Wait()
	if maxSeen != 1 {
		t.Errorf("same key ran %d in parallel, want 1", maxSeen)
	}
}

func TestDifferentKeysProceed(t *testing.T) {
	l := NewLocker()
	unlock := l.Lock("a")
	defer unlock()
	done := make(chan struct{})
	go func() {
		l.Lock("b")()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("different key blocked on a held key")
	}
}

func TestEntryReclaimedAfterUnlock(t *testing.T) {
	l := NewLocker()
	l.Lock("x")()
	l.mu.Lock()
	n := len(l.locks)
	l.mu.Unlock()
	if n != 0 {
		t.Errorf("locks map still holds %d entries after unlock, want 0", n)
	}
}
