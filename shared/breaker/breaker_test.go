package breaker

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/torrin-app/torrin/shared/failure"
)

func TestTripsOn503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	call := func() (int, error) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		s, _, err := RoundTrip("test-503", http.DefaultClient, req)
		return s, err
	}
	for i := 0; i < 5; i++ {
		if s, err := call(); s != 503 || err != nil {
			t.Fatalf("call %d: status=%d err=%v, want 503 with no breaker error", i, s, err)
		}
	}
	if _, err := call(); failure.Message(err) != failure.Upstream.Msg {
		t.Fatalf("breaker should be open after 5 systemic failures, got %v", err)
	}
}

func TestStaysClosedOn4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	for i := 0; i < 10; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		s, _, err := RoundTrip("test-4xx", http.DefaultClient, req)
		if s != 404 || err != nil {
			t.Fatalf("call %d: status=%d err=%v, 4xx must not trip the breaker", i, s, err)
		}
	}
}
