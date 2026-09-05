package cluster

import (
	"context"
	"testing"

	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
)

type mapSizer map[string]int64

func (m mapSizer) GetTotalCachedSize(_ context.Context, node string) (int64, error) {
	return m[node], nil
}

func (m mapSizer) GetInFlightSize(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

type reserveSizer struct {
	cached   map[string]int64
	inflight map[string]int64
}

func (s reserveSizer) GetTotalCachedSize(_ context.Context, node string) (int64, error) {
	return s.cached[node], nil
}

func (s reserveSizer) GetInFlightSize(_ context.Context, node string) (int64, error) {
	return s.inflight[node], nil
}

type fakePub struct {
	subject string
	msg     any
}

func (f *fakePub) Publish(subject string, v any) error {
	f.subject, f.msg = subject, v
	return nil
}

type fakeRepo struct {
	jobs.Repository
	updated *jobs.Job
}

func (f *fakeRepo) Update(_ context.Context, j *jobs.Job) error {
	f.updated = j
	return nil
}

func TestAssign(t *testing.T) {
	t.Setenv("EVICTION_CAP_BYTES", "1000")
	t.Setenv("CLUSTER_NODES", "box2:2000")
	pub, repo := &fakePub{}, &fakeRepo{}
	job := &jobs.Job{ID: "j1", InfoHash: "h", Magnet: "m", Source: jobs.SourceTorrent, MaxBytes: 100}

	Assign(context.Background(), pub, mapSizer{"": 950}, repo, job) // box1 full -> box2

	if job.Node != "box2" {
		t.Fatalf("node not stamped on job: %q", job.Node)
	}
	if repo.updated == nil || repo.updated.Node != "box2" {
		t.Fatal("repo.Update not called with the stamped node")
	}
	if pub.subject != events.JobAssigned {
		t.Fatalf("wrong subject %q", pub.subject)
	}
	a, ok := pub.msg.(events.Assigned)
	if !ok || a.JobID != "j1" || a.Node != "box2" || a.Source != string(jobs.SourceTorrent) {
		t.Fatalf("wrong event payload: %+v", pub.msg)
	}
}

func TestAssignKeepsStagedInputOnPrimaryNode(t *testing.T) {
	t.Setenv("EVICTION_CAP_BYTES", "1000")
	t.Setenv("PRIMARY_NODE_ID", "box1")
	t.Setenv("CLUSTER_NODES", "box2:2000")
	pub, repo := &fakePub{}, &fakeRepo{}
	job := &jobs.Job{ID: "j1", InfoHash: "h", Source: jobs.SourceTorrent, MaxBytes: 100, InputKey: "torrent-input/u/h.torrent"}

	Assign(context.Background(), pub, mapSizer{"": 950}, repo, job)

	if job.Node != "box1" {
		t.Fatalf("staged input routed away from primary node: %q", job.Node)
	}
}

func TestTargetNode(t *testing.T) {
	ctx := context.Background()

	t.Setenv("NODE2_ID", "")
	t.Setenv("CLUSTER_NODES", "")
	if got := TargetNode(ctx, mapSizer{"": 999}, "torrent", 0); got != "" {
		t.Fatalf("single box should route to box1 (\"\"), got %q", got)
	}

	t.Setenv("EVICTION_CAP_BYTES", "1000")
	t.Setenv("CLUSTER_NODES", "box2:2000,box3:3000")

	if got := TargetNode(ctx, mapSizer{"": 500}, "torrent", 100); got != "" {
		t.Fatalf("box1 has room -> box1, got %q", got)
	}
	if got := TargetNode(ctx, mapSizer{"": 950}, "torrent", 100); got != "box2" {
		t.Fatalf("box1 full, box2 has room -> box2, got %q", got)
	}
	if got := TargetNode(ctx, mapSizer{"": 950, "box2": 1950}, "torrent", 100); got != "box3" {
		t.Fatalf("box1+box2 full, box3 has room -> box3, got %q", got)
	}
	if got := TargetNode(ctx, mapSizer{"": 950, "box2": 1950, "box3": 2950}, "torrent", 100); got != "" {
		t.Fatalf("all full -> box1 (best effort), got %q", got)
	}

	if got := TargetNode(ctx, mapSizer{"": 950}, "usenet", 100); got != "box2" {
		t.Fatalf("usenet is portable, full -> box2, got %q", got)
	}
	if got := TargetNode(ctx, mapSizer{"": 950}, "hoster", 100); got != "box2" {
		t.Fatalf("hoster is portable, full -> box2, got %q", got)
	}
	if got := TargetNode(ctx, mapSizer{"": 950}, "ytdlp", 100); got != "box2" {
		t.Fatalf("ytdlp is portable, full -> box2, got %q", got)
	}
	if got := TargetNode(ctx, mapSizer{"": 950}, "scenerls", 100); got != "box2" {
		t.Fatalf("scenerls is portable, full -> box2, got %q", got)
	}
	if got := TargetNode(ctx, mapSizer{"": 950}, "telegram", 100); got != "box2" {
		t.Fatalf("telegram is portable, full -> box2, got %q", got)
	}
	if got := TargetNode(ctx, mapSizer{"": 950}, "hdencode", 100); got != "box2" {
		t.Fatalf("hdencode is portable, full -> box2, got %q", got)
	}

	t.Setenv("CLUSTER_NODES", "")
	t.Setenv("NODE2_ID", "box2")
	if got := TargetNode(ctx, mapSizer{"": 950}, "torrent", 100); got != "box2" {
		t.Fatalf("legacy NODE2_ID (no cap) full -> box2, got %q", got)
	}
}

func TestTargetNodeReservesInFlight(t *testing.T) {
	ctx := context.Background()
	t.Setenv("EVICTION_CAP_BYTES", "1000")
	t.Setenv("CLUSTER_NODES", "box2:5000")

	room := reserveSizer{cached: map[string]int64{"": 500}, inflight: map[string]int64{"": 100}}
	if got := TargetNode(ctx, room, "torrent", 100); got != "" {
		t.Fatalf("cached+inflight+job under cap -> box1, got %q", got)
	}

	burst := reserveSizer{cached: map[string]int64{"": 500}, inflight: map[string]int64{"": 600}}
	if got := TargetNode(ctx, burst, "torrent", 100); got != "box2" {
		t.Fatalf("in-flight burst pushes over cap -> box2, got %q", got)
	}
}

type fakeStatuser map[string]int64

func (f fakeStatuser) Free(_ context.Context, node string) (int64, bool) {
	v, ok := f[node]
	return v, ok
}

func (f fakeStatuser) MinFree(_ context.Context, _ string) (int64, bool) { return 0, false }

type fakeStatuserMF struct{ free, minFree map[string]int64 }

func (f fakeStatuserMF) Free(_ context.Context, n string) (int64, bool) {
	v, ok := f.free[n]
	return v, ok
}
func (f fakeStatuserMF) MinFree(_ context.Context, n string) (int64, bool) {
	v, ok := f.minFree[n]
	return v, ok
}

func TestTargetNodeUsesReportedMinFree(t *testing.T) {
	ctx := context.Background()
	defer SetStatuser(nil)
	t.Setenv("CLUSTER_NODES", "box2")
	SetStatuser(fakeStatuserMF{
		free:    map[string]int64{"": 500, "box2": 5000},
		minFree: map[string]int64{"": 600, "box2": 100},
	})
	if got := TargetNode(ctx, reserveSizer{inflight: map[string]int64{}}, "torrent", 10); got != "box2" {
		t.Fatalf("box1 under its reported floor -> box2, got %q", got)
	}
}

func TestTargetNodeByFreeSpace(t *testing.T) {
	ctx := context.Background()
	defer SetStatuser(nil)
	t.Setenv("CLUSTER_NODES", "box2")
	t.Setenv("STORE_MIN_FREE_BYTES", "100")

	empty := reserveSizer{inflight: map[string]int64{}}

	SetStatuser(fakeStatuser{"": 500, "box2": 500})
	if got := TargetNode(ctx, empty, "torrent", 10); got != "" {
		t.Fatalf("box1 has room -> box1, got %q", got)
	}

	SetStatuser(fakeStatuser{"": 50, "box2": 500})
	if got := TargetNode(ctx, empty, "torrent", 10); got != "box2" {
		t.Fatalf("box1 below floor -> overflow box2, got %q", got)
	}

	SetStatuser(fakeStatuser{"": 50, "box2": 40})
	if got := TargetNode(ctx, empty, "torrent", 10); got != "" {
		t.Fatalf("all full -> least-full box1 for eviction, got %q", got)
	}

	SetStatuser(fakeStatuser{"": 200, "box2": 500})
	burst := reserveSizer{inflight: map[string]int64{"": 150}}
	if got := TargetNode(ctx, burst, "torrent", 10); got != "box2" {
		t.Fatalf("in-flight reservation overflows box1 -> box2, got %q", got)
	}

	SetStatuser(fakeStatuser{})
	t.Setenv("EVICTION_CAP_BYTES", "1000")
	if got := TargetNode(ctx, mapSizer{"": 500}, "torrent", 10); got != "" {
		t.Fatalf("no heartbeat -> legacy cap path, box1 under cap, got %q", got)
	}
}

func TestTargetNodePrimaryNamed(t *testing.T) {
	ctx := context.Background()
	defer SetStatuser(nil)
	t.Setenv("PRIMARY_NODE_ID", "box1")
	t.Setenv("CLUSTER_NODES", "box2")
	t.Setenv("STORE_MIN_FREE_BYTES", "100")

	empty := reserveSizer{inflight: map[string]int64{}}

	SetStatuser(fakeStatuser{"box1": 500, "box2": 500})
	if got := TargetNode(ctx, empty, "torrent", 10); got != "box1" {
		t.Fatalf("primary named box1 with room -> box1, got %q", got)
	}

	SetStatuser(fakeStatuser{"box1": 50, "box2": 500})
	if got := TargetNode(ctx, empty, "torrent", 10); got != "box2" {
		t.Fatalf("box1 full -> overflow box2, got %q", got)
	}

	if got := TargetNode(ctx, empty, "internal", 10); got != "box1" {
		t.Fatalf("non-portable -> primary box1 (never empty), got %q", got)
	}
}

func TestParseCap(t *testing.T) {
	cases := map[string]int64{
		"45TB":  45_000_000_000_000,
		"50T":   50_000_000_000_000,
		"200GB": 200_000_000_000,
		"500G":  500_000_000_000,
		"1000":  1000,
		"":      0,
		"1.5TB": 1_500_000_000_000,
	}
	for in, want := range cases {
		if got := parseCap(in); got != want {
			t.Errorf("parseCap(%q) = %d, want %d", in, got, want)
		}
	}
}
