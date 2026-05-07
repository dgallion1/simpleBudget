package retirement

import (
	"testing"

	"budget2/internal/models"
)

// fixtureProjection builds a *models.ProjectionResult inline for unit tests
// of BuildRMDAnalysis. Months[i].TaxDeferredBalance is set to taxDeferredFn(i),
// Months[i].RMDWithdrawal to rmdFn(i). depletionMonth is honored when non-nil.
func fixtureProjection(months int, taxDeferredFn func(m int) float64, rmdFn func(m int) float64, depletionMonth *int) *models.ProjectionResult {
	out := &models.ProjectionResult{
		Months:         make([]models.ProjectionMonth, months),
		DepletionMonth: depletionMonth,
		Survives:       depletionMonth == nil,
	}
	for m := 0; m < months; m++ {
		td := 0.0
		rmd := 0.0
		if taxDeferredFn != nil {
			td = taxDeferredFn(m)
		}
		if rmdFn != nil {
			rmd = rmdFn(m)
		}
		out.Months[m] = models.ProjectionMonth{
			Month:              m,
			Year:               float64(m) / 12,
			TaxDeferredBalance: td,
		}
		out.Months[m].RMDWithdrawal = rmd
	}
	return out
}

func newCalcF072(currentAge, spouseAge int, portfolio, tdPercent float64, projYears int, startDate string) *Calculator {
	s := &models.WhatIfSettings{
		CurrentAge:         currentAge,
		SpouseAge:          spouseAge,
		PortfolioValue:     portfolio,
		TaxDeferredPercent: tdPercent,
		ProjectionYears:    projYears,
		StartDate:          startDate,
	}
	return NewCalculator(s)
}

// 1. Depletion before first RMD year → empty Projections, DepletedBeforeRMD true.
func TestBuildRMDAnalysis_F072_DepletionBeforeRMD(t *testing.T) {
	// F-077: StartDate=2019-01 with CurrentAge=60 → birth year 1959 → applicable
	// age 73 (legacy SECURE 2.0 boundary). Preserves the test's original
	// "startsInYears=13" assertion under birth-year semantics.
	calc := newCalcF072(60, 0, 100_000, 60, 30, "2019-01")
	depletion := 24 // month 24 = year 2
	proj := fixtureProjection(360, func(m int) float64 { return 1.0 }, nil, &depletion)

	analysis := calc.BuildRMDAnalysis(proj)

	if !analysis.DepletedBeforeRMD {
		t.Errorf("DepletedBeforeRMD = false; want true")
	}
	if len(analysis.Projections) != 0 {
		t.Errorf("len(Projections) = %d; want 0", len(analysis.Projections))
	}
	if analysis.TotalRMDsOver10Yr != 0 {
		t.Errorf("TotalRMDsOver10Yr = %.2f; want 0", analysis.TotalRMDsOver10Yr)
	}
	if analysis.DepletionYear == nil || *analysis.DepletionYear != 2 {
		t.Errorf("DepletionYear = %v; want 2", analysis.DepletionYear)
	}
	if analysis.DepletionAge == nil || *analysis.DepletionAge != 62 {
		t.Errorf("DepletionAge = %v; want 62", analysis.DepletionAge)
	}
	if analysis.StartsInYears != 13 {
		t.Errorf("StartsInYears = %d; want 13", analysis.StartsInYears)
	}
}

// 2. Depletion during RMD years → only pre-depletion rows emitted.
func TestBuildRMDAnalysis_F072_DepletionDuringRMD(t *testing.T) {
	// olderAge=60, startAge=73, depletion at month 12*15 = year 15
	// → first RMD year is 13 (age 73); fixture supplies non-zero RMD only in
	// years 13 and 14 (2 rows); year 15 hits depletion break.
	// F-077: StartDate=2019-01 → birth 1959 → applicable age 73 preserved.
	calc := newCalcF072(60, 0, 100_000, 60, 30, "2019-01")
	depletion := 12 * 15
	proj := fixtureProjection(360,
		func(m int) float64 { return 60_000 - float64(m)*100 }, // balance trends down
		func(m int) float64 {
			// 1000/mo only during RMD years before depletion
			y := m / 12
			if y >= 13 && y < 15 {
				return 1000
			}
			return 0
		},
		&depletion)

	analysis := calc.BuildRMDAnalysis(proj)

	if analysis.DepletedBeforeRMD {
		t.Errorf("DepletedBeforeRMD = true; want false")
	}
	if len(analysis.Projections) != 2 {
		t.Errorf("len(Projections) = %d; want 2", len(analysis.Projections))
	}
	if analysis.DepletionYear == nil || *analysis.DepletionYear != 15 {
		t.Errorf("DepletionYear = %v; want 15", analysis.DepletionYear)
	}
	wantTotal := 12 * 1000.0 * 2 // 2 years × 12 months × 1000
	if analysis.TotalRMDsOver10Yr != wantTotal {
		t.Errorf("TotalRMDsOver10Yr = %.2f; want %.2f", analysis.TotalRMDsOver10Yr, wantTotal)
	}
	if analysis.Projections[0].Age != 73 {
		t.Errorf("first row age = %d; want 73", analysis.Projections[0].Age)
	}
	if analysis.Projections[1].Age != 74 {
		t.Errorf("second row age = %d; want 74", analysis.Projections[1].Age)
	}
}

