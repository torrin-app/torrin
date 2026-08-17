package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
)

type mockPlanStore struct {
	balance   int64
	granted   bool
	grants    int
	subID     string
	failGrant bool
	seenRefs  map[string]bool
}

func (m *mockPlanStore) WalletBuyPlan(_ context.Context, _ string, cents int64, _, ref, _, subID, _, _ string, _ time.Time) (bool, bool, error) {
	if m.seenRefs[ref] {
		return true, false, nil
	}
	if m.balance < cents {
		return false, false, nil
	}
	if m.failGrant {
		return false, false, errors.New("boom")
	}
	if m.seenRefs == nil {
		m.seenRefs = map[string]bool{}
	}
	m.seenRefs[ref] = true
	m.balance -= cents
	m.granted = true
	m.grants++
	m.subID = subID
	return true, true, nil
}
func (m *mockPlanStore) CreditReferral(context.Context, string) {}

func TestTopupRef(t *testing.T) {
	if _, ok := topupRef("topup:gumroad", ""); ok {
		t.Fatal("empty provider ref must be rejected")
	}
	got, ok := topupRef("topup:gumroad", "SALE123")
	if !ok || got != "topup:gumroad:SALE123" {
		t.Fatalf("got %q ok=%v, want provider-namespaced ref", got, ok)
	}
	if a, _ := topupRef("topup:gumroad", "X"); a == mustRef("topup:bachs", "X") {
		t.Fatal("same id from different providers must not collide")
	}
}

func mustRef(reason, ref string) string {
	r, _ := topupRef(reason, ref)
	return r
}

func TestBuyPlanFromWallet(t *testing.T) {
	ctx := context.Background()
	u := &auth.User{ID: "u"}

	m := &mockPlanStore{balance: 100}
	if ok, _ := BuyPlanFromWallet(ctx, m, u, "standard", "monthly", false, ""); ok || m.granted {
		t.Fatal("insufficient balance must not grant")
	}

	m = &mockPlanStore{balance: 10_000_000}
	ok, err := BuyPlanFromWallet(ctx, m, u, "standard", "monthly", true, "")
	if err != nil || !ok || !m.granted || m.subID != "wallet:u" {
		t.Fatalf("recurring buy: ok=%v granted=%v sub=%q err=%v", ok, m.granted, m.subID, err)
	}

	m = &mockPlanStore{balance: 10_000_000}
	BuyPlanFromWallet(ctx, m, u, "standard", "lifetime", false, "")
	if m.subID != "" {
		t.Fatalf("one-off/lifetime must not set a recurring marker, got %q", m.subID)
	}

	m = &mockPlanStore{balance: 10_000_000, failGrant: true}
	before := m.balance
	if ok, err := BuyPlanFromWallet(ctx, m, u, "standard", "monthly", false, ""); ok || err == nil || m.granted || m.balance != before {
		t.Fatalf("failed tx must leave balance untouched and not grant: ok=%v granted=%v balance=%d(was %d) err=%v", ok, m.granted, m.balance, before, err)
	}

	m = &mockPlanStore{balance: 10_000_000}
	price := m.balance
	BuyPlanFromWallet(ctx, m, u, "standard", "monthly", false, "dup-key")
	afterFirst := m.balance
	ok, err = BuyPlanFromWallet(ctx, m, u, "standard", "monthly", false, "dup-key")
	if err != nil || !ok || m.balance != afterFirst || m.grants != 1 {
		t.Fatalf("double-click with same key must charge/grant once: bal=%d(was %d) grants=%d err=%v", m.balance, price, m.grants, err)
	}
}
