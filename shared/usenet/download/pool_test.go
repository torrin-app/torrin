package download

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Tensai75/nntpPool"
)

type fakePool struct {
	mu     sync.Mutex
	closed bool
}

func (f *fakePool) Conns() (uint32, uint32)                         { return 0, 0 }
func (f *fakePool) MaxConns() uint32                                { return 0 }
func (f *fakePool) Get(context.Context) (*nntpPool.NNTPConn, error) { return nil, nil }
func (f *fakePool) Put(*nntpPool.NNTPConn)                          {}
func (f *fakePool) Close()                                          { f.mu.Lock(); f.closed = true; f.mu.Unlock() }
func (f *fakePool) isClosed() bool                                  { f.mu.Lock(); defer f.mu.Unlock(); return f.closed }

func TestSharedPoolLingersAndReuses(t *testing.T) {
	origPool, origLinger := newPool, poolLinger
	t.Cleanup(func() {
		newPool, poolLinger = origPool, origLinger
		sharedMu.Lock()
		sharedPools = map[string]*sharedEntry{}
		sharedMu.Unlock()
	})
	sharedMu.Lock()
	sharedPools = map[string]*sharedEntry{}
	sharedMu.Unlock()

	fp := &fakePool{}
	made := 0
	newPool = func(Credentials) (nntpPool.ConnectionPool, error) { made++; return fp, nil }
	poolLinger = 30 * time.Millisecond

	c := Credentials{Host: "h", Username: "u"}
	p1, rel1, err := AcquireShared(c)
	if err != nil {
		t.Fatal(err)
	}
	rel1()

	p2, rel2, err := AcquireShared(c)
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 || made != 1 || fp.isClosed() {
		t.Fatalf("expected warm reuse within linger: made=%d closed=%v same=%v", made, fp.isClosed(), p1 == p2)
	}
	rel2()

	time.Sleep(80 * time.Millisecond)
	if !fp.isClosed() {
		t.Error("pool should close after linger with no refs")
	}
	sharedMu.Lock()
	_, still := sharedPools["h|u"]
	sharedMu.Unlock()
	if still {
		t.Error("pool should be evicted from the shared map after linger")
	}
}
