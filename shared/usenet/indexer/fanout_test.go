package indexer

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestFanOutMergesAndTags(t *testing.T) {
	sources := []Source{{ID: "a"}, {ID: "b"}}
	got := FanOut(context.Background(), sources, time.Second, func(c *Client) ([]Result, error) {
		return []Result{{Title: "x"}}, nil
	})
	if len(got) != 2 {
		t.Fatalf("want 2 merged results, got %d", len(got))
	}
	seen := map[string]bool{}
	for _, r := range got {
		seen[r.Source] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("results not tagged by source: %+v", got)
	}
}

func TestFanOutToleratesFailure(t *testing.T) {
	var calls int32
	sources := []Source{{ID: "ok"}, {ID: "dead"}}
	got := FanOut(context.Background(), sources, time.Second, func(c *Client) ([]Result, error) {
		if atomic.AddInt32(&calls, 1) == 2 {
			return nil, errors.New("boom")
		}
		return []Result{{Title: "y"}}, nil
	})
	if len(got) != 1 {
		t.Fatalf("a dead source must not sink the search; got %d results", len(got))
	}
}

func TestFanOutAbandonsHungSource(t *testing.T) {
	sources := []Source{{ID: "slow"}}
	start := time.Now()
	got := FanOut(context.Background(), sources, 50*time.Millisecond, func(c *Client) ([]Result, error) {
		time.Sleep(2 * time.Second)
		return []Result{{Title: "late"}}, nil
	})
	if len(got) != 0 {
		t.Fatalf("hung source should be abandoned, got %d", len(got))
	}
	if time.Since(start) > time.Second {
		t.Fatalf("did not abandon at the per-source timeout")
	}
}

func TestFanOutRecoversPanic(t *testing.T) {
	sources := []Source{
		{ID: "ok", Client: NewTestClient("http://ok", "k")},
		{ID: "bad", Client: NewTestClient("http://bad", "k")},
	}
	got := FanOut(context.Background(), sources, time.Second, func(c *Client) ([]Result, error) {
		if c.BaseURL() == "http://bad" {
			panic("boom")
		}
		return []Result{{Title: "z"}}, nil
	})
	if len(got) != 1 {
		t.Fatalf("a panicking indexer must not crash the search; got %d results", len(got))
	}
}

func TestDedupKeepsHigherGrabs(t *testing.T) {
	in := []Result{
		{Title: "The.Show.S01E01.1080p", Size: 100, Grabs: 3},
		{Title: "The Show S01E01 1080p", Size: 100, Grabs: 9},
		{Title: "Other.Movie.2020", Size: 200, Grabs: 1},
	}
	out := Dedup(in)
	if len(out) != 2 {
		t.Fatalf("want 2 after dedup, got %d", len(out))
	}
	for _, r := range out {
		if r.Size == 100 && r.Grabs != 9 {
			t.Fatalf("dedup kept lower-grab duplicate: %+v", r)
		}
	}
}
