package rss

import (
	"testing"

	"github.com/mmcdole/gofeed"
	ext "github.com/mmcdole/gofeed/extensions"
	"github.com/torrin-app/torrin/shared/magnet"
)

func TestFetchFeedString(t *testing.T) {
	xml := `<?xml version="1.0"?><rss version="2.0"><channel><title>t</title>
	<item><title>Example.Show.S01E01.1080p.WEB.H264</title>
	<link>https://hdencode.org/example-show-s01e01/</link>
	<guid>https://hdencode.org/example-show-s01e01/</guid></item></channel></rss>`
	items, err := FetchFeedString(xml)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Link != "https://hdencode.org/example-show-s01e01/" {
		t.Errorf("link = %q", items[0].Link)
	}
	if items[0].Title == "" {
		t.Error("title missing")
	}
}

func TestMatchesFilter(t *testing.T) {
	cases := []struct {
		title, filter string
		want          bool
	}{
		{"The Show S01E01 1080p", "", true},             // empty filter matches all
		{"The Show S01E01 1080p", "1080p", true},        // single keyword
		{"The Show S01E01 720p", "1080p,2160p", false},  // no keyword matches
		{"The Show S01E01 2160p", "1080p, 2160p", true}, // spaced list, OR match
		{"Movie REPACK", "repack", true},                // case-insensitive
	}
	for _, c := range cases {
		if got := MatchesFilter(c.title, c.filter); got != c.want {
			t.Errorf("MatchesFilter(%q,%q)=%v want %v", c.title, c.filter, got, c.want)
		}
	}
}

func TestFilterMatchesStructured(t *testing.T) {
	// Havoc's case: require 1080p AND WEB-DL AND HEVC (codec values are rls's own tags).
	f := Filter{Resolutions: []string{"1080p"}, Sources: []string{"WEB-DL"}, Codecs: []string{"HEVC"}}
	cases := []struct {
		title string
		want  bool
	}{
		{"Abcd.S01E01.1080p.WEB-DL.HEVC-GRP", true},  // all three match
		{"Abcd.S01E01.1080p.web-dl.hevc-GRP", true},  // case-insensitive
		{"Abcd.S01E01.1080p.WEBRip.HEVC-GRP", false}, // wrong source
		{"Abcd.S01E01.720p.WEB-DL.HEVC-GRP", false},  // wrong resolution
		{"Abcd.S01E01.1080p.WEB-DL.x265-GRP", false}, // x265 is a distinct rls tag from HEVC
		{"Abcd.S01E01.1080p.WEB-DL.x264-GRP", false}, // wrong codec
	}
	for _, c := range cases {
		if got := f.Matches(c.title); got != c.want {
			t.Errorf("structured %q = %v, want %v", c.title, got, c.want)
		}
	}
}

func TestFilterMatchesSDR(t *testing.T) {
	// SDR means "no HDR tag", not a literal "SDR" token in the title.
	f := Filter{Resolutions: []string{"1080p"}, HDR: []string{"SDR"}}
	cases := []struct {
		title string
		want  bool
	}{
		{"Movie.2026.1080p.WEB-DL.x264-GRP", true},         // untagged = SDR
		{"Movie.2026.1080p.WEB-DL.DV.HDR.H265-GRP", false}, // HDR/DV rejected
		{"Movie.2026.1080p.WEB-DL.HDR10Plus-GRP", false},   // HDR10+ rejected
	}
	for _, c := range cases {
		if got := f.Matches(c.title); got != c.want {
			t.Errorf("SDR %q = %v, want %v", c.title, got, c.want)
		}
	}
}

func TestKnownTagsFromRLS(t *testing.T) {
	for _, cat := range []string{"resolution", "source", "codec"} {
		if len(KnownTags(cat)) == 0 {
			t.Errorf("expected rls vocabulary for %q, got none", cat)
		}
	}
}

