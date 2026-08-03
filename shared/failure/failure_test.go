package failure

import (
	"errors"
	"testing"
)

func TestMessage(t *testing.T) {
	if got := Message(NotOnUsenet); got != "not available on usenet" {
		t.Errorf("sentinel: got %q", got)
	}
	if got := Message(Wrap(NotOnUsenet, "no matching nzb for %s", "Silo")); got != "not available on usenet" {
		t.Errorf("wrapped sentinel should resolve through context: got %q", got)
	}
	if got := Message(Wrap(Corrupted, "par2 failed")); got != Corrupted.Msg {
		t.Errorf("corrupted: got %q", got)
	}
	if got := Message(errors.New("dial tcp 1.2.3.4: connection refused")); got != Generic.Msg {
		t.Errorf("unknown error must be generic, not leaked: got %q", got)
	}
	if got := Message(nil); got != "" {
		t.Errorf("nil: got %q", got)
	}
}

func TestNewf(t *testing.T) {
	f := Newf("too_large", "nzb too large (%dGB, max %dGB)", 94, 50)
	if Message(f) != "nzb too large (94GB, max 50GB)" {
		t.Errorf("dynamic message wrong: %q", f.Msg)
	}
}
