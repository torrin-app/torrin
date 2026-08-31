package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOffcloudCached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cache/info" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("bad request: %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Write([]byte(`[{"cached":true},{"cached":false}]`))
	}))
	defer srv.Close()
	old := ocBase
	ocBase = srv.URL
	defer func() { ocBase = old }()

	got := OffcloudCached(context.Background(), "k", []string{"AABB", "CCDD"})
	if !got["aabb"] || got["ccdd"] {
		t.Fatalf("want aabb cached, ccdd not: %v", got)
	}
}

func TestOffcloudFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/cloud/history":
			w.Write([]byte(`[]`))
		case r.URL.Path == "/api/cloud":
			w.Write([]byte(`{"requestId":"r1","fileName":"Show S01"}`))
		case r.URL.Path == "/api/cloud/status":
			w.Write([]byte(`{"status":{"status":"downloaded"}}`))
		case r.URL.Path == "/api/cloud/explore/r1":
			w.Write([]byte(`{"files":[{"name":"ep1.mkv","size":10,"url":"http://x/ep1.mkv"},{"name":"note.txt","size":1,"url":"http://x/note.txt"}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	ob, op := ocBase, ocPollInterval
	ocBase, ocPollInterval = srv.URL, time.Millisecond
	defer func() { ocBase, ocPollInterval = ob, op }()

	res, err := newOffcloud("k").Fetch(context.Background(), "magnet:?xt=urn:btih:aabb", "aabb")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Handle != "r1" || len(res.Files) != 1 || res.Files[0].Name != "ep1.mkv" {
		t.Fatalf("bad result: %+v", res)
	}
}

func TestOffcloudLibrary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cloud/history" {
			t.Errorf("bad path %s", r.URL.Path)
		}
		w.Write([]byte(`[
			{"fileName":"Movie","status":"downloaded","originalLink":"f409d526a1863b60eefae8d015be889866afa7aa.torrent"},
			{"fileName":"Still going","status":"created","originalLink":"aabbccddeeff00112233445566778899aabbccdd.torrent"},
			{"fileName":"A hoster file","status":"downloaded","originalLink":"https://example.com/file.mkv"}
		]`))
	}))
	defer srv.Close()
	old := ocBase
	ocBase = srv.URL
	defer func() { ocBase = old }()

	items, err := OffcloudLibrary(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Hash != "f409d526a1863b60eefae8d015be889866afa7aa" {
		t.Fatalf("want 1 downloaded magnet item, got %+v", items)
	}
}

func TestValidateOCBadKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"NOAUTH"}`))
	}))
	defer srv.Close()
	old := ocBase
	ocBase = srv.URL
	defer func() { ocBase = old }()

	if err := ValidateOC(context.Background(), "bad"); err == nil {
		t.Fatal("expected error for bad key")
	}
}

func TestOffcloudLibraryFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/cloud/history":
			w.Write([]byte(`[{"requestId":"r9","fileName":"MyFilm","status":"downloaded","originalLink":"f409d526a1863b60eefae8d015be889866afa7aa.torrent"}]`))
		case r.URL.Path == "/api/cloud/explore/r9":
			w.Write([]byte(`{"files":[{"name":"film.mkv","size":100,"url":"http://x/film.mkv"}]}`))
		case r.URL.Path == "/api/cloud":
			t.Error("must not add a magnet when the item is already in the library")
		case r.URL.Path == "/api/cloud/remove":
			t.Error("a library item must NOT be removed")
		default:
			w.WriteHeader(200)
		}
	}))
	defer srv.Close()
	old := ocBase
	ocBase = srv.URL
	defer func() { ocBase = old }()

	res, err := newOffcloud("k").Fetch(context.Background(), "magnet:x", "f409d526a1863b60eefae8d015be889866afa7aa")
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
