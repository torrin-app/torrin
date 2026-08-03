package handlers

import "testing"

func TestNormalizeIndexerURL(t *testing.T) {
	cases := map[string]string{
		"https://idx.com/":     "https://idx.com",
		"https://idx.com/api":  "https://idx.com",
		"https://idx.com/api/": "https://idx.com",
		"  https://idx.com  ":  "https://idx.com",
	}
	for in, want := range cases {
		if got := normalizeIndexerURL(in); got != want {
			t.Errorf("normalizeIndexerURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestFallbackQuery(t *testing.T) {
	if got := fallbackQuery("Silo", 1, 2); got != "Silo S01E02" {
		t.Errorf("tv episode: got %q", got)
	}
	if got := fallbackQuery("Avatar The Way of Water", 0, 0); got != "Avatar The Way of Water" {
		t.Errorf("movie: got %q", got)
	}
	if got := fallbackQuery("Show", 2, 0); got != "Show" {
		t.Errorf("partial se: got %q", got)
	}
}
