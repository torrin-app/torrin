package post

import (
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

type fakeConn struct {
	posts     int
	failUntil int
	closed    bool
}

func (f *fakeConn) RawPost(r io.Reader) error {
	io.Copy(io.Discard, r)
	f.posts++
	if f.posts <= f.failUntil {
		return io.EOF
	}
	return nil
}

func (f *fakeConn) Close() { f.closed = true }

type scriptPool struct {
	conns []nntpConn
	gets  int
}

func (s *scriptPool) Get(context.Context) (nntpConn, error) {
	c := s.conns[s.gets]
	s.gets++
	return c, nil
}

func (s *scriptPool) Put(nntpConn) {}

func TestBuildArticleFormat(t *testing.T) {
	chunk := []byte("hello usenet")
	meta := articleMeta{
		From: "poster@x.com", Group: "alt.binaries.torrin", MessageID: "<abc@torrin>",
		Date:    "Mon, 14 Jul 2026 23:00:00 +0000",
		Subject: "deadbeef", Name: "cafe1234", FileNum: 1, FileTotal: 2,
		PartNum: 1, PartTotal: 1, FileSize: int64(len(chunk)), Begin: 0,
	}
	got := string(buildArticle(meta, chunk))

	for _, want := range []string{
		"From: poster@x.com\n",
		"Newsgroups: alt.binaries.torrin\n",
		"Date: Mon, 14 Jul 2026 23:00:00 +0000\n",
		"Message-ID: <abc@torrin>\n",
		"Subject: deadbeef [1/2] - \"cafe1234\" yEnc (1/1)\n",
		"=ybegin part=1 total=1 line=128 size=12 name=cafe1234\n",
		"=ypart begin=1 end=12\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("article missing %q\n---\n%s", want, got)
		}
	}

	h := crc32.NewIEEE()
	h.Write(chunk)
	if !strings.Contains(got, "pcrc32=") {
		t.Fatal("missing pcrc32")
	}
	if strings.Contains(got, "\r") {
		t.Error("article must be LF-only; nntp.sendLines re-adds CR")
	}
	if idx := strings.Index(got, "\n\n"); idx < 0 {
		t.Error("missing header/body separator")
	}
}

func TestBuildArticleBeginOffset(t *testing.T) {
	got := string(buildArticle(articleMeta{PartNum: 2, PartTotal: 3, Begin: 100}, make([]byte, 50)))
	if !strings.Contains(got, "=ypart begin=101 end=150\n") {
		t.Errorf("ypart offset wrong:\n%s", got)
	}
}

func TestConfigEnabled(t *testing.T) {
	if (Config{}).Enabled() {
		t.Error("empty config should be disabled")
	}
	if !(Config{Host: "news.x", Username: "u"}).Enabled() {
		t.Error("host+user config should be enabled")
	}
	if (Config{Host: "news.x"}).Enabled() {
		t.Error("host without user should be disabled")
	}
}

func TestObfuscationUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := randMessageID()
		if !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, "@torrin>") {
			t.Fatalf("bad message-id: %s", id)
		}
		if seen[id] {
			t.Fatalf("duplicate message-id: %s", id)
		}
		seen[id] = true
	}
}

func TestRandFromIsValidEmail(t *testing.T) {
	for i := 0; i < 50; i++ {
		f := randFrom()
		at := strings.IndexByte(f, '@')
		if at <= 0 || at == len(f)-1 {
			t.Fatalf("from missing user or domain: %s", f)
		}
		domain := f[at+1:]
		if !strings.Contains(domain, ".") {
			t.Fatalf("from domain has no TLD: %s", f)
		}
		if strings.ContainsAny(f, "<>\"() ") {
			t.Fatalf("from has forbidden chars: %s", f)
		}
	}
}

func TestPostFileSplitsAndBuildsNZB(t *testing.T) {
	// 3 full parts + a remainder.
	size := partSize*3 + 1234
	data := bytes.Repeat([]byte{0xAB}, size)
	p := &Poster{cfg: Config{Group: "alt.binaries.boneless"}}

	var calls int64
	poster := func(_ context.Context, build func(string) []byte) (string, error) {
		n := atomic.AddInt64(&calls, 1)
		build("<x@torrin>")
		return fmt.Sprintf("<seg%d@torrin>", n), nil
	}
	out, err := p.postFile(context.Background(), poster, "from@x.com", 1, 1, FileInput{
		Name: "movie.mkv", Size: int64(size),
		Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.segments) != 4 || calls != 4 {
		t.Fatalf("segments=%d calls=%d, want 4/4", len(out.segments), calls)
	}
	for i, seg := range out.segments {
		if seg.Number != i+1 {
			t.Errorf("segment %d number = %d", i, seg.Number)
		}
		if strings.ContainsAny(seg.MessageID, "<>") {
			t.Errorf("segment message-id must be stripped: %s", seg.MessageID)
		}
	}
	if out.segments[3].Bytes != 1234 {
		t.Errorf("last segment bytes = %d, want 1234", out.segments[3].Bytes)
	}
}

func TestPostArticleRetriesOnDrop(t *testing.T) {
	dead := &fakeConn{failUntil: 100}
	live := &fakeConn{}
	pool := &scriptPool{conns: []nntpConn{dead, live}}
	p := &Poster{pool: pool}

	mid, err := p.postArticle(context.Background(), func(string) []byte { return []byte("article") })
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if mid == "" {
		t.Error("expected a message-id")
	}
	if !dead.closed {
		t.Error("dropped connection must be closed before retry")
	}
	if pool.gets != 2 {
		t.Errorf("expected 2 pool.Get calls (retry), got %d", pool.gets)
	}
}
