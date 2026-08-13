package handlers

import "testing"

func TestRemoteTorrentState(t *testing.T) {
	cases := []struct {
		progress float64
		speed    int64
		want     string
	}{
		{4.5, 120000, "downloading"},
		{4.5, 0, "stalled"},
		{0, 0, "fetching metadata"},
		{0, 5000, "downloading"},
	}
	for _, c := range cases {
		if got := remoteTorrentState(c.progress, c.speed); got != c.want {
			t.Errorf("progress=%.1f speed=%d: got %q want %q", c.progress, c.speed, got, c.want)
		}
	}
}
