package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

type fakeCairns struct {
	items  []auth.CairnItem
	nzbs   map[string][]byte
	listed string
}

type fakeCachedLookup map[string]*jobs.Job

func (f fakeCachedLookup) CachedByHashes(_ context.Context, hashes []string) (map[string]*jobs.Job, error) {
	out := make(map[string]*jobs.Job, len(hashes))
	for _, hash := range hashes {
		if job := f[hash]; job != nil {
			out[hash] = job
		}
	}
	return out, nil
}

type recordingCachedLookup struct {
	jobs   fakeCachedLookup
	calls  int
	hashes []string
}

type countingStorage struct {
	Storage
	gets map[string]int
	has  map[string]int
}

func (s *countingStorage) GetBytes(ctx context.Context, key string) ([]byte, error) {
	s.gets[key]++
	return s.Storage.GetBytes(ctx, key)
}

func (s *countingStorage) Has(ctx context.Context, key string) (bool, error) {
	s.has[key]++
	return s.Storage.Has(ctx, key)
}

func (f *recordingCachedLookup) CachedByHashes(ctx context.Context, hashes []string) (map[string]*jobs.Job, error) {
	f.calls++
	f.hashes = append(f.hashes, hashes...)
	return f.jobs.CachedByHashes(ctx, hashes)
}

func (f *fakeCairns) GetCairnArchive(_ context.Context, hash string) (string, string, bool) {
	for _, item := range f.items {
		if item.InfoHash == hash && item.Archived {
			return nzb.StorageKey(hash), item.Name, true
		}
	}
	return "", "", false
}
func (f *fakeCairns) GetCairnNZB(_ context.Context, hash string) ([]byte, bool) {
	b, ok := f.nzbs[hash]
	return b, ok
}
func (*fakeCairns) AddUserCairn(context.Context, string, string) error    { return nil }
func (*fakeCairns) DeleteUserCairn(context.Context, string, string) error { return nil }
func (f *fakeCairns) HasUserCairn(_ context.Context, _, hash string) bool {
	for _, item := range f.items {
		if item.InfoHash == hash {
			return true
		}
	}
	return false
}
func (f *fakeCairns) ListUserCairns(_ context.Context, user string) ([]auth.CairnItem, error) {
	f.listed = user
	return f.items, nil
}

