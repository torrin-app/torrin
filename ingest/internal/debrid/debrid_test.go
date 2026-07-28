package debrid

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/providers"
)

func TestDownloadWithRenew(t *testing.T) {
	ctx := context.Background()

	var got []string
	link := providers.Link{URL: "u1", Renew: func(context.Context) (string, error) { return "u2", nil }}
	err := downloadWithRenew(ctx, link, func(u string) error {
		got = append(got, u)
		if u == "u1" {
			return errors.New("broken")
		}
		return nil
	}, 3)
	if err != nil {
		t.Fatalf("want success after renew, got %v", err)
	}
	if len(got) != 2 || got[1] != "u2" {
		t.Fatalf("expected retry on fresh url u2, got %v", got)
	}

	noRenew := providers.Link{URL: "u1"}
	calls := 0
	if err := downloadWithRenew(ctx, noRenew, func(string) error { calls++; return errors.New("x") }, 3); err == nil {
		t.Fatal("want error with no Renew")
	}
	if calls != 1 {
		t.Fatalf("no Renew should try once, got %d", calls)
	}

	n, tries := 0, 0
	allFail := providers.Link{URL: "u0", Renew: func(context.Context) (string, error) { n++; return fmt.Sprintf("u%d", n), nil }}
	if err := downloadWithRenew(ctx, allFail, func(string) error { tries++; return errors.New("x") }, 2); err == nil {
		t.Fatal("want error after exhausting renews")
	}
	if tries != 3 {
		t.Fatalf("expected 1 initial + 2 renew tries = 3, got %d", tries)
	}
}

type fakeProv struct {
	res      *providers.Result
	released bool
}

func (f *fakeProv) Name() string { return "fake" }
func (f *fakeProv) Fetch(context.Context, string, string) (*providers.Result, error) {
	return f.res, nil
}
func (f *fakeProv) Release(context.Context, string) error { f.released = true; return nil }

type capturePub struct{ files []publish.File }

func (c *capturePub) Publish(_ context.Context, _ *jobs.Job, files []publish.File) error {
	c.files = files
	return nil
}

type fakeProgress struct {
	last  float64
	named string
}

func (f *fakeProgress) SetProgress(_ context.Context, _ string, pct float64, _ int64) error {
	f.last = pct
	return nil
}
func (f *fakeProgress) Update(_ context.Context, j *jobs.Job) error {
	f.named = j.Name
	return nil
}

func TestRunDownloadsAndPublishes(t *testing.T) {
	content := make([]byte, 2_000_000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	prov := &fakeProv{res: &providers.Result{
		Name:   "Movie",
		Handle: "h1",
		Files:  []providers.Link{{Name: "movie.mkv", Size: int64(len(content)), URL: srv.URL}},
	}}
	pub := &capturePub{}
	provsFor := func(context.Context, *jobs.Job) []providers.Provider { return []providers.Provider{prov} }
	prog := &fakeProgress{}
	r := NewRunner(provsFor, pub, prog, t.TempDir(), nil, 1, nil)

	handled, err := r.Run(context.Background(), &jobs.Job{ID: "j1", InfoHash: "hash1"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("should be handled")
	}
	if len(pub.files) != 1 || pub.files[0].Name != "movie.mkv" {
		t.Fatalf("published files = %+v", pub.files)
	}
	if !prov.released {
		t.Error("provider was not released")
	}
	if prog.last == 0 {
		t.Error("download progress was never reported")
	}
	if prog.named != "Movie" {
		t.Errorf("title not persisted before download, got %q", prog.named)
	}
}

func TestRunNotCached(t *testing.T) {
	provsFor := func(context.Context, *jobs.Job) []providers.Provider {
		return []providers.Provider{&fakeProv{res: nil}}
	}
	r := NewRunner(provsFor, &capturePub{}, &fakeProgress{}, t.TempDir(), nil, 1, nil)
	handled, err := r.Run(context.Background(), &jobs.Job{ID: "j1", InfoHash: "h"})
	if err != nil || handled {
		t.Fatalf("want (false,nil), got (%v,%v)", handled, err)
	}
}

func TestRunOverPlanLimit(t *testing.T) {
	prov := &fakeProv{res: &providers.Result{
		Files: []providers.Link{{Name: "big.mkv", Size: 5_000_000_000}},
	}}
	provsFor := func(context.Context, *jobs.Job) []providers.Provider { return []providers.Provider{prov} }
	r := NewRunner(provsFor, &capturePub{}, &fakeProgress{}, t.TempDir(), nil, 1, nil)
	handled, err := r.Run(context.Background(), &jobs.Job{ID: "j1", InfoHash: "h", MaxBytes: 1_000_000_000})
	if !handled || err == nil {
		t.Fatalf("want handled with error, got (%v,%v)", handled, err)
	}
}

func TestRunFailsOverToNextProvider(t *testing.T) {
	content := make([]byte, 2_000_000)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(content)
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()

	provA := &fakeProv{res: &providers.Result{Name: "Movie", Handle: "hA",
		Files: []providers.Link{{Name: "movie.mkv", Size: int64(len(content)), URL: bad.URL}}}}
	provB := &fakeProv{res: &providers.Result{Name: "Movie", Handle: "hB",
		Files: []providers.Link{{Name: "movie.mkv", Size: int64(len(content)), URL: good.URL}}}}
	pub := &capturePub{}
	provsFor := func(context.Context, *jobs.Job) []providers.Provider {
		return []providers.Provider{provA, provB}
	}
	r := NewRunner(provsFor, pub, &fakeProgress{}, t.TempDir(), nil, 1, nil)

	handled, err := r.Run(context.Background(), &jobs.Job{ID: "j1", InfoHash: "h"})
	if err != nil {
		t.Fatalf("failover should succeed, got %v", err)
	}
	if !handled {
		t.Fatal("should be handled")
	}
	if len(pub.files) != 1 || pub.files[0].Name != "movie.mkv" {
		t.Fatalf("published files = %+v", pub.files)
	}
	if !provA.released {
		t.Error("provider A not released after failover")
	}
	if !provB.released {
		t.Error("provider B not released")
	}
}
