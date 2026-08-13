package plans

import "testing"

func TestProductSuffixParsing(t *testing.T) {
	days := map[string]int{"torrinpro7d": 7, "torrinstd15d": 15, "torrinpro": 0, "torrinprolifetime": 0}
	for in, want := range days {
		if got := DaysFromProduct(in); got != want {
			t.Errorf("DaysFromProduct(%q)=%d want %d", in, got, want)
		}
	}
	rec := map[string]string{
		"torrinpro7d":       "days",
		"torrinstd15d":      "days",
		"torrinprolifetime": "lifetime",
		"torrinproyearly":   "yearly",
		"torrinpromonthly":  "monthly",
		"torrinpro":         "monthly",
	}
	for in, want := range rec {
		if got := RecurrenceFromProduct(in); got != want {
			t.Errorf("RecurrenceFromProduct(%q)=%q want %q", in, got, want)
		}
	}
}
