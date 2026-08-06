package download

import "testing"

func TestAcquireSharedRefCount(t *testing.T) {
	sharedMu.Lock()
	sharedPools = map[string]*sharedEntry{}
	sharedMu.Unlock()

	c := Credentials{Host: "test.invalid", Port: 119, Username: "u", MaxConns: 4}
	key := c.Host + "|" + c.Username

	refs := func() int {
		sharedMu.Lock()
		defer sharedMu.Unlock()
		if e, ok := sharedPools[key]; ok {
			return e.refs
		}
		return -1
	}

	p1, rel1, err := AcquireShared(c)
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	p2, rel2, err := AcquireShared(c)
	if err != nil {
		t.Fatalf("acquire 2: %v", err)
	}
	if p1 != p2 {
		t.Fatal("concurrent acquirers must share one pool")
	}
	if got := refs(); got != 2 {
		t.Fatalf("refs = %d, want 2", got)
	}

	rel1()
	if got := refs(); got != 1 {
		t.Fatalf("after one release refs = %d, want 1 (pool must stay while held)", got)
	}

	rel2()
	if got := refs(); got != -1 {
		t.Fatalf("after last release pool must be removed, refs = %d", got)
	}

	rel2()
	if got := refs(); got != -1 {
		t.Fatal("double release must be a no-op")
	}

	p3, rel3, err := AcquireShared(c)
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	if p3 == p1 {
		t.Fatal("re-acquire after full release must build a fresh pool, not reuse the closed one")
	}
	rel3()
}

func TestIsOptional(t *testing.T) {
	skip := []string{"cover.jpg", "poster.PNG", "info.nfo", "files.sfv", "readme.txt"}
	keep := []string{"movie.mkv", "movie.part01.rar", "data.r00", "archive.par2", "ep.s01e02.mp4"}
	for _, n := range skip {
		if !isOptional(n) {
			t.Errorf("%q should be optional (skipped)", n)
		}
	}
	for _, n := range keep {
		if isOptional(n) {
			t.Errorf("%q should NOT be optional (par2 kept for repair, media kept)", n)
		}
	}
}
