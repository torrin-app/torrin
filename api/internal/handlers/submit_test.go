package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
)

// --- fakes for the handler deps (Store/Bus are interfaces; Jobs is jobs.Repository) ---

type fakeStore struct {
	has   bool
	bytes []byte
}

func (f *fakeStore) Has(context.Context, string) (bool, error) { return f.has, nil }
func (f *fakeStore) GetBytes(context.Context, string) ([]byte, error) {
	if f.bytes == nil {
		return nil, errors.New("not found")
	}
	return f.bytes, nil
}
func (f *fakeStore) Put(context.Context, string, io.Reader, string) error { return nil }
func (f *fakeStore) SignURL(path string, _ time.Duration) string          { return "signed://" + path }
func (f *fakeStore) SignURLNode(node, path string, _ time.Duration) string {
	return "signed://" + node + "/" + path
}
func (f *fakeStore) SignURLNodeUser(node, path, user string, _ time.Duration) string {
	return "signed://" + node + "/" + path + "?u=" + user
}
func (f *fakeStore) DeletePrefix(_ context.Context, _ string) error { return nil }

type fakePub struct{ published []string }

func (f *fakePub) Publish(subject string, _ any) error {
	f.published = append(f.published, subject)
	return nil
}

type fakeRepo struct {
	existing *jobs.Job
	created  []*jobs.Job
	budget   int64
	toGet    *jobs.Job
}

func (f *fakeRepo) Create(_ context.Context, j *jobs.Job) error {
	if j.ID == "" {
		j.ID = "new-id"
	}
	f.created = append(f.created, j)
	return nil
}
func (f *fakeRepo) GetByInfoHash(context.Context, string) (*jobs.Job, error) {
	if f.existing == nil {
		return nil, jobs.ErrNotFound
	}
	return f.existing, nil
}
func (f *fakeRepo) ActiveCount(context.Context, string) (int, error)      { return 0, nil }
func (f *fakeRepo) DownloadingCount(context.Context, string) (int, error) { return 0, nil }
func (f *fakeRepo) BudgetUsed(context.Context) (int64, error)             { return f.budget, nil }

// unused-by-submit stubs (satisfy jobs.Repository)
func (f *fakeRepo) Update(context.Context, *jobs.Job) error                      { return nil }
func (f *fakeRepo) Get(context.Context, string) (*jobs.Job, error)               { return f.toGet, nil }
func (f *fakeRepo) ListByInfoHash(context.Context, string) ([]*jobs.Job, error)  { return nil, nil }
func (f *fakeRepo) ListByUser(context.Context, string, int) ([]*jobs.Job, error) { return nil, nil }
func (f *fakeRepo) ListByUserBefore(context.Context, string, time.Time, string, int) ([]*jobs.Job, error) {
	return nil, nil
}
func (f *fakeRepo) ListByStatus(context.Context, jobs.Status) ([]*jobs.Job, error) { return nil, nil }
func (f *fakeRepo) Delete(context.Context, string) error                           { return nil }
func (f *fakeRepo) RecordView(context.Context, string, string) (bool, error)       { return false, nil }
func (f *fakeRepo) SetProgress(context.Context, string, float64, int64) error      { return nil }

func newTestServer(repo *fakeRepo, store *fakeStore, pub *fakePub, budget int64) *Server {
	return New(Deps{
		Jobs: repo, Store: store, Bus: pub, Slots: middleware.NewSlotTracker(repo), Budget: budget,
	})
}

const testHash = "0123456789abcdef0123456789abcdef01234567"

func TestSubmitMagnet_NewJob(t *testing.T) {
	repo, store, pub := &fakeRepo{}, &fakeStore{has: false}, &fakePub{}
	s := newTestServer(repo, store, pub, 1_000_000_000_000)
	r := httptest.NewRequest("POST", "/api/jobs", nil)
	r = r.WithContext(context.WithValue(r.Context(), middleware.UserContextKey, &auth.User{ID: "u1", PlanID: "pro", ExpiresAt: time.Now().Add(time.Hour)}))
	w := httptest.NewRecorder()

	s.submitMagnet(w, r, testHash, "magnet:x", "", jobs.SourceTorrent, true)

	if w.Code != 202 {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created %d jobs, want 1", len(repo.created))
	}
	j := repo.created[0]
	if j.Source != jobs.SourceTorrent || j.Magnet != "magnet:x" || j.Status != jobs.StatusPending {
		t.Errorf("job = %+v", j)
	}
	if len(pub.published) != 1 {
		t.Errorf("expected 1 assign publish, got %d", len(pub.published))
	}
}

