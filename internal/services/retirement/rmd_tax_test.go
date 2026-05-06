package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// F-032 tests — EffectiveRMDStartAge

func TestEffectiveRMDStartAge_F032_Pre2033(t *testing.T) {
	s := &models.WhatIfSettings{
		StartDate: "2026-01",
	}
	if got := EffectiveRMDStartAge(s); got != 73 {
		t.Errorf("pre-2033 start age = %d; want 73", got)
	}
}

func TestEffectiveRMDStartAge_F032_PostJan2033(t *testing.T) {
	s := &models.WhatIfSettings{
		StartDate: "2033-01",
	}
	if got := EffectiveRMDStartAge(s); got != 75 {
		t.Errorf("2033 start age = %d; want 75", got)
	}
}

func TestEffectiveRMDStartAge_F032_Post2033(t *testing.T) {
	s := &models.WhatIfSettings{
		StartDate: "2040-06",
	}
	if got := EffectiveRMDStartAge(s); got != 75 {
		t.Errorf("2040 start age = %d; want 75", got)
	}
}

func TestEffectiveRMDStartAge_F032_NilSafe(t *testing.T) {
	if got := EffectiveRMDStartAge(nil); got != 73 {
		t.Errorf("nil settings start age = %d; want 73", got)
	}
}

func TestEffectiveRMDStartAge_F032_ExactBoundary2032(t *testing.T) {
	s := &models.WhatIfSettings{
		StartDate: "2032-12",
	}
	if got := EffectiveRMDStartAge(s); got != 73 {
		t.Errorf("Dec 2032 start age = %d; want 73", got)
	}
}

func TestCalculateRMDAnalysis(t *testing.T) {
	t.Run("age 65 with 8 years until RMD", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 65
		s.SpouseAge = 0
		s.PortfolioValue = 1_000_000
		s.TaxDeferredPercent = 60
		s.InvestmentReturn = 6.0
		s.ProjectionYears = 30
		// Pin to start-of-year to match original test assertions (which predate F-035).
		s.RMDTiming = models.RMDTimingStartOfYear

		c := NewCalculator(s)
		result := c.CalculateRMDAnalysis()

		if result.StartsInYears != 8 {
			t.Errorf("StartsInYears = %d, want 8", result.StartsInYears)
		}
		if result.StartAge != RMDStartAge {
			t.Errorf("StartAge = %d, want %d", result.StartAge, RMDStartAge)
		}
		if result.CurrentAge != 65 {
			t.Errorf("CurrentAge = %d, want 65", result.CurrentAge)
		}
		expectedTDValue := 1_000_000 * 0.60
		if math.Abs(result.TaxDeferredValue-expectedTDValue) > 0.01 {
			t.Errorf("TaxDeferredValue = %.2f, want %.2f", result.TaxDeferredValue, expectedTDValue)
		}
		if len(result.Projections) == 0 {
			t.Fatal("expected projections, got none")
		}
		if result.Projections[0].Age != 73 {
			t.Errorf("first projection age = %d, want 73", result.Projections[0].Age)
		}
		expectedBalanceAt73 := expectedTDValue * math.Pow(1.06, 8)
		if math.Abs(result.Projections[0].TaxDeferredBal-expectedBalanceAt73) > 0.01 {
			t.Errorf("balance at 73 = %.2f, want %.2f", result.Projections[0].TaxDeferredBal, expectedBalanceAt73)
		}
		if result.TotalRMDsOver10Yr <= 0 {
			t.Error("TotalRMDsOver10Yr should be positive")
		}
		// Verify projections have valid RMD data
		for _, p := range result.Projections {
			if p.Age < RMDStartAge {
				t.Errorf("projection at age %d should not exist (below RMD start)", p.Age)
			}
			if p.RMDAmount <= 0 {
				t.Errorf("RMDAmount at age %d should be positive, got %.2f", p.Age, p.RMDAmount)
			}
			if p.LifeExpFactor <= 0 {
				t.Errorf("LifeExpFactor at age %d should be positive", p.Age)
			}
		}
	})

	t.Run("age 75 already past RMD start", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 75
		s.SpouseAge = 0
		s.PortfolioValue = 500_000
		s.TaxDeferredPercent = 80
		s.InvestmentReturn = 5.0
		s.ProjectionYears = 20

		c := NewCalculator(s)
		result := c.CalculateRMDAnalysis()

		if result.StartsInYears != 0 {
			t.Errorf("StartsInYears = %d, want 0", result.StartsInYears)
		}
		if result.CurrentAge != 75 {
			t.Errorf("CurrentAge = %d, want 75", result.CurrentAge)
		}
		if len(result.Projections) == 0 {
			t.Fatal("expected projections, got none")
		}
		if result.Projections[0].Age != 75 {
			t.Errorf("first projection age = %d, want 75", result.Projections[0].Age)
		}
	})

	t.Run("with spouse older age used", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 60
		s.SpouseAge = 70
		s.PortfolioValue = 800_000
		s.TaxDeferredPercent = 50
		s.InvestmentReturn = 7.0
		s.ProjectionYears = 30

		c := NewCalculator(s)
		result := c.CalculateRMDAnalysis()

		// Older age is 70, so 3 years until RMD
		if result.StartsInYears != 3 {
			t.Errorf("StartsInYears = %d, want 3", result.StartsInYears)
		}
		if result.CurrentAge != 70 {
			t.Errorf("CurrentAge = %d, want 70 (older spouse)", result.CurrentAge)
		}
	})

	t.Run("InvestmentReturn zero uses allocation-based return", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 72
		s.SpouseAge = 0
		s.PortfolioValue = 1_000_000
		s.TaxDeferredPercent = 60
		s.InvestmentReturn = 0 // triggers GetExpectedReturnFromAllocation
		s.StockPercent = 60
		s.CashPercent = 0
		s.ProjectionYears = 10

		c := NewCalculator(s)
		result := c.CalculateRMDAnalysis()

		if len(result.Projections) == 0 {
			t.Fatal("expected projections")
		}
		// With allocation-based return, balance should still grow
		// First projection is at age 73 (year 1), balance should reflect growth
		initialTD := 1_000_000 * 0.60
		if result.Projections[0].TaxDeferredBal <= initialTD*0.9 {
			t.Errorf("balance should reflect growth from allocation return, got %.2f vs initial %.2f",
				result.Projections[0].TaxDeferredBal, initialTD)
		}
	})

	t.Run("zero portfolio produces zero RMDs", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 75
		s.PortfolioValue = 0
		s.InvestmentReturn = 5.0
		s.ProjectionYears = 10

		c := NewCalculator(s)
		result := c.CalculateRMDAnalysis()

		if result.TaxDeferredValue != 0 {
			t.Errorf("TaxDeferredValue = %.2f, want 0", result.TaxDeferredValue)
		}
		for _, p := range result.Projections {
			if p.RMDAmount != 0 {
				t.Errorf("RMDAmount at age %d should be 0, got %.2f", p.Age, p.RMDAmount)
			}
		}
	})
}

