package dashboard

import (
	"math"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/metrics"
)

// TestDashboardVerdictBar_RenderedFiguresReflectPlanExclusion is SY4
// acceptance criterion 4 (rendered-string arithmetic rule, ruling
// 2026-08-29b): "where a surface renders living + healthcare = combined, a
// test asserts on RENDERED strings with a fractional-cent fixture including
// a flagged def." Unlike TestDashboardVerdictBar_LivingHealthcareBreakdown_
// RenderedSumHoldsOnFractionalCentBase (which hand-builds DashboardMetrics),
// this fixture is a real TransactionSet with a flagged major expense run
// through the REAL metrics.Calculate -> BuildBudgetVerdict ->
// "dashboard-verdict-bar" pipeline end to end, proving the SY4 exclusion
// arithmetic doesn't disturb the existing rendered-sum guarantee and that
// the "Spent" figure itself reflects the exclusion.
func TestDashboardVerdictBar_RenderedFiguresReflectPlanExclusion(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-income", Amount: 8000, Date: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), TransactionType: models.Income, Category: "Payroll"},
		{Hash: "h-rent", Amount: -1804.415, Date: time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-premium", Amount: -300.415, Date: time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Health Insurance"},
		// Flagged: modeled separately in the plan, must drop out of living.
		{Hash: "h-lucid", Amount: -600.415, Date: time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-lucid": {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
	}

	m := metrics.Calculate(ts, start, end, 1700, 250, start, true, planExclusions)
	if math.Abs(m.PlanExcludedTotal-600.415) > 0.001 {
		t.Fatalf("PlanExcludedTotal = %v, want ~600.415 (fixture sanity check)", m.PlanExcludedTotal)
	}

	v := BuildBudgetVerdict(m)
	if !v.Living.Configured || !v.Healthcare.Configured {
		t.Fatalf("fixture must configure both buckets; got Living.Configured=%v Healthcare.Configured=%v", v.Living.Configured, v.Healthcare.Configured)
	}
	if !v.IsOver {
		t.Fatalf("fixture must land over budget (deterministic case); got IsOver=%v Delta=%v", v.IsOver, v.Delta)
	}

	out, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{"BudgetVerdict": v})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	// "Spent" renders LivingExpensesTotal + HealthcareTotal -- must reflect
	// the flagged $600.415 dropped from living, not the raw outflow total.
	wantSpent := m.TotalExpenses - m.PlanExcludedTotal
	gotSpent := extractDollarAfter(t, out, "Spent</div>")
	if math.Abs(gotSpent-wantSpent) > 0.01 {
		t.Errorf("rendered Spent = %.2f, want %.2f (TotalExpenses %.2f minus PlanExcludedTotal %.2f -- flagged spend must be excluded from the displayed figure): %s",
			gotSpent, wantSpent, m.TotalExpenses, m.PlanExcludedTotal, trunc(out, 1200))
	}

	// Living + Healthcare rendered deltas must still sum to the rendered
	// combined "over budget" headline (ruling 2026-08-29b), now exercised
	// with the SY4 exclusion arithmetic feeding the pipeline instead of a
	// hand-built fixture.
	total := extractDollarBefore(t, out, "over budget")
	living := extractDollarAfter(t, out, "Living:")
	healthcare := extractDollarAfter(t, out, "Healthcare:")
	if got, want := living+healthcare, total; math.Abs(got-want) > 0.001 {
		t.Errorf("rendered Living (%.2f) + rendered Healthcare (%.2f) = %.2f, want rendered total %.2f (must sum to the cent as DISPLAYED): %s",
			living, healthcare, got, want, trunc(out, 1400))
	}
}

// TestDashboardVerdictBar_RenderedSpentReflectsNetRefundExclusion is the
// sign-divergent fixture required by ruling SY-2026-08-30c, exercised at the
// rendered layer: the flagged group is a NET REFUND ($800 payment + $1200
// refund = net +$400). The rendered "Spent" figure must equal the unflagged
// spend exactly (rent + healthcare premium), never an over-subtracted
// figure from math.Abs-ing the refund.
func TestDashboardVerdictBar_RenderedSpentReflectsNetRefundExclusion(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-income", Amount: 8000, Date: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), TransactionType: models.Income, Category: "Payroll"},
		{Hash: "h-rent", Amount: -1800, Date: time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-premium", Amount: -300, Date: time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Health Insurance"},
		{Hash: "h-lucid-pay", Amount: -800, Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-lucid-refund", Amount: 1200, Date: time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-lucid-pay":    {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
		"h-lucid-refund": {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
	}

	m := metrics.Calculate(ts, start, end, 1700, 250, start, true, planExclusions)
	if !floatEqual(m.PlanExcludedTotal, -400) {
		t.Fatalf("PlanExcludedTotal = %v, want -400 (signed net refund; fixture sanity check)", m.PlanExcludedTotal)
	}
	if !floatEqual(m.LivingExpensesTotal, 1800) {
		t.Fatalf("LivingExpensesTotal = %v, want 1800 (rent only; fixture sanity check)", m.LivingExpensesTotal)
	}

	v := BuildBudgetVerdict(m)
	out, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{"BudgetVerdict": v})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	wantSpent := m.LivingExpensesTotal + m.HealthcareTotal // 1800 + 300
	gotSpent := extractDollarAfter(t, out, "Spent</div>")
	if math.Abs(gotSpent-wantSpent) > 0.01 {
		t.Errorf("rendered Spent = %.2f, want %.2f (1800 rent + 300 healthcare; a net-refund flagged group must never inflate what's subtracted from Spent): %s",
			gotSpent, wantSpent, trunc(out, 1200))
	}
}
