package release

import (
	"errors"
	"testing"

	"github.com/torrin-app/torrin/shared/failure"
)

func TestDeadLinksErr(t *testing.T) {
	err := deadLinksErr()
	if !errors.Is(err, ErrSourceUnavailable) {
		t.Fatal("dead part must trigger the usenet fallback via ErrSourceUnavailable")
	}
	if got := failure.Message(err); got != failure.DeadLinks.Msg {
		t.Fatalf("user message = %q, want %q", got, failure.DeadLinks.Msg)
	}
}

func TestReleaseName(t *testing.T) {
	cases := []struct {
		archives [][][]string
		want     string
	}{
		{[][][]string{{{"https://rapidgator.net/file/x/Grand.Theft.Auto.VI.2026.2160p.WEB-DL.H.264-Kitsune.mkv"}}}, "Grand.Theft.Auto.VI.2026.2160p.WEB-DL.H.264-Kitsune.mkv"},
		{[][][]string{{{"https://rapidgator.net/file/x/Movie.2024.part01.rar.html"}, {"https://rapidgator.net/file/y/Movie.2024.part02.rar.html"}}}, "Movie.2024"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := releaseName(c.archives); got != c.want {
			t.Errorf("releaseName = %q, want %q", got, c.want)
		}
	}
}
