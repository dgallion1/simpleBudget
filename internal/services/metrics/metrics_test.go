package metrics

import (
	"encoding/json"
	"math"
	"regexp"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

func makeTransaction(desc string, amount float64, date time.Time, txnType models.TransactionType, category string) models.Transaction {
	return models.Transaction{
		Description:     desc,
		Amount:          amount,
		Date:            date,
		TransactionType: txnType,
		Category:        category,
	}
}

func makeTransactionSet(txns ...models.Transaction) *models.TransactionSet {
	return &models.TransactionSet{Transactions: txns}
}

func floatEqual(a, b float64) bool {
	return math.Abs(a-b) < 0.01
}

// fullCoverage is a coverage-start sentinel earlier than every date used in
// this file's fixtures, so passing (fullCoverage, true) to Calculate
// preserves each pre-HC1 test's original intent: healthcare budgeted over
// the whole selected window, exactly like the old signature's implicit
// full-window accrual.
var fullCoverage = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// makePhaseSettings builds a WhatIfSettings with a primary person whose
// birth month places them at `ageAtStart` on `startMonth`, plus the
// default Go-Go/Active/Slow-Go phase ladder. Used by phase-adjustment
// tests that need to anchor the projection to a specific calendar age.
func makePhaseSettings(t *testing.T, monthlyExpenses float64, startMonth string, ageAtStart int, phasesEnabled bool) *models.WhatIfSettings {
	t.Helper()
	birth := models.BirthMonthForAge(startMonth, ageAtStart)
	if birth == "" {
		t.Fatalf("BirthMonthForAge(%q, %d) returned empty", startMonth, ageAtStart)
	}
	s := &models.WhatIfSettings{
		MonthlyLivingExpenses: monthlyExpenses,
		StartDate:             startMonth,
		Persons: []models.Person{
			{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: birth},
		},
		PhaseAgeReference: "primary",
		SpendingPhaseConfig: &models.SpendingPhaseConfig{
			Enabled: phasesEnabled,
			Phases:  models.DefaultSpendingPhases(),
		},
	}
	prepare.ComputeAges(s)
	return s
}

// --- MonthsBetween ---

// New test (not moved): the split from dashboard's combined test suite into
// metrics' own package-level suite left MonthsBetween's defensive "negative
// range" clamp (days < 1) uncovered -- none of the moved calculateMetrics
// tests ever pass an inverted range, though something in the dashboard HTTP
// suite evidently did by coincidence. Covering it directly here is more
// robust than relying on an indirect hit.
func TestMonthsBetween_ClampsNegativeRangeToOneDay(t *testing.T) {
	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) // end before start
	got := MonthsBetween(start, end)
	want := 1.0 / avgDaysPerMonth
	if !floatEqual(got, want) {
		t.Errorf("MonthsBetween(reversed) = %v, want %v (clamped to 1 day)", got, want)
	}
}

// --- BudgetTargets ---

// New tests (not moved): BudgetTargets is a new function per the brief,
// wrapping phaseAdjustedMonthlyTarget + currentHealthcareTarget so handler
// call sites ask one question instead of two.
func TestBudgetTargets_NilSettingsReturnsZero(t *testing.T) {
	living, healthcare := BudgetTargets(nil, time.Now(), time.Now())
	if living != 0 || healthcare != 0 {
		t.Errorf("BudgetTargets(nil, ...) = (%v, %v), want (0, 0)", living, healthcare)
	}
}

func TestBudgetTargets_WrapsPhaseAndHealthcareHelpers(t *testing.T) {
	s := makePhaseSettings(t, 5000, "2025-01", 86, true) // No-Go (0.65)
	s.MonthlyHealthcare = 1200
	s.HealthcareStartYears = 0
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)

	living, healthcare := BudgetTargets(s, start, end)
	wantLiving := phaseAdjustedMonthlyTarget(s, start, end)
	wantHealthcare := currentHealthcareTarget(s)
	if !floatEqual(living, wantLiving) {
		t.Errorf("BudgetTargets living = %v, want %v", living, wantLiving)
	}
	if !floatEqual(healthcare, wantHealthcare) {
		t.Errorf("BudgetTargets healthcare = %v, want %v", healthcare, wantHealthcare)
	}
}

// --- Comparison ---

