package indexer

import "testing"

func TestTitleMatch(t *testing.T) {
	job := "Ozark S01E08 1080p WEB H264-CAKES"
	cases := map[string]bool{
		"Ozark.S01E08.Kaleidoscope.1080p.WEBRip.DD5.1.H265-d3g":       true,
		"Ozark.S01E08.Kaleidoscope.1080p.NF.WEB-DL.DDP5.1.H.264-Kit":  true,
		"Ozark.Law.S01E08.1080p.WEB.h264-EDITH":                       false,
		"This.is.America.Charlie.Brown.S01E08.1080p.WEB.h264-DOLORES": false,
		"Succession.S01E08.Prague.2160p.AMZN.WEB-DL.H.265":            false,
	}
	for result, want := range cases {
		if got := TitleMatch(result, job); got != want {
			t.Errorf("TitleMatch(%q, Ozark)=%v want %v", result, got, want)
		}
	}
}

func TestIMDBEqual(t *testing.T) {
	if !IMDBEqual("tt1234567", "1234567") {
		t.Error("tt-prefixed and bare should be equal")
	}
	if !IMDBEqual("", "1234567") || !IMDBEqual("tt1", "") {
		t.Error("empty on either side should pass")
	}
	if IMDBEqual("tt1", "tt2") {
		t.Error("different ids should not be equal")
	}
}

func TestVerify(t *testing.T) {
	results := []Result{
		{Title: "Avatar.The.Way.of.Water.2022.2160p.WEB-DL.DDP5.1.H.265-XYZ", IMDBID: "1630029"},
		{Title: "Avatar.2009.1080p.BluRay.x264-GRP", IMDBID: "0499549"},
		{Title: "The.Last.Airbender.2010.1080p.BluRay.x264-GRP"},
	}
	got := Verify(results, "1630029", "Avatar: The Way of Water", 0, 0)
	if len(got) != 1 || got[0].IMDBID != "1630029" {
		t.Fatalf("movie verify: got %+v", got)
	}

	eps := []Result{
		{Title: "Silo.S01E02.Holstons.Pick.2160p.ATVP.WEB-DL.H.265-NTb"},
		{Title: "Silo.S01E03.Machine.2160p.ATVP.WEB-DL.H.265-NTb"},
		{Title: "Silo.Law.S01E02.1080p.WEB.h264-EDITH"},
	}
	got = Verify(eps, "", "Silo", 1, 2)
	if len(got) != 1 || got[0].Title != eps[0].Title {
		t.Fatalf("episode verify: got %+v", got)
	}
}
