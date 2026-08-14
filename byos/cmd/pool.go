package main

import "sync"

type dispatcher struct {
	inFlight sync.Map
	sem      chan struct{}
	perGroup int
	mu       sync.Mutex
	groupN   map[string]int
}

func newDispatcher(parallel, perGroup int) *dispatcher {
	if parallel < 1 {
		parallel = 1
	}
	return &dispatcher{sem: make(chan struct{}, parallel), perGroup: perGroup, groupN: map[string]int{}}
}

func (p *dispatcher) reserveGroup(g string) bool {
	if p.perGroup < 1 {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.groupN[g] >= p.perGroup {
		return false
	}
	p.groupN[g]++
	return true
}

func (p *dispatcher) releaseGroup(g string) {
	if p.perGroup < 1 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.groupN[g]--; p.groupN[g] <= 0 {
		delete(p.groupN, g)
	}
}

func (p *dispatcher) submit(id, group string, run func()) bool {
	if _, busy := p.inFlight.LoadOrStore(id, true); busy {
		return false
	}
	if !p.reserveGroup(group) {
		p.inFlight.Delete(id)
		return false
	}
	select {
	case p.sem <- struct{}{}:
		go func() {
			defer func() { <-p.sem; p.releaseGroup(group); p.inFlight.Delete(id) }()
			run()
		}()
		return true
	default:
		p.releaseGroup(group)
		p.inFlight.Delete(id)
		return false
	}
}
