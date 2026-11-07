package usenet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"sync"
	"testing"

	"github.com/madcowfred/yencode"
	"github.com/torrin-app/torrin/shared/usenet/decoder"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

type fakeFetcher struct {
	articles map[string][]byte
	err      error
	mu       sync.Mutex
	calls    []string
	groups   []string
}

func (f *fakeFetcher) Fetch(_ context.Context, id, group string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, id)
	f.groups = append(f.groups, group)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.articles[id], nil
}

func (f *fakeFetcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeFetcher) duplicated() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := map[string]bool{}
	for _, c := range f.calls {
		if seen[c] {
			return true
		}
		seen[c] = true
	}
	return false
}

func (f *fakeFetcher) lastGroup() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.groups) == 0 {
		return ""
	}
	return f.groups[len(f.groups)-1]
}

func article(data []byte, part, total int, begin int64) []byte {
	var body bytes.Buffer
	yencode.Encode(data, &body)
	return []byte(fmt.Sprintf("=ybegin part=%d total=%d line=128 size=%d name=x.bin\n=ypart begin=%d end=%d\n%s=yend size=%d part=%d pcrc32=%08X\n",
		part, total, len(data), begin+1, begin+int64(len(data)), body.String(), len(data), part, crc32.ChecksumIEEE(data)))
}

func testFile(parts ...[]byte) (nzb.File, map[string][]byte) {
	f := nzb.File{Groups: []string{"alt.test"}}
	articles := map[string][]byte{}
	var begin int64
	for i, part := range parts {
		id := fmt.Sprintf("part-%d", i+1)
		f.Segments = append(f.Segments, nzb.Segment{MessageID: id, Number: i + 1, Bytes: int64(len(part))})
		articles[id] = article(part, i+1, len(parts), begin)
		begin += int64(len(part))
	}
	return f, articles
}

func TestReaderLazySeekAndSegmentCache(t *testing.T) {
	file, articles := testFile([]byte("abcdef"), []byte("ghijkl"))
	fetch := &fakeFetcher{articles: articles}
	r, err := NewReader(context.Background(), file, fetch)
	if err != nil {
		t.Fatal(err)
	}
	if fetch.count() != 0 {
		t.Fatal("constructor fetched an article")
	}

	buf := make([]byte, 4)
	if n, err := r.ReadAt(buf, 1); err != nil || n != 4 || string(buf) != "bcde" {
		t.Fatalf("first read: n=%d err=%v data=%q", n, err, buf)
	}
	if n, err := r.ReadAt(buf[:2], 3); err != nil || n != 2 || string(buf[:2]) != "de" {
		t.Fatalf("cached read: n=%d err=%v data=%q", n, err, buf[:2])
	}
	buf = make([]byte, 6)
	if n, err := r.ReadAt(buf, 4); err != nil || n != 6 || string(buf) != "efghij" {
		t.Fatalf("cross read: n=%d err=%v data=%q", n, err, buf)
	}
	// Read-ahead plus the segment cache: each of the two segments is fetched
	// exactly once across the reads, never twice.
	if fetch.duplicated() {
		t.Fatalf("a segment was fetched more than once: %v", fetch.calls)
	}
	if fetch.count() != 2 {
		t.Fatalf("want 2 distinct fetches, got %d: %v", fetch.count(), fetch.calls)
	}
	if fetch.lastGroup() != "alt.test" {
		t.Fatalf("group = %q", fetch.lastGroup())
	}
}

func TestReaderParallelReadaheadDedup(t *testing.T) {
	parts := make([][]byte, 20)
	for i := range parts {
		parts[i] = bytes.Repeat([]byte{byte('A' + i)}, 100)
	}
	file, articles := testFile(parts...)
	fetch := &fakeFetcher{articles: articles}
	r, _ := NewReader(context.Background(), file, fetch)

	// Stream the whole file in small chunks (as http.ServeContent does), which
	// drives concurrent read-ahead across segment boundaries.
	full := make([]byte, 20*100)
	for off := 0; off < len(full); off += 37 {
		end := off + 37
		if end > len(full) {
			end = len(full)
		}
		if _, err := r.ReadAt(full[off:end], int64(off)); err != nil && err != io.EOF {
			t.Fatalf("read at %d: %v", off, err)
		}
	}

	var want []byte
	for _, p := range parts {
		want = append(want, p...)
	}
	if !bytes.Equal(full, want) {
		t.Fatal("reassembled stream does not match source")
	}
	if fetch.duplicated() {
		t.Fatalf("a segment was fetched more than once under read-ahead: %v", fetch.calls)
	}
	if fetch.count() != 20 {
		t.Fatalf("want 20 distinct fetches, got %d", fetch.count())
	}
}

