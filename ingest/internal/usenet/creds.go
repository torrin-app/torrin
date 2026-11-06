package usenet

import (
	"context"

	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/usenet/download"
)

func (r *Runner) HasProvider(ctx context.Context, userID string) bool {
	list, _ := r.credsList(ctx, userID)
	return len(list) > 0
}

func (r *Runner) credsList(ctx context.Context, userID string) ([]download.Credentials, bool) {
	if ps, err := r.users.EnabledUsenetProviders(ctx, userID); err == nil && len(ps) > 0 {
		out := make([]download.Credentials, 0, len(ps))
		for _, p := range ps {
			out = append(out, download.Credentials{Host: p.Host, Port: p.Port, Username: p.Username, Password: p.Password, SSL: p.SSL, MaxConns: p.MaxConns})
		}
		return out, false
	}
	if c, err := r.users.GetUsenetCreds(ctx, userID); err == nil && c.Host != "" {
		return []download.Credentials{{Host: c.Host, Port: c.Port, Username: c.Username, Password: c.Password, SSL: c.SSL, MaxConns: c.MaxConns}}, false
	}
	if u, err := r.users.GetByID(ctx, userID); err == nil && u != nil {
		if p, ok := plans.Get(u.PlanID); ok && p.SystemUsenet {
			return r.sysCreds, true
		}
	}
	return nil, false
}
