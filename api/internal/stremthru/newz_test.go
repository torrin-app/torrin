package stremthru

import (
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
)

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
