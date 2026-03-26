package dashboard

import (
	"math"
	"testing"
	"time"

	"budget2/internal/models"
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

// --- percentChange ---

func TestPercentChange_Normal(t *testing.T) {
	// 50 -> 75 = +50%
	got := percentChange(75, 50)
	if !floatEqual(got, 50.0) {
		t.Errorf("percentChange(75, 50) = %v, want 50", got)
	}

	// 100 -> 50 = -50%
	got = percentChange(50, 100)
	if !floatEqual(got, -50.0) {
		t.Errorf("percentChange(50, 100) = %v, want -50", got)
	}
}

func TestPercentChange_ZeroPrevious(t *testing.T) {
	got := percentChange(100, 0)
	if !floatEqual(got, 100.0) {
		t.Errorf("percentChange(100, 0) = %v, want 100", got)
	}
}

func TestPercentChange_BothZero(t *testing.T) {
	got := percentChange(0, 0)
	if got != 0 {
		t.Errorf("percentChange(0, 0) = %v, want 0", got)
	}
}

// --- calculateMetrics ---

func TestCalculateMetrics_BasicTotals(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Bonus", 1000, time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1500, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Groceries", -500, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
	)

	m := calculateMetrics(ts)

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

	m := calculateMetrics(ts)

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

	m := calculateMetrics(ts)

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

// --- buildSpendingTrendChartData ---

func TestBuildSpendingTrendChartData_Basic(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -1000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Rent", -1200, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Rent", -900, time.Date(2025, 3, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	result := buildSpendingTrendChartData(ts)

	data, ok := result["data"].([]map[string]interface{})
	if !ok || len(data) == 0 {
		t.Fatal("expected non-empty data array")
	}

	trace := data[0]

	// Check months - should have 2 change entries (Feb, Mar)
	months := trace["x"].([]string)
	if len(months) != 2 {
		t.Fatalf("expected 2 change months, got %d", len(months))
	}
	if months[0] != "2025-02" || months[1] != "2025-03" {
		t.Errorf("months = %v, want [2025-02, 2025-03]", months)
	}

	// Check change values
	// Jan->Feb: (1200-1000)/1000 * 100 = 20%
	// Feb->Mar: (900-1200)/1200 * 100 = -25%
	values := trace["y"].([]float64)
	if !floatEqual(values[0], 20.0) {
		t.Errorf("change[0] = %v, want 20", values[0])
	}
	if !floatEqual(values[1], -25.0) {
		t.Errorf("change[1] = %v, want -25", values[1])
	}

	// Check customdata has [currAmount, prevAmount] pairs
	customdata := trace["customdata"].([][]float64)
	if len(customdata) != 2 {
		t.Fatalf("customdata length = %d, want 2", len(customdata))
	}
	if !floatEqual(customdata[0][0], 1200) || !floatEqual(customdata[0][1], 1000) {
		t.Errorf("customdata[0] = %v, want [1200, 1000]", customdata[0])
	}

	// Check colors: Feb increased (red), Mar decreased (green)
	colors := trace["marker"].(map[string]interface{})["color"].([]string)
	if colors[0] != "#ef4444" {
		t.Errorf("color[0] = %v, want #ef4444 (red for increase)", colors[0])
	}
	if colors[1] != "#22c55e" {
		t.Errorf("color[1] = %v, want #22c55e (green for decrease)", colors[1])
	}
}

func TestBuildSpendingTrendChartData_LessThanTwoMonths(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -1000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	result := buildSpendingTrendChartData(ts)

	data := result["data"].([]map[string]interface{})
	if len(data) != 0 {
		t.Errorf("expected empty data for < 2 months, got %d traces", len(data))
	}
}

func TestBuildSpendingTrendChartData_DecreasingSpending(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Stuff", -2000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Shopping"),
		makeTransaction("Stuff", -1000, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Shopping"),
	)

	result := buildSpendingTrendChartData(ts)
	data := result["data"].([]map[string]interface{})
	trace := data[0]

	values := trace["y"].([]float64)
	if !floatEqual(values[0], -50.0) {
		t.Errorf("change = %v, want -50", values[0])
	}

	colors := trace["marker"].(map[string]interface{})["color"].([]string)
	if colors[0] != "#22c55e" {
		t.Errorf("color = %v, want #22c55e (green for decrease)", colors[0])
	}
}

// --- buildMonthlyChartData ---

func TestBuildMonthlyChartData_Basic(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1500, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Salary", 5000, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -2000, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	result := buildMonthlyChartData(ts)

	data := result["data"].([]map[string]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 traces (income + expenses), got %d", len(data))
	}

	incomeTrace := data[0]
	expenseTrace := data[1]

	if incomeTrace["name"] != "Income" {
		t.Errorf("first trace name = %v, want Income", incomeTrace["name"])
	}
	if expenseTrace["name"] != "Expenses" {
		t.Errorf("second trace name = %v, want Expenses", expenseTrace["name"])
	}

	incomeY := incomeTrace["y"].([]float64)
	expenseY := expenseTrace["y"].([]float64)

	if len(incomeY) != 2 || len(expenseY) != 2 {
		t.Fatalf("expected 2 months of data, got income=%d, expenses=%d", len(incomeY), len(expenseY))
	}

	if !floatEqual(incomeY[0], 5000) {
		t.Errorf("income Jan = %v, want 5000", incomeY[0])
	}
	if !floatEqual(expenseY[0], 1500) {
		t.Errorf("expenses Jan = %v, want 1500", expenseY[0])
	}
	if !floatEqual(expenseY[1], 2000) {
		t.Errorf("expenses Feb = %v, want 2000", expenseY[1])
	}

	// Check layout has barmode group
	layout := result["layout"].(map[string]interface{})
	if layout["barmode"] != "group" {
		t.Errorf("layout barmode = %v, want group", layout["barmode"])
	}
}

// --- buildCategoryChartData ---

func TestBuildCategoryChartData_TopTenPlusOther(t *testing.T) {
	var txns []models.Transaction
	// Create 12 categories with decreasing amounts
	for i := 0; i < 12; i++ {
		cat := string(rune('A'+i)) + "-Category"
		amount := -float64(1200-i*100) // -1200, -1100, ..., -100
		txns = append(txns, makeTransaction("Merchant", amount,
			time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, cat))
	}
	ts := makeTransactionSet(txns...)

	result := buildCategoryChartData(ts)

	data := result["data"].([]map[string]interface{})
	if len(data) != 1 {
		t.Fatalf("expected 1 trace (pie), got %d", len(data))
	}

	trace := data[0]
	if trace["type"] != "pie" {
		t.Errorf("chart type = %v, want pie", trace["type"])
	}

	labels := trace["labels"].([]string)
	values := trace["values"].([]float64)

	// Should have 11 entries: top 10 categories + "Other"
	if len(labels) != 11 {
		t.Errorf("expected 11 labels (top 10 + Other), got %d: %v", len(labels), labels)
	}

	// Last label should be "Other"
	if labels[len(labels)-1] != "Other" {
		t.Errorf("last label = %v, want Other", labels[len(labels)-1])
	}

	// "Other" should be sum of categories 11 and 12 (amounts 200 + 100 = 300)
	otherVal := values[len(values)-1]
	if !floatEqual(otherVal, 300) {
		t.Errorf("Other value = %v, want 300", otherVal)
	}
}

// --- buildMerchantsChartData ---

func TestBuildMerchantsChartData_TopTen(t *testing.T) {
	var txns []models.Transaction
	// Create 12 unique merchants
	merchants := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot",
		"Golf", "Hotel", "India", "Juliet", "Kilo", "Lima"}
	for i, m := range merchants {
		amount := -float64((len(merchants) - i) * 100) // Alpha=1200, Bravo=1100, ..., Lima=100
		txns = append(txns, makeTransaction(m, amount,
			time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Shopping"))
	}
	ts := makeTransactionSet(txns...)

	result := buildMerchantsChartData(ts)

	data := result["data"].([]map[string]interface{})
	if len(data) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(data))
	}

	trace := data[0]
	if trace["orientation"] != "h" {
		t.Errorf("orientation = %v, want h", trace["orientation"])
	}

	labels := trace["y"].([]string)
	values := trace["x"].([]float64)

	// Should have exactly 10 merchants
	if len(labels) != 10 {
		t.Errorf("expected 10 merchants, got %d", len(labels))
	}

	// Labels are reversed for horizontal bar chart (lowest at bottom = first in array)
	// Top merchant (Alpha=1200) should be last in the reversed array
	if labels[len(labels)-1] != "Alpha" {
		t.Errorf("top merchant (last in reversed order) = %v, want Alpha", labels[len(labels)-1])
	}
	if !floatEqual(values[len(values)-1], 1200) {
		t.Errorf("top merchant value = %v, want 1200", values[len(values)-1])
	}

	// Kilo and Lima (ranks 11, 12) should NOT be present
	for _, label := range labels {
		if label == "Kilo" || label == "Lima" {
			t.Errorf("merchant %s should not be in top 10", label)
		}
	}
}

