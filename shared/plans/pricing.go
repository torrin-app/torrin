package plans

import "time"

var (
	LifetimeEnabled   = true
	BlackFridayActive = false
)

var blackFridayMonthly = map[string]int{
	"starter":  224,
	"standard": 449,
	"pro":      899,
}

var blackFridayYearly = map[string]int{
	"starter":  2149,
	"standard": 4299,
	"pro":      8599,
}

var blackFridayLifetime = map[string]int{
	"starter":  5900,
	"standard": 10900,
	"pro":      21500,
}

var lifetimeBumpDate = time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)

var preBumpLifetime = map[string]int{
	"starter":  5382,
	"standard": 10782,
	"pro":      21582,
}

func PriceCents(planID, billingPeriod string, days int) (int, bool) {
	plan, ok := All[planID]
	if !ok {
		return 0, false
	}
	switch billingPeriod {
	case "lifetime":
		if BlackFridayActive {
			if p, ok := blackFridayLifetime[planID]; ok {
				return p, true
			}
		}
		return plan.LifetimePrice, true
	case "yearly":
		if BlackFridayActive {
			if p, ok := blackFridayYearly[planID]; ok {
				return p, true
			}
		}
		return plan.YearlyPrice, true
	case "days":
		p, ok := plan.DayPrices[days]
		return p, ok
	default:
		if BlackFridayActive {
			if p, ok := blackFridayMonthly[planID]; ok {
				return p, true
			}
		}
		return plan.MonthlyPrice, true
	}
}

func PriceCentsAt(planID, billingPeriod string, days int, when time.Time) (int, bool) {
	if billingPeriod == "lifetime" && when.Before(lifetimeBumpDate) {
		if p, ok := preBumpLifetime[planID]; ok {
			return p, true
		}
	}
	return PriceCents(planID, billingPeriod, days)
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
