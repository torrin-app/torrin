package eviction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/torrin-app/torrin/shared/blob"
	"github.com/torrin-app/torrin/shared/jobs"
)

type Policy struct {
	NeverAccessedTTL int   // days before deleting never-watched content
	StandardTTL      int   // days of inactivity for 1-9 access count
	PopularTTL       int   // days of inactivity for 10+ access count
	PrewarmColdTTL   int   // days a never-watched prewarmed title is kept
	StorageCapBytes  int64 // soft cap; over this the budget pass reclaims cold content
	BudgetGraceDays  int   // budget pass never touches content idle fewer than this many days
}

var DefaultPolicy = Policy{
	NeverAccessedTTL: 7,
	StandardTTL:      14,
	PopularTTL:       45,
	PrewarmColdTTL:   14,
	StorageCapBytes:  300_000_000_000,
	BudgetGraceDays:  3,
}

const largeFileBytes int64 = 50_000_000_000

type Repo interface {
	GetEvictionCandidates(ctx context.Context, node string) ([]jobs.EvictionCandidate, error)
	GetTotalCachedSize(ctx context.Context, node string) (int64, error)
	ListByInfoHash(ctx context.Context, infoHash string) ([]*jobs.Job, error)
	Update(ctx context.Context, j *jobs.Job) error
	DropBlobRefs(ctx context.Context, infoHash string) ([]string, error)
	DeleteBlob(ctx context.Context, contentKey string) error
	OrphanedBlobs(ctx context.Context, limit int) ([]jobs.Blob, error)
}

type Storage interface {
	DeletePrefix(ctx context.Context, prefix string) error
	Delete(ctx context.Context, key string) error
}

type Engine struct {
	repo   Repo
	store  Storage
	policy Policy
	node   string
}

func New(repo Repo, store Storage, policy Policy, node string) *Engine {
	return &Engine{repo: repo, store: store, policy: policy, node: node}
}

func (e *Engine) RunDaily(ctx context.Context) {
	candidates, err := e.repo.GetEvictionCandidates(ctx, e.node)
	if err != nil {
		slog.Error("eviction: get candidates", "err", err)
		return
	}

	var evicted, freed int64
	for _, c := range candidates {
		if reason := e.ttlVerdict(c); reason != "" {
			if e.evict(ctx, c.InfoHash) == nil {
				evicted++
				freed += c.FileSize
				slog.Info("evicted", "name", c.Name, "reason", reason, "size_mb", c.FileSize/1e6)
			}
		}
	}

	e.RunGC(ctx)
	e.budgetPass(ctx, &evicted, &freed)
	slog.Info("eviction: complete", "evicted", evicted, "freed_gb", freed/1e9)
}

func (e *Engine) RunGC(ctx context.Context) {
	var deleted, freed int64
	for {
		orphans, err := e.repo.OrphanedBlobs(ctx, 500)
		if err != nil {
			slog.Error("eviction: orphaned blobs", "err", err)
			break
		}
		if len(orphans) == 0 {
			break
		}
		before := deleted
		for _, b := range orphans {
			if err := e.store.Delete(ctx, blob.StorageKey(b.ContentKey)); err != nil {
				slog.Warn("gc: delete blob", "key", b.ContentKey, "err", err)
				continue
			}
			e.repo.DeleteBlob(ctx, b.ContentKey)
			deleted++
			freed += b.Size
		}
		if deleted == before {
			break
		}
	}
	if deleted > 0 {
		slog.Info("eviction: gc complete", "deleted_blobs", deleted, "freed_gb", freed/1e9)
	}
}

