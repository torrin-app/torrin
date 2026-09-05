package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/qbit"
)

// Simulate a canonical row appearing after a handler's preflight lookup.
type canonicalQueueRepo struct {
	*fakeRepo
	winner *jobs.Job
}

func (r *canonicalQueueRepo) CreateOnce(_ context.Context, job *jobs.Job) (bool, error) {
	*job = *r.winner
	return false, nil
}

type stagedQueueStore struct {
	fakeStore
	put, deleted []string
}

func (s *stagedQueueStore) Put(_ context.Context, key string, _ io.Reader, _ string) error {
	s.put = append(s.put, key)
	return nil
}

func (s *stagedQueueStore) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return nil
}

func TestConcurrentUploadLoserCleansOnlyItsOwnInput(t *testing.T) {
	info, err := bencode.Marshal(metainfo.Info{Name: "movie.mkv", PieceLength: 16384, Length: 1024, Pieces: make([]byte, 20)})
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := (&metainfo.MetaInfo{InfoBytes: info}).Write(&data); err != nil {
		t.Fatal(err)
	}
	winner := &jobs.Job{ID: "canonical", UserID: "u1", Source: jobs.SourceTorrent,
		Status: jobs.StatusQueued, InputKey: "torrent-input/winner.torrent"}
	repo := &canonicalQueueRepo{fakeRepo: &fakeRepo{}, winner: winner}
	store, pub := &stagedQueueStore{}, &fakePub{}
	s := New(Deps{Jobs: repo, Store: store, Bus: pub, Slots: middleware.NewSlotTracker(repo), Qbit: qbit.NewClient("http://unused", "", "")})
	plan := plans.Free
	plan.MonthlyIngestBytes = 0
	for range 2 {
		w := httptest.NewRecorder()
		s.ingestTorrent(w, httptest.NewRequest(http.MethodPost, "/api/jobs/torrent", nil), &auth.User{ID: "u1"}, plan, data.Bytes())
		var got jobs.Job
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != http.StatusOK || got.ID != winner.ID {
			t.Fatalf("losing upload should return canonical row: status=%d body=%s err=%v", w.Code, w.Body.String(), err)
		}
	}
	if len(pub.published) != 0 {
		t.Fatalf("duplicate upload dispatched work: %v", pub.published)
	}
	if len(store.put) != 2 || len(store.deleted) != 2 || store.put[0] == store.put[1] {
		t.Fatalf("attempts must own distinct staged keys: put=%v deleted=%v", store.put, store.deleted)
	}
	for i, key := range store.deleted {
		if key == winner.InputKey || key != store.put[i] {
			t.Fatalf("deleted another attempt's payload: %s", key)
		}
	}
}

func TestAdmissionHTTPStatus(t *testing.T) {
	for _, d := range []jobs.Admission{jobs.AdmissionAdmitted, jobs.AdmissionQueued} {
		if got := admissionStatus(d); got != http.StatusAccepted {
			t.Fatalf("%s status=%d", d, got)
		}
	}
	if got := admissionStatus(jobs.AdmissionExisting); got != http.StatusOK {
		t.Fatalf("canonical replay status=%d", got)
	}
}

func TestActiveCrossAccountDownloadQueuesWithoutDispatch(t *testing.T) {
	repo := &fakeRepo{existing: &jobs.Job{ID: "physical", UserID: "other", InfoHash: testHash,
		Source: jobs.SourceTorrent, Status: jobs.StatusDownloading, Node: "box2"}}
	pub := &fakePub{}
	s := newTestServer(repo, &fakeStore{}, pub, 1_000_000_000_000)
	r := httptest.NewRequest(http.MethodPost, "/api/jobs", nil)
	r = r.WithContext(context.WithValue(r.Context(), middleware.UserContextKey, &auth.User{ID: "u1", PlanID: "pro"}))
	w := httptest.NewRecorder()
	s.submitMagnet(w, r, testHash, "magnet:x", "", jobs.SourceTorrent, true)
	if w.Code != http.StatusAccepted || len(repo.created) != 1 || repo.created[0].Status != jobs.StatusQueued {
		t.Fatalf("active sibling should queue: status=%d jobs=%+v", w.Code, repo.created)
	}
	if len(pub.published) != 0 || repo.created[0].Priority != plans.Pro.Priority || repo.created[0].MaxBytes != plans.Pro.MaxTorrentBytes {
		t.Fatalf("queued sibling dispatched or lost plan metadata: published=%v job=%+v", pub.published, repo.created[0])
	}
}
