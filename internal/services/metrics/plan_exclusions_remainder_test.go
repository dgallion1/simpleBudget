package metrics

import (
	"testing"
	"time"

	"budget2/internal/models"
)

// SY4 attempt 3 / ruling SY-2026-08-30d: attempt 2's "subtract the flagged
// group's signed net from an already-Abs'd total" shape was itself the
// defect -- it only fixed the case where the FLAGGED group's sign diverges.
// The REMAINDER (everything else) can also net a refund (e.g. an
// outflow-typed credit misclassified by the load-time keyword inference),
// independent of the flagged group's own sign, and |S+F|+F != |S| in
// general. These tests are the required remainder-sign-divergent fixture:
// the checker-second probe verbatim (remainder = -1000 grocery + 4000
// outflow-typed credit, net +3000; flagged = one ordinary -500 car
// payment), asserting the living figure equals |remainder| exactly.
// Kept ALONGSIDE (not replacing) the flagged-sign-divergent fixtures in
// plan_exclusions_signed_net_test.go.

// TestCalculateMetrics_PlanExclusions_RemainderNetsRefundLivingEqualsAbsRemainder
// is the ruling's exact probe run through the real Calculate.
func TestCalculateMetrics_PlanExclusions_RemainderNetsRefundLivingEqualsAbsRemainder(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-grocery", Amount: -1000, Date: time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Food"},
		{Hash: "h-credit", Amount: 4000, Date: time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Misc"},
		{Hash: "h-car", Amount: -500, Date: time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-car": {ID: "car", Name: "Car Loan", ExcludeFromPlanSync: true},
	}

	m := Calculate(ts, start, end, 0, 0, fullCoverage, true, planExclusions)

	// remainder S = -1000 + 4000 = +3000; living must equal |S| = 3000
	// exactly -- NOT |S+F|+F = |3000-500|-(-500) = 2500+500 = 3000... the
	// discriminating attempt-2 defect value is |S+F|+F computed the OTHER
	// way attempt 2 actually shipped it: |S+F| + F where F is signed net
	// spend (-500 here, i.e. planExcludedTotal=+500 in the OLD convention
	// direction) -- the point is ONLY math.Abs(remainder) is ever correct;
	// this assertion pins that value directly.
	if !floatEqual(m.LivingExpensesTotal, 3000) {
		t.Errorf("LivingExpensesTotal = %v, want 3000 (|remainder| exactly; the flagged group's own presence must never perturb it)", m.LivingExpensesTotal)
	}
	// PlanExcludedTotal is still the SIGNED net (display-only annotation,
	// attempt-2 convention, unchanged): the car payment is an ordinary net
	// spend of $500, so +500.
	if !floatEqual(m.PlanExcludedTotal, 500) {
		t.Errorf("PlanExcludedTotal = %v, want 500 (signed net spend, display-only)", m.PlanExcludedTotal)
	}
	if m.PlanExcludedCount != 1 {
		t.Errorf("PlanExcludedCount = %d, want 1", m.PlanExcludedCount)
	}
}

