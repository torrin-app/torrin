package main

import (
	"context"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/billing"
)

func walletRenewLoop(ctx context.Context, users *auth.Store, wake func()) {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	billing.RenewWalletPlans(ctx, users)
	wake()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			billing.RenewWalletPlans(ctx, users)
			wake()
		}
	}
}
