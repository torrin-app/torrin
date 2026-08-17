package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
)

type userLookup interface {
	GetByEmail(ctx context.Context, email string) (*auth.User, error)
	GetBySubscription(ctx context.Context, subID string) (*auth.User, error)
	CreateUser(ctx context.Context, email string, referralCode ...string) (*auth.User, error)
}

func topupRef(reason, ref string) (string, bool) {
	if ref == "" {
		return "", false
	}
	return reason + ":" + ref, true
}

func creditWallet(ctx context.Context, users *auth.Store, userID, creditsStr, reason, ref string) {
	fullRef, ok := topupRef(reason, ref)
	if !ok {
		slog.Warn("wallet topup with no ref", "reason", reason)
		return
	}
	cents := 0
	fmt.Sscanf(creditsStr, "%d", &cents)
	if cents <= 0 {
		slog.Warn("wallet topup with no credits", "ref", fullRef)
		return
	}
	if err := users.Credit(ctx, userID, int64(cents), reason, fullRef); err != nil {
		slog.Error("wallet topup credit", "err", err, "ref", fullRef)
		return
	}
	slog.Info("wallet topped up", "user", userID, "cents", cents, "via", reason)
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

type planUpdater interface {
	UpdatePlan(ctx context.Context, userID, planID, subID, license, recurrence string, expiresAt time.Time) error
}

type planStore interface {
	WalletBuyPlan(ctx context.Context, userID string, cents int64, reason, ref, planID, subID, license, recurrence string, expiresAt time.Time) (ok, applied bool, err error)
	CreditReferral(ctx context.Context, userID string)
}

func planExpiry(user *auth.User, period string, days int) time.Time {
	expiresAt := cryptoExpiry(period, days)
	if user.ExpiresAt.After(time.Now()) && !user.IsPaused() && user.PlanID != "free" && period != "lifetime" {
		if remaining := time.Until(user.ExpiresAt); remaining > 0 {
			expiresAt = expiresAt.Add(remaining)
		}
	}
	return expiresAt
}

func grantPlan(ctx context.Context, users planUpdater, user *auth.User, planID, period, subID, license string, days int) (time.Time, error) {
	expiresAt := planExpiry(user, period, days)
	return expiresAt, users.UpdatePlan(ctx, user.ID, planID, subID, license, period, expiresAt)
}

func applyCryptoPlan(ctx context.Context, users *auth.Store, user *auth.User, email, eventID, planID, period, via string, days int) {
	expiresAt, err := grantPlan(ctx, users, user, planID, period, "", eventID, days)
	if err != nil {
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

func RenewWalletPlans(ctx context.Context, users *auth.Store) {
	due, err := users.WalletRecurringDue(ctx)
	if err != nil {
		slog.Warn("wallet renew: list due", "err", err)
		return
	}
	for _, u := range due {
		paid, err := BuyPlanFromWallet(ctx, users, u, u.PlanID, u.Recurrence, true, "")
		if err != nil {
			slog.Warn("wallet renew failed", "user", u.ID, "err", err)
		} else if !paid {
			slog.Info("wallet renew: insufficient balance, lapsing", "user", u.ID, "plan", u.PlanID)
		}
	}
}

func BuyPlanFromWallet(ctx context.Context, users planStore, user *auth.User, planID, period string, recurring bool, idempotencyKey string) (bool, error) {
	cents, ok := plans.PriceCents(planID, period, 0)
	if !ok || cents <= 0 {
		return false, nil
	}
	ref := "wallet:" + uuid.NewString()
	if idempotencyKey != "" {
		ref = "wallet:buy:" + idempotencyKey
	}
	subID := ""
	if recurring && period != "lifetime" {
		subID = "wallet:" + user.ID
	}
	expiresAt := planExpiry(user, period, 0)
	paid, applied, err := users.WalletBuyPlan(ctx, user.ID, int64(cents), "plan:"+planID+":"+period, ref, planID, subID, ref, period, expiresAt)
	if err != nil || !paid {
		return paid, err
	}
	if applied && (period == "monthly" || period == "yearly") {
		users.CreditReferral(ctx, user.ID)
	}
	slog.Info("plan bought with wallet", "user", user.ID, "plan", planID, "period", period, "cents", cents, "recurring", recurring)
	return true, nil
}
