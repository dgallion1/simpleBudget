package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func TestCalculateRMDAnalysis(t *testing.T) {
	t.Run("age 65 with 8 years until RMD", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 65
		s.SpouseAge = 0
		s.PortfolioValue = 1_000_000
		s.TaxDeferredPercent = 60
		s.InvestmentReturn = 6.0
		s.ProjectionYears = 30

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
