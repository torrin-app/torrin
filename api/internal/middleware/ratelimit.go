package middleware

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

type limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rps     rate.Limit
	burst   int
	exempt  map[string]bool
}

func RateLimit(rps, burst int, exempt []string) func(http.Handler) http.Handler {
	if rps <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	l := &limiter{
		buckets: map[string]*bucket{},
		rps:     rate.Limit(rps),
		burst:   burst,
		exempt:  map[string]bool{},
	}
	for _, e := range exempt {
		if e != "" {
			l.exempt[e] = true
		}
	}
	go l.sweep()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := extractAPIKey(r)
			if key != "" && l.exempt[key] {
				next.ServeHTTP(w, r)
				return
			}
			if key == "" {
				key = EdgeIP(r)
			}
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !l.get(key).Allow() {
				w.Header().Set("Retry-After", "1")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"rate limit exceeded"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (l *limiter) get(key string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{lim: rate.NewLimiter(l.rps, l.burst)}
		l.buckets[key] = b
	}
	b.seen = time.Now()
	return b.lim
}

func (l *limiter) sweep() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-10 * time.Minute)
		l.mu.Lock()
		for k, b := range l.buckets {
			if b.seen.Before(cutoff) {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}
