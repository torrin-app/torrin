package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFetchFileStalledHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out")
	if err := FetchFileStalled(context.Background(), srv.Client(), srv.URL, dst, nil, 1); err != nil {
		t.Fatalf("happy path: %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "hello world" {
		t.Errorf("got %q", b)
	}
}

func TestFetchFileStalledCancelsOnStall(t *testing.T) {
	old := StallTimeout
	StallTimeout = 150 * time.Millisecond
	defer func() { StallTimeout = old }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		w.WriteHeader(200)
		if f, ok := w.(http.Flusher); ok {
			w.Write([]byte{0})
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out")
	if err := FetchFileStalled(context.Background(), srv.Client(), srv.URL, dst, nil, 1); err == nil {
		t.Error("expected the stall watchdog to cancel the download")
	}
}
