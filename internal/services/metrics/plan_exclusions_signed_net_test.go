package metrics

import (
	"testing"
	"time"

	"budget2/internal/models"
)

// SY4 attempt 2 / ruling SY-2026-08-30c: every plan-exclusion site subtracts
// a SIGNED net, never math.Abs. These tests are the sign-divergent fixture
// required at every consumer -- the flagged group's REFUNDS EXCEED its
// PAYMENTS (net refund), which the attempt-1 math.Abs defect inflated into
// an over-subtraction. All attempt-1 tests are kept unmodified; these
// extend coverage rather than replace it.

// TestCalculateMetrics_PlanExclusions_NetRefundGroupAddsBackNotSubtracts is
// the exact probe from the ruling: a flagged $2,000 payment plus a $2,500
// refund (net refund +$500) beside $3,000 rent. The attempt-1 defect
// (math.Abs) produced LivingExpensesTotal 2,000; the fix must produce 3,000
// -- the unflagged spend exactly, with the net-refund group adding nothing
// (and, if anything, giving back headroom) rather than being subtracted as
// if it were a $500 expense.
func TestCalculateMetrics_PlanExclusions_NetRefundGroupAddsBackNotSubtracts(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-rent", Amount: -3000, Date: time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-pay", Amount: -2000, Date: time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-refund", Amount: 2500, Date: time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-pay":    {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
		"h-refund": {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
	}

	m := Calculate(ts, start, end, 0, 0, fullCoverage, true, planExclusions)

	// The unflagged spend (rent only) exactly -- NOT 2000 (the attempt-1
	// math.Abs defect: abs(-2000+2500)=abs(500)=500, 3000-500... actually
	// the observed attempt-1 number was 2000; either way, the ONLY correct
	// answer is 3000).
	if !floatEqual(m.LivingExpensesTotal, 3000) {
		t.Errorf("LivingExpensesTotal = %v, want 3000 (unflagged rent only; a net-refund flagged group must never reduce living below the unflagged total)", m.LivingExpensesTotal)
	}
	// PlanExcludedTotal carries the SIGNED net: -(-2000+2500) = -500,
	// negative because the group is a net refund.
	if !floatEqual(m.PlanExcludedTotal, -500) {
		t.Errorf("PlanExcludedTotal = %v, want -500 (signed net refund, never math.Abs)", m.PlanExcludedTotal)
	}
	if m.PlanExcludedCount != 2 {
		t.Errorf("PlanExcludedCount = %d, want 2", m.PlanExcludedCount)
	}
	// TotalExpenses (money really spent, netting the refund) is unaffected
	// by the flag either way: 3000 rent + 2000 payment - 2500 refund = 2500.
	if !floatEqual(m.TotalExpenses, 2500) {
		t.Errorf("TotalExpenses = %v, want 2500 (3000+2000-2500, unaffected by classification)", m.TotalExpenses)
	}
}

// TestCalculateMetrics_PlanExclusions_NetRefundMonthInLivingTrend guards the
// per-month LivingExpensesTrend array against the same sign defect: a month
// whose flagged group is a net refund must show that month's unflagged
// spend, not an inflated (over-subtracted) figure.
func TestCalculateMetrics_PlanExclusions_NetRefundMonthInLivingTrend(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-jan-rent", Amount: -1500, Date: jan, TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-jan-pay", Amount: -800, Date: jan, TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-jan-refund", Amount: 1200, Date: jan, TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-feb-rent", Amount: -1500, Date: feb, TransactionType: models.Outflow, Category: "Housing"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-jan-pay":    {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
		"h-jan-refund": {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
	}

	m := Calculate(ts, start, end, 0, 0, fullCoverage, true, planExclusions)

	if len(m.LivingExpensesTrend) != 2 {
		t.Fatalf("LivingExpensesTrend has %d entries, want 2: %v", len(m.LivingExpensesTrend), m.LivingExpensesTrend)
	}
	if !floatEqual(m.LivingExpensesTrend[0], 1500) {
		t.Errorf("LivingExpensesTrend[Jan] = %v, want 1500 (rent only; Jan's flagged group is a net refund of +400 and must not inflate the subtraction)", m.LivingExpensesTrend[0])
	}
	if !floatEqual(m.LivingExpensesTrend[1], 1500) {
		t.Errorf("LivingExpensesTrend[Feb] = %v, want 1500 (unaffected)", m.LivingExpensesTrend[1])
	}
}

// TestCalculateMetrics_PlanExclusions_NetRefundCombinedCumulativeBalanceInvariantHolds
// re-guards the documented CombinedCumulativeBalance invariant (last point
// == -CombinedCumulativeDelta) specifically with a net-refund flagged group,
// since the walk recomputes "spend" independently from LivingExpensesTotal
// and has its own signed-net subtraction to get right.
func TestCalculateMetrics_PlanExclusions_NetRefundCombinedCumulativeBalanceInvariantHolds(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-jan-rent", Amount: -1500, Date: jan, TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-jan-premium", Amount: -400, Date: jan, TransactionType: models.Outflow, Category: "Health Insurance"},
		{Hash: "h-jan-pay", Amount: -800, Date: jan, TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-jan-refund", Amount: 1200, Date: jan, TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-feb-rent", Amount: -1500, Date: feb, TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-feb-premium", Amount: -400, Date: feb, TransactionType: models.Outflow, Category: "Health Insurance"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-jan-pay":    {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
		"h-jan-refund": {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
	}

	m := Calculate(ts, start, end, 1200, 350, fullCoverage, true, planExclusions)

	if len(m.CombinedCumulativeBalance) == 0 {
		t.Fatalf("CombinedCumulativeBalance is empty, want 2 points")
	}
	last := m.CombinedCumulativeBalance[len(m.CombinedCumulativeBalance)-1]
	want := -m.CombinedCumulativeDelta
	if diff := last - want; diff > 0.01 || diff < -0.01 {
		t.Errorf("last CombinedCumulativeBalance point = %v, want %v (== -CombinedCumulativeDelta) with a net-refund flagged group", last, want)
	}
}

// TestComparison_PlanExclusions_NetRefundAppliedToBothWindows extends the
// attempt-1 Comparison coverage with a sign-divergent fixture on both
// windows.
func TestComparison_PlanExclusions_NetRefundAppliedToBothWindows(t *testing.T) {
	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-jan-rent", Amount: -1500, Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-jan-pay", Amount: -800, Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-jan-refund", Amount: 1200, Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-feb-rent", Amount: -1500, Date: time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-feb-pay", Amount: -800, Date: time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-feb-refund", Amount: 1200, Date: time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-jan-pay":    {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
		"h-jan-refund": {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
		"h-feb-pay":    {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
		"h-feb-refund": {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
	}

	got := Comparison(ts, start, end, "previous", nil, planExclusions)
	if got == nil || !got.HasData {
		t.Fatalf("Comparison returned nil/no-data: %+v", got)
	}
	if !floatEqual(got.Current.LivingExpensesTotal, 1500) {
		t.Errorf("Current.LivingExpensesTotal = %v, want 1500 (rent only; the net-refund flagged group must not reduce it)", got.Current.LivingExpensesTotal)
	}
	if !floatEqual(got.Previous.LivingExpensesTotal, 1500) {
		t.Errorf("Previous.LivingExpensesTotal = %v, want 1500", got.Previous.LivingExpensesTotal)
	}
}