func TestCalculateComparison_PreviousPeriod(t *testing.T) {
	// Current period: Feb 2025, Previous period: Jan 2025
	ts := makeTransactionSet(
		// Jan 2025 (previous period)
		makeTransaction("Salary", 4000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1000, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		// Feb 2025 (current period)
		makeTransaction("Salary", 5000, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1500, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	result := Comparison(ts, start, end, "previous", nil, nil)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.HasData {
		t.Fatal("expected HasData to be true")
	}

	// Income: 4000 -> 5000 = +25%
	if !floatEqual(result.IncomeChange, 25.0) {
		t.Errorf("IncomeChange = %v, want 25", result.IncomeChange)
	}
	// Expenses: 1000 -> 1500 = +50%
	if !floatEqual(result.ExpensesChange, 50.0) {
		t.Errorf("ExpensesChange = %v, want 50", result.ExpensesChange)
	}
	// Current metrics
	if !floatEqual(result.Current.TotalIncome, 5000) {
		t.Errorf("Current.TotalIncome = %v, want 5000", result.Current.TotalIncome)
	}
	if !floatEqual(result.Previous.TotalIncome, 4000) {
		t.Errorf("Previous.TotalIncome = %v, want 4000", result.Previous.TotalIncome)
	}
}

func TestCalculateComparison_YearOverYear(t *testing.T) {
	ts := makeTransactionSet(
		// Feb 2024 (previous year)
		makeTransaction("Salary", 4000, time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1200, time.Date(2024, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		// Feb 2025 (current year)
		makeTransaction("Salary", 5000, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1500, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	result := Comparison(ts, start, end, "year", nil, nil)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.HasData {
		t.Fatal("expected HasData to be true")
	}

	// Income: 4000 -> 5000 = +25%
	if !floatEqual(result.IncomeChange, 25.0) {
		t.Errorf("IncomeChange = %v, want 25", result.IncomeChange)
	}
	// Expenses: 1200 -> 1500 = +25%
	if !floatEqual(result.ExpensesChange, 25.0) {
		t.Errorf("ExpensesChange = %v, want 25", result.ExpensesChange)
	}
	// Verify the comparison looked at the right year
	if !floatEqual(result.Previous.TotalIncome, 4000) {
		t.Errorf("Previous.TotalIncome = %v, want 4000 (from 2024)", result.Previous.TotalIncome)
	}
}

func TestCalculateComparison_NoComparisonData(t *testing.T) {
	// Only current period data, no data in the comparison period
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
	)

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	result := Comparison(ts, start, end, "previous", nil, nil)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.HasData {
		t.Error("expected HasData to be false when comparison period has no data")
	}
}

func TestCalculateComparison_InvalidType(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
	)

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	result := Comparison(ts, start, end, "bogus", nil, nil)

	if result != nil {
		t.Errorf("expected nil for unknown comparison type, got %+v", result)
	}
}

func TestCalculateComparison_PopulatesBudgetDeltas(t *testing.T) {
	ts := makeTransactionSet(
		// Jan 2025: 1500 outflow → previous period
		makeTransaction("Rent", -1500, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		// Feb 2025: 2500 outflow → current period (1000 more)
		makeTransaction("Rent", -2500, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	pc := Comparison(ts, start, end, "previous", nil, nil)
	if pc == nil || !pc.HasData {
		t.Fatalf("expected non-nil comparison with HasData=true, got %+v", pc)
	}

	// Each period spans ~28 days ≈ 0.95 months.
	// current.ActualMonthly  ≈ 2500 / 0.95 ≈ 2632
	// previous.ActualMonthly ≈ 1500 / 0.95 ≈ 1579
	// ActualMonthlyChange    ≈ 2632 - 1579 ≈ 1053
	if pc.ActualMonthlyChange < 950 || pc.ActualMonthlyChange > 1150 {
		t.Errorf("ActualMonthlyChange = %v, want ~1053", pc.ActualMonthlyChange)
	}

	// CumulativeDeltaChange = current.CumulativeDelta - previous.CumulativeDelta
	// With target=0 (passed below), CumulativeDelta = TotalExpenses, so
	// CumulativeDeltaChange = 2500 - 1500 = 1000
	if pc.CumulativeDeltaChange < 950 || pc.CumulativeDeltaChange > 1050 {
		t.Errorf("CumulativeDeltaChange = %v, want ~1000", pc.CumulativeDeltaChange)
	}
}

func TestCalculateComparison_SavingsRateChange(t *testing.T) {
	ts := makeTransactionSet(
		// Jan 2025
		makeTransaction("Salary", 4000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -2000, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		// Feb 2025
		makeTransaction("Salary", 5000, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1000, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	result := Comparison(ts, start, end, "previous", nil, nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Current savings rate: (5000-1000)/5000 * 100 = 80%
	// Previous savings rate: (4000-2000)/4000 * 100 = 50%
	// Change = 80 - 50 = 30 percentage points
	if !floatEqual(result.SavingsRateChange, 30.0) {
		t.Errorf("SavingsRateChange = %v, want 30", result.SavingsRateChange)
	}
}

// --- PercentChange ---

func TestPercentChange_Normal(t *testing.T) {
	// 50 -> 75 = +50%
	got := PercentChange(75, 50)
	if !floatEqual(got, 50.0) {
		t.Errorf("PercentChange(75, 50) = %v, want 50", got)
	}

	// 100 -> 50 = -50%
	got = PercentChange(50, 100)
	if !floatEqual(got, -50.0) {
		t.Errorf("PercentChange(50, 100) = %v, want -50", got)
	}
}

func TestPercentChange_ZeroPrevious(t *testing.T) {
	got := PercentChange(100, 0)
	if !floatEqual(got, 100.0) {
		t.Errorf("PercentChange(100, 0) = %v, want 100", got)
	}
}

func TestPercentChange_BothZero(t *testing.T) {
	got := PercentChange(0, 0)
	if got != 0 {
		t.Errorf("PercentChange(0, 0) = %v, want 0", got)
	}
}

func TestPercentChange_NegativePrevious(t *testing.T) {
	// Test with negative previous value (e.g. negative savings -> positive savings)
	got := PercentChange(100, -50)
	// ((100 - (-50)) / abs(-50)) * 100 = (150/50) * 100 = 300
	if !floatEqual(got, 300.0) {
		t.Errorf("PercentChange(100, -50) = %v, want 300", got)
	}
}

func TestPercentChange_ZeroCurrentNonZeroPrevious(t *testing.T) {
	got := PercentChange(0, 100)
	// ((0-100)/100)*100 = -100
	if !floatEqual(got, -100.0) {
		t.Errorf("PercentChange(0, 100) = %v, want -100", got)
	}
}

// TestPercentChange_NegativeBase pins CB3-E: the |previous| denominator is
// the deliberate signed-base convention (see PercentChange's doc comment),
// not an abs-per-transaction bug. Both current and previous are negative
// here, unlike TestPercentChange_NegativePrevious above (positive current,
// negative previous) -- this covers a negative CURRENT too, e.g. a
// refund-dominant period's signed net (CB3-A/CB3-D) compared against a
// prior refund-dominant period.
func TestPercentChange_NegativeBase(t *testing.T) {
	// -1000 -> -500: less negative = improvement.
	// ((-500 - (-1000)) / abs(-1000)) * 100 = (500/1000)*100 = 50
	got := PercentChange(-500, -1000)
	if !floatEqual(got, 50.0) {
		t.Errorf("PercentChange(-500, -1000) = %v, want 50", got)
	}

	// -1000 -> -1500: more negative = worse.
	// ((-1500 - (-1000)) / abs(-1000)) * 100 = (-500/1000)*100 = -50
	got = PercentChange(-1500, -1000)
	if !floatEqual(got, -50.0) {
		t.Errorf("PercentChange(-1500, -1000) = %v, want -50", got)
	}
}

// --- Calculate ---

func TestCalculateMetrics_BasicTotals(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Bonus", 1000, time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1500, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Groceries", -500, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
	)

	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 0, fullCoverage, true, nil)

	if !floatEqual(m.TotalIncome, 6000) {
		t.Errorf("TotalIncome = %v, want 6000", m.TotalIncome)
	}
	if !floatEqual(m.TotalExpenses, 2000) {
		t.Errorf("TotalExpenses = %v, want 2000", m.TotalExpenses)
	}
	if !floatEqual(m.NetSavings, 4000) {
		t.Errorf("NetSavings = %v, want 4000", m.NetSavings)
	}
	// SavingsRate = (4000/6000)*100 = 66.67
	expectedRate := (4000.0 / 6000.0) * 100
	if !floatEqual(m.SavingsRate, expectedRate) {
		t.Errorf("SavingsRate = %v, want %v", m.SavingsRate, expectedRate)
	}
	if m.TransactionCount != 4 {
		t.Errorf("TransactionCount = %v, want 4", m.TransactionCount)
	}
}

// TestCalculateMetrics_ExcludesTransfers is the metrics-side half of the
// transfer contract (GLOSSARY: "Transfer"). Both legs of a paired transfer
// and an external leg are present in the set and counted in
// TransactionCount, but must appear in neither TotalIncome nor
// TotalExpenses -- nor in the trends, which are built from the same by-type
// buckets. The transfer amounts here are larger than the real ones, so a leg
// leaking back in cannot be mistaken for a rounding difference.
func TestCalculateMetrics_ExcludesTransfers(t *testing.T) {
	paired := makeTransaction("Schwab MoneyLink", -9000, time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC), models.Transfer, "Transfer")
	paired.TransferClass = "paired"
	paired.TransferPairKey = "abc123abc123"
	counterLeg := makeTransaction("Transfer in from Schwab", 9000, time.Date(2025, 1, 9, 0, 0, 0, 0, time.UTC), models.Transfer, "Deposit")
	counterLeg.TransferClass = "paired"
	counterLeg.TransferPairKey = "abc123abc123"
	external := makeTransaction("Vanguard buy investment", -7000, time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC), models.Transfer, "Investing")
	external.TransferClass = "external"

	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1500, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		paired, counterLeg, external,
	)

	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 0, fullCoverage, true, nil)

	if m.TransactionCount != 5 {
		t.Fatalf("TransactionCount = %v, want 5 -- transfers stay in the ledger", m.TransactionCount)
	}
	if !floatEqual(m.TotalIncome, 5000) {
		t.Errorf("TotalIncome = %v, want 5000 (the 9000 credit leg must be excluded)", m.TotalIncome)
	}
	if !floatEqual(m.TotalExpenses, 1500) {
		t.Errorf("TotalExpenses = %v, want 1500 (the 9000 debit and 7000 external legs must be excluded)", m.TotalExpenses)
	}
	if !floatEqual(m.NetSavings, 3500) {
		t.Errorf("NetSavings = %v, want 3500", m.NetSavings)
	}
	if !floatEqual(m.SavingsRate, (3500.0/5000.0)*100) {
		t.Errorf("SavingsRate = %v, want %v", m.SavingsRate, (3500.0/5000.0)*100)
	}
	if len(m.IncomeTrend) != 1 || !floatEqual(m.IncomeTrend[0], 5000) {
		t.Errorf("IncomeTrend = %v, want [5000]", m.IncomeTrend)
	}
	if len(m.ExpensesTrend) != 1 || !floatEqual(m.ExpensesTrend[0], 1500) {
		t.Errorf("ExpensesTrend = %v, want [1500]", m.ExpensesTrend)
	}
}

func TestCalculateMetrics_ZeroIncome(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -1500, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 0, fullCoverage, true, nil)

	if m.SavingsRate != 0 {
		t.Errorf("SavingsRate = %v, want 0 when no income", m.SavingsRate)
	}
	if !floatEqual(m.TotalIncome, 0) {
		t.Errorf("TotalIncome = %v, want 0", m.TotalIncome)
	}
	if !floatEqual(m.TotalExpenses, 1500) {
		t.Errorf("TotalExpenses = %v, want 1500", m.TotalExpenses)
	}
}

func TestCalculateMetrics_TrendsLimitedToSixMonths(t *testing.T) {
	var txns []models.Transaction
	// Create 8 months of data
	for i := 0; i < 8; i++ {
		date := time.Date(2025, time.Month(i+1), 15, 0, 0, 0, 0, time.UTC)
		txns = append(txns, makeTransaction("Salary", 5000, date, models.Income, "Payroll"))
		txns = append(txns, makeTransaction("Rent", -1500, date, models.Outflow, "Housing"))
	}
	ts := makeTransactionSet(txns...)

	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 0, fullCoverage, true, nil)

	if len(m.IncomeTrend) != 6 {
		t.Errorf("IncomeTrend length = %v, want 6", len(m.IncomeTrend))
	}
	if len(m.ExpensesTrend) != 6 {
		t.Errorf("ExpensesTrend length = %v, want 6", len(m.ExpensesTrend))
	}
	if len(m.SavingsTrend) != 6 {
		t.Errorf("SavingsTrend length = %v, want 6", len(m.SavingsTrend))
	}
	if len(m.TrendLabels) != 6 {
		t.Errorf("TrendLabels length = %v, want 6", len(m.TrendLabels))
	}

	// Should start from month 3 (index 2) since we drop the first 2
	if len(m.TrendLabels) == 6 && m.TrendLabels[0] != "2025-03" {
		t.Errorf("TrendLabels[0] = %v, want 2025-03", m.TrendLabels[0])
	}
}

func TestCalculateMetrics_EmptyTransactionSet(t *testing.T) {
	ts := makeTransactionSet()
	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 0, fullCoverage, true, nil)

	if m.TotalIncome != 0 || m.TotalExpenses != 0 || m.NetSavings != 0 {
		t.Errorf("expected all zeros, got income=%v, expenses=%v, savings=%v",
			m.TotalIncome, m.TotalExpenses, m.NetSavings)
	}
	if m.TransactionCount != 0 {
		t.Errorf("TransactionCount = %d, want 0", m.TransactionCount)
	}
	if len(m.IncomeTrend) != 0 {
		t.Errorf("IncomeTrend should be empty, got %d items", len(m.IncomeTrend))
	}
}

// --- Calculate: budget tracking ---

func TestCalculateMetrics_MonthsInRange_ApproxFromDates(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -1000, time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC) // 90-day inclusive span

	m := Calculate(ts, start, end, 0, 0, fullCoverage, true, nil)

	// Jan 1 to Mar 31: end.Sub(start) = 89 days; +1 for inclusive = 90 days / 30.4375 ≈ 2.957
	if m.MonthsInRange < 2.90 || m.MonthsInRange > 3.05 {
		t.Errorf("MonthsInRange = %v, want ~2.957 (Jan-Mar inclusive span)", m.MonthsInRange)
	}
}

func TestCalculateMetrics_ActualMonthly_DividesExpensesByMonths(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Groceries", -3000, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		makeTransaction("Gas", -3000, time.Date(2025, 3, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Auto"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC) // ~3 months

	m := Calculate(ts, start, end, 0, 0, fullCoverage, true, nil)

	if !floatEqual(m.TotalExpenses, 9000) {
		t.Fatalf("precondition: TotalExpenses = %v, want 9000", m.TotalExpenses)
	}
	// 9000 / ~2.99 ≈ 3010
	if m.ActualMonthly < 2950 || m.ActualMonthly > 3050 {
		t.Errorf("ActualMonthly = %v, want ~3010", m.ActualMonthly)
	}
}

func TestCalculateMetrics_BudgetOverTarget(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -6000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Groceries", -6000, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		makeTransaction("Gas", -6000, time.Date(2025, 3, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Auto"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC) // ~3 months

	target := 5000.0
	m := Calculate(ts, start, end, target, 0, fullCoverage, true, nil)

	if !m.HasBudgetTarget {
		t.Errorf("HasBudgetTarget = false, want true (target=%v)", target)
	}
	if !floatEqual(m.BudgetTarget, target) {
		t.Errorf("BudgetTarget = %v, want %v", m.BudgetTarget, target)
	}
	// ActualMonthly ≈ 6080; PerMonthDelta = 6080 - 5000 = ~1080
	// (90 days / 30.4375 ≈ 2.957 months; 18000/2.957 ≈ 6088)
	if m.PerMonthDelta < 950 || m.PerMonthDelta > 1200 {
		t.Errorf("PerMonthDelta = %v, want ~1080 (over)", m.PerMonthDelta)
	}
	// CumulativeDelta = 18000 - 5000 * 2.957 = 18000 - 14784 = ~3216
	if m.CumulativeDelta < 3000 || m.CumulativeDelta > 3400 {
		t.Errorf("CumulativeDelta = %v, want ~3216 (over)", m.CumulativeDelta)
	}
}

func TestCalculateMetrics_BudgetUnderTarget(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Groceries", -3000, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		makeTransaction("Gas", -3000, time.Date(2025, 3, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Auto"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)

	m := Calculate(ts, start, end, 5000, 0, fullCoverage, true, nil)

	// ActualMonthly ≈ 3044; PerMonthDelta = 3044 - 5000 = ~-1956
	// (90 days / 30.4375 ≈ 2.957 months; 9000/2.957 ≈ 3044)
	if m.PerMonthDelta > -1800 || m.PerMonthDelta < -2100 {
		t.Errorf("PerMonthDelta = %v, want ~-1956 (under)", m.PerMonthDelta)
	}
	// CumulativeDelta = 9000 - 5000*2.957 = 9000 - 14784 = ~-5784
	if m.CumulativeDelta > -5600 || m.CumulativeDelta < -6000 {
		t.Errorf("CumulativeDelta = %v, want ~-5784 (under)", m.CumulativeDelta)
	}
}

func TestCalculateMetrics_NoBudgetTarget(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	m := Calculate(ts, start, end, 0, 0, fullCoverage, true, nil)

	if m.HasBudgetTarget {
		t.Errorf("HasBudgetTarget = true, want false when target=0")
	}
	if !floatEqual(m.BudgetTarget, 0) {
		t.Errorf("BudgetTarget = %v, want 0", m.BudgetTarget)
	}
	// ActualMonthly should still be computed
	if m.ActualMonthly == 0 {
		t.Errorf("ActualMonthly = 0, want non-zero (TotalExpenses > 0 even without target)")
	}
	// PerMonthDelta = ActualMonthly - 0 = ActualMonthly
	if !floatEqual(m.PerMonthDelta, m.ActualMonthly) {
		t.Errorf("PerMonthDelta = %v, want ActualMonthly (%v) when target=0", m.PerMonthDelta, m.ActualMonthly)
	}
	// CumulativeDelta = TotalExpenses - 0 = TotalExpenses
	if !floatEqual(m.CumulativeDelta, m.TotalExpenses) {
		t.Errorf("CumulativeDelta = %v, want TotalExpenses (%v) when target=0", m.CumulativeDelta, m.TotalExpenses)
	}
}

