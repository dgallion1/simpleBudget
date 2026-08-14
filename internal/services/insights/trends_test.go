package insights

import (
	"fmt"
	"math"
	"testing"
	"time"

	"budget2/internal/models"
)

// income creates an income transaction at a fixed date (not relative to now)
func income(desc string, amount float64, date time.Time) models.Transaction {
	return models.Transaction{
		Description:     desc,
		Amount:          amount,
		Date:            date,
		TransactionType: models.Income,
	}
}

// catTxn creates an outflow transaction with a category at a specific date
func catTxn(desc, category string, amount float64, date time.Time) models.Transaction {
	return models.Transaction{
		Description:     desc,
		Amount:          -amount,
		Category:        category,
		Date:            date,
		TransactionType: models.Outflow,
	}
}

// --- CategoryTrends tests ---

func TestAnalyzeCategoryTrends_BasicUpDown(t *testing.T) {
	// Current period: Feb 2025, Previous period: Jan 2025
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			// Previous period (Jan): food=$100, transport=$200
			catTxn("grocery store", "food", 100, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)),
			catTxn("bus pass", "transport", 200, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)),
			// Current period (Feb): food=$200 (up), transport=$50 (down)
			catTxn("grocery store", "food", 200, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)),
			catTxn("bus pass", "transport", 50, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)),
		},
	}

	trends := CategoryTrends(ts, currentStart, currentEnd)

	foodFound, transportFound := false, false
	for _, tr := range trends {
		switch tr.Category {
		case "food":
			foodFound = true
			if tr.Direction != "up" {
				t.Errorf("food direction = %q, want \"up\"", tr.Direction)
			}
			if tr.CurrentAmount != 200 {
				t.Errorf("food CurrentAmount = %.2f, want 200", tr.CurrentAmount)
			}
			if tr.PreviousAmount != 100 {
				t.Errorf("food PreviousAmount = %.2f, want 100", tr.PreviousAmount)
			}
		case "transport":
			transportFound = true
			if tr.Direction != "down" {
				t.Errorf("transport direction = %q, want \"down\"", tr.Direction)
			}
			if tr.CurrentAmount != 50 {
				t.Errorf("transport CurrentAmount = %.2f, want 50", tr.CurrentAmount)
			}
			if tr.PreviousAmount != 200 {
				t.Errorf("transport PreviousAmount = %.2f, want 200", tr.PreviousAmount)
			}
		}
	}
	if !foodFound {
		t.Error("expected 'food' category in trends")
	}
	if !transportFound {
		t.Error("expected 'transport' category in trends")
	}
}

func TestAnalyzeCategoryTrends_StableCategory(t *testing.T) {
	// Spending changes < 5% should be "stable"
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			// Previous: $100
			catTxn("rent", "housing", 100, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)),
			// Current: $103 (3% change, under 5% threshold)
			catTxn("rent", "housing", 103, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)),
		},
	}

	trends := CategoryTrends(ts, currentStart, currentEnd)

	found := false
	for _, tr := range trends {
		if tr.Category == "housing" {
			found = true
			if tr.Direction != "stable" {
				t.Errorf("housing direction = %q, want \"stable\"", tr.Direction)
			}
		}
	}
	if !found {
		t.Error("expected 'housing' category in trends")
	}
}

func TestAnalyzeCategoryTrends_MaxTenCategories(t *testing.T) {
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	var txns []models.Transaction
	for i := 0; i < 12; i++ {
		cat := fmt.Sprintf("category-%d", i)
		// Add both previous and current period so we get a trend entry
		txns = append(txns,
			catTxn("vendor", cat, float64(10+i), time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)),
			catTxn("vendor", cat, float64(20+i), time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)),
		)
	}

	ts := &models.TransactionSet{Transactions: txns}
	trends := CategoryTrends(ts, currentStart, currentEnd)

	if len(trends) > 10 {
		t.Errorf("expected at most 10 category trends, got %d", len(trends))
	}
}

// --- SpendingVelocity tests ---

