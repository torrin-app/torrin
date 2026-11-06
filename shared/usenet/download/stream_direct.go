package download

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/Tensai75/nntpPool"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
	"github.com/torrin-app/torrin/shared/usenet/rarfs"
	"github.com/torrin-app/torrin/shared/video"
)

const openConcurrency = 16

type Kind int

const (
	KindNone Kind = iota
	KindDirect
	KindArchive
)

var ErrNotDirectStreamable = errors.New("usenet: not streamable")

func Classify(n *nzb.NZB) (Kind, nzb.File) {
	if f, ok := Streamable(n); ok {
		return KindDirect, f
	}
	for _, f := range n.Files {
		name := strings.ToLower(FileName(f))
		if isRarName(name) || strings.Contains(name, ".par2") {
			return KindArchive, nzb.File{}
		}
	}
	return KindNone, nzb.File{}
}

func OpenStream(ctx context.Context, pools []nntpPool.ConnectionPool, n *nzb.NZB) (io.ReaderAt, int64, error) {
	kind, direct := Classify(n)
	switch kind {
	case KindDirect:
		ra := NewFileReaderAt(ctx, pools, direct, groupOf(direct))
		size, err := ra.Size()
		if err != nil {
			return nil, 0, err
		}
		return ra, size, nil
	case KindArchive:
		vols, err := orderVolumes(ctx, pools, n)
		if err != nil {
			return nil, 0, err
		}
		prewarmVolumes(vols)
		return openArchiveVols(vols)
	}
	return nil, 0, ErrNotDirectStreamable
}

// prewarmVolumes fetches each volume's leading bytes concurrently so the
// sequential rarfs header walk hits warm per-volume caches instead of a
// round-trip per volume (an N-volume archive otherwise opens in N serial fetches).
func prewarmVolumes(vols []rarfs.Volume) {
	parallelDo(len(vols), openConcurrency, func(i int) {
		vols[i].Size() // rarfs walk calls Size() per volume (fetches the last segment)
		buf := make([]byte, 64*1024)
		vols[i].ReadAt(buf, 0) // and reads headers from the start
	})
}

func parallelDo(n, bound int, fn func(i int)) {
	if bound < 1 {
		bound = 1
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, bound)
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}

func openArchiveVols(vols []rarfs.Volume) (io.ReaderAt, int64, error) {
	arc, err := rarfs.Open(vols)
	if err != nil {
		return nil, 0, err
	}
	inner := pickInner(arc)
	if inner == nil {
		return nil, 0, errors.New("usenet: no streamable video in archive")
	}
	return inner.ReaderAt(vols), inner.Size, nil
}

func pickInner(a *rarfs.Archive) *rarfs.Entry {
	var best *rarfs.Entry
	for i := range a.Entries {
		e := &a.Entries[i]
		if !e.Streamable || !video.IsVideo(e.Name) {
			continue
		}
		if best == nil || e.Size > best.Size {
			best = e
		}
	}
	return best
}

func isRarName(name string) bool {
	if strings.HasSuffix(name, ".rar") {
		return true
	}
	if n := len(name); n >= 4 && name[n-4] == '.' && name[n-3] == 'r' && isDigit(name[n-2]) && isDigit(name[n-1]) {
		return true
	}
	return false
}

func groupOf(f nzb.File) string {
	if len(f.Groups) > 0 {
		return f.Groups[0]
	}
	return ""
}
