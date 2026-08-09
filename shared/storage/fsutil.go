package storage

import (
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/torrin-app/torrin/shared/video"
)

const ctypeSidecarSuffix = ".ctype"

func ctypeFor(key, path string) string {
	if strings.HasSuffix(key, ".json") {
		return "application/json"
	}
	if ct := video.ContentType(key); ct != "application/octet-stream" {
		return ct
	}
	if b, err := os.ReadFile(path + ctypeSidecarSuffix); err == nil && len(b) > 0 {
		return strings.TrimSpace(string(b))
	}
	return "application/octet-stream"
}

func ParseRange(header string, total int64) (start, end int64, ok bool) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, false
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, 0, false
	}
	lo, hi := spec[:dash], spec[dash+1:]
	if lo == "" {
		n, err := strconv.ParseInt(hi, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > total {
			n = total
		}
		return total - n, total - 1, true
	}
	start, err := strconv.ParseInt(lo, 10, 64)
	if err != nil || start >= total {
		return 0, 0, false
	}
	end = total - 1
	if hi != "" {
		if e, err := strconv.ParseInt(hi, 10, 64); err == nil && e < end {
			end = e
		}
	}
	if end < start {
		return 0, 0, false
	}
	return start, end, true
}

type limitedFile struct {
	f         *os.File
	remaining int64
}

func (l *limitedFile) Read(p []byte) (int, error) {
	if l.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > l.remaining {
		p = p[:l.remaining]
	}
	n, err := l.f.Read(p)
	l.remaining -= int64(n)
	return n, err
}

func (l *limitedFile) Close() error { return l.f.Close() }
