package cluster

import (
	"context"
	"strconv"
	"strings"

	"github.com/torrin-app/torrin/shared/env"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
)

type Sizer interface {
	GetTotalCachedSize(ctx context.Context, node string) (int64, error)
}

type Publisher interface {
	Publish(subject string, v any) error
}

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
	case string(jobs.SourceTorrent), string(jobs.SourceUsenet), string(jobs.SourceHoster), string(jobs.SourceYtdlp):
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

func TargetNode(ctx context.Context, sizer Sizer, source string, sizeHint int64) string {
	if !portable(source) {
		return ""
	}
	nodes := overflowNodes()
	if len(nodes) == 0 {
		return ""
	}
	cap1 := env.Int("EVICTION_CAP_BYTES", 0)
	if cap1 <= 0 {
		return ""
	}
	if used, err := sizer.GetTotalCachedSize(ctx, ""); err != nil || used+sizeHint < cap1 {
		return ""
	}
	for _, n := range nodes {
		used, err := sizer.GetTotalCachedSize(ctx, n.id)
		if err != nil {
			continue
		}
		if n.cap <= 0 || used+sizeHint < n.cap {
			return n.id
		}
	}
	return ""
}
