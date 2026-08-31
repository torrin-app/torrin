package cluster

import (
	"context"
	"strconv"
	"strings"

	"github.com/torrin-app/torrin/shared/diskfree"
	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
)

type Sizer interface {
	GetTotalCachedSize(ctx context.Context, node string) (int64, error)
	GetInFlightSize(ctx context.Context, node string) (int64, error)
}

type Statuser interface {
	Free(ctx context.Context, node string) (int64, bool)
	MinFree(ctx context.Context, node string) (int64, bool)
}

type Publisher interface {
	Publish(subject string, v any) error
}

var statuser Statuser

func SetStatuser(s Statuser) { statuser = s }

func Assign(ctx context.Context, pub Publisher, sizer Sizer, repo jobs.Repository, job *jobs.Job) {
	job.Node = TargetNode(ctx, sizer, string(job.Source), job.MaxBytes)
	repo.Update(ctx, job)
	pub.Publish(events.JobAssigned, events.Assigned{
		JobID: job.ID, InfoHash: job.InfoHash, Magnet: job.Magnet,
		Source: string(job.Source), MaxBytes: job.MaxBytes, Node: job.Node,
	})
}

func portable(source string) bool {
	switch source {
	case string(jobs.SourceTorrent), string(jobs.SourceUsenet), string(jobs.SourceHoster),
		string(jobs.SourceYtdlp), string(jobs.SourceScenerls), string(jobs.SourceHDEncode),
		string(jobs.SourceTelegram):
		return true
	default:
		return false
	}
}

type nodeCap struct {
	id  string
	cap int64
}

func overflowNodes() []nodeCap {
	raw := env.Get("CLUSTER_NODES", "")
	if raw == "" {
		if n := env.Get("NODE2_ID", ""); n != "" {
			return []nodeCap{{id: n}}
		}
		return nil
	}
	var out []nodeCap
	for _, part := range strings.Split(raw, ",") {
		id, capStr, _ := strings.Cut(strings.TrimSpace(part), ":")
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, nodeCap{id: id, cap: parseCap(capStr)})
		}
	}
	return out
}

func parseCap(s string) int64 {
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "TB"):
		mult, s = 1_000_000_000_000, strings.TrimSuffix(s, "TB")
	case strings.HasSuffix(s, "GB"):
		mult, s = 1_000_000_000, strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "T"):
		mult, s = 1_000_000_000_000, strings.TrimSuffix(s, "T")
	case strings.HasSuffix(s, "G"):
		mult, s = 1_000_000_000, strings.TrimSuffix(s, "G")
	}
	n, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return int64(n * float64(mult))
}

func primaryNode() string { return env.Get("PRIMARY_NODE_ID", "") }

func TargetNode(ctx context.Context, sizer Sizer, source string, sizeHint int64) string {
	if !portable(source) {
		return primaryNode()
	}
	if n, ok := targetByFree(ctx, sizer, sizeHint); ok {
		return n
	}
	return targetByCap(ctx, sizer, sizeHint)
}

func orderedNodes() []string {
	out := []string{primaryNode()}
	for _, n := range overflowNodes() {
		if n.id == primaryNode() {
			continue
		}
		out = append(out, n.id)
	}
	return out
}

func targetByFree(ctx context.Context, sizer Sizer, sizeHint int64) (string, bool) {
	if statuser == nil {
		return "", false
	}
	envFloor := env.Int("STORE_MIN_FREE_BYTES", 0)
	var best string
	var bestFree int64
	seen := false
	for _, id := range orderedNodes() {
		free, ok := statuser.Free(ctx, id)
		if !ok {
			continue
		}
		floor := envFloor
		if f, ok := statuser.MinFree(ctx, id); ok {
			floor = f
		}
		reserved, err := sizer.GetInFlightSize(ctx, id)
		if err != nil {
			continue
		}
		eff := free - reserved
		if eff-sizeHint >= floor {
			return id, true
		}
		if !seen || eff > bestFree {
			seen, bestFree, best = true, eff, id
		}
	}
	if !seen {
		return "", false
	}
	return best, true
}

func targetByCap(ctx context.Context, sizer Sizer, sizeHint int64) string {
	nodes := overflowNodes()
	if len(nodes) == 0 {
		return primaryNode()
	}
	cap1 := env.Int("EVICTION_CAP_BYTES", 0)
	if cap1 <= 0 {
		return primaryNode()
	}
	if localHasRoom(ctx, sizer, cap1, sizeHint) {
		return primaryNode()
	}
	for _, n := range nodes {
		if n.id == primaryNode() {
			continue
		}
		if used, ok := nodeUsed(ctx, sizer, n.id); ok && (n.cap <= 0 || used+sizeHint < n.cap) {
			return n.id
		}
	}
	return primaryNode()
}

func localHasRoom(ctx context.Context, sizer Sizer, cap1, sizeHint int64) bool {
	if !diskAboveFloor() {
		return false
	}
	used, ok := nodeUsed(ctx, sizer, primaryNode())
	if !ok {
		return true
	}
	return used+sizeHint < cap1
}

func nodeUsed(ctx context.Context, sizer Sizer, node string) (int64, bool) {
	cached, err := sizer.GetTotalCachedSize(ctx, node)
	if err != nil {
		return 0, false
	}
	inflight, err := sizer.GetInFlightSize(ctx, node)
	if err != nil {
		return 0, false
	}
	return cached + inflight, true
}

func diskAboveFloor() bool {
	minFree := env.Int("STORE_MIN_FREE_BYTES", 0)
	if minFree <= 0 {
		return true
	}
	free, _, ok := diskfree.Stat(env.Get("STORE_DIR", "/mnt/cache/store"))
	if !ok {
		return true
	}
	return free >= minFree
}
