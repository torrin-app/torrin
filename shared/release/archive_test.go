package release

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func docFrom(t *testing.T, html string) *goquery.Document {
	t.Helper()
	d, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestBestArchiveMultiQuality(t *testing.T) {
	doc := docFrom(t, `
		<a href="https://rapidgator.net/file/aaa/Show.S01E02.1080p.WEB.h264-EDITH.mkv.html">x</a>
		<a href="https://nitroflare.com/view/BBB/Show.S01E02.1080p.WEB.h264-EDITH.mkv">x</a>
		<a href="https://rapidgator.net/file/ccc/Show.S01E02.1080p.HEVC.x265-MeGusta.mkv.html">x</a>
		<a href="https://nitroflare.com/view/DDD/Show.S01E02.480p.WEB.x264-mSD.mkv.html">x</a>`)
	parts := BestArchive(doc, "Show S01E02 1080p WEB h264-EDITH")
	if len(parts) != 1 {
		t.Fatalf("want 1 part, got %d: %v", len(parts), parts)
	}
	if len(parts[0]) != 2 || !strings.Contains(parts[0][0], "rapidgator") || !strings.Contains(parts[0][1], "nitroflare") {
		t.Fatalf("want [rapidgator, nitroflare] mirrors of EDITH, got %v", parts[0])
	}
	for _, m := range parts[0] {
		if !strings.Contains(strings.ToLower(m), "edith") {
			t.Fatalf("picked wrong quality: %s", m)
		}
	}
}

func TestBestArchiveMultiPartMirrors(t *testing.T) {
	doc := docFrom(t, `
		<a href="https://rapidgator.net/file/a2/Movie.2024.part02.rar.html">x</a>
		<a href="https://rapidgator.net/file/a1/Movie.2024.part01.rar.html">x</a>
		<a href="https://nitroflare.com/view/N1/Movie.2024.part01.rar">x</a>
		<a href="https://nitroflare.com/view/N2/Movie.2024.part02.rar">x</a>`)
	parts := BestArchive(doc, "Movie 2024 1080p")
	if len(parts) != 2 {
		t.Fatalf("want 2 parts, got %d: %v", len(parts), parts)
	}
	if !strings.Contains(parts[0][0], "part01") || !strings.Contains(parts[1][0], "part02") {
		t.Fatalf("parts not sorted: %v", parts)
	}
	for i, p := range parts {
		if len(p) != 2 {
			t.Fatalf("part %d wants 2 mirrors, got %v", i, p)
		}
	}
}

func TestReleaseFileFilter(t *testing.T) {
	keep := []string{"movie.2024.1080p.mkv", "movie.2024.part01.rar", "movie.2024.rar", "movie.mp4"}
	drop := []string{"movie.nfo", "movie.srt", "movie.sfv", "movie.jpg"}
	for _, n := range keep {
		if !isReleaseFile(n) {
			t.Errorf("isReleaseFile(%q) should be true", n)
		}
	}
	for _, n := range drop {
		if isReleaseFile(n) {
			t.Errorf("isReleaseFile(%q) should be false", n)
		}
	}
	if !isSample("movie.sample.mkv") || isSample("movie.part1.rar") {
		t.Error("isSample wrong")
	}
}

func TestPartNum(t *testing.T) {
	cases := map[string]int{
		"name.part01.rar": 1,
		"name.part2.rar":  2,
		"name.part12.rar": 12,
		"name.rar":        0,
		"name.mkv":        0,
	}
	for n, want := range cases {
		if got := partNum(n); got != want {
			t.Errorf("partNum(%q)=%d want %d", n, got, want)
		}
	}
}

func TestSplitTitle(t *testing.T) {
	title, size := SplitTitle("Dune.Part.Two.2024.2160p.MA.WEB-DL.TrueHD.Atmos.7.1.DV.H.265-FLUX – 32.8 GB")
	if title != "Dune.Part.Two.2024.2160p.MA.WEB-DL.TrueHD.Atmos.7.1.DV.H.265-FLUX" {
		t.Errorf("title = %q", title)
	}
	if size != "32.8 GB" {
		t.Errorf("size = %q", size)
	}
	if tt, sz := SplitTitle("No Separator Here"); tt != "No Separator Here" || sz != "" {
		t.Errorf("got %q / %q", tt, sz)
	}
}

func TestParseSize(t *testing.T) {
	gib := float64(1 << 30)
	cases := []struct {
		in   string
		want int64
	}{
		{"131.9 GB", int64(131.9 * gib)},
		{"15.4 GB", int64(15.4 * gib)},
		{"700 MB", 700 * (1 << 20)},
		{"", 0},
		{"weird", 0},
	}
	for _, c := range cases {
		if got := ParseSize(c.in); got != c.want {
			t.Errorf("ParseSize(%q)=%d want %d", c.in, got, c.want)
		}
	}
}