// --- Healthcare KPI ---

func TestCalculateMetrics_HealthcareUnderTarget(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Premium", -1500, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
		makeTransaction("Premium", -1500, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC) // ~1.94 months

	m := Calculate(ts, start, end, 0, 2000, fullCoverage, true, nil)

	if !m.HasHealthcareTarget {
		t.Errorf("HasHealthcareTarget = false, want true (target=2000)")
	}
	if !floatEqual(m.HealthcareTarget, 2000) {
		t.Errorf("HealthcareTarget = %v, want 2000", m.HealthcareTarget)
	}
	if !floatEqual(m.HealthcareTotal, 3000) {
		t.Errorf("HealthcareTotal = %v, want 3000", m.HealthcareTotal)
	}
	// HealthcareActual ≈ 3000 / 1.939 ≈ 1547
	if m.HealthcareActual < 1500 || m.HealthcareActual > 1600 {
		t.Errorf("HealthcareActual = %v, want ~1547", m.HealthcareActual)
	}
	// PerMonthDelta = ~1547 - 2000 ≈ -453 (under)
	if m.HealthcarePerMonthDelta > -400 || m.HealthcarePerMonthDelta < -500 {
		t.Errorf("HealthcarePerMonthDelta = %v, want ~-453 (under)", m.HealthcarePerMonthDelta)
	}
	// CumulativeDelta = 3000 - 2000*1.939 ≈ -879 (under)
	if m.HealthcareCumulativeDelta > -800 || m.HealthcareCumulativeDelta < -950 {
		t.Errorf("HealthcareCumulativeDelta = %v, want ~-879 (under)", m.HealthcareCumulativeDelta)
	}
}

func TestCalculateMetrics_HealthcareOverTarget(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Premium", -2500, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
		makeTransaction("Premium", -2500, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	m := Calculate(ts, start, end, 0, 2000, fullCoverage, true, nil)

	if m.HealthcarePerMonthDelta <= 0 {
		t.Errorf("HealthcarePerMonthDelta = %v, want positive (over)", m.HealthcarePerMonthDelta)
	}
	if m.HealthcareCumulativeDelta <= 0 {
		t.Errorf("HealthcareCumulativeDelta = %v, want positive (over)", m.HealthcareCumulativeDelta)
	}
}

func TestCalculateMetrics_HealthcareIgnoresOtherCategories(t *testing.T) {
	// Health & Fitness, medical co-pays, anything outside "Health Insurance"
	// must NOT count toward the premium KPI — the user splits premiums vs
	// extra costs by tagging.
	ts := makeTransactionSet(
		makeTransaction("Fitbit", -86, time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC), models.Outflow, "Health & Fitness"),
		makeTransaction("Copay", -50, time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC), models.Outflow, "Medical"),
		makeTransaction("Premium", -1500, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	m := Calculate(ts, start, end, 0, 1500, fullCoverage, true, nil)
	if !floatEqual(m.HealthcareTotal, 1500) {
		t.Errorf("HealthcareTotal = %v, want 1500 (only Health Insurance counts)", m.HealthcareTotal)
	}
}

func TestCalculateMetrics_HealthcareCategoryCaseInsensitive(t *testing.T) {
	// FilterByCategory uses case-insensitive match — verify that holds for
	// users whose CSVs export "health insurance" with different casing.
	ts := makeTransactionSet(
		makeTransaction("Premium", -1500, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "health insurance"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	m := Calculate(ts, start, end, 0, 1500, fullCoverage, true, nil)
	if !floatEqual(m.HealthcareTotal, 1500) {
		t.Errorf("HealthcareTotal = %v, want 1500 (case-insensitive match)", m.HealthcareTotal)
	}
}

func TestCalculateMetrics_NoHealthcareTarget(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Premium", -1500, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	m := Calculate(ts, start, end, 0, 0, fullCoverage, true, nil)
	if m.HasHealthcareTarget {
		t.Errorf("HasHealthcareTarget = true, want false when target=0")
	}
	// Actual still computed (so the user can see spend even without a target).
	if m.HealthcareTotal == 0 {
		t.Errorf("HealthcareTotal = 0, want 1500")
	}
}

func TestCalculateMetrics_LivingExpensesExcludeHealthInsurance(t *testing.T) {
	// Living-vs-target variance must NOT include Health Insurance
	// premiums — those are tracked by the Healthcare KPI. Without this
	// split, $X premium spend would inflate Living variance + the
	// Budget cumulative card by the same $X tracked elsewhere.
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Groceries", -500, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		makeTransaction("Premium", -1500, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC) // ~1 month

	m := Calculate(ts, start, end, 4000, 1500, fullCoverage, true, nil)

	// TotalExpenses keeps every outflow (Total Expenses card unchanged)
	if !floatEqual(m.TotalExpenses, 5000) {
		t.Errorf("TotalExpenses = %v, want 5000 (all outflows)", m.TotalExpenses)
	}
	// LivingExpensesTotal subtracts the $1500 premium
	if !floatEqual(m.LivingExpensesTotal, 3500) {
		t.Errorf("LivingExpensesTotal = %v, want 3500 (5000 - 1500 healthcare)", m.LivingExpensesTotal)
	}
	// ActualMonthly tracks LIVING only — ~3500 / ~1.02 months ≈ 3434
	if m.ActualMonthly < 3300 || m.ActualMonthly > 3600 {
		t.Errorf("ActualMonthly = %v, want ~3434 (living only, not 4900 total)", m.ActualMonthly)
	}
	// PerMonthDelta = ActualMonthly - 4000 ≈ -566 (under living target)
	if m.PerMonthDelta > -400 || m.PerMonthDelta < -700 {
		t.Errorf("PerMonthDelta = %v, want ~-566 (under target after excluding healthcare)", m.PerMonthDelta)
	}
	// Healthcare KPI still owns the $1500 premium
	if !floatEqual(m.HealthcareTotal, 1500) {
		t.Errorf("HealthcareTotal = %v, want 1500", m.HealthcareTotal)
	}
}

func TestCalculateMetrics_LivingExpensesTrendExcludesHealthcare(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, jan, models.Outflow, "Housing"),
		makeTransaction("Premium", -1500, jan, models.Outflow, "Health Insurance"),
		makeTransaction("Rent", -3000, feb, models.Outflow, "Housing"),
		makeTransaction("Premium", -1500, feb, models.Outflow, "Health Insurance"),
	)
	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 0, fullCoverage, true, nil)

	if len(m.LivingExpensesTrend) != len(m.TrendLabels) {
		t.Fatalf("LivingExpensesTrend length = %d, want %d", len(m.LivingExpensesTrend), len(m.TrendLabels))
	}
	for i, v := range m.LivingExpensesTrend {
		if !floatEqual(v, 3000) {
			t.Errorf("LivingExpensesTrend[%d] = %v, want 3000 (rent only, not 4500)", i, v)
		}
	}
	// ExpensesTrend (Total Expenses card) still shows the full 4500
	for i, v := range m.ExpensesTrend {
		if !floatEqual(v, 4500) {
			t.Errorf("ExpensesTrend[%d] = %v, want 4500 (all outflows)", i, v)
		}
	}
}

func TestCalculateMetrics_HealthcareTrendPopulated(t *testing.T) {
	var txns []models.Transaction
	for i := 0; i < 4; i++ {
		date := time.Date(2025, time.Month(i+1), 15, 0, 0, 0, 0, time.UTC)
		txns = append(txns, makeTransaction("Salary", 5000, date, models.Income, "Payroll"))
		txns = append(txns, makeTransaction("Premium", -1500, date, models.Outflow, "Health Insurance"))
	}
	ts := makeTransactionSet(txns...)
	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 1500, fullCoverage, true, nil)

	if len(m.HealthcareTrend) != len(m.TrendLabels) {
		t.Errorf("HealthcareTrend length = %d, want %d (aligned with TrendLabels)",
			len(m.HealthcareTrend), len(m.TrendLabels))
	}
	for i, v := range m.HealthcareTrend {
		if !floatEqual(v, 1500) {
			t.Errorf("HealthcareTrend[%d] = %v, want 1500", i, v)
		}
	}
}

// --- Combined plan variance (Budget card) ---

func TestCalculateMetrics_CombinedNetsLivingAndHealthcare(t *testing.T) {
	// Living over by 200/mo, Healthcare under by 500/mo → combined under 300/mo.
	// 1-month range, target Living=3000, Healthcare=2000.
	ts := makeTransactionSet(
		makeTransaction("Rent", -3200, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Premium", -1500, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	m := Calculate(ts, start, end, 3000, 2000, fullCoverage, true, nil)

	if !m.HasCombinedTarget {
		t.Errorf("HasCombinedTarget = false, want true")
	}
	if !floatEqual(m.CombinedTarget, 5000) {
		t.Errorf("CombinedTarget = %v, want 5000", m.CombinedTarget)
	}
	// CombinedActualMonthly ≈ (3200+1500)/1.018 ≈ 4615; under by ~385 vs 5000 target
	if m.CombinedPerMonthDelta > -300 || m.CombinedPerMonthDelta < -500 {
		t.Errorf("CombinedPerMonthDelta = %v, want ~-385 (net under)", m.CombinedPerMonthDelta)
	}
	if m.CombinedCumulativeDelta >= 0 {
		t.Errorf("CombinedCumulativeDelta = %v, want negative (net under)", m.CombinedCumulativeDelta)
	}
}

func TestCalculateMetrics_CombinedDegeneratesWhenOnlyOneTarget(t *testing.T) {
	// Only living target — combined target = living target only.
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	m := Calculate(ts, start, end, 4000, 0, fullCoverage, true, nil)
	if !floatEqual(m.CombinedTarget, 4000) {
		t.Errorf("CombinedTarget = %v, want 4000 (living only)", m.CombinedTarget)
	}
	// CombinedCumulativeDelta == CumulativeDelta when there's no healthcare
	if !floatEqual(m.CombinedCumulativeDelta, m.CumulativeDelta) {
		t.Errorf("CombinedCumulativeDelta = %v, want %v (matches living-only)", m.CombinedCumulativeDelta, m.CumulativeDelta)
	}
}

func TestCalculateMetrics_CumulativeTargetTotalsExposed(t *testing.T) {
	// Budget card needs the cumulative target totals (target × months) so
	// the user can read "Living spent X of Y" with the math working out.
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Premium", -1500, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC) // ~1.02 months

	m := Calculate(ts, start, end, 4000, 2000, fullCoverage, true, nil)

	wantLiving := 4000 * m.MonthsInRange
	wantHealth := 2000 * m.MonthsInRange
	if !floatEqual(m.LivingTargetTotal, wantLiving) {
		t.Errorf("LivingTargetTotal = %v, want %v (4000 × %v months)", m.LivingTargetTotal, wantLiving, m.MonthsInRange)
	}
	if !floatEqual(m.HealthcareTargetTotal, wantHealth) {
		t.Errorf("HealthcareTargetTotal = %v, want %v (2000 × %v months)", m.HealthcareTargetTotal, wantHealth, m.MonthsInRange)
	}
	// Composition check: living variance + healthcare variance == combined variance (within float precision).
	got := m.CumulativeDelta + m.HealthcareCumulativeDelta
	if !floatEqual(got, m.CombinedCumulativeDelta) {
		t.Errorf("CumulativeDelta + HealthcareCumulativeDelta = %v, want %v (CombinedCumulativeDelta)", got, m.CombinedCumulativeDelta)
	}
}

func TestCalculateMetrics_CombinedZeroTargets(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	m := Calculate(ts, start, end, 0, 0, fullCoverage, true, nil)
	if m.HasCombinedTarget {
		t.Errorf("HasCombinedTarget = true, want false when both targets are 0")
	}
}

func TestCalculateMetrics_SingleDayRange_NoDivideByZero(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -1000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)
	day := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Calculate panicked on single-day range: %v", r)
		}
	}()
	m := Calculate(ts, day, day, 5000, 0, fullCoverage, true, nil)

	// (0 + 1) / 30.4375 ≈ 0.0329
	if m.MonthsInRange < 0.03 || m.MonthsInRange > 0.04 {
		t.Errorf("MonthsInRange = %v, want ~0.033 (single-day span)", m.MonthsInRange)
	}
}

