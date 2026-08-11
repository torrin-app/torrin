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
