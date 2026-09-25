package handlers

import "testing"

func TestRSSExhausted(t *testing.T) {
	for n := 1; n < rssMaxAttempts; n++ {
		if rssExhausted(n) {
			t.Errorf("attempt %d should still retry (cap %d)", n, rssMaxAttempts)
		}
	}
	if !rssExhausted(rssMaxAttempts) {
		t.Errorf("attempt %d should give up (cap %d)", rssMaxAttempts, rssMaxAttempts)
	}
	if !rssExhausted(rssMaxAttempts + 5) {
		t.Error("past the cap should give up")
	}
}

func TestReleaseSourceForItemLinks(t *testing.T) {
	cases := map[string]string{
		"https://hdencode.org/feed/":                       "hdencode",
		"https://hdencode.org/some-2160p-release-43-8-gb/": "hdencode",
		"https://www.hdencode.org/x/":                      "hdencode",
		"https://scene-rls.net/x/":                         "scenerls",
		"https://myfeed.trycloudflare.com/rss":             "",
		"magnet:?xt=urn:btih:abc":                          "",
	}
	for u, want := range cases {
		if got := string(releaseSourceFor(u)); got != want {
			t.Errorf("releaseSourceFor(%q) = %q, want %q", u, got, want)
		}
	}
}
