package providers

import (
	"context"

	"golang.org/x/time/rate"
	"sync"
)

type RateRegistry struct {
	mu sync.Mutex
	m  map[string]*rateEntry
}

type rateEntry struct {
	lim  *rate.Limiter
	refs int
}

func NewRateRegistry() *RateRegistry {
	return &RateRegistry{m: make(map[string]*rateEntry)}
}

func (r *RateRegistry) Acquire(userID string, bytesPerSec int64) *rate.Limiter {
	if bytesPerSec <= 0 || userID == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.m[userID]
	if e == nil {
		e = &rateEntry{lim: rate.NewLimiter(rate.Limit(bytesPerSec), int(bytesPerSec))}
		r.m[userID] = e
	}
	e.refs++
	return e.lim
}

func (r *RateRegistry) Release(userID string) {
	if userID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.m[userID]
	if e == nil {
		return
	}
	e.refs--
	if e.refs <= 0 {
		delete(r.m, userID)
	}
}

type limiterKey struct{}

func WithRateLimiter(ctx context.Context, lim *rate.Limiter) context.Context {
	if lim == nil {
		return ctx
	}
	return context.WithValue(ctx, limiterKey{}, lim)
}

func LimiterFrom(ctx context.Context) *rate.Limiter {
	lim, _ := ctx.Value(limiterKey{}).(*rate.Limiter)
	return lim
}
