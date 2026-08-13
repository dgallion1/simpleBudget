package metrics

import (
	"math"
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

	result := Comparison(ts, start, end, "previous", nil)

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

	result := Comparison(ts, start, end, "year", nil)

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

	result := Comparison(ts, start, end, "previous", nil)

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

	result := Comparison(ts, start, end, "bogus", nil)

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

	pc := Comparison(ts, start, end, "previous", nil)
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

	result := Comparison(ts, start, end, "previous", nil)
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

// --- Calculate ---

func TestCalculateMetrics_BasicTotals(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Bonus", 1000, time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1500, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Groceries", -500, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
	)

	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 0)

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

func TestCalculateMetrics_ZeroIncome(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -1500, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 0)

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

	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 0)

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
	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 0)

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

	m := Calculate(ts, start, end, 0, 0)

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

	m := Calculate(ts, start, end, 0, 0)

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
	m := Calculate(ts, start, end, target, 0)

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

	m := Calculate(ts, start, end, 5000, 0)

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

	m := Calculate(ts, start, end, 0, 0)

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

	m := Calculate(ts, start, end, 0, 2000)

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

	m := Calculate(ts, start, end, 0, 2000)

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

	m := Calculate(ts, start, end, 0, 1500)
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

	m := Calculate(ts, start, end, 0, 1500)
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

	m := Calculate(ts, start, end, 0, 0)
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

	m := Calculate(ts, start, end, 4000, 1500)

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
	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 0)

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
	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 1500)

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

	m := Calculate(ts, start, end, 3000, 2000)

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

	m := Calculate(ts, start, end, 4000, 0)
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

	m := Calculate(ts, start, end, 4000, 2000)

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

	m := Calculate(ts, start, end, 0, 0)
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
	m := Calculate(ts, day, day, 5000, 0)

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

	m := Calculate(ts, ts.MinDate(), ts.MaxDate(), 0, 0)

	if m.CombinedCumulativeBalance != nil {
		t.Errorf("CombinedCumulativeBalance = %v, want nil when no combined target", m.CombinedCumulativeBalance)
	}
}

func TestCalculateMetrics_CombinedCumulativeBalance_AccumulatesMonthlyBalance(t *testing.T) {
	// Two months: Jan $1000 living, Feb $2000 living. Target $1500/mo combined,
	// no healthcare. Balance uses target-actual (positive = saved):
	// Jan = +500 (under by $500), Feb = +500 - 500 = 0.
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1000, jan, models.Outflow, "Housing"),
		makeTransaction("Rent", -2000, feb, models.Outflow, "Housing"),
	)

	m := Calculate(ts, start, end, 1500, 0)

	if len(m.CombinedCumulativeBalance) != 2 {
		t.Fatalf("CombinedCumulativeBalance length = %d, want 2", len(m.CombinedCumulativeBalance))
	}
	if !floatEqual(m.CombinedCumulativeBalance[0], 500) {
		t.Errorf("CombinedCumulativeBalance[0] = %.2f, want +500 (saved $500 in Jan)", m.CombinedCumulativeBalance[0])
	}
	if !floatEqual(m.CombinedCumulativeBalance[1], 0) {
		t.Errorf("CombinedCumulativeBalance[1] = %.2f, want 0 (Feb $500 over erases Jan savings)", m.CombinedCumulativeBalance[1])
	}
}

func TestCalculateMetrics_CombinedCumulativeBalance_LastIsNegationOfCumulativeDelta(t *testing.T) {
	// Invariant: the last value of CombinedCumulativeBalance must equal the
	// negation of CombinedCumulativeDelta (within month-rounding slack).
	// Balance uses target-actual; Delta uses actual-target. If they fail to
	// negate, the chart and the Budget KPI card will show contradictory
	// over/under signs.
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

	m := Calculate(ts, start, end, 1200, 350) // combined target = 1550

	if len(m.CombinedCumulativeBalance) == 0 {
		t.Fatalf("CombinedCumulativeBalance empty; want non-empty when combined target is set")
	}
	last := m.CombinedCumulativeBalance[len(m.CombinedCumulativeBalance)-1]
	// Slack: per-month series uses integer-month target (1550) while
	// CombinedCumulativeDelta uses fractional MonthsInRange (e.g. 2.957 mo
	// for a 90-day Jan 1 – Mar 31 window). Difference is bounded by
	// combinedTarget * |len(trend) - MonthsInRange| ≈ 1550 * 0.043 ≈ $67,
	// so we allow $100 to comfortably cover the rounding gap while still
	// catching any real divergence between the chart and the KPI card.
	if math.Abs(last-(-m.CombinedCumulativeDelta)) > 100 {
		t.Errorf("balance tail %.2f vs -CombinedCumulativeDelta %.2f — must agree (within $100 month-rounding slack)", last, -m.CombinedCumulativeDelta)
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

func TestPhaseAdjustedMonthlyTarget_FlowsIntoCalculateMetrics(t *testing.T) {
	// Verify the full pipeline: phase-adjusted target reaches DashboardMetrics.
	s := makePhaseSettings(t, 5000, "2025-01", 86, true) // No-Go (0.65)
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	target := phaseAdjustedMonthlyTarget(s, start, end)
	m := Calculate(ts, start, end, target, 0)

	if !m.HasBudgetTarget {
		t.Fatalf("HasBudgetTarget = false, want true")
	}
	if !floatEqual(m.BudgetTarget, 3250) { // 5000 * 0.65
		t.Errorf("BudgetTarget = %v, want 3250 (No-Go-adjusted)", m.BudgetTarget)
	}
}
