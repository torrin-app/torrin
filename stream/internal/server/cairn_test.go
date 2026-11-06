package server

import (
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/madcowfred/yencode"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

type fakeCairnStore struct {
	data []byte
	err  error
}

func (f fakeCairnStore) GetBytes(context.Context, string) ([]byte, error) {
	return f.data, f.err
}

type fakeCairnFetcher struct {
	articles map[string][]byte
	mu       sync.Mutex
	calls    []string
}

func (f *fakeCairnFetcher) fetchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// sortedCalls returns the fetched message IDs in a stable order; concurrent
// read-ahead makes the fetch order itself nondeterministic.
func (f *fakeCairnFetcher) sortedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.calls...)
	sort.Strings(out)
	return out
}

type blockingCairnFetcher struct {
	articles map[string][]byte
	entered  chan string
	release  chan struct{}
	calls    atomic.Int32
}

func (f *blockingCairnFetcher) Fetch(ctx context.Context, id, _ string) ([]byte, error) {
	f.calls.Add(1)
	select {
	case f.entered <- id:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-f.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	b, ok := f.articles[id]
	if !ok {
		return nil, fmt.Errorf("missing article %s", id)
	}
	return b, nil
}

func (f *fakeCairnFetcher) Fetch(_ context.Context, id, _ string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, id)
	f.mu.Unlock()
	b, ok := f.articles[id]
	if !ok {
		return nil, fmt.Errorf("missing article %s", id)
	}
	return b, nil
}

func cairnArticle(data []byte, part, total int, begin int64) []byte {
	var body bytes.Buffer
	yencode.Encode(data, &body)
	return []byte(fmt.Sprintf("=ybegin part=%d total=%d line=128 size=%d name=x.bin\n=ypart begin=%d end=%d\n%s=yend size=%d part=%d pcrc32=%08X\n",
		part, total, len(data), begin+1, begin+int64(len(data)), body.String(), len(data), part, crc32.ChecksumIEEE(data)))
}

func cairnFixture(data []byte, partSize int) ([]byte, map[string][]byte) {
	var segments []nzb.Segment
	articles := map[string][]byte{}
	for start, part := 0, 1; start < len(data); start, part = start+partSize, part+1 {
		end := start + partSize
		if end > len(data) {
			end = len(data)
		}
		id := fmt.Sprintf("part-%d", part)
		segments = append(segments, nzb.Segment{MessageID: id, Number: part, Bytes: int64(end - start)})
		articles[id] = cairnArticle(data[start:end], part, (len(data)+partSize-1)/partSize, int64(start))
	}
	return nzb.Generate([]nzb.OutFile{{Name: "movie.mkv", Group: "alt.test", Segments: segments}}), articles
}

func cairnServer(data []byte, partSize int, cipher *crypto.Stream) (*Server, *fakeCairnFetcher) {
	nzbData, articles := cairnFixture(data, partSize)
	fetch := &fakeCairnFetcher{articles: articles}
	srv := New(&fakeStorage{valid: true}, "*", "", cipher)
	srv.SetCairn(fakeCairnStore{data: nzbData}, fetch)
	return srv, fetch
}

func cairnURL(hash string, index int, extra string) string {
	return fmt.Sprintf("/%s/cairn/%d/movie.mkv?expires=9999999999&sig=ok%s", hash, index, extra)
}

func TestServeCairnFullHeadAndRange(t *testing.T) {
	hash := strings.Repeat("a", 40)
	data := []byte("abcdefghijklmnop")
	srv, fetch := cairnServer(data, 8, nil)
	w := do(srv, http.MethodHead, cairnURL(hash, 0, ""), nil)
	if w.Code != 200 || w.Header().Get("Content-Length") != strconv.Itoa(len(data)) || fetch.fetchCount() != 0 {
		t.Fatalf("head: code=%d len=%q fetches=%d", w.Code, w.Header().Get("Content-Length"), fetch.fetchCount())
	}

	srv, fetch = cairnServer(data, 8, nil)
	w = do(srv, http.MethodGet, cairnURL(hash, 0, ""), nil)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatalf("full: code=%d body=%q", w.Code, w.Body.Bytes())
	}
	if got := fmt.Sprint(fetch.sortedCalls()); got != "[part-1 part-2]" {
		t.Fatalf("full fetches = %s", got)
	}

	srv, fetch = cairnServer(data, 8, nil)
	w = do(srv, http.MethodGet, cairnURL(hash, 0, ""), http.Header{"Range": {"bytes=9-12"}})
	if w.Code != 206 || w.Body.String() != "jklm" || w.Header().Get("Content-Range") != "bytes 9-12/16" {
		t.Fatalf("range: code=%d cr=%q body=%q", w.Code, w.Header().Get("Content-Range"), w.Body.String())
	}
	if got := fmt.Sprint(fetch.sortedCalls()); got != "[part-2]" {
		t.Fatalf("range fetched unrelated articles: %s", got)
	}
}

