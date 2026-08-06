package providers

import (
	"errors"
	"fmt"
	"strings"

	"github.com/torrin-app/torrin/shared/failure"
)

type adError struct{ code, message string }

func (e *adError) Error() string { return fmt.Sprintf("alldebrid %s: %s", e.code, e.message) }

func hosterFailure(err error) error {
	var ad *adError
	if errors.As(err, &ad) {
		return failure.Newf(ad.code, "%s", adReason(ad.message))
	}
	return err
}

func adReason(msg string) string {
	msg = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(msg), "."))
	if msg == "" {
		return failure.Generic.Msg
	}
	if c := msg[0]; c >= 'A' && c <= 'Z' {
		return string(c+32) + msg[1:]
	}
	return msg
}

var deadLinkCodes = map[string]bool{
	"BAD_LINK":                   true,
	"LINK_DOWN":                  true,
	"LINK_HOST_NOT_SUPPORTED":    true,
	"LINK_NOT_SUPPORTED":         true,
	"LINK_HOST_UNAVAILABLE":      true,
	"LINK_PASS_PROTECTED":        true,
	"LINK_ERROR":                 true,
	"LINK_TEMPORARY_UNAVAILABLE": true,
	"REDIRECTOR_NOT_SUPPORTED":   true,
	"REDIRECTOR_ERROR":           true,
}

func DeadLink(err error) bool {
	var f *failure.Failure
	return errors.As(err, &f) && deadLinkCodes[f.Code]
}
