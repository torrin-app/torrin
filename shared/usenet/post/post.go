package post

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Tensai75/nntpPool"
	"github.com/torrin-app/torrin/shared/usenet/download"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
	"golang.org/x/sync/errgroup"
)

const (
	postAttempts    = 5
	postMaxConns    = 10
	postConcurrency = 10
)

type Config struct {
	Host, Port, Username, Password, Group, From string
}

func (c Config) Enabled() bool { return c.Host != "" && c.Username != "" }

type FileInput struct {
	Name string
	Size int64
	Open func() (io.ReadCloser, error)
}

type nntpConn interface {
	RawPost(io.Reader) error
	Close()
}

type connGetter interface {
	Get(ctx context.Context) (nntpConn, error)
	Put(nntpConn)
}

type Poster struct {
	cfg  Config
	pool connGetter
}

func New(cfg Config) (*Poster, error) {
	port, err := strconv.Atoi(cfg.Port)
	if err != nil {
		return nil, fmt.Errorf("post port: %w", err)
	}
	pool, err := download.NewPool(download.Credentials{
		Host: cfg.Host, Port: port, SSL: true,
		Username: cfg.Username, Password: cfg.Password, MaxConns: postMaxConns,
	})
	if err != nil {
		return nil, err
	}
	return &Poster{cfg: cfg, pool: poolAdapter{pool}}, nil
}

func (p *Poster) Post(ctx context.Context, files []FileInput) ([]byte, error) {
	from := p.cfg.From
	if from == "" {
		from = randFrom()
	}
	out := make([]nzb.OutFile, 0, len(files))
	for i, f := range files {
		slog.Info("cairn: posting file", "name", f.Name, "size", f.Size, "n", i+1, "of", len(files))
		posted, err := p.postFile(ctx, p.postArticle, from, i+1, len(files), f)
		if err != nil {
			slog.Warn("cairn: post file failed", "name", f.Name, "err", err)
			return nil, err
		}
		slog.Info("cairn: posted file", "name", f.Name, "segments", len(posted.segments))
		out = append(out, nzb.OutFile{
			Subject: posted.subject, Name: posted.name,
			Group: p.cfg.Group, Segments: posted.segments,
		})
	}
	return nzb.Generate(out), nil
}

type postedFile struct {
	subject, name string
	segments      []nzb.Segment
}

type articlePoster func(ctx context.Context, build func(msgID string) []byte) (string, error)

func (p *Poster) postFile(ctx context.Context, poster articlePoster, from string, fileNum, fileTotal int, f FileInput) (postedFile, error) {
	total := (f.Size + partSize - 1) / partSize
	if total == 0 {
		total = 1
	}
	out := postedFile{subject: randSubject(), name: randName()}
	src, err := f.Open()
	if err != nil {
		return out, fmt.Errorf("open %s: %w", f.Name, err)
	}
	defer src.Close()
	date := time.Now().UTC().Format(time.RFC1123Z)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(postConcurrency)
	var mu sync.Mutex
	var segs []nzb.Segment
	var done int64

	var begin int64
	var readErr error
	for part := int64(1); ; part++ {
		chunk := make([]byte, partSize)
		n, rerr := io.ReadFull(src, chunk)
		if n > 0 {
			chunk, pnum, pbegin := chunk[:n], part, begin
			g.Go(func() error {
				msgID, err := poster(gctx, func(msgID string) []byte {
					return buildArticle(articleMeta{
						From: from, Group: p.cfg.Group, MessageID: msgID, Date: date,
						Subject: out.subject, Name: out.name,
						FileNum: fileNum, FileTotal: fileTotal,
						PartNum: pnum, PartTotal: total,
						FileSize: f.Size, Begin: pbegin,
					}, chunk)
				})
				if err != nil {
					return fmt.Errorf("post %s part %d: %w", out.name, pnum, err)
				}
				mu.Lock()
				segs = append(segs, nzb.Segment{MessageID: strings.Trim(msgID, "<>"), Number: int(pnum), Bytes: int64(len(chunk))})
				mu.Unlock()
				if d := atomic.AddInt64(&done, 1); d%100 == 0 {
					slog.Info("cairn: post progress", "file", out.name, "part", d, "of", total)
				}
				return nil
			})
			begin += int64(n)
		}
		if rerr != nil {
			if rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
				readErr = rerr
			}
			break
		}
		if gctx.Err() != nil {
			break
		}
	}
	if err := g.Wait(); err != nil {
		return out, err
	}
	if readErr != nil {
		return out, readErr
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].Number < segs[j].Number })
	out.segments = segs
	return out, nil
}

func (p *Poster) postArticle(ctx context.Context, build func(msgID string) []byte) (string, error) {
	var lastErr error
	for attempt := 0; attempt < postAttempts; attempt++ {
		conn, err := p.pool.Get(ctx)
		if err != nil {
			slog.Warn("cairn: pool get failed", "attempt", attempt, "err", err)
			lastErr = err
			continue
		}
		msgID := randMessageID()
		if err := conn.RawPost(bytes.NewReader(build(msgID))); err != nil {
			slog.Warn("cairn: rawpost failed", "attempt", attempt, "err", err)
			conn.Close()
			p.pool.Put(conn)
			lastErr = err
			continue
		}
		p.pool.Put(conn)
		return msgID, nil
	}
	return "", lastErr
}

type poolAdapter struct{ p nntpPool.ConnectionPool }

func (a poolAdapter) Get(ctx context.Context) (nntpConn, error) {
	c, err := a.p.Get(ctx)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (a poolAdapter) Put(c nntpConn) { a.p.Put(c.(*nntpPool.NNTPConn)) }
