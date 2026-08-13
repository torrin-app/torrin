package torrent

import (
	"testing"

	"github.com/torrin-app/torrin/shared/qbit"
)

func TestSeedTargetMet(t *testing.T) {
	week := int64(7 * 24 * 3600)
	month := int64(30 * 24 * 3600)
	cases := []struct {
		ratio float64
		secs  int64
		want  bool
	}{
		{1.0, week, true},
		{2.0, week + 1, true},
		{0.9, week, false},
		{1.5, week - 1, false},
		{0.2, month, true},
		{0, 0, false},
	}
	for _, c := range cases {
		got := seedTargetMet(&qbit.Torrent{Ratio: c.ratio, SeedingTime: c.secs})
		if got != c.want {
			t.Errorf("ratio=%.1f secs=%d: got %v want %v", c.ratio, c.secs, got, c.want)
		}
	}
}
