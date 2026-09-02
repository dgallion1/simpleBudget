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

// --- resolveDateRange ---

func TestResolveDateRange_ExplicitDates(t *testing.T) {
	minDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	maxDate := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)

	start, end := resolveDateRange("2025-03-01", "2025-06-30", minDate, maxDate)

	if start != time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("start = %v, want 2025-03-01", start)
	}
	if end != time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC) {
		t.Errorf("end = %v, want 2025-06-30", end)
	}
}

func TestResolveDateRange_DefaultsToYTD(t *testing.T) {
	minDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.Local)
	maxDate := time.Date(2030, 12, 31, 0, 0, 0, 0, time.Local)

	start, end := resolveDateRange("", "", minDate, maxDate)

	expectedStart := time.Date(time.Now().Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	if start != expectedStart {
		t.Errorf("start = %v, want %v (Jan 1 of current year)", start, expectedStart)
	}
	if end != maxDate {
		t.Errorf("end = %v, want %v (maxDate)", end, maxDate)
	}
}

// Regression: the YTD default window must be built on the same UTC calendar
// the ledger's dates are parsed in. Built in time.Local, a negative-offset
// zone pushed the window's start past midnight UTC and silently dropped
// January 1 rows from the dashboard's first render -- while the date filter
// beside them still read 01/01, and every drill-down (which posts explicit
// dates) counted them. Found as a $4.99 Jan 1 row, 2026-08-30.
func TestResolveDateRange_YTDDefaultIncludesJanuaryFirst(t *testing.T) {
	saved := time.Local
	time.Local = time.FixedZone("UTC-6", -6*60*60)
	defer func() { time.Local = saved }()

	year := time.Now().Year()
	janFirst := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	maxDate := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(makeTransaction("New Year purchase", -4.99, janFirst, models.Outflow, "Shopping"))

	start, end := resolveDateRange("", "", janFirst, maxDate)

	if got := ts.FilterByDateRange(start, end).Len(); got != 1 {
		t.Errorf("January 1 transactions inside the default window = %d, want 1 (start = %v)", got, start)
	}
}

func TestResolveDateRange_FallbackWhenYTDAfterMaxDate(t *testing.T) {
	// Data ends in the past, so YTD start would be after maxDate
	minDate := time.Date(2020, 3, 1, 0, 0, 0, 0, time.Local)
	maxDate := time.Date(2020, 12, 31, 0, 0, 0, 0, time.Local)

	start, end := resolveDateRange("", "", minDate, maxDate)

	// Should fall back to minDate since YTD (Jan 1 of current year) > maxDate
	if start != minDate {
		t.Errorf("start = %v, want %v (minDate fallback)", start, minDate)
	}
	if end != maxDate {
		t.Errorf("end = %v, want %v (maxDate)", end, maxDate)
	}
}

func TestResolveDateRange_EmptyEndDefaultsToMaxDate(t *testing.T) {
	minDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	maxDate := time.Date(2025, 9, 15, 0, 0, 0, 0, time.UTC)

	_, end := resolveDateRange("2025-03-01", "", minDate, maxDate)

	if end != maxDate {
		t.Errorf("end = %v, want %v (maxDate)", end, maxDate)
	}
}

