package stremthru

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/torrentclaw"
)

type diagnosticItem struct {
	Hash        string `json:"hash"`
	Status      string `json:"status"`
	Diagnostics *struct {
		Version         int                `json:"version"`
		BudgetExhausted bool               `json:"budget_exhausted"`
		Checks          []checkObservation `json:"checks"`
	} `json:"diagnostics"`
}

func checkFixture(t *testing.T, h *Handler, ctx context.Context, query string) ([]diagnosticItem, string) {
	t.Helper()
	r := httptest.NewRequest("GET", "/v0/store/magnets/check?"+query, nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.checkMagnets(w, r, &auth.User{ID: "fixture-user", PlanID: "free"})
	if w.Code != 200 {
		t.Fatalf("HTTP %d", w.Code)
	}
	var response struct {
		Data struct {
			Items []diagnosticItem `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data.Items, w.Body.String()
}

func observation(t *testing.T, item diagnosticItem, tier string) checkObservation {
	t.Helper()
	if item.Diagnostics == nil || item.Diagnostics.Version != 1 {
		t.Fatal("missing versioned diagnostics")
	}
	for _, o := range item.Diagnostics.Checks {
		if o.Tier == tier {
			return o
		}
	}
	t.Fatalf("tier %s missing", tier)
	return checkObservation{}
}

type failingCachedLookup struct{ err error }

func (f failingCachedLookup) CachedByHashes(context.Context, []string) (map[string]*jobs.Job, error) {
	return nil, f.err
}

func TestCheckDiagnosticsOptInAndNoProviderConfigured(t *testing.T) {
	h := New(Deps{CachedJobs: fakeCachedLookup{}})
	q := "hash=" + strings.Repeat("a", 40)
	legacy, _ := checkFixture(t, h, context.Background(), q)
	if legacy[0].Diagnostics != nil {
		t.Fatal("legacy wire response changed")
	}
	items, _ := checkFixture(t, h, context.Background(), q+"&diagnostics=1")
	if items[0].Status != "unknown" {
		t.Fatal("disabled checks became availability")
	}
	if got := observation(t, items[0], "warm_cache"); got.Outcome != "miss" {
		t.Fatalf("lookup result=%+v", got)
	}
	if got := observation(t, items[0], "system_alldebrid"); got.Reason != "not_configured" {
		t.Fatalf("provider result=%+v", got)
	}
	if got := observation(t, items[0], "personal_premiumize"); got.Reason != "plan_not_eligible" {
		t.Fatalf("plan result=%+v", got)
	}
}

func TestCheckDiagnosticsDatabaseFailureIsNotMissAndNoSecret(t *testing.T) {
	h := New(Deps{CachedJobs: failingCachedLookup{errors.New("postgres://fixture-private-secret")}})
	items, body := checkFixture(t, h, context.Background(), "hash="+strings.Repeat("b", 40)+"&diagnostics=1")
	if strings.Contains(body, "fixture-private-secret") {
		t.Fatal("raw error leaked")
	}
	for _, tier := range []string{"warm_cache", "cross_node_cache"} {
		if got := observation(t, items[0], tier); got.Reason != "database_error" || got.Outcome != "error" {
			t.Fatalf("result=%+v", got)
		}
	}
	if items[0].Status != "unknown" {
		t.Fatal("database failure became a negative")
	}
}

func TestCheckDiagnosticsStorageTimeoutIsNotMissingFile(t *testing.T) {
	hash := strings.Repeat("c", 40)
	h := New(Deps{CachedJobs: fakeCachedLookup{hash: {InfoHash: hash, Files: []jobs.File{{Name: "movie.mkv"}}}}, Store: fakeStore{err: context.DeadlineExceeded}})
	items, _ := checkFixture(t, h, context.Background(), "hash="+hash+"&diagnostics=1")
	if got := observation(t, items[0], "warm_cache"); got.Reason != "timeout" || got.Outcome != "error" {
		t.Fatalf("result=%+v", got)
	}
	if items[0].Diagnostics.BudgetExhausted {
		t.Fatal("storage timeout confused with whole request deadline")
	}
}

type diagnosticTransport func(*http.Request) (*http.Response, error)

func (f diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCheckDiagnosticsUpstreamFailuresAndPartialResults(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, tc := range []struct {
		name   string
		status int
		body   string
		err    error
		reason string
	}{
		{"auth", 401, `{"secret":"fixture-private-secret"}`, nil, "authentication_failed"},
		{"rate limit", 429, `{}`, nil, "rate_limited"},
		{"server", 503, `{}`, nil, "upstream_http_error"},
		{"schema", 200, `not JSON fixture-private-secret`, nil, "invalid_response"},
		{"timeout", 0, "", context.DeadlineExceeded, "timeout"},
		{"partial", 200, `{"cached":{"` + a + `":false}}`, nil, "not_cached"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			old := http.DefaultTransport
			http.DefaultTransport = diagnosticTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "torrentclaw.com" {
					t.Error("unexpected fixture request")
				}
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			defer func() { http.DefaultTransport = old }()
			h := New(Deps{CachedJobs: fakeCachedLookup{}, TC: torrentclaw.New("fixture-private-secret"), SysADKey: "fixture-debrid-secret"})
			items, body := checkFixture(t, h, context.Background(), "hash="+a+","+b+"&diagnostics=1")
			if calls != 1 {
				t.Fatalf("calls=%d want one original batched request", calls)
			}
			if strings.Contains(body, "fixture-private-secret") || strings.Contains(body, "fixture-debrid-secret") {
				t.Fatal("diagnostics leaked credentials/body")
			}
			got := observation(t, items[0], "system_alldebrid")
			if got.Reason != tc.reason {
				t.Fatalf("reason=%s want %s", got.Reason, tc.reason)
			}
			if tc.name == "partial" {
				if observation(t, items[1], "system_alldebrid").Reason != "partial_response" {
					t.Fatal("omission became a false miss")
				}
			} else if got.Outcome != "error" {
				t.Fatalf("failure outcome=%s", got.Outcome)
			}
			if items[0].Status != "unknown" || items[1].Status != "unknown" {
				t.Fatal("diagnostics changed uncertain availability")
			}
		})
	}
}

func TestCheckDiagnosticsPreservesActualCachedFile(t *testing.T) {
	hash := strings.Repeat("d", 40)
	h := directCairnHandler(t, hash)
	items, _ := checkFixture(t, h, context.Background(), "hash="+hash+"&diagnostics=1")
	if items[0].Status != "cached" || observation(t, items[0], "durable_archive").Outcome != "hit" {
		t.Fatal("cached file proof lost")
	}
}

func TestCheckTraceConcurrentAndFixedCodes(t *testing.T) {
	trace := &checkTrace{byHash: make(map[string]map[string]checkObservation)}
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() { defer wg.Done(); trace.record("hash", "system_alldebrid", "raw-secret-error") }()
	}
	wg.Wait()
	items := []map[string]any{{"hash": "hash", "status": "unknown"}}
	trace.attach(context.Background(), items)
	data, _ := json.Marshal(items)
	if strings.Contains(string(data), "raw-secret") {
		t.Fatal("unapproved reason serialized")
	}
}
