package torrentfile

import (
	"bytes"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

func build(t *testing.T, private bool) []byte {
	t.Helper()
	info := metainfo.Info{Name: "movie.mkv", PieceLength: 16384, Length: 1024, Pieces: make([]byte, 20)}
	if private {
		p := true
		info.Private = &p
	}
	b, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := (&metainfo.MetaInfo{InfoBytes: b}).Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParsePrivate(t *testing.T) {
	m, err := Parse(build(t, true))
	if err != nil {
		t.Fatal(err)
	}
	if !m.Private {
		t.Error("expected private=true")
	}
	if m.Name != "movie.mkv" || m.Size != 1024 {
		t.Errorf("name=%q size=%d", m.Name, m.Size)
	}
	if len(m.InfoHash) != 40 {
		t.Errorf("infohash = %q", m.InfoHash)
	}
	if len(m.Files) != 1 || m.Files[0].Path != "movie.mkv" || m.Files[0].Size != 1024 {
		t.Errorf("files = %+v", m.Files)
	}
}

func TestParsePublic(t *testing.T) {
	m, err := Parse(build(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if m.Private {
		t.Error("expected private=false")
	}
}

func TestSubsetBySize(t *testing.T) {
	have := []int64{100, 200, 300}
	if !SubsetBySize([]int64{100, 200, 300}, have) {
		t.Error("exact match should be true")
	}
	if !SubsetBySize([]int64{200}, have) {
		t.Error("strict subset should be true")
	}
	if SubsetBySize([]int64{100, 999}, have) {
		t.Error("missing size should be false")
	}
	if SubsetBySize([]int64{100, 100}, have) {
		t.Error("needs two of a size but have has one → false")
	}
	if SubsetBySize(nil, have) {
		t.Error("empty want should be false")
	}
}

func TestParseGarbage(t *testing.T) {
	if _, err := Parse([]byte("not a torrent")); err == nil {
		t.Error("expected error on garbage input")
	}
}