func (e *Engine) ttlVerdict(c jobs.EvictionCandidate) string {
	switch {
	case c.FileSize > largeFileBytes:
		if c.DaysSinceAccess >= e.policy.PopularTTL {
			return fmt.Sprintf("large file (%dGB), %d days inactive", c.FileSize/1e9, c.DaysSinceAccess)
		}
	case c.IsPrewarm && c.AccessCount == 0:
		if c.DaysSinceAccess >= e.policy.PrewarmColdTTL {
			return fmt.Sprintf("cold prewarm, %d days unwatched", c.DaysSinceAccess)
		}
	case c.AccessCount == 0 && c.DaysSinceAccess >= e.policy.NeverAccessedTTL:
		return fmt.Sprintf("never accessed, %d days old", c.DaysSinceAccess)
	case c.AccessCount > 0 && c.AccessCount < 10 && c.DaysSinceAccess >= e.policy.StandardTTL:
		return fmt.Sprintf("%d accesses, %d days inactive", c.AccessCount, c.DaysSinceAccess)
	case c.AccessCount >= 10 && c.DaysSinceAccess >= e.policy.PopularTTL:
		return fmt.Sprintf("popular (%d accesses) but %d days inactive", c.AccessCount, c.DaysSinceAccess)
	}
	return ""
}

func (e *Engine) budgetPass(ctx context.Context, evicted, freed *int64) {
	total, _ := e.repo.GetTotalCachedSize(ctx, e.node)
	if total <= e.policy.StorageCapBytes {
		return
	}
	slog.Warn("eviction: over storage cap", "node", e.node, "total_gb", total/1e9, "cap_gb", e.policy.StorageCapBytes/1e9)

	candidates, _ := e.repo.GetEvictionCandidates(ctx, e.node)
	for _, c := range orderForReclaim(candidates) {
		if total <= e.policy.StorageCapBytes {
			return
		}
		if c.DaysSinceAccess < e.policy.BudgetGraceDays {
			continue
		}
		if e.evict(ctx, c.InfoHash) != nil {
			continue
		}
		total -= c.FileSize
		*evicted++
		*freed += c.FileSize
		slog.Info("budget evicted", "name", c.Name, "size_mb", c.FileSize/1e6, "idle_days", c.DaysSinceAccess)
	}
}

func orderForReclaim(candidates []jobs.EvictionCandidate) []jobs.EvictionCandidate {
	var coldPrewarm, small, large []jobs.EvictionCandidate
	for _, c := range candidates {
		switch {
		case c.IsPrewarm && c.AccessCount == 0:
			coldPrewarm = append(coldPrewarm, c)
		case c.FileSize > largeFileBytes:
			large = append(large, c)
		default:
			small = append(small, c)
		}
	}
	ordered := make([]jobs.EvictionCandidate, 0, len(candidates))
	ordered = append(ordered, coldPrewarm...)
	ordered = append(ordered, small...)
	return append(ordered, large...)
}

func (e *Engine) evict(ctx context.Context, infoHash string) error {
	siblings, _ := e.repo.ListByInfoHash(ctx, infoHash)
	for _, sib := range siblings {
		if sib.Status == jobs.StatusSeeding {
			return errSeeding
		}
	}
	if err := e.store.DeletePrefix(ctx, infoHash+"/"); err != nil {
		slog.Warn("eviction: delete failed", "hash", infoHash, "err", err)
		return err
	}
	e.purgeBlobs(ctx, infoHash)
	for _, sib := range siblings {
		if sib.Node != e.node {
			continue
		}
		sib.Status = jobs.StatusEvicted
		sib.Error = "content evicted from cache"
		e.repo.Update(ctx, sib)
	}
	return nil
}

func (e *Engine) purgeBlobs(ctx context.Context, infoHash string) {
	orphaned, err := e.repo.DropBlobRefs(ctx, infoHash)
	if err != nil {
		slog.Warn("eviction: drop blob refs", "hash", infoHash, "err", err)
		return
	}
	for _, ck := range orphaned {
		if err := e.store.Delete(ctx, blob.StorageKey(ck)); err != nil {
			slog.Warn("eviction: delete blob", "key", ck, "err", err)
			continue
		}
		e.repo.DeleteBlob(ctx, ck)
	}
}

var errSeeding = errors.New("skipped: sibling still seeding")

func (e *Engine) StartSchedule(ctx context.Context, hour int) {
	go func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
			if next.Before(now) {
				next = next.Add(24 * time.Hour)
			}
			slog.Info("eviction: next run", "at", next.Format("2006-01-02 15:04"))
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(next)):
				e.RunDaily(ctx)
			}
		}
	}()
}