func TestCalculateSpendingVelocity_BasicCalculation(t *testing.T) {
	now := time.Now()
	// 10 days of spending, $100/day
	var txns []models.Transaction
	for i := 0; i < 10; i++ {
		txns = append(txns, models.Transaction{
			Description:     "spending",
			Amount:          -100,
			Date:            now.AddDate(0, 0, -i),
			TransactionType: models.Outflow,
		})
	}

	currentPeriod := &models.TransactionSet{Transactions: txns}
	allData := currentPeriod

	velocity := SpendingVelocity(currentPeriod, allData)

	if velocity.DailyAverage == 0 {
		t.Fatal("DailyAverage should be > 0")
	}
	// 10 transactions of $100 over 10 days = $100/day
	expectedDaily := 1000.0 / 10.0
	if math.Abs(velocity.DailyAverage-expectedDaily) > 1.0 {
		t.Errorf("DailyAverage = %.2f, want ~%.2f", velocity.DailyAverage, expectedDaily)
	}
}

func TestCalculateSpendingVelocity_EmptyData(t *testing.T) {
	empty := &models.TransactionSet{}
	velocity := SpendingVelocity(empty, empty)

	if velocity.DailyAverage != 0 {
		t.Errorf("DailyAverage = %.2f, want 0", velocity.DailyAverage)
	}
	if velocity.HistoricalDaily != 0 {
		t.Errorf("HistoricalDaily = %.2f, want 0", velocity.HistoricalDaily)
	}
	if velocity.MonthProjection != 0 {
		t.Errorf("MonthProjection = %.2f, want 0", velocity.MonthProjection)
	}
}

// Regression: a refund (opposite-signed Outflow row) must REDUCE the burn rate
// numerator, not be added as an absolute value. Pre-fix: $600 of purchases
// plus a $50 refund produced DailyAverage=$650/day instead of $550/day.
func TestCalculateSpendingVelocity_RefundReducesDailyAverage(t *testing.T) {
	now := time.Now()
	txns := []models.Transaction{
		{Description: "purchase", Amount: -100, Date: now, TransactionType: models.Outflow},
		{Description: "purchase", Amount: -200, Date: now, TransactionType: models.Outflow},
		{Description: "purchase", Amount: -300, Date: now, TransactionType: models.Outflow},
		{Description: "refund", Amount: 50, Date: now, TransactionType: models.Outflow}, // opposite sign
	}
	period := &models.TransactionSet{Transactions: txns}

	velocity := SpendingVelocity(period, period)

	// All same day -> currentDays clamped to 1, so DailyAverage = net spend.
	const want = 550.0
	if math.Abs(velocity.DailyAverage-want) > 0.01 {
		t.Errorf("DailyAverage = %.2f, want %.2f (refund must subtract, not add)", velocity.DailyAverage, want)
	}
	if math.Abs(velocity.HistoricalDaily-want) > 0.01 {
		t.Errorf("HistoricalDaily = %.2f, want %.2f", velocity.HistoricalDaily, want)
	}
}

// --- IncomePatterns tests ---

func TestAnalyzeIncomePatterns_MonthlyIncome(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			income("employer", 5000, base),
			income("employer", 5000, base.AddDate(0, 1, 0)),
			income("employer", 5000, base.AddDate(0, 2, 0)),
			income("employer", 5000, base.AddDate(0, 3, 0)),
		},
	}

	patterns := IncomePatterns(ts)

	found := false
	for _, p := range patterns {
		if p.Description == "employer" {
			found = true
			if p.Frequency != "monthly" {
				t.Errorf("frequency = %q, want \"monthly\"", p.Frequency)
			}
			if !p.IsRegular {
				t.Error("IsRegular = false, want true")
			}
			if p.Occurrences != 4 {
				t.Errorf("Occurrences = %d, want 4", p.Occurrences)
			}
		}
	}
	if !found {
		t.Error("expected 'employer' in income patterns")
	}
}

