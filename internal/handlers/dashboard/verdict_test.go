package dashboard

import (
	"testing"

	"budget2/internal/models"
)

func TestBuildBudgetVerdict(t *testing.T) {
	t.Run("nil metrics is neutral with no target", func(t *testing.T) {
		v := BuildBudgetVerdict(nil)
		if v.Health != BudgetNeutral {
			t.Errorf("Health = %q, want neutral", v.Health)
		}
		if v.HasTarget {
			t.Errorf("HasTarget = true, want false")
		}
	})

	t.Run("no combined target is neutral", func(t *testing.T) {
		m := &models.DashboardMetrics{HasCombinedTarget: false, TotalIncome: 5000, NetSavings: 1000}
		v := BuildBudgetVerdict(m)
		if v.Health != BudgetNeutral {
			t.Errorf("Health = %q, want neutral", v.Health)
		}
		if v.HasTarget {
			t.Errorf("HasTarget = true, want false")
		}
		// Income/savings figures are still carried for the band.
		if v.TotalIncome != 5000 || v.NetSavings != 1000 {
			t.Errorf("income/savings = (%v,%v), want (5000,1000)", v.TotalIncome, v.NetSavings)
		}
	})

	t.Run("under budget is green", func(t *testing.T) {
		m := &models.DashboardMetrics{
			HasCombinedTarget:       true,
			CombinedCumulativeDelta: -500,
			LivingExpensesTotal:     8000, HealthcareTotal: 1500,
			LivingTargetTotal: 8500, HealthcareTargetTotal: 1500,
			MonthsInRange: 3,
		}
		v := BuildBudgetVerdict(m)
		if v.Health != BudgetGreen {
			t.Errorf("Health = %q, want green", v.Health)
		}
		if !v.IsUnder || v.IsOver {
			t.Errorf("IsUnder/IsOver = (%v,%v), want (true,false)", v.IsUnder, v.IsOver)
		}
		if v.SpentTotal != 9500 {
			t.Errorf("SpentTotal = %v, want 9500", v.SpentTotal)
		}
		if v.TargetTotal != 10000 {
			t.Errorf("TargetTotal = %v, want 10000", v.TargetTotal)
		}
	})

	t.Run("on budget is green", func(t *testing.T) {
		m := &models.DashboardMetrics{HasCombinedTarget: true, CombinedCumulativeDelta: 0.5, LivingTargetTotal: 10000}
		v := BuildBudgetVerdict(m)
		if v.Health != BudgetGreen {
			t.Errorf("Health = %q, want green", v.Health)
		}
		if v.IsOver || v.IsUnder {
			t.Errorf("IsOver/IsUnder = (%v,%v), want both false (on budget)", v.IsOver, v.IsUnder)
		}
	})

	t.Run("slightly over budget is amber", func(t *testing.T) {
		m := &models.DashboardMetrics{
			HasCombinedTarget:       true,
			CombinedCumulativeDelta: 500, // 5% of 10000 target
			LivingTargetTotal:       10000,
		}
		v := BuildBudgetVerdict(m)
		if v.Health != BudgetAmber {
			t.Errorf("Health = %q, want amber", v.Health)
		}
		if !v.IsOver {
			t.Errorf("IsOver = false, want true")
		}
	})

	t.Run("well over budget is red", func(t *testing.T) {
		m := &models.DashboardMetrics{
			HasCombinedTarget:       true,
			CombinedCumulativeDelta: 2500, // 25% of 10000 target
			LivingTargetTotal:       10000,
		}
		v := BuildBudgetVerdict(m)
		if v.Health != BudgetRed {
			t.Errorf("Health = %q, want red", v.Health)
		}
	})
}
