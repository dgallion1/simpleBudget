package metrics

import (
	"reflect"
	"testing"
	"time"

	"budget2/internal/models"
)

// SY4: metrics.Calculate's planExclusions param excludes plan-modeled
// spend (a major expense flagged models.MajorExpense.ExcludeFromPlanSync,
// classified upstream by majorexpenses.ComputePlanSyncExclusions) from the
// living-expense figures, mirroring the what-if dashboard sync's own
// exclusion (D-SY-e). These tests build the exclusion map by hand (keyed by
// transaction Hash), exactly as ComputePlanSyncExclusions' documented output
// shape -- Calculate consumes the map opaquely and never re-runs the
// classifier.

// TestCalculateMetrics_PlanExclusions_DropsLivingByExactFlaggedNetTotalExpensesUnchanged
// is acceptance criterion 5(a): living actual drops by exactly the flagged
// net while TotalExpenses (money really spent) does not change.
func TestCalculateMetrics_PlanExclusions_DropsLivingByExactFlaggedNetTotalExpensesUnchanged(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	base := []models.Transaction{
		{Hash: "h-income", Description: "Salary", Amount: 8000, Date: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), TransactionType: models.Income, Category: "Payroll"},
		{Hash: "h-rent", Description: "Rent", Amount: -3000, Date: time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-groceries", Description: "Groceries", Amount: -500, Date: time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Food"},
		{Hash: "h-lucid", Description: "Lucid Loan Payment", Amount: -600, Date: time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
	}
	ts := models.NewTransactionSet(base)
	flaggedDef := models.MajorExpense{ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true}
	planExclusions := map[string]models.MajorExpense{"h-lucid": flaggedDef}

	withFlag := Calculate(ts, start, end, 2000, 0, fullCoverage, true, planExclusions)
	withoutFlag := Calculate(ts, start, end, 2000, 0, fullCoverage, true, nil)

	// TotalExpenses/TotalIncome/NetSavings/SavingsRate are the "money
	// really spent" totals and must be IDENTICAL regardless of the flag.
	if !floatEqual(withFlag.TotalExpenses, withoutFlag.TotalExpenses) {
		t.Errorf("TotalExpenses changed: with-flag=%v without-flag=%v, want identical", withFlag.TotalExpenses, withoutFlag.TotalExpenses)
	}
	if !floatEqual(withFlag.TotalIncome, withoutFlag.TotalIncome) {
		t.Errorf("TotalIncome changed: with-flag=%v without-flag=%v, want identical", withFlag.TotalIncome, withoutFlag.TotalIncome)
	}
	if !floatEqual(withFlag.NetSavings, withoutFlag.NetSavings) {
		t.Errorf("NetSavings changed: with-flag=%v without-flag=%v, want identical", withFlag.NetSavings, withoutFlag.NetSavings)
	}
	if !floatEqual(withFlag.SavingsRate, withoutFlag.SavingsRate) {
		t.Errorf("SavingsRate changed: with-flag=%v without-flag=%v, want identical", withFlag.SavingsRate, withoutFlag.SavingsRate)
	}
	if !floatEqual(withFlag.TotalExpenses, 4100) {
		t.Errorf("TotalExpenses = %v, want 4100 (all outflows, flag never removes a dollar of real spend)", withFlag.TotalExpenses)
	}

	// LivingExpensesTotal drops by EXACTLY the flagged net (600).
	wantLivingDrop := 600.0
	gotDrop := withoutFlag.LivingExpensesTotal - withFlag.LivingExpensesTotal
	if !floatEqual(gotDrop, wantLivingDrop) {
		t.Errorf("LivingExpensesTotal drop = %v, want %v (the flagged net exactly)", gotDrop, wantLivingDrop)
	}
	if !floatEqual(withFlag.LivingExpensesTotal, 3500) {
		t.Errorf("LivingExpensesTotal (with flag) = %v, want 3500 (4100 total - 600 flagged, no healthcare)", withFlag.LivingExpensesTotal)
	}

	if !floatEqual(withFlag.PlanExcludedTotal, 600) {
		t.Errorf("PlanExcludedTotal = %v, want 600", withFlag.PlanExcludedTotal)
	}
	if withFlag.PlanExcludedCount != 1 {
		t.Errorf("PlanExcludedCount = %d, want 1", withFlag.PlanExcludedCount)
	}
	if withoutFlag.PlanExcludedTotal != 0 || withoutFlag.PlanExcludedCount != 0 {
		t.Errorf("without-flag PlanExcludedTotal/Count = %v/%d, want 0/0", withoutFlag.PlanExcludedTotal, withoutFlag.PlanExcludedCount)
	}
}

