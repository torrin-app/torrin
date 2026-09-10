package jobs

import "context"

// Scheduler snapshots can be stale when another API replica promotes a job.
// Only mutate queued rows, and never write the snapshot's other fields back.
func (p *Postgres) SetQueuedPriority(ctx context.Context, id string, priority int) (bool, error) {
	ct, err := p.pool.Exec(ctx, `UPDATE jobs SET priority=$2, updated_at=now()
		WHERE id=$1 AND status='queued' AND priority<>$2`, id, priority)
	return ct.RowsAffected() > 0, err
}

func (p *Postgres) FailQueued(ctx context.Context, id, reason string) (bool, error) {
	ct, err := p.pool.Exec(ctx, `UPDATE jobs SET status='failed', error=$2, updated_at=now()
		WHERE id=$1 AND status='queued'`, id, reason)
	return ct.RowsAffected() > 0, err
}