// TestCalculateMetrics_PlanExclusions_RemainderNetsRefundMonthInLivingTrend
// guards the per-month LivingExpensesTrend array against the same class of
// defect: a month whose REMAINDER (non-flagged, non-HI spend) nets a
// refund must show the SIGNED negated net of that month's remainder (CB2:
// a refund-dominant month is negative, a credit), not its absolute value.
func TestCalculateMetrics_PlanExclusions_RemainderNetsRefundMonthInLivingTrend(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-jan-grocery", Amount: -1000, Date: jan, TransactionType: models.Outflow, Category: "Food"},
		{Hash: "h-jan-credit", Amount: 4000, Date: jan, TransactionType: models.Outflow, Category: "Misc"},
		{Hash: "h-jan-car", Amount: -500, Date: jan, TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-feb-rent", Amount: -1500, Date: feb, TransactionType: models.Outflow, Category: "Housing"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-jan-car": {ID: "car", Name: "Car Loan", ExcludeFromPlanSync: true},
	}

	m := Calculate(ts, start, end, 0, 0, fullCoverage, true, planExclusions)

	if len(m.LivingExpensesTrend) != 2 {
		t.Fatalf("LivingExpensesTrend has %d entries, want 2: %v", len(m.LivingExpensesTrend), m.LivingExpensesTrend)
	}
	if !floatEqual(m.LivingExpensesTrend[0], -3000) {
		t.Errorf("LivingExpensesTrend[Jan] = %v, want -3000 (signed negated net of the remainder; CB2: refund-dominant month is a credit)", m.LivingExpensesTrend[0])
	}
	if !floatEqual(m.LivingExpensesTrend[1], 1500) {
		t.Errorf("LivingExpensesTrend[Feb] = %v, want 1500 (unaffected)", m.LivingExpensesTrend[1])
	}
}

// NOTE on CombinedCumulativeBalance (UPDATED, CB2): a remainder-refund
// fixture large enough to net positive (per the ruling's probe, a +4000
// outflow-typed credit) used to break the walk's undocumented, relied-upon
// precondition that "per-month |sum| partitions range-level |sum|" -- a
// precondition abs-of-parts only satisfies when every part shares one sign.
// CB1 (PR #80) fixed this: the walk's per-month spend is now the SIGNED
// negated net of the month bucket (-bucket.SumAmount()), not math.Abs, so a
// CALENDAR MONTH whose combined (living+healthcare+flagged) sign flips
// positive enters the walk as a credit instead of breaking the partition
// invariant. See TestCalculateMetrics_CombinedCumulativeBalance_
// RefundDominantMonthEntersAsCredit and TestChartCumulativeWalk_
// AgreesWithMetricsCombinedCumulativeBalance for the walk's own regression
// coverage of exactly this fixture shape. This does not touch the
// RANGE-level totals (LivingExpensesTotal, TotalExpenses, etc.), which
// still use math.Abs and still only partition into signed per-month parts
// while the RANGE as a whole nets outflow-negative (unchanged, out of
// scope). The flagged-sign-divergent walk invariant test in
// plan_exclusions_signed_net_test.go (whose fixture never flips a month's
// raw sign) covers the walk's OWN plan-exclusion arithmetic separately.

// TestComparison_PlanExclusions_RemainderNetsRefundAppliedToBothWindows
// extends Comparison coverage with the remainder-sign-divergent fixture on
// both windows.
func TestComparison_PlanExclusions_RemainderNetsRefundAppliedToBothWindows(t *testing.T) {
	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-jan-grocery", Amount: -1000, Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Food"},
		{Hash: "h-jan-credit", Amount: 4000, Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Misc"},
		{Hash: "h-jan-car", Amount: -500, Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-feb-grocery", Amount: -1000, Date: time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Food"},
		{Hash: "h-feb-credit", Amount: 4000, Date: time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Misc"},
		{Hash: "h-feb-car", Amount: -500, Date: time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-jan-car": {ID: "car", Name: "Car Loan", ExcludeFromPlanSync: true},
		"h-feb-car": {ID: "car", Name: "Car Loan", ExcludeFromPlanSync: true},
	}

	got := Comparison(ts, start, end, "previous", nil, planExclusions)
	if got == nil || !got.HasData {
		t.Fatalf("Comparison returned nil/no-data: %+v", got)
	}
	if !floatEqual(got.Current.LivingExpensesTotal, 3000) {
		t.Errorf("Current.LivingExpensesTotal = %v, want 3000 (|remainder| exactly)", got.Current.LivingExpensesTotal)
	}
	if !floatEqual(got.Previous.LivingExpensesTotal, 3000) {
		t.Errorf("Previous.LivingExpensesTotal = %v, want 3000", got.Previous.LivingExpensesTotal)
	}
}
