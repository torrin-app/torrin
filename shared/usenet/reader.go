package usenet

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/torrin-app/torrin/shared/usenet/decoder"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

// ArticleFetcher is the narrow network boundary used by Reader. Implementations
// return the complete raw article body for a message ID and group.
type ArticleFetcher interface {
	Fetch(ctx context.Context, msgID, group string) ([]byte, error)
}

type segment struct {
	nzb.Segment
	start int64
	end   int64
}

const (
	readAhead    = 16
	maxParallel  = 8
	maxCachedSeg = 32
)

// Reader exposes one NZB file as a lazy, seekable byte source. Segments are
// fetched concurrently with a read-ahead window so cold usenet playback sustains
// throughput across the connection pool instead of one article at a time. The
// deep window hides the per-article round trip on high-latency relay paths.
type Reader struct {
	ctx     context.Context
	fetcher ArticleFetcher
	group   string
	segs    []segment
	size    int64
	sem     chan struct{}

	mu       sync.Mutex
	cache    map[int][]byte
	order    []int
	inflight map[int]*inflight
}

type inflight struct {
	done chan struct{}
	data []byte
	err  error
}

func NewReader(ctx context.Context, file nzb.File, fetcher ArticleFetcher) (*Reader, error) {
	if fetcher == nil {
		return nil, fmt.Errorf("usenet reader: nil article fetcher")
	}
	if len(file.Segments) == 0 {
		return nil, fmt.Errorf("usenet reader: file has no segments")
	}
	parts := append([]nzb.Segment(nil), file.Segments...)
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].Number < parts[j].Number })
	r := &Reader{
		ctx:      ctx,
		fetcher:  fetcher,
		sem:      make(chan struct{}, maxParallel),
		cache:    map[int][]byte{},
		inflight: map[int]*inflight{},
	}
	if len(file.Groups) > 0 {
		r.group = file.Groups[0]
	}
	for _, part := range parts {
		if part.Bytes <= 0 || part.MessageID == "" {
			return nil, fmt.Errorf("usenet reader: invalid segment %d", part.Number)
		}
		start := r.size
		r.size += part.Bytes
		if r.size < start {
			return nil, fmt.Errorf("usenet reader: file size overflow")
		}
		r.segs = append(r.segs, segment{Segment: part, start: start, end: r.size})
	}
	return r, nil
}

func (r *Reader) Size() int64 { return r.size }

func (r *Reader) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 {
		return 0, fmt.Errorf("usenet reader: negative offset")
	}
	if off >= r.size {
		return 0, io.EOF
	}

	first := sort.Search(len(r.segs), func(i int) bool { return r.segs[i].end > off })
	end := off + int64(len(p))
	last := first
	for last+1 < len(r.segs) && r.segs[last].end < end {
		last++
	}
	r.prefetch(first, last+readAhead)

	written := 0
	idx := first
	for written < len(p) && idx < len(r.segs) {
		data, err := r.get(idx)
		if err != nil {
			return written, err
		}
		seg := r.segs[idx]
		at := off - seg.start
		n := copy(p[written:], data[at:])
		written += n
		off += int64(n)
		idx++
	}
	if written < len(p) {
		return written, io.EOF
	}
	return written, nil
}

// prefetch warms [from, to] in the background without blocking the caller, so a
// seek pulls its landing segment plus the window ahead of the playhead.
func (r *Reader) prefetch(from, to int) {
	if from < 0 {
		from = 0
	}
	if to >= len(r.segs) {
		to = len(r.segs) - 1
	}
	for i := from; i <= to; i++ {
		r.mu.Lock()
		_, cached := r.cache[i]
		_, pending := r.inflight[i]
		r.mu.Unlock()
		if cached || pending {
			continue
		}
		go r.get(i)
	}
}

// get returns the decoded segment, fetching on miss. Concurrent requests for the
// same segment share one fetch; completed segments are cached.
func (r *Reader) get(idx int) ([]byte, error) {
	r.mu.Lock()
	if d, ok := r.cache[idx]; ok {
		r.mu.Unlock()
		return d, nil
	}
	if f, ok := r.inflight[idx]; ok {
		r.mu.Unlock()
		<-f.done
		return f.data, f.err
	}
	f := &inflight{done: make(chan struct{})}
	r.inflight[idx] = f
	r.mu.Unlock()

	r.sem <- struct{}{}
	data, err := r.fetch(idx)
	<-r.sem

	r.mu.Lock()
	delete(r.inflight, idx)
	if err == nil {
		r.store(idx, data)
	}
	r.mu.Unlock()

	f.data, f.err = data, err
	close(f.done)
	return data, err
}

// store caches a segment under mu, evicting oldest entries past the window.
func (r *Reader) store(idx int, data []byte) {
	if _, ok := r.cache[idx]; ok {
		return
	}
	r.cache[idx] = data
	r.order = append(r.order, idx)
	for len(r.order) > maxCachedSeg {
		old := r.order[0]
		r.order = r.order[1:]
		delete(r.cache, old)
	}
}

func (r *Reader) fetch(idx int) ([]byte, error) {
	seg := r.segs[idx]
	raw, err := r.fetcher.Fetch(r.ctx, seg.MessageID, r.group)
	if err != nil {
		return nil, fmt.Errorf("fetch article %s: %w", seg.MessageID, err)
	}
	decoded, err := decoder.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("decode article %s: %w", seg.MessageID, err)
	}
	if int64(len(decoded.Data)) != seg.Bytes {
		return nil, fmt.Errorf("decode article %s: got %d bytes, want %d", seg.MessageID, len(decoded.Data), seg.Bytes)
	}
	if decoded.Begin > 0 && decoded.Begin-1 != seg.start {
		return nil, fmt.Errorf("decode article %s: begins at %d, want %d", seg.MessageID, decoded.Begin-1, seg.start)
	}
	return decoded.Data, nil
}
