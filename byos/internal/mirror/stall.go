package mirror

import (
	"context"
	"io"
	"sync/atomic"
	"time"
)

type progressReader struct {
	r    io.Reader
	last atomic.Int64
}

func newProgressReader(r io.Reader) *progressReader {
	p := &progressReader{r: r}
	p.last.Store(time.Now().UnixNano())
	return p
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.last.Store(time.Now().UnixNano())
	}
	return n, err
}

func (p *progressReader) idle() time.Duration {
	return time.Since(time.Unix(0, p.last.Load()))
}

func guardStall(ctx context.Context, pr *progressReader, window time.Duration) (context.Context, func()) {
	if window <= 0 {
		return ctx, func() {}
	}
	gctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(window / 4)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-gctx.Done():
				return
			case <-t.C:
				if pr.idle() >= window {
					cancel()
					return
				}
			}
		}
	}()
	return gctx, func() { close(done); cancel() }
}
