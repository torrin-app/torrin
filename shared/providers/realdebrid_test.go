package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRealDebridLibraryFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/torrents":
			w.Write([]byte(`[{"id":"t7","hash":"ABC","filename":"MyFilm","status":"downloaded","links":["https://rd/l1"]}]`))
		case r.URL.Path == "/torrents/info/t7":
			w.Write([]byte(`{"id":"t7","hash":"ABC","filename":"MyFilm","status":"downloaded","links":["https://rd/l1"]}`))
		case r.URL.Path == "/unrestrict/link":
			w.Write([]byte(`{"filename":"film.mkv","filesize":100,"download":"https://rd/dl/film.mkv"}`))
		case strings.HasPrefix(r.URL.Path, "/torrents/addMagnet"):
			t.Error("must not add a magnet when the item is already in the library")
		case strings.HasPrefix(r.URL.Path, "/torrents/delete/"):
			t.Error("a library item must NOT be deleted")
		default:
			w.WriteHeader(200)
		}
	}))
	defer srv.Close()
	old := rdBase
	rdBase = srv.URL
	defer func() { rdBase = old }()

	res, err := NewRealDebrid("key").Fetch(context.Background(), "magnet:x", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Files) != 1 || res.Files[0].Name != "film.mkv" {
		t.Fatalf("library fallback should resolve the file, got %+v", res)
	}
	if res.Handle != "" {
		t.Fatalf("library item must have an empty Handle so it is never deleted, got %q", res.Handle)
	}
}
