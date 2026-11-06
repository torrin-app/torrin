package plans

import "strings"

type Plan struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	DayPrices          map[int]int `json:"day_prices"`
	MonthlyPrice       int         `json:"monthly_price_cents"`
	MonthlyFullPrice   int         `json:"monthly_full_price_cents,omitempty"`
	YearlyPrice        int         `json:"yearly_price_cents"`
	YearlyFullPrice    int         `json:"yearly_full_price_cents,omitempty"`
	LifetimePrice      int         `json:"lifetime_price_cents"`
	LifetimeFullPrice  int         `json:"lifetime_full_price_cents,omitempty"`
	MaxConcurrent      int         `json:"max_concurrent_jobs"`
	ColdPullsPerHour   int         `json:"cold_pulls_per_hour"`
	MaxDownloadMbps    int         `json:"max_download_mbps"`
	MaxTorrentBytes    int64       `json:"max_torrent_bytes"`
	MonthlyIngestBytes int64       `json:"monthly_ingest_bytes"`
	CachedStreams      int         `json:"cached_streams"`
	SeedSlots          int         `json:"seed_slots"`
	Priority           int         `json:"priority"`
	Features           []string    `json:"features"`
	Badge              string      `json:"badge,omitempty"`
	SystemUsenet       bool        `json:"system_usenet"`
	RSS                bool        `json:"rss"`
}

var (
	Free = Plan{
		ID:                 "free",
		Name:               "Free",
		DayPrices:          nil,
		MonthlyPrice:       0,
		YearlyPrice:        0,
		LifetimePrice:      0,
		MaxConcurrent:      1,
		ColdPullsPerHour:   15,
		MaxDownloadMbps:    250,
		MaxTorrentBytes:    50_000_000_000,
		MonthlyIngestBytes: 500_000_000_000,
		CachedStreams:      -1,
		Priority:           0,
		Features: []string{
			"1 concurrent download",
			"50 GB max torrent",
			"3-day trial",
			"WebDAV",
			"Shared cache",
			"Stremio addon",
			"Referral credits",
		},
	}

	Starter = Plan{
		ID:                 "starter",
		Name:               "Starter",
		DayPrices:          map[int]int{7: 149, 15: 249},
		MonthlyPrice:       299,
		YearlyPrice:        2870,
		LifetimePrice:      8900,
		MaxConcurrent:      2,
		ColdPullsPerHour:   40,
		MaxDownloadMbps:    500,
		MaxTorrentBytes:    100_000_000_000,
		MonthlyIngestBytes: 2_000_000_000_000,
		CachedStreams:      -1,
		SeedSlots:          3,
		Priority:           1,
		Features: []string{
			"2 concurrent downloads",
			"100 GB max torrent",
			"Cairn permanent archive",
			"Hoster link support",
			"Private-tracker seeding",
			"BYOK Usenet",
			"BYOK Real-Debrid",
			"BYOK Premiumize",
			"BYOK TorBox",
			"Library import (RD, AllDebrid, TorBox, Offcloud)",
			"WebDAV",
			"Shared cache",
			"Stremio addon",
			"Referral credits",
		},
	}

	Standard = Plan{
		ID:                 "standard",
		Name:               "Standard",
		Badge:              "Best value",
		DayPrices:          map[int]int{7: 299, 15: 499},
		MonthlyPrice:       599,
		YearlyPrice:        5750,
		LifetimePrice:      16900,
		MaxConcurrent:      4,
		ColdPullsPerHour:   80,
		MaxDownloadMbps:    1000,
		MaxTorrentBytes:    250_000_000_000,
		MonthlyIngestBytes: 4_000_000_000_000,
		CachedStreams:      -1,
		SeedSlots:          5,
		Priority:           2,
		SystemUsenet:       true,
		RSS:                true,
		Features: []string{
			"4 concurrent downloads",
			"250 GB max torrent",
			"Cairn permanent archive",
			"Hoster link support",
			"Private-tracker seeding",
			"Usenet (included)",
			"BYOK Real-Debrid",
			"BYOK Premiumize",
			"BYOK TorBox",
			"Library import (RD, AllDebrid, TorBox, Offcloud)",
			"RSS auto-download",
			"WebDAV",
			"Shared cache",
			"Stremio addon",
			"Priority queue",
			"Referral credits",
		},
	}

	Pro = Plan{
		ID:                 "pro",
		Name:               "Pro",
		DayPrices:          map[int]int{7: 599, 15: 999},
		MonthlyPrice:       1199,
		YearlyPrice:        11500,
		LifetimePrice:      32900,
		MaxConcurrent:      8,
		ColdPullsPerHour:   200,
		MaxDownloadMbps:    2500,
		MaxTorrentBytes:    1_000_000_000_000,
		MonthlyIngestBytes: 8_000_000_000_000,
		CachedStreams:      -1,
		SeedSlots:          10,
		Priority:           3,
		SystemUsenet:       true,
		RSS:                true,
		Features: []string{
			"8 concurrent downloads",
			"1 TB max torrent",
			"Cairn permanent archive",
			"Hoster link support",
			"Private-tracker seeding",
			"Usenet (included)",
			"BYOK Real-Debrid",
			"BYOK Premiumize",
			"BYOK TorBox",
			"Library import (RD, AllDebrid, TorBox, Offcloud)",
			"RSS auto-download",
			"WebDAV",
			"Shared cache",
			"Stremio addon",
			"Priority queue",
			"Referral credits",
		},
	}

	All = map[string]Plan{
		"free":     Free,
		"starter":  Starter,
		"standard": Standard,
		"pro":      Pro,
	}
)

