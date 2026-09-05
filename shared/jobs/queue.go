package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Admission string

const (
	AdmissionAdmitted Admission = "admitted"
	AdmissionQueued   Admission = "queued"
	AdmissionExisting Admission = "existing"
)

var ErrQueueFull = errors.New("download queue full")

const (
	queueGlobalLock       int64 = 0x746f7272696e7175
	initialBudgetHeadroom       = int64(1_000_000_000)
	promoteBudgetHeadroom       = int64(5_000_000_000)
)

// Admit creates a job and atomically reserves a slot when one is available.
// PostgreSQL advisory locks make the decision safe across API replicas.
func (p *Postgres) Admit(ctx context.Context, j *Job, maxConcurrent, maxQueued int, budget int64) (Admission, error) {
	if err := prepareCreate(j); err != nil {
		return "", err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	if err := lockAdmission(ctx, tx, j.UserID, j.BudgetGated); err != nil {
		return "", err
	}
	if err := lockDownloadHash(ctx, tx, j.InfoHash); err != nil {
		return "", err
	}
	if existing, err := liveAccountJobTx(ctx, tx, j); err != nil {
		return "", err
	} else if existing != nil {
		*j = *existing
		return AdmissionExisting, nil
	}
	queued, err := queuedCountTx(ctx, tx, j.UserID, "")
	if err != nil {
		return "", err
	}
	inflight, err := inflightCountTx(ctx, tx, j.UserID)
	if err != nil {
		return "", err
	}
	sameHashActive, err := activeHashTx(ctx, tx, j.InfoHash, j.ID)
	if err != nil {
		return "", err
	}
	mustQueue := queued > 0 || inflight >= normalizeConcurrent(maxConcurrent) || sameHashActive
	if j.BudgetGated {
		budgetQueued, err := budgetQueueExistsTx(ctx, tx, "")
		if err != nil {
			return "", err
		}
		used, err := budgetUsedTx(ctx, tx)
		if err != nil {
			return "", err
		}
		mustQueue = mustQueue || budgetQueued || budget-used < initialBudgetHeadroom
	}
	if mustQueue {
		if maxQueued > 0 && queued >= maxQueued {
			return "", ErrQueueFull
		}
		j.Status = StatusQueued
	} else {
		j.Status = StatusPending
	}
	created, err := insertJob(ctx, tx, j)
	if err != nil {
		return "", err
	}
	if !created {
		existing, err := liveAccountJobTx(ctx, tx, j)
		if err != nil {
			return "", err
		}
		if existing == nil {
			return "", ErrNotFound
		}
		*j = *existing
		return AdmissionExisting, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	if j.Status == StatusQueued {
		return AdmissionQueued, nil
	}
	return AdmissionAdmitted, nil
}

// Readmit retries or rechecks an existing job without allowing it to bypass
// older queued work.
func (p *Postgres) Readmit(ctx context.Context, id string, maxConcurrent, maxQueued int, budget int64) (Admission, *Job, error) {
	current, err := p.Get(ctx, id)
	if err != nil {
		return "", nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback(ctx)
	if err := lockAdmission(ctx, tx, current.UserID, current.BudgetGated); err != nil {
		return "", nil, err
	}
	if err := lockDownloadHash(ctx, tx, current.InfoHash); err != nil {
		return "", nil, err
	}
	job, err := scanOne(tx.QueryRow(ctx, `SELECT `+cols+` FROM jobs WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return "", nil, err
	}
	if job.Status != StatusFailed && job.Status != StatusEvicted && job.Status != StatusComplete {
		return AdmissionExisting, job, nil
	}
	if existing, err := liveAccountJobTx(ctx, tx, job); err != nil {
		return "", nil, err
	} else if existing != nil && existing.ID != job.ID {
		return AdmissionExisting, existing, nil
	}
	queued, err := queuedCountTx(ctx, tx, job.UserID, job.ID)
	if err != nil {
		return "", nil, err
	}
	inflight, err := inflightCountTx(ctx, tx, job.UserID)
	if err != nil {
		return "", nil, err
	}
	sameHashActive, err := activeHashTx(ctx, tx, job.InfoHash, job.ID)
	if err != nil {
		return "", nil, err
	}
	mustQueue := queued > 0 || inflight >= normalizeConcurrent(maxConcurrent) || sameHashActive
	if job.BudgetGated {
		budgetQueued, err := budgetQueueExistsTx(ctx, tx, job.ID)
		if err != nil {
			return "", nil, err
		}
		used, err := budgetUsedTx(ctx, tx)
		if err != nil {
			return "", nil, err
		}
		mustQueue = mustQueue || budgetQueued || budget-used < initialBudgetHeadroom
	}
	if mustQueue && maxQueued > 0 && queued >= maxQueued {
		return "", nil, ErrQueueFull
	}
	if mustQueue {
		job.Status = StatusQueued
	} else {
		job.Status = StatusPending
	}
	now := time.Now().UTC()
	job.Error, job.Node, job.Progress, job.Speed = "", "", 0, 0
	job.CreatedAt, job.UpdatedAt = now, now
	_, err = tx.Exec(ctx, `UPDATE jobs SET status=$2, error='', node='', progress=0, dl_speed=0,
		created_at=$3, updated_at=$3 WHERE id=$1`, job.ID, string(job.Status), now)
	if err != nil {
		return "", nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", nil, err
	}
	if job.Status == StatusQueued {
		return AdmissionQueued, job, nil
	}
	return AdmissionAdmitted, job, nil
}

// PromoteQueued atomically claims one queued job. A nil job means the
// candidate is no longer queued or is not currently eligible.
func (p *Postgres) PromoteQueued(ctx context.Context, id string, maxConcurrent int, budget int64) (*Job, error) {
	current, err := p.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if current.Status != StatusQueued {
		return nil, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := lockAdmission(ctx, tx, current.UserID, current.BudgetGated); err != nil {
		return nil, err
	}
	if err := lockDownloadHash(ctx, tx, current.InfoHash); err != nil {
		return nil, err
	}
	job, err := scanOne(tx.QueryRow(ctx, `SELECT `+cols+` FROM jobs WHERE id=$1 FOR UPDATE`, id))
	if err != nil || job.Status != StatusQueued {
		if errors.Is(err, ErrNotFound) || err == nil {
			return nil, nil
		}
		return nil, err
	}
	inflight, err := inflightCountTx(ctx, tx, job.UserID)
	if err != nil || inflight >= normalizeConcurrent(maxConcurrent) {
		return nil, err
	}
	var older bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs q WHERE q.user_id=$1 AND q.status='queued' AND q.id<>$2
		AND (q.priority>$3 OR (q.priority=$3 AND (q.created_at<$4 OR (q.created_at=$4 AND q.id<$2))))
		AND NOT EXISTS (SELECT 1 FROM jobs a WHERE a.id<>q.id AND a.info_hash=q.info_hash
			AND a.seed=false AND a.status IN `+concurrencyStates+`))`,
		job.UserID, job.ID, job.Priority, job.CreatedAt).Scan(&older)
	if err != nil || older {
		return nil, err
	}
	var sameHashActive bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE id<>$1 AND info_hash=$2 AND seed=false
		AND status IN `+concurrencyStates+`)`, job.ID, job.InfoHash).Scan(&sameHashActive)
	if err != nil || sameHashActive {
		return nil, err
	}
	if job.BudgetGated {
		used, err := budgetUsedTx(ctx, tx)
		if err != nil || budget-used < promoteBudgetHeadroom {
			return nil, err
		}
	}
	job.Status, job.Node, job.UpdatedAt = StatusPending, "", time.Now().UTC()
	_, err = tx.Exec(ctx, `UPDATE jobs SET status='pending', node='', updated_at=$2 WHERE id=$1 AND status='queued'`, job.ID, job.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return job, nil
}

func (p *Postgres) QueuedCount(ctx context.Context, userID string) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE user_id=$1 AND status='queued'`, userID).Scan(&n)
	return n, err
}

func lockAdmission(ctx context.Context, tx pgx.Tx, userID string, global bool) error {
	if global {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, queueGlobalLock); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, userID)
	return err
}

func normalizeConcurrent(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

func queuedCountTx(ctx context.Context, tx pgx.Tx, userID, excludeID string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE user_id=$1 AND status='queued' AND id<>$2`, userID, excludeID).Scan(&n)
	return n, err
}

func inflightCountTx(ctx context.Context, tx pgx.Tx, userID string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE user_id=$1 AND seed=false AND status IN `+concurrencyStates, userID).Scan(&n)
	return n, err
}

func budgetQueueExistsTx(ctx context.Context, tx pgx.Tx, excludeID string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE status='queued' AND budget_gated=true AND id<>$1)`, excludeID).Scan(&ok)
	return ok, err
}

func budgetUsedTx(ctx context.Context, tx pgx.Tx) (int64, error) {
	var total int64
	err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(CASE WHEN file_size > 0 THEN file_size ELSE 5000000000 END), 0)
		FROM jobs WHERE status IN `+budgetStates).Scan(&total)
	return total, err
}
