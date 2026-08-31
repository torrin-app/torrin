package hdencode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestIsPost(t *testing.T) {
	cases := map[string]bool{
		"https://hdencode.org/dune-part-two-2024-1080p-bluray-h264-risehd-30-5-gb/": true,
		"https://hdencode.org/advanced-search/":                                     false,
		"https://hdencode.org/watchlist/":                                           false,
		"https://hdencode.org/category/movies/":                                     false,
		"https://hdencode.org/tag/1080p/":                                           false,
		"https://other.com/dune-part-two-2024-1080p/":                               false,
		"https://hdencode.org/short/":                                               false, // too short
	}
	for href, want := range cases {
		if got := isPost(href); got != want {
			t.Errorf("isPost(%q)=%v want %v", href, got, want)
		}
	}
}

func TestFilterEp(t *testing.T) {
	rs := []Result{
		{Title: "Show.S01E05.1080p.WEB-DL"},
		{Title: "Show.S01E06.1080p.WEB-DL"},
		{Title: "Show.S02E05.1080p.WEB-DL"},
		{Title: "Show.S01.1080p.BluRay.x264-GRP"},
	}
	got := filterEp(rs, 1, 5)
	if len(got) != 2 {
		t.Fatalf("want episode + season pack (2), got %d: %+v", len(got), got)
	}
	if len(filterEp(rs, 0, 0)) != 4 {
		t.Error("movie (season 0) should return all")
	}
}

func TestResolveViaSolver(t *testing.T) {
	revealed := `<html><body>
		<a href="https://rapidgator.net/file/abc/Movie.2026.1080p-GRP.part1.rar.html">p1</a>
		<a href="https://rapidgator.net/file/def/Movie.2026.1080p-GRP.part2.rar.html">p2</a>
	</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/resolve" {
			t.Errorf("path = %s, want /resolve", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"html": revealed})
	}))
	defer srv.Close()

	archives, err := NewClient(srv.URL).Resolve(context.Background(), "https://hdencode.org/some-movie-2026-1080p-grp-9-gb/", "", "Movie")
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("got %d packagings, want 1 (rar only)", len(archives))
	}
	if len(archives[0]) != 2 {
		t.Fatalf("got %d parts, want 2 (multi-part rar)", len(archives[0]))
	}
}

func TestResolveNoSolver(t *testing.T) {
	_, err := NewClient("").Resolve(context.Background(), "https://hdencode.org/some-movie-2026-1080p-grp-9-gb/", "", "Movie")
	if err == nil {
		t.Fatal("expected error when solver not configured")
	}
}

func TestStripURL(t *testing.T) {
	inner := context.DeadlineExceeded
	ue := &url.Error{Op: "Post", URL: "http://gluetun:8090/resolve", Err: inner}
	got := stripURL(ue)
	if got != inner {
		t.Fatalf("stripURL should unwrap to the underlying error, got %v", got)
	}
	if strings.Contains(got.Error(), "gluetun") || strings.Contains(got.Error(), "http") {
		t.Fatalf("stripped error must not leak the internal solver URL: %q", got.Error())
	}
	plain := errors.New("boom")
	if stripURL(plain) != plain {
		t.Fatal("non-url errors must pass through unchanged")
	}
}
