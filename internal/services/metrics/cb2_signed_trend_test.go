package metrics

import (
	"testing"
	"time"

	"budget2/internal/models"
)

// CB2 regression: the per-month trend arrays (ExpensesTrend, HealthcareTrend,
// LivingExpensesTrend) must be the SIGNED negated net of their month bucket,
// not math.Abs'd. A refund-dominant month -- one whose outflow-typed rows
// net POSITIVE -- must show as NEGATIVE (a credit), and the DERIVED
// SavingsTrend must correctly ADD that refund to income rather than
// subtract it (income + net refund).
//
// Two-month fixture (amounts distinct from the CB2 oracle's 5000/300/800):
// Jan is an ordinary month (living and healthcare both net outflow-negative
// as usual). Feb is refund-dominant in BOTH the living bucket (a furniture
// store credit exceeds the month's grocery spend) and the healthcare bucket
// (an insurance overpayment return exceeds nothing, since it is the only HI
// row that month) -- exercising all three trend sites (expAmt/hcAmt/
// livingMonth) at once.
func TestCalculateMetrics_CB2_TrendSeriesSignedAndSavingsAddsRefund(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := makeTransactionSet(
		// Jan: ordinary month. Living = -1200-200 = -1400. Healthcare = -300.
		// Total outflow net = -1700.
		makeTransaction("Salary", 4000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1200, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Groceries", -200, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		makeTransaction("Health Premium", -300, time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),

		// Feb: refund-dominant in both living (-200+900=+700) and
		// healthcare (+600, its only HI row). Total outflow net =
		// -200+900+600 = +1300.
		makeTransaction("Salary", 4000, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Groceries", -200, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		makeTransaction("Boutique Store Credit", 900, time.Date(2025, 2, 14, 0, 0, 0, 0, time.UTC), models.Outflow, "Furniture"),
		makeTransaction("Insurance Overpayment Return", 600, time.Date(2025, 2, 18, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)

	m := Calculate(ts, start, end, 0, 0, fullCoverage, true, nil)

	if len(m.ExpensesTrend) != 2 || len(m.LivingExpensesTrend) != 2 || len(m.HealthcareTrend) != 2 || len(m.SavingsTrend) != 2 {
		t.Fatalf("harness error: trend lengths = expenses %d, living %d, healthcare %d, savings %d, want 2 each",
			len(m.ExpensesTrend), len(m.LivingExpensesTrend), len(m.HealthcareTrend), len(m.SavingsTrend))
	}

	// Harness validity: Jan is identical under the bug and the fix.
	if !floatEqual(m.ExpensesTrend[0], 1700) {
		t.Errorf("harness error, not the defect: Jan ExpensesTrend = %v, want 1700", m.ExpensesTrend[0])
	}
	if !floatEqual(m.LivingExpensesTrend[0], 1400) {
		t.Errorf("harness error, not the defect: Jan LivingExpensesTrend = %v, want 1400", m.LivingExpensesTrend[0])
	}
	if !floatEqual(m.HealthcareTrend[0], 300) {
		t.Errorf("harness error, not the defect: Jan HealthcareTrend = %v, want 300", m.HealthcareTrend[0])
	}
	if !floatEqual(m.SavingsTrend[0], 2300) {
		t.Errorf("harness error, not the defect: Jan SavingsTrend = %v, want 2300 (4000-1700)", m.SavingsTrend[0])
	}

	// Discriminators: Feb's refund-dominant month must be signed negative
	// in every trend it feeds, and savings must ADD the net refund.
	if !floatEqual(m.ExpensesTrend[1], -1300) {
		t.Errorf("Feb ExpensesTrend = %v, want -1300 (signed net credit); per-month abs gives 1300", m.ExpensesTrend[1])
	}
	if !floatEqual(m.LivingExpensesTrend[1], -700) {
		t.Errorf("Feb LivingExpensesTrend = %v, want -700 (signed net credit); per-month abs gives 700", m.LivingExpensesTrend[1])
	}
	if !floatEqual(m.HealthcareTrend[1], -600) {
		t.Errorf("Feb HealthcareTrend = %v, want -600 (signed net credit); per-month abs gives 600", m.HealthcareTrend[1])
	}
	if !floatEqual(m.SavingsTrend[1], 5300) {
		t.Errorf("Feb SavingsTrend = %v, want 5300 (income 4000 PLUS 1300 net refund); abs gives 2700", m.SavingsTrend[1])
	}
}
