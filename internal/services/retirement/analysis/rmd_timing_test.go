package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// F-074: RMDTriggerMonth maps each timing to a single month-of-year.
func TestRMDTriggerMonth_F074_AllTimings(t *testing.T) {
	cases := []struct {
		timing models.RMDTiming
		want   int
	}{
		{models.RMDTimingStartOfYear, 0},
		{models.RMDTimingMidYear, 6},
		{models.RMDTimingEndOfYear, 11},
		{models.RMDTiming(""), 6}, // empty → mid-year (matches NormalizeRMDTiming default)
	}
	for _, c := range cases {
		if got := engine.RMDTriggerMonth(c.timing); got != c.want {
			t.Errorf("RMDTriggerMonth(%q) = %d; want %d", c.timing, got, c.want)
		}
	}
}

// runRMDTimingProjection builds a minimal-noise 1-year retiree-only
// projection suitable for asserting RMD-timing effects: $1M
// tax-deferred, no income, no expenses (incl. healthcare), 7% return,
// 0% inflation, age 73 (RMD active).
func runRMDTimingProjection(t *testing.T, timing models.RMDTiming) *models.ProjectionResult {
	t.Helper()
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 73
	s.SpouseAge = 0
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	// Taxable = 100 - 100 - 0 = 0 (computed)
	s.MonthlyLivingExpenses = 0
	s.MonthlyHealthcare = 0
	s.HealthcareStartYears = 0
	s.HealthcarePersons = nil
	s.IncomeSources = []models.IncomeSource{}
	s.ExpenseSources = []models.ExpenseSource{}
	s.InvestmentReturn = 7
	s.InflationRate = 0
	s.HealthcareInflation = 0
	s.SpendingDeclineRate = 0
	s.ProjectionYears = 1
	s.RMDTiming = timing
	s.StartDate = "2026-01"
	// Re-anchor primary person's birth month to age 73 at start.
	if len(s.Persons) > 0 {
		s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 73)
	}
	prepare.ComputeAges(s)
	return engine.New().Run(engine.Input{Prepared: prepare.MustFrom(t, s)})
}

// F-074: end_of_year timing produces a higher year-end portfolio than
// start_of_year because tax-deferred grows on the full balance for 11
// months before the RMD haircut. Run a 1-year projection with no other
// expenses/income, age 73 (RMD active).
func TestProjection_F074_TimingAffectsYearEndBalance(t *testing.T) {
	startProj := runRMDTimingProjection(t, models.RMDTimingStartOfYear)
	midProj := runRMDTimingProjection(t, models.RMDTimingMidYear)
	endProj := runRMDTimingProjection(t, models.RMDTimingEndOfYear)

	if startProj == nil || midProj == nil || endProj == nil {
		t.Fatal("nil projection")
	}

	// month 11 = December of year 1
	startEOY := startProj.Months[11].PortfolioBalance
	midEOY := midProj.Months[11].PortfolioBalance
	endEOY := endProj.Months[11].PortfolioBalance

	if !(endEOY > midEOY && midEOY > startEOY) {
		t.Errorf("expected end_of_year > mid_year > start_of_year; got start=%.2f mid=%.2f end=%.2f",
			startEOY, midEOY, endEOY)
	}

	// All three must withdraw the same total annual RMD (year-start balance ÷ 26.5 at age 73).
	annualRMD, _ := engine.CalculateRMD(1_000_000, 73)
	for _, c := range []struct {
		name string
		proj *models.ProjectionResult
	}{
		{"start_of_year", startProj},
		{"mid_year", midProj},
		{"end_of_year", endProj},
	} {
		var total float64
		for m := 0; m < 12; m++ {
			total += c.proj.Months[m].RMDWithdrawal
		}
		if math.Abs(total-annualRMD) > 1.0 { // ±$1 for float
			t.Errorf("%s: sum(RMDWithdrawal) over year = %.2f; want %.2f", c.name, total, annualRMD)
		}
	}
}

// F-074: each timing pins the withdrawal to exactly one month within the year.
func TestProjection_F074_TriggerMonthIsExclusive(t *testing.T) {
	cases := []struct {
		timing  models.RMDTiming
		trigger int
	}{
		{models.RMDTimingStartOfYear, 0},
		{models.RMDTimingMidYear, 6},
		{models.RMDTimingEndOfYear, 11},
	}
	for _, c := range cases {
		proj := runRMDTimingProjection(t, c.timing)
		if proj == nil {
			t.Fatalf("%s: nil projection", c.timing)
		}
		for m := 0; m < 12; m++ {
			rmd := proj.Months[m].RMDWithdrawal
			if m == c.trigger {
				if rmd <= 0 {
					t.Errorf("%s: month %d (trigger) RMDWithdrawal = %.2f; want > 0", c.timing, m, rmd)
				}
			} else if rmd != 0 {
				t.Errorf("%s: month %d RMDWithdrawal = %.2f; want 0 (only trigger month %d should withdraw)",
					c.timing, m, rmd, c.trigger)
			}
		}
	}
}