func TestFilterMatchesOther(t *testing.T) {
	if !(Filter{}).Matches("anything 1080p") {
		t.Error("empty filter should match all")
	}
	// OR within a category.
	or := Filter{Resolutions: []string{"1080p", "2160p"}}
	if !or.Matches("X.2160p.WEB-DL-GRP") || or.Matches("X.720p.WEB-DL-GRP") {
		t.Error("resolution OR failed")
	}
	// Exclude.
	ex := Filter{Resolutions: []string{"1080p"}, Except: "cam, hindi"}
	if ex.Matches("Movie.1080p.WEB-DL.HINDI-GRP") {
		t.Error("except should reject hindi")
	}
	if !ex.Matches("Movie.1080p.WEB-DL-GRP") {
		t.Error("except should pass clean title")
	}
	// Freetext match: space = AND, comma = OR (on raw title).
	m := Filter{Match: "remux atmos, 2160p"}
	if !m.Matches("Movie.2160p.WEB-DL.DDP-GRP") {
		t.Error("match OR group (2160p) should pass")
	}
	if m.Matches("Movie.1080p.WEB-DL.DDP-GRP") {
		t.Error("neither group matched, should fail")
	}
	// Year range.
	y := Filter{YearMin: 2020, YearMax: 2022}
	if !y.Matches("Movie.2021.1080p.WEB-DL-GRP") || y.Matches("Movie.2019.1080p.WEB-DL-GRP") {
		t.Error("year range failed")
	}
}

func TestParseFilterRoundTrip(t *testing.T) {
	f := ParseFilter([]byte(`{"resolutions":["1080p"],"codecs":["HEVC"]}`))
	if len(f.Resolutions) != 1 || f.Resolutions[0] != "1080p" || f.Codecs[0] != "HEVC" {
		t.Fatalf("parse failed: %+v", f)
	}
	if (Filter{}).Empty() != true || f.Empty() != false {
		t.Error("Empty() wrong")
	}
}

func TestExtractMagnet(t *testing.T) {
	const m = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=x"
	if got := extractMagnet(&gofeed.Item{Link: m}); got != m {
		t.Errorf("link magnet = %q", got)
	}
	if got := extractMagnet(&gofeed.Item{Description: "see " + m + " here"}); got != m {
		t.Errorf("description magnet = %q", got)
	}
	if got := extractMagnet(&gofeed.Item{Link: "https://example.com/x"}); got != "" {
		t.Errorf("non-magnet should be empty, got %q", got)
	}
}

func TestExtractMagnetFromInfoHash(t *testing.T) {
	hash := "5a181a17d765ee945c1786503bd8bdba67085226"

	nyaa := &gofeed.Item{
		Title: "Test",
		Extensions: ext.Extensions{
			"nyaa": {"infoHash": []ext.Extension{{Name: "infoHash", Value: hash}}},
		},
	}
	if got := magnet.Hash(extractMagnet(nyaa)); got != hash {
		t.Fatalf("nyaa infoHash: got %q want %q", got, hash)
	}

	torznab := &gofeed.Item{
		Title: "Test",
		Extensions: ext.Extensions{
			"torznab": {"attr": []ext.Extension{{Name: "attr", Attrs: map[string]string{"name": "infohash", "value": hash}}}},
		},
	}
	if got := magnet.Hash(extractMagnet(torznab)); got != hash {
		t.Fatalf("torznab infohash: got %q want %q", got, hash)
	}
}

func TestSizeOK(t *testing.T) {
	f := Filter{SizeMinBytes: 1_000_000_000, SizeMaxBytes: 10_000_000_000} // 1GB..10GB
	cases := []struct {
		size int64
		want bool
	}{
		{0, true},             // unknown size -> allow (never silently drop)
		{500_000_000, false},  // under 1GB
		{5_000_000_000, true}, // in range
		{12_000_000_000, false},
	}
	for _, c := range cases {
		if got := f.SizeOK(c.size); got != c.want {
			t.Errorf("SizeOK(%d)=%v want %v", c.size, got, c.want)
		}
	}
	if !(Filter{}).SizeOK(5_000_000_000) {
		t.Error("no size filter should allow any size")
	}
}

func TestExtractSize(t *testing.T) {
	encl := &gofeed.Item{Enclosures: []*gofeed.Enclosure{{URL: "x.torrent", Length: "2314659356"}}}
	if got := extractSize(encl); got != 2314659356 {
		t.Errorf("enclosure length size = %d", got)
	}
	attr := &gofeed.Item{Extensions: ext.Extensions{
		"newznab": {"attr": []ext.Extension{{Name: "attr", Attrs: map[string]string{"name": "size", "value": "7238898314"}}}},
	}}
	if got := extractSize(attr); got != 7238898314 {
		t.Errorf("newznab size attr = %d", got)
	}
	if got := extractSize(&gofeed.Item{Title: "Movie 2021 1080p"}); got != 0 {
		t.Errorf("no size info should be 0, got %d", got)
	}
}
