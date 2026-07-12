package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSPreflight(t *testing.T) {
	nextCalled := false
	h := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest(http.MethodOptions, "/rest/1.0/user", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want 204", rec.Code)
	}
	if nextCalled {
		t.Error("next should not run on a preflight request")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("missing Access-Control-Allow-Origin")
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("missing Access-Control-Allow-Methods")
	}
	if rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Error("missing Access-Control-Allow-Headers")
	}
}

func TestCORSPassthrough(t *testing.T) {
	nextCalled := false
	h := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v0/store/magnets", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("next should run for non-OPTIONS requests")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("non-preflight response missing Access-Control-Allow-Origin")
	}
}
