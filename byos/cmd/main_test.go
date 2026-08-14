package main

import (
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
)

func TestQueueActionFor(t *testing.T) {
	done := func(node string) *jobs.Job { return &jobs.Job{Status: jobs.StatusComplete, Node: node} }
	cases := []struct {
		name     string
		job      *jobs.Job
		nodeID   string
		mirrored bool
		want     queueAction
	}{
		{"own node, unmirrored -> mirror", done("box2"), "box2", false, actionMirror},
		{"box1 primary node -> mirror", done(""), "", false, actionMirror},
		{"other node's job -> skip, never delete", done(""), "box2", false, actionSkip},
		{"other node's job (reverse) -> skip", done("box2"), "", false, actionSkip},
		{"already mirrored -> delete", done("box2"), "box2", true, actionDelete},
		{"not complete -> delete", &jobs.Job{Status: jobs.StatusDownloading, Node: "box2"}, "box2", false, actionDelete},
		{"missing job -> delete", nil, "box2", false, actionDelete},
	}
	for _, c := range cases {
		if got := queueActionFor(c.job, c.nodeID, c.mirrored); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