// Regression: refunds within a month must reduce that month's total in the
// trend chart, so month-over-month change reflects net spending.
// Pre-fix: Jan=$1000, Feb=$1000 purchase + $200 refund produced Feb total
// $1200 (showing +20% increase) instead of $800 (-20% decrease).
func TestBuildSpendingTrendChartData_RefundReducesMonthlyTotal(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -1000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Rent", -1000, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Refund", 200, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	result := buildSpendingTrendChartData(ts)
	data := result["data"].([]map[string]interface{})
	trace := data[0]

	// Jan=1000, Feb=1000-200=800 -> change = -20%
	values := trace["y"].([]float64)
	if !floatEqual(values[0], -20.0) {
		t.Errorf("change = %.2f, want -20 (refund must reduce Feb total to 800)", values[0])
	}

	customdata := trace["customdata"].([][]float64)
	if !floatEqual(customdata[0][0], 800) {
		t.Errorf("Feb total = %.2f, want 800", customdata[0][0])
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

// TestBuildMerchantsChartData_RefundNetsAgainstMerchant is a CB3-B
// regression: a refund at a merchant must net against that merchant's
// total (signed), not inflate it via AbsAmount. Also covers the
// net-refund case: a merchant whose refunds outweigh its purchases must
// render a NEGATIVE bar.
func TestBuildMerchantsChartData_RefundNetsAgainstMerchant(t *testing.T) {
	ts := makeTransactionSet(
		// Mixed, still net-positive: 700 spend - 150 refund = 550.
		makeTransaction("Gadget Depot", -700, time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC), models.Outflow, "Shopping"),
		makeTransaction("Gadget Depot", 150, time.Date(2025, 2, 8, 0, 0, 0, 0, time.UTC), models.Outflow, "Shopping"),
		// Net-refund merchant: 100 spend - 900 refund = -800 (negative bar).
		makeTransaction("Outlet Mall", -100, time.Date(2025, 2, 2, 0, 0, 0, 0, time.UTC), models.Outflow, "Shopping"),
		makeTransaction("Outlet Mall", 900, time.Date(2025, 2, 9, 0, 0, 0, 0, time.UTC), models.Outflow, "Shopping"),
	)

	chart := buildMerchantsChartData(ts)
	data := chart["data"].([]map[string]interface{})
	trace := data[0]
	labels := trace["y"].([]string)
	values := trace["x"].([]float64)

	found := map[string]float64{}
	for i, label := range labels {
		found[label] = values[i]
	}

	gadget, ok := found["Gadget Depot"]
	if !ok {
		t.Fatalf("Gadget Depot not in chart: %v", found)
	}
	if !floatEqual(gadget, 550) {
		t.Errorf("Gadget Depot total = %.2f, want 550 (signed net); abs gives 850", gadget)
	}

	outlet, ok := found["Outlet Mall"]
	if !ok {
		t.Fatalf("Outlet Mall not in chart: %v", found)
	}
	if !floatEqual(outlet, -800) {
		t.Errorf("Outlet Mall total = %.2f, want -800 (net-refund merchant renders negative)", outlet)
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

// TestBuildCumulativeChartData_PositiveAmountOutflows (ruling
// CB3-2026-09-02a): this test used to pin "positive-amount outflows
// subtract (unsigned bank exports; use type not sign)". That premise
// CONFLICTS with the classifier's documented pipeline contract
// (classifier.go ClassifyTransactions): after classification, negative
// amounts are normalized for purchases and positive non-income amounts are
// DELIBERATELY kept positive as credits/refunds. An unsigned bank export
// can never reach this chart un-normalized through the real loader; the old
// fixture bypassed the classifier by constructing Outflow-typed rows with
// positive amounts directly. Superseded: the chart (and every CB3 surface)
// follows the pipeline contract -- a positive, Outflow-typed amount IS a
// refund and ADDS to cash flow, same fixture, new expectations. If
// unsigned-export support is ever needed, it belongs in the LOADER (which
// would normalize the sign before classification), not in per-chart abs.
func TestBuildCumulativeChartData_PositiveAmountOutflows(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", 1500, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Food", 500, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
	)

	result := buildCumulativeChartData(ts)
	data := result["data"].([]map[string]interface{})
	trace := data[0]
	cumulative := trace["y"].([]float64)

	// Day 1: +5000 (income). Day 5: a positive-amount Outflow-typed row is
	// a refund per the classifier's pipeline contract, so it ADDS:
	// 5000+1500=6500. Day 10: same, 6500+500=7000.
	if !floatEqual(cumulative[0], 5000) {
		t.Errorf("cumulative[0] = %v, want 5000", cumulative[0])
	}
	if !floatEqual(cumulative[1], 6500) {
		t.Errorf("cumulative[1] = %v, want 6500", cumulative[1])
	}
	if !floatEqual(cumulative[2], 7000) {
		t.Errorf("cumulative[2] = %v, want 7000", cumulative[2])
	}
}

// TestBuildCumulativeChartData_SkipsTransfers guards the one income/expense
// consumer in the app that is not a FilterByType call. Its else branch treats
// everything that is not Income as money leaving, so without an explicit skip
// both legs of a paired transfer would be SUBTRACTED from cumulative cash
// flow -- the transfer would read as double the spending it never was.
func TestBuildCumulativeChartData_SkipsTransfers(t *testing.T) {
	debit := makeTransaction("Schwab MoneyLink", -2000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Transfer, "Transfer")
	debit.TransferClass = "paired"
	debit.TransferPairKey = "abc123abc123"
	credit := makeTransaction("Transfer in from Schwab", 2000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Transfer, "Deposit")
	credit.TransferClass = "paired"
	credit.TransferPairKey = "abc123abc123"
	external := makeTransaction("Vanguard buy investment", -3000, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Transfer, "Investing")
	external.TransferClass = "external"

	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		debit, credit,
		makeTransaction("Food", -500, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		external,
	)

	result := buildCumulativeChartData(ts)
	data := result["data"].([]map[string]interface{})
	cumulative := data[0]["y"].([]float64)

	// Three dates: Jan 1, Jan 5 (transfers only), Jan 10 (food + external).
	if len(cumulative) != 3 {
		t.Fatalf("expected 3 data points, got %d", len(cumulative))
	}
	if !floatEqual(cumulative[0], 5000) {
		t.Errorf("cumulative[0] = %v, want 5000", cumulative[0])
	}
	if !floatEqual(cumulative[1], 5000) {
		t.Errorf("cumulative[1] = %v, want 5000 -- a transfer day moves the line by nothing", cumulative[1])
	}
	if !floatEqual(cumulative[2], 4500) {
		t.Errorf("cumulative[2] = %v, want 4500 -- only the 500 grocery run is spending", cumulative[2])
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

// TestBuildCumulativeChartData_RefundDayIncreasesRunningTotal is a CB3-C
// regression for the wrong-direction bug: a refund day (an Outflow-typed
// row with a positive amount, per the classifier's pipeline contract) must
// INCREASE the running cash-flow total, not decrease it.
func TestBuildCumulativeChartData_RefundDayIncreasesRunningTotal(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Salary", 2000, time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Groceries", -800, time.Date(2025, 4, 2, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		makeTransaction("Appliance Return", 300, time.Date(2025, 4, 3, 0, 0, 0, 0, time.UTC), models.Outflow, "Shopping"),
	)

	result := buildCumulativeChartData(ts)
	data := result["data"].([]map[string]interface{})
	cumulative := data[0]["y"].([]float64)

	// Day 1: 2000. Day 2: 2000-800=1200. Day 3 (refund day): the running
	// total must INCREASE to 1500; the wrong-direction bug would instead
	// subtract, giving 900.
	if !floatEqual(cumulative[0], 2000) {
		t.Errorf("cumulative[0] = %v, want 2000", cumulative[0])
	}
	if !floatEqual(cumulative[1], 1200) {
		t.Errorf("cumulative[1] = %v, want 1200", cumulative[1])
	}
	if !floatEqual(cumulative[2], 1500) {
		t.Errorf("cumulative[2] (refund day) = %v, want 1500 (refund ADDS cash); wrong-direction bug gives 900", cumulative[2])
	}
}

// --- buildBudgetVsActualChartData ---

func TestBuildBudgetVsActualChartData_Empty(t *testing.T) {
	ts := makeTransactionSet()

	result := buildBudgetVsActualChartData(ts, time.Time{}, time.Time{}, 0, 0, time.Time{}, false, nil)

	data, ok := result["data"].([]map[string]interface{})
	if !ok {
		t.Fatalf("data field missing or wrong type")
	}
	if len(data) != 0 {
		t.Errorf("data length = %d, want 0 for empty target+empty txns", len(data))
	}
}

func TestBuildBudgetVsActualChartData_Structure(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1500, jan, models.Outflow, "Housing"),
		makeTransaction("Rent", -1500, feb, models.Outflow, "Housing"),
		makeTransaction("Premium", -400, jan, models.Outflow, "Health Insurance"),
		makeTransaction("Premium", -400, feb, models.Outflow, "Health Insurance"),
	)

	result := buildBudgetVsActualChartData(ts, start, end, 1200, 350, start.AddDate(-1, 0, 0), true, nil)

	data, ok := result["data"].([]map[string]interface{})
	if !ok {
		t.Fatalf("data field missing")
	}
	// 3 traces: living bar, healthcare bar, cumulative line
	if len(data) != 3 {
		t.Fatalf("traces = %d, want 3 (living bar + healthcare bar + cumulative line)", len(data))
	}

	// Trace 0: living bar
	if data[0]["type"] != "bar" {
		t.Errorf("trace[0].type = %v, want bar", data[0]["type"])
	}
	if data[0]["name"] != "Living" {
		t.Errorf("trace[0].name = %v, want Living", data[0]["name"])
	}
	livingY := data[0]["y"].([]float64)
	if len(livingY) != 2 || !floatEqual(livingY[0], 1500) || !floatEqual(livingY[1], 1500) {
		t.Errorf("trace[0].y = %v, want [1500 1500]", livingY)
	}

	// Trace 1: healthcare bar
	if data[1]["name"] != "Healthcare" {
		t.Errorf("trace[1].name = %v, want Healthcare", data[1]["name"])
	}

	// Trace 2: cumulative line on subplot 2
	if data[2]["type"] != "scatter" {
		t.Errorf("trace[2].type = %v, want scatter", data[2]["type"])
	}
	if data[2]["yaxis"] != "y2" {
		t.Errorf("trace[2].yaxis = %v, want y2 (bottom subplot)", data[2]["yaxis"])
	}
	cumY := data[2]["y"].([]float64)
	// Combined target = 1550. Balance = target - actual.
	// Jan: 1550 - 1900 = -350 (overspent). Feb: cum = -350 + (-350) = -700.
	if len(cumY) != 2 || !floatEqual(cumY[0], -350) || !floatEqual(cumY[1], -700) {
		t.Errorf("trace[2].y = %v, want [-350 -700]", cumY)
	}

	// Layout has barmode=stack and a target line shape
	layout, ok := result["layout"].(map[string]interface{})
	if !ok {
		t.Fatalf("layout missing")
	}
	if layout["barmode"] != "stack" {
		t.Errorf("layout.barmode = %v, want stack", layout["barmode"])
	}
	shapes, ok := layout["shapes"].([]map[string]interface{})
	if !ok || len(shapes) == 0 {
		t.Fatalf("layout.shapes missing or empty; want target line + zero baseline")
	}
}

func TestBuildBudgetVsActualChartData_NoTarget(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1500, jan, models.Outflow, "Housing"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	result := buildBudgetVsActualChartData(ts, start, end, 0, 0, time.Time{}, false, nil)

	data := result["data"].([]map[string]interface{})
	if len(data) != 0 {
		t.Errorf("traces = %d, want 0 when no combined target (front end shows empty state)", len(data))
	}
}

// Regression: refund rows (opposite-signed Outflow) must reduce the month's
// effective outflow used by the new Budget vs Actual chart. Previously, on
// the now-removed Monthly Variance chart, $1500 purchases plus a $300 refund
// produced $1800 instead of $1200. Same invariant applies on the new builder.
func TestBuildBudgetVsActualChartData_RefundReducesMonthLiving(t *testing.T) {
	jan := time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, jan, models.Income, "Payroll"),
		makeTransaction("Rent", -1500, jan, models.Outflow, "Housing"),
		makeTransaction("Refund", 300, jan, models.Outflow, "Housing"),
	)

	result := buildBudgetVsActualChartData(ts, start, end, 1200, 0, time.Time{}, false, nil)

	data := result["data"].([]map[string]interface{})
	if len(data) == 0 {
		t.Fatalf("expected traces; got empty data")
	}
	livingY := data[0]["y"].([]float64)
	if !floatEqual(livingY[0], 1200) {
		t.Errorf("Jan living = %.2f, want 1200 (refund of +300 must subtract)", livingY[0])
	}
	cumY := data[2]["y"].([]float64)
	if !floatEqual(cumY[0], 0) {
		t.Errorf("Jan cumulative variance = %.2f, want 0 (1200 actual = 1200 target)", cumY[0])
	}
}
