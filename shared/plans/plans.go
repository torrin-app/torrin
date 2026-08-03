package plans

type Plan struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	DayPrices       map[int]int `json:"day_prices"`
	MonthlyPrice    int         `json:"monthly_price_cents"`
	YearlyPrice     int         `json:"yearly_price_cents"`
	LifetimePrice   int         `json:"lifetime_price_cents"`
	MaxConcurrent   int         `json:"max_concurrent_jobs"`
	MaxTorrentBytes int64       `json:"max_torrent_bytes"`
	CachedStreams   int         `json:"cached_streams"`
	Priority        int         `json:"priority"`
	Features        []string    `json:"features"`
	Badge           string      `json:"badge,omitempty"`
	SystemUsenet    bool        `json:"system_usenet"`
}

var (
	Free = Plan{
		ID:              "free",
		Name:            "Free",
		DayPrices:       nil,
		MonthlyPrice:    0,
		YearlyPrice:     0,
		LifetimePrice:   0,
		MaxConcurrent:   1,
		MaxTorrentBytes: 50_000_000_000,
		CachedStreams:   -1,
		Priority:        0,
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
		ID:              "starter",
		Name:            "Starter",
		DayPrices:       map[int]int{7: 149, 15: 249},
		MonthlyPrice:    299,
		YearlyPrice:     2870,
		LifetimePrice:   5382,
		MaxConcurrent:   2,
		MaxTorrentBytes: 100_000_000_000,
		CachedStreams:   -1,
		Priority:        1,
		Features: []string{
			"2 concurrent downloads",
			"100 GB max torrent",
			"Hoster link support",
			"BYOK Usenet",
			"BYOK Real-Debrid",
			"BYOK Premiumize",
			"BYOK TorBox",
			"Library import (RD, AllDebrid, TorBox)",
			"RSS auto-download",
			"WebDAV",
			"Shared cache",
			"Stremio addon",
			"Referral credits",
		},
	}

	Standard = Plan{
		ID:              "standard",
		Name:            "Standard",
		Badge:           "Best value",
		DayPrices:       map[int]int{7: 299, 15: 499},
		MonthlyPrice:    599,
		YearlyPrice:     5750,
		LifetimePrice:   10782,
		MaxConcurrent:   4,
		MaxTorrentBytes: 250_000_000_000,
		CachedStreams:   -1,
		Priority:        2,
		SystemUsenet:    true,
		Features: []string{
			"4 concurrent downloads",
			"250 GB max torrent",
			"Hoster link support",
			"Usenet (included)",
			"BYOK Real-Debrid",
			"BYOK Premiumize",
			"BYOK TorBox",
			"Library import (RD, AllDebrid, TorBox)",
			"RSS auto-download",
			"WebDAV",
			"Shared cache",
			"Stremio addon",
			"Priority queue",
			"Referral credits",
		},
	}

	Pro = Plan{
		ID:              "pro",
		Name:            "Pro",
		DayPrices:       map[int]int{7: 599, 15: 999},
		MonthlyPrice:    1199,
		YearlyPrice:     11500,
		LifetimePrice:   21582,
		MaxConcurrent:   8,
		MaxTorrentBytes: 1_000_000_000_000,
		CachedStreams:   -1,
		Priority:        3,
		SystemUsenet:    true,
		Features: []string{
			"8 concurrent downloads",
			"1 TB max torrent",
			"Hoster link support",
			"Usenet (included)",
			"BYOK Real-Debrid",
			"BYOK Premiumize",
			"BYOK TorBox",
			"Library import (RD, AllDebrid, TorBox)",
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
	case len(permalink) > 3 && permalink[len(permalink)-2:] == "7d":
		return 7
	case len(permalink) > 4 && permalink[len(permalink)-3:] == "15d":
		return 15
	}
	return 0
}

func RecurrenceFromProduct(permalink string) string {
	switch {
	case DaysFromProduct(permalink) > 0:
		return "days"
	case len(permalink) > 7 && permalink[len(permalink)-8:] == "lifetime":
		return "lifetime"
	case len(permalink) > 6 && permalink[len(permalink)-6:] == "yearly":
		return "yearly"
	default:
		return "monthly"
	}
}

func Get(id string) (Plan, bool) {
	p, ok := All[id]
	return p, ok
}

func CanBYOK(planID string) bool {
	return planID != "" && planID != "free"
}

func IndexerAccess(plan Plan, hasOwn, hasSystem bool) string {
	if hasOwn && CanBYOK(plan.ID) {
		return "own"
	}
	if hasSystem && plan.SystemUsenet {
		return "system"
	}
	return ""
}

func PriceCents(planID, billingPeriod string, days int) (int, bool) {
	plan, ok := All[planID]
	if !ok {
		return 0, false
	}
	switch billingPeriod {
	case "lifetime":
		return plan.LifetimePrice, true
	case "yearly":
		return plan.YearlyPrice, true
	case "days":
		p, ok := plan.DayPrices[days]
		return p, ok
	default:
		return plan.MonthlyPrice, true
	}
}

func ValidatePrice(planID, billingPeriod string, priceCents int) bool {
	plan, ok := All[planID]
	if !ok {
		return false
	}
	var expected int
	switch billingPeriod {
	case "lifetime":
		expected = plan.LifetimePrice
	case "yearly":
		expected = plan.YearlyPrice
	case "days":
		for _, p := range plan.DayPrices {
			if priceCents >= p {
				return true
			}
		}
		return false
	default:
		expected = plan.MonthlyPrice
	}
	return priceCents >= expected
}
