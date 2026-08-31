package eviction

import (
	"context"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/storage"
)

func TestReconcileDeletesOnlyUnreferencedAndAged(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	fresh := time.Now().Add(-1 * time.Hour)
	repo := &fakeRepo{
		evicted:      map[string]bool{},
		deletedBlobs: map[string]bool{},
		refs:         map[string]struct{}{"b_ref": {}},
	}
	store := &fakeStorage{
		deleted:     map[string]bool{},
		deletedKeys: map[string]bool{},
		listObjs: []storage.ObjMeta{
			{Key: "blobs/b_ref", Size: 100, ModTime: old},
			{Key: "blobs/b_orphan_old", Size: 200, ModTime: old},
			{Key: "blobs/b_orphan_fresh", Size: 300, ModTime: fresh},
		},
	}

	New(repo, store, DefaultPolicy, "").RunDaily(context.Background())

	if store.deletedKeys["blobs/b_ref"] {
		t.Fatal("deleted a referenced blob")
	}
	if store.deletedKeys["blobs/b_orphan_fresh"] {
		t.Fatal("deleted an orphan still within the grace window")
	}
	if !store.deletedKeys["blobs/b_orphan_old"] {
		t.Fatal("did not delete the aged orphan")
	}
	if !repo.deletedBlobs["b_orphan_old"] {
		t.Fatal("did not drop the aged orphan blob row")
	}
	if repo.deletedBlobs["b_ref"] || repo.deletedBlobs["b_orphan_fresh"] {
		t.Fatal("dropped a blob row it should have kept")
	}
}

func TestReconcileRunsForRemoteSkipGCNodes(t *testing.T) {
	repo := &fakeRepo{
		evicted:      map[string]bool{},
		deletedBlobs: map[string]bool{},
	}
	store := &fakeStorage{
		deleted:     map[string]bool{},
		deletedKeys: map[string]bool{},
		listObjs:    []storage.ObjMeta{{Key: "blobs/b_orphan", Size: 1, ModTime: time.Now().Add(-48 * time.Hour)}},
	}
	eng := New(repo, store, DefaultPolicy, "box2")
	eng.SkipGC()
	eng.RunDaily(context.Background())

	if !store.deletedKeys["blobs/b_orphan"] {
		t.Fatal("controller must reconcile a remote (skip-gc) storage node")
	}
}