func TestReaderWarm(t *testing.T) {
	parts := make([][]byte, 10)
	for i := range parts {
		parts[i] = bytes.Repeat([]byte{byte('A' + i)}, 100)
	}
	file, articles := testFile(parts...)
	fetch := &fakeFetcher{articles: articles}
	r, _ := NewReader(context.Background(), file, fetch)

	// Warm the tail, then reading it must be correct and must not re-fetch the
	// segments the warm already pulled.
	r.Warm(700, 300) // segments 8, 9, 10
	buf := make([]byte, 300)
	if _, err := r.ReadAt(buf, 700); err != nil {
		t.Fatalf("read warmed tail: %v", err)
	}
	var want []byte
	for _, p := range parts[7:] {
		want = append(want, p...)
	}
	if !bytes.Equal(buf, want) {
		t.Fatal("warmed-tail bytes mismatch")
	}
	if fetch.duplicated() {
		t.Fatalf("warm + read double-fetched a segment: %v", fetch.calls)
	}
}

func TestReaderEOFAndInvalidInput(t *testing.T) {
	file, articles := testFile([]byte("abc"))
	r, _ := NewReader(context.Background(), file, &fakeFetcher{articles: articles})
	buf := make([]byte, 5)
	if n, err := r.ReadAt(buf, 1); n != 2 || !errors.Is(err, io.EOF) || string(buf[:n]) != "bc" {
		t.Fatalf("tail read: n=%d err=%v data=%q", n, err, buf[:n])
	}
	if n, err := r.ReadAt(buf, 3); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("eof read: n=%d err=%v", n, err)
	}
	if _, err := r.ReadAt(buf, -1); err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, err := NewReader(context.Background(), nzb.File{}, &fakeFetcher{}); err == nil {
		t.Fatal("empty file accepted")
	}
}

func TestReaderPropagatesFetchAndDecodeErrors(t *testing.T) {
	file, articles := testFile([]byte("abc"))
	t.Run("fetch", func(t *testing.T) {
		r, _ := NewReader(context.Background(), file, &fakeFetcher{err: errors.New("offline")})
		if _, err := r.ReadAt(make([]byte, 1), 0); err == nil || !bytes.Contains([]byte(err.Error()), []byte("offline")) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("yenc", func(t *testing.T) {
		r, _ := NewReader(context.Background(), file, &fakeFetcher{articles: map[string][]byte{"part-1": []byte("bad")}})
		if _, err := r.ReadAt(make([]byte, 1), 0); err == nil {
			t.Fatal("invalid yenc accepted")
		}
	})
	t.Run("checksum", func(t *testing.T) {
		bad := append([]byte(nil), articles["part-1"]...)
		bad = bytes.Replace(bad, []byte(fmt.Sprintf("%08X", crc32.ChecksumIEEE([]byte("abc")))), []byte("DEADBEEF"), 1)
		r, _ := NewReader(context.Background(), file, &fakeFetcher{articles: map[string][]byte{"part-1": bad}})
		_, err := r.ReadAt(make([]byte, 1), 0)
		if !errors.Is(err, decoder.ErrChecksumMismatch) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("size", func(t *testing.T) {
		wrong := file
		wrong.Segments = append([]nzb.Segment(nil), file.Segments...)
		wrong.Segments[0].Bytes++
		r, _ := NewReader(context.Background(), wrong, &fakeFetcher{articles: articles})
		if _, err := r.ReadAt(make([]byte, 1), 0); err == nil {
			t.Fatal("decoded size mismatch accepted")
		}
	})
}