func TestAnalyzeIncomePatterns_IrregularIncome(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			income("freelance", 500, base),
			income("freelance", 800, base.AddDate(0, 0, 10)),   // 10 days later
			income("freelance", 300, base.AddDate(0, 0, 55)),   // 45 days later
			income("freelance", 1200, base.AddDate(0, 0, 120)), // 65 days later
		},
	}

	patterns := IncomePatterns(ts)

	found := false
	for _, p := range patterns {
		if p.Description == "freelance" {
			found = true
			if p.IsRegular {
				t.Error("IsRegular = true, want false for irregular income")
			}
			if p.Frequency != "irregular" {
				t.Errorf("frequency = %q, want \"irregular\"", p.Frequency)
			}
		}
	}
	if !found {
		t.Error("expected 'freelance' in income patterns")
	}
}

func TestAnalyzeIncomePatterns_MaxTenPatterns(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	var txns []models.Transaction
	for i := 0; i < 12; i++ {
		src := fmt.Sprintf("source-%d", i)
		// Each source needs at least 2 occurrences to appear in patterns
		txns = append(txns,
			income(src, float64(100+i*10), base),
			income(src, float64(100+i*10), base.AddDate(0, 1, 0)),
		)
	}

	ts := &models.TransactionSet{Transactions: txns}
	patterns := IncomePatterns(ts)

	if len(patterns) > 10 {
		t.Errorf("expected at most 10 income patterns, got %d", len(patterns))
	}
}

// --- Additional coverage tests ---

// TestAnalyzeCategoryTrends_NewCategory covers the previous==0 && current>0 branch
func TestAnalyzeCategoryTrends_NewCategory(t *testing.T) {
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			// Only current period, no previous
			catTxn("new store", "new-cat", 150, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)),
		},
	}

	trends := CategoryTrends(ts, currentStart, currentEnd)

	for _, tr := range trends {
		if tr.Category == "new-cat" {
			if tr.Direction != "up" {
				t.Errorf("direction = %q, want \"up\"", tr.Direction)
			}
			if tr.ChangePercent != 100 {
				t.Errorf("changePercent = %.2f, want 100", tr.ChangePercent)
			}
			return
		}
	}
	t.Error("expected 'new-cat' in trends")
}

// TestAnalyzeCategoryTrends_OnlyInPreviousPeriod covers a category that was in
// previous period but not current (current==0, previous>0 => down)
func TestAnalyzeCategoryTrends_OnlyInPreviousPeriod(t *testing.T) {
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			// Only in previous period
			catTxn("gym", "fitness", 50, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)),
		},
	}

	trends := CategoryTrends(ts, currentStart, currentEnd)

	for _, tr := range trends {
		if tr.Category == "fitness" {
			if tr.Direction != "down" {
				t.Errorf("direction = %q, want \"down\"", tr.Direction)
			}
			return
		}
	}
	t.Error("expected 'fitness' in trends")
}

// TestAnalyzeIncomePatterns_TooFew covers the < 2 income transactions path
func TestAnalyzeIncomePatterns_TooFew(t *testing.T) {
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			income("employer", 5000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
	}
	patterns := IncomePatterns(ts)
	if len(patterns) != 0 {
		t.Errorf("expected 0 patterns for < 2 income transactions, got %d", len(patterns))
	}
}

// TestAnalyzeIncomePatterns_SingleOccurrenceGroup covers the len(txns) < 2 continue
func TestAnalyzeIncomePatterns_SingleOccurrenceGroup(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			income("employer", 5000, base),
			income("employer", 5000, base.AddDate(0, 1, 0)),
			income("one-time bonus", 1000, base), // only 1 occurrence
		},
	}
	patterns := IncomePatterns(ts)
	for _, p := range patterns {
		if p.Description == "one-time bonus" {
			t.Error("single occurrence should not appear in patterns")
		}
	}
}

