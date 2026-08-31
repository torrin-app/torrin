package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const maxDownloadAttempts = 6

var backoff = Backoff

type RangeOpener func(offset int64) (body io.ReadCloser, total int64, full bool, err error)

func HTTPRange(ctx context.Context, client *http.Client, url string) RangeOpener {
	return func(offset int64) (io.ReadCloser, int64, bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, false, err
		}
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, false, err
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			return nil, 0, false, fmt.Errorf("unexpected status %d", resp.StatusCode)
		}
		full := offset > 0 && resp.StatusCode == http.StatusOK
		total := resp.ContentLength
		if resp.StatusCode == http.StatusPartialContent {
			total = totalFromContentRange(resp.Header.Get("Content-Range"))
		}
		return resp.Body, total, full, nil
	}
}

func Download(ctx context.Context, open RangeOpener, localPath string, onProgress func(written, total int64)) error {
	f, err := os.OpenFile(localPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := resilient(ctx, open, f, onProgress); err != nil {
		if ctx.Err() == nil {
			os.Remove(localPath)
		}
		return err
	}
	return nil
}

func resilient(ctx context.Context, open RangeOpener, f *os.File, onProgress func(written, total int64)) error {
	var written, total int64
	var lastErr error

	if fi, err := f.Stat(); err == nil && fi.Size() > 0 {
		written = fi.Size()
	}

	for attempt := 0; attempt < maxDownloadAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		body, sz, full, err := open(written)
		if err != nil {
			lastErr = err
			backoff(ctx, attempt)
			continue
		}

		if full && written > 0 {
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				body.Close()
				return err
			}
			if err := f.Truncate(0); err != nil {
				body.Close()
				return err
			}
			written = 0
		} else if written > 0 {
			if _, err := f.Seek(written, io.SeekStart); err != nil {
				body.Close()
				return err
			}
		}
		if sz > 0 {
			total = sz
		}

		n, copyErr, fatal := copyBody(ctx, body, f, written, total, onProgress)
		body.Close()
		written = n

		if fatal {
			return copyErr
		}
		if copyErr == nil {
			if total > 0 && written < total {
				lastErr = fmt.Errorf("truncated: got %d of %d bytes", written, total)
				backoff(ctx, attempt)
				continue
			}
			if onProgress != nil {
				onProgress(written, total)
			}
			return nil
		}

		lastErr = copyErr
		backoff(ctx, attempt)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("download failed after %d attempts", maxDownloadAttempts)
	}
	return lastErr
}

func copyBody(ctx context.Context, body io.Reader, f io.Writer, written, total int64, onProgress func(written, total int64)) (int64, error, bool) {
	buf := make([]byte, 256*1024)
	lastUpdate := time.Now()
	lim := LimiterFrom(ctx)

	for {
		if ctx.Err() != nil {
			return written, ctx.Err(), true
		}
		n, readErr := body.Read(buf)
		if n > 0 {
			if _, wErr := f.Write(buf[:n]); wErr != nil {
				return written, wErr, true
			}
			written += int64(n)
			addBytes(ctx, n)
			if lim != nil {
				if err := lim.WaitN(ctx, n); err != nil {
					return written, err, true
				}
			}
			if onProgress != nil && time.Since(lastUpdate) >= 2*time.Second {
				onProgress(written, total)
				lastUpdate = time.Now()
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return written, nil, false
			}
			return written, readErr, false
		}
	}
}

func Backoff(ctx context.Context, attempt int) {
	d := time.Duration(1<<uint(attempt)) * time.Second
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func totalFromContentRange(cr string) int64 {
	i := strings.LastIndex(cr, "/")
	if i < 0 || i+1 >= len(cr) {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(cr[i+1:]), 10, 64)
	return n
}