// --- Calculate: cumulative balance trend ---

func TestCalculateMetrics_CombinedCumulativeBalance_NoTargetReturnsNil(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1500, jan, models.Outflow, "Housing"),
		makeTransaction("Rent", -1500, feb, models.Outflow, "Housing"),
	)

	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 0, fullCoverage, true, nil)

	if m.CombinedCumulativeBalance != nil {
		t.Errorf("CombinedCumulativeBalance = %v, want nil when no combined target", m.CombinedCumulativeBalance)
	}
}

func TestCalculateMetrics_CombinedCumulativeBalance_AccumulatesMonthlyBalance(t *testing.T) {
	// Two calendar months, both fully inside the range: Jan (31 days) and
	// Feb (28 days). Target $1500/mo combined, no healthcare, actual spend
	// $1000 in Jan and $2000 in Feb. Each month's accrual is pro-rated by
	// MonthsBetween on that calendar month's own day count (not a flat
	// 1.0), so:
	//   Jan accrual = 1500 * (31/30.4375) = 1527.7207...
	//   Feb accrual = 1500 * (28/30.4375) = 1379.8768...
	//   Jan balance = 1527.7207 - 1000              =  527.7207
	//   Feb balance = 527.7207 + (1379.8768 - 2000) =  -92.4025
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1000, jan, models.Outflow, "Housing"),
		makeTransaction("Rent", -2000, feb, models.Outflow, "Housing"),
	)

	m := Calculate(ts, start, end, 1500, 0, fullCoverage, true, nil)

	if len(m.CombinedCumulativeBalance) != 2 {
		t.Fatalf("CombinedCumulativeBalance length = %d, want 2", len(m.CombinedCumulativeBalance))
	}
	if !floatEqual(m.CombinedCumulativeBalance[0], 527.72) {
		t.Errorf("CombinedCumulativeBalance[0] = %.4f, want ~527.7207 (1500*31/30.4375 - 1000)", m.CombinedCumulativeBalance[0])
	}
	if !floatEqual(m.CombinedCumulativeBalance[1], -92.40) {
		t.Errorf("CombinedCumulativeBalance[1] = %.4f, want ~-92.4025 (prior + 1500*28/30.4375 - 2000)", m.CombinedCumulativeBalance[1])
	}
}