// TestAnalyzeIncomePatterns_WeeklyIncome covers the weekly branch
func TestAnalyzeIncomePatterns_WeeklyIncome(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			income("gig work", 200, base),
			income("gig work", 200, base.AddDate(0, 0, 7)),
			income("gig work", 200, base.AddDate(0, 0, 14)),
			income("gig work", 200, base.AddDate(0, 0, 21)),
		},
	}
	patterns := IncomePatterns(ts)
	for _, p := range patterns {
		if p.Description == "gig work" {
			if p.Frequency != "weekly" {
				t.Errorf("frequency = %q, want \"weekly\"", p.Frequency)
			}
			if !p.IsRegular {
				t.Error("expected IsRegular = true")
			}
			return
		}
	}
	t.Error("expected 'gig work' in patterns")
}

// TestAnalyzeIncomePatterns_BiweeklyIncome covers the biweekly branch
func TestAnalyzeIncomePatterns_BiweeklyIncome(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			income("salary", 2500, base),
			income("salary", 2500, base.AddDate(0, 0, 14)),
			income("salary", 2500, base.AddDate(0, 0, 28)),
			income("salary", 2500, base.AddDate(0, 0, 42)),
		},
	}
	patterns := IncomePatterns(ts)
	for _, p := range patterns {
		if p.Description == "salary" {
			if p.Frequency != "biweekly" {
				t.Errorf("frequency = %q, want \"biweekly\"", p.Frequency)
			}
			if !p.IsRegular {
				t.Error("expected IsRegular = true")
			}
			return
		}
	}
	t.Error("expected 'salary' in patterns")
}

// TestCalculateSpendingVelocity_BurnRateChange covers the burn rate change calculation
func TestCalculateSpendingVelocity_BurnRateChange(t *testing.T) {
	now := time.Now()
	// Current period: last 30 days, $200/day
	var currentTxns []models.Transaction
	for i := 0; i < 30; i++ {
		currentTxns = append(currentTxns, models.Transaction{
			Description:     "spending",
			Amount:          -200,
			Date:            now.AddDate(0, 0, -i),
			TransactionType: models.Outflow,
		})
	}

	// All data: 60 days, first 30 days at $100/day
	allTxns := make([]models.Transaction, len(currentTxns))
	copy(allTxns, currentTxns)
	for i := 30; i < 60; i++ {
		allTxns = append(allTxns, models.Transaction{
			Description:     "spending",
			Amount:          -100,
			Date:            now.AddDate(0, 0, -i),
			TransactionType: models.Outflow,
		})
	}

	currentPeriod := &models.TransactionSet{Transactions: currentTxns}
	allData := &models.TransactionSet{Transactions: allTxns}

	velocity := SpendingVelocity(currentPeriod, allData)

	if velocity.BurnRateChange == 0 {
		t.Error("BurnRateChange should be non-zero when current > historical")
	}
}

// --- Remaining edge case coverage ---

// TestCalculateSpendingVelocity_SingleDayData covers currentDays < 1 and allDays < 1
func TestCalculateSpendingVelocity_SingleDayData(t *testing.T) {
	now := time.Now()
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "purchase", Amount: -50, Date: now, TransactionType: models.Outflow},
		},
	}
	velocity := SpendingVelocity(ts, ts)
	// With single day, currentDays = 1 (0 + 1 = 1 after rounding, but
	// maxDate - minDate = 0 days, so 0 + 1 = 1; the < 1 branch may not trigger)
	// The important thing is it doesn't panic
	if velocity.DailyAverage == 0 {
		t.Error("DailyAverage should be > 0 for single transaction")
	}
}

// TestAnalyzeCategoryTrends_CategoryOnlyInPrevious covers current==0 with previous>0
// The changePercent formula: ((0 - prev) / prev) * 100 = -100%
func TestAnalyzeCategoryTrends_DisappearedCategory(t *testing.T) {
	currentStart := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			// Only in previous period (Feb)
			catTxn("gym membership", "fitness", 60, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC)),
		},
	}

	trends := CategoryTrends(ts, currentStart, currentEnd)
	for _, tr := range trends {
		if tr.Category == "fitness" {
			if tr.CurrentAmount != 0 {
				t.Errorf("CurrentAmount = %.2f, want 0", tr.CurrentAmount)
			}
			if tr.Direction != "down" {
				t.Errorf("direction = %q, want \"down\"", tr.Direction)
			}
			return
		}
	}
	t.Error("expected 'fitness' category in trends")
}

