package providers

import (
	"context"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestRateRegistryAggregate(t *testing.T) {
	r := NewRateRegistry()
	a := r.Acquire("u1", 1000)
	b := r.Acquire("u1", 1000)
	if a != b {
		t.Fatal("same user must share one limiter")
	}
	if c := r.Acquire("u2", 1000); c == a {
		t.Fatal("different users must not share a limiter")
	}
}

func TestRateRegistryRefcount(t *testing.T) {
	r := NewRateRegistry()
	r.Acquire("u1", 1000)
	r.Acquire("u1", 1000)
	r.Release("u1")
	if _, ok := r.m["u1"]; !ok {
		t.Fatal("entry dropped while still referenced")
	}
	r.Release("u1")
	if _, ok := r.m["u1"]; ok {
		t.Fatal("last release must delete the entry")
	}
}

func TestRateRegistryUnlimited(t *testing.T) {
	r := NewRateRegistry()
	if r.Acquire("u1", 0) != nil {
		t.Fatal("zero rate must yield a nil (unlimited) limiter")
	}
	if r.Acquire("", 1000) != nil {
		t.Fatal("empty user must yield a nil limiter")
	}
}

func TestLimiterContext(t *testing.T) {
	lim := rate.NewLimiter(1000, 1000)
	ctx := WithRateLimiter(context.Background(), lim)
	if LimiterFrom(ctx) != lim {
		t.Fatal("limiter not carried on context")
	}
	if LimiterFrom(context.Background()) != nil {
		t.Fatal("absent limiter must read as nil")
	}
}

func TestAggregateThrottles(t *testing.T) {
	const limit = 400_000
	const burst = 10_000
	const perG = 50_000
	const n = 4
	lim := rate.NewLimiter(rate.Limit(limit), burst)

	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			left := perG
			for left > 0 {
				c := 5000
				if c > left {
					c = left
				}
				if err := lim.WaitN(context.Background(), c); err != nil {
					t.Error(err)
					return
				}
				left -= c
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	min := time.Duration(float64(n*perG-burst) / float64(limit) * float64(time.Second) * 0.7)
	if elapsed < min {
		t.Fatalf("aggregate served %d bytes in %v, faster than shared cap allows (>= %v)", n*perG, elapsed, min)
	}
}
