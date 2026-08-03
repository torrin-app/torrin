package failure

import (
	"errors"
	"fmt"
)

type Failure struct {
	Code string
	Msg  string
}

func (f *Failure) Error() string { return f.Msg }

func Newf(code, format string, args ...any) *Failure {
	return &Failure{Code: code, Msg: fmt.Sprintf(format, args...)}
}

func Wrap(f *Failure, format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), f)
}

func Message(err error) string {
	var f *Failure
	if errors.As(err, &f) {
		return f.Msg
	}
	if err != nil {
		return Generic.Msg
	}
	return ""
}

var (
	Blocked        = &Failure{"blocked", "blocked by content policy"}
	EngineDown     = &Failure{"engine_down", "download engine unavailable, please retry"}
	AddFailed      = &Failure{"add_failed", "couldn't start this download, please retry"}
	NotOnUsenet    = &Failure{"not_on_usenet", "not available on usenet"}
	UsenetNotSetup = &Failure{"usenet_not_setup", "usenet isn't set up for your account"}
	SeasonPartial  = &Failure{"season_partial", "only some episodes of this season are on usenet"}
	Corrupted      = &Failure{"corrupted", "this release is corrupted and couldn't be repaired"}
	Unpack         = &Failure{"unpack", "couldn't unpack this release"}
	Interrupted    = &Failure{"interrupted", "download interrupted, please retry"}
	NoSources      = &Failure{"no_sources", "no working sources for this release right now"}
	NoVideo        = &Failure{"no_video", "no playable video found in this release"}
	Upstream       = &Failure{"upstream", "source temporarily unavailable, please retry"}
	Generic        = &Failure{"generic", "download failed, please retry"}
)
