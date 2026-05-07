package retirement

import (
	"testing"

	"budget2/internal/models"
)

// F-075: a projection starting in 2033 with the older spouse at age 73
// must NOT trigger RMD that year — SECURE 2.0 raises the start age to 75
// for distributions taken in 2033 or later.
func TestProjection_F075_2033StartAge73NoRMD(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 73
	s.SpouseAge = 0
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 1
	s.StartDate = "2033-01"
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := NewCalculator(s).RunProjection()
	if proj == nil || len(proj.Months) < 12 {
		t.Fatal("nil/short projection")
	}
	for m := 0; m < 12; m++ {
		if proj.Months[m].RMDWithdrawal != 0 {
			t.Errorf("month %d RMDWithdrawal = %.2f; want 0 (age 73 in 2033 → no RMD per SECURE 2.0)",
				m, proj.Months[m].RMDWithdrawal)
		}
	}
}

// F-075: a projection starting in 2033 with the older spouse at age 75
// MUST trigger RMD that year (effective start age is 75 for 2033+).
func TestProjection_F075_2033StartAge75DoesRMD(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 75
	s.SpouseAge = 0
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 1
	s.StartDate = "2033-01"
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := NewCalculator(s).RunProjection()
	if proj == nil || len(proj.Months) < 12 {
		t.Fatal("nil/short projection")
	}
	var total float64
	for m := 0; m < 12; m++ {
		total += proj.Months[m].RMDWithdrawal
	}
	if total <= 0 {
		t.Errorf("annual RMDWithdrawal = %.2f; want > 0 (age 75 in 2033 must trigger RMD)", total)
	}
}

// F-075: pre-2033 scenarios still trigger RMD at age 73 (legacy behavior).
func TestProjection_F075_2026StartAge73DoesRMD(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 73
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 1
	s.StartDate = "2026-01"
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := NewCalculator(s).RunProjection()
	if proj == nil {
		t.Fatal("nil projection")
	}
	var total float64
	for m := 0; m < 12; m++ {
		total += proj.Months[m].RMDWithdrawal
	}
	if total <= 0 {
		t.Errorf("annual RMDWithdrawal = %.2f; want > 0 (pre-2033 age 73 must trigger)", total)
	}
}
