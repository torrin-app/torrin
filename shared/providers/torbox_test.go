package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func torboxServer(t *testing.T, mylist string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/torrents/checkcached":
			w.Write([]byte(`{"success":true,"data":[{"name":"pack","size":100}]}`))
		case "/torrents/createtorrent":
			w.Write([]byte(`{"success":true,"data":{"torrent_id":1}}`))
		case "/torrents/mylist":
			w.Write([]byte(mylist))
		case "/torrents/requestdl":
			w.Write([]byte(`{"success":true,"data":"http://` + r.Host + `/cdn/movie.mkv"}`))
		case "/torrents/controltorrent":
			w.Write([]byte(`{"success":true}`))
		case "/cdn/movie.mkv":
			w.WriteHeader(200)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
}

func TestTorboxGateSkipsUnfinished(t *testing.T) {
	srv := torboxServer(t, `{"success":true,"data":{"hash":"h","name":"pack","size":100,"progress":0.46,"download_finished":false,"download_present":true,"files":[{"id":0,"name":"movie.mkv","size":100}]}}`)
	defer srv.Close()
	old := tbBase
	tbBase = srv.URL
	defer func() { tbBase = old }()

	res, err := NewTorBox("key").Fetch(context.Background(), "magnet:x", "h")
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatalf("expected nil for a still-downloading torrent, got %+v", res)
	}
}

func TestTorboxFinishedResolves(t *testing.T) {
	srv := torboxServer(t, `{"success":true,"data":{"hash":"h","name":"pack","size":100,"progress":1,"download_finished":true,"download_present":true,"files":[{"id":0,"name":"movie.mkv","size":100}]}}`)
	defer srv.Close()
	old := tbBase
	tbBase = srv.URL
	defer func() { tbBase = old }()

	res, err := NewTorBox("key").Fetch(context.Background(), "magnet:x", "h")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Files) != 1 || res.Files[0].Name != "movie.mkv" {
		t.Fatalf("expected 1 resolved video file, got %+v", res)
	}
}

func TestTorboxLibraryFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/torrents/checkcached":
			w.Write([]byte(`{"success":true,"data":[]}`))
		case "/torrents/mylist":
			if r.URL.Query().Get("id") != "" {
				w.Write([]byte(`{"success":true,"data":{"hash":"abc","name":"MyFilm","download_finished":true,"download_present":true,"files":[{"id":0,"name":"film.mkv","size":100}]}}`))
			} else {
				w.Write([]byte(`{"success":true,"data":[{"id":7,"hash":"ABC","name":"MyFilm","download_finished":true,"download_present":true}]}`))
			}
		case "/torrents/requestdl":
			w.Write([]byte(`{"success":true,"data":"http://` + r.Host + `/cdn/film.mkv"}`))
		case "/torrents/controltorrent":
			t.Error("a library item must NOT be deleted")
			w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(200)
		}
	}))
	defer srv.Close()
	old := tbBase
	tbBase = srv.URL
	defer func() { tbBase = old }()

	res, err := NewTorBox("key").Fetch(context.Background(), "magnet:x", "abc")
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

func TestTorboxRequestdlTokenInQuery(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/torrents/checkcached":
			w.Write([]byte(`{"success":true,"data":[{"name":"pack","size":100}]}`))
		case "/torrents/createtorrent":
			w.Write([]byte(`{"success":true,"data":{"torrent_id":1}}`))
		case "/torrents/mylist":
			w.Write([]byte(`{"success":true,"data":{"hash":"h","name":"pack","size":100,"progress":1,"download_finished":true,"download_present":true,"files":[{"id":0,"name":"movie.mkv","size":100}]}}`))
		case "/torrents/requestdl":
			gotToken = r.URL.Query().Get("token")
			w.Write([]byte(`{"success":true,"data":"http://` + r.Host + `/cdn/movie.mkv"}`))
		case "/cdn/movie.mkv":
			w.WriteHeader(200)
		default:
			w.Write([]byte(`{"success":true}`))
		}
	}))
	defer srv.Close()
	old := tbBase
	tbBase = srv.URL
	defer func() { tbBase = old }()

	if _, err := NewTorBox("tb_secretkey").Fetch(context.Background(), "magnet:x", "h"); err != nil {
		t.Fatal(err)
	}
	if gotToken != "tb_secretkey" {
		t.Errorf("requestdl must pass the key as a query token (TorBox requires it, not the header), got %q", gotToken)
	}
}

func TestTorboxRedactsKeyInError(t *testing.T) {
	tb := &torbox{key: "tb_secret123"}
	err := tb.redact(errors.New(`Get "https://api.torbox.app/v1/api/torrents/requestdl?token=tb_secret123&x=1": context deadline exceeded`))
	if strings.Contains(err.Error(), "tb_secret123") {
		t.Errorf("key not redacted from error: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Errorf("expected REDACTED marker, got: %q", err.Error())
	}
}
