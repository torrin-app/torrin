package rclonerc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func recordingServer(calls *[]map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var in map[string]any
		json.Unmarshal(body, &in)
		*calls = append(*calls, in)
		w.Write([]byte("{}"))
	}))
}

func TestEnsureUserRemotePlain(t *testing.T) {
	var calls []map[string]any
	srv := recordingServer(&calls)
	defer srv.Close()

	remote, err := New(srv.URL).EnsureUserRemote(context.Background(), "user123", "koofr", map[string]string{"user": "x"}, true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if remote != "u_user123" {
		t.Errorf("remote = %q, want u_user123", remote)
	}
	if len(calls) != 1 {
		t.Fatalf("want 1 config/create, got %d", len(calls))
	}
	if calls[0]["name"] != "u_user123" || calls[0]["type"] != "koofr" {
		t.Errorf("unexpected create: %v", calls[0])
	}
}

func TestEnsureUserRemoteCrypt(t *testing.T) {
	var calls []map[string]any
	srv := recordingServer(&calls)
	defer srv.Close()

	remote, err := New(srv.URL).EnsureUserRemote(context.Background(), "user123", "koofr", map[string]string{"user": "x"}, true, "sekret", "")
	if err != nil {
		t.Fatal(err)
	}
	if remote != "u_user123" {
		t.Errorf("remote = %q, want the crypt wrapper u_user123", remote)
	}
	if len(calls) != 2 {
		t.Fatalf("want base + crypt creates, got %d", len(calls))
	}
	if calls[0]["name"] != "u_user123_src" || calls[0]["type"] != "koofr" {
		t.Errorf("base remote wrong: %v", calls[0])
	}
	if calls[1]["name"] != "u_user123" || calls[1]["type"] != "crypt" {
		t.Errorf("crypt remote wrong: %v", calls[1])
	}
	p, _ := calls[1]["parameters"].(map[string]any)
	if p["remote"] != "u_user123_src:" {
		t.Errorf("crypt should wrap the base remote, got remote=%v", p["remote"])
	}
	if p["filename_encryption"] != "standard" {
		t.Errorf("filenames must be encrypted, got %v", p["filename_encryption"])
	}
	if p["password"] != "sekret" {
		t.Errorf("crypt password not passed through: %v", p["password"])
	}
}

func TestCreateRemoteS3ListVersion(t *testing.T) {
	var calls []map[string]any
	srv := recordingServer(&calls)
	defer srv.Close()
	c := New(srv.URL)

	if err := c.CreateRemote(context.Background(), "s3rem", "s3", map[string]string{"endpoint": "x"}, false); err != nil {
		t.Fatal(err)
	}
	if p, _ := calls[0]["parameters"].(map[string]any); p["list_version"] != "2" {
		t.Errorf("s3 list_version = %v, want 2", p["list_version"])
	}

	calls = nil
	if err := c.CreateRemote(context.Background(), "kr", "koofr", map[string]string{"user": "x"}, false); err != nil {
		t.Fatal(err)
	}
	if p, _ := calls[0]["parameters"].(map[string]any); p["list_version"] != nil {
		t.Errorf("non-s3 must not get list_version, got %v", p["list_version"])
	}
}

func TestErrorAuth(t *testing.T) {
	auth := []*Error{
		{Status: 401},
		{Status: 403},
		{Status: 500, Msg: "Update mkParentDir failed: Unauthorized: 401 Unauthorized"},
		{Status: 500, Msg: "403 Forbidden"},
	}
	for _, e := range auth {
		if !e.Auth() {
			t.Errorf("expected Auth()=true for %+v", e)
		}
	}
	notAuth := []*Error{
		{Status: 404, Msg: "directory not found"},
		{Status: 500, Msg: "connection reset by peer"},
		{Status: 429, Msg: "rate limited"},
	}
	for _, e := range notAuth {
		if e.Auth() {
			t.Errorf("expected Auth()=false for %+v", e)
		}
	}
}
