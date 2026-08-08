package server

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/storage"
)

type fakeStorage struct {
	data         []byte
	manifestJSON []byte
	valid        bool
	fetchErr     error
}

func (f *fakeStorage) Verify(string, int64, string, string) bool { return f.valid }

func (f *fakeStorage) GetBytes(context.Context, string) ([]byte, error) {
	if f.manifestJSON != nil {
		return f.manifestJSON, nil
	}
	return f.data, nil
}

func (f *fakeStorage) Head(context.Context, string) (*storage.Object, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return &storage.Object{Size: int64(len(f.data)), ContentType: "video/x-matroska"}, nil
}

func (f *fakeStorage) Get(_ context.Context, _, rng string) (*storage.Object, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	o := &storage.Object{Body: io.NopCloser(bytes.NewReader(f.data)), Size: int64(len(f.data)), ContentType: "video/x-matroska"}
	if rng != "" {
		o.ContentRange = "bytes 0-9/" + strconv.Itoa(len(f.data))
		o.Size = 10
	}
	return o, nil
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestServeFetchFailureLogs(t *testing.T) {
	buf := captureLogs(t)
	srv := New(&fakeStorage{valid: true, fetchErr: errors.New("upstream down")}, "*", "", nil)
	w := do(srv, "GET", "/hash/file_0/movie.mkv?expires=9999999999&sig=ok", nil)
	if w.Code != 404 {
		t.Fatalf("got %d, want 404", w.Code)
	}
	out := buf.String()
	if !strings.Contains(out, "stream: fetch failed") || !strings.Contains(out, "upstream down") {
		t.Errorf("expected fetch-failed log, got: %q", out)
	}
}

func TestServeSuccessDoesNotWarn(t *testing.T) {
	buf := captureLogs(t)
	srv := New(&fakeStorage{data: []byte("hello world"), valid: true}, "*", "", nil)
	if w := do(srv, "GET", "/hash/file_0/movie.mkv?expires=9999999999&sig=ok", nil); w.Code != 200 {
		t.Fatalf("got %d, want 200", w.Code)
	}
	if buf.Len() != 0 {
		t.Errorf("successful serve must not warn, got: %q", buf.String())
	}
}

func do(srv *Server, method, target string, header http.Header) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		r.Header[k] = v
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	return w
}

func TestMissingSignature(t *testing.T) {
	srv := New(&fakeStorage{valid: true}, "*", "", nil)
	if w := do(srv, "GET", "/hash/file_0/movie.mkv", nil); w.Code != 401 {
		t.Fatalf("got %d, want 401", w.Code)
	}
}

func TestBadSignature(t *testing.T) {
	srv := New(&fakeStorage{valid: false}, "*", "", nil)
	if w := do(srv, "GET", "/hash/file_0/movie.mkv?expires=9999999999&sig=bad", nil); w.Code != 403 {
		t.Fatalf("got %d, want 403", w.Code)
	}
}

func TestServeFull(t *testing.T) {
	srv := New(&fakeStorage{data: []byte("hello world"), valid: true}, "*", "", nil)
	w := do(srv, "GET", "/hash/file_0/movie.mkv?expires=9999999999&sig=ok", nil)
	if w.Code != 200 || w.Body.String() != "hello world" {
		t.Fatalf("got %d body=%q", w.Code, w.Body.String())
	}
	if w.Header().Get("Accept-Ranges") != "bytes" {
		t.Error("missing Accept-Ranges")
	}
}

func TestServeRange(t *testing.T) {
	srv := New(&fakeStorage{data: []byte("hello world"), valid: true}, "*", "", nil)
	w := do(srv, "GET", "/hash/file_0/movie.mkv?expires=9999999999&sig=ok", http.Header{"Range": {"bytes=0-9"}})
	if w.Code != 206 {
		t.Fatalf("got %d, want 206", w.Code)
	}
	if w.Header().Get("Content-Range") == "" {
		t.Error("missing Content-Range")
	}
}

func TestDownloadFilenameFromManifest(t *testing.T) {
	hash := strings.Repeat("a", 40)
	m := manifest.Manifest{InfoHash: hash, Name: "Movie", Files: []manifest.File{
		{FileName: "Colony.2026.1080p.WEB-DL.mkv", DirectURL: "blobs/b_abc", FileSize: 11},
	}}
	mj, _ := m.Marshal()
	srv := New(&fakeStorage{data: []byte("hello world"), manifestJSON: mj, valid: true}, "*", "", nil)

	w := do(srv, "GET", "/blobs/b_abc?dl=1&ih="+hash+"&expires=9999999999&sig=ok", nil)
	if w.Code != 200 {
		t.Fatalf("code %d", w.Code)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, `"Colony.2026.1080p.WEB-DL.mkv"`) {
		t.Errorf("disposition = %q, want the real filename", cd)
	}
}

func TestDownloadFilenameFallback(t *testing.T) {
	srv := New(&fakeStorage{data: []byte("hello world"), valid: true}, "*", "", nil)
	w := do(srv, "GET", "/blobs/b_abc?dl=1&expires=9999999999&sig=ok", nil)
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, `"b_abc"`) {
		t.Errorf("disposition = %q, want fallback basename", cd)
	}
}

func TestSanitizeFilename(t *testing.T) {
	if got := sanitizeFilename("a\"b\r\nc"); got != "abc" {
		t.Errorf("sanitizeFilename = %q, want abc", got)
	}
}

func TestZipDownload(t *testing.T) {
	hash := strings.Repeat("a", 40)
	m := manifest.Manifest{InfoHash: hash, Name: "Show.S01", Files: []manifest.File{
		{FileName: "ep1.mkv", FileSize: 5},
		{FileName: "ep2.mkv", FileSize: 5},
	}}
	mj, _ := m.Marshal()
	srv := New(&fakeStorage{data: []byte("hello"), manifestJSON: mj, valid: true}, "*", "", nil)

	w := do(srv, "GET", "/"+manifest.ZipKey(hash)+"?expires=9999999999&sig=x", nil)
	if w.Code != 200 {
		t.Fatalf("code %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("content-type %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, `"Show.S01.zip"`) {
		t.Errorf("disposition %q", cd)
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("zip read: %v", err)
	}
	if len(zr.File) != 2 || zr.File[0].Name != "ep1.mkv" || zr.File[1].Name != "ep2.mkv" {
		t.Fatalf("unexpected zip contents: %+v", zr.File)
	}
}
