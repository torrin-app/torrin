package jobs

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("job not found")

type Repository interface {
	Create(ctx context.Context, j *Job) error
	Update(ctx context.Context, j *Job) error
	Get(ctx context.Context, id string) (*Job, error)
	GetByInfoHash(ctx context.Context, infoHash string) (*Job, error)
	ListByInfoHash(ctx context.Context, infoHash string) ([]*Job, error)
	ListByUser(ctx context.Context, userID string, limit int) ([]*Job, error)
	ListByUserBefore(ctx context.Context, userID string, before time.Time, beforeID string, limit int) ([]*Job, error)
	ListByStatus(ctx context.Context, status Status) ([]*Job, error)
	Delete(ctx context.Context, id string) error
	ActiveCount(ctx context.Context, userID string) (int, error)
	BudgetUsed(ctx context.Context) (int64, error)
	RecordView(ctx context.Context, infoHash, userID string) (bool, error)
	SetProgress(ctx context.Context, id string, pct float64, speed int64) error
}