// --- buildCumulativeChartData ---

func TestBuildCumulativeChartData_PositiveBalance(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1500, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Food", -500, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
	)

	result := buildCumulativeChartData(ts)

	data := result["data"].([]map[string]interface{})
	if len(data) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(data))
	}

	trace := data[0]

	cumulative := trace["y"].([]float64)
	if len(cumulative) != 3 {
		t.Fatalf("expected 3 data points, got %d", len(cumulative))
	}

	// Day 1: 5000, Day 5: 5000-1500=3500, Day 10: 3500-500=3000
	if !floatEqual(cumulative[0], 5000) {
		t.Errorf("cumulative[0] = %v, want 5000", cumulative[0])
	}
	if !floatEqual(cumulative[1], 3500) {
		t.Errorf("cumulative[1] = %v, want 3500", cumulative[1])
	}
	if !floatEqual(cumulative[2], 3000) {
		t.Errorf("cumulative[2] = %v, want 3000", cumulative[2])
	}

	// Positive balance -> green
	lineColor := trace["line"].(map[string]interface{})["color"].(string)
	if lineColor != "#22c55e" {
		t.Errorf("line color = %v, want #22c55e (green)", lineColor)
	}

	fillColor := trace["fillcolor"].(string)
	if fillColor != "rgba(34, 197, 94, 0.1)" {
		t.Errorf("fill color = %v, want rgba(34, 197, 94, 0.1)", fillColor)
	}
}

func TestBuildCumulativeChartData_NegativeBalance(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Salary", 1000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	result := buildCumulativeChartData(ts)

	data := result["data"].([]map[string]interface{})
	trace := data[0]

	cumulative := trace["y"].([]float64)
	// Day 1: 1000, Day 5: 1000-3000=-2000
	if !floatEqual(cumulative[len(cumulative)-1], -2000) {
		t.Errorf("final cumulative = %v, want -2000", cumulative[len(cumulative)-1])
	}

	// Negative balance -> red
	lineColor := trace["line"].(map[string]interface{})["color"].(string)
	if lineColor != "#ef4444" {
		t.Errorf("line color = %v, want #ef4444 (red)", lineColor)
	}

	fillColor := trace["fillcolor"].(string)
	if fillColor != "rgba(239, 68, 68, 0.1)" {
		t.Errorf("fill color = %v, want rgba(239, 68, 68, 0.1)", fillColor)
	}
}
