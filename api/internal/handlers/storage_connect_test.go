package handlers

import (
	"errors"
	"testing"

	"github.com/torrin-app/torrin/shared/rclonerc"
)

func TestStorageAccessMsgSurfacesProviderReason(t *testing.T) {
	err := &rclonerc.Error{Method: "operations/list", Status: 500, Msg: `couldn't list files: Error "error-notPremium"`}
	if got := storageAccessMsg(err, "fallback"); got != err.Msg {
		t.Fatalf("want provider reason %q, got %q", err.Msg, got)
	}
}

func TestStorageAccessMsgFallsBack(t *testing.T) {
	if got := storageAccessMsg(errors.New("boom"), "fallback"); got != "fallback" {
		t.Fatalf("plain error should use fallback, got %q", got)
	}
	if got := storageAccessMsg(&rclonerc.Error{Method: "x", Status: 500}, "fallback"); got != "fallback" {
		t.Fatalf("empty-msg rclone error should use fallback, got %q", got)
	}
}