// 3. Surviving 30-year projection emits exactly 10 RMD rows in TotalRMDsOver10Yr.
func TestBuildRMDAnalysis_F072_FullTenYears_NoDepletion(t *testing.T) {
	// olderAge=72, startAge=73, projection survives full 30 years.
	// Year 1..29 are RMD years; expect 20 rows (rmdCount cap), 10 in 10-yr total.
	calc := newCalcF072(72, 0, 100_000, 60, 30, "2026-01")
	proj := fixtureProjection(360,
		func(m int) float64 { return 60_000 },
		func(m int) float64 {
			y := m / 12
			if y >= 1 { // RMD starts at year 1 (age 73)
				return 200 // 200/mo => 2400/yr
			}
			return 0
		},
		nil)

	analysis := calc.BuildRMDAnalysis(proj)

	if analysis.DepletedBeforeRMD {
		t.Errorf("DepletedBeforeRMD = true; want false")
	}
	// Years 1..20 = 20 rows (rmdCount cap).
	if len(analysis.Projections) != 20 {
		t.Errorf("len(Projections) = %d; want 20", len(analysis.Projections))
	}
	wantTotal := 2400.0 * 10
	if analysis.TotalRMDsOver10Yr != wantTotal {
		t.Errorf("TotalRMDsOver10Yr = %.2f; want %.2f", analysis.TotalRMDsOver10Yr, wantTotal)
	}
	if analysis.Projections[0].Age != 73 {
		t.Errorf("first row age = %d; want 73", analysis.Projections[0].Age)
	}
	if analysis.Projections[0].RMDAmount != 2400 {
		t.Errorf("first row RMDAmount = %.2f; want 2400", analysis.Projections[0].RMDAmount)
	}
}

// 4. TaxDeferredPercent = 0 → empty projections, no panic.
func TestBuildRMDAnalysis_F072_TaxDeferredPercentZero(t *testing.T) {
	calc := newCalcF072(72, 0, 100_000, 0, 30, "2026-01")
	proj := fixtureProjection(360, nil, nil, nil)

	analysis := calc.BuildRMDAnalysis(proj)

	if analysis.TaxDeferredValue != 0 {
		t.Errorf("TaxDeferredValue = %.2f; want 0", analysis.TaxDeferredValue)
	}
	if len(analysis.Projections) != 0 {
		t.Errorf("len(Projections) = %d; want 0 (no tax-deferred bucket)", len(analysis.Projections))
	}
	if analysis.TotalRMDsOver10Yr != 0 {
		t.Errorf("TotalRMDsOver10Yr = %.2f; want 0", analysis.TotalRMDsOver10Yr)
	}
}

// 5. SECURE 2.0: start year >= 2033 → start age 75.
func TestBuildRMDAnalysis_F072_StartAge75_Secure20(t *testing.T) {
	// olderAge=72, projection start 2034 → effectiveStartAge=75.
	// Expect first emitted row at age 75 (year 3).
	calc := newCalcF072(72, 0, 100_000, 60, 30, "2034-01")
	proj := fixtureProjection(360,
		func(m int) float64 { return 60_000 },
		func(m int) float64 {
			if m/12 >= 3 {
				return 100
			}
			return 0
		},
		nil)

	analysis := calc.BuildRMDAnalysis(proj)

	if analysis.StartAge != 75 {
		t.Errorf("StartAge = %d; want 75", analysis.StartAge)
	}
	if len(analysis.Projections) == 0 {
		t.Fatal("expected projections under SECURE 2.0; got none")
	}
	if analysis.Projections[0].Age != 75 {
		t.Errorf("first row age = %d; want 75", analysis.Projections[0].Age)
	}
}

