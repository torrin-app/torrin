package jobs

import (
	"context"
	"testing"
	"time"
)

type fakeLister struct {
	all []*Job
}

func (f *fakeLister) ListByUser(_ context.Context, _ string, limit int) ([]*Job, error) {
	return f.slice(0, limit), nil
}

func (f *fakeLister) ListByUserBefore(_ context.Context, _ string, before time.Time, beforeID string, limit int) ([]*Job, error) {
	start := 0
	for i, j := range f.all {
		if j.CreatedAt.Equal(before) && j.ID == beforeID {
			start = i + 1
			break
		}
	}
	return f.slice(start, limit), nil
}

func (f *fakeLister) slice(start, limit int) []*Job {
	end := start + limit
	if end > len(f.all) {
		end = len(f.all)
	}
	if start >= end {
		return nil
	}
	return f.all[start:end]
}

func TestListAllDrainsEveryPage(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	var all []*Job
	for i := 0; i < listAllPage*2+37; i++ {
		all = append(all, &Job{ID: string(rune('a')) + itoa(i), CreatedAt: base.Add(-time.Duration(i) * time.Second)})
	}
	got, err := ListAll(context.Background(), &fakeLister{all: all}, "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(all) {
		t.Fatalf("drained %d, want %d (tail silently dropped)", len(got), len(all))
	}
	if got[0].ID != all[0].ID || got[len(got)-1].ID != all[len(all)-1].ID {
		t.Error("order not preserved across pages")
	}
}

func TestListAllShortCircuitsOnPartialPage(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	all := []*Job{{ID: "x", CreatedAt: base}, {ID: "y", CreatedAt: base.Add(-time.Second)}}
	got, err := ListAll(context.Background(), &fakeLister{all: all}, "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
