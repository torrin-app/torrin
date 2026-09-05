package episodes

import (
	"context"
	"github.com/torrin-app/torrin/shared/jobs"
	"testing"
)

// Filename/catalog-only fixture from the reported pack; no account data or URLs.
func TestReportedCombinedStoryPack(t *testing.T) {
	r := New(testCatalog{
		{Season: 13, Number: 1, Name: "Pups Solve a Snap-N-Wrap Problem"},
		{Season: 13, Number: 2, Name: "Pups Save Helga and the Dingers"},
		{Season: 13, Number: 3, Name: "Pups Save the Orienteers"},
		{Season: 13, Number: 4, Name: "Pups Save Teatime"},
		{Season: 13, Number: 5, Name: "Pups and the Alien Grandpa"},
		{Season: 13, Number: 6, Name: "Pups Save a Lemon-Loving Duckling"},
		{Season: 13, Number: 7, Name: "Pups Save a Hum-Kicker"},
		{Season: 13, Number: 8, Name: "Pups Save the One-Man Band"},
	})
	files := []jobs.File{
		{Index: 0, Name: "PAW.Patrol.S13E01.Pups.Solve.a.Snap-N-Wrap.Problem - Pups.Save.Helga.and.the.Dingers.mkv"},
		{Index: 1, Name: "PAW.Patrol.S13E02.Pups.Save.the.Orienteers - Pups.Save.Teatime.mkv"},
		{Index: 2, Name: "PAW.Patrol.S13E03.Pups.and.the.Alien.Grandpa - Pups.Save.a.Lemon-Loving.Duckling.mkv"},
		{Index: 3, Name: "PAW.Patrol.S13E04.Pups.Save.a.Hum-Kicker - Pups.Save.the.One-Man.Band.mkv"},
	}
	for e := 1; e <= 8; e++ {
		got := r.Select(context.Background(), "tt3121722", nil, files, 13, e)
		wantIndex := (e - 1) / 2 // Expected fixture mapping, never production selection logic.
		if len(got) != 1 || got[0].Index != wantIndex {
			t.Errorf("E%d files=%+v, want index %d", e, got, wantIndex)
		}
	}
}
