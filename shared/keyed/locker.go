package keyed

import "sync"

type ref struct {
	mu   sync.Mutex
	refs int
}

type Locker struct {
	mu    sync.Mutex
	locks map[string]*ref
}

func NewLocker() *Locker { return &Locker{locks: map[string]*ref{}} }

func (l *Locker) Lock(key string) func() {
	l.mu.Lock()
	r := l.locks[key]
	if r == nil {
		r = &ref{}
		l.locks[key] = r
	}
	r.refs++
	l.mu.Unlock()

	r.mu.Lock()
	return func() {
		r.mu.Unlock()
		l.mu.Lock()
		r.refs--
		if r.refs == 0 {
			delete(l.locks, key)
		}
		l.mu.Unlock()
	}
}

var std = NewLocker()

func Lock(key string) func() { return std.Lock(key) }
