package download

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Tensai75/nntpPool"
	"github.com/torrin-app/torrin/shared/usenet/decoder"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

var drainOnce sync.Once

func drainPoolLogs() {
	go func() {
		for m := range nntpPool.LogChan {
			slog.Info("nntpPool", "msg", m)
		}
	}()
	go func() {
		for e := range nntpPool.WarnChan {
			slog.Warn("nntpPool", "err", e)
		}
	}()
	go func() {
		for m := range nntpPool.DebugChan {
			slog.Debug("nntpPool", "msg", m)
		}
	}()
}

type Credentials struct {
	Host     string
	Port     int
	Username string
	Password string
	SSL      bool
	MaxConns int
}

func NewPool(c Credentials) (nntpPool.ConnectionPool, error) {
	drainOnce.Do(drainPoolLogs)
	max := uint32(c.MaxConns)
	if max == 0 {
		max = 10
	}
	return nntpPool.New(&nntpPool.Config{
		Host:                  c.Host,
		Port:                  uint32(c.Port),
		SSL:                   c.SSL,
		User:                  c.Username,
		Pass:                  c.Password,
		MaxConns:              max,
		HealthCheck:           true,
		ConnWaitTime:          5 * time.Second,
		IdleTimeout:           60 * time.Second,
		MaxConnErrors:         5,
		MaxTooManyConnsErrors: 3,
	}, 0)
}

type sharedEntry struct {
	pool nntpPool.ConnectionPool
	refs int
}

var (
	sharedMu    sync.Mutex
	sharedPools = map[string]*sharedEntry{}
)

func AcquireShared(c Credentials) (nntpPool.ConnectionPool, func(), error) {
	key := c.Host + "|" + c.Username
	sharedMu.Lock()
	defer sharedMu.Unlock()
	e, ok := sharedPools[key]
	if !ok {
		p, err := NewPool(c)
		if err != nil {
			return nil, nil, err
		}
		e = &sharedEntry{pool: p}
		sharedPools[key] = e
	}
	e.refs++
	var once sync.Once
	release := func() {
		once.Do(func() {
			sharedMu.Lock()
			defer sharedMu.Unlock()
			e.refs--
			if e.refs == 0 {
				e.pool.Close()
				delete(sharedPools, key)
			}
		})
	}
	return e.pool, release, nil
}

type Result struct {
	Name string
	Path string
	Size int64
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

func Download(ctx context.Context, pool nntpPool.ConnectionPool, n *nzb.NZB, outDir string, conns int, onProgress func(done, total int64)) ([]Result, error) {
	if conns < 1 {
		conns = 10
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	total := n.TotalSize()
	var done int64
	var results []Result

	for _, file := range n.Files {
		name := filepath.Base(fileName(file))
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
			return nil, err
		}
		missing, err := downloadFile(ctx, pool, file, group, f, &done, total, conns, onProgress)
		f.Sync()
		f.Close()
		if err != nil {
			os.Remove(partPath)
			return nil, err
		}
		if missing > 0 {
			slog.Warn("usenet: missing articles, leaving gaps for par2 repair", "file", name, "missing_mb", missing/1e6)
		}
		path := filepath.Join(outDir, name)
		if err := os.Rename(partPath, path); err != nil {
			return nil, err
		}
		size := int64(0)
		if info, err := os.Stat(path); err == nil {
			size = info.Size()
		}
		results = append(results, Result{Name: name, Path: path, Size: size})
	}
	return results, nil
}

func isOptional(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".nfo") || strings.HasSuffix(l, ".sfv") ||
		strings.HasSuffix(l, ".txt") || strings.HasSuffix(l, ".jpg") || strings.HasSuffix(l, ".png")
}

func downloadFile(ctx context.Context, pool nntpPool.ConnectionPool, file nzb.File, group string, f *os.File, done *int64, total int64, conns int, onProgress func(done, total int64)) (int64, error) {
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
			data, err := fetchSegment(ctx, pool, seg.MessageID, group)
			if err == nil {
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

func fetchSegment(ctx context.Context, pool nntpPool.ConnectionPool, msgID, group string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < segAttempts; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		conn, err := pool.Get(ctx)
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
	return nil, fmt.Errorf("segment %s after %d attempts: %w", msgID, segAttempts, lastErr)
}

func isArticleMissing(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no such article") || strings.Contains(s, "article not found") ||
		strings.Contains(s, "430 ") || strings.Contains(s, "423 ")
}

func fileName(f nzb.File) string {
	if f.Filename != "" {
		return f.Filename
	}
	return f.Subject
}
