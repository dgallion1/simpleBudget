package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// With spending phases enabled and a month-0 multiplier != 1.0, the Living
// Expenses row must carry sub-items breaking the phase-adjusted total back
// into the base slider value plus the phase adjustment, so the panel never
// silently applies a multiplier the slider elsewhere doesn't show.
func TestBudgetFit_LivingExpensesSubItems_PhaseAbove1(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 7386
	s.PhaseAgeReference = "older"
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.1},
			{Name: "Slow-Go", StartAge: 70, Multiplier: 0.9},
			{Name: "No-Go", StartAge: 85, Multiplier: 0.8},
		},
	}

	in := engineInput(t, s)
	result := BudgetFit(in, nil)

	item := livingExpensesItem(t, result)
	if len(item.SubItems) != 2 {
		t.Fatalf("expected 2 sub-items, got %d: %+v", len(item.SubItems), item.SubItems)
	}

	base := item.SubItems[0]
	adj := item.SubItems[1]

	if base.Name != "Base (slider setting)" {
		t.Errorf("base sub-item name = %q", base.Name)
	}
	if math.Abs(base.Amount-7386) > 0.005 {
		t.Errorf("base amount = %.2f, want 7386.00", base.Amount)
	}
	if adj.Name != "Go-Go phase ×1.1" {
		t.Errorf("adjustment sub-item name = %q, want %q", adj.Name, "Go-Go phase ×1.1")
	}
	if !adj.SignedAmount {
		t.Errorf("adjustment sub-item must be SignedAmount so a positive value renders with a leading +")
	}
	if math.Abs(adj.Amount-738.60) > 0.005 {
		t.Errorf("adjustment amount = %.2f, want 738.60", adj.Amount)
	}

	// Sum invariant to the cent.
	if math.Abs(base.Amount+adj.Amount-item.Amount) > 0.005 {
		t.Errorf("base (%.2f) + adjustment (%.2f) = %.2f, want Amount %.2f",
			base.Amount, adj.Amount, base.Amount+adj.Amount, item.Amount)
	}
	if math.Abs(item.Amount-8124.60) > 0.005 {
		t.Errorf("Living Expenses amount = %.2f, want 8124.60", item.Amount)
	}
}

// A multiplier below 1.0 must render as a signed negative adjustment (e.g.
// "-$738.60/mo" via formatMoney), not an unsigned magnitude.
func TestBudgetFit_LivingExpensesSubItems_PhaseBelow1(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 7386
	s.PhaseAgeReference = "older"
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Slow-Go", StartAge: 0, Multiplier: 0.9},
		},
	}

	in := engineInput(t, s)
	result := BudgetFit(in, nil)

	item := livingExpensesItem(t, result)
	if len(item.SubItems) != 2 {
		t.Fatalf("expected 2 sub-items, got %d: %+v", len(item.SubItems), item.SubItems)
	}
	adj := item.SubItems[1]
	if adj.Amount >= 0 {
		t.Errorf("adjustment amount = %.2f, want a negative value for a 0.9 multiplier", adj.Amount)
	}
	if math.Abs(adj.Amount-(-738.60)) > 0.005 {
		t.Errorf("adjustment amount = %.2f, want -738.60", adj.Amount)
	}
	if math.Abs(item.Amount-6647.40) > 0.005 {
		t.Errorf("Living Expenses amount = %.2f, want 6647.40", item.Amount)
	}
}

// Sub-items must be absent entirely when spending phases are disabled.
func TestBudgetFit_LivingExpensesSubItems_AbsentWhenPhasesDisabled(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 7386
	s.SpendingPhaseConfig = nil
	s.SpendingDeclineRate = 1.0

	in := engineInput(t, s)
	result := BudgetFit(in, nil)

	item := livingExpensesItem(t, result)
	if item.SubItems != nil {
		t.Errorf("expected no sub-items when phases are disabled, got %+v", item.SubItems)
	}
}

// Sub-items must be absent when phases are enabled but the month-0
// multiplier is exactly 1.0 (nothing to disclose).
func TestBudgetFit_LivingExpensesSubItems_AbsentWhenMultiplierIsOne(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 7386
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.0},
		},
	}

	in := engineInput(t, s)
	result := BudgetFit(in, nil)

	item := livingExpensesItem(t, result)
	if item.SubItems != nil {
		t.Errorf("expected no sub-items when the multiplier is 1.0, got %+v", item.SubItems)
	}
}

func livingExpensesItem(t *testing.T, result *models.BudgetFitAnalysis) models.ExpenseBreakdownItem {
	t.Helper()
	for _, item := range result.ExpenseBreakdown {
		if item.Name == "Living Expenses" {
			return item
		}
	}
	t.Fatalf("no Living Expenses row in ExpenseBreakdown: %+v", result.ExpenseBreakdown)
	return models.ExpenseBreakdownItem{}
}
