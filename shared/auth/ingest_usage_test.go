package auth

import (
	"context"
	"testing"
)

func TestMonthlyQuotaExceededGuards(t *testing.T) {
	ctx := context.Background()
	s := &Store{}
	orig := QuotaEnforceMonth
	defer func() { QuotaEnforceMonth = orig }()

	QuotaEnforceMonth = "2099-01"
	if over, err := s.MonthlyQuotaExceeded(ctx, "u", 0); over || err != nil {
		t.Errorf("unlimited plan (cap<=0) must never be over: over=%v err=%v", over, err)
	}

	QuotaEnforceMonth = ""
	if over, err := s.MonthlyQuotaExceeded(ctx, "u", 1); over || err != nil {
		t.Errorf("empty enforce-month must not enforce: over=%v err=%v", over, err)
	}

	QuotaEnforceMonth = "2099-01"
	if over, err := s.MonthlyQuotaExceeded(ctx, "u", 1); over || err != nil {
		t.Errorf("future enforce-month must not enforce yet: over=%v err=%v", over, err)
	}
}
