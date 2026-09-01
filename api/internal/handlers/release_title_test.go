package handlers

import "testing"

func TestTitleFromSlug(t *testing.T) {
	cases := map[string]string{
		"https://hdencode.org/28-days-later-2002-2160p-uhd-blu-ray-remux-dv-hdr-hevc-truehd-atmos-7-1-cinephiles-59-2-gb/":      "28.days.later.2002.2160p.uhd.blu.ray.remux.dv.hdr.hevc.truehd.atmos.7.1.cinephiles",
		"https://hdencode.org/minions-and-monsters-2026-2160p-uhd-blu-ray-remux-hevc-dv-hdr-truehd-7-1-atmos-artelabo-57-1-gb/": "minions.and.monsters.2026.2160p.uhd.blu.ray.remux.hevc.dv.hdr.truehd.7.1.atmos.artelabo",
		"https://hdencode.org/some-release-1080p-web-group/":                                                                    "some.release.1080p.web.group",
	}
	for in, want := range cases {
		if got := titleFromSlug(in); got != want {
			t.Errorf("titleFromSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