func TestSubmitMagnet_BudgetGateQueues(t *testing.T) {
	repo, store, pub := &fakeRepo{budget: 0}, &fakeStore{has: false}, &fakePub{}
	s := newTestServer(repo, store, pub, 0) // Budget 0 → near cap → queue
	r := httptest.NewRequest("POST", "/api/jobs", nil)
	r = r.WithContext(context.WithValue(r.Context(), middleware.UserContextKey, &auth.User{ID: "u1", PlanID: "pro", ExpiresAt: time.Now().Add(time.Hour)}))
	w := httptest.NewRecorder()

	s.submitMagnet(w, r, testHash, "magnet:x", "", jobs.SourceTorrent, true)

	if repo.created[0].Status != jobs.StatusQueued {
		t.Errorf("status = %s, want queued", repo.created[0].Status)
	}
	if len(pub.published) != 0 {
		t.Error("queued job should NOT be assigned")
	}
}

func TestSubmitMagnet_HosterNoBudgetGate(t *testing.T) {
	repo, store, pub := &fakeRepo{budget: 0}, &fakeStore{has: false}, &fakePub{}
	s := newTestServer(repo, store, pub, 0)
	r := httptest.NewRequest("POST", "/api/jobs/hoster", nil)
	r = r.WithContext(context.WithValue(r.Context(), middleware.UserContextKey, &auth.User{ID: "u1", PlanID: "pro", ExpiresAt: time.Now().Add(time.Hour)}))
	w := httptest.NewRecorder()

	// budgetGate=false → even with Budget 0, it must NOT queue.
	s.submitMagnet(w, r, testHash, "https://host/f", "", jobs.SourceHoster, false)

	j := repo.created[0]
	if j.Source != jobs.SourceHoster || j.Status != jobs.StatusPending {
		t.Errorf("hoster job = %+v, want pending hoster", j)
	}
	if len(pub.published) != 1 {
		t.Error("pending job should be assigned")
	}
}

func TestSubmitMagnet_Cached(t *testing.T) {
	m := manifest.Manifest{InfoHash: testHash, Name: "Movie", Files: []manifest.File{{FileName: "movie.mkv", FileSize: 100}}}
	data, _ := m.Marshal()
	repo, store, pub := &fakeRepo{}, &fakeStore{has: true, bytes: data}, &fakePub{}
	s := newTestServer(repo, store, pub, 1_000_000_000_000)
	r := httptest.NewRequest("POST", "/api/jobs", nil)
	r = r.WithContext(context.WithValue(r.Context(), middleware.UserContextKey, &auth.User{ID: "u1", PlanID: "pro", ExpiresAt: time.Now().Add(time.Hour)}))
	w := httptest.NewRecorder()

	s.submitMagnet(w, r, testHash, "magnet:x", "", jobs.SourceTorrent, true)

	if w.Code != 200 {
		t.Fatalf("cached status = %d, want 200", w.Code)
	}
	// A cache hit records a *complete* job the user owns — but never starts a download.
	if len(repo.created) != 1 || repo.created[0].Status != jobs.StatusComplete {
		t.Errorf("cached hit should create one complete job, got %+v", repo.created)
	}
	if len(pub.published) != 0 {
		t.Error("cached hit should not assign (no download to run)")
	}
}

func TestProgressJob_Ytdlp(t *testing.T) {
	job := &jobs.Job{ID: "j1", UserID: "u1", Source: jobs.SourceYtdlp, Status: jobs.StatusDownloading, Progress: 23, Speed: 1_000_000}
	s := newTestServer(&fakeRepo{toGet: job}, &fakeStore{}, &fakePub{}, 0)
	r := httptest.NewRequest("GET", "/api/jobs/j1/progress", nil)
	r.SetPathValue("id", "j1")
	r = r.WithContext(context.WithValue(r.Context(), middleware.UserContextKey, &auth.User{ID: "u1"}))
	w := httptest.NewRecorder()

	s.progressJob(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["progress"] != float64(23) {
		t.Errorf("progress = %v, want 23 (ytdlp must report DB progress, not fall through to empty)", resp["progress"])
	}
	if resp["speed"] != float64(1_000_000) {
		t.Errorf("speed = %v, want 1000000", resp["speed"])
	}
}

func TestSubmitMagnet_DedupSameUser(t *testing.T) {
	existing := &jobs.Job{ID: "exist1", UserID: "u1", InfoHash: testHash, Status: jobs.StatusComplete, Name: "X"}
	repo, store, pub := &fakeRepo{existing: existing}, &fakeStore{has: false}, &fakePub{}
	s := newTestServer(repo, store, pub, 1_000_000_000_000)
	r := httptest.NewRequest("POST", "/api/jobs", nil)
	r = r.WithContext(context.WithValue(r.Context(), middleware.UserContextKey, &auth.User{ID: "u1", PlanID: "pro", ExpiresAt: time.Now().Add(time.Hour)}))
	w := httptest.NewRecorder()

	s.submitMagnet(w, r, testHash, "magnet:x", "", jobs.SourceTorrent, true)

	if w.Code != 200 {
		t.Fatalf("dedup status = %d, want 200", w.Code)
	}
	if len(repo.created) != 0 {
		t.Error("same-user dedup must not create a new job")
	}
}