func TestCanCairn(t *testing.T) {
	active := &auth.User{ExpiresAt: time.Now().Add(time.Hour)}
	cases := []struct {
		name string
		plan plans.Plan
		user *auth.User
		want bool
	}{
		{"free excluded", plans.Free, active, false},
		{"paid monthly ok", plans.Plan{ID: "standard"}, &auth.User{Recurrence: "monthly"}, true},
		{"paid lifetime ok", plans.Plan{ID: "pro"}, &auth.User{Recurrence: "lifetime"}, true},
		{"day plan excluded", plans.Plan{ID: "standard"}, &auth.User{Recurrence: "days"}, false},
	}
	for _, c := range cases {
		if got := canCairn(c.plan, c.user); got != c.want {
			t.Errorf("%s: canCairn = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCairnListReportsWarmAndDirectStreamsAsCached(t *testing.T) {
	warmHash := strings.Repeat("a", 40)
	coldHash := strings.Repeat("b", 40)
	pendingHash := strings.Repeat("c", 40)
	remoteHash := strings.Repeat("d", 40)
	legacyHash := strings.Repeat("e", 40)
	warmManifest, _ := (manifest.Manifest{InfoHash: warmHash, Name: "Warm", Files: []manifest.File{
		{FileName: "warm.mkv", DirectURL: "blobs/b_warm", FileSize: 1234},
	}}).Marshal()
	legacyManifest, _ := (manifest.Manifest{InfoHash: legacyHash, Name: "Legacy", Files: []manifest.File{
		{FileName: "legacy.mkv", DirectURL: "blobs/b_legacy", FileSize: 2345},
	}}).Marshal()

	cipher, err := crypto.NewStream(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	coldPlainSize := int64(100000)
	coldEncryptedSize, _ := cipher.EncryptedSize(coldPlainSize)
	coldNZB := nzb.Generate([]nzb.OutFile{{Name: "cold.mkv", Group: "alt.test", Segments: []nzb.Segment{
		{MessageID: "cold-1", Number: 1, Bytes: coldEncryptedSize},
	}}})

	primaryStore := &fakeStore{
		bytesByKey: map[string][]byte{manifest.Path(warmHash): warmManifest, manifest.Path(legacyHash): legacyManifest},
		hasByKey:   map[string]bool{"blobs/b_warm": true, "blobs/b_legacy": true},
	}
	primary := &countingStorage{Storage: primaryStore, gets: map[string]int{}, has: map[string]int{}}
	archiveStore := &fakeStore{bytesByKey: map[string][]byte{
		nzb.StorageKey(coldHash): coldNZB, nzb.StorageKey(remoteHash): coldNZB,
	}}
	archive := &countingStorage{Storage: archiveStore, gets: map[string]int{}, has: map[string]int{}}
	repo := &fakeCairns{items: []auth.CairnItem{
		{InfoHash: warmHash, Name: "Warm", Archived: true},
		{InfoHash: coldHash, Name: "Cold", Archived: true},
		{InfoHash: pendingHash, Name: "Pending", Archived: false},
		{InfoHash: remoteHash, Name: "Remote", Archived: true},
		{InfoHash: legacyHash, Name: "Legacy", Archived: true},
	}}
	lookup := &recordingCachedLookup{jobs: fakeCachedLookup{
		warmHash: {
			InfoHash: warmHash, Name: "Warm", Status: jobs.StatusComplete, Node: "box1",
			Files: []jobs.File{{Index: 0, Name: "warm.mkv", Size: 1234, Key: "blobs/b_warm"}},
		},
		remoteHash: {
			InfoHash: remoteHash, Name: "Remote", Status: jobs.StatusComplete, Node: "box2",
			Files: []jobs.File{{Index: 0, Name: "remote.mkv", Size: 4321, Key: "blobs/b_remote"}},
		},
		legacyHash: {
			InfoHash: legacyHash, Name: "Legacy", Status: jobs.StatusComplete,
			Files: []jobs.File{{Index: 0, Name: "legacy.mkv", Size: 2345, Key: "blobs/b_legacy"}},
		},
	}}
	s := New(Deps{
		Jobs: &fakeRepo{}, Store: primary, Cairns: repo, CairnStore: archive,
		CairnCipher: cipher, CairnDirect: true, CachedJobs: lookup,
	})
	r := httptest.NewRequest("GET", "/api/cairn", nil)
	r = r.WithContext(context.WithValue(r.Context(), middleware.UserContextKey, &auth.User{ID: "u1"}))
	w := httptest.NewRecorder()
	s.cairnList(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var response struct {
		Cairns []cairnListItem `json:"cairns"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if repo.listed != "u1" || len(response.Cairns) != 5 {
		t.Fatalf("listed=%q items=%d", repo.listed, len(response.Cairns))
	}
	if lookup.calls != 1 || len(lookup.hashes) != 5 {
		t.Fatalf("warm lookup calls=%d hashes=%v, want one five-hash batch", lookup.calls, lookup.hashes)
	}
	for _, hash := range []string{warmHash, coldHash, pendingHash, remoteHash} {
		if got := primary.gets[manifest.Path(hash)]; got != 0 {
			t.Fatalf("unnecessary manifest reads for %s=%d, want 0", hash, got)
		}
	}
	if got := primary.has["blobs/b_warm"]; got != 0 {
		t.Fatalf("unnecessary warm file checks=%d, want 0", got)
	}
	if got := primary.gets[manifest.Path(legacyHash)]; got != 1 {
		t.Fatalf("legacy warm manifest reads=%d, want 1", got)
	}
	if got := primary.has["blobs/b_legacy"]; got != 1 {
		t.Fatalf("legacy warm file checks=%d, want 1", got)
	}
	if got := archive.gets[nzb.StorageKey(coldHash)]; got != 1 {
		t.Fatalf("cold NZB reads=%d, want 1", got)
	}
	for _, hash := range []string{warmHash, pendingHash, remoteHash} {
		if got := archive.gets[nzb.StorageKey(hash)]; got != 0 {
			t.Fatalf("unnecessary NZB reads for %s=%d, want 0", hash, got)
		}
	}
	warm := response.Cairns[0]
	if !warm.Cached || warm.StreamSource != "cache" || len(warm.StreamURLs) != 1 || strings.Contains(warm.StreamURLs[0].SignedURL, "/cairn/") {
		t.Fatalf("warm item = %+v", warm)
	}
	cold := response.Cairns[1]
	if !cold.Cached || cold.StreamSource != "cairn" || len(cold.StreamURLs) != 1 {
		t.Fatalf("cold item = %+v", cold)
	}
	if cold.StreamURLs[0].Size != coldPlainSize || !strings.Contains(cold.StreamURLs[0].SignedURL, coldHash+"/cairn/0/cold.mkv") ||
		!strings.Contains(cold.StreamURLs[0].SignedURL, "?u=u1") || !strings.Contains(cold.StreamURLs[0].SignedURL, "&enc=1") {
		t.Fatalf("cold stream = %+v", cold.StreamURLs[0])
	}
	pending := response.Cairns[2]
	if pending.Cached || pending.StreamSource != "" || len(pending.StreamURLs) != 0 {
		t.Fatalf("pending item = %+v", pending)
	}
	remote := response.Cairns[3]
	if !remote.Cached || remote.StreamSource != "cache" || len(remote.StreamURLs) != 1 ||
		strings.Contains(remote.StreamURLs[0].SignedURL, "/cairn/") ||
		!strings.Contains(remote.StreamURLs[0].SignedURL, "signed://box2/blobs/b_remote") {
		t.Fatalf("remote warm item = %+v", remote)
	}
	legacy := response.Cairns[4]
	if !legacy.Cached || legacy.StreamSource != "cache" || len(legacy.StreamURLs) != 1 ||
		strings.Contains(legacy.StreamURLs[0].SignedURL, "/cairn/") {
		t.Fatalf("legacy warm item = %+v", legacy)
	}
}

func TestCairnRestoreKeepsWarmAndColdBehavior(t *testing.T) {
	hash := strings.Repeat("d", 40)
	man, _ := (manifest.Manifest{InfoHash: hash, Name: "Movie", Files: []manifest.File{
		{FileName: "movie.mkv", DirectURL: "blobs/b_movie", FileSize: 1234},
	}}).Marshal()
	repository := &fakeCairns{items: []auth.CairnItem{{InfoHash: hash, Name: "Movie", Archived: true}}}
	request := func() *http.Request {
		r := httptest.NewRequest("POST", "/api/cairn/"+hash+"/restore", nil)
		r.SetPathValue("hash", hash)
		return r.WithContext(context.WithValue(r.Context(), middleware.UserContextKey, &auth.User{ID: "u1", PlanID: "pro"}))
	}

	t.Run("warm is immediate", func(t *testing.T) {
		repo, pub := &fakeRepo{}, &fakePub{}
		store := &fakeStore{bytesByKey: map[string][]byte{manifest.Path(hash): man}, hasByKey: map[string]bool{"blobs/b_movie": true}}
		s := New(Deps{Jobs: repo, Store: store, Cairns: repository, Bus: pub, Slots: middleware.NewSlotTracker(repo)})
		w := httptest.NewRecorder()
		s.cairnRestore(w, request())
		if w.Code != 200 || len(repo.created) != 1 || repo.created[0].Status != jobs.StatusComplete || len(pub.published) != 0 {
			t.Fatalf("code=%d created=%+v published=%v", w.Code, repo.created, pub.published)
		}
	})

	t.Run("cold still queues restore", func(t *testing.T) {
		repo, pub := &fakeRepo{}, &fakePub{}
		s := New(Deps{Jobs: repo, Store: &fakeStore{}, Cairns: repository, Bus: pub, Slots: middleware.NewSlotTracker(repo)})
		w := httptest.NewRecorder()
		s.cairnRestore(w, request())
		if w.Code != 202 || len(repo.created) != 1 || repo.created[0].Status != jobs.StatusPending || len(pub.published) != 1 {
			t.Fatalf("code=%d created=%+v published=%v", w.Code, repo.created, pub.published)
		}
	})

	t.Run("remote warm is immediate", func(t *testing.T) {
		repo, pub := &fakeRepo{}, &fakePub{}
		lookup := fakeCachedLookup{hash: {
			UserID: "other", InfoHash: hash, Name: "Remote Movie", Status: jobs.StatusComplete, Node: "box2", FileSize: 4321,
			Files: []jobs.File{{Index: 0, Name: "remote.mkv", Size: 4321, Key: "blobs/b_remote"}},
		}}
		s := New(Deps{
			Jobs: repo, Store: &fakeStore{}, Cairns: repository, CachedJobs: lookup,
			Bus: pub, Slots: middleware.NewSlotTracker(repo),
		})
		w := httptest.NewRecorder()
		s.cairnRestore(w, request())
		if w.Code != 200 || len(repo.created) != 1 || len(pub.published) != 0 {
			t.Fatalf("code=%d created=%+v published=%v body=%s", w.Code, repo.created, pub.published, w.Body.String())
		}
		created := repo.created[0]
		if created.Status != jobs.StatusComplete || created.Node != "box2" || created.UserID != "u1" {
			t.Fatalf("linked warm job = %+v", created)
		}
		if !strings.Contains(w.Body.String(), "signed://box2/blobs/b_remote") {
			t.Fatalf("remote warm response = %s", w.Body.String())
		}
	})
}

func TestWarmCachedNode(t *testing.T) {
	s := &Server{Deps{CachedJobs: fakeCachedLookup{
		"h1": &jobs.Job{Node: "box2"},
	}}}
	if n, ok := s.warmCachedNode(context.Background(), "h1"); !ok || n != "box2" {
		t.Fatalf("cached hash = (%q,%v), want (box2,true)", n, ok)
	}
	if n, ok := s.warmCachedNode(context.Background(), "missing"); ok || n != "" {
		t.Fatalf("uncached hash = (%q,%v), want (\"\",false)", n, ok)
	}
}
