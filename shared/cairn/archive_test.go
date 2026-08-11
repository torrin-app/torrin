package cairn

import (
	"context"
	"io"
	"testing"
)

type fakeStore struct{ id string }

func (*fakeStore) GetBytes(context.Context, string) ([]byte, error)         { return nil, nil }
func (*fakeStore) GetReader(context.Context, string) (io.ReadCloser, error) { return nil, nil }
func (*fakeStore) Put(context.Context, string, io.Reader, string) error     { return nil }

func TestNZBTarget(t *testing.T) {
	blob := &fakeStore{"blob"}
	r2 := &fakeStore{"r2"}

	a := NewArchiver(blob, r2, nil, nil)
	if store, db := a.nzbTarget([]byte("nzb")); store != Store(r2) || db != nil {
		t.Fatalf("with a durable store: target=%v db=%v", store, db)
	}

	a2 := NewArchiver(blob, nil, nil, nil)
	if store, db := a2.nzbTarget([]byte("nzb")); store != Store(blob) || string(db) != "nzb" {
		t.Fatalf("without one: target=%v db=%q", store, db)
	}
}
