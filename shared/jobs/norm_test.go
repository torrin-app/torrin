package jobs

import "testing"

func TestNormTitle(t *testing.T) {
	cases := map[string]string{
		"The Matrix":            "matrix",    // stopword dropped
		"The Lord of the Rings": "lordrings", // stopwords "the","of" dropped
		"Avatar 2009":           "avatar",    // year token dropped
		"Mr. Robot":             "mrrobot",   // punctuation collapsed
		"2012":                  "2012",      // year-only falls back to raw
		"The":                   "the",       // all-stopword falls back to raw
	}
	for in, want := range cases {
		if got := NormTitle(in); got != want {
			t.Errorf("NormTitle(%q)=%q want %q", in, got, want)
		}
	}
}

func TestTitleNormFromName(t *testing.T) {
	cases := map[string]string{
		"The.Matrix.1999.1080p.BluRay.x264":                                                 "matrix",
		"www.1TamilMV.tf - Anjaan (2014) Tamil BluRay - 1080p - x264":                       "anjaan",
		"www.1TamilMV.earth - Dhool (2003) Tamil TRUE WEB-DL - 1080p":                       "dhool",
		"[MM] Pathaan (2023) Hindi HDRip .mkv":                                              "pathaan",
		"[Ex-torrenty.org]The.Big.Bang.Theory.S02.MULTi.1080p.HMAX.WEB-DL.DDP2.0.H264-Ralf": "bigbangtheory",
		"[Erai-raws] One Piece - 1168 [1080p CR WEB-DL AVC AAC][MultiSub][9C645A80]":        "onepiece",
		"Avatar.Fire.and.Ash.2025.1080p.AMZN.WEB-DL.DDP5.1.Atmos.H.264-BYNDR":               "avatarfireash",
	}
	for name, want := range cases {
		if got := titleNormFromName(name); got != want {
			t.Errorf("titleNormFromName(%q) = %q, want %q", name, got, want)
		}
	}
}
