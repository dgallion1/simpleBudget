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

// TestAnalyzeMajorExpenseTrends_RefundInNormalPeriodSignedNet is a CB3-D
// regression: a refund inside an otherwise outflow-dominant period must
// reduce the period total via signed net, not inflate it via AbsAmount.
// Delta Air Lines purchase -450, a positive-amount (non-income-keyword)
// airline credit +100 -> signed net 350; the old per-txn abs bug gives 550.
func TestAnalyzeMajorExpenseTrends_RefundInNormalPeriodSignedNet(t *testing.T) {
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	defs := []models.MajorExpense{{ID: "air", Name: "Airline", Keywords: []string{"delta air"}}}
	ts := &models.TransactionSet{Transactions: []models.Transaction{
		{Description: "Delta Air Lines Ticket", Amount: -450, Category: "Travel",
			Date: time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow},
		{Description: "Delta Air Lines Flight Credit", Amount: 100, Category: "Travel",
			Date: time.Date(2025, 2, 12, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow},
	}}

	trends := MajorExpenseTrends(ts, defs, nil, currentStart, currentEnd)
	got := make(map[string]models.CategoryTrend, len(trends))
	for _, tr := range trends {
		got[tr.Category] = tr
	}
	air, ok := got["Airline"]
	if !ok {
		t.Fatalf("expected trend for 'Airline', got categories: %v", keysOf(got))
	}
	if math.Abs(air.CurrentAmount-350) > 0.01 {
		t.Errorf("Airline CurrentAmount = %.2f, want 350 (signed net); abs gives 550", air.CurrentAmount)
	}
	// CB3-c: no previous-period Airline activity (previous=0), current=350>0
	// -> ChangePercent=+100, Direction="up". Asserting these (not just
	// CurrentAmount) is what would have caught the CB3-c live bug.
	if math.Abs(air.ChangePercent-100) > 0.01 {
		t.Errorf("Airline ChangePercent = %.2f, want 100", air.ChangePercent)
	}
	if air.Direction != "up" {
		t.Errorf("Airline Direction = %q, want \"up\"", air.Direction)
	}
}

// TestAnalyzeMajorExpenseTrends_RefundDominantPeriodSignedNet is a CB3-D
// regression: a REFUND-DOMINANT period (refunds outweigh purchases) must
// render a NEGATIVE period total, matching CB3-A's drilldown contract.
// Purchase -150, credit +650 -> signed net -(-150+650) = -500.
func TestAnalyzeMajorExpenseTrends_RefundDominantPeriodSignedNet(t *testing.T) {
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	defs := []models.MajorExpense{{ID: "air", Name: "Airline", Keywords: []string{"delta air"}}}
	ts := &models.TransactionSet{Transactions: []models.Transaction{
		{Description: "Delta Air Lines Ticket", Amount: -150, Category: "Travel",
			Date: time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow},
		{Description: "Delta Air Lines Flight Credit", Amount: 650, Category: "Travel",
			Date: time.Date(2025, 2, 12, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow},
	}}

	trends := MajorExpenseTrends(ts, defs, nil, currentStart, currentEnd)
	got := make(map[string]models.CategoryTrend, len(trends))
	for _, tr := range trends {
		got[tr.Category] = tr
	}
	air, ok := got["Airline"]
	if !ok {
		t.Fatalf("expected trend for 'Airline', got categories: %v", keysOf(got))
	}
	if math.Abs(air.CurrentAmount-(-500)) > 0.01 {
		t.Errorf("Airline CurrentAmount = %.2f, want -500 (refund-dominant period renders negative)", air.CurrentAmount)
	}
	// CB3-c conceded shape (previous=0, current<0) -> Direction="down",
	// ChangePercent=-100 (sign-consistent replacement for the old flat
	// +100/"up" that ignored the sign of change entirely).
	if math.Abs(air.ChangePercent-(-100)) > 0.01 {
		t.Errorf("Airline ChangePercent = %.2f, want -100", air.ChangePercent)
	}
	if air.Direction != "down" {
		t.Errorf("Airline Direction = %q, want \"down\"", air.Direction)
	}
}

// TestAnalyzeMajorExpenseTrends_ZeroCurrentNetRefundPreviousSignConsistent
// covers the OTHER CB3-c conceded shape: current=0, previous<0 (a
// refund-dominant PREVIOUS period, nothing this period). change =
// 0-(-600)=+600 must report Direction="up" / ChangePercent>0 -- the live
// bug divided by the SIGNED previous (-600) and got changePercent=-100,
// "down", self-contradicting a positive change (spending fell to zero,
// which is an improvement, not a decline).
func TestAnalyzeMajorExpenseTrends_ZeroCurrentNetRefundPreviousSignConsistent(t *testing.T) {
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	defs := []models.MajorExpense{{ID: "furn", Name: "Furniture", Keywords: []string{"comfy furniture co"}}}
	ts := &models.TransactionSet{Transactions: []models.Transaction{
		// Previous period (Jan): refund-dominant, net -600.
		{Description: "Comfy Furniture Co Purchase", Amount: -80, Category: "Home",
			Date: time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow},
		{Description: "Comfy Furniture Co Store Credit", Amount: 680, Category: "Home",
			Date: time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow},
		// Current period (Feb): nothing for Furniture.
	}}

	trends := MajorExpenseTrends(ts, defs, nil, currentStart, currentEnd)
	got := make(map[string]models.CategoryTrend, len(trends))
	for _, tr := range trends {
		got[tr.Category] = tr
	}
	furn, ok := got["Furniture"]
	if !ok {
		t.Fatalf("expected trend for 'Furniture', got categories: %v", keysOf(got))
	}
	if math.Abs(furn.PreviousAmount-(-600)) > 0.01 || math.Abs(furn.CurrentAmount-0) > 0.01 {
		t.Fatalf("harness error: current=%.2f previous=%.2f, want 0/-600", furn.CurrentAmount, furn.PreviousAmount)
	}
	if furn.Direction != "up" {
		t.Errorf("Direction = %q, want \"up\" (ChangeAmount=+600); the live bug divided by signed previous and reported \"down\"", furn.Direction)
	}
	if furn.ChangePercent <= 0 {
		t.Errorf("ChangePercent = %.2f, want positive (sign must agree with ChangeAmount=+600)", furn.ChangePercent)
	}
}

// TestAnalyzeMajorExpenseTrends_PinnedRefundNetsSigned is a CB3-c
// mutation-survivor regression: both a spend row and a refund row reach
// their MajorExpense def via the PIN path (models.ResolveByIdentity), not
// keyword matching -- the def's keywords deliberately do NOT match either
// description, so a broken pin lookup would leave both rows unmatched
// (and this trend absent) instead of merely wrong. Signed net:
// -450 (spend) + 130 (refund) -> total = -(-450+130) = 320; abs gives 580.
func TestAnalyzeMajorExpenseTrends_PinnedRefundNetsSigned(t *testing.T) {
	currentStart := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)

	defs := []models.MajorExpense{{ID: "furn2", Name: "Furniture Set", Keywords: []string{"zzz-nomatch-2"}}}
	spend := models.Transaction{
		Description: "Some Furniture Store", Amount: -450, Category: "Home",
		Date: time.Date(2025, 3, 5, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow,
		Hash: "h-cb3c-pin-spend",
	}
	refund := models.Transaction{
		Description: "Some Furniture Store Refund Credit", Amount: 130, Category: "Home",
		Date: time.Date(2025, 3, 12, 0, 0, 0, 0, time.UTC), TransactionType: models.Outflow,
		Hash: "h-cb3c-pin-refund",
	}
	pins := map[string]string{
		"h-cb3c-pin-spend":  "furn2",
		"h-cb3c-pin-refund": "furn2",
	}
	ts := &models.TransactionSet{Transactions: []models.Transaction{spend, refund}}

	trends := MajorExpenseTrends(ts, defs, pins, currentStart, currentEnd)
	got := make(map[string]models.CategoryTrend, len(trends))
	for _, tr := range trends {
		got[tr.Category] = tr
	}
	set, ok := got["Furniture Set"]
	if !ok {
		t.Fatalf("expected pinned trend for 'Furniture Set' (keywords do not match either description), got categories: %v", keysOf(got))
	}
	if math.Abs(set.CurrentAmount-320) > 0.01 {
		t.Errorf("pinned CurrentAmount = %.2f, want 320 (signed net via PIN path); abs gives 580", set.CurrentAmount)
	}
}

// TestCalculateSpendingVelocity_MonthProjectionIdentity is a CB3-c
// mutation-survivor regression for the spentSoFar site: rows must be
// dated INSIDE the current calendar month in time.Local, since
// SpendingVelocity's month-to-date bucket is [month-start, now) in
// time.Local -- a UTC-midnight first-of-month row can fall BEFORE that
// local month start west of UTC, silently missing the bucket. Asserts the
// exact identity, not just the sign, so a mutant that flips spentSoFar
// back to abs (shifting MonthProjection by 2*|signed net|) is caught.
func TestCalculateSpendingVelocity_MonthProjectionIdentity(t *testing.T) {
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	d2 := now.Add(-time.Hour)
	if d2.Before(monthStart) {
		d2 = now
	}
	d1 := d2.Add(-time.Hour)
	if d1.Before(monthStart) {
		d1 = monthStart.Add(time.Minute)
		// If `now` itself is within the first minute of the month,
		// monthStart+1min can be AFTER now, pushing d1 back OUT of the
		// [monthStart, now] bucket (a 60s/year flake); clamp to whichever
		// of the two is earlier.
		if d1.After(now) {
			d1 = now
		}
	}

	ts := &models.TransactionSet{Transactions: []models.Transaction{
		{Description: "purchase", Amount: -280, Date: d1, TransactionType: models.Outflow},
		{Description: "merchandise credit", Amount: 860, Date: d2, TransactionType: models.Outflow},
	}}

	velocity := SpendingVelocity(ts, ts)

	signedSpent := -580.0 // -(-280 + 860)
	expected := signedSpent + velocity.DailyAverage*float64(velocity.DaysRemaining)
	if math.Abs(velocity.MonthProjection-expected) > 0.01 {
		t.Errorf("MonthProjection = %.2f, want %.2f (signedSpent %.2f + DailyAverage %.2f * DaysRemaining %d); an abs spentSoFar mutant breaks this identity",
			velocity.MonthProjection, expected, signedSpent, velocity.DailyAverage, velocity.DaysRemaining)
	}
}

// TestCalculateSpendingVelocity_RefundDominantPeriodIsNegative is a CB3-D
// regression: unlike TestCalculateSpendingVelocity_RefundReducesDailyAverage
// above (a net-positive period whose refund merely reduces the average),
// this period's refunds OUTWEIGH its purchases, so DailyAverage and
// MonthProjection must go negative -- honest, not clamped to zero.
func TestCalculateSpendingVelocity_RefundDominantPeriodIsNegative(t *testing.T) {
	now := time.Now()
	txns := []models.Transaction{
		{Description: "purchase", Amount: -150, Date: now, TransactionType: models.Outflow},
		{Description: "purchase", Amount: -200, Date: now, TransactionType: models.Outflow},
		{Description: "merchandise credit", Amount: 900, Date: now, TransactionType: models.Outflow},
	}
	period := &models.TransactionSet{Transactions: txns}

	velocity := SpendingVelocity(period, period)

	// All same day -> currentDays clamped to 1: net = -(-150-200+900) = -550.
	const want = -550.0
	if math.Abs(velocity.DailyAverage-want) > 0.01 {
		t.Errorf("DailyAverage = %.2f, want %.2f (refund-dominant period is negative)", velocity.DailyAverage, want)
	}
	if math.Abs(velocity.HistoricalDaily-want) > 0.01 {
		t.Errorf("HistoricalDaily = %.2f, want %.2f", velocity.HistoricalDaily, want)
	}
	if velocity.MonthProjection >= 0 {
		t.Errorf("MonthProjection = %.2f, want negative for a refund-dominant month-to-date", velocity.MonthProjection)
	}
	// CB8: period == allData here, so dailyAvg == historicalDaily exactly
	// and change == 0 -- pins that IDENTICAL current/historical sets
	// report a flat 0%, not +-100 (a zero-base-style mutant that ignores
	// historicalDaily's actual nonzero value would fail this).
	if velocity.BurnRateChange != 0 {
		t.Errorf("BurnRateChange = %.4f, want exactly 0 (current period == historical period)", velocity.BurnRateChange)
	}
}

// TestCalculateSpendingVelocity_RefundDominantHistoryStillReportsChange is a
// CB8 regression: CB3-D made historicalDaily signed but left the `> 0` guard
// on burnRateChange, so a ledger whose ENTIRE HISTORY nets a refund
// (historicalDaily < 0) silently reported BurnRateChange=0 no matter how
// fast the current period was spending. allData here is the current
// period's purchases plus a much larger, much older refund, so the
// ledger-wide net is negative while the current period is genuinely
// spending. The old `> 0` guard yields 0 for this fixture; ruling
// CB8-2026-09-03a's |historicalDaily| formula must report the real,
// positive change instead.
func TestCalculateSpendingVelocity_RefundDominantHistoryStillReportsChange(t *testing.T) {
	now := time.Now()
	currentTxns := []models.Transaction{
		{Description: "purchase", Amount: -300, Date: now, TransactionType: models.Outflow},
	}
	currentPeriod := &models.TransactionSet{Transactions: currentTxns}

	allTxns := make([]models.Transaction, len(currentTxns))
	copy(allTxns, currentTxns)
	allTxns = append(allTxns, models.Transaction{
		Description:     "large old refund",
		Amount:          1200,
		Date:            now.AddDate(0, 0, -65), // 65 days ago: well before the current period
		TransactionType: models.Outflow,
	})
	allData := &models.TransactionSet{Transactions: allTxns}

	velocity := SpendingVelocity(currentPeriod, allData)

	if velocity.HistoricalDaily >= 0 {
		t.Fatalf("harness error: HistoricalDaily = %.4f, want negative (refund-dominant ledger)", velocity.HistoricalDaily)
	}
	if velocity.DailyAverage <= 0 {
		t.Fatalf("harness error: DailyAverage = %.4f, want positive (current period is spending)", velocity.DailyAverage)
	}

	want := (velocity.DailyAverage - velocity.HistoricalDaily) / math.Abs(velocity.HistoricalDaily) * 100
	if math.Abs(velocity.BurnRateChange-want) > 0.01 {
		t.Errorf("BurnRateChange = %.4f, want %.4f (the old `> 0` guard on historicalDaily would report 0 here)", velocity.BurnRateChange, want)
	}
	if velocity.BurnRateChange <= 0 {
		t.Errorf("BurnRateChange = %.4f, want positive (spending faster than a refund-dominant history is an increase)", velocity.BurnRateChange)
	}
}

// TestCalculateSpendingVelocity_ZeroHistoricalBaseSpendingIsPositive100 is a
// CB8 regression: when historicalDaily is EXACTLY zero (the ledger's
// outflow-typed rows net to zero), the zero-base case must pick its result
// by the SIGN OF CHANGE (CB3-c's rule), not call metrics.PercentChange
// (whose zero-base case is an unconditional +100 regardless of direction).
// Here change is positive (a spending current period against a flat
// history), so the sign-of-change rule and PercentChange happen to agree
// on +100 -- the companion refund-current test below is what actually
// distinguishes the two implementations.
func TestCalculateSpendingVelocity_ZeroHistoricalBaseSpendingIsPositive100(t *testing.T) {
	now := time.Now()
	allData := &models.TransactionSet{Transactions: []models.Transaction{
		{Description: "purchase", Amount: -300, Date: now.AddDate(0, 0, -60), TransactionType: models.Outflow},
		{Description: "refund", Amount: 300, Date: now.AddDate(0, 0, -59), TransactionType: models.Outflow},
	}}
	currentPeriod := &models.TransactionSet{Transactions: []models.Transaction{
		{Description: "purchase", Amount: -100, Date: now, TransactionType: models.Outflow},
	}}

	velocity := SpendingVelocity(currentPeriod, allData)

	if velocity.HistoricalDaily != 0 {
		t.Fatalf("harness error: HistoricalDaily = %.4f, want exactly 0", velocity.HistoricalDaily)
	}
	if velocity.DailyAverage <= 0 {
		t.Fatalf("harness error: DailyAverage = %.4f, want positive", velocity.DailyAverage)
	}
	if velocity.BurnRateChange != 100 {
		t.Errorf("BurnRateChange = %.4f, want exactly 100 (zero base, positive change)", velocity.BurnRateChange)
	}
}

// TestCalculateSpendingVelocity_ZeroHistoricalBaseRefundIsNegative100 is the
// CB8 companion to the test above: historicalDaily is exactly zero again,
// but the CURRENT period is refund-dominant (change is negative). The old
// `> 0` guard never reached this branch either way (it only ever produced
// 0), but metrics.PercentChange's zero-base rule of an unconditional +100
// would WRONGLY report a slowdown as +100% growth; CB3-c's sign-of-change
// rule must report -100 instead.
func TestCalculateSpendingVelocity_ZeroHistoricalBaseRefundIsNegative100(t *testing.T) {
	now := time.Now()
	allData := &models.TransactionSet{Transactions: []models.Transaction{
		{Description: "purchase", Amount: -300, Date: now.AddDate(0, 0, -60), TransactionType: models.Outflow},
		{Description: "refund", Amount: 300, Date: now.AddDate(0, 0, -59), TransactionType: models.Outflow},
	}}
	currentPeriod := &models.TransactionSet{Transactions: []models.Transaction{
		{Description: "refund", Amount: 100, Date: now, TransactionType: models.Outflow},
	}}

	velocity := SpendingVelocity(currentPeriod, allData)

	if velocity.HistoricalDaily != 0 {
		t.Fatalf("harness error: HistoricalDaily = %.4f, want exactly 0", velocity.HistoricalDaily)
	}
	if velocity.DailyAverage >= 0 {
		t.Fatalf("harness error: DailyAverage = %.4f, want negative (refund-dominant current period)", velocity.DailyAverage)
	}
	if velocity.BurnRateChange != -100 {
		t.Errorf("BurnRateChange = %.4f, want exactly -100 (zero base, negative change); metrics.PercentChange's unconditional +100 would fail this", velocity.BurnRateChange)
	}
}

// TestAnalyzeMajorExpenseTrends_DirectionBandEdges pins the +-5 direction
// band exactly at its edges (V3 promotion): nothing today asserts that
// changePercent==+-5 lands on "stable" rather than "up"/"down", so a
// mutant collapsing the band to +-0 survives unnoticed. Fixtures are
// constructed so changePercent lands exactly on (or just past) each edge:
// prev=100, cur=105 -> changePercent=+5 (stable); cur=105.01 -> >5 (up);
// cur=95 -> changePercent=-5 (stable); cur=94.99 -> <-5 (down).
func TestAnalyzeMajorExpenseTrends_DirectionBandEdges(t *testing.T) {
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	defs := []models.MajorExpense{{ID: "widget", Name: "Widget Co", Keywords: []string{"widget co"}}}

	cases := []struct {
		name          string
		prevAmt       float64
		curAmt        float64
		wantDirection string
	}{
		{"exactly +5 is stable, not up", 100, 105, "stable"},
		{"just past +5 is up", 100, 105.01, "up"},
		{"exactly -5 is stable, not down", 100, 95, "stable"},
		{"just past -5 is down", 100, 94.99, "down"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := &models.TransactionSet{Transactions: []models.Transaction{
				catTxn("Widget Co Purchase", "misc", tc.prevAmt, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)),
				catTxn("Widget Co Purchase", "misc", tc.curAmt, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)),
			}}

			trends := MajorExpenseTrends(ts, defs, nil, currentStart, currentEnd)
			got := make(map[string]models.CategoryTrend, len(trends))
			for _, tr := range trends {
				got[tr.Category] = tr
			}
			widget, ok := got["Widget Co"]
			if !ok {
				t.Fatalf("expected trend for 'Widget Co', got categories: %v", keysOf(got))
			}
			if widget.Direction != tc.wantDirection {
				t.Errorf("Direction = %q (changePercent=%.10f), want %q", widget.Direction, widget.ChangePercent, tc.wantDirection)
			}
		})
	}
}

func keysOf(m map[string]models.CategoryTrend) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// IncomePatterns must report each pattern's most recent occurrence so
// consumers (the what-if sync) can tell ended income from ongoing income.
func TestAnalyzeIncomePatterns_LastDate(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	last := base.AddDate(0, 3, 0)
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			income("employer", 5000, base.AddDate(0, 2, 0)),
			income("employer", 5000, base),
			income("employer", 5000, last),
			income("employer", 5000, base.AddDate(0, 1, 0)),
		},
	}

	patterns := IncomePatterns(ts)

	found := false
	for _, p := range patterns {
		if p.Description == "employer" {
			found = true
			if !p.LastDate.Equal(last) {
				t.Errorf("LastDate = %v, want %v", p.LastDate, last)
			}
		}
	}
	if !found {
		t.Fatal("expected employer pattern")
	}
}
