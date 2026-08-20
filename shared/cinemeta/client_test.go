package cinemeta

import "testing"

func TestParseRuntimeMinutes(t *testing.T) {
	cases := map[string]int{
		"159 min":  159,
		"104 min":  104,
		"2h 41min": 161,
		"1h 30min": 90,
		"90":       90,
		"":         0,
	}
	for in, want := range cases {
		if got := parseRuntimeMinutes(in); got != want {
			t.Errorf("parseRuntimeMinutes(%q) = %d, want %d", in, got, want)
		}
	}
}
