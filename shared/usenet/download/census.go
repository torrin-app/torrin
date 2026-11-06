package download

import (
	"context"
	"sync/atomic"

	"github.com/Tensai75/nntpPool"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

type CensusVerdict int

const (
	CensusClean CensusVerdict = iota
	CensusDegraded
	CensusUnstreamable
)

const censusSample = 24

func (v CensusVerdict) Streamable() bool { return v != CensusUnstreamable }

func CensusRelease(ctx context.Context, pools []nntpPool.ConnectionPool, n *nzb.NZB) CensusVerdict {
	f, ok := Streamable(n)
	if !ok {
		return CensusClean
	}
	group := groupOf(f)
	return census(ctx, func(msgID string) bool {
		return statSegment(ctx, pools, msgID, group)
	}, f.Segments, censusSample)
}

func census(ctx context.Context, stat func(string) bool, segs []nzb.Segment, sample int) CensusVerdict {
	total := len(segs)
	if total == 0 {
		return CensusUnstreamable
	}
	if sample <= 0 || sample > total {
		sample = total
	}
	step := total / sample
	if step < 1 {
		step = 1
	}
	var idxs []int
	for i := 0; i < total; i += step {
		idxs = append(idxs, i)
	}
	var missing int64
	parallelDo(len(idxs), openConcurrency, func(k int) {
		if ctx.Err() != nil || !stat(segs[idxs[k]].MessageID) {
			atomic.AddInt64(&missing, 1)
		}
	})
	if missing == 0 {
		return CensusClean
	}
	if int(missing)*total/len(idxs) > maxPadTotal {
		return CensusUnstreamable
	}
	return CensusDegraded
}

func statSegment(ctx context.Context, pools []nntpPool.ConnectionPool, msgID, group string) bool {
	for _, pool := range pools {
		if ctx.Err() != nil {
			return false
		}
		if statFromPool(ctx, pool, msgID, group) {
			return true
		}
	}
	return false
}

func statFromPool(ctx context.Context, pool nntpPool.ConnectionPool, msgID, group string) bool {
	conn, err := getPoolConn(ctx, pool)
	if err != nil {
		return false
	}
	if group != "" {
		if _, _, _, err := conn.Group(group); err != nil {
			conn.Close()
			pool.Put(conn)
			return false
		}
	}
	_, _, err = conn.Stat("<" + msgID + ">")
	pool.Put(conn)
	return err == nil
}
