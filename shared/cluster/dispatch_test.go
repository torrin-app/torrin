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
