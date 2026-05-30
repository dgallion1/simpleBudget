package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// A manually-entered Social-Security-typed income source must appear
// exactly once in the income breakdown. CalculateMonthlyIncomeBreakdown
// routes such sources into the aggregated SocialSecurityIncome bucket, and
// BudgetFit surfaces that bucket as its own "Social Security" row — so the
// per-source loop must skip them. Otherwise the source is listed twice
// (once under its own name, once under the aggregate) and the breakdown
// rows sum to more than the Income total shown above them.
func TestBudgetFit_SocialSecuritySourceNotDoubleCounted(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.SocialSecurity = nil // no optimizer; manual source feeds the SS bucket
	s.IncomeSources = []models.IncomeSource{
		{ID: "ss", Name: "Social Security", Amount: 2000, StartMonth: 0},
		{ID: "pen", Name: "Pension", Amount: 1500, StartMonth: 0},
	}

	in := engineInput(t, s)
	result := BudgetFit(in, nil)

	var ssRows int
	var ssAmount, breakdownSum float64
	for _, item := range result.IncomeBreakdown {
		breakdownSum += item.Amount
		if item.Name == "Social Security" {
			ssRows++
			ssAmount = item.Amount
		}
	}

	if ssRows != 1 {
		t.Fatalf("expected exactly 1 Social Security income row, got %d: %+v", ssRows, result.IncomeBreakdown)
	}
	if math.Abs(ssAmount-2000) > 0.01 {
		t.Errorf("Social Security row amount = %.2f, want 2000", ssAmount)
	}
	// The breakdown rows must reconcile with the Income total; a duplicated
	// SS row would push the sum 2000 over MonthlyIncome.
	if math.Abs(breakdownSum-result.MonthlyIncome) > 0.01 {
		t.Errorf("income breakdown rows sum to %.2f but MonthlyIncome = %.2f", breakdownSum, result.MonthlyIncome)
	}
}
