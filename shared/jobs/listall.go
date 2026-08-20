package jobs

import (
	"context"
	"log/slog"
	"time"
)

const (
	listAllPage = 500
	listAllMax  = 100_000
)

type userLister interface {
	ListByUser(ctx context.Context, userID string, limit int) ([]*Job, error)
	ListByUserBefore(ctx context.Context, userID string, before time.Time, beforeID string, limit int) ([]*Job, error)
}

func ListAll(ctx context.Context, l userLister, userID string) ([]*Job, error) {
	page, err := l.ListByUser(ctx, userID, listAllPage)
	if err != nil {
		return nil, err
	}
	out := page
	for len(page) == listAllPage && len(out) < listAllMax {
		last := out[len(out)-1]
		page, err = l.ListByUserBefore(ctx, userID, last.CreatedAt, last.ID, listAllPage)
		if err != nil {
			return out, err
		}
		out = append(out, page...)
	}
	if len(out) >= listAllMax {
		slog.Warn("jobs: user list truncated at ceiling", "user", userID, "count", len(out))
	}
	return out, nil
}
