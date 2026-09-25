package server

import (
	"context"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/cairn"
)

type dummyBlobs struct{}

func (dummyBlobs) GetBytes(context.Context, string) ([]byte, error) { return nil, nil }

func TestMergeCairns(t *testing.T) {
	s := &Server{cairnStore: dummyBlobs{}, cairns: newCairnCache()}
	s.cairns.m["u1"] = cairnEntry{at: time.Now(), folders: []cairnFolder{
		{hash: "cccccccccold", name: "Cold Show", mod: time.Unix(1700000000, 0), files: []cairnFile{
			{name: "ep1.mkv", idx: 0, size: 1000, enc: true},
			{name: "ep1.mkv", idx: 1, size: 2000, enc: true},
		}},
		{hash: "aaaaaaaa1111", name: "already cached", files: []cairnFile{{name: "x.mkv", idx: 0, size: 1}}},
	}}

	tree := buildTree(sample(), nil)
	s.mergeCairns(context.Background(), "u1", tree)

	var cold *node
	dupes := 0
	for _, c := range tree.children {
		if c.hash == "cccccccccold" {
			cold = c
		}
		if c.hash == "aaaaaaaa1111" {
			dupes++
		}
	}
	if dupes != 1 {
		t.Fatalf("already-cached hash must not be re-added from cairn, got %d folders", dupes)
	}
	if cold == nil || cold.idx != -1 {
		t.Fatalf("cold cairn folder should be added as a dir, got %+v", cold)
	}
	if len(cold.children) != 2 {
		t.Fatalf("cold folder files = %d, want 2", len(cold.children))
	}
	f := cold.children[0]
	if !f.cairn || !f.enc || f.size != 1000 || f.key != cairn.StreamPath("cccccccccold", 0, "ep1.mkv") {
		t.Fatalf("bad cairn file node: cairn=%v enc=%v size=%d key=%q", f.cairn, f.enc, f.size, f.key)
	}
	if cold.children[1].name == cold.children[0].name {
		t.Error("duplicate file names within a cairn folder must be disambiguated")
	}
}