func TestCalculateStateTax(t *testing.T) {
	t.Run("zero income returns zero", func(t *testing.T) {
		tc := NewTaxCalculator(&models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: 5.0,
		}, 3.0)

		got := tc.CalculateStateTax(0)
		if got != 0 {
			t.Errorf("CalculateStateTax(0) = %.2f, want 0", got)
		}
	})

	t.Run("negative income returns zero", func(t *testing.T) {
		tc := NewTaxCalculator(&models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: 5.0,
		}, 3.0)

		got := tc.CalculateStateTax(-10000)
		if got != 0 {
			t.Errorf("CalculateStateTax(-10000) = %.2f, want 0", got)
		}
	})

	t.Run("zero state rate returns zero", func(t *testing.T) {
		tc := NewTaxCalculator(&models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: 0,
		}, 3.0)

		got := tc.CalculateStateTax(50000)
		if got != 0 {
			t.Errorf("CalculateStateTax(50000) with 0%% rate = %.2f, want 0", got)
		}
	})

	t.Run("normal case", func(t *testing.T) {
		tc := NewTaxCalculator(&models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: 5.0,
		}, 3.0)

		got := tc.CalculateStateTax(100000)
		want := 100000 * 0.05
		if math.Abs(got-want) > 0.01 {
			t.Errorf("CalculateStateTax(100000) = %.2f, want %.2f", got, want)
		}
	})
}

