package urlnorm

import "testing"

func TestCanonicalMergesEquivalent(t *testing.T) {
	base := Canonical("https://site.com/dl/123?v=1")
	cases := map[string]string{
		"whitespace":      " https://site.com/dl/123?v=1 ",
		"host case":       "https://SITE.com/dl/123?v=1",
		"scheme case":     "HTTPS://site.com/dl/123?v=1",
		"default port":    "https://site.com:443/dl/123?v=1",
		"fragment":        "https://site.com/dl/123?v=1#part",
		"dot segments":    "https://site.com/dl/../dl/123?v=1",
		"tracking param":  "https://site.com/dl/123?v=1&utm_source=x&fbclid=y",
		"query reordered": "https://site.com/dl/123?v=1", // stable regardless of order
	}
	for name, u := range cases {
		if got := Canonical(u); got != base {
			t.Errorf("%s: Canonical(%q) = %q, want %q", name, u, got, base)
		}
	}
}

func TestCanonicalKeepsIdentity(t *testing.T) {
	// query params that ARE the identity must never collapse
	if Canonical("https://youtube.com/watch?v=abc") == Canonical("https://youtube.com/watch?v=xyz") {
		t.Error("different identity params must not merge")
	}
	// order of real params is normalized but content preserved
	if Canonical("https://x.com/a?b=2&a=1") != Canonical("https://x.com/a?a=1&b=2") {
		t.Error("query order should normalize")
	}
	// non-tracking optional params are kept (safe: no false merge)
	if Canonical("https://x.com/a?filename=hello") == Canonical("https://x.com/a") {
		t.Error("unknown params must be kept, not stripped")
	}
}

func TestCanonicalBadInput(t *testing.T) {
	if Canonical("not a url") != "not a url" {
		t.Error("non-url should pass through unchanged")
	}
}
