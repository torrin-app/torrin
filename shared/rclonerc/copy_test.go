package rclonerc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCopyFileAsyncAndStatus(t *testing.T) {
	var copyIn map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var in map[string]any
		json.Unmarshal(body, &in)
		switch r.URL.Path {
		case "/operations/copyfile":
			copyIn = in
			w.Write([]byte(`{"jobid": 42}`))
		case "/job/status":
			w.Write([]byte(`{"finished": true, "success": true}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL)

	id, err := c.CopyFileAsync(context.Background(), ":http,url=http://x", "src/t/j/0", "u_x:", "p/file.mkv", "byos/j/0")
	if err != nil || id != 42 {
		t.Fatalf("copyfile id=%d err=%v", id, err)
	}
	if copyIn["_async"] != true || copyIn["_group"] != "byos/j/0" || copyIn["srcFs"] != ":http,url=http://x" || copyIn["dstFs"] != "u_x:" {
		t.Errorf("copyfile params wrong: %v", copyIn)
	}

	st, err := c.JobStatus(context.Background(), 42)
	if err != nil || !st.Finished || !st.Success {
		t.Fatalf("status = %+v err=%v", st, err)
	}
}