func TestCalculateTotalTax(t *testing.T) {
	t.Run("normal case with state tax", func(t *testing.T) {
		tc := NewTaxCalculator(&models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: 5.0,
		}, 3.0)

		federalTax, stateTax, totalTax, effectiveRate := tc.CalculateTotalTax(100000, 0)

		if federalTax <= 0 {
			t.Errorf("federalTax should be positive, got %.2f", federalTax)
		}
		if stateTax <= 0 {
			t.Errorf("stateTax should be positive, got %.2f", stateTax)
		}
		if math.Abs(totalTax-(federalTax+stateTax)) > 0.01 {
			t.Errorf("totalTax (%.2f) != federalTax (%.2f) + stateTax (%.2f)", totalTax, federalTax, stateTax)
		}
		if effectiveRate <= 0 || effectiveRate >= 100 {
			t.Errorf("effectiveRate = %.2f, expected between 0 and 100", effectiveRate)
		}
		expectedEffective := (totalTax / 100000) * 100
		if math.Abs(effectiveRate-expectedEffective) > 0.01 {
			t.Errorf("effectiveRate = %.2f, want %.2f", effectiveRate, expectedEffective)
		}
	})

	t.Run("zero income returns all zeros", func(t *testing.T) {
		tc := NewTaxCalculator(&models.TaxConfig{
			FilingStatus:       models.FilingMarriedJoint,
			StateIncomeTaxRate: 5.0,
		}, 3.0)

		federalTax, stateTax, totalTax, effectiveRate := tc.CalculateTotalTax(0, 0)

		if federalTax != 0 || stateTax != 0 || totalTax != 0 || effectiveRate != 0 {
			t.Errorf("expected all zeros for zero income, got federal=%.2f state=%.2f total=%.2f rate=%.2f",
				federalTax, stateTax, totalTax, effectiveRate)
		}
	})

	t.Run("no state tax", func(t *testing.T) {
		tc := NewTaxCalculator(&models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: 0,
		}, 3.0)

		federalTax, stateTax, totalTax, _ := tc.CalculateTotalTax(100000, 0)

		if stateTax != 0 {
			t.Errorf("stateTax should be 0 with no state rate, got %.2f", stateTax)
		}
		if math.Abs(totalTax-federalTax) > 0.01 {
			t.Errorf("totalTax (%.2f) should equal federalTax (%.2f) when no state tax", totalTax, federalTax)
		}
	})

	t.Run("with inflation adjustment", func(t *testing.T) {
		tc := NewTaxCalculator(&models.TaxConfig{
			FilingStatus:       models.FilingMarriedJoint,
			StateIncomeTaxRate: 4.0,
		}, 3.0)

		// Same income, 10 years from base should produce lower tax due to inflation-adjusted brackets
		_, _, totalNow, _ := tc.CalculateTotalTax(80000, 0)
		_, _, totalFuture, _ := tc.CalculateTotalTax(80000, 10)

		if totalFuture >= totalNow {
			t.Errorf("tax in future (%.2f) should be <= tax now (%.2f) due to inflation-adjusted brackets",
				totalFuture, totalNow)
		}
	})
}

func TestRunProjectionDeductsTaxesFromRMDCashFlow(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 2_000_000
	s.ProjectionYears = 1
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.MonthlyLivingExpenses = 0
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.ExpenseSources = nil
	s.IncomeSources = nil
	s.CurrentAge = 75
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	s.StockPercent = 0
	s.CashPercent = 100

	calc := NewCalculator(s)
	result := calc.RunProjection()
	if len(result.Months) == 0 {
		t.Fatal("expected projection months")
	}

	month0 := result.Months[0]
	if month0.RMDWithdrawal <= 0 {
		t.Fatalf("expected positive RMD withdrawal, got %.2f", month0.RMDWithdrawal)
	}
	if month0.TaxableWithdrawals != month0.RMDWithdrawal {
		t.Fatalf("expected taxable withdrawals to equal the forced RMD, got taxable=%.2f rmd=%.2f", month0.TaxableWithdrawals, month0.RMDWithdrawal)
	}
	if month0.TaxesPaid <= 0 {
		t.Fatalf("expected positive taxes from RMD cash flow, got %.2f", month0.TaxesPaid)
	}
	if month0.NetIncome >= month0.GrossIncome {
		t.Fatalf("expected taxes to reduce net income, got gross=%.2f net=%.2f", month0.GrossIncome, month0.NetIncome)
	}
	if month0.TaxableBalance >= month0.RMDWithdrawal {
		t.Fatalf("expected some RMD cash to be consumed by taxes, got taxable balance %.2f from RMD %.2f", month0.TaxableBalance, month0.RMDWithdrawal)
	}
}

// F-035 tests — configurable RMD timing

// buildF035Settings creates settings for a 73-year-old at projection start
// with $1M all in tax-deferred, 7% return, and zero inflation.
func buildF035Settings(timing models.RMDTiming) *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	// Set BirthMonth so that age = 73 at StartDate "2026-01".
	// BirthMonthForAge("2026-01", 73) → "1953-01".
	s.StartDate = "2026-01"
	if primary := s.GetPrimaryPerson(); primary != nil {
		primary.BirthMonth = models.BirthMonthForAge("2026-01", 73)
	}
	s.SpouseAge = 0
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	s.InvestmentReturn = 7.0
	s.InflationRate = 0
	s.ProjectionYears = 1
	s.RMDTiming = timing
	s.ComputeAges()
	return s
}

// At age 73, factor=26.5 → RMD = balance/26.5.
// All three timings produce the same mathematical year-end balance
// (B×G×(1-1/F)), but they apply growth differently around the RMD,
// so the recorded TaxDeferredBal (= balance at RMD time) differs.

