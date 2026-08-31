package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/torrin-app/torrin/shared/nodestatus"
)

type fakeDB struct{ execs [][]any }

func (f *fakeDB) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	f.execs = append(f.execs, args)
	return pgconn.CommandTag{}, nil
}

func (f *fakeDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row { return nil }

func TestPolicyFromEnv(t *testing.T) {
	if p := policyFromEnv(); p.StandardTTL != 14 || p.PopularTTL != 45 || p.NeverAccessedTTL != 7 {
		t.Fatalf("defaults wrong: %+v", p)
	}
	t.Setenv("EVICTION_STANDARD_TTL", "30")
	t.Setenv("EVICTION_POPULAR_TTL", "90")
	t.Setenv("EVICTION_CAP_BYTES", "18000000000000")
	p := policyFromEnv()
	if p.StandardTTL != 30 || p.PopularTTL != 90 {
		t.Errorf("overrides not applied: standard=%d popular=%d", p.StandardTTL, p.PopularTTL)
	}
	if p.StorageCapBytes != 18000000000000 {
		t.Errorf("cap override not applied: %d", p.StorageCapBytes)
	}
	if p.NeverAccessedTTL != 7 {
		t.Errorf("unset field should keep default, got %d", p.NeverAccessedTTL)
	}
}

func TestReportOnceWritesDiskStatus(t *testing.T) {
	f := &fakeDB{}
	ns := nodestatus.New(time.Minute)
	if err := ns.SetDB(context.Background(), f); err != nil {
		t.Fatal(err)
	}

	reportOnce(context.Background(), ns, "box1", t.TempDir(), 1000)

	if len(f.execs) == 0 {
		t.Fatal("no report written")
	}
	last := f.execs[len(f.execs)-1]
	if len(last) != 4 || last[0] != "box1" {
		t.Fatalf("report args = %v, want node box1", last)
	}
	if total, ok := last[2].(int64); !ok || total <= 0 {
		t.Fatalf("total = %v, want > 0", last[2])
	}
	if minFree, ok := last[3].(int64); !ok || minFree != 1000 {
		t.Fatalf("min_free = %v, want 1000", last[3])
	}
}
