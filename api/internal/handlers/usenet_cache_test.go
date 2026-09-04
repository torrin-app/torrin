package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/jobs"
)

func TestReadNZBBody(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("nzb", "x.nzb")
	fw.Write([]byte("<nzb>multi</nzb>"))
	mw.Close()
	r := httptest.NewRequest("POST", "/api/jobs/nzb", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if got, err := readNZBBody(r); err != nil || string(got) != "<nzb>multi</nzb>" {
		t.Fatalf("multipart: got %q err %v", got, err)
	}

	r2 := httptest.NewRequest("POST", "/api/jobs/nzb", strings.NewReader("<nzb>raw</nzb>"))
	if got, err := readNZBBody(r2); err != nil || string(got) != "<nzb>raw</nzb>" {
		t.Fatalf("raw: got %q err %v", got, err)
	}
}

func TestUserJobForHash(t *testing.T) {
	sibs := []*jobs.Job{
		{UserID: "other", Status: jobs.StatusComplete},
		{UserID: "u1", Status: jobs.StatusFailed},
		{UserID: "u1", Status: jobs.StatusComplete},
	}
	if j := userJobForHash(sibs, "u1"); j == nil || j.Status != jobs.StatusComplete {
		t.Fatalf("should return u1's non-failed job, got %+v", j)
	}
	if userJobForHash(sibs, "nobody") != nil {
		t.Error("no job for unknown user")
	}
	if userJobForHash([]*jobs.Job{{UserID: "u1", Status: jobs.StatusFailed}}, "u1") != nil {
		t.Error("a failed job should not count (allows retry)")
	}
	if userJobForHash([]*jobs.Job{{UserID: "u1", Status: jobs.StatusEvicted}}, "u1") != nil {
		t.Error("an evicted job should not count (allows restore)")
	}
}

func TestRecentlyFailed(t *testing.T) {
	sibs := []*jobs.Job{
		{UserID: "u1", Status: jobs.StatusComplete, UpdatedAt: time.Now()},
		{UserID: "u1", Status: jobs.StatusFailed, UpdatedAt: time.Now().Add(-2 * time.Hour)},
	}
	if recentlyFailed(sibs, "u1", 30*time.Minute) != nil {
		t.Error("a stale failure past the cooldown should allow retry")
	}
	sibs = append(sibs, &jobs.Job{UserID: "u1", Status: jobs.StatusFailed, UpdatedAt: time.Now()})
	if recentlyFailed(sibs, "u1", 30*time.Minute) == nil {
		t.Error("a fresh failure within the cooldown should suppress re-grab")
	}
	if recentlyFailed(sibs, "other", 30*time.Minute) != nil {
		t.Error("another user's failure must not suppress this user")
	}
}

func TestLockGrabSerializes(t *testing.T) {
	unlock := lockGrab("h1")
	done := make(chan struct{})
	go func() { u := lockGrab("h1"); u(); close(done) }()
	select {
	case <-done:
		t.Fatal("second grab of same hash should block until the first releases")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second grab should proceed after release")
	}
	// different hashes must not block each other
	a := lockGrab("a")
	b := lockGrab("b")
	a()
	b()
}
