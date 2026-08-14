package billing

import (
	"context"
	"log/slog"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
)

type userLookup interface {
	GetByEmail(ctx context.Context, email string) (*auth.User, error)
	GetBySubscription(ctx context.Context, subID string) (*auth.User, error)
	CreateUser(ctx context.Context, email string, referralCode ...string) (*auth.User, error)
}

func getOrCreateUser(ctx context.Context, users userLookup, email string) (*auth.User, bool, error) {
	if u, err := users.GetByEmail(ctx, email); err == nil && u != nil {
		return u, false, nil
	}
	u, err := users.CreateUser(ctx, email)
	return u, err == nil, err
}

func resolveSaleUser(ctx context.Context, users userLookup, email, subID string) (*auth.User, bool, error) {
	if subID != "" {
		if u, err := users.GetBySubscription(ctx, subID); err == nil && u != nil {
			return u, false, nil
		}
	}
	return getOrCreateUser(ctx, users, email)
}

func applyCryptoPlan(ctx context.Context, users *auth.Store, user *auth.User, email, eventID, planID, period, via string, days int) {
	expiresAt := cryptoExpiry(period, days)
	if user.ExpiresAt.After(time.Now()) && !user.IsPaused() && user.PlanID != "free" && period != "lifetime" {
		if remaining := time.Until(user.ExpiresAt); remaining > 0 {
			expiresAt = expiresAt.Add(remaining)
		}
	}
	if err := users.UpdatePlan(ctx, user.ID, planID, "", eventID, period, expiresAt); err != nil {
		slog.Error(via+" update plan", "err", err)
		return
	}
	cents, _ := plans.PriceCents(planID, period, days)
	users.RecordProcessedSale(ctx, eventID, user.ID, cents, "USD")
	if period == "monthly" || period == "yearly" {
		users.CreditReferral(ctx, user.ID)
	}
	slog.Info("plan activated via "+via, "email", email, "plan", planID, "period", period, "expires", expiresAt.Format("2006-01-02"))
}