func TestServeCairnAuthorizationAndFailures(t *testing.T) {
	hash := strings.Repeat("a", 40)
	nzbData, articles := cairnFixture([]byte("abcdefgh"), 8)
	fetch := &fakeCairnFetcher{articles: articles}
	srv := New(&fakeStorage{valid: false}, "*", "", nil)
	srv.SetCairn(fakeCairnStore{data: nzbData}, fetch)
	if w := do(srv, http.MethodGet, cairnURL(hash, 0, ""), nil); w.Code != 403 || fetch.fetchCount() != 0 {
		t.Fatalf("bad signature: code=%d fetches=%d", w.Code, fetch.fetchCount())
	}

	srv = New(&fakeStorage{valid: true}, "*", "", nil)
	if w := do(srv, http.MethodGet, cairnURL(hash, 0, ""), nil); w.Code != 503 {
		t.Fatalf("unconfigured code = %d", w.Code)
	}

	srv.SetCairn(fakeCairnStore{data: nzbData}, fetch)
	if w := do(srv, http.MethodGet, cairnURL(hash, 4, ""), nil); w.Code != 404 || fetch.fetchCount() != 0 {
		t.Fatalf("bad index: code=%d fetches=%d", w.Code, fetch.fetchCount())
	}

	missing := &fakeCairnFetcher{articles: map[string][]byte{}}
	srv.SetCairn(fakeCairnStore{data: nzbData}, missing)
	if w := do(srv, http.MethodGet, cairnURL(hash, 0, ""), nil); w.Code != http.StatusBadGateway || missing.fetchCount() < 1 {
		t.Fatalf("missing article: code=%d fetches=%d body=%s", w.Code, missing.fetchCount(), w.Body.String())
	}

	srv.SetCairn(fakeCairnStore{data: nzbData}, fetch)
	if w := do(srv, http.MethodGet, cairnURL(hash, 0, "&enc=1"), nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("encrypted without key: code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestServeEncryptedCairnSeek(t *testing.T) {
	cipher, err := crypto.NewStream(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	plain := bytes.Repeat([]byte("seekable-cairn-data"), 12000)
	encReader, err := cipher.EncryptReader(bytes.NewReader(plain))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := io.ReadAll(encReader)
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("b", 40)
	srv, _ := cairnServer(encrypted, 70000, cipher)
	w := do(srv, http.MethodGet, cairnURL(hash, 0, "&enc=1"), http.Header{"Range": {"bytes=100000-110999"}})
	if w.Code != 206 {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	if want := plain[100000:111000]; !bytes.Equal(w.Body.Bytes(), want) {
		t.Fatalf("decrypted range mismatch: got=%d want=%d", w.Body.Len(), len(want))
	}
	if got := w.Header().Get("Content-Range"); got != fmt.Sprintf("bytes 100000-110999/%d", len(plain)) {
		t.Fatalf("content-range = %q", got)
	}
}

func TestServeCairnConcurrentStreamLimits(t *testing.T) {
	hash := strings.Repeat("c", 40)
	nzbData, articles := cairnFixture([]byte("abcdefgh"), 8)
	fetch := &blockingCairnFetcher{
		articles: articles, entered: make(chan string, 2), release: make(chan struct{}),
	}
	srv := New(&fakeStorage{valid: true}, "*", "", nil)
	srv.SetCairn(fakeCairnStore{data: nzbData}, fetch)
	srv.SetCairnLimits(2, 1)

	responses := make(chan int, 2)
	go func() { responses <- do(srv, http.MethodGet, cairnURL(hash, 0, "&u=u1"), nil).Code }()
	<-fetch.entered

	w := do(srv, http.MethodGet, cairnURL(hash, 0, "&u=u1"), nil)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "5" {
		t.Fatalf("same user: code=%d retry=%q body=%s", w.Code, w.Header().Get("Retry-After"), w.Body.String())
	}

	go func() { responses <- do(srv, http.MethodGet, cairnURL(hash, 0, "&u=u2"), nil).Code }()
	<-fetch.entered
	w = do(srv, http.MethodGet, cairnURL(hash, 0, "&u=u3"), nil)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "5" {
		t.Fatalf("global limit: code=%d retry=%q body=%s", w.Code, w.Header().Get("Retry-After"), w.Body.String())
	}

	// Metadata probes do not fetch articles or hold scarce streaming capacity.
	w = do(srv, http.MethodHead, cairnURL(hash, 0, "&u=u3"), nil)
	if w.Code != http.StatusOK || fetch.calls.Load() != 2 {
		t.Fatalf("head while full: code=%d fetches=%d", w.Code, fetch.calls.Load())
	}

	close(fetch.release)
	for range 2 {
		if code := <-responses; code != http.StatusOK {
			t.Fatalf("admitted request code = %d", code)
		}
	}
	if w = do(srv, http.MethodGet, cairnURL(hash, 0, "&u=u3"), nil); w.Code != http.StatusOK {
		t.Fatalf("slot not released: code=%d body=%s", w.Code, w.Body.String())
	}
}
