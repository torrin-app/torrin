package main

import "context"

type gate struct{ ch chan struct{} }

func newGate(n int) *gate {
	if n < 1 {
		return nil
	}
	return &gate{ch: make(chan struct{}, n)}
}

func (g *gate) acquire(ctx context.Context) bool {
	if g == nil {
		return true
	}
	select {
	case g.ch <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (g *gate) release() {
	if g == nil {
		return
	}
	<-g.ch
}
