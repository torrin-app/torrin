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
	warmManifest, _ := (manifest.Manifest{InfoHash: warmHash, Name: "Warm", Files: []manifest.File{
		{FileName: "warm.mkv", DirectURL: "blobs/b_warm", FileSize: 1234},
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

	primary := &fakeStore{
		bytesByKey: map[string][]byte{manifest.Path(warmHash): warmManifest},
		hasByKey:   map[string]bool{"blobs/b_warm": true},
	}
	archive := &fakeStore{bytesByKey: map[string][]byte{nzb.StorageKey(coldHash): coldNZB}}
	repo := &fakeCairns{items: []auth.CairnItem{
		{InfoHash: warmHash, Name: "Warm", Archived: true},
		{InfoHash: coldHash, Name: "Cold", Archived: true},
		{InfoHash: pendingHash, Name: "Pending", Archived: false},
	}}
	s := New(Deps{
		Jobs: &fakeRepo{}, Store: primary, Cairns: repo, CairnStore: archive,
		CairnCipher: cipher, CairnDirect: true,
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
	if repo.listed != "u1" || len(response.Cairns) != 3 {
		t.Fatalf("listed=%q items=%d", repo.listed, len(response.Cairns))
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
}
