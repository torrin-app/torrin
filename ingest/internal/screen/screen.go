package screen

import (
	"context"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/safety"
)

type BanFunc func(ctx context.Context, userID, reason string)

// AllowFunc reports whether a release hash is allowlisted (approved to bypass
// soft safety flags). Never consulted for hard terms or bans.
type AllowFunc func(ctx context.Context, hash string) bool

func Blocked(ctx context.Context, ban BanFunc, job *jobs.Job, names ...string) bool {
	return BlockedAllow(ctx, ban, nil, job, names...)
}

func BlockedAllow(ctx context.Context, ban BanFunc, allow AllowFunc, job *jobs.Job, names ...string) bool {
	v := safety.Screen(append([]string{job.Name, job.Magnet}, names...)...)
	if !v.Blocked {
		return false
	}
	if !v.Ban && allow != nil && job.InfoHash != "" && allow(ctx, job.InfoHash) {
		return false
	}
	if v.Ban && ban != nil && job.UserID != "" && job.UserID != "system" {
		ban(ctx, job.UserID, v.Reason)
	}
	return true
}