func TestCalculateRMDAnalysis_F035_TimingStartOfYear(t *testing.T) {
	s := buildF035Settings(models.RMDTimingStartOfYear)
	calc := NewCalculator(s)
	analysis := calc.CalculateRMDAnalysis()
	if analysis == nil || len(analysis.Projections) == 0 {
		t.Fatal("nil or empty analysis")
	}
	p := analysis.Projections[0]
	// Start-of-year: no pre-RMD growth → TaxDeferredBal = 1,000,000.
	wantBal := 1_000_000.0
	if math.Abs(p.TaxDeferredBal-wantBal) > 1.0 {
		t.Errorf("start-of-year TaxDeferredBal = %.2f; want ~%.2f", p.TaxDeferredBal, wantBal)
	}
	// RMD = 1,000,000 / 26.5 ≈ 37,735.85
	wantRMD := 1_000_000.0 / 26.5
	if math.Abs(p.RMDAmount-wantRMD) > 1.0 {
		t.Errorf("start-of-year RMDAmount = %.2f; want ~%.2f", p.RMDAmount, wantRMD)
	}
}

func TestCalculateRMDAnalysis_F035_TimingMidYearIsDefault(t *testing.T) {
	// Empty RMDTiming → NormalizeRMDTiming → mid_year for new scenarios.
	s := buildF035Settings("") // empty → mid_year
	calc := NewCalculator(s)
	analysis := calc.CalculateRMDAnalysis()
	if analysis == nil || len(analysis.Projections) == 0 {
		t.Fatal("nil or empty analysis")
	}
	p := analysis.Projections[0]
	// Mid-year: half-year growth before RMD.
	// TaxDeferredBal = 1M × 1.07^0.5 ≈ 1,034,408.
	wantBal := 1_000_000.0 * math.Pow(1.07, 0.5)
	if math.Abs(p.TaxDeferredBal-wantBal) > 10.0 {
		t.Errorf("mid-year TaxDeferredBal = %.2f; want ~%.2f", p.TaxDeferredBal, wantBal)
	}
	// RMD is larger because balance grew before withdrawal.
	if p.RMDAmount <= 1_000_000.0/26.5 {
		t.Errorf("mid-year RMDAmount (%.2f) should exceed start-of-year RMD (%.2f)",
			p.RMDAmount, 1_000_000.0/26.5)
	}
}

func TestCalculateRMDAnalysis_F035_TimingEndOfYear(t *testing.T) {
	s := buildF035Settings(models.RMDTimingEndOfYear)
	calc := NewCalculator(s)
	analysis := calc.CalculateRMDAnalysis()
	if analysis == nil || len(analysis.Projections) == 0 {
		t.Fatal("nil or empty analysis")
	}
	p := analysis.Projections[0]
	// End-of-year: full year of growth before RMD.
	// TaxDeferredBal = 1M × 1.07 = 1,070,000.
	wantBal := 1_000_000.0 * 1.07
	if math.Abs(p.TaxDeferredBal-wantBal) > 10.0 {
		t.Errorf("end-of-year TaxDeferredBal = %.2f; want ~%.2f", p.TaxDeferredBal, wantBal)
	}
	// RMD is even larger because a full year's growth precedes it.
	wantRMD := wantBal / 26.5
	if math.Abs(p.RMDAmount-wantRMD) > 1.0 {
		t.Errorf("end-of-year RMDAmount = %.2f; want ~%.2f", p.RMDAmount, wantRMD)
	}
}

func TestCalculateRMDAnalysis_F035_StartOfYearLargestBalance(t *testing.T) {
	// The three timings should record increasing TaxDeferredBal values:
	// start < mid < end (because more pre-RMD growth → larger recorded balance).
	soy := buildF035Settings(models.RMDTimingStartOfYear)
	mid := buildF035Settings(models.RMDTimingMidYear)
	eoy := buildF035Settings(models.RMDTimingEndOfYear)

	pSOY := NewCalculator(soy).CalculateRMDAnalysis().Projections[0].TaxDeferredBal
	pMid := NewCalculator(mid).CalculateRMDAnalysis().Projections[0].TaxDeferredBal
	pEOY := NewCalculator(eoy).CalculateRMDAnalysis().Projections[0].TaxDeferredBal

	if !(pSOY < pMid && pMid < pEOY) {
		t.Errorf("expected SOY(%.2f) < Mid(%.2f) < EOY(%.2f)", pSOY, pMid, pEOY)
	}
}

func TestNormalizeRMDTiming_F035(t *testing.T) {
	cases := []struct {
		input models.RMDTiming
		want  models.RMDTiming
	}{
		{models.RMDTimingStartOfYear, models.RMDTimingStartOfYear},
		{models.RMDTimingMidYear, models.RMDTimingMidYear},
		{models.RMDTimingEndOfYear, models.RMDTimingEndOfYear},
		{"", models.RMDTimingMidYear},
		{"bogus", models.RMDTimingMidYear},
	}
	for _, tc := range cases {
		got := models.NormalizeRMDTiming(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeRMDTiming(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}
