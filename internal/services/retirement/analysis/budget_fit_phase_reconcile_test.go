package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// With spending phases enabled, the itemized expense breakdown must still
// sum to the MonthlyExpenses header. engine.TotalExpenses applies the
// phase multiplier to base living expenses and to discretionary expense
// sources, so the "Living Expenses" row and discretionary source rows
// must carry the same multiplier — otherwise the rows visibly under-sum
// the total shown above them.
func TestBudgetFit_ExpenseBreakdownReconcilesWithSpendingPhases(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.10},
		},
	}
	s.ExpenseSources = []models.ExpenseSource{
		{ID: "travel", Name: "Travel", Amount: 800, Discretionary: true},
	}

	in := engineInput(t, s)
	result := BudgetFit(in, nil)

	var rowSum float64
	for _, item := range result.ExpenseBreakdown {
		rowSum += item.Amount
	}
	if math.Abs(rowSum-result.MonthlyExpenses) > 0.01 {
		t.Errorf("expense breakdown rows sum to %.2f but MonthlyExpenses = %.2f (diff %.2f)",
			rowSum, result.MonthlyExpenses, result.MonthlyExpenses-rowSum)
	}
}
