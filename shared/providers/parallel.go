package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const (
	minParallelBytes = 16 << 20
	chunkAttempts    = 6
)

func FetchFile(ctx context.Context, client *http.Client, url, localPath string, onProgress func(written, total int64), conns int) error {
	part := localPath + ".part"
	if err := fetchToPart(ctx, client, url, part, onProgress, conns); err != nil {
		return err
	}
	return os.Rename(part, localPath)
}

func fetchToPart(ctx context.Context, client *http.Client, url, part string, onProgress func(written, total int64), conns int) error {
	if conns > 1 {
		if total, ok := probeRange(ctx, client, url); ok && total >= minParallelBytes {
			if err := downloadParallel(ctx, client, url, part, total, conns, onProgress); err != nil {
				if ctx.Err() != nil {
					return err
				}
				os.Remove(part)
			} else {
				return nil
			}
		}
	}
	return Download(ctx, HTTPRange(ctx, client, url), part, onProgress)
}

func probeRange(ctx context.Context, client *http.Client, url string) (int64, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, false
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusPartialContent {
		return 0, false
	}
	return totalFromContentRange(resp.Header.Get("Content-Range")), true
}

func downloadParallel(ctx context.Context, client *http.Client, url, localPath string, total int64, conns int, onProgress func(written, total int64)) error {
	f, err := os.OpenFile(localPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Truncate(total); err != nil {
		return err
	}

	var progress int64
	stop := make(chan struct{})
	var reporter sync.WaitGroup
	if onProgress != nil {
		reporter.Add(1)
		go func() {
			defer reporter.Done()
			t := time.NewTicker(2 * time.Second)
			defer t.Stop()
			var last int64
			for {
				select {
				case <-stop:
					return
				case <-ctx.Done():
					return
				case <-t.C:
					if cur := atomic.LoadInt64(&progress); cur != last {
						onProgress(cur, total)
						last = cur
					}
				}
			}
		}()
	}

	chunk := total / int64(conns)
	var wg sync.WaitGroup
	errs := make([]error, conns)
	for i := 0; i < conns; i++ {
		start := int64(i) * chunk
		end := start + chunk - 1
		if i == conns-1 {
			end = total - 1
		}
		if start > end {
			continue
		}
		wg.Add(1)
		go func(i int, start, end int64) {
			defer wg.Done()
			errs[i] = fetchChunk(ctx, client, url, f, start, end, &progress)
		}(i, start, end)
	}
	wg.Wait()
	close(stop)
	reporter.Wait()

	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	if onProgress != nil {
		onProgress(total, total)
	}
	return nil
}

func fetchChunk(ctx context.Context, client *http.Client, url string, f *os.File, start, end int64, progress *int64) error {
	pos := start
	var lastErr error
	for attempt := 0; attempt < chunkAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if pos > end {
			return nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", pos, end))
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			backoff(ctx, attempt)
			continue
		}
		if resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			lastErr = fmt.Errorf("unexpected status %d", resp.StatusCode)
			backoff(ctx, attempt)
			continue
		}
		n, copyErr := copyChunkAt(ctx, resp.Body, f, pos, progress)
		resp.Body.Close()
		pos += n
		if pos > end {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = copyErr
		if lastErr == nil {
			lastErr = fmt.Errorf("short chunk: got %d of %d bytes", pos-start, end-start+1)
		}
		backoff(ctx, attempt)
	}
	return lastErr
}

func copyChunkAt(ctx context.Context, body io.Reader, f *os.File, pos int64, progress *int64) (int64, error) {
	buf := make([]byte, 256*1024)
	var written int64
	for {
		if ctx.Err() != nil {
			return written, ctx.Err()
		}
		n, readErr := body.Read(buf)
		if n > 0 {
			if _, wErr := f.WriteAt(buf[:n], pos+written); wErr != nil {
				return written, wErr
			}
			written += int64(n)
			atomic.AddInt64(progress, int64(n))
		}
		if readErr != nil {
			if readErr == io.EOF {
				return written, nil
			}
			return written, readErr
		}
	}
}
