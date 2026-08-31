package plans

import (
	"testing"
	"time"
)

func TestPriceCentsAt(t *testing.T) {
	before := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	after := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)

	if c, _ := PriceCentsAt("standard", "lifetime", 0, before); c != 10782 {
		t.Errorf("pre-bump standard lifetime = %d, want 10782", c)
	}
	if c, _ := PriceCentsAt("standard", "lifetime", 0, after); c != 16900 {
		t.Errorf("post-bump standard lifetime = %d, want 16900", c)
	}
	if c, _ := PriceCentsAt("pro", "lifetime", 0, before); c != 21582 {
		t.Errorf("pre-bump pro lifetime = %d, want 21582", c)
	}
	monthly, _ := PriceCents("standard", "monthly", 0)
	if c, _ := PriceCentsAt("standard", "monthly", 0, before); c != monthly {
		t.Errorf("monthly is date-independent: got %d, want %d", c, monthly)
	}
}

func TestColdPullsPerHour(t *testing.T) {
	free, _ := Get("free")
	starter, _ := Get("starter")
	standard, _ := Get("standard")
	pro, _ := Get("pro")
	if free.ColdPullsPerHour <= 0 {
		t.Error("free must have a positive cold-pull cap")
	}
	if !(free.ColdPullsPerHour < starter.ColdPullsPerHour &&
		starter.ColdPullsPerHour < standard.ColdPullsPerHour &&
		standard.ColdPullsPerHour < pro.ColdPullsPerHour) {
		t.Errorf("cold-pull caps must increase by tier: free=%d starter=%d standard=%d pro=%d",
			free.ColdPullsPerHour, starter.ColdPullsPerHour, standard.ColdPullsPerHour, pro.ColdPullsPerHour)
	}
}

func TestMonthlyIngestBytes(t *testing.T) {
	want := map[string]int64{
		"free":     500_000_000_000,
		"starter":  2_000_000_000_000,
		"standard": 4_000_000_000_000,
		"pro":      8_000_000_000_000,
	}
	for id, w := range want {
		p, _ := Get(id)
		if p.MonthlyIngestBytes != w {
			t.Errorf("%s monthly ingest cap = %d, want %d", id, p.MonthlyIngestBytes, w)
		}
	}
	if !(Free.MonthlyIngestBytes < Starter.MonthlyIngestBytes &&
		Starter.MonthlyIngestBytes < Standard.MonthlyIngestBytes &&
		Standard.MonthlyIngestBytes < Pro.MonthlyIngestBytes) {
		t.Error("ingest caps must increase by tier")
	}
}

func TestRSSEntitlement(t *testing.T) {
	if Free.RSS || Starter.RSS {
		t.Error("free and starter must not have RSS auto-download")
	}
	if !Standard.RSS || !Pro.RSS {
		t.Error("standard and pro must have RSS auto-download")
	}
}

func TestListedStripsDayPricesWhenRetired(t *testing.T) {
	defer func() { DayPlansEnabled = true }()

	DayPlansEnabled = false
	for id, p := range Listed() {
		if p.DayPrices != nil {
			t.Errorf("%s: day prices should be nil when retired", id)
		}
	}
	if All["starter"].DayPrices == nil {
		t.Error("Listed must not mutate All")
	}

	DayPlansEnabled = true
	if len(Listed()["starter"].DayPrices) == 0 {
		t.Error("day prices should be present when enabled")
	}
}

func TestIndexerPolicy(t *testing.T) {
	cases := []struct {
		name                string
		plan                Plan
		toggle              bool
		wantBYO, wantSystem bool
	}{
		{"free never", Free, true, false, false},
		{"starter byo only", Starter, true, true, false},
		{"standard both on", Standard, true, true, true},
		{"standard system off", Standard, false, true, false},
		{"pro both on", Pro, true, true, true},
		{"pro system off", Pro, false, true, false},
	}
	for _, c := range cases {
		byo, sys := IndexerPolicy(c.plan, c.toggle)
		if byo != c.wantBYO || sys != c.wantSystem {
			t.Errorf("%s: got byo=%v system=%v want byo=%v system=%v", c.name, byo, sys, c.wantBYO, c.wantSystem)
		}
	}
}

func TestGet(t *testing.T) {
	p, ok := Get("pro")
	if !ok || p.MaxConcurrent != 8 || p.MaxTorrentBytes != 1_000_000_000_000 {
		t.Fatalf("pro plan wrong: %+v", p)
	}
	if _, ok := Get("nope"); ok {
		t.Error("unknown plan should not resolve")
	}
}

func TestCanBYOK(t *testing.T) {
	if CanBYOK("free") || CanBYOK("") {
		t.Error("free/empty must not allow BYOK")
	}
	for _, id := range []string{"starter", "standard", "pro"} {
		if !CanBYOK(id) {
			t.Errorf("%s should allow BYOK", id)
		}
	}
}

func TestValidatePrice(t *testing.T) {
	if !ValidatePrice("standard", "monthly", 599) {
		t.Error("exact monthly price should pass")
	}
	if ValidatePrice("standard", "monthly", 100) {
		t.Error("underpayment should fail")
	}
	if !ValidatePrice("pro", "yearly", 12000) {
		t.Error("overpayment should pass")
	}
	if !ValidatePrice("starter", "days", 149) {
		t.Error("valid day price should pass")
	}
}

func TestCairnListedForPaidPlans(t *testing.T) {
	has := func(feats []string, want string) bool {
		for _, f := range feats {
			if f == want {
				return true
			}
		}
		return false
	}
	for _, id := range []string{"starter", "standard", "pro"} {
		p, _ := Get(id)
		if !has(p.Features, "Cairn permanent archive") {
			t.Errorf("%s should list 'Cairn permanent archive'", id)
		}
		if has(p.Features, "Cairn archive (restore needs Standard)") {
			t.Errorf("%s must not gate cairn restore behind Standard", id)
		}
	}
	if p, _ := Get("free"); has(p.Features, "Cairn permanent archive") {
		t.Error("free should not list Cairn")
	}
}
