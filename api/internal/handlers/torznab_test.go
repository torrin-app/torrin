package handlers

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/torrin-app/torrin/shared/comet"
)

func TestTorznabCaps(t *testing.T) {
	w := httptest.NewRecorder()
	torznabCaps(w)
	body := w.Body.String()
	for _, want := range []string{"<caps>", `tv-search available="yes"`, `movie-search available="yes"`, `id="5000"`, `id="2000"`} {
		if !strings.Contains(body, want) {
			t.Errorf("caps missing %q\n%s", want, body)
		}
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=3600" {
		t.Errorf("caps Cache-Control = %q, want cacheable", cc)
	}
}

func TestWriteTorznabFeed(t *testing.T) {
	w := httptest.NewRecorder()
	results := []comet.Result{{
		Title: "Some Movie 2021 1080p", Size: 1500000000, Seeders: 12, Peers: 3,
		InfoHash: "abc123def456", Cached: true,
	}}
	writeTorznabFeed(w, results, "movie")
	body := w.Body.String()
	checks := []string{
		`xmlns:torznab="http://torznab.com/schemas/2015/feed"`,
		`type="application/x-bittorrent"`,
		"magnet:?xt=urn:btih:abc123def456",
		"Some Movie 2021 1080p [CACHED]</title>", // cached tag in display title
		`<torznab:attr name="infohash" value="abc123def456"/>`,
		`<torznab:attr name="seeders" value="12"/>`,
		`<torznab:attr name="peers" value="12"/>`, // peers floored to seeders
		`<torznab:attr name="magneturl"`,
		`<torznab:attr name="category" value="2000"/>`,
		`<torznab:attr name="cached" value="true"/>`,
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("feed missing %q\n%s", c, body)
		}
	}
}

func TestDedupeTorrents(t *testing.T) {
	in := []comet.Result{
		{InfoHash: "AA", Seeders: 5, Cached: false},
		{InfoHash: "aa", Seeders: 20, Cached: false}, // same hash, higher seeders wins
		{InfoHash: "bb", Seeders: 1, Cached: true},   // cached sorts first
		{InfoHash: "", Seeders: 99},                  // dropped, no hash
	}
	out := dedupeTorrents(in)
	if len(out) != 2 {
		t.Fatalf("got %d results, want 2 (deduped, no-hash dropped)", len(out))
	}
	if out[0].InfoHash != "bb" || !out[0].Cached {
		t.Errorf("cached result should sort first, got %+v", out[0])
	}
	if out[1].Seeders != 20 {
		t.Errorf("dedupe should keep highest seeders, got %d", out[1].Seeders)
	}
}

func TestMarkCached(t *testing.T) {
	s := &Server{Deps{CachedJobs: fakeCachedLookup{"abc": {}}}}
	in := []comet.Result{{InfoHash: "ABC"}, {InfoHash: "def"}}
	out := s.markCached(context.Background(), in)
	if !out[0].Cached {
		t.Error("hash present in cache (case-insensitive) should be marked cached")
	}
	if out[1].Cached {
		t.Error("hash absent from cache should not be marked cached")
	}
}

func TestTorznabType(t *testing.T) {
	cases := []struct {
		t      string
		season int
		want   string
	}{
		{"movie", 0, "movie"},
		{"tvsearch", 0, "series"},
		{"search", 2, "series"},
		{"search", 0, "movie"},
	}
	for _, c := range cases {
		if got := torznabType(c.t, c.season); got != c.want {
			t.Errorf("torznabType(%q,%d)=%q want %q", c.t, c.season, got, c.want)
		}
	}
}

func TestNormalizeIMDB(t *testing.T) {
	for in, want := range map[string]string{"tt123": "tt123", "123": "tt123", "": "", "  456 ": "tt456"} {
		if got := normalizeIMDB(in); got != want {
			t.Errorf("normalizeIMDB(%q)=%q want %q", in, got, want)
		}
	}
}
