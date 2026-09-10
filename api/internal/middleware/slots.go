package middleware

import (
	"context"
	"sync"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
)

type SlotTracker struct {
	repo      jobs.Repository
	locks     sync.Map
	budget    int64
	maxQueued int
	wakeMu    sync.RWMutex
	wake      func()
}

func NewSlotTracker(repo jobs.Repository) *SlotTracker {
	return &SlotTracker{repo: repo, maxQueued: 100}
}

type atomicQueue interface {
	Admit(context.Context, *jobs.Job, int, int, int64) (jobs.Admission, error)
	Readmit(context.Context, string, int, int, int64) (jobs.Admission, *jobs.Job, error)
	QueuedCount(context.Context, string) (int, error)
}

func (st *SlotTracker) Configure(budget int64, maxQueued int) {
	st.budget = budget
	if maxQueued > 0 {
		st.maxQueued = maxQueued
	}
}

func (st *SlotTracker) SetWake(fn func()) {
	st.wakeMu.Lock()
	st.wake = fn
	st.wakeMu.Unlock()
}

func (st *SlotTracker) Wake() {
	st.wakeMu.RLock()
	fn := st.wake
	st.wakeMu.RUnlock()
	if fn != nil {
		fn()
	}
}

func (st *SlotTracker) userLock(userID string) *sync.Mutex {
	v, _ := st.locks.LoadOrStore(userID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (st *SlotTracker) ActiveSlots(ctx context.Context, userID string) int {
	n, _ := st.repo.DownloadingCount(ctx, userID)
	return n
}

func (st *SlotTracker) Queued(ctx context.Context, userID string) int {
	if q, ok := st.repo.(interface {
		QueuedCount(context.Context, string) (int, error)
	}); ok {
		n, _ := q.QueuedCount(ctx, userID)
		return n
	}
	return 0
}

func (st *SlotTracker) MaxQueued() int { return st.maxQueued }

// Admit creates a normal download as pending when capacity is available, or
// queued otherwise. PostgreSQL supplies the cross-process atomic path; the
// locked fallback keeps lightweight handler fakes useful in unit tests.
func (st *SlotTracker) Admit(ctx context.Context, job *jobs.Job, plan plans.Plan, budgetGated bool) (jobs.Admission, error) {
	job.BudgetGated = budgetGated
	job.MaxBytes, job.Priority = plan.MaxTorrentBytes, plan.Priority
	if q, ok := st.repo.(atomicQueue); ok {
		d, err := q.Admit(ctx, job, plan.MaxConcurrent, st.maxQueued, st.budget)
		if d == jobs.AdmissionQueued {
			st.Wake()
		}
		return d, err
	}
	mu := st.userLock(job.UserID)
	mu.Lock()
	defer mu.Unlock()
	sameHashActive := false
	if siblings, err := st.repo.ListByInfoHash(ctx, job.InfoHash); err == nil {
		for _, existing := range siblings {
			if existing.UserID == job.UserID && !existing.Seed && existing.Status != jobs.StatusFailed && existing.Status != jobs.StatusEvicted {
				*job = *existing
				return jobs.AdmissionExisting, nil
			}
			if existing.ConsumesDownloadSlot() {
				sameHashActive = true
			}
		}
	}
	queued := st.Queued(ctx, job.UserID)
	maxConcurrent := plan.MaxConcurrent
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	if queued > 0 || st.ActiveSlots(ctx, job.UserID) >= maxConcurrent || sameHashActive {
		job.Status = jobs.StatusQueued
	} else if budgetGated {
		used, _ := st.repo.BudgetUsed(ctx)
		if st.budget-used < 1_000_000_000 {
			job.Status = jobs.StatusQueued
		} else {
			job.Status = jobs.StatusPending
		}
	} else {
		job.Status = jobs.StatusPending
	}
	if job.Status == jobs.StatusQueued && st.maxQueued > 0 && queued >= st.maxQueued {
		return "", jobs.ErrQueueFull
	}
	created, err := jobs.CreateAccountOnce(ctx, st.repo, job)
	if err != nil {
		return "", err
	}
	if !created {
		return jobs.AdmissionExisting, nil
	}
	if job.Status == jobs.StatusQueued {
		st.Wake()
		return jobs.AdmissionQueued, nil
	}
	return jobs.AdmissionAdmitted, nil
}

func (st *SlotTracker) Readmit(ctx context.Context, id string, plan plans.Plan) (jobs.Admission, *jobs.Job, error) {
	if q, ok := st.repo.(atomicQueue); ok {
		d, job, err := q.Readmit(ctx, id, plan.MaxConcurrent, st.maxQueued, st.budget)
		if d == jobs.AdmissionQueued {
			st.Wake()
		}
		return d, job, err
	}
	job, err := st.repo.Get(ctx, id)
	if err != nil {
		return "", nil, err
	}
	job.Status, job.Error = jobs.StatusPending, ""
	if err := st.repo.Update(ctx, job); err != nil {
		return "", nil, err
	}
	return jobs.AdmissionAdmitted, job, nil
}
