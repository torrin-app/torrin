package download

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/Tensai75/nntpPool"
	"github.com/torrin-app/torrin/shared/usenet/decoder"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

const (
	raCacheMax    = 48
	prefetchAhead = 16
	prefetchConns = 8
	maxPadRun     = 4
	maxPadTotal   = 64
)

var ErrTooManyHoles = errors.New("usenet: release too damaged to stream")

type segChunk struct {
	begin int64
	end   int64
	data  []byte
	hole  bool
}

type segFlight struct {
	done chan struct{}
	err  error
}

type segFetch func(ctx context.Context, seg nzb.Segment) (*decoder.YencResult, error)

type FileReaderAt struct {
	ctx   context.Context
	file  nzb.File
	fetch segFetch

	readMu sync.Mutex

	mu       sync.Mutex
	cache    map[int]*segChunk
	order    []int
	inflight map[int]*segFlight
	typ      int64

	size     int64
	padTotal int
	lastHole int
	curRun   int

	sem chan struct{}
}

func NewFileReaderAt(ctx context.Context, pools []nntpPool.ConnectionPool, file nzb.File, group string) *FileReaderAt {
	return newReaderAt(ctx, file, func(c context.Context, s nzb.Segment) (*decoder.YencResult, error) {
		raw, err := fetchSegment(c, pools, s.MessageID, group)
		if err != nil {
			return nil, err
		}
		return decoder.Decode(raw)
	})
}

func newReaderAt(ctx context.Context, file nzb.File, fetch segFetch) *FileReaderAt {
	return &FileReaderAt{
		ctx: ctx, file: file, fetch: fetch,
		cache: map[int]*segChunk{}, inflight: map[int]*segFlight{},
		size: -1, lastHole: -1, sem: make(chan struct{}, prefetchConns),
	}
}

func (r *FileReaderAt) fetchSeg(i int) (*segChunk, error) {
	r.mu.Lock()
	if c := r.cache[i]; c != nil {
		r.mu.Unlock()
		return c, nil
	}
	if f := r.inflight[i]; f != nil {
		r.mu.Unlock()
		<-f.done
		r.mu.Lock()
		c, err := r.cache[i], f.err
		r.mu.Unlock()
		if c != nil {
			return c, nil
		}
		return nil, err
	}
	f := &segFlight{done: make(chan struct{})}
	r.inflight[i] = f
	r.mu.Unlock()

	dec, err := r.fetch(r.ctx, r.file.Segments[i])

	r.mu.Lock()
	delete(r.inflight, i)
	if err != nil {
		f.err = err
		close(f.done)
		r.mu.Unlock()
		return nil, err
	}
	begin := dec.Begin - 1
	if begin < 0 {
		begin = 0
	}
	c := &segChunk{begin: begin, end: begin + int64(len(dec.Data)), data: dec.Data}
	r.store(i, c)
	if i == 0 {
		r.typ = int64(len(dec.Data))
	}
	close(f.done)
	r.mu.Unlock()
	return c, nil
}

func (r *FileReaderAt) seg(i int) (*segChunk, error) {
	c, err := r.fetchSeg(i)
	if err == nil {
		return c, nil
	}
	if isArticleMissing(err) {
		return r.holeChunk(i)
	}
	return nil, err
}

func (r *FileReaderAt) holeChunk(i int) (*segChunk, error) {
	r.mu.Lock()
	size := r.typ
	r.mu.Unlock()
	if size <= 0 {
		size = r.file.Segments[i].Bytes
	}
	var begin int64
	if i > 0 {
		prev, err := r.seg(i - 1)
		if err != nil {
			return nil, err
		}
		begin = prev.end
	}
	if i == r.lastHole+1 {
		r.curRun++
	} else {
		r.curRun = 1
	}
	r.lastHole = i
	r.padTotal++
	if r.curRun > maxPadRun || r.padTotal > maxPadTotal {
		return nil, ErrTooManyHoles
	}
	c := &segChunk{begin: begin, end: begin + size, data: make([]byte, size), hole: true}
	r.mu.Lock()
	r.store(i, c)
	r.mu.Unlock()
	return c, nil
}

func (r *FileReaderAt) store(i int, c *segChunk) {
	r.cache[i] = c
	r.order = append(r.order, i)
	if len(r.order) > raCacheMax {
		evict := r.order[0]
		r.order = r.order[1:]
		delete(r.cache, evict)
	}
}

func (r *FileReaderAt) prefetch(from int) {
	n := len(r.file.Segments)
	for i := from; i < from+prefetchAhead && i < n; i++ {
		r.mu.Lock()
		_, cached := r.cache[i]
		_, busy := r.inflight[i]
		r.mu.Unlock()
		if cached || busy {
			continue
		}
		select {
		case r.sem <- struct{}{}:
			go func(i int) {
				defer func() { <-r.sem }()
				r.fetchSeg(i)
			}(i)
		default:
			return
		}
	}
}

func (r *FileReaderAt) typeSize() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.typ
}

func (r *FileReaderAt) segForOffset(off int64) (*segChunk, int, error) {
	n := len(r.file.Segments)
	t := r.typeSize()
	if t == 0 {
		if _, err := r.seg(0); err != nil {
			return nil, 0, err
		}
		t = r.typeSize()
	}
	guess := 0
	if t > 0 {
		guess = int(off / t)
	}
	if guess >= n {
		guess = n - 1
	}
	for iter := 0; iter < n+4; iter++ {
		c, err := r.seg(guess)
		if err != nil {
			return nil, 0, err
		}
		switch {
		case off < c.begin:
			guess--
			if guess < 0 {
				return nil, 0, io.EOF
			}
		case off >= c.end:
			guess++
			if guess >= n {
				return nil, 0, io.EOF
			}
		default:
			return c, guess, nil
		}
	}
	return nil, 0, errors.New("usenet: segment for offset not found")
}

func (r *FileReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.readMu.Lock()
	defer r.readMu.Unlock()
	total := 0
	last := 0
	for total < len(p) {
		c, idx, err := r.segForOffset(off + int64(total))
		if err != nil {
			if total > 0 && errors.Is(err, io.EOF) {
				return total, io.EOF
			}
			return total, err
		}
		last = idx
		start := (off + int64(total)) - c.begin
		total += copy(p[total:], c.data[start:])
	}
	r.prefetch(last + 1)
	return total, nil
}

func (r *FileReaderAt) Size() (int64, error) {
	r.readMu.Lock()
	defer r.readMu.Unlock()
	if r.size >= 0 {
		return r.size, nil
	}
	c, err := r.seg(len(r.file.Segments) - 1)
	if err != nil {
		return 0, err
	}
	r.size = c.end
	return r.size, nil
}
