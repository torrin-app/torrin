package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/torrin-app/torrin/shared/failure"
)

func TestHosterUnlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v4/link/unlock" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("link") != "https://host/file" {
			t.Errorf("link not forwarded: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"status":"success","data":{"link":"https://dl/x.mkv","filename":"x.mkv","filesize":123}}`))
	}))
	defer srv.Close()

	old := adBase
	adBase = srv.URL
	defer func() { adBase = old }()

	name, dl, size, err := HosterUnlock(context.Background(), "key", "https://host/file")
	if err != nil {
		t.Fatal(err)
	}
	if name != "x.mkv" || dl != "https://dl/x.mkv" || size != 123 {
		t.Fatalf("got %q %q %d", name, dl, size)
	}
}

func TestHosterUnlockError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"error","error":{"code":"LINK_HOST_NOT_SUPPORTED","message":"This host or link is not supported."}}`))
	}))
	defer srv.Close()

	old := adBase
	adBase = srv.URL
	defer func() { adBase = old }()

	_, _, _, err := HosterUnlock(context.Background(), "key", "https://host/file")
	if err == nil {
		t.Fatal("expected error from failed unlock")
	}
	if got := failure.Message(err); got != "this host or link is not supported" {
		t.Errorf("surfaced reason = %q, want the real AllDebrid message", got)
	}
	if !DeadLink(err) {
		t.Error("LINK_HOST_NOT_SUPPORTED should be a dead link")
	}
}

func TestDeadLink(t *testing.T) {
	dead := []string{"LINK_DOWN", "LINK_HOST_NOT_SUPPORTED", "LINK_HOST_UNAVAILABLE", "BAD_LINK"}
	live := []string{"LINK_TOO_MANY_DOWNLOADS", "LINK_HOST_FULL", "MUST_BE_PREMIUM", "NO_SERVER", "LINK_IS_MISSING", "LINK_TEMPORARY_UNAVAILABLE"}
	for _, code := range dead {
		if !DeadLink(failure.Newf(code, "x")) {
			t.Errorf("%s should be dead", code)
		}
	}
	for _, code := range live {
		if DeadLink(failure.Newf(code, "x")) {
			t.Errorf("%s should not be dead", code)
		}
	}
	if DeadLink(errors.New("some other error")) {
		t.Error("non-alldebrid error should not be a dead link")
	}
}

func TestADReason(t *testing.T) {
	if got := adReason("Host under maintenance or not available"); got != "host under maintenance or not available" {
		t.Errorf("got %q", got)
	}
	if got := adReason("This host or link is not supported."); got != "this host or link is not supported" {
		t.Errorf("got %q", got)
	}
	if got := adReason(""); got != failure.Generic.Msg {
		t.Errorf("empty should fall back to generic, got %q", got)
	}
}

func TestAlldebridLibraryFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4.1/magnet/status":
			w.Write([]byte(`{"status":"success","data":{"magnets":[{"id":7,"hash":"ABC","filename":"MyFilm","status":"Ready"}]}}`))
		case "/v4/magnet/files":
			w.Write([]byte(`{"status":"success","data":{"magnets":[{"files":[{"n":"film.mkv","s":100,"l":"https://ad/l1"}]}]}}`))
		case "/v4/link/unlock":
			w.Write([]byte(`{"status":"success","data":{"link":"https://ad/dl/film.mkv","filename":"film.mkv","filesize":100}}`))
		case "/v4/magnet/upload":
			t.Error("must not upload a magnet when the item is already in the library")
			w.Write([]byte(`{"status":"success","data":{"magnets":[]}}`))
		case "/v4/magnet/delete":
			t.Error("a library item must NOT be deleted")
			w.Write([]byte(`{"status":"success","data":{}}`))
		default:
			w.Write([]byte(`{"status":"success","data":{}}`))
		}
	}))
	defer srv.Close()
	old := adBase
	adBase = srv.URL
	defer func() { adBase = old }()

	res, err := NewAllDebrid("key").Fetch(context.Background(), "magnet:x", "abc")
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
