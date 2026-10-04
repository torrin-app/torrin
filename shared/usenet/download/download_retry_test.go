package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Tensai75/nntpPool"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

func TestDownloadFileRetriesTransient(t *testing.T) {
	orig := fetchOne
	defer func() { fetchOne = orig }()
	origBackoff := retryBackoff
	retryBackoff = 0
	defer func() { retryBackoff = origBackoff }()

	good := []byte("=ybegin line=128 size=4 name=x.bin\r\nklmn\r\n=yend size=4\r\n")
	var bFails int32
	fetchOne = func(_ context.Context, _ nntpPool.ConnectionPool, mid, _ string) ([]byte, error) {
		if mid == "b" && atomic.AddInt32(&bFails, 1) == 1 {
			return nil, errors.New("connection reset by peer")
		}
		return good, nil
	}

	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	file := nzb.File{Segments: []nzb.Segment{{MessageID: "a", Bytes: 4}, {MessageID: "b", Bytes: 4}}}
	var done int64
	missing, err := downloadFile(context.Background(), make([]nntpPool.ConnectionPool, 1), file, "grp", f, &done, 8, 4, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if missing != 0 {
		t.Errorf("transient failure should be recovered on retry, got missing=%d", missing)
	}
}

func TestDownloadFileMissingNotRetried(t *testing.T) {
	orig := fetchOne
	defer func() { fetchOne = orig }()
	retryBackoff = 0

	var calls int32
	fetchOne = func(_ context.Context, _ nntpPool.ConnectionPool, mid, _ string) ([]byte, error) {
		if mid == "b" {
			atomic.AddInt32(&calls, 1)
			return nil, errors.New("430 no such article")
		}
		return []byte("=ybegin line=128 size=4 name=x.bin\r\nklmn\r\n=yend size=4\r\n"), nil
	}
	dir := t.TempDir()
	f, _ := os.Create(filepath.Join(dir, "out"))
	defer f.Close()
	file := nzb.File{Segments: []nzb.Segment{{MessageID: "a", Bytes: 4}, {MessageID: "b", Bytes: 4}}}
	var done int64
	missing, err := downloadFile(context.Background(), make([]nntpPool.ConnectionPool, 1), file, "grp", f, &done, 8, 4, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if missing != 4 {
		t.Errorf("genuinely-missing article should count as missing, got %d", missing)
	}
	if calls != 1 {
		t.Errorf("a 430 article must not be retried, got %d attempts", calls)
	}
}