// TestCalculateMetrics_PlanExclusions_NilMapEqualsEmptyMapEqualsPreSY4Fields
// is acceptance criterion 5(b): nil == empty == zero-value behavior
// identical to pre-SY4 for EVERY DashboardMetrics field. A golden
// reflect.DeepEqual between a nil-map call and an explicitly-empty-map call
// on the same fixture pins that a planExclusions with nothing in it changes
// nothing at all, not even a stray field.
func TestCalculateMetrics_PlanExclusions_NilMapEqualsEmptyMapEqualsPreSY4Fields(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Salary", 6000, time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -2000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Premium", -400, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	withNil := Calculate(ts, start, end, 1800, 350, fullCoverage, true, nil)
	withEmpty := Calculate(ts, start, end, 1800, 350, fullCoverage, true, map[string]models.MajorExpense{})

	if !reflect.DeepEqual(withNil, withEmpty) {
		t.Errorf("nil planExclusions produced a different DashboardMetrics than an empty map:\nnil:   %+v\nempty: %+v", withNil, withEmpty)
	}

	// And a planExclusions map whose only entry doesn't match any
	// transaction's Hash is equally a no-op -- not "empty", but nothing in
	// it ever claims a row.
	withNoMatch := Calculate(ts, start, end, 1800, 350, fullCoverage, true, map[string]models.MajorExpense{
		"h-nonexistent": {ID: "x", Name: "X", ExcludeFromPlanSync: true},
	})
	if !reflect.DeepEqual(withNil, withNoMatch) {
		t.Errorf("a planExclusions map with no matching Hash produced a different DashboardMetrics than nil:\nnil:      %+v\nno-match: %+v", withNil, withNoMatch)
	}
}

// TestCalculateMetrics_PlanExclusions_HIOverlapExcludedOnceNotDoubleSubtracted
// mirrors D-SY-e: a row that is BOTH HealthInsuranceCategory AND
// hash-matched in planExclusions (e.g. a flagged def whose amount happens to
// coincide with a premium payment) is removed from living exactly once, via
// the healthcare split -- and must never also inflate PlanExcludedTotal/
// Count or get double-subtracted from LivingExpensesTotal.
func TestCalculateMetrics_PlanExclusions_HIOverlapExcludedOnceNotDoubleSubtracted(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-rent", Amount: -3000, Date: time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-premium", Amount: -400, Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Health Insurance"},
		// Both HI-category AND present in planExclusions (a flagged def's
		// amount coincidentally matched this premium row upstream).
		{Hash: "h-overlap", Amount: -700, Date: time.Date(2025, 1, 18, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Health Insurance"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-overlap": {ID: "car", Name: "Car Loan", ExcludeFromPlanSync: true},
	}

	m := Calculate(ts, start, end, 2000, 500, fullCoverage, true, planExclusions)

	// TotalExpenses unaffected by classification either way.
	if !floatEqual(m.TotalExpenses, 4100) {
		t.Errorf("TotalExpenses = %v, want 4100", m.TotalExpenses)
	}
	// HealthcareTotal claims BOTH HI rows (400+700=1100) -- HI-first
	// ordering, unaffected by the overlap being also flagged.
	if !floatEqual(m.HealthcareTotal, 1100) {
		t.Errorf("HealthcareTotal = %v, want 1100 (both HI rows, overlap included)", m.HealthcareTotal)
	}
	// The overlap row is claimed by HI and must NEVER also count in the
	// plan-excluded total/count.
	if m.PlanExcludedTotal != 0 {
		t.Errorf("PlanExcludedTotal = %v, want 0 (the only flagged row is HI-category and already removed via HealthcareTotal)", m.PlanExcludedTotal)
	}
	if m.PlanExcludedCount != 0 {
		t.Errorf("PlanExcludedCount = %d, want 0", m.PlanExcludedCount)
	}
	// LivingExpensesTotal = TotalExpenses - HealthcareTotal - 0 = 3000
	// (just rent). A double-subtraction bug would show 2300 (3000-700).
	if !floatEqual(m.LivingExpensesTotal, 3000) {
		t.Errorf("LivingExpensesTotal = %v, want 3000 (rent only; the overlap row must not be subtracted twice)", m.LivingExpensesTotal)
	}
}

// TestCalculateMetrics_PlanExclusions_LivingTrendExcludesFlaggedMonth guards
// the per-month LivingExpensesTrend array (feeds the Monthly Living
// Expenses card's sparkline) -- the flagged spend must drop out of its
// month's bucket, not just the range total.
func TestCalculateMetrics_PlanExclusions_LivingTrendExcludesFlaggedMonth(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-jan-rent", Amount: -1500, Date: jan, TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-jan-lucid", Amount: -600, Date: jan, TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-feb-rent", Amount: -1500, Date: feb, TransactionType: models.Outflow, Category: "Housing"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-jan-lucid": {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
	}

	m := Calculate(ts, start, end, 0, 0, fullCoverage, true, planExclusions)

	if len(m.LivingExpensesTrend) != 2 {
		t.Fatalf("LivingExpensesTrend has %d entries, want 2: %v", len(m.LivingExpensesTrend), m.LivingExpensesTrend)
	}
	if !floatEqual(m.LivingExpensesTrend[0], 1500) {
		t.Errorf("LivingExpensesTrend[Jan] = %v, want 1500 (2100 - 600 flagged)", m.LivingExpensesTrend[0])
	}
	if !floatEqual(m.LivingExpensesTrend[1], 1500) {
		t.Errorf("LivingExpensesTrend[Feb] = %v, want 1500 (unaffected -- flag only touched January)", m.LivingExpensesTrend[1])
	}
}

// TestCalculateMetrics_PlanExclusions_CombinedCumulativeBalanceInvariantHolds
// re-guards the CombinedCumulativeBalance field's documented invariant
// (last element == -CombinedCumulativeDelta) with plan exclusions applied --
// the walk recomputes "spend" from raw monthly outflows independently of
// LivingExpensesTotal, so it has its own subtraction to get right (see
// Calculate's inline comment on the walk).
func TestCalculateMetrics_PlanExclusions_CombinedCumulativeBalanceInvariantHolds(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-jan-rent", Amount: -1500, Date: jan, TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-jan-premium", Amount: -400, Date: jan, TransactionType: models.Outflow, Category: "Health Insurance"},
		{Hash: "h-jan-lucid", Amount: -600, Date: jan, TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-feb-rent", Amount: -1500, Date: feb, TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-feb-premium", Amount: -400, Date: feb, TransactionType: models.Outflow, Category: "Health Insurance"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-jan-lucid": {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
	}

	m := Calculate(ts, start, end, 1200, 350, fullCoverage, true, planExclusions)

	if len(m.CombinedCumulativeBalance) == 0 {
		t.Fatalf("CombinedCumulativeBalance is empty, want 2 points")
	}
	last := m.CombinedCumulativeBalance[len(m.CombinedCumulativeBalance)-1]
	want := -m.CombinedCumulativeDelta
	if diff := last - want; diff > 0.01 || diff < -0.01 {
		t.Errorf("last CombinedCumulativeBalance point = %v, want %v (== -CombinedCumulativeDelta) even with plan exclusions applied", last, want)
	}
}

