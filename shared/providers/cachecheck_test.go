package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/availcache"
)

func TestDetailedProviderCoverage(t *testing.T) {
	oldPM, oldOC, oldTB, oldCache := pmBase, ocBase, tbBase, availCache
	defer func() { pmBase, ocBase, tbBase, availCache = oldPM, oldOC, oldTB, oldCache }()
	tests := []struct {
		name, body string
		check      func(context.Context, string, []string) availcache.CheckResult
		known      map[string]bool
		reasons    map[string]string
	}{
		{"pm-explicit", `{"status":"success","response":[true,false]}`, PremiumizeCachedDetailed, map[string]bool{"aabb": true, "ccdd": false}, nil},
		{"pm-partial", `{"status":"success","response":[true]}`, PremiumizeCachedDetailed, map[string]bool{"aabb": true}, map[string]string{"ccdd": "partial_response"}},
		{"pm-invalid-sibling", `{"status":"success","response":[true,null]}`, PremiumizeCachedDetailed, map[string]bool{"aabb": true}, map[string]string{"ccdd": "invalid_response"}},
		{"pm-failure", `{"status":"error"}`, PremiumizeCachedDetailed, nil, map[string]string{"aabb": "check_failed", "ccdd": "check_failed"}},
		{"pm-missing-status", `{"response":[false,false]}`, PremiumizeCachedDetailed, nil, map[string]string{"aabb": "invalid_response", "ccdd": "invalid_response"}},
		{"oc-explicit", `[{"cached":true},{"cached":false}]`, OffcloudCachedDetailed, map[string]bool{"aabb": true, "ccdd": false}, nil},
		{"oc-partial", `[{"cached":true}]`, OffcloudCachedDetailed, map[string]bool{"aabb": true}, map[string]string{"ccdd": "partial_response"}},
		{"oc-missing-boolean", `[{"cached":true},{}]`, OffcloudCachedDetailed, map[string]bool{"aabb": true}, map[string]string{"ccdd": "invalid_response"}},
		{"oc-error", `{"error":"private-secret"}`, OffcloudCachedDetailed, nil, map[string]string{"aabb": "invalid_response", "ccdd": "invalid_response"}},
		{"tb-positive-only", `{"success":true,"data":[{"hash":"AABB"}]}`, TorBoxCachedDetailed, map[string]bool{"aabb": true}, map[string]string{"ccdd": "positive_only_response"}},
		{"tb-empty-positive-only", `{"success":true,"data":[]}`, TorBoxCachedDetailed, nil, map[string]string{"aabb": "positive_only_response", "ccdd": "positive_only_response"}},
		{"tb-failure", `{"success":false,"error":"private-secret","data":[]}`, TorBoxCachedDetailed, nil, map[string]string{"aabb": "check_failed", "ccdd": "check_failed"}},
		{"tb-missing-success", `{"data":[]}`, TorBoxCachedDetailed, nil, map[string]string{"aabb": "invalid_response", "ccdd": "invalid_response"}},
		{"tb-invalid-sibling", `{"success":true,"data":[{"hash":"AABB"},null]}`, TorBoxCachedDetailed, map[string]bool{"aabb": true}, map[string]string{"ccdd": "invalid_response"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; io.WriteString(w, tc.body) }))
			defer server.Close()
			pmBase, ocBase, tbBase, availCache = server.URL, server.URL, server.URL, nil
			result := tc.check(context.Background(), "private-key", []string{"AABB", "CCDD"})
			if len(result.Cached) != len(tc.known) || len(result.Reasons) != len(tc.reasons) {
				t.Fatalf("result=%+v", result)
			}
			for hash, want := range tc.known {
				got, ok := result.Cached[hash]
				if !ok || got != want {
					t.Fatalf("result=%+v want %s=%v", result, hash, want)
				}
			}
			for hash, want := range tc.reasons {
				if result.Reasons[hash] != want {
					t.Fatalf("result=%+v want %s=%s", result, hash, want)
				}
			}
			if calls != 1 {
				t.Fatalf("check issued %d requests", calls)
			}
		})
	}
}
func TestDetailedProviderFailureDoesNotPopulateCache(t *testing.T) {
	oldPM, oldOC, oldTB, oldCache := pmBase, ocBase, tbBase, availCache
	defer func() { pmBase, ocBase, tbBase, availCache = oldPM, oldOC, oldTB, oldCache }()
	for _, tc := range []struct {
		name  string
		check func(context.Context, string, []string) availcache.CheckResult
	}{{"pm", PremiumizeCachedDetailed}, {"oc", OffcloudCachedDetailed}, {"tb", TorBoxCachedDetailed}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(401)
				io.WriteString(w, "private-secret")
			}))
			defer server.Close()
			pmBase, ocBase, tbBase = server.URL, server.URL, server.URL
			availCache = availcache.New(time.Minute)
			for i := 0; i < 2; i++ {
				result := tc.check(context.Background(), "private-key", []string{"aabb"})
				if len(result.Cached) != 0 || result.Reasons["aabb"] != "authentication_failed" {
					t.Fatalf("result=%+v", result)
				}
			}
			if calls != 2 {
				t.Fatalf("failure cached or extra request: %d", calls)
			}
		})
	}
}

type checkRoundTripFunc func(*http.Request) (*http.Response, error)

func (f checkRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failedBody struct{ sent bool }

func (b *failedBody) Read(p []byte) (int, error) {
	if b.sent {
		return 0, io.ErrUnexpectedEOF
	}
	b.sent = true
	return copy(p, `{"cached":false}`), io.ErrUnexpectedEOF
}
func (b *failedBody) Close() error { return nil }
func TestCacheCheckHTTPFailureClassificationAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   io.ReadCloser
		err    error
		reason string
	}{
		{"forbidden", 403, io.NopCloser(strings.NewReader("private-secret")), nil, "authentication_failed"},
		{"rate-limited", 429, io.NopCloser(strings.NewReader("private-secret")), nil, "rate_limited"},
		{"server", 503, io.NopCloser(strings.NewReader("private-secret")), nil, "upstream_http_error"},
		{"network", 0, nil, errors.New("private-key network fault"), "network_error"},
		{"timeout", 0, nil, context.DeadlineExceeded, "timeout"},
		{"cancelled", 0, nil, context.Canceled, "cancelled"},
		{"read-failed", 200, &failedBody{}, nil, "network_error"},
		{"oversized", 200, io.NopCloser(strings.NewReader(strings.Repeat(" ", maxCacheCheckBody+1))), nil, "invalid_response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: checkRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.status, Body: tc.body, Header: make(http.Header)}, nil
			})}
			req, _ := http.NewRequest(http.MethodGet, "https://test.invalid/check", nil)
			_, reason := cacheCheckRequest(context.Background(), client, "diagnostic-test-"+tc.name, req)
			if reason != tc.reason || calls != 1 {
				t.Fatalf("reason=%s requests=%d", reason, calls)
			}
		})
	}
}
