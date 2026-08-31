package nodestatus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestFresh(t *testing.T) {
	max := 3 * time.Minute
	cases := []struct {
		age  float64
		want bool
	}{
		{0, true},
		{60, true},
		{180, true},
		{181, false},
		{600, false},
		{-1, false},
	}
	for _, c := range cases {
		if got := fresh(c.age, max); got != c.want {
			t.Errorf("fresh(%v, %v) = %v, want %v", c.age, max, got, c.want)
		}
	}
}

type fakeRow struct {
	n   int
	err error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) > 0 {
		if p, ok := dest[0].(*int); ok {
			*p = r.n
		}
	}
	return nil
}

type fakeDB struct {
	execs [][]any
	row   fakeRow
}

func (f *fakeDB) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	f.execs = append(f.execs, args)
	return pgconn.CommandTag{}, nil
}

func (f *fakeDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row { return f.row }

func TestReport(t *testing.T) {
	f := &fakeDB{}
	s := New(time.Minute)
	s.db = f
	if err := s.Report(context.Background(), "box2", 111, 222, 50); err != nil {
		t.Fatal(err)
	}
	if len(f.execs) != 1 {
		t.Fatalf("want 1 exec, got %d", len(f.execs))
	}
	a := f.execs[0]
	if a[0] != "box2" || a[1].(int64) != 111 || a[2].(int64) != 222 {
		t.Fatalf("report args = %v", a)
	}
}

func TestOtherHasRoom(t *testing.T) {
	ctx := context.Background()
	s := New(time.Minute)

	if s.OtherHasRoom(ctx, "", 100) {
		t.Fatal("no db -> false")
	}
	if s.OtherHasRoom(ctx, "", 0) {
		t.Fatal("minFree<=0 -> false")
	}

	s.db = &fakeDB{row: fakeRow{n: 1}}
	if !s.OtherHasRoom(ctx, "", 100) {
		t.Fatal("a sibling has room -> true")
	}

	s.db = &fakeDB{row: fakeRow{n: 0}}
	if s.OtherHasRoom(ctx, "", 100) {
		t.Fatal("no sibling room -> false")
	}

	s.db = &fakeDB{row: fakeRow{err: errors.New("boom")}}
	if s.OtherHasRoom(ctx, "", 100) {
		t.Fatal("query error -> false")
	}
}
