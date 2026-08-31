package providers

import (
	"context"
	"sync/atomic"
)

type ByteTally struct {
	Downloaded atomic.Int64
	published  atomic.Bool
}

func (t *ByteTally) MarkPublished()    { t.published.Store(true) }
func (t *ByteTally) Unpublished() bool { return !t.published.Load() }

type tallyKey struct{}

func WithByteTally(ctx context.Context, t *ByteTally) context.Context {
	if t == nil {
		return ctx
	}
	return context.WithValue(ctx, tallyKey{}, t)
}

func TallyFrom(ctx context.Context) *ByteTally {
	t, _ := ctx.Value(tallyKey{}).(*ByteTally)
	return t
}

func addBytes(ctx context.Context, n int) {
	if n <= 0 {
		return
	}
	if t := TallyFrom(ctx); t != nil {
		t.Downloaded.Add(int64(n))
	}
}