// --- MajorExpenseTrends tests ---

func TestAnalyzeMajorExpenseTrends_GroupsByExpenseName(t *testing.T) {
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	defs := []models.MajorExpense{
		{ID: "mortgage", Name: "Mortgage", Keywords: []string{"wells fargo home"}},
		{ID: "tesla", Name: "Tesla Loan", Keywords: []string{"tesla finance"}},
	}

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			// Previous period
			catTxn("Wells Fargo Home Mortgage", "housing", 2000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC)),
			catTxn("Tesla Finance Loan", "transport", 800, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)),
			// Unmatched outflow that should NOT appear in trends
			catTxn("Trader Joes Grocery", "food", 250, time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC)),
			// Current period
			catTxn("Wells Fargo Home Mortgage", "housing", 2300, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC)),
			catTxn("Tesla Finance Loan", "transport", 800, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC)),
			catTxn("Trader Joes Grocery", "food", 999, time.Date(2025, 2, 12, 0, 0, 0, 0, time.UTC)),
		},
	}

	trends := MajorExpenseTrends(ts, defs, nil, currentStart, currentEnd)

	got := make(map[string]models.CategoryTrend, len(trends))
	for _, tr := range trends {
		got[tr.Category] = tr
	}

	if _, ok := got["food"]; ok {
		t.Errorf("unmatched outflows must not appear in trends; saw category=food")
	}

	mortgage, ok := got["Mortgage"]
	if !ok {
		t.Fatalf("expected trend for major-expense name 'Mortgage', got categories: %v", keysOf(got))
	}
	if mortgage.CurrentAmount != 2300 || mortgage.PreviousAmount != 2000 {
		t.Errorf("Mortgage amounts = (current=%.2f, previous=%.2f), want (2300, 2000)", mortgage.CurrentAmount, mortgage.PreviousAmount)
	}
	if mortgage.Direction != "up" {
		t.Errorf("Mortgage direction = %q, want \"up\"", mortgage.Direction)
	}

	tesla, ok := got["Tesla Loan"]
	if !ok {
		t.Fatalf("expected trend for 'Tesla Loan'")
	}
	if tesla.Direction != "stable" {
		t.Errorf("Tesla Loan direction = %q, want \"stable\" (no change)", tesla.Direction)
	}
}

func TestAnalyzeMajorExpenseTrends_PinOverridesKeyword(t *testing.T) {
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	defs := []models.MajorExpense{
		{ID: "groc", Name: "Groceries", Keywords: []string{"trader joe"}},
		{ID: "treat", Name: "Eating Out"},
	}

	pinned := models.Transaction{
		Description:     "Trader Joe's Run",
		Amount:          -150,
		Hash:            "h-trader-pin",
		Date:            time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC),
		TransactionType: models.Outflow,
	}
	ts := &models.TransactionSet{Transactions: []models.Transaction{pinned}}
	pins := map[string]string{"h-trader-pin": "treat"}

	trends := MajorExpenseTrends(ts, defs, pins, currentStart, currentEnd)

	for _, tr := range trends {
		if tr.Category == "Groceries" {
			t.Errorf("pinned txn fell back to keyword match; got Groceries with current=%.2f", tr.CurrentAmount)
		}
	}
	found := false
	for _, tr := range trends {
		if tr.Category == "Eating Out" && tr.CurrentAmount == 150 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pinned $150 outflow to land in 'Eating Out'; got %+v", trends)
	}
}

func TestAnalyzeMajorExpenseTrends_NoDefsReturnsNil(t *testing.T) {
	ts := &models.TransactionSet{Transactions: []models.Transaction{
		catTxn("anything", "food", 10, time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
	}}
	if got := MajorExpenseTrends(ts, nil, nil, time.Now().AddDate(0, -1, 0), time.Now()); got != nil {
		t.Errorf("expected nil trends with no defs, got %v", got)
	}
}

func keysOf(m map[string]models.CategoryTrend) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
