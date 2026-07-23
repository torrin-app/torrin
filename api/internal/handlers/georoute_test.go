package handlers

import (
	"net/http"
	"testing"
)

func reqWithGeo(lat, lng string) *http.Request {
	r, _ := http.NewRequest("GET", "/", nil)
	if lat != "" {
		r.Header.Set("CF-IPLatitude", lat)
		r.Header.Set("CF-IPLongitude", lng)
	}
	return r
}

func TestNearestRelay(t *testing.T) {
	cases := []struct {
		name     string
		lat, lng float64
		want     string
	}{
		{"Chennai", 13.08, 80.27, "beam-sg.torrin.app"},
		{"Tokyo", 35.6, 139.7, "beam-sg.torrin.app"},
		{"Berlin", 52.5, 13.4, "beam-eu.torrin.app"},
		{"New York", 40.7, -74.0, "beam-eu.torrin.app"},
		{"Lagos", 6.45, 3.4, "beam-za.torrin.app"},
		{"Cape Town", -33.9, 18.4, "beam-za.torrin.app"},
	}
	for _, c := range cases {
		if got := nearestRelay(c.lat, c.lng); got != c.want {
			t.Errorf("%s: nearestRelay=%s want %s", c.name, got, c.want)
		}
	}
}

func TestGeorouteURL(t *testing.T) {
	in := "https://beam-eu.torrin.app/abc/all.zip?expires=1&sig=xyz"

	got := georouteURL(reqWithGeo("6.45", "3.4"), in)
	if got != "https://beam-za.torrin.app/abc/all.zip?expires=1&sig=xyz" {
		t.Errorf("Lagos should route to beam-za, got %q", got)
	}

	ext := "https://real-debrid.com/d/abc"
	if got := georouteURL(reqWithGeo("6.45", "3.4"), ext); got != ext {
		t.Errorf("non-relay host must be untouched, got %q", got)
	}

	if got := georouteURL(reqWithGeo("", ""), in); got != in {
		t.Errorf("no geo headers must be untouched, got %q", got)
	}

	if got := georouteURL(reqWithGeo("52.5", "13.4"), in); got != in {
		t.Errorf("already-nearest host must be untouched, got %q", got)
	}
}
