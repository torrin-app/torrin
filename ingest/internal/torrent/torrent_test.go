package torrent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/qbit"
)

type fakeRepo struct {
	jobs.Repository
	byStatus map[jobs.Status][]*jobs.Job
}

func (f *fakeRepo) ListByStatus(_ context.Context, s jobs.Status) ([]*jobs.Job, error) {
	return f.byStatus[s], nil
}

type progRepo struct {
	jobs.Repository
	pct   float64
	speed int64
}

func (p *progRepo) SetProgress(_ context.Context, _ string, pct float64, speed int64) error {
	p.pct, p.speed = pct, speed
	return nil
}

func TestDrivePersistsProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "auth/login"):
			w.Write([]byte("Ok."))
		case strings.Contains(r.URL.Path, "torrents/info"):
			w.Write([]byte(`[{"hash":"aabbcc","progress":0.42,"dlspeed":1000,"state":"downloading","size":100,"name":"x"}]`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	fr := &progRepo{}
	r := &Runner{qb: qbit.NewClient(srv.URL, "u", "p"), repo: fr}
	job := &jobs.Job{ID: "j1", InfoHash: "aabbcc", Name: "x", Files: []jobs.File{{Index: 0}}}
	r.drive(context.Background(), job)

	if fr.pct != 42 {
		t.Fatalf("progress not persisted to db: got %v, want 42", fr.pct)
	}
	if fr.speed != 1000 {
		t.Fatalf("speed not persisted: got %d, want 1000", fr.speed)
	}
}

func TestActiveHashesProtectsLiveJobs(t *testing.T) {
	v2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	r := &Runner{repo: &fakeRepo{byStatus: map[jobs.Status][]*jobs.Job{
		jobs.StatusDownloading: {{InfoHash: "AABBCC"}},
		jobs.StatusPublishing: {{InfoHash: "0000000000000000000000000000000000000000",
			Magnet: "magnet:?xt=urn:btih:0000000000000000000000000000000000000000&xt=urn:btmh:1220" + v2}},
		jobs.StatusComplete: {{InfoHash: "deadbeef"}},
	}}}
	keep := r.activeHashes(context.Background())
	if !keep["aabbcc"] {
		t.Error("downloading job not protected")
	}
	if !keep[v2[:40]] {
		t.Error("v2 hash of active job not protected")
	}
	if keep["deadbeef"] {
		t.Error("completed job should not be protected (reapable)")
	}
}

func TestExtractV2Hash(t *testing.T) {
	v2 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 64 hex (sha256)
	magnet := "magnet:?xt=urn:btih:0000000000000000000000000000000000000000&xt=urn:btmh:1220" + v2
	if got := extractV2Hash(magnet); got != v2[:40] {
		t.Errorf("got %q, want %q", got, v2[:40])
	}
	if got := extractV2Hash("magnet:?xt=urn:btih:abc"); got != "" {
		t.Errorf("no btmh should yield empty, got %q", got)
	}
}
