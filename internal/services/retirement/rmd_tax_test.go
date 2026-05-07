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
	// F-074: pin RMD trigger month to 0 so this test continues to assert
	// month-0 behavior. Default timing is mid_year (trigger month 6), which
	// would push the RMD out of month 0 and break the test's premise. The
	// test's purpose is verifying tax deduction from RMD cash flow, not
	// timing semantics; start_of_year preserves the original intent.
	s.RMDTiming = models.RMDTimingStartOfYear

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
