package comet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearch(t *testing.T) {
	var gotQuery, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotKey = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"title":"A","size":42,"seeders":7,"infohash":"deadbeef","cached":true}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "sekret")
	res, err := c.Search(context.Background(), Params{Type: "series", IMDB: "tt99", Season: 1, Episode: 2, Limit: 50})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if gotKey != "sekret" {
		t.Errorf("X-Api-Key = %q, want sekret", gotKey)
	}
	for _, want := range []string{"type=series", "imdb=tt99", "season=1", "ep=2", "limit=50"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
	if len(res) != 1 || res[0].InfoHash != "deadbeef" || res[0].Seeders != 7 || !res[0].Cached {
		t.Errorf("unexpected results: %+v", res)
	}
}

func TestSearchStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "").Search(context.Background(), Params{IMDB: "tt1"}); err == nil {
		t.Error("expected error on non-200 status")
	}
}