func TestCalculateMetrics_CombinedCumulativeBalance_LastIsNegationOfCumulativeDelta(t *testing.T) {
	// Invariant: the last value of CombinedCumulativeBalance must equal the
	// EXACT negation of CombinedCumulativeDelta (float-summation noise
	// only -- not a month-rounding approximation). Both are now built from
	// the same calendar-month walk over [rangeStart, rangeEnd]: per-month
	// accruals sum to combinedTarget*MonthsInRange exactly because the
	// per-month day segments partition the range's inclusive days, and
	// per-month spends sum to totalExpenses exactly because callers pass a
	// range-pre-filtered TransactionSet. So running == -(actual-target) ==
	// -CombinedCumulativeDelta to within float64 noise, not dollars.
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1500, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Rent", -1500, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Rent", -1500, time.Date(2025, 3, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Premium", -400, time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
		makeTransaction("Premium", -400, time.Date(2025, 2, 6, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
		makeTransaction("Premium", -400, time.Date(2025, 3, 6, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)

	m := Calculate(ts, start, end, 1200, 350, fullCoverage, true, nil) // combined target = 1550

	if len(m.CombinedCumulativeBalance) == 0 {
		t.Fatalf("CombinedCumulativeBalance empty; want non-empty when combined target is set")
	}
	last := m.CombinedCumulativeBalance[len(m.CombinedCumulativeBalance)-1]
	if math.Abs(last-(-m.CombinedCumulativeDelta)) > 0.01 {
		t.Errorf("balance tail %.4f vs -CombinedCumulativeDelta %.4f — must agree exactly (float noise only, no month-rounding slack)", last, -m.CombinedCumulativeDelta)
	}
}

func TestCalculateMetrics_CombinedCumulativeBalance_MoreThanSixMonths_CapsAndCarriesIn(t *testing.T) {
	// 8 calendar months (Jan-Aug 2025), combined target $1000/mo, $500
	// spend each month (Housing only). The walked series has 8 points but
	// the display cap keeps only the LAST 6 (Mar-Aug); the running totals
	// those 6 points carry are not reset at the cap boundary -- Aug's
	// point still reflects Jan+Feb's carry-in. Proven by checking the tail
	// against -CombinedCumulativeDelta computed over the FULL 8-month
	// range: if the cap zeroed the carry-in instead of just trimming the
	// display, the tail would be off by the dropped months' net
	// contribution and the invariant would fail.
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 8, 31, 0, 0, 0, 0, time.UTC)
	var txns []models.Transaction
	for i := 0; i < 8; i++ {
		date := time.Date(2025, time.Month(i+1), 15, 0, 0, 0, 0, time.UTC)
		txns = append(txns, makeTransaction("Rent", -500, date, models.Outflow, "Housing"))
	}
	ts := makeTransactionSet(txns...)

	m := Calculate(ts, start, end, 1000, 0, fullCoverage, true, nil)

	if len(m.CombinedCumulativeBalance) != 6 {
		t.Fatalf("CombinedCumulativeBalance length = %d, want 6 (capped to last 6 of 8 walked months)", len(m.CombinedCumulativeBalance))
	}
	last := m.CombinedCumulativeBalance[len(m.CombinedCumulativeBalance)-1]
	if math.Abs(last-(-m.CombinedCumulativeDelta)) > 0.01 {
		t.Errorf("balance tail %.4f vs -CombinedCumulativeDelta %.4f — must agree (carry-in must survive the 6-month display cap)", last, -m.CombinedCumulativeDelta)
	}
}

func TestCalculateMetrics_CombinedCumulativeBalance_PartialMonthRange(t *testing.T) {
	// Range starts mid-January and ends mid-March -- neither endpoint is a
	// full calendar month. Each of the 3 intersecting calendar months
	// contributes only its intersection with [start, end], so accruals
	// still sum to combinedTarget*MonthsInRange exactly (day-partitioned).
	start := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -600, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Rent", -700, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Rent", -800, time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	m := Calculate(ts, start, end, 750, 0, fullCoverage, true, nil)

	if len(m.CombinedCumulativeBalance) != 3 {
		t.Fatalf("CombinedCumulativeBalance length = %d, want 3 (Jan, Feb, Mar)", len(m.CombinedCumulativeBalance))
	}
	last := m.CombinedCumulativeBalance[len(m.CombinedCumulativeBalance)-1]
	if math.Abs(last-(-m.CombinedCumulativeDelta)) > 0.01 {
		t.Errorf("balance tail %.4f vs -CombinedCumulativeDelta %.4f — must agree even with partial first/last calendar months", last, -m.CombinedCumulativeDelta)
	}
}

func TestCalculateMetrics_CombinedCumulativeBalance_ZeroTransactionMiddleMonth(t *testing.T) {
	// Jan and Mar have transactions; Feb has none at all. Feb must still
	// get a walked point (target accrues, nothing spent) rather than being
	// skipped -- skipping it would both undercount the series length and
	// break the tail invariant (Feb's accrual would never be added).
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Rent", -1000, time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	m := Calculate(ts, start, end, 800, 0, fullCoverage, true, nil)

	if len(m.CombinedCumulativeBalance) != 3 {
		t.Fatalf("CombinedCumulativeBalance length = %d, want 3 (Jan, Feb, Mar -- Feb still gets a point)", len(m.CombinedCumulativeBalance))
	}
	last := m.CombinedCumulativeBalance[len(m.CombinedCumulativeBalance)-1]
	if math.Abs(last-(-m.CombinedCumulativeDelta)) > 0.01 {
		t.Errorf("balance tail %.4f vs -CombinedCumulativeDelta %.4f — must agree even with an empty middle month", last, -m.CombinedCumulativeDelta)
	}
}

// CB1 regression: a refund-dominant month's outflow-typed rows net POSITIVE
// (e.g. a furniture-store refund larger than the month's other spending),
// and must enter the walk as a CREDIT, never charged as spend via
// math.Abs. A single-month fixture cannot discriminate per-month-abs from
// signed arithmetic (both give the same magnitude), so this uses two
// months: Jan is an ordinary spend month, Feb is refund-dominant.
func TestCalculateMetrics_CombinedCumulativeBalance_RefundDominantMonthEntersAsCredit(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1800, time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		// Feb's outflow-typed rows net +900 (refund-dominant): a $1100
		// furniture-store return exceeds the month's $200 utility bill.
		makeTransaction("Furniture return", 1100, time.Date(2025, 2, 3, 0, 0, 0, 0, time.UTC), models.Outflow, "Furniture"),
		makeTransaction("Utilities", -200, time.Date(2025, 2, 20, 0, 0, 0, 0, time.UTC), models.Outflow, "Utilities"),
	)

	m := Calculate(ts, start, end, 1200, 0, fullCoverage, true, nil)

	if len(m.CombinedCumulativeBalance) != 2 {
		t.Fatalf("CombinedCumulativeBalance length = %d, want 2", len(m.CombinedCumulativeBalance))
	}

	// Harness validity guard: Jan is an ordinary spend month, identical
	// under the bug and the fix. Jan accrual = 1200*(31/30.4375) =
	// 1222.1766; balance = accrual - 1800.
	if !floatEqual(m.CombinedCumulativeBalance[0], -577.82) {
		t.Errorf("harness error, not the defect under test: Jan point = %.4f, want ~-577.8234", m.CombinedCumulativeBalance[0])
	}

	// The discriminator: Feb's step must be its accrual PLUS the $900 net
	// refund (a credit), not accrual minus |900| (which is what math.Abs
	// on a net-positive outflow bucket produces).
	// Feb accrual = 1200*(28/30.4375) = 1103.9014.
	step := m.CombinedCumulativeBalance[1] - m.CombinedCumulativeBalance[0]
	if math.Abs(step-2003.9014) > 0.01 {
		t.Errorf("refund-dominant month mis-signed: Feb step = %.4f, want ~2003.9014 (accrual 1103.9014 + 900 credit); per-month abs would give ~203.9014", step)
	}

	// The documented invariant must hold WITH the refund-dominant month
	// present -- it is broken under math.Abs because the sign flip means
	// per-month spends no longer partition TotalExpenses.
	last := m.CombinedCumulativeBalance[len(m.CombinedCumulativeBalance)-1]
	if math.Abs(last-(-m.CombinedCumulativeDelta)) > 0.01 {
		t.Errorf("invariant broken with a refund-dominant month present: last walk point = %.4f, -CombinedCumulativeDelta = %.4f", last, -m.CombinedCumulativeDelta)
	}
}

// --- currentHealthcareTarget ---

func TestCurrentHealthcareTarget_NilSettings(t *testing.T) {
	if got := currentHealthcareTarget(nil); got != 0 {
		t.Errorf("currentHealthcareTarget(nil) = %v, want 0", got)
	}
}

func TestCurrentHealthcareTarget_NoHealthcareConfigured(t *testing.T) {
	s := &models.WhatIfSettings{MonthlyLivingExpenses: 5000}
	if got := currentHealthcareTarget(s); got != 0 {
		t.Errorf("currentHealthcareTarget(empty) = %v, want 0", got)
	}
}

func TestCurrentHealthcareTarget_LegacySingleValue(t *testing.T) {
	// Legacy single-person model: HealthcareStartYears=0 means active at month 0.
	s := &models.WhatIfSettings{
		MonthlyHealthcare:    1200,
		HealthcareStartYears: 0,
	}
	if got := currentHealthcareTarget(s); !floatEqual(got, 1200) {
		t.Errorf("currentHealthcareTarget(legacy) = %v, want 1200", got)
	}
}

// --- phaseAdjustedMonthlyTarget ---

func TestPhaseAdjustedMonthlyTarget_NilSettings(t *testing.T) {
	got := phaseAdjustedMonthlyTarget(nil,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
	if got != 0 {
		t.Errorf("phaseAdjustedMonthlyTarget(nil, ...) = %v, want 0", got)
	}
}

func TestPhaseAdjustedMonthlyTarget_ZeroBaseExpenses(t *testing.T) {
	s := makePhaseSettings(t, 0, "2025-01", 70, true)
	got := phaseAdjustedMonthlyTarget(s,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
	if got != 0 {
		t.Errorf("phaseAdjustedMonthlyTarget with zero MonthlyLivingExpenses = %v, want 0", got)
	}
}

func TestPhaseAdjustedMonthlyTarget_PhasesDisabled_PassesBaseThrough(t *testing.T) {
	s := makePhaseSettings(t, 5000, "2025-01", 80, false) // No-Go age, but phases off
	got := phaseAdjustedMonthlyTarget(s,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
	if !floatEqual(got, 5000) {
		t.Errorf("phases disabled → got %v, want flat 5000", got)
	}
}

func TestPhaseAdjustedMonthlyTarget_GoGoPhase_NoChange(t *testing.T) {
	// Default Go-Go starts at age 0 → multiplier 1.0
	// Primary is 60 at start → still in Go-Go (next phase Active@65)
	s := makePhaseSettings(t, 5000, "2025-01", 60, true)
	got := phaseAdjustedMonthlyTarget(s,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
	if !floatEqual(got, 5000) {
		t.Errorf("Go-Go (mult 1.0) → got %v, want 5000", got)
	}
}

func TestPhaseAdjustedMonthlyTarget_NoGoPhase_AppliesMultiplier(t *testing.T) {
	// Primary is 86 at start → No-Go phase (multiplier 0.65)
	s := makePhaseSettings(t, 5000, "2025-01", 86, true)
	got := phaseAdjustedMonthlyTarget(s,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
	want := 5000 * 0.65
	if !floatEqual(got, want) {
		t.Errorf("No-Go (mult 0.65) → got %v, want %v", got, want)
	}
}

func TestPhaseAdjustedMonthlyTarget_StraddlesPhaseTransition(t *testing.T) {
	// Primary turns 65 at start (Jan 2025): 12-month range Jan-Dec 2025.
	// All 12 months are at age 65+ in Active phase (mult 0.95).
	// (Phase boundaries are integer ages; the user is age 65 throughout.)
	s := makePhaseSettings(t, 5000, "2025-01", 65, true)
	got := phaseAdjustedMonthlyTarget(s,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
	want := 5000 * 0.95
	if !floatEqual(got, want) {
		t.Errorf("12 months at age 65 (Active 0.95) → got %v, want %v", got, want)
	}

	// Now: primary is 64 at start, range spans Jan 2025 → Dec 2026 (24
	// months). Year 1 (age 64) is Go-Go (1.00); year 2 (age 65) is
	// Active (0.95). Average multiplier = (12*1.00 + 12*0.95)/24 = 0.975.
	s64 := makePhaseSettings(t, 5000, "2025-01", 64, true)
	got2 := phaseAdjustedMonthlyTarget(s64,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	want2 := 5000 * 0.975
	if !floatEqual(got2, want2) {
		t.Errorf("range straddles Go-Go→Active → got %v, want %v", got2, want2)
	}
}

// --- TargetProvenance ---

func TestTargetProvenance_NilSettings(t *testing.T) {
	got := TargetProvenance(nil,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
	if got.Annotate {
		t.Errorf("Annotate = true, want false for nil settings")
	}
	if got != (BudgetTargetProvenance{}) {
		t.Errorf("TargetProvenance(nil, ...) = %+v, want zero value", got)
	}
}

func TestTargetProvenance_ZeroBaseExpenses(t *testing.T) {
	s := makePhaseSettings(t, 0, "2025-01", 70, true)
	got := TargetProvenance(s,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
	if got.Annotate {
		t.Errorf("Annotate = true, want false when MonthlyLivingExpenses is zero")
	}
	if got.Base != 0 {
		t.Errorf("Base = %v, want 0 (no budget configured sentinel)", got.Base)
	}
}

func TestTargetProvenance_PhasesDisabled(t *testing.T) {
	s := makePhaseSettings(t, 5000, "2025-01", 86, false) // No-Go age, but phases off
	got := TargetProvenance(s,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
	if got.Annotate {
		t.Errorf("Annotate = true, want false when phases are disabled")
	}
	if !floatEqual(got.Base, 5000) {
		t.Errorf("Base = %v, want 5000", got.Base)
	}
	if !floatEqual(got.Multiplier, 1.0) {
		t.Errorf("Multiplier = %v, want 1.0 when phases disabled", got.Multiplier)
	}
}

func TestTargetProvenance_NoPhaseConfig(t *testing.T) {
	s := makePhaseSettings(t, 5000, "2025-01", 86, true)
	s.SpendingPhaseConfig = nil
	got := TargetProvenance(s,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
	if got.Annotate {
		t.Errorf("Annotate = true, want false when no phase config exists")
	}
}

func TestTargetProvenance_GoGoPhase_MultiplierOne_NoAnnotate(t *testing.T) {
	// Default Go-Go starts at age 0 -> multiplier 1.0. Primary is 60 at
	// start -> still in Go-Go for the whole 12-month range. Annotating a
	// 1.0 multiplier adds noise, not information -- must not annotate.
	s := makePhaseSettings(t, 5000, "2025-01", 60, true)
	got := TargetProvenance(s,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
	if got.Annotate {
		t.Errorf("Annotate = true, want false when effective multiplier is 1.0")
	}
	if !floatEqual(got.Multiplier, 1.0) {
		t.Errorf("Multiplier = %v, want 1.0", got.Multiplier)
	}
}

func TestTargetProvenance_SinglePhaseAcrossRange(t *testing.T) {
	// Primary is 86 at start -> No-Go phase (multiplier 0.65) for the
	// whole 12-month range: single phase, no straddling.
	s := makePhaseSettings(t, 5000, "2025-01", 86, true)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)

	got := TargetProvenance(s, start, end)
	if !got.Annotate {
		t.Fatalf("Annotate = false, want true (multiplier 0.65 != 1.0)")
	}
	if !floatEqual(got.Base, 5000) {
		t.Errorf("Base = %v, want 5000", got.Base)
	}
	if !floatEqual(got.Multiplier, 0.65) {
		t.Errorf("Multiplier = %v, want 0.65", got.Multiplier)
	}
	if got.PhaseName != "No-Go" {
		t.Errorf("PhaseName = %q, want %q", got.PhaseName, "No-Go")
	}
	if got.Straddles {
		t.Errorf("Straddles = true, want false (single phase across the whole range)")
	}
	// Cross-check: target/base must equal the walk's own multiplier
	// (the brief requires reading it from the walk, not re-deriving by
	// division -- this asserts the two never drift apart).
	target := phaseAdjustedMonthlyTarget(s, start, end)
	if !floatEqual(target/got.Base, got.Multiplier) {
		t.Errorf("target/base = %v, want got.Multiplier %v", target/got.Base, got.Multiplier)
	}
}

func TestTargetProvenance_StraddlesTransition_WeightedAverage(t *testing.T) {
	// Primary is 64 at start, range spans Jan 2025 -> Dec 2026 (24
	// months). Year 1 (age 64) is Go-Go (1.00); year 2 (age 65) is
	// Active (0.95). Average multiplier = (12*1.00 + 12*0.95)/24 = 0.975.
	s := makePhaseSettings(t, 5000, "2025-01", 64, true)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	got := TargetProvenance(s, start, end)
	if !got.Annotate {
		t.Fatalf("Annotate = false, want true")
	}
	if !floatEqual(got.Multiplier, 0.975) {
		t.Errorf("Multiplier = %v, want 0.975 (weighted average)", got.Multiplier)
	}
	if !got.Straddles {
		t.Errorf("Straddles = false, want true (range crosses Go-Go -> Active)")
	}
	if got.PhaseName != "Go-Go" {
		t.Errorf("PhaseName = %q, want %q (phase active at range start)", got.PhaseName, "Go-Go")
	}
}

func TestPhaseAdjustedMonthlyTarget_FlowsIntoCalculateMetrics(t *testing.T) {
	// Verify the full pipeline: phase-adjusted target reaches DashboardMetrics.
	s := makePhaseSettings(t, 5000, "2025-01", 86, true) // No-Go (0.65)
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	target := phaseAdjustedMonthlyTarget(s, start, end)
	m := Calculate(ts, start, end, target, 0, fullCoverage, true, nil)

	if !m.HasBudgetTarget {
		t.Fatalf("HasBudgetTarget = false, want true")
	}
	if !floatEqual(m.BudgetTarget, 3250) { // 5000 * 0.65
		t.Errorf("BudgetTarget = %v, want 3250 (No-Go-adjusted)", m.BudgetTarget)
	}
}

// --- HealthcareCoverageStart (HC1) ---

func TestHealthcareCoverageStart_EarliestBillSelected(t *testing.T) {
	// Bills out of chronological order in the slice -- the earliest must
	// win regardless of position.
	ts := makeTransactionSet(
		makeTransaction("Premium", -900, time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
		makeTransaction("Premium", -900, time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
		makeTransaction("Premium", -900, time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)
	start, ok := HealthcareCoverageStart(ts)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC)
	if !start.Equal(want) {
		t.Errorf("HealthcareCoverageStart = %v, want %v (earliest bill)", start, want)
	}
}

func TestHealthcareCoverageStart_RefundsIgnored(t *testing.T) {
	// The earliest transaction overall is a positive-amount refund; it must
	// not set coverage start. The earliest NEGATIVE bill (May) wins instead.
	ts := makeTransactionSet(
		makeTransaction("Refund", 200, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
		makeTransaction("Premium", -900, time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)
	start, ok := HealthcareCoverageStart(ts)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	if !start.Equal(want) {
		t.Errorf("HealthcareCoverageStart = %v, want %v (refund must not count)", start, want)
	}
}

func TestHealthcareCoverageStart_NoQualifyingRows_ReturnsFalse(t *testing.T) {
	// A refund-only Health Insurance history, plus unrelated spend, yields
	// no coverage start at all.
	ts := makeTransactionSet(
		makeTransaction("Rent", -1500, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Refund", 200, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)
	if _, ok := HealthcareCoverageStart(ts); ok {
		t.Error("expected ok=false when no negative-amount Health Insurance outflow exists")
	}
}

func TestHealthcareCoverageStart_EmptySet_ReturnsFalse(t *testing.T) {
	if _, ok := HealthcareCoverageStart(makeTransactionSet()); ok {
		t.Error("expected ok=false for an empty transaction set")
	}
}

func TestHealthcareCoverageStart_IgnoresNonOutflowType(t *testing.T) {
	// A Health Insurance-categorized Income row (e.g. a misfiled deposit)
	// must not count even though its category matches -- only Outflow-typed
	// rows qualify.
	ts := makeTransactionSet(
		makeTransaction("Weird deposit", -900, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), models.Income, "Health Insurance"),
		makeTransaction("Premium", -900, time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)
	start, ok := HealthcareCoverageStart(ts)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	if !start.Equal(want) {
		t.Errorf("HealthcareCoverageStart = %v, want %v (Income-typed row must not count)", start, want)
	}
}

// --- ClippedHealthcareMonths (HC1) ---

func TestClippedHealthcareMonths_NoCoverageFlag(t *testing.T) {
	segStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	segEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	if got := ClippedHealthcareMonths(segStart, segEnd, segStart, false); got != 0 {
		t.Errorf("ClippedHealthcareMonths(hasCoverage=false) = %v, want 0", got)
	}
}

func TestClippedHealthcareMonths_CoverageAfterSegment(t *testing.T) {
	segStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	segEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	coverage := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if got := ClippedHealthcareMonths(segStart, segEnd, coverage, true); got != 0 {
		t.Errorf("ClippedHealthcareMonths(coverage after segment) = %v, want 0", got)
	}
}

func TestClippedHealthcareMonths_CoverageBeforeSegment_Unclipped(t *testing.T) {
	segStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	segEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	coverage := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	want := MonthsBetween(segStart, segEnd)
	if got := ClippedHealthcareMonths(segStart, segEnd, coverage, true); !floatEqual(got, want) {
		t.Errorf("ClippedHealthcareMonths(coverage before segment) = %v, want %v (unclipped)", got, want)
	}
}

func TestClippedHealthcareMonths_CoverageInsideSegment(t *testing.T) {
	segStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	segEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	coverage := time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)
	want := MonthsBetween(coverage, segEnd)
	got := ClippedHealthcareMonths(segStart, segEnd, coverage, true)
	if !floatEqual(got, want) {
		t.Errorf("ClippedHealthcareMonths(coverage mid-segment) = %v, want %v", got, want)
	}
	full := MonthsBetween(segStart, segEnd)
	if got >= full {
		t.Errorf("ClippedHealthcareMonths(coverage mid-segment) = %v, want strictly less than the unclipped %v", got, full)
	}
}

// --- Calculate: coverage-clipped healthcare accrual (HC1) ---

// healthcareCoverageFixture builds a 6-month window (Jan-Jun 2026) with a
// flat $1000 living spend/mo and two $900 Health Insurance premiums, in May
// and June only -- coverage start 2026-05-05.
func healthcareCoverageFixture() (*models.TransactionSet, time.Time, time.Time) {
	var tx []models.Transaction
	for m := 1; m <= 6; m++ {
		tx = append(tx, makeTransaction("Rent", -1000, time.Date(2026, time.Month(m), 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"))
	}
	tx = append(tx,
		makeTransaction("Premium", -900, time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
		makeTransaction("Premium", -900, time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	return makeTransactionSet(tx...), start, end
}

func TestCalculateMetrics_CoverageInsideWindow_ClipsHealthcareAccrual(t *testing.T) {
	ts, start, end := healthcareCoverageFixture()
	coverage := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)

	m := Calculate(ts, start, end, 0, 1000, coverage, true, nil)

	if !m.HasHealthcareTarget {
		t.Fatal("HasHealthcareTarget = false, want true")
	}
	wantCoverageMonths := MonthsBetween(coverage, end)
	wantMonthsInRange := MonthsBetween(start, end)
	if wantCoverageMonths >= wantMonthsInRange {
		t.Fatalf("test fixture precondition broken: coverage months %v not less than full window %v", wantCoverageMonths, wantMonthsInRange)
	}
	if !floatEqual(m.HealthcareTargetTotal, 1000*wantCoverageMonths) {
		t.Errorf("HealthcareTargetTotal = %v, want %v (1000 x %v clipped months, not %v full months)",
			m.HealthcareTargetTotal, 1000*wantCoverageMonths, wantCoverageMonths, wantMonthsInRange)
	}
	if !m.HealthcareCoverageStartInRange {
		t.Error("HealthcareCoverageStartInRange = false, want true (coverage start 2026-05-05 is inside 2026-01-01..2026-06-30)")
	}
	if !m.HealthcareCoverageStart.Equal(coverage) {
		t.Errorf("HealthcareCoverageStart = %v, want %v", m.HealthcareCoverageStart, coverage)
	}
}

func TestCalculateMetrics_CoverageBeforeWindow_FullAccrualUnchanged(t *testing.T) {
	ts, start, end := healthcareCoverageFixture()
	coverage := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) // long before the window

	m := Calculate(ts, start, end, 0, 1000, coverage, true, nil)

	wantMonthsInRange := MonthsBetween(start, end)
	if !floatEqual(m.HealthcareTargetTotal, 1000*wantMonthsInRange) {
		t.Errorf("HealthcareTargetTotal = %v, want %v (full window, coverage predates the range)", m.HealthcareTargetTotal, 1000*wantMonthsInRange)
	}
	if m.HealthcareCoverageStartInRange {
		t.Error("HealthcareCoverageStartInRange = true, want false (coverage start is before the selected range)")
	}
}

func TestCalculateMetrics_CoverageAfterWindow_SuppressesWithoutNaN(t *testing.T) {
	ts, start, end := healthcareCoverageFixture()
	coverage := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) // after the window ends

	m := Calculate(ts, start, end, 0, 1000, coverage, true, nil)

	if m.HasHealthcareTarget {
		t.Error("HasHealthcareTarget = true, want false when coverage starts after the window")
	}
	for name, v := range map[string]float64{
		"HealthcareActual":          m.HealthcareActual,
		"HealthcareTargetTotal":     m.HealthcareTargetTotal,
		"HealthcareCumulativeDelta": m.HealthcareCumulativeDelta,
		"CombinedCumulativeDelta":   m.CombinedCumulativeDelta,
	} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("%s = %v, want a finite number (no NaN/Inf when coverage months = 0)", name, v)
		}
	}
	if !floatEqual(m.HealthcareTargetTotal, 0) {
		t.Errorf("HealthcareTargetTotal = %v, want 0", m.HealthcareTargetTotal)
	}
}

func TestCalculateMetrics_NoCoverageFlag_SuppressesWithoutNaN(t *testing.T) {
	ts, start, end := healthcareCoverageFixture()

	m := Calculate(ts, start, end, 0, 1000, time.Time{}, false, nil)

	if m.HasHealthcareTarget {
		t.Error("HasHealthcareTarget = true, want false when hasCoverage=false")
	}
	for name, v := range map[string]float64{
		"HealthcareActual":          m.HealthcareActual,
		"HealthcareTargetTotal":     m.HealthcareTargetTotal,
		"HealthcareCumulativeDelta": m.HealthcareCumulativeDelta,
		"CombinedCumulativeDelta":   m.CombinedCumulativeDelta,
	} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("%s = %v, want a finite number (no NaN/Inf when hasCoverage=false)", name, v)
		}
	}
	if m.HealthcareCoverageStartInRange {
		t.Error("HealthcareCoverageStartInRange = true, want false when hasCoverage=false")
	}
}

func TestCalculateMetrics_CoverageStartInRange_ExactBoundaries(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet()

	// Coverage start exactly at rangeStart and exactly at rangeEnd both
	// count as "inside" (inclusive range).
	atStart := Calculate(ts, start, end, 0, 1000, start, true, nil)
	if !atStart.HealthcareCoverageStartInRange {
		t.Error("coverage start == rangeStart: HealthcareCoverageStartInRange = false, want true")
	}
	atEnd := Calculate(ts, start, end, 0, 1000, end, true, nil)
	if !atEnd.HealthcareCoverageStartInRange {
		t.Error("coverage start == rangeEnd: HealthcareCoverageStartInRange = false, want true")
	}
	oneDayAfter := Calculate(ts, start, end, 0, 1000, end.AddDate(0, 0, 1), true, nil)
	if oneDayAfter.HealthcareCoverageStartInRange {
		t.Error("coverage start one day after rangeEnd: HealthcareCoverageStartInRange = true, want false")
	}
}

// --- Comparison's HealthcareCoverageStart call site (attempt-3 criterion 3b) ---
//
// Comparison derives coverage start once (from data.Active(), the full
// post-duplicate-resolution ledger) and applies it to BOTH currentMetrics
// and compMetrics -- ruling 2026-08-30b's cleanup item, previously
// behaviorally inert/untested (ruling 2026-08-30c backlog). These two tests
// are mutation-killing regressions for that call site: replacing the
// argument with either the window-filtered current-period set or the raw
// (duplicates-included) set must change Current.HealthcareTargetTotal on a
// fixture built so the two derivations diverge materially.

func TestComparisonDerivesCoverageStartFromFullLedgerNotWindow(t *testing.T) {
	// Earliest Health Insurance bill sits well before BOTH the current and
	// comparison windows -- a window-derived coverage start (from either
	// filtered period) can never see it and would instead anchor on the
	// in-window bill below.
	ts := makeTransactionSet(
		makeTransaction("Premium", -900, time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), models.Outflow, HealthInsuranceCategory),
		// In current-window bill -- the only one a window-derived coverage
		// start could ever find.
		makeTransaction("Premium", -900, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), models.Outflow, HealthInsuranceCategory),
		// Comparison-window living spend so compFiltered.Len() > 0 (HasData).
		makeTransaction("Rent", -1000, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Rent", -1200, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)
	settings := &models.WhatIfSettings{MonthlyHealthcare: 900, HealthcareStartYears: 0}

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	result := Comparison(ts, start, end, "previous", settings, nil)
	if result == nil || !result.HasData {
		t.Fatalf("expected non-nil comparison with HasData=true, got %+v", result)
	}

	// Correct: coverage start (2024-06-01) predates the current window
	// entirely, so the whole window is covered.
	monthsInRange := MonthsBetween(start, end)
	wantTargetTotal := round2(900 * monthsInRange)

	// What a window-filtered mutation would produce: coverage start derived
	// only from the current period's filtered set, anchoring on the Feb 15
	// bill instead -- clipping the covered months down to the back half of
	// February.
	mutatedCoverageStart := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	mutatedCoverageMonths := ClippedHealthcareMonths(start, end, mutatedCoverageStart, true)
	mutatedTargetTotal := round2(900 * mutatedCoverageMonths)
	if wantTargetTotal == mutatedTargetTotal {
		t.Fatalf("test fixture precondition broken: full-ledger and window-derived coverage starts produce the "+
			"same HealthcareTargetTotal (%v) -- fixture can't distinguish them", wantTargetTotal)
	}

	got := round2(result.Current.HealthcareTargetTotal)
	if got != wantTargetTotal {
		t.Errorf("Current.HealthcareTargetTotal = %v, want %v (coverage start must come from the FULL ledger, "+
			"not the window-derived figure %v a mutated call site would produce)", got, wantTargetTotal, mutatedTargetTotal)
	}
}

func TestComparisonCoverageStartExcludesSuppressedDuplicates(t *testing.T) {
	// Earliest Health Insurance bill is duplicate-suppressed; a raw
	// (duplicates-included) derivation would still anchor on it. The
	// correct coverage start must come from the next-earliest ACTIVE bill,
	// which lands inside the current window rather than well before it.
	ts := makeTransactionSet(
		makeTransaction("Premium (dup)", -900, time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), models.Outflow, HealthInsuranceCategory),
		makeTransaction("Premium", -900, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), models.Outflow, HealthInsuranceCategory),
		makeTransaction("Rent", -1000, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Rent", -1200, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)
	ts.Transactions[0].Suppressed = true
	settings := &models.WhatIfSettings{MonthlyHealthcare: 900, HealthcareStartYears: 0}

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	result := Comparison(ts, start, end, "previous", settings, nil)
	if result == nil || !result.HasData {
		t.Fatalf("expected non-nil comparison with HasData=true, got %+v", result)
	}

	// Correct: coverage start is the earliest ACTIVE bill (2025-02-15),
	// which lands inside the window -- clipped to the back half of February.
	activeCoverageStart := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	wantCoverageMonths := ClippedHealthcareMonths(start, end, activeCoverageStart, true)
	wantTargetTotal := round2(900 * wantCoverageMonths)

	// What a duplicates-included mutation would produce: coverage start
	// derived from the raw set, anchoring on the suppressed 2024-06-01 row,
	// which predates the window entirely -- the whole window would appear
	// covered.
	monthsInRange := MonthsBetween(start, end)
	mutatedTargetTotal := round2(900 * monthsInRange)
	if wantTargetTotal == mutatedTargetTotal {
		t.Fatalf("test fixture precondition broken: active and duplicates-included coverage starts produce the "+
			"same HealthcareTargetTotal (%v) -- fixture can't distinguish them", wantTargetTotal)
	}

	got := round2(result.Current.HealthcareTargetTotal)
	if got != wantTargetTotal {
		t.Errorf("Current.HealthcareTargetTotal = %v, want %v (coverage start must exclude the suppressed "+
			"duplicate; a mutated call site passing the raw set would produce %v)", got, wantTargetTotal, mutatedTargetTotal)
	}
}

// round2 rounds to 2 decimal places using the same %.2f-then-parse
// convention as the rendered-string rule (ruling 2026-08-29b), so
// comparisons here match what a rendered figure would show.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// TestCalculateMetrics_RefundDominantRange_SignedTotalsAndCombinedInvariantHolds
// is CB7's required refund-dominant RANGE fixture: two months, Jan
// ordinary (a Rent charge and a Health Insurance premium, both plain
// spend) and Feb a pair of refunds -- one non-HI (+6000, exceeding the
// WHOLE RANGE's ordinary spend, not just Feb's own) and one HI-category
// (+500, exceeding Jan's own premium) -- so BOTH the living share and the
// healthcare share independently flip refund-dominant, alongside the
// combined range total. Before CB7, TotalExpenses/LivingExpensesTotal/
// HealthcareTotal all ran math.Abs(...SumAmount()), so this range would
// have reported POSITIVE expenses despite refunds outweighing spend
// overall -- understating NetSavings/SavingsRate and breaking the
// CombinedCumulativeBalance partition invariant dashboard.go used to
// document as out of scope for exactly this shape. A math.Abs mutant at
// any of metrics.go's three range-level sites (totalExpenses,
// healthcareTotal, livingTotal) must fail this test.
func TestCalculateMetrics_RefundDominantRange_SignedTotalsAndCombinedInvariantHolds(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Premium", -200, time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
		makeTransaction("Cruise Refund", 6000, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Misc"),
		makeTransaction("Premium Refund", 500, time.Date(2025, 2, 11, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)

	m := Calculate(ts, start, end, 800, 150, fullCoverage, true, nil)
	monthsInRange := MonthsBetween(start, end)

	// Whole-range outflow set nets: -1000 (rent) - 200 (premium) + 6000
	// (refund) + 500 (premium refund) = +5300. TotalExpenses is the
	// signed negated net: -5300 -- a refund-dominant RANGE (the Feb
	// refund alone exceeds the range's entire 1200 of ordinary spend).
	if !floatEqual(m.TotalExpenses, -5300) {
		t.Fatalf("TotalExpenses = %v, want -5300 (signed negated net; CB7)", m.TotalExpenses)
	}
	if !floatEqual(m.NetSavings, m.TotalIncome-m.TotalExpenses) {
		t.Errorf("NetSavings = %v, want TotalIncome-TotalExpenses = %v", m.NetSavings, m.TotalIncome-m.TotalExpenses)
	}
	if !floatEqual(m.NetSavings, 10300) {
		t.Errorf("NetSavings = %v, want 10300 (5000 income - (-5300) expenses)", m.NetSavings)
	}
	if m.NetSavings <= m.TotalIncome {
		t.Errorf("NetSavings = %v, want > TotalIncome %v (refund exceeds spend, so savings must EXCEED income)", m.NetSavings, m.TotalIncome)
	}

	// Living (non-HI) outflows: -1000 (rent) + 6000 (refund) = +5000 net
	// credit -- LivingExpensesTotal is the signed negated net, -5000, and
	// every figure it feeds (ActualMonthly, PerMonthDelta,
	// CumulativeDelta) must derive from that signed value directly.
	if !floatEqual(m.LivingExpensesTotal, -5000) {
		t.Fatalf("LivingExpensesTotal = %v, want -5000 (signed negated net; CB7)", m.LivingExpensesTotal)
	}
	wantActualMonthly := -5000 / monthsInRange
	if !floatEqual(m.ActualMonthly, wantActualMonthly) {
		t.Errorf("ActualMonthly = %v, want %v", m.ActualMonthly, wantActualMonthly)
	}
	wantPerMonthDelta := wantActualMonthly - 800
	if !floatEqual(m.PerMonthDelta, wantPerMonthDelta) {
		t.Errorf("PerMonthDelta = %v, want %v", m.PerMonthDelta, wantPerMonthDelta)
	}
	wantCumulativeDelta := -5000 - 800*monthsInRange
	if !floatEqual(m.CumulativeDelta, wantCumulativeDelta) {
		t.Errorf("CumulativeDelta = %v, want %v", m.CumulativeDelta, wantCumulativeDelta)
	}

	// Healthcare (HI-only) outflows: -200 (Jan premium) + 500 (Feb premium
	// refund) = +300 net credit -- HealthcareTotal is ALSO the signed
	// negated net, -300, independently of living/range going
	// refund-dominant, proving the split-classification (living vs.
	// healthcare vs. whole-range) stays correct when every share flips
	// sign on its own.
	if !floatEqual(m.HealthcareTotal, -300) {
		t.Fatalf("HealthcareTotal = %v, want -300 (signed negated net; CB7)", m.HealthcareTotal)
	}
	wantHealthcareActual := -300 / monthsInRange
	if !floatEqual(m.HealthcareActual, wantHealthcareActual) {
		t.Errorf("HealthcareActual = %v, want %v", m.HealthcareActual, wantHealthcareActual)
	}
	wantHealthcarePerMonthDelta := wantHealthcareActual - 150
	if !floatEqual(m.HealthcarePerMonthDelta, wantHealthcarePerMonthDelta) {
		t.Errorf("HealthcarePerMonthDelta = %v, want %v", m.HealthcarePerMonthDelta, wantHealthcarePerMonthDelta)
	}
	wantHealthcareCumulativeDelta := -300 - 150*monthsInRange
	if !floatEqual(m.HealthcareCumulativeDelta, wantHealthcareCumulativeDelta) {
		t.Errorf("HealthcareCumulativeDelta = %v, want %v", m.HealthcareCumulativeDelta, wantHealthcareCumulativeDelta)
	}

	// CB7's core claim: the CombinedCumulativeBalance walk's per-month
	// signed spends now partition the range-level signed total even
	// though the range as a whole nets outflow-POSITIVE (refund
	// dominant) -- previously out of scope per dashboard.go's old
	// invariant doc, which required the range to still net
	// outflow-negative.
	if len(m.CombinedCumulativeBalance) == 0 {
		t.Fatalf("CombinedCumulativeBalance empty; want non-empty (both targets set)")
	}
	last := m.CombinedCumulativeBalance[len(m.CombinedCumulativeBalance)-1]
	if math.Abs(last-(-m.CombinedCumulativeDelta)) > 0.01 {
		t.Errorf("balance tail %.4f vs -CombinedCumulativeDelta %.4f -- must agree even for a wholly refund-dominant range (CB7)", last, -m.CombinedCumulativeDelta)
	}
}

// TestCalculateComparison_ExpensesChangeTracksNegativeComparisonPeriod pins
// CB7's PeriodComparison surface: ExpensesChange (PercentChange applied to
// TotalExpenses) must track the sign of the change even when the
// COMPARISON (previous) period's TotalExpenses is itself negative (a
// refund-dominant prior period) -- PercentChange's own |previous|
// denominator (CB3-E) is untouched by CB7, but this proves the composition
// through Comparison/Calculate produces a genuinely negative
// Previous.TotalExpenses to feed it, not a math.Abs'd one.
func TestCalculateComparison_ExpensesChangeTracksNegativeComparisonPeriod(t *testing.T) {
	ts := makeTransactionSet(
		// Jan 2025 (previous period): a single refund, no ordinary spend --
		// nets outflow-positive, so TotalExpenses must be negative.
		makeTransaction("Big Refund", 3000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Misc"),
		// Feb 2025 (current period): ordinary spend.
		makeTransaction("Rent", -1000, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	result := Comparison(ts, start, end, "previous", nil, nil)
	if result == nil || !result.HasData {
		t.Fatalf("Comparison returned nil/no-data: %+v", result)
	}

	if !floatEqual(result.Previous.TotalExpenses, -3000) {
		t.Fatalf("Previous.TotalExpenses = %v, want -3000 (refund-dominant prior period; CB7)", result.Previous.TotalExpenses)
	}
	if !floatEqual(result.Current.TotalExpenses, 1000) {
		t.Fatalf("Current.TotalExpenses = %v, want 1000 (ordinary)", result.Current.TotalExpenses)
	}

	wantExpensesChange := ((1000.0 - (-3000.0)) / math.Abs(-3000.0)) * 100
	if !floatEqual(result.ExpensesChange, wantExpensesChange) {
		t.Errorf("ExpensesChange = %v, want %v", result.ExpensesChange, wantExpensesChange)
	}
	// Expenses went from a net CREDIT (-3000) to ordinary net spend
	// (1000): a genuine worsening of $4000, so ExpensesChange must be
	// POSITIVE, tracking the change's own sign (CB3-E's convention).
	if result.ExpensesChange <= 0 {
		t.Errorf("ExpensesChange = %v, want positive (expenses got worse: credit -> spend)", result.ExpensesChange)
	}
}

// negZeroJSONPattern matches a JSON-serialized IEEE negative zero token:
// "-0", "-0.0", "-0.00", etc., immediately followed by a JSON delimiter
// (comma, closing brace, or closing bracket) -- i.e. the token is a whole
// number, not a substring of a larger negative number like "-10" or
// "-0.5". encoding/json serializes float64(math.Copysign(0, -1)) as "-0",
// which is what this pattern is built to catch (ruling CB7-2026-09-03c).
var negZeroJSONPattern = regexp.MustCompile(`-0(\.0+)?[,}\]]`)

// TestCalculateMetrics_EmptyHealthcareWindow_NoNegativeZero is CB7-
// 2026-09-03c's required fixture: a window with ordinary (non-healthcare)
// spend and a healthcare TARGET configured, but ZERO transactions in
// HealthInsuranceCategory. Before the fix, healthcareOutflows.SumAmount()
// is exactly 0.0 (an empty set), and the old `-healthcareOutflows.
// SumAmount()` negated that to IEEE -0.0 -- HealthcareActual inherited it
// via division, and kpis.html/formatMoney would render "$-0.00" on the
// Monthly Healthcare card for every real window with no Health Insurance
// rows (confirmed true of the live ledger in every month/year).
func TestCalculateMetrics_EmptyHealthcareWindow_NoNegativeZero(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	m := Calculate(ts, start, end, 0, 150, fullCoverage, true, nil)

	if m.HealthcareTotal != 0 || math.Signbit(m.HealthcareTotal) {
		t.Errorf("HealthcareTotal = %v (Signbit=%v), want +0 with Signbit false", m.HealthcareTotal, math.Signbit(m.HealthcareTotal))
	}
	if m.HealthcareActual != 0 || math.Signbit(m.HealthcareActual) {
		t.Errorf("HealthcareActual = %v (Signbit=%v), want +0 with Signbit false", m.HealthcareActual, math.Signbit(m.HealthcareActual))
	}
}

// TestCalculateMetrics_NoOutflowsAtAll_NoNegativeZero is CB7-2026-09-03c's
// second required fixture: a window with income but NO outflow
// transactions at all. TotalExpenses/LivingExpensesTotal (and
// HealthcareTotal, exercised as a bonus since it shares the same defect
// class) all derive from SignedNet of an empty set and must be +0, never
// IEEE -0.0. Also asserts the whole struct's json.Marshal output carries
// no negative-zero token anywhere (encoding/json serializes -0.0 as the
// literal "-0", so a stray unnormalized site anywhere in DashboardMetrics
// would show up here even if this test's other explicit checks miss it).
func TestCalculateMetrics_NoOutflowsAtAll_NoNegativeZero(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
	)

	m := Calculate(ts, start, end, 500, 150, fullCoverage, true, nil)

	if m.TotalExpenses != 0 || math.Signbit(m.TotalExpenses) {
		t.Errorf("TotalExpenses = %v (Signbit=%v), want +0 with Signbit false", m.TotalExpenses, math.Signbit(m.TotalExpenses))
	}
	if m.LivingExpensesTotal != 0 || math.Signbit(m.LivingExpensesTotal) {
		t.Errorf("LivingExpensesTotal = %v (Signbit=%v), want +0 with Signbit false", m.LivingExpensesTotal, math.Signbit(m.LivingExpensesTotal))
	}
	if m.HealthcareTotal != 0 || math.Signbit(m.HealthcareTotal) {
		t.Errorf("HealthcareTotal = %v (Signbit=%v), want +0 with Signbit false", m.HealthcareTotal, math.Signbit(m.HealthcareTotal))
	}

	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if loc := negZeroJSONPattern.FindString(string(b)); loc != "" {
		t.Errorf("json output contains a negative-zero token %q: %s", loc, b)
	}
}

// TestCalculateMetrics_IncomeOnlyMonth_TrendEntriesNoNegativeZero is CB7-
// 2026-09-03c's third required fixture: a two-month window where the
// FIRST month has income only (no outflow transactions at all that
// month) and the second is ordinary. The income-only month's
// ExpensesTrend/LivingExpensesTrend/HealthcareTrend entries must all be
// +0 with Signbit false, never IEEE -0.0.
func TestCalculateMetrics_IncomeOnlyMonth_TrendEntriesNoNegativeZero(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1000, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	m := Calculate(ts, start, end, 0, 0, fullCoverage, true, nil)

	if len(m.ExpensesTrend) != 2 || len(m.LivingExpensesTrend) != 2 || len(m.HealthcareTrend) != 2 {
		t.Fatalf("trend lengths = %d/%d/%d, want 2/2/2: expenses=%v living=%v hc=%v",
			len(m.ExpensesTrend), len(m.LivingExpensesTrend), len(m.HealthcareTrend),
			m.ExpensesTrend, m.LivingExpensesTrend, m.HealthcareTrend)
	}
	if m.ExpensesTrend[0] != 0 || math.Signbit(m.ExpensesTrend[0]) {
		t.Errorf("ExpensesTrend[Jan] = %v (Signbit=%v), want +0 with Signbit false", m.ExpensesTrend[0], math.Signbit(m.ExpensesTrend[0]))
	}
	if m.LivingExpensesTrend[0] != 0 || math.Signbit(m.LivingExpensesTrend[0]) {
		t.Errorf("LivingExpensesTrend[Jan] = %v (Signbit=%v), want +0 with Signbit false", m.LivingExpensesTrend[0], math.Signbit(m.LivingExpensesTrend[0]))
	}
	if m.HealthcareTrend[0] != 0 || math.Signbit(m.HealthcareTrend[0]) {
		t.Errorf("HealthcareTrend[Jan] = %v (Signbit=%v), want +0 with Signbit false", m.HealthcareTrend[0], math.Signbit(m.HealthcareTrend[0]))
	}
}

// TestCalculateMetrics_MonthWithExactlyCancellingOutflows_TrendEntriesNoNegativeZero
// closes a gap the income-only-month fixture above does NOT cover: expAmt/
// hcAmt/livingMonth (metrics.go's per-month SignedNet call sites) only run
// AT ALL when the month has a bucket entry in monthlyOutflows/
// monthlyHealthcare/monthlyLiving -- an income-only month never looks the
// key up, so it can never observe a -0.0 from these three sites. This
// fixture instead gives one month BOTH a Health-Insurance-category charge
// and an EQUAL, opposite-signed refund (net exactly 0) alongside an
// ordinary living-category charge and its own equal, opposite-signed
// refund (also net exactly 0) -- so the whole month's outflow bucket, its
// living-only bucket, AND its healthcare-only bucket all sum to exactly
// 0.0, exercising all three per-month SignedNet call sites at once.
func TestCalculateMetrics_MonthWithExactlyCancellingOutflows_TrendEntriesNoNegativeZero(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Housing Charge", -50, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Housing Refund", 50, time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Premium Charge", -30, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
		makeTransaction("Premium Refund", 30, time.Date(2025, 1, 11, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)

	m := Calculate(ts, start, end, 0, 0, fullCoverage, true, nil)

	if len(m.ExpensesTrend) != 1 || len(m.LivingExpensesTrend) != 1 || len(m.HealthcareTrend) != 1 {
		t.Fatalf("trend lengths = %d/%d/%d, want 1/1/1", len(m.ExpensesTrend), len(m.LivingExpensesTrend), len(m.HealthcareTrend))
	}
	if m.ExpensesTrend[0] != 0 || math.Signbit(m.ExpensesTrend[0]) {
		t.Errorf("ExpensesTrend[0] = %v (Signbit=%v), want +0 with Signbit false", m.ExpensesTrend[0], math.Signbit(m.ExpensesTrend[0]))
	}
	if m.LivingExpensesTrend[0] != 0 || math.Signbit(m.LivingExpensesTrend[0]) {
		t.Errorf("LivingExpensesTrend[0] = %v (Signbit=%v), want +0 with Signbit false", m.LivingExpensesTrend[0], math.Signbit(m.LivingExpensesTrend[0]))
	}
	if m.HealthcareTrend[0] != 0 || math.Signbit(m.HealthcareTrend[0]) {
		t.Errorf("HealthcareTrend[0] = %v (Signbit=%v), want +0 with Signbit false", m.HealthcareTrend[0], math.Signbit(m.HealthcareTrend[0]))
	}
}

// TestSignedNet is a direct unit test of the helper (CB7-2026-09-03c).
func TestSignedNet(t *testing.T) {
	t.Run("empty set", func(t *testing.T) {
		got := SignedNet(&models.TransactionSet{})
		if got != 0 || math.Signbit(got) {
			t.Errorf("SignedNet(empty) = %v (Signbit=%v), want +0 with Signbit false", got, math.Signbit(got))
		}
	})

	t.Run("exactly cancelling set", func(t *testing.T) {
		ts := makeTransactionSet(
			makeTransaction("Charge", -10, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), models.Outflow, "Misc"),
			makeTransaction("Refund", 10, time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), models.Outflow, "Misc"),
		)
		got := SignedNet(ts)
		if got != 0 || math.Signbit(got) {
			t.Errorf("SignedNet(cancelling) = %v (Signbit=%v), want +0 with Signbit false", got, math.Signbit(got))
		}
	})

	t.Run("ordinary spend is positive", func(t *testing.T) {
		ts := makeTransactionSet(
			makeTransaction("Rent", -1000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		)
		got := SignedNet(ts)
		if !floatEqual(got, 1000) {
			t.Errorf("SignedNet(ordinary) = %v, want 1000", got)
		}
	})

	t.Run("refund-dominant is negative", func(t *testing.T) {
		ts := makeTransactionSet(
			makeTransaction("Rent", -1000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
			makeTransaction("Big Refund", 5000, time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), models.Outflow, "Misc"),
		)
		got := SignedNet(ts)
		if !floatEqual(got, -4000) {
			t.Errorf("SignedNet(refund-dominant) = %v, want -4000", got)
		}
	})
}
