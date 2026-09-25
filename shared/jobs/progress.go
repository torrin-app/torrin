package jobs

import (
	"context"
	"sync/atomic"
	"time"
)

type ProgressSetter interface {
	SetProgress(ctx context.Context, id string, pct float64, speed int64) error
}

const progressRefresh = 2 * time.Second

func ProgressReporter(ctx context.Context, repo ProgressSetter, jobID string) func(current, total int64) {
	var lastPct, lastBytes, lastT atomic.Int64
	lastPct.Store(-1)
	lastT.Store(time.Now().UnixNano())
	return func(current, total int64) {
		if total <= 0 {
			return
		}
		pct := current * 1000 / total
		now := time.Now().UnixNano()
		if pct == lastPct.Load() && now-lastT.Load() < int64(progressRefresh) {
			return
		}
		lastPct.Store(pct)
		prevB := lastBytes.Swap(current)
		prevT := lastT.Swap(now)
		var speed int64
		if dt := float64(now-prevT) / 1e9; dt > 0 {
			speed = int64(float64(current-prevB) / dt)
		}
		repo.SetProgress(ctx, jobID, float64(pct)/10, speed)
	}
}
