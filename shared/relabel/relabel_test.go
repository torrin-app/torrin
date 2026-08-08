package relabel

import "testing"

func TestMatchByExactSize(t *testing.T) {
	real := []NamedSize{
		{Name: "Show/S01E01 Pilot.mkv", Size: 100},
		{Name: "Show/S01E02 Two.mkv", Size: 200},
		{Name: "Show/extras/nfo.txt", Size: 5},
	}
	cached := []NamedSize{
		{Name: "aaaaaaaaaaaaaaaaaaaa.mkv", Size: 200},
		{Name: "bbbbbbbbbbbbbbbbbbbb.mkv", Size: 100},
	}
	res := Match(real, cached)
	if res.Mapping["aaaaaaaaaaaaaaaaaaaa.mkv"] != "S01E02 Two.mkv" {
		t.Errorf("wrong match: %v", res.Mapping)
	}
	if res.Mapping["bbbbbbbbbbbbbbbbbbbb.mkv"] != "S01E01 Pilot.mkv" {
		t.Errorf("wrong match: %v", res.Mapping)
	}
	if len(res.Ambiguous) != 0 || len(res.Unmatched) != 0 {
		t.Errorf("expected clean match, got ambiguous=%v unmatched=%v", res.Ambiguous, res.Unmatched)
	}
}

func TestMatchFlagsAmbiguousAndUnmatched(t *testing.T) {
	real := []NamedSize{
		{Name: "A.mkv", Size: 100},
		{Name: "B.mkv", Size: 100},
	}
	cached := []NamedSize{
		{Name: "hex1.mkv", Size: 100},
		{Name: "hex2.mkv", Size: 999},
	}
	res := Match(real, cached)
	if len(res.Mapping) != 0 {
		t.Errorf("nothing should map cleanly, got %v", res.Mapping)
	}
	if len(res.Ambiguous) != 1 || res.Ambiguous[0] != "hex1.mkv" {
		t.Errorf("expected hex1 ambiguous, got %v", res.Ambiguous)
	}
	if len(res.Unmatched) != 1 || res.Unmatched[0] != "hex2.mkv" {
		t.Errorf("expected hex2 unmatched, got %v", res.Unmatched)
	}
}
