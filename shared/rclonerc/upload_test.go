package rclonerc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadFile(t *testing.T) {
	var fs, remote, name, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs = r.URL.Query().Get("fs")
		remote = r.URL.Query().Get("remote")
		mr, err := r.MultipartReader()
		if err != nil {
			t.Errorf("multipart: %v", err)
			return
		}
		p, err := mr.NextPart()
		if err != nil {
			t.Errorf("part: %v", err)
			return
		}
		name = p.FileName()
		b, _ := io.ReadAll(p)
		body = string(b)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	err := New(srv.URL).UploadFile(context.Background(), "u_x:", "dir/sub", "file.mkv", strings.NewReader("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if fs != "u_x:" || remote != "dir/sub" || name != "file.mkv" || body != "hello world" {
		t.Fatalf("fs=%q remote=%q name=%q body=%q", fs, remote, name, body)
	}
}

func TestUploadFileError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"nope"}`, 500)
	}))
	defer srv.Close()
	if err := New(srv.URL).UploadFile(context.Background(), "u_x:", "d", "f", strings.NewReader("x")); err == nil {
		t.Fatal("expected error on non-200")
	}
}
