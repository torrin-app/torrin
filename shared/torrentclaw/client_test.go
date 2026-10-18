package torrentclaw

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/availcache"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDetailedPartialResponseAndRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Hashes []string `json:"infoHashes"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if r.Header.Get("X-Debrid-Key") != "private-key" {
			t.Error("missing credential header")
		}
		if calls == 1 {
			io.WriteString(w, `{"cached":{"AABB":true}}`)
		} else {
			if len(req.Hashes) != 1 || req.Hashes[0] != "ccdd" {
				t.Errorf("request=%v", req.Hashes)
			}
			io.WriteString(w, `{"cached":{"ccdd":false}}`)
		}
	}))
	defer server.Close()
	old := checkCacheURL
	checkCacheURL = server.URL
	defer func() { checkCacheURL = old }()
	c := New("private-token")
	c.SetCache(availcache.New(time.Minute))
	first := c.CheckCacheDetailed(context.Background(), "alldebrid", "private-key", []string{"AABB", "CCDD"})
	if !first.Cached["aabb"] || first.Reasons["ccdd"] != "partial_response" {
		t.Fatalf("first=%+v", first)
	}
	if _, ok := first.Cached["ccdd"]; ok {
		t.Fatal("omission is not false")
	}
	second := c.CheckCacheDetailed(context.Background(), "alldebrid", "private-key", []string{"aabb", "ccdd"})
	if val, ok := second.Cached["ccdd"]; !ok || val || calls != 2 {
		t.Fatalf("second=%+v calls=%d", second, calls)
	}
}
func TestDetailedFailures(t *testing.T) {
	cases := []struct {
		name, body, reason string
		status             int
	}{
		{"unauthorized", "private-body", "authentication_failed", 401},
		{"rate-limit", "private-body", "rate_limited", 429},
		{"upstream", "private-body", "upstream_http_error", 503},
		{"invalid-json", "{", "invalid_response", 200},
		{"missing-field", "{}", "invalid_response", 200},
		{"empty-map", `{"cached":{}}`, "partial_response", 200},
		{"invalid-bool", `{"cached":{"aabb":null}}`, "invalid_response", 200},
		{"oversize", strings.Repeat(" ", maxCheckBody+1), "invalid_response", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New("secret")
			c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			result := c.CheckCacheDetailed(context.Background(), "alldebrid", "secret", []string{"aabb"})
			if len(result.Cached) != 0 || result.Reasons["aabb"] != tc.reason {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}
func TestDetailedCancellationAndConfiguration(t *testing.T) {
	c := New("secret")
	c.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })
	for _, tc := range []struct {
		ctx    context.Context
		reason string
	}{{func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }(), "cancelled"}, {func() context.Context {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		return ctx
	}(), "timeout"}} {
		if r := c.CheckCacheDetailed(tc.ctx, "alldebrid", "secret", []string{"aabb"}); r.Reasons["aabb"] != tc.reason {
			t.Fatalf("result=%+v", r)
		}
	}
	if r := New("").CheckCacheDetailed(context.Background(), "alldebrid", "secret", []string{"aabb"}); r.Reasons["aabb"] != "not_configured" {
		t.Fatalf("result=%+v", r)
	}
}
