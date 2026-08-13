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