const SeedSlotProduct = "torrin-seed-slots"
const TopupProduct = "torrin-topup"

var ByGumroadProduct = map[string]string{
	// Day plans
	"torrin-starter-7d":   "starter",
	"torrin-starter-15d":  "starter",
	"torrin-standard-7d":  "standard",
	"torrin-standard-15d": "standard",
	"torrin-pro-7d":       "pro",
	"torrin-pro-15d":      "pro",
	// Monthly
	"torrin-starter-monthly":  "starter",
	"torrin-standard-monthly": "standard",
	"torrin-pro-monthly":      "pro",
	// Yearly
	"torrin-starter-yearly":  "starter",
	"torrin-standard-yearly": "standard",
	"torrin-pro-yearly":      "pro",
	// Lifetime
	"torrin-starter-lifetime":  "starter",
	"torrin-standard-lifetime": "standard",
	"torrin-pro-lifetime":      "pro",
}

func DaysFromProduct(permalink string) int {
	switch {
	case strings.HasSuffix(permalink, "7d"):
		return 7
	case strings.HasSuffix(permalink, "15d"):
		return 15
	}
	return 0
}

func RecurrenceFromProduct(permalink string) string {
	switch {
	case DaysFromProduct(permalink) > 0:
		return "days"
	case strings.HasSuffix(permalink, "lifetime"):
		return "lifetime"
	case strings.HasSuffix(permalink, "yearly"):
		return "yearly"
	default:
		return "monthly"
	}
}

func Get(id string) (Plan, bool) {
	p, ok := All[id]
	return p, ok
}

var DayPlansEnabled = true

func Listed() map[string]Plan {
	out := make(map[string]Plan, len(All))
	for id, p := range All {
		if !DayPlansEnabled {
			p.DayPrices = nil
		}
		if BlackFridayActive {
			if bf, ok := blackFridayMonthly[id]; ok && p.MonthlyPrice > 0 {
				p.MonthlyFullPrice, p.MonthlyPrice = p.MonthlyPrice, bf
			}
			if bf, ok := blackFridayYearly[id]; ok && p.YearlyPrice > 0 {
				p.YearlyFullPrice, p.YearlyPrice = p.YearlyPrice, bf
			}
			if bf, ok := blackFridayLifetime[id]; ok && p.LifetimePrice > 0 {
				p.LifetimeFullPrice, p.LifetimePrice = p.LifetimePrice, bf
			}
		}
		if !LifetimeEnabled {
			p.LifetimePrice = 0
			p.LifetimeFullPrice = 0
		}
		out[id] = p
	}
	return out
}

func CanBYOK(planID string) bool {
	return planID != "" && planID != "free"
}

func IndexerPolicy(plan Plan, systemToggleOn bool) (allowBYO, allowSystem bool) {
	return CanBYOK(plan.ID), plan.SystemUsenet && systemToggleOn
}
