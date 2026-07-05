package release

import (
	"context"
	"testing"

	"github.com/torrin-app/torrin/shared/usenet/indexer"
)

func eps(nums ...int) map[int]*indexer.Result {
	m := map[int]*indexer.Result{}
	for _, n := range nums {
		m[n] = &indexer.Result{ID: "e", Title: "ep"}
	}
	return m
}

func TestSeasonLengthGapless(t *testing.T) {
	f := &UsenetFallback{}
	ctx := context.Background()

	if n, err := f.seasonLength(ctx, "", 1, eps(1, 2, 3)); err != nil || n != 3 {
		t.Fatalf("contiguous 1-3: got %d, %v", n, err)
	}
	if _, err := f.seasonLength(ctx, "", 1, eps(1, 2, 4)); err == nil {
		t.Fatal("internal gap should fail")
	}
	if _, err := f.seasonLength(ctx, "", 1, eps(2, 3)); err == nil {
		t.Fatal("missing E01 should fail")
	}
}

func TestBestMatch(t *testing.T) {
	results := []indexer.Result{
		{Title: "Silo.S01E02.Holstons.Pick.2160p.ATVP.WEB-DL.DDP5.1.H.265-NTb", Size: 2_800_000_000},
		{Title: "Some.Other.Show.S03E04.1080p.WEB.h264-XYZ", Size: 1_000_000_000},
	}
	best := bestMatch(results, "Silo S01E02 2160p ATVP WEB-DL H.265-NTb", 2_800_000_000, "")
	if best == nil || best.Title != results[0].Title {
		t.Fatalf("wrong match: %+v", best)
	}
	if got := bestMatch([]indexer.Result{results[1]}, "Silo S01E02 2160p ATVP", 0, ""); got != nil {
		t.Fatalf("unrelated result should not match: %+v", got)
	}
	wrongIMDB := []indexer.Result{{Title: "Silo.S01E02.2160p.ATVP.WEB-DL.H.265-NTb", Size: 2_800_000_000, IMDBID: "9999999"}}
	if got := bestMatch(wrongIMDB, "Silo S01E02 2160p ATVP", 2_800_000_000, "14688458"); got != nil {
		t.Fatalf("imdb mismatch should be rejected: %+v", got)
	}
}

func TestTitleMatchesRealShows(t *testing.T) {
	job := "Ozark S01E08 1080p WEB H264-CAKES"
	cases := map[string]bool{
		"Ozark.S01E08.Kaleidoscope.1080p.WEBRip.DD5.1.H265-d3g":       true, // real Ozark, diff group/quality
		"Ozark.S01E08.Kaleidoscope.1080p.NF.WEB-DL.DDP5.1.H.264-Kit":  true,
		"Ozark.Law.S01E08.1080p.WEB.h264-EDITH":                       false, // different show
		"This.is.America.Charlie.Brown.S01E08.1080p.WEB.h264-DOLORES": false, // the wrong grab
		"Succession.S01E08.Prague.2160p.AMZN.WEB-DL.H.265":            false,
	}
	for result, want := range cases {
		if got := titleMatches(result, job); got != want {
			t.Errorf("titleMatches(%q, Ozark)=%v want %v", result, got, want)
		}
	}
}