// TestComparison_PlanExclusionsAppliedToBothWindows is acceptance criterion
// 3: Comparison (which feeds kpis.html's "vs prior" deltas --
// ActualMonthlyChange/CumulativeDeltaChange) computes a living actual from
// transactions via its own two Calculate calls, so it must consume the same
// exclusion map for BOTH the current and comparison windows, or the "vs
// prior" delta would compare an adjusted figure against an unadjusted one.
func TestComparison_PlanExclusionsAppliedToBothWindows(t *testing.T) {
	// "previous"-type comparison: current = Feb 1-28, previous = the
	// preceding 28-day window (Jan 4-31).
	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-jan-rent", Amount: -1500, Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-jan-lucid", Amount: -600, Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
		{Hash: "h-feb-rent", Amount: -1500, Date: time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Housing"},
		{Hash: "h-feb-lucid", Amount: -600, Date: time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow, Category: "Loan"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-jan-lucid": {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
		"h-feb-lucid": {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
	}

	withFlag := Comparison(ts, start, end, "previous", nil, planExclusions)
	if withFlag == nil || !withFlag.HasData {
		t.Fatalf("Comparison returned nil/no-data: %+v", withFlag)
	}
	// Both windows exclude their $600 Lucid row, so both actuals are 1500
	// and the living-actual delta between them is ~0 -- NOT the ~0 you'd
	// coincidentally also get without the flag here (both windows are
	// symmetric), so this also checks each window individually below.
	if !floatEqual(withFlag.Current.LivingExpensesTotal, 1500) {
		t.Errorf("Current.LivingExpensesTotal = %v, want 1500 (2100 - 600 flagged)", withFlag.Current.LivingExpensesTotal)
	}
	if !floatEqual(withFlag.Previous.LivingExpensesTotal, 1500) {
		t.Errorf("Previous.LivingExpensesTotal = %v, want 1500 (2100 - 600 flagged)", withFlag.Previous.LivingExpensesTotal)
	}

	withoutFlag := Comparison(ts, start, end, "previous", nil, nil)
	if withoutFlag == nil || !withoutFlag.HasData {
		t.Fatalf("Comparison (no flag) returned nil/no-data: %+v", withoutFlag)
	}
	if !floatEqual(withoutFlag.Current.LivingExpensesTotal, 2100) {
		t.Errorf("Current.LivingExpensesTotal (no flag) = %v, want 2100 (unadjusted)", withoutFlag.Current.LivingExpensesTotal)
	}
}
