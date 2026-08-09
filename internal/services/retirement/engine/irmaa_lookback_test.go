package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// irmaaLookbackScenario is a Medicare-eligible household with no income, no
// expenses and no dividends, so the only thing that can move MAGI — and
// therefore IRMAA — is the Roth conversion the caller adds.
func irmaaLookbackScenario() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{{
		ID:         "primary",
		Name:       "You",
		BirthMonth: models.BirthMonthForAge("2026-01", 66),
		Role:       models.PersonRolePrimary,
	}}
	// Tax-deferred is sized to exactly the conversion amount below, so the
	// conversion drains it in year 0 and RothConversionAmountForYear returns 0
	// for every later year (it is capped at the available balance). That is the
	// only way to express a one-shot conversion through settings that survive
	// prepare.DeepCopy — PerYearOverrides is json:"-" and does not.
	s.PortfolioValue = 2_000_000
	s.TaxDeferredPercent = 20 // 400,000 tax-deferred
	s.RothPercent = 0
	s.MonthlyLivingExpenses = 0
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.ExpenseSources = nil
	s.IncomeSources = nil
	s.ProjectionYears = 4
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.Guardrails = nil
	s.SocialSecurity = nil
	s.TaxableDividendYield = 0
	s.TaxableQualifiedDividendPercent = 0
	s.TaxableCapitalGainsDistributionRate = 0
	return s
}

// TestYear0MAGIIsSurchargedOnceNotThreeTimes is the falsification test for
// audit finding F-2. IRMAA uses a two-year MAGI lookback, so a Roth conversion
// in projection year 0 raises the premium in projection year 2 and nowhere
// else.
//
// The engine seeds the lookback with &st.AssumedLookbackMAGI (stepper.go:311),
// which resolveIRMAALookbackMAGI always accepts, and then overwrites that seed
// with year 0's own MAGI throughout year 0 (stepper.go:321-323). That makes the
// conversion surcharge year 0 (from month 1 on), year 1 (December of year 0
// carried forward), and year 2 (real history) — three times for one event.
func TestYear0MAGIIsSurchargedOnceNotThreeTimes(t *testing.T) {
	baseline := irmaaLookbackScenario()

	converted := irmaaLookbackScenario()
	converted.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		StartYear:    0,
		AnnualAmount: 400_000,
	}

	baseProj := New().Run(Input{Prepared: prepare.MustFrom(t, baseline)})
	convProj := New().Run(Input{Prepared: prepare.MustFrom(t, converted)})

	if len(baseProj.YearlySummaries) != 4 || len(convProj.YearlySummaries) != 4 {
		t.Fatalf("yearly summaries: baseline=%d converted=%d, want 4 each",
			len(baseProj.YearlySummaries), len(convProj.YearlySummaries))
	}
	if convProj.Months[0].RothConversions <= 0 {
		t.Fatal("scenario produced no year-0 Roth conversion; it cannot exercise F-2")
	}

	// TotalExpenses carries the IRMAA actually charged (stepper.go:329), and
	// every other expense is identical between the two runs, so the delta is
	// IRMAA dollars paid.
	surcharge := make([]float64, 4)
	for y := range surcharge {
		surcharge[y] = convProj.YearlySummaries[y].Expenses - baseProj.YearlySummaries[y].Expenses
	}

	t.Logf("IRMAA surcharge attributable to the year-0 conversion, by projection year: "+
		"y0=%.2f y1=%.2f y2=%.2f y3=%.2f", surcharge[0], surcharge[1], surcharge[2], surcharge[3])

	for _, y := range []int{0, 1, 3} {
		if math.Abs(surcharge[y]) > 1 {
			t.Errorf("year %d was surcharged %.2f for a year-0 conversion; the two-year lookback puts it in year 2 only", y, surcharge[y])
		}
	}
	if surcharge[2] <= 1 {
		t.Errorf("year 2 surcharge = %.2f; want the year-0 conversion to raise the premium there", surcharge[2])
	}
}
