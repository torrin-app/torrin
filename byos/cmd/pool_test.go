package main

import (
	"runtime"
	"sync"
	"testing"
)

func TestDispatcherDedupsInFlight(t *testing.T) {
	p := newDispatcher(4, 0)
	release := make(chan struct{})
	defer close(release)
	if !p.submit("a", "u1", func() { <-release }) {
		t.Fatal("first submit should run")
	}
	if p.submit("a", "u1", func() { t.Error("duplicate must not run") }) {
		t.Fatal("in-flight id must not be dispatched twice")
	}
}

func TestDispatcherRespectsCap(t *testing.T) {
	p := newDispatcher(2, 0)
	release := make(chan struct{})
	defer close(release)
	var started sync.WaitGroup
	started.Add(2)
	for _, id := range []string{"a", "b"} {
		if !p.submit(id, "u1", func() { started.Done(); <-release }) {
			t.Fatalf("submit %s should run", id)
		}
	}
	started.Wait()
	if p.submit("c", "u2", func() { t.Error("over-cap must not run") }) {
		t.Fatal("submit past the cap must be rejected")
	}
}

func TestDispatcherFreesSlot(t *testing.T) {
	p := newDispatcher(1, 0)
	ran := make(chan struct{})
	if !p.submit("a", "u1", func() { close(ran) }) {
		t.Fatal("first submit should run")
	}
	<-ran
	for i := 0; i < 10000; i++ {
		if p.submit("b", "u1", func() {}) {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("slot never freed after job finished")
}

func TestDispatcherPerUserCap(t *testing.T) {
	p := newDispatcher(4, 1)
	release := make(chan struct{})
	defer close(release)
	if !p.submit("a", "u1", func() { <-release }) {
		t.Fatal("first item for u1 should run")
	}
	if p.submit("b", "u1", func() { t.Error("second u1 item must not run past per-user cap") }) {
		t.Fatal("one user must not exceed its per-user slot cap")
	}
	if !p.submit("c", "u2", func() { <-release }) {
		t.Fatal("a different user must still get a free global slot")
	}
}

func TestDispatcherPerUserCapReleases(t *testing.T) {
	p := newDispatcher(4, 1)
	ran := make(chan struct{})
	if !p.submit("a", "u1", func() { close(ran) }) {
		t.Fatal("first u1 item should run")
	}
	<-ran
	for i := 0; i < 10000; i++ {
		if p.submit("b", "u1", func() {}) {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("per-user slot never released after job finished")
}
