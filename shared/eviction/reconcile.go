package eviction

import (
	"context"
	"log/slog"
	"path"
	"time"
)

const reconcileGrace = 24 * time.Hour

func (e *Engine) runReconcile(ctx context.Context) {
	refs, err := e.repo.ReferencedKeys(ctx)
	if err != nil {
		slog.Error("reconcile: referenced keys", "err", err)
		return
	}
	objs, err := e.store.List(ctx, "blobs/")
	if err != nil {
		slog.Error("reconcile: list blobs", "err", err)
		return
	}
	cutoff := time.Now().Add(-reconcileGrace)
	var deleted, freed int64
	for _, o := range objs {
		ck := path.Base(o.Key)
		if _, ok := refs[ck]; ok {
			continue
		}
		if o.ModTime.After(cutoff) {
			continue
		}
		if err := e.store.Delete(ctx, o.Key); err != nil {
			slog.Warn("reconcile: delete", "key", ck, "err", err)
			continue
		}
		e.repo.DeleteBlob(ctx, ck)
		deleted++
		freed += o.Size
	}
	if deleted > 0 {
		slog.Info("reconcile: complete", "node", e.node, "deleted_blobs", deleted, "freed_gb", freed/1e9)
	}
}