// 6. olderAge already at RMD age → first row at year 0 uses Months[0] balance.
func TestBuildRMDAnalysis_F072_AlreadyAtRMDAge(t *testing.T) {
	calc := newCalcF072(75, 0, 100_000, 80, 20, "2026-01")
	proj := fixtureProjection(240,
		func(m int) float64 {
			if m == 0 {
				return 80_000 // start-of-year balance
			}
			return 70_000
		},
		func(m int) float64 {
			if m < 12 {
				return 300
			}
			return 0
		},
		nil)

	analysis := calc.BuildRMDAnalysis(proj)

	if analysis.StartsInYears != 0 {
		t.Errorf("StartsInYears = %d; want 0", analysis.StartsInYears)
	}
	if len(analysis.Projections) == 0 {
		t.Fatal("expected at least one projection row")
	}
	if analysis.Projections[0].Age != 75 {
		t.Errorf("first row age = %d; want 75", analysis.Projections[0].Age)
	}
	// Year 0 should sample Months[0].TaxDeferredBalance == 80000.
	if analysis.Projections[0].TaxDeferredBal != 80_000 {
		t.Errorf("year-0 TaxDeferredBal = %.2f; want 80000", analysis.Projections[0].TaxDeferredBal)
	}
	wantRMD := 12 * 300.0
	if analysis.Projections[0].RMDAmount != wantRMD {
		t.Errorf("year-0 RMDAmount = %.2f; want %.2f", analysis.Projections[0].RMDAmount, wantRMD)
	}
}

// 7. RMDPercent reports IRS table value, not realized percent.
func TestBuildRMDAnalysis_F072_RMDPercentIsTableValue(t *testing.T) {
	// olderAge=73, balance 100k, but actual RMD is only 1000 (well below table %).
	// Table for age 73 → factor 26.5 → percent = 100/26.5 ≈ 3.7736.
	calc := newCalcF072(73, 0, 100_000, 60, 5, "2026-01")
	proj := fixtureProjection(60,
		func(m int) float64 { return 60_000 },
		func(m int) float64 {
			if m < 12 {
				return 1000.0 / 12 // 1000 total for the year
			}
			return 0
		},
		nil)

	analysis := calc.BuildRMDAnalysis(proj)

	if len(analysis.Projections) == 0 {
		t.Fatal("expected projections")
	}
	wantPercent := 100.0 / 26.5
	if got := analysis.Projections[0].RMDPercent; (got-wantPercent) > 0.001 || (wantPercent-got) > 0.001 {
		t.Errorf("RMDPercent = %.4f; want %.4f (table value, not realized)", got, wantPercent)
	}
	// Sanity: realized RMD is much smaller than table% × balance.
	if analysis.Projections[0].RMDAmount > 1001 {
		t.Errorf("RMDAmount = %.2f; want ~1000 (the fixture value)", analysis.Projections[0].RMDAmount)
	}
}

// 8. Depletion exactly at the first RMD year → DepletedBeforeRMD true.
//
// Boundary case caught in F-072 final review: when dy == startsInYears,
// no row can be emitted (depletion break at y == dy excludes the depletion
// year), and the banner must still fire so the user sees the depletion
// context instead of the generic empty-state.
func TestBuildRMDAnalysis_F072_DepletionAtFirstRMDYear(t *testing.T) {
	// olderAge=65, startAge=73, startsInYears=8
	// depletion at month 96 → dy=8 (exactly first RMD year)
	calc := newCalcF072(65, 0, 100_000, 60, 30, "2026-01")
	depletion := 96
	proj := fixtureProjection(360, func(m int) float64 { return 1.0 }, nil, &depletion)

	analysis := calc.BuildRMDAnalysis(proj)

	if !analysis.DepletedBeforeRMD {
		t.Errorf("DepletedBeforeRMD = false; want true (depletion at exactly first RMD year)")
	}
	if len(analysis.Projections) != 0 {
		t.Errorf("len(Projections) = %d; want 0", len(analysis.Projections))
	}
	if analysis.DepletionYear == nil || *analysis.DepletionYear != 8 {
		t.Errorf("DepletionYear = %v; want 8", analysis.DepletionYear)
	}
	if analysis.DepletionAge == nil || *analysis.DepletionAge != 73 {
		t.Errorf("DepletionAge = %v; want 73", analysis.DepletionAge)
	}
}
