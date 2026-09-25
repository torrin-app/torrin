package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/usenet/indexer"
)

func TestReqBase(t *testing.T) {
	mk := func(host string, hdrs map[string]string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/newznab/api", nil)
		r.Host = host
		for k, v := range hdrs {
			r.Header.Set(k, v)
		}
		return r
	}
	cases := []struct {
		name, host string
		hdrs       map[string]string
		want       string
	}{
		{"public defaults https", "api.torrin.app", nil, "https://api.torrin.app"},
		{"explicit forwarded http", "api.torrin.app", map[string]string{"X-Forwarded-Proto": "http"}, "http://api.torrin.app"},
		{"cloudflare visitor https", "api.torrin.app", map[string]string{"CF-Visitor": `{"scheme":"https"}`}, "https://api.torrin.app"},
		{"lan self-host http", "192.168.1.5:9092", nil, "http://192.168.1.5:9092"},
		{"localhost http", "localhost:9092", nil, "http://localhost:9092"},
	}
	for _, c := range cases {
		if got := reqBase(mk(c.host, c.hdrs)); got != c.want {
			t.Errorf("%s: reqBase=%q want %q", c.name, got, c.want)
		}
	}
}

func TestNewznabCaps(t *testing.T) {
	w := httptest.NewRecorder()
	newznabCaps(w)
	body := w.Body.String()
	for _, want := range []string{"<caps>", `tv-search available="yes"`, "supportedParams=\"q,imdbid,season,ep\"", `id="5000"`} {
		if !strings.Contains(body, want) {
			t.Errorf("caps missing %q\n%s", want, body)
		}
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=3600" {
		t.Errorf("caps Cache-Control = %q, want cacheable", cc)
	}
}

func TestNewznabError(t *testing.T) {
	w := httptest.NewRecorder()
	newznabError(w, 100, "Incorrect user credentials")
	if !strings.Contains(w.Body.String(), `<error code="100" description="Incorrect user credentials"/>`) {
		t.Errorf("bad error body: %s", w.Body.String())
	}
}

func TestNewznabFeed(t *testing.T) {
	w := httptest.NewRecorder()
	results := []indexer.Result{{
		ID: "abc123", Title: "Show & Co S01E01", Size: 1500000000,
		Source: "system", Category: "5040", IMDBID: "tt99", Grabs: 5,
		PubDate: time.Unix(1600000000, 0).UTC(),
	}}
	writeNewznabFeed(w, "https://api.torrin.app", "tr_key", results)
	body := w.Body.String()
	checks := []string{
		"newznab/getnzb?id=abc123",
		"s=system",
		"apikey=tr_key",
		"Show &amp; Co S01E01", // title xml-escaped
		"&amp;s=system",        // ampersand escaped in the enclosure attr url
		`<newznab:attr name="size" value="1500000000"/>`,
		`<newznab:attr name="imdb" value="99"/>`,
		`type="application/x-nzb"`,
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("feed missing %q\n%s", c, body)
		}
	}
}
