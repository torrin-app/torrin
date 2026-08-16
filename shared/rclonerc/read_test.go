package rclonerc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadFile(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte("blob-bytes"))
	}))
	defer srv.Close()

	body, err := New(srv.URL).ReadFile(context.Background(), "u_x", "Show S01/ep 1.mkv")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	b, _ := io.ReadAll(body)
	if string(b) != "blob-bytes" {
		t.Fatalf("body = %q", b)
	}
	if gotPath != "/[u_x:]/Show S01/ep 1.mkv" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestReadFileError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	if _, err := New(srv.URL).ReadFile(context.Background(), "u_x", "gone.mkv"); err == nil {
		t.Fatal("expected error on 404")
	}
}
