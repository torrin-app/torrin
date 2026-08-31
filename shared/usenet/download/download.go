package download

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Tensai75/nntpPool"
	"github.com/torrin-app/torrin/shared/failure"
	"github.com/torrin-app/torrin/shared/providers"
	"github.com/torrin-app/torrin/shared/usenet/decoder"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

type Result struct {
	Name string
	Path string
	Size int64
}

// ArticleFetcher retrieves a raw article body. Keeping this boundary small
// lets seekable readers and the bulk downloader share the NNTP retry policy.
type ArticleFetcher interface {
	Fetch(ctx context.Context, msgID, group string) ([]byte, error)
}

type poolArticleFetcher struct{ pools []nntpPool.ConnectionPool }

func NewArticleFetcher(pools []nntpPool.ConnectionPool) ArticleFetcher {
	return poolArticleFetcher{pools: pools}
}

func (f poolArticleFetcher) Fetch(ctx context.Context, msgID, group string) ([]byte, error) {
	return fetchSegment(ctx, f.pools, msgID, group)
}

func TestCredentials(ctx context.Context, c Credentials) error {
	pool, err := NewPool(c)
	if err != nil {
		return err
	}
	defer pool.Close()
	conn, err := pool.Get(ctx)
	if err != nil {
		return err
	}
	pool.Put(conn)
	return nil
}

func Download(ctx context.Context, pools []nntpPool.ConnectionPool, n *nzb.NZB, outDir string, conns int, onProgress func(done, total int64)) ([]Result, int64, error) {
	if conns < 1 {
		conns = 10
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, 0, err
	}
	total := n.TotalSize()
	var done, totalMissing int64
	var results []Result

	for _, file := range n.Files {
		name := filepath.Base(FileName(file))
		if isOptional(name) {
			continue
		}
		group := ""
		if len(file.Groups) > 0 {
			group = file.Groups[0]
		}
		partPath := filepath.Join(outDir, name+".part")
		f, err := os.Create(partPath)
		if err != nil {
			return nil, 0, err
		}
		missing, err := downloadFile(ctx, pools, file, group, f, &done, total, conns, onProgress)
		f.Sync()
		f.Close()
		if err != nil {
			os.Remove(partPath)
			return nil, 0, err
		}
		totalMissing += missing
		if missing > 0 {
			slog.Warn("usenet: missing articles, leaving gaps for par2 repair", "file", name, "missing_mb", missing/1e6)
		}
		path := filepath.Join(outDir, name)
		if err := os.Rename(partPath, path); err != nil {
			return nil, 0, err
		}
		size := int64(0)
		if info, err := os.Stat(path); err == nil {
			size = info.Size()
		}
		results = append(results, Result{Name: name, Path: path, Size: size})
	}
	return results, totalMissing, nil
}

func isOptional(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".nfo") || strings.HasSuffix(l, ".sfv") ||
		strings.HasSuffix(l, ".txt") || strings.HasSuffix(l, ".jpg") || strings.HasSuffix(l, ".png")
}

func downloadFile(ctx context.Context, pools []nntpPool.ConnectionPool, file nzb.File, group string, f *os.File, done *int64, total int64, conns int, onProgress func(done, total int64)) (int64, error) {
	sem := make(chan struct{}, conns)
	var wg sync.WaitGroup
	var fatal error
	var fatalOnce sync.Once
	var missing int64

	for _, seg := range file.Segments {
		wg.Add(1)
		sem <- struct{}{}
		go func(seg nzb.Segment) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			data, err := fetchSegment(ctx, pools, seg.MessageID, group)
			if err == nil {
				if lim := providers.LimiterFrom(ctx); lim != nil {
					if werr := lim.WaitN(ctx, len(data)); werr != nil {
						fatalOnce.Do(func() { fatal = werr })
						return
					}
				}
				var dec *decoder.YencResult
				if dec, err = decoder.Decode(data); err == nil {
					writeSegment(f, dec)
					n := atomic.AddInt64(done, seg.Bytes)
					if onProgress != nil && total > 0 {
						onProgress(n, total)
					}
					return
				}
			}
			if ctx.Err() != nil {
				fatalOnce.Do(func() { fatal = ctx.Err() })
				return
			}
			atomic.AddInt64(&missing, seg.Bytes)
			if n := atomic.AddInt64(done, seg.Bytes); onProgress != nil && total > 0 {
				onProgress(n, total)
			}
		}(seg)
	}
	wg.Wait()
	if fatal != nil {
		return 0, fatal
	}
	return missing, nil
}

func writeSegment(f *os.File, dec *decoder.YencResult) {
	if dec.Begin > 0 {
		f.WriteAt(dec.Data, dec.Begin-1)
	} else {
		f.Write(dec.Data)
	}
}

const segAttempts = 3

var fetchOne = fetchFromPool

func fetchSegment(ctx context.Context, pools []nntpPool.ConnectionPool, msgID, group string) ([]byte, error) {
	var lastErr error
	for _, pool := range pools {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		data, err := fetchOne(ctx, pool, msgID, group)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = failure.Interrupted
	}
	return nil, lastErr
}

func fetchFromPool(ctx context.Context, pool nntpPool.ConnectionPool, msgID, group string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < segAttempts; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		conn, err := getPoolConn(ctx, pool)
		if err != nil {
			lastErr = err
			continue
		}
		if group != "" {
			if _, _, _, err := conn.Group(group); err != nil {
				conn.Close()
				pool.Put(conn)
				lastErr = err
				continue
			}
		}
		body, err := conn.Body("<" + msgID + ">")
		if err != nil {
			conn.Close()
			pool.Put(conn)
			if isArticleMissing(err) {
				return nil, err
			}
			lastErr = err
			continue
		}
		data, err := io.ReadAll(body)
		pool.Put(conn)
		if err != nil {
			lastErr = err
			continue
		}
		return data, nil
	}
	return nil, failure.Wrap(failure.Interrupted, "segment %s after %d attempts: %v", msgID, segAttempts, lastErr)
}

type poolConnResult struct {
	conn *nntpPool.NNTPConn
	err  error
}

func getPoolConn(ctx context.Context, pool nntpPool.ConnectionPool) (*nntpPool.NNTPConn, error) {
	result := make(chan poolConnResult)
	abandoned := make(chan struct{})
	go func() {
		// nntpPool closes the entire shared pool when the context passed to Get
		// is canceled. Detach that call, while letting this request stop waiting.
		conn, err := pool.Get(context.WithoutCancel(ctx))
		select {
		case result <- poolConnResult{conn: conn, err: err}:
		case <-abandoned:
			if conn != nil {
				pool.Put(conn)
			}
		}
	}()
	select {
	case out := <-result:
		return out.conn, out.err
	case <-ctx.Done():
		close(abandoned)
		return nil, ctx.Err()
	}
}

// isArticleMissing reports whether the article is genuinely absent (NNTP 430/423)
// rather than a transient connection error, no point retrying a missing article.
func isArticleMissing(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no such article") || strings.Contains(s, "article not found") ||
		strings.Contains(s, "430 ") || strings.Contains(s, "423 ")
}

func FileName(f nzb.File) string {
	if f.Filename != "" {
		return f.Filename
	}
	return f.Subject
}
