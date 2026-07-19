package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRcloneCacheRead(t *testing.T) {
	var gotRange, gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange, gotMethod, gotPath = r.Header.Get("Range"), r.Method, r.URL.Path
		if r.URL.Path != "/abc/file_0/movie.mkv" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "video/x-matroska")
		w.Header().Set("Content-Range", "bytes 0-4/100")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(http.StatusPartialContent)
			return
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	c := &Client{}
	c.SetRcloneCache(srv.URL + "/")

	o, err := c.Get(context.Background(), "abc/file_0/movie.mkv", "bytes=0-4")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != "GET" || gotPath != "/abc/file_0/movie.mkv" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotRange != "bytes=0-4" {
		t.Errorf("range not forwarded: %q", gotRange)
	}
	if o.ContentRange != "bytes 0-4/100" || o.ContentType != "video/x-matroska" {
		t.Errorf("meta: range=%q type=%q", o.ContentRange, o.ContentType)
	}
	if b, _ := io.ReadAll(o.Body); string(b) != "hello" {
		t.Errorf("body = %q", b)
	}
	o.Body.Close()

	ho, err := c.Head(context.Background(), "abc/file_0/movie.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != "HEAD" || ho.Size != 100 {
		t.Errorf("head: method=%s size=%d", gotMethod, ho.Size)
	}

	if _, err := c.Get(context.Background(), "missing/key", ""); err == nil {
		t.Error("expected error on 404 from rclone")
	}
}

func TestRcloneCacheFallbackToOrigin(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		w.Header().Set("Content-Type", "video/x-matroska")
		w.Write([]byte("world"))
	}))
	defer origin.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()

	c := NewClient(origin.URL, "garage", "k", "s", "bucket", "", "")
	c.SetRcloneCache(broken.URL)

	o, err := c.Get(context.Background(), "abc/file_0/movie.mkv", "")
	if err != nil {
		t.Fatalf("expected fallback to origin, got %v", err)
	}
	b, _ := io.ReadAll(o.Body)
	o.Body.Close()
	if string(b) != "world" {
		t.Errorf("body = %q, want world (served direct from origin)", b)
	}
}
