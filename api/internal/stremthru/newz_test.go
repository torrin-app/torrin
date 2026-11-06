package stremthru

import (
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
	"github.com/torrin-app/torrin/shared/usenet/nzburl"
)

// Obfuscated indexers re-roll message-ids on every fetch, so the content hash is
// unstable across fetches of the same release; the nzb link is the stable key.
// addNewz dedups on the link id so player retries don't start duplicate downloads.
func TestNewzDedupKeyIsStableLinkNotContentHash(t *testing.T) {
	gen := func(mid string) string {
		pa, _ := nzb.ParseBytes(nzb.Generate([]nzb.OutFile{{Name: "ep.mkv", Group: "a.b.test", Segments: []nzb.Segment{{MessageID: mid, Number: 1, Bytes: 100}}}}))
		return nzb.Hash(pa)
	}
	if gen("first@obf") == gen("second@obf") {
		t.Fatal("content hash should differ when message-ids are re-obfuscated")
	}
	// Same release link (even with a varying apikey/token) -> same stable dedup id.
	a := nzburl.Hash("https://indexer.example/getnzb?id=sim101&apikey=AAA")
	b := nzburl.Hash("https://indexer.example/getnzb?id=sim101&apikey=BBB")
	if a != b {
		t.Fatal("same release link should dedup to the same id regardless of apikey")
	}
	if a == nzburl.Hash("https://indexer.example/getnzb?id=sim102&apikey=AAA") {
		t.Fatal("different releases should hash distinctly")
	}
}

func TestNewzStatus(t *testing.T) {
	cases := map[jobs.Status]string{
		jobs.StatusComplete:    "downloaded",
		jobs.StatusSeeding:     "downloaded",
		jobs.StatusDownloading: "downloading",
		jobs.StatusProcessing:  "downloading",
		jobs.StatusPublishing:  "downloading",
		jobs.StatusFailed:      "failed",
		jobs.StatusPending:     "queued",
		jobs.StatusQueued:      "queued",
	}
	for in, want := range cases {
		if got := stStatus(in); got != want {
			t.Errorf("stStatus(%s)=%q want %q", in, got, want)
		}
	}
}

func TestDispositionName(t *testing.T) {
	cases := map[string]string{
		`attachment; filename="The.Show.S01E01.1080p.nzb"`: "The.Show.S01E01.1080p.nzb",
		`inline; filename=movie.nzb`:                       "movie.nzb",
		"":                                                 "",
		"attachment":                                       "",
		"garbage;;;":                                       "",
	}
	for cd, want := range cases {
		if got := dispositionName(cd); got != want {
			t.Errorf("dispositionName(%q)=%q want %q", cd, got, want)
		}
	}
}

func TestCleanNzbName(t *testing.T) {
	cases := map[string]string{
		"  The.Show.nzb ": "The.Show",
		"movie":           "movie",
		"":                "",
	}
	for in, want := range cases {
		if got := cleanNzbName(in); got != want {
			t.Errorf("cleanNzbName(%q)=%q want %q", in, got, want)
		}
	}
}
