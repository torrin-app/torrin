package handlers

import "testing"

func TestExtractInfoHash(t *testing.T) {
	hash := "c12fe1c06bba254a9dc9f519b335aa7c1367a88a"
	cases := map[string]string{
		"magnet:?xt=urn:btih:" + hash + "&dn=movie": hash,
		hash: hash, // bare hash
		"magnet:?xt=urn:btih:C12FE1C06BBA254A9DC9F519B335AA7C1367A88A": hash, // uppercase
		"not a magnet":              "",
		"magnet:?xt=urn:btih:short": "",
	}
	for in, want := range cases {
		if got := extractInfoHash(in); got != want {
			t.Errorf("extractInfoHash(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsWebURL(t *testing.T) {
	web := []string{
		"http://example.com/v",
		"https://youtube.com/watch?v=abc",
		"  https://vimeo.com/1  ", // trimmed
		"HTTPS://YT.BE/x",         // case-insensitive
	}
	for _, u := range web {
		if !isWebURL(u) {
			t.Errorf("isWebURL(%q) = false, want true", u)
		}
	}
	notWeb := []string{
		"magnet:?xt=urn:btih:abc",
		"c12fe1c06bba254a9dc9f519b335aa7c1367a88a",
		"ftp://example.com/x",
		"",
	}
	for _, u := range notWeb {
		if isWebURL(u) {
			t.Errorf("isWebURL(%q) = true, want false", u)
		}
	}
}

func TestURLKey(t *testing.T) {
	a := urlKey("https://youtube.com/watch?v=abc")
	if len(a) != 40 {
		t.Fatalf("urlKey length = %d, want 40 (sha1 hex)", len(a))
	}
	if urlKey(" https://youtube.com/watch?v=abc ") != a {
		t.Error("urlKey should be stable across surrounding whitespace")
	}
	if urlKey("https://youtube.com/watch?v=xyz") == a {
		t.Error("different URLs should yield different keys")
	}
}
