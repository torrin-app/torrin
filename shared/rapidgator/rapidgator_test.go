package rapidgator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsURL(t *testing.T) {
	if !IsURL("https://rapidgator.net/file/abc/x.rar.html") {
		t.Fatal("should match rapidgator")
	}
	if IsURL("https://nitroflare.com/view/abc/x.rar") {
		t.Fatal("should not match nitroflare")
	}
}

func TestFileName(t *testing.T) {
	if got := fileName("https://rapidgator.net/file/abc/Movie.part1.rar.html"); got != "Movie.part1.rar" {
		t.Fatalf("fileName = %q", got)
	}
}

func TestNewEmptyCreds(t *testing.T) {
	if New("", "p") != nil || New("u", "") != nil {
		t.Fatal("empty creds should return nil")
	}
}

func mockServer(handler http.HandlerFunc) (*httptest.Server, func()) {
	srv := httptest.NewServer(handler)
	old := apiBase
	apiBase = srv.URL
	return srv, func() { apiBase = old; srv.Close() }
}

func TestDirectLink(t *testing.T) {
	_, done := mockServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/login":
			w.Write([]byte(`{"status":200,"response":{"token":"tok123"}}`))
		case "/file/download":
			if r.URL.Query().Get("token") != "tok123" {
				w.Write([]byte(`{"status":401,"response":[]}`))
				return
			}
			w.Write([]byte(`{"status":200,"response":{"download_url":"https://cdn/x.rar","file":{"size":42}}}`))
		}
	})
	defer done()
	name, dl, size, err := New("u", "p").DirectLink(context.Background(), "https://rapidgator.net/file/abc/x.rar.html")
	if err != nil {
		t.Fatal(err)
	}
	if name != "x.rar" || dl != "https://cdn/x.rar" || size != 42 {
		t.Fatalf("got %q %q %d", name, dl, size)
	}
}

func TestDirectLinkNotAvailable(t *testing.T) {
	_, done := mockServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/login" {
			w.Write([]byte(`{"status":200,"response":{"token":"t"}}`))
			return
		}
		w.Write([]byte(`{"status":404,"response":[]}`))
	})
	defer done()
	if _, _, _, err := New("u", "p").DirectLink(context.Background(), "https://rapidgator.net/file/abc/x.rar.html"); err != ErrNotAvailable {
		t.Fatalf("want ErrNotAvailable, got %v", err)
	}
}

func TestReloginOnExpiredToken(t *testing.T) {
	logins := 0
	_, done := mockServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/login" {
			logins++
			w.Write([]byte(`{"status":200,"response":{"token":"fresh"}}`))
			return
		}
		if r.URL.Query().Get("token") == "fresh" {
			w.Write([]byte(`{"status":200,"response":{"download_url":"https://cdn/y.rar","file":{"size":9}}}`))
			return
		}
		w.Write([]byte(`{"status":401,"response":[]}`))
	})
	defer done()
	c := New("u", "p")
	c.token = "stale"
	if _, dl, _, err := c.DirectLink(context.Background(), "https://rapidgator.net/file/abc/y.rar.html"); err != nil || dl != "https://cdn/y.rar" {
		t.Fatalf("expected re-login recovery, got dl=%q err=%v", dl, err)
	}
	if logins != 1 {
		t.Fatalf("expected exactly 1 re-login, got %d", logins)
	}
}
