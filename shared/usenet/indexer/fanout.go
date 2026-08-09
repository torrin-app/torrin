package indexer

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type Source struct {
	ID     string
	Label  string
	Client *Client
}

func Find(sources []Source, id string) *Client {
	for i := range sources {
		if sources[i].ID == id {
			return sources[i].Client
		}
	}
	return nil
}

const fanoutConcurrency = 4

func FanOut(ctx context.Context, sources []Source, perTimeout time.Duration, fn func(*Client) ([]Result, error)) []Result {
	sem := make(chan struct{}, fanoutConcurrency)
	var mu sync.Mutex
	var merged []Result
	var wg sync.WaitGroup

	for _, src := range sources {
		wg.Add(1)
		go func(src Source) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			done := make(chan []Result, 1)
			go func() {
				res, err := fn(src.Client)
				if err != nil {
					slog.Warn("indexer search failed", "source", src.Label, "err", err)
					done <- nil
					return
				}
				done <- res
			}()

			var res []Result
			select {
			case res = <-done:
			case <-time.After(perTimeout):
				slog.Warn("indexer search timed out", "source", src.Label)
				return
			case <-ctx.Done():
				return
			}
			for i := range res {
				res[i].Source = src.ID
			}
			mu.Lock()
			merged = append(merged, res...)
			mu.Unlock()
		}(src)
	}
	wg.Wait()
	return merged
}
