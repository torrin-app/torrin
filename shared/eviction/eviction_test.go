package eviction

import (
	"context"
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/storage"
)

type fakeRepo struct {
	candidates   []jobs.EvictionCandidate
	total        int64
	evicted      map[string]bool
	evictedIDs   map[string]bool
	siblings     map[string][]*jobs.Job
	orphans      map[string][]string
	blobs        []jobs.Blob
	deletedBlobs map[string]bool
	refs         map[string]struct{}

	danglingDropped int64
	danglingCalled  bool
}

func (f *fakeRepo) ReferencedKeys(context.Context) (map[string]struct{}, error) {
	return f.refs, nil
}

func (f *fakeRepo) OrphanedBlobs(_ context.Context, limit int) ([]jobs.Blob, error) {
	var out []jobs.Blob
	for _, b := range f.blobs {
		if !f.deletedBlobs[b.ContentKey] {
			out = append(out, b)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeRepo) GetEvictionCandidates(context.Context, string) ([]jobs.EvictionCandidate, error) {
	return f.candidates, nil
}
func (f *fakeRepo) GetTotalCachedSize(context.Context, string) (int64, error) { return f.total, nil }
func (f *fakeRepo) ListByInfoHash(_ context.Context, h string) ([]*jobs.Job, error) {
	if s, ok := f.siblings[h]; ok {
		return s, nil
	}
	return []*jobs.Job{{ID: h, InfoHash: h}}, nil
}
func (f *fakeRepo) Update(_ context.Context, j *jobs.Job) error {
	if j.Status == jobs.StatusEvicted {
		f.evicted[j.InfoHash] = true
		if f.evictedIDs != nil {
			f.evictedIDs[j.ID] = true
		}
	}
	return nil
}
func (f *fakeRepo) DropBlobRefs(_ context.Context, infoHash string) ([]string, error) {
	if f.orphans == nil {
		return nil, nil
	}
	return f.orphans[infoHash], nil
}
func (f *fakeRepo) DropDanglingRefs(context.Context) (int64, error) {
	f.danglingCalled = true
	return f.danglingDropped, nil
}
func (f *fakeRepo) DeleteBlob(_ context.Context, ck string) error {
	if f.deletedBlobs == nil {
		f.deletedBlobs = map[string]bool{}
	}
	f.deletedBlobs[ck] = true
	return nil
}

type fakeStorage struct {
	deleted     map[string]bool
	deletedKeys map[string]bool
	listObjs    []storage.ObjMeta
}

func (f *fakeStorage) List(context.Context, string) ([]storage.ObjMeta, error) {
	return f.listObjs, nil
}

func (f *fakeStorage) DeletePrefix(_ context.Context, prefix string) error {
	f.deleted[prefix] = true
	return nil
}
func (f *fakeStorage) Delete(_ context.Context, key string) error {
	if f.deletedKeys == nil {
		f.deletedKeys = map[string]bool{}
	}
	f.deletedKeys[key] = true
	return nil
}

func TestEvictOnlyMarksOwnNode(t *testing.T) {
	repo := &fakeRepo{
		evicted:    map[string]bool{},
		evictedIDs: map[string]bool{},
		siblings: map[string][]*jobs.Job{
			"h1": {{ID: "local", InfoHash: "h1", Node: ""}, {ID: "other", InfoHash: "h1", Node: "box2"}},
		},
		candidates: []jobs.EvictionCandidate{{InfoHash: "h1", AccessCount: 0, DaysSinceAccess: 30}},
	}
	store := &fakeStorage{deleted: map[string]bool{}}
	New(repo, store, DefaultPolicy, "").RunDaily(context.Background())
	if !repo.evictedIDs["local"] {
		t.Error("same-node job should be marked evicted")
	}
	if repo.evictedIDs["other"] {
		t.Error("other-node job must NOT be marked evicted (that node still has the content)")
	}
}

func TestTTLEviction(t *testing.T) {
	repo := &fakeRepo{
		evicted: map[string]bool{},
		candidates: []jobs.EvictionCandidate{
			{InfoHash: "never_old", AccessCount: 0, DaysSinceAccess: 10},     // > 7 → evict
			{InfoHash: "never_fresh", AccessCount: 0, DaysSinceAccess: 5},    // < 7 → keep
			{InfoHash: "popular", AccessCount: 15, DaysSinceAccess: 10},      // < 45 → keep
			{InfoHash: "stale_mid", AccessCount: 3, DaysSinceAccess: 20},     // > 14 → evict
			{InfoHash: "big", FileSize: 60_000_000_000, DaysSinceAccess: 50}, // large, > 45 → evict
		},
	}
	store := &fakeStorage{deleted: map[string]bool{}}
	New(repo, store, DefaultPolicy, "").RunDaily(context.Background())

	for _, h := range []string{"never_old", "stale_mid", "big"} {
		if !store.deleted[h+"/"] || !repo.evicted[h] {
			t.Errorf("%s should be evicted", h)
		}
	}
	for _, h := range []string{"never_fresh", "popular"} {
		if store.deleted[h+"/"] {
			t.Errorf("%s should NOT be evicted", h)
		}
	}
}

func TestEvictPurgesOnlyOrphanBlobs(t *testing.T) {
	repo := &fakeRepo{
		evicted: map[string]bool{},
		orphans: map[string][]string{"cold": {"b_orphan"}, "shared": nil},
		candidates: []jobs.EvictionCandidate{
			{InfoHash: "cold", AccessCount: 0, DaysSinceAccess: 30},
			{InfoHash: "shared", AccessCount: 0, DaysSinceAccess: 30},
		},
	}
	store := &fakeStorage{deleted: map[string]bool{}}
	New(repo, store, DefaultPolicy, "").RunDaily(context.Background())

	if !store.deletedKeys["blobs/b_orphan"] {
		t.Error("orphaned blob should be deleted")
	}
	if store.deletedKeys["blobs/b_shared"] {
		t.Error("still-referenced blob must not be deleted")
	}
}

func TestRunDailyDropsDanglingRefs(t *testing.T) {
	repo := &fakeRepo{evicted: map[string]bool{}}
	New(repo, &fakeStorage{deleted: map[string]bool{}}, DefaultPolicy, "").RunDaily(context.Background())
	if !repo.danglingCalled {
		t.Error("RunDaily must drop dangling blob_refs so deleted downloads stop stranding blobs")
	}
}

func TestBudgetEvictsSmallBeforeLarge(t *testing.T) {
	repo := &fakeRepo{
		evicted: map[string]bool{},
		total:   340_000_000_000, // 40GB over the 300GB cap
		candidates: []jobs.EvictionCandidate{
			{InfoHash: "big", FileSize: 200_000_000_000, AccessCount: 5, DaysSinceAccess: 5}, // large, listed first
			{InfoHash: "small_a", FileSize: 30_000_000_000, AccessCount: 5, DaysSinceAccess: 5},
			{InfoHash: "small_b", FileSize: 30_000_000_000, AccessCount: 5, DaysSinceAccess: 5},
		},
	}
	store := &fakeStorage{deleted: map[string]bool{}}
	New(repo, store, DefaultPolicy, "").RunDaily(context.Background())

	if store.deleted["big/"] {
		t.Error("large file must be spared when evicting small files frees enough")
	}
	if !store.deleted["small_a/"] || !store.deleted["small_b/"] {
		t.Error("small cold files should be budget-evicted first")
	}
}

func TestBudgetPassRespectsGrace(t *testing.T) {
	repo := &fakeRepo{
		evicted: map[string]bool{},
		total:   400_000_000_000, // over the 300GB cap
		candidates: []jobs.EvictionCandidate{
			{InfoHash: "fresh_big", FileSize: 200_000_000_000, AccessCount: 20, DaysSinceAccess: 1}, // within grace → skip
		},
	}
	store := &fakeStorage{deleted: map[string]bool{}}
	New(repo, store, DefaultPolicy, "").RunDaily(context.Background())

	if store.deleted["fresh_big/"] {
		t.Error("content inside grace window must not be budget-evicted")
	}
}

func TestGCDeletesOrphanBlobs(t *testing.T) {
	repo := &fakeRepo{
		evicted:      map[string]bool{},
		deletedBlobs: map[string]bool{},
		blobs: []jobs.Blob{
			{ContentKey: "b_1", Size: 100},
			{ContentKey: "b_2", Size: 200},
			{ContentKey: "b_3", Size: 300},
		},
	}
	store := &fakeStorage{deleted: map[string]bool{}, deletedKeys: map[string]bool{}}
	New(repo, store, DefaultPolicy, "").RunGC(context.Background())

	for _, ck := range []string{"b_1", "b_2", "b_3"} {
		if !repo.deletedBlobs[ck] {
			t.Errorf("orphan %s not removed from blobs table", ck)
		}
		if !store.deletedKeys["blobs/"+ck] {
			t.Errorf("orphan %s not physically deleted (keys=%v)", ck, store.deletedKeys)
		}
	}
}
