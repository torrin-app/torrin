package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
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
