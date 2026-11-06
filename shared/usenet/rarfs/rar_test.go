package rarfs

import (
	"bytes"
	"io"
	"os"
	"testing"
)

type bytesVolume []byte

func (b bytesVolume) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(b)) {
		return 0, io.EOF
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (b bytesVolume) Size() (int64, error) { return int64(len(b)), nil }

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func loadVols(t *testing.T, names ...string) []Volume {
	t.Helper()
	vols := make([]Volume, len(names))
	for i, n := range names {
		vols[i] = bytesVolume(mustRead(t, n))
	}
	return vols
}

func findEntry(a *Archive, name string) *Entry {
	for i := range a.Entries {
		if a.Entries[i].Name == name {
			return &a.Entries[i]
		}
	}
	return nil
}

func readInner(t *testing.T, a *Archive, vols []Volume, name string) *Entry {
	t.Helper()
	e := findEntry(a, name)
	if e == nil {
		t.Fatalf("inner %q not found; got %+v", name, a.Entries)
	}
	return e
}

func assertPayload(t *testing.T, e *Entry, vols []Volume, want []byte) {
	t.Helper()
	if e.Size != int64(len(want)) {
		t.Fatalf("size = %d, want %d", e.Size, len(want))
	}
	got := make([]byte, e.Size)
	if _, err := io.ReadFull(io.NewSectionReader(e.ReaderAt(vols), 0, e.Size), got); err != nil {
		t.Fatalf("read inner: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("inner bytes differ from payload")
	}
}

func TestRar5StoreSingle(t *testing.T) {
	want := mustRead(t, "testdata/video.mkv")
	vols := loadVols(t, "testdata/store5_single.rar")
	a, err := Open(vols)
	if err != nil {
		t.Fatal(err)
	}
	e := readInner(t, a, vols, "video.mkv")
	if !e.Streamable {
		t.Fatal("store method must be streamable")
	}
	assertPayload(t, e, vols, want)
}

func TestRar5StoreMultiVolume(t *testing.T) {
	want := mustRead(t, "testdata/video.mkv")
	vols := loadVols(t, "testdata/store5_multi.part1.rar", "testdata/store5_multi.part2.rar", "testdata/store5_multi.part3.rar")
	a, err := Open(vols)
	if err != nil {
		t.Fatal(err)
	}
	e := readInner(t, a, vols, "video.mkv")
	if !e.Streamable {
		t.Fatal("store method must be streamable")
	}
	assertPayload(t, e, vols, want)
}

func TestRar5CompressedNotStreamable(t *testing.T) {
	vols := loadVols(t, "testdata/comp5.rar")
	a, err := Open(vols)
	if err != nil {
		t.Fatal(err)
	}
	e := readInner(t, a, vols, "video.mkv")
	if e.Streamable {
		t.Fatal("compressed method must not be flagged streamable")
	}
}

func TestRar4StoreSingle(t *testing.T) {
	want := mustRead(t, "testdata/video.mkv")
	vols := loadVols(t, "testdata/store4_single.rar")
	a, err := Open(vols)
	if err != nil {
		t.Fatal(err)
	}
	e := readInner(t, a, vols, "video.mkv")
	if !e.Streamable {
		t.Fatal("rar4 store method must be streamable")
	}
	assertPayload(t, e, vols, want)
}

func TestReaderAtRandomAccess(t *testing.T) {
	want := mustRead(t, "testdata/video.mkv")
	vols := loadVols(t, "testdata/store5_multi.part1.rar", "testdata/store5_multi.part2.rar", "testdata/store5_multi.part3.rar")
	a, err := Open(vols)
	if err != nil {
		t.Fatal(err)
	}
	e := readInner(t, a, vols, "video.mkv")
	ra := e.ReaderAt(vols)
	for _, off := range []int64{0, 1, 11999, 12000, 12001, 20000, 29999} {
		buf := make([]byte, 64)
		n, err := ra.ReadAt(buf, off)
		if err != nil && err != io.EOF {
			t.Fatalf("ReadAt(%d): %v", off, err)
		}
		if !bytes.Equal(buf[:n], want[off:off+int64(n)]) {
			t.Fatalf("ReadAt(%d) mismatch", off)
		}
	}
}
