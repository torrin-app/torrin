package server

import (
	"context"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/cairn"
)

type dummyBlobs struct{}

func (dummyBlobs) GetBytes(context.Context, string) ([]byte, error) { return nil, nil }

type recordingBlobs struct{ gets int }

func (r *recordingBlobs) GetBytes(context.Context, string) ([]byte, error) {
	r.gets++
	return nil, nil
}

func TestBuildCairnFolderUsesLayout(t *testing.T) {
	blobs := &recordingBlobs{}
	s := &Server{cairnStore: blobs, cairns: newCairnCache()}
	it := auth.CairnItem{InfoHash: "hh", Name: "Show", Archived: true, Files: []auth.CairnFile{
		{Name: "ep1.mkv", Size: 111}, {Name: "ep2.mkv", Size: 222},
	}}
	fd, ok := s.buildCairnFolder(context.Background(), it)
	if !ok || len(fd.files) != 2 || fd.files[1].size != 222 || fd.name != "Show" {
		t.Fatalf("layout folder = %+v ok=%v", fd, ok)
	}
	if blobs.gets != 0 {
		t.Fatalf("must not fetch the nzb when layout is present, gets=%d", blobs.gets)
	}
}

func TestMergeCairns(t *testing.T) {
	s := &Server{cairnStore: dummyBlobs{}, cairns: newCairnCache()}
	s.cairns.m["u1"] = cairnEntry{at: time.Now(), folders: []cairnFolder{
		{hash: "cccccccccold", name: "Cold Show", mod: time.Unix(1700000000, 0), files: []cairnFile{
			{name: "ep1.mkv", idx: 0, size: 1000, enc: true},
			{name: "ep1.mkv", idx: 1, size: 2000, enc: true},
		}},
		{hash: "aaaaaaaa1111", name: "already cached", files: []cairnFile{{name: "x.mkv", idx: 0, size: 1}}},
	}}

	overrides := map[string]auth.WebdavOverride{
		auth.WebdavKey("cccccccccold", -1): {InfoHash: "cccccccccold", FileIndex: -1, Alias: "Renamed Cold"},
		auth.WebdavKey("cccccccccold", 0):  {InfoHash: "cccccccccold", FileIndex: 0, Alias: "ep1-renamed.mkv"},
		auth.WebdavKey("cccccccccold", 1):  {InfoHash: "cccccccccold", FileIndex: 1, Excluded: true},
	}
	tree := buildTree(sample(), nil)
	s.mergeCairns(context.Background(), "u1", tree, overrides)

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
	if cold.name != "Renamed Cold" {
		t.Fatalf("cairn folder rename not applied: name=%q", cold.name)
	}
	if len(cold.children) != 2 {
		t.Fatalf("cold folder files = %d, want 2", len(cold.children))
	}
	f := cold.children[0]
	if f.name != "ep1-renamed.mkv" || f.key != cairn.StreamPath("cccccccccold", 0, "ep1.mkv") {
		t.Fatalf("cairn file rename: name=%q key=%q (key must keep original name)", f.name, f.key)
	}
	if !f.cairn || !f.enc || f.size != 1000 {
		t.Fatalf("bad cairn file node: cairn=%v enc=%v size=%d", f.cairn, f.enc, f.size)
	}
	if !cold.children[1].hidden {
		t.Error("cairn file hide override not applied")
	}
}
