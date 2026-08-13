package rdapi

import (
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
)

func TestExtractHash(t *testing.T) {
	h := "0123456789abcdef0123456789abcdef01234567"
	if got := extractHash("magnet:?xt=urn:btih:" + h + "&dn=x"); got != h {
		t.Errorf("got %q", got)
	}
	if extractHash("magnet:?xt=urn:btih:short") != "" {
		t.Error("invalid should be empty")
	}
}

func TestIsValidKey(t *testing.T) {
	good := "0123456789abcdef0123456789abcdef01234567/file_0/Movie.mkv"
	if !isValidKey(good) {
		t.Error("valid key rejected")
	}
	for _, bad := range []string{
		"0123456789abcdef0123456789abcdef01234567/manifest.json",
		"../../etc/passwd",
		"0123456789abcdef0123456789abcdef01234567/file_0/../x",
		"deadbeef/file_0/x.mkv",
	} {
		if isValidKey(bad) {
			t.Errorf("bad key accepted: %q", bad)
		}
	}
}

func TestMapStatus(t *testing.T) {
	cases := map[jobs.Status]string{
		jobs.StatusComplete: "downloaded", jobs.StatusDownloading: "downloading",
		jobs.StatusProcessing: "downloading", jobs.StatusPublishing: "uploading",
		jobs.StatusSeeding: "downloaded",
		jobs.StatusFailed:  "error",
		jobs.StatusPending: "magnet_conversion", jobs.StatusQueued: "queued",
	}
	for in, want := range cases {
		if got := mapStatus(in); got != want {
			t.Errorf("mapStatus(%s)=%q want %q", in, got, want)
		}
	}
}
