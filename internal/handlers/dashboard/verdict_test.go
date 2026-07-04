package dashboard

import (
	"testing"

	"budget2/internal/models"
)

func TestBuildBudgetVerdict(t *testing.T) {
	t.Run("nil metrics is neutral with no target", func(t *testing.T) {
		v := BuildBudgetVerdict(nil)
		if v.Health != models.HealthNeutral {
			t.Errorf("Health = %q, want neutral", v.Health)
		}
		if v.HasTarget {
			t.Errorf("HasTarget = true, want false")
		}
	})

	t.Run("no combined target is neutral", func(t *testing.T) {
		m := &models.DashboardMetrics{HasCombinedTarget: false, TotalIncome: 5000, NetSavings: 1000}
		v := BuildBudgetVerdict(m)
		if v.Health != models.HealthNeutral {
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
		if v.Health != models.HealthGreen {
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
		if v.Health != models.HealthGreen {
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
		if v.Health != models.HealthAmber {
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
		if v.Health != models.HealthRed {
			t.Errorf("Health = %q, want red", v.Health)
		}
	})

	t.Run("over budget with zero target total is red, not amber", func(t *testing.T) {
		// HasCombinedTarget can be true while the living+healthcare target
		// total is zero (combined delta spans other categories). A real
		// overage must not silently downgrade to amber via the missing ratio.
		m := &models.DashboardMetrics{
			HasCombinedTarget:       true,
			CombinedCumulativeDelta: 5000,
			LivingTargetTotal:       0,
			HealthcareTargetTotal:   0,
		}
		if v := BuildBudgetVerdict(m); v.Health != models.HealthRed {
			t.Errorf("Health = %q, want red (over budget, zero target denominator)", v.Health)
		}
	})

	t.Run("exactly at the amber/red ratio boundary is amber", func(t *testing.T) {
		// Delta/TargetTotal == overAmberPct (10%) → not > → amber (pins > vs >=).
		m := &models.DashboardMetrics{
			HasCombinedTarget:       true,
			CombinedCumulativeDelta: 1000, // exactly 10% of 10000
			LivingTargetTotal:       10000,
		}
		if v := BuildBudgetVerdict(m); v.Health != models.HealthAmber {
			t.Errorf("Health = %q, want amber at exactly 10%%", v.Health)
		}
	})

	t.Run("within the on-budget dead-band is green and neither over nor under", func(t *testing.T) {
		// Delta == onBudgetEps (1.0): not > eps, not < -eps → on budget.
		m := &models.DashboardMetrics{HasCombinedTarget: true, CombinedCumulativeDelta: 1.0, LivingTargetTotal: 10000}
		v := BuildBudgetVerdict(m)
		if v.Health != models.HealthGreen || v.IsOver || v.IsUnder {
			t.Errorf("at eps: health=%q over=%v under=%v, want green/false/false", v.Health, v.IsOver, v.IsUnder)
		}
	})
}
