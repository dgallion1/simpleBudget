package insights

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/internal/testutil"
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

// --- analyzeCategoryTrends tests ---

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

	trends := analyzeCategoryTrends(ts, currentStart, currentEnd)

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

	trends := analyzeCategoryTrends(ts, currentStart, currentEnd)

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
	trends := analyzeCategoryTrends(ts, currentStart, currentEnd)

	if len(trends) > 10 {
		t.Errorf("expected at most 10 category trends, got %d", len(trends))
	}
}

// --- calculateInsights tests ---

func TestCalculateInsights_SplitsSubscriptionsAndBills(t *testing.T) {
	// Netflix should be classified as subscription; electric company as bill
	now := time.Now()
	var txns []models.Transaction
	for i := 0; i < 4; i++ {
		txns = append(txns,
			models.Transaction{
				Description:     "netflix",
				Amount:          -15.99,
				Date:            now.AddDate(0, 0, -30*i),
				TransactionType: models.Outflow,
			},
			models.Transaction{
				Description:     "electric company",
				Amount:          -120,
				Date:            now.AddDate(0, 0, -30*i),
				TransactionType: models.Outflow,
			},
		)
	}

	allData := &models.TransactionSet{Transactions: txns}
	startDate := now.AddDate(0, -6, 0)
	endDate := now

	insights := calculateInsights(allData, allData, startDate, endDate)

	subFound := false
	for _, s := range insights.Subscriptions {
		if s.Description == "netflix" {
			subFound = true
		}
	}
	if !subFound {
		t.Error("expected 'netflix' in Subscriptions list")
		for _, s := range insights.Subscriptions {
			t.Logf("  subscription: %q", s.Description)
		}
	}

	billFound := false
	for _, b := range insights.RecurringPayments {
		if b.Description == "electric company" {
			billFound = true
		}
	}
	if !billFound {
		t.Error("expected 'electric company' in RecurringPayments (bills) list")
		for _, b := range insights.RecurringPayments {
			t.Logf("  bill: %q", b.Description)
		}
	}
}

func TestCalculateInsights_TotalCalculations(t *testing.T) {
	now := time.Now()
	var txns []models.Transaction
	// 4 monthly netflix payments (subscription)
	for i := 0; i < 4; i++ {
		txns = append(txns, models.Transaction{
			Description:     "netflix",
			Amount:          -15.99,
			Date:            now.AddDate(0, 0, -30*i),
			TransactionType: models.Outflow,
		})
	}
	// 4 monthly insurance payments (bill, not subscription)
	for i := 0; i < 4; i++ {
		txns = append(txns, models.Transaction{
			Description:     "insurance",
			Amount:          -200,
			Date:            now.AddDate(0, 0, -30*i),
			TransactionType: models.Outflow,
		})
	}

	allData := &models.TransactionSet{Transactions: txns}
	startDate := now.AddDate(0, -6, 0)
	endDate := now

	insights := calculateInsights(allData, allData, startDate, endDate)

	// TotalRecurring should be sum of all recurring AnnualCost values
	if insights.TotalRecurring == 0 {
		t.Error("TotalRecurring should be > 0")
	}

	// MonthlyRecurring = TotalRecurring / 12
	expectedMonthly := insights.TotalRecurring / 12
	if math.Abs(insights.MonthlyRecurring-expectedMonthly) > 0.01 {
		t.Errorf("MonthlyRecurring = %.2f, want %.2f", insights.MonthlyRecurring, expectedMonthly)
	}

	// MonthlySubscriptions should only include subscription annual costs / 12
	var subAnnual float64
	for _, s := range insights.Subscriptions {
		subAnnual += s.AnnualCost
	}
	expectedMonthlySub := subAnnual / 12
	if math.Abs(insights.MonthlySubscriptions-expectedMonthlySub) > 0.01 {
		t.Errorf("MonthlySubscriptions = %.2f, want %.2f", insights.MonthlySubscriptions, expectedMonthlySub)
	}
}

func TestCalculateInsights_UsesSelectedEndDateForRecurringFreshness(t *testing.T) {
	allData := &models.TransactionSet{
		Transactions: []models.Transaction{
			{
				Description:     "legacy gym",
				Amount:          -49,
				Date:            time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
				TransactionType: models.Outflow,
			},
			{
				Description:     "legacy gym",
				Amount:          -49,
				Date:            time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC),
				TransactionType: models.Outflow,
			},
			{
				Description:     "legacy gym",
				Amount:          -49,
				Date:            time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC),
				TransactionType: models.Outflow,
			},
			{
				Description:     "one-off purchase",
				Amount:          -200,
				Date:            time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC),
				TransactionType: models.Outflow,
			},
		},
	}
	startDate := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC)
	filtered := allData.FilterByDateRange(startDate, endDate)

	insights := calculateInsights(allData, filtered, startDate, endDate)

	for _, r := range insights.Subscriptions {
		if r.Description == "legacy gym" {
			return
		}
	}

	t.Fatal("expected recurring freshness to respect the selected end date even when newer unrelated data exists")
}

// --- calculateSpendingVelocity tests ---

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

	velocity := calculateSpendingVelocity(currentPeriod, allData)

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
	velocity := calculateSpendingVelocity(empty, empty)

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

	velocity := calculateSpendingVelocity(period, period)

	// All same day -> currentDays clamped to 1, so DailyAverage = net spend.
	const want = 550.0
	if math.Abs(velocity.DailyAverage-want) > 0.01 {
		t.Errorf("DailyAverage = %.2f, want %.2f (refund must subtract, not add)", velocity.DailyAverage, want)
	}
	if math.Abs(velocity.HistoricalDaily-want) > 0.01 {
		t.Errorf("HistoricalDaily = %.2f, want %.2f", velocity.HistoricalDaily, want)
	}
}

// --- AnalyzeIncomePatterns tests ---

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

	patterns := AnalyzeIncomePatterns(ts)

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

	patterns := AnalyzeIncomePatterns(ts)

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
	patterns := AnalyzeIncomePatterns(ts)

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

	trends := analyzeCategoryTrends(ts, currentStart, currentEnd)

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

	trends := analyzeCategoryTrends(ts, currentStart, currentEnd)

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
	patterns := AnalyzeIncomePatterns(ts)
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
	patterns := AnalyzeIncomePatterns(ts)
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
	patterns := AnalyzeIncomePatterns(ts)
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
	patterns := AnalyzeIncomePatterns(ts)
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

// TestCalculateInsights_RegularIncomeTotal covers the regular income total aggregation
func TestCalculateInsights_RegularIncomeTotal(t *testing.T) {
	now := time.Now()
	base := now.AddDate(0, -4, 0)
	var txns []models.Transaction
	// Monthly income
	for i := 0; i < 4; i++ {
		txns = append(txns, models.Transaction{
			Description:     "employer pay",
			Amount:          5000,
			Date:            base.AddDate(0, i, 0),
			TransactionType: models.Income,
		})
	}
	// Some outflow so the velocity doesn't panic
	txns = append(txns, models.Transaction{
		Description:     "grocery",
		Amount:          -100,
		Date:            now,
		TransactionType: models.Outflow,
	})

	allData := &models.TransactionSet{Transactions: txns}
	startDate := base
	endDate := now

	insights := calculateInsights(allData, allData, startDate, endDate)

	if insights.RegularIncomeTotal == 0 {
		t.Error("expected RegularIncomeTotal > 0")
	}
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

	velocity := calculateSpendingVelocity(currentPeriod, allData)

	if velocity.BurnRateChange == 0 {
		t.Error("BurnRateChange should be non-zero when current > historical")
	}
}

// --- HTTP Handler Tests ---

// testCSV generates CSV content with monthly outflows and income for handler testing.
func testCSV() string {
	return `Date,Description,Amount,Category
2025-01-15,ACME PAYROLL,3500.00,Paycheck
2025-02-15,ACME PAYROLL,3500.00,Paycheck
2025-03-15,ACME PAYROLL,3500.00,Paycheck
2025-04-15,ACME PAYROLL,3500.00,Paycheck
2025-01-10,Netflix,-15.99,Entertainment
2025-02-10,Netflix,-15.99,Entertainment
2025-03-10,Netflix,-15.99,Entertainment
2025-04-10,Netflix,-15.99,Entertainment
2025-01-05,Electric Company,-120.00,Utilities
2025-02-05,Electric Company,-120.00,Utilities
2025-03-05,Electric Company,-120.00,Utilities
2025-04-05,Electric Company,-120.00,Utilities
2025-01-20,Grocery Store,-87.00,Groceries
2025-02-20,Grocery Store,-95.00,Groceries
2025-03-20,Grocery Store,-82.00,Groceries
2025-04-20,Grocery Store,-90.00,Groceries
`
}

func setupTestLoader(t *testing.T, csvContent string) func() {
	t.Helper()
	tmpDir := t.TempDir()
	csvPath := tmpDir + "/test.csv"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write test CSV: %v", err)
	}

	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	oldLoader := loader
	oldRenderer := renderer

	loader = dataloader.New(tmpDir, store)
	renderer = nil // Use fallback JSON/HTML responses

	return func() {
		loader = oldLoader
		renderer = oldRenderer
	}
}

func setupErrorLoader(t *testing.T) func() {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	oldLoader := loader
	oldRenderer := renderer

	// Use a directory path containing '[' to trigger filepath.ErrBadPattern in Glob
	loader = dataloader.New(tmpDir+"/[invalid", store)
	renderer = nil

	return func() {
		loader = oldLoader
		renderer = oldRenderer
	}
}

func TestInitialize(t *testing.T) {
	oldLoader := loader
	oldRenderer := renderer
	defer func() {
		loader = oldLoader
		renderer = oldRenderer
	}()

	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	dl := dataloader.New(tmpDir, store)
	Initialize(dl, nil)

	if loader != dl {
		t.Error("Initialize did not set loader")
	}
	if renderer != nil {
		t.Error("Initialize should have set renderer to nil")
	}
}

func TestRegisterRoutes(t *testing.T) {
	r := chi.NewRouter()
	RegisterRoutes(r)

	// Verify routes were registered by walking them
	routes := []string{
		"/insights",
		"/insights/recurring",
		"/insights/trends",
		"/insights/trends/chart",
		"/insights/velocity",
		"/insights/income",
	}

	for _, route := range routes {
		found := false
		_ = chi.Walk(r, func(method, path string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
			if path == route {
				found = true
			}
			return nil
		})
		if !found {
			t.Errorf("expected route %q to be registered", route)
		}
	}
}

func TestHandleInsights_Success(t *testing.T) {
	cleanup := setupTestLoader(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights", nil)
	w := httptest.NewRecorder()

	handleInsights(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	// With nil renderer, should return fallback HTML
	if ct := resp.Header.Get("Content-Type"); ct != "text/html" {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

func TestHandleInsights_WithDateParams(t *testing.T) {
	cleanup := setupTestLoader(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights?start=2025-01-01&end=2025-04-30&preset=custom", nil)
	w := httptest.NewRecorder()

	handleInsights(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHandleInsights_LoadError(t *testing.T) {
	cleanup := setupErrorLoader(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights", nil)
	w := httptest.NewRecorder()

	handleInsights(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestHandleRecurringPartial_Success(t *testing.T) {
	cleanup := setupTestLoader(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/recurring", nil)
	w := httptest.NewRecorder()

	handleRecurringPartial(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	// With nil renderer, should fall back to JSON
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if _, ok := result["TotalRecurring"]; !ok {
		t.Error("expected TotalRecurring in response")
	}
	if _, ok := result["MonthlyRecurring"]; !ok {
		t.Error("expected MonthlyRecurring in response")
	}
}

func TestHandleTrendsPartial_Success(t *testing.T) {
	cleanup := setupTestLoader(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/trends?start=2025-03-01&end=2025-04-30", nil)
	w := httptest.NewRecorder()

	handleTrendsPartial(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestHandleTrendsPartial_DefaultDates(t *testing.T) {
	cleanup := setupTestLoader(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/trends", nil)
	w := httptest.NewRecorder()

	handleTrendsPartial(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHandleTrendsChartData_Success(t *testing.T) {
	cleanup := setupTestLoader(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/trends/chart?start=2025-03-01&end=2025-04-30", nil)
	w := httptest.NewRecorder()

	handleTrendsChartData(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if _, ok := result["data"]; !ok {
		t.Error("expected 'data' in chart response")
	}
	if _, ok := result["layout"]; !ok {
		t.Error("expected 'layout' in chart response")
	}
}

func TestHandleTrendsChartData_DefaultDates(t *testing.T) {
	cleanup := setupTestLoader(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/trends/chart", nil)
	w := httptest.NewRecorder()

	handleTrendsChartData(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHandleVelocityPartial_Success(t *testing.T) {
	cleanup := setupTestLoader(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/velocity?start=2025-01-01&end=2025-04-30", nil)
	w := httptest.NewRecorder()

	handleVelocityPartial(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestHandleVelocityPartial_DefaultDates(t *testing.T) {
	cleanup := setupTestLoader(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/velocity", nil)
	w := httptest.NewRecorder()

	handleVelocityPartial(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHandleIncomePartial_Success(t *testing.T) {
	cleanup := setupTestLoader(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/income", nil)
	w := httptest.NewRecorder()

	handleIncomePartial(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if _, ok := result["IncomePatterns"]; !ok {
		t.Error("expected IncomePatterns in response")
	}
	if _, ok := result["RegularIncomeTotal"]; !ok {
		t.Error("expected RegularIncomeTotal in response")
	}
}

// Error path tests for all handlers
func TestHandleRecurringPartial_LoadError(t *testing.T) {
	cleanup := setupErrorLoader(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/recurring", nil)
	w := httptest.NewRecorder()
	handleRecurringPartial(w, req)
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Result().StatusCode)
	}
}

func TestHandleTrendsPartial_LoadError(t *testing.T) {
	cleanup := setupErrorLoader(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/trends", nil)
	w := httptest.NewRecorder()
	handleTrendsPartial(w, req)
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Result().StatusCode)
	}
}

func TestHandleTrendsChartData_LoadError(t *testing.T) {
	cleanup := setupErrorLoader(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/trends/chart", nil)
	w := httptest.NewRecorder()
	handleTrendsChartData(w, req)
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Result().StatusCode)
	}
}

func TestHandleVelocityPartial_LoadError(t *testing.T) {
	cleanup := setupErrorLoader(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/velocity", nil)
	w := httptest.NewRecorder()
	handleVelocityPartial(w, req)
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Result().StatusCode)
	}
}

func TestHandleIncomePartial_LoadError(t *testing.T) {
	cleanup := setupErrorLoader(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/income", nil)
	w := httptest.NewRecorder()
	handleIncomePartial(w, req)
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Result().StatusCode)
	}
}

// --- Tests with real renderer (covers renderer != nil branches) ---

func setupTestLoaderWithRenderer(t *testing.T, csvContent string) func() {
	t.Helper()
	tmpDir := t.TempDir()
	csvPath := tmpDir + "/test.csv"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write test CSV: %v", err)
	}

	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	templateDir := testutil.ProjectRoot() + "/web/templates"
	r, err := templates.New(templateDir, true)
	if err != nil {
		t.Fatalf("failed to create renderer: %v", err)
	}

	oldLoader := loader
	oldRenderer := renderer

	loader = dataloader.New(tmpDir, store)
	renderer = r

	return func() {
		loader = oldLoader
		renderer = oldRenderer
	}
}

func TestHandleInsights_WithRenderer(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights?start=2025-01-01&end=2025-04-30", nil)
	w := httptest.NewRecorder()

	handleInsights(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHandleRecurringPartial_WithRenderer(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/recurring", nil)
	w := httptest.NewRecorder()

	handleRecurringPartial(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHandleTrendsPartial_WithRenderer(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/trends", nil)
	w := httptest.NewRecorder()

	handleTrendsPartial(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHandleVelocityPartial_WithRenderer(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/velocity", nil)
	w := httptest.NewRecorder()

	handleVelocityPartial(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHandleIncomePartial_WithRenderer(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights/income", nil)
	w := httptest.NewRecorder()

	handleIncomePartial(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
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
	velocity := calculateSpendingVelocity(ts, ts)
	// With single day, currentDays = 1 (0 + 1 = 1 after rounding, but
	// maxDate - minDate = 0 days, so 0 + 1 = 1; the < 1 branch may not trigger)
	// The important thing is it doesn't panic
	if velocity.DailyAverage == 0 {
		t.Error("DailyAverage should be > 0 for single transaction")
	}
}

// TestDetectByAmount_LowConfidenceFiltered covers confidence < 0.4 in amount-based detection.
// For monthly (medianInterval ~30), stdDev must be > 18 to get confidence < 0.4.
// But stdDev must be <= 10. So confidence = 1-(10/30) = 0.67 min. Can't reach < 0.4 for monthly.
// For quarterly (median ~90), stdDev=10: confidence = 1-(10/90)*0.9 = 0.81. Can't reach < 0.4.
// This branch is essentially unreachable for the allowed frequency buckets.

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

	trends := analyzeCategoryTrends(ts, currentStart, currentEnd)
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

// --- analyzeMajorExpenseTrends tests ---

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

	trends := analyzeMajorExpenseTrends(ts, defs, nil, currentStart, currentEnd)

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

	trends := analyzeMajorExpenseTrends(ts, defs, pins, currentStart, currentEnd)

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
	if got := analyzeMajorExpenseTrends(ts, nil, nil, time.Now().AddDate(0, -1, 0), time.Now()); got != nil {
		t.Errorf("expected nil trends with no defs, got %v", got)
	}
}

func TestLoadAndAnalyzeTrends_FallsBackWhenNoDefs(t *testing.T) {
	cleanup := setupTestLoader(t, testCSV())
	defer cleanup()

	data, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData failed: %v", err)
	}

	// No major expenses on disk → falls back to category-based trends.
	trends := loadAndAnalyzeTrends(data, data.MaxDate().AddDate(0, -1, 0), data.MaxDate())
	if len(trends) == 0 {
		t.Fatalf("expected category-trend fallback to return entries, got 0")
	}
}

func TestLoadAndAnalyzeTrends_UsesMajorExpensesWhenConfigured(t *testing.T) {
	cleanup := setupTestLoader(t, testCSV())
	defer cleanup()

	defs := []models.MajorExpense{
		{ID: "elec", Name: "Power Bill", Keywords: []string{"electric company"}},
	}
	if err := loader.SaveMajorExpenses(defs); err != nil {
		t.Fatalf("SaveMajorExpenses failed: %v", err)
	}

	data, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData failed: %v", err)
	}

	trends := loadAndAnalyzeTrends(data, data.MaxDate().AddDate(0, -1, 0), data.MaxDate())
	for _, tr := range trends {
		if tr.Category == "Utilities" {
			t.Errorf("expected major-expense names, but raw category 'Utilities' surfaced: %+v", tr)
		}
	}
	found := false
	for _, tr := range trends {
		if tr.Category == "Power Bill" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'Power Bill' (major-expense name) in trends, got %+v", trends)
	}
}

func keysOf(m map[string]models.CategoryTrend) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- P4: Anomalies / Price-creep Insights sections ---

// anomalyTxn builds an expense Transaction (Outflow, negative Amount) with
// Hash and derived fields populated the way the real loader stamps them —
// required for anomalies.Detect's Hash-keyed dedupe to behave like it does
// against production data.
func anomalyTxn(desc, category string, absAmount float64, on time.Time) models.Transaction {
	t := models.Transaction{
		Description:     desc,
		Amount:          -absAmount,
		Category:        category,
		Date:            on,
		TransactionType: models.Outflow,
	}
	t.Hash = t.ComputeHash()
	t.ComputeDerivedFields()
	return t
}

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestAnomalyMethodLabel(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{"mad_category", "Category outlier"},
		{"mad_merchant", "Merchant outlier"},
		{"new_merchant", "New merchant"},
		{"something_unmapped", "something_unmapped"}, // defensive fallback
	}
	for _, tt := range tests {
		if got := anomalyMethodLabel(tt.method); got != tt.want {
			t.Errorf("anomalyMethodLabel(%q) = %q, want %q", tt.method, got, tt.want)
		}
	}
}

func TestAnomaliesForPeriod_NilTransactionSet(t *testing.T) {
	views, total := anomaliesForPeriod(nil, time.Time{}, time.Time{})
	if views == nil {
		t.Error("expected non-nil (empty) slice for nil input")
	}
	if len(views) != 0 || total != 0 {
		t.Errorf("expected empty result for nil input, got views=%v total=%d", views, total)
	}
}

func TestPriceCreepForDisplay_NilTransactionSet(t *testing.T) {
	rows, total := priceCreepForDisplay(nil)
	if rows != nil || total != 0 {
		t.Errorf("expected nil/0 for nil input, got rows=%v total=%d", rows, total)
	}
}

// TestAnomaliesForPeriod_WindowFiltersDisplayOnly plants one mad_category
// anomaly inside the selected display window and one outside it (same
// shape, different dates) and asserts the window only controls what is
// shown, not what anomalies.Detect finds.
func TestAnomaliesForPeriod_WindowFiltersDisplayOnly(t *testing.T) {
	windowStart := date(2025, 3, 1)
	windowEnd := date(2025, 3, 31)

	var txns []models.Transaction
	// Baseline: 6 uniform $20 rows, unique letter-word descriptions
	// (singleton merchant groups) so the category pool sees all of them.
	// Digit suffixes deliberately avoided: merchants.Normalize drops
	// standalone all-digit tokens, which would collapse "Widget 0".."Widget
	// 5" into a single merged "WIDGET" merchant group instead of 6 distinct
	// singletons.
	baselineNames := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"}
	for i, name := range baselineNames {
		txns = append(txns, anomalyTxn("Widget "+name, "Widgets", 20, date(2025, 1, 1+i)))
	}
	inWindow := anomalyTxn("Widget Splurge In Window", "Widgets", 200, date(2025, 3, 15))
	outsideWindow := anomalyTxn("Widget Splurge Outside Window", "Widgets", 200, date(2025, 6, 15))
	txns = append(txns, inWindow, outsideWindow)

	full := &models.TransactionSet{Transactions: txns}

	views, total := anomaliesForPeriod(full, windowStart, windowEnd)

	if total < 2 {
		t.Fatalf("expected both planted anomalies detected across full history, got total=%d", total)
	}

	var sawIn, sawOutside bool
	for _, v := range views {
		if v.Hash == inWindow.Hash {
			sawIn = true
			if v.Method != "mad_category" || v.MethodLabel != "Category outlier" {
				t.Errorf("in-window anomaly: method=%q label=%q, want mad_category/Category outlier", v.Method, v.MethodLabel)
			}
		}
		if v.Hash == outsideWindow.Hash {
			sawOutside = true
		}
	}
	if !sawIn {
		t.Errorf("expected the in-window planted anomaly to be displayed, got views=%+v", views)
	}
	if sawOutside {
		t.Errorf("expected the outside-window planted anomaly to be excluded from display, got views=%+v", views)
	}
}

// TestAnomaliesForPeriod_FullHistoryFirstOccurrence is the P2-finding
// regression carried into P4 (ANALYTICS_PORT_SPEC.md Rulings): a
// merchant's first-ever occurrence must be computed against the FULL
// transaction history, not the display window, so a large charge from a
// long-standing merchant is never mislabeled "new merchant" just because
// the selected window happens to start after that merchant's true first
// appearance.
func TestAnomaliesForPeriod_FullHistoryFirstOccurrence(t *testing.T) {
	windowStart := date(2025, 4, 1)
	windowEnd := date(2025, 4, 30)

	// OldReliable's true first-ever charge predates the window by five
	// years; a much larger charge from the same merchant lands inside the
	// window. Category "Misc" is kept under minCategoryRows (6) and the
	// merchant group under minMerchantGroupRows (4) so neither mad_category
	// nor mad_merchant can independently flag it — isolating new_merchant.
	oldFirst := anomalyTxn("OldReliable", "Misc", 30, date(2020, 1, 1))
	oldRecentBig := anomalyTxn("OldReliable", "Misc", 900, date(2025, 4, 15))

	// BrandNewCo has no history at all before the window — a genuine
	// first-ever large purchase, which SHOULD flag as "new merchant".
	brandNew := anomalyTxn("BrandNewCo", "Startup", 200, date(2025, 4, 20))

	// Baseline noise (outside the window) so the p95 threshold used by
	// new_merchant isn't trivially dominated by the two planted rows.
	var txns []models.Transaction
	for i := 0; i < 20; i++ {
		txns = append(txns, anomalyTxn(fmt.Sprintf("Noise Item %d", i), "Baseline Noise", 10, date(2019, 6, 1)))
	}
	txns = append(txns, oldFirst, oldRecentBig, brandNew)
	full := &models.TransactionSet{Transactions: txns}

	views, _ := anomaliesForPeriod(full, windowStart, windowEnd)

	for _, v := range views {
		if v.Hash == oldRecentBig.Hash && v.Method == "new_merchant" {
			t.Errorf("OldReliable's in-window charge must never be flagged new_merchant merely because its true first occurrence (2020) predates the window: %+v", v)
		}
	}

	var foundBrandNew bool
	for _, v := range views {
		if v.Hash == brandNew.Hash {
			foundBrandNew = true
			if v.Method != "new_merchant" || v.MethodLabel != "New merchant" {
				t.Errorf("expected BrandNewCo flagged new_merchant, got method=%q label=%q", v.Method, v.MethodLabel)
			}
		}
	}
	if !foundBrandNew {
		t.Fatalf("expected BrandNewCo (genuinely new, in-window) to be flagged as a new-merchant anomaly, got views=%+v", views)
	}

	// Regression guard: prove this fixture would actually reproduce the
	// bug if detection were (incorrectly) scoped to the window-filtered
	// set instead of the full history, so the assertions above aren't
	// vacuously true.
	windowOnly := full.FilterByDateRange(windowStart, windowEnd)
	wrongViews, _ := anomaliesForPeriod(windowOnly, windowStart, windowEnd)
	var wronglyFlagged bool
	for _, v := range wrongViews {
		if v.Hash == oldRecentBig.Hash && v.Method == "new_merchant" {
			wronglyFlagged = true
		}
	}
	if !wronglyFlagged {
		t.Fatalf("fixture did not reproduce the window-scoping bug when detection is (wrongly) run on window-only data; strengthen the fixture")
	}
}

// TestPriceCreepForDisplay_CapsAtTenWithTotal plants 11 qualifying
// price-creep merchant groups and asserts the display list is capped at
// maxPriceCreepRows while the total reflects all of them.
func TestPriceCreepForDisplay_CapsAtTenWithTotal(t *testing.T) {
	var txns []models.Transaction
	for g := 0; g < 11; g++ {
		merchant := fmt.Sprintf("Streamer%d", g)
		amounts := []float64{10, 10, 10, 12, 13, 13}
		for i, amt := range amounts {
			txns = append(txns, anomalyTxn(merchant, "Subscriptions", amt, date(2023, time.Month(i+1), 10)))
		}
	}
	full := &models.TransactionSet{Transactions: txns}

	rows, total := priceCreepForDisplay(full)
	if total != 11 {
		t.Errorf("total = %d, want 11", total)
	}
	if len(rows) != maxPriceCreepRows {
		t.Errorf("len(rows) = %d, want %d", len(rows), maxPriceCreepRows)
	}
}

// plantedAnalyticsCSV plants:
//   - a mad_category anomaly ("Widget Splurge") inside the query window
//   - a same-shape anomaly ("Gadget Splurge Old") dated outside the query
//     window used by the render test below, to prove display filtering
//   - a 6-occurrence price-creep series ("StreamPlus", +30% first-3 vs
//     last-3 median) — price creep is never window-filtered.
func plantedAnalyticsCSV() string {
	return `Date,Description,Amount,Category
2025-01-05,Widget A,-20.00,Widgets
2025-01-12,Widget B,-20.00,Widgets
2025-02-05,Widget C,-20.00,Widgets
2025-02-12,Widget D,-20.00,Widgets
2025-03-05,Widget E,-20.00,Widgets
2025-03-12,Widget F,-20.00,Widgets
2025-03-15,Widget Splurge,-200.00,Widgets
2022-01-05,Gadget A,-15.00,Gadgets
2022-02-05,Gadget B,-15.00,Gadgets
2022-03-05,Gadget C,-15.00,Gadgets
2022-04-05,Gadget D,-15.00,Gadgets
2022-05-05,Gadget E,-15.00,Gadgets
2022-06-05,Gadget F,-15.00,Gadgets
2022-01-15,Gadget Splurge Old,-150.00,Gadgets
2023-01-10,StreamPlus,-10.00,Subscriptions
2023-02-10,StreamPlus,-10.00,Subscriptions
2023-03-10,StreamPlus,-10.00,Subscriptions
2023-04-10,StreamPlus,-12.00,Subscriptions
2023-05-10,StreamPlus,-13.00,Subscriptions
2023-06-10,StreamPlus,-13.00,Subscriptions
`
}

// TestHandleInsights_AnomaliesAndPriceCreepSections_PlantedData renders the
// full Insights page (real renderer) over plantedAnalyticsCSV with the
// query window restricted to 2025-01-01..2025-06-30 and asserts: both new
// sections render with plain-language method labels, the in-window
// anomaly is shown, the out-of-window same-shape anomaly is not shown, and
// the price-creep row (which spans 2023, entirely outside the query
// window) is still shown because price creep is never window-filtered.
func TestHandleInsights_AnomaliesAndPriceCreepSections_PlantedData(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, plantedAnalyticsCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights?start=2025-01-01&end=2025-06-30", nil)
	w := httptest.NewRecorder()

	handleInsights(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := w.Body.String()

	for _, want := range []string{
		"Anomalies",
		"Price Creep",
		"Widget Splurge",
		"Category outlier",
		"streamplus", // merchants.DisplayLabel lowercases its output by design
		"30.0%",      // formatPercent(30.0); html/template escapes the leading "+" to "&#43;"
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected body to contain %q, it did not", want)
		}
	}

	if strings.Contains(body, "Gadget Splurge Old") {
		t.Error("expected the out-of-window planted anomaly to be absent from the rendered page")
	}
}

// TestHandleInsights_AnomaliesAndPriceCreepSections_EmptyStates reuses the
// existing testCSV() fixture, which is deliberately too thin (max group
// size 4, max category size 4) to trigger any of the three anomaly
// methods or the 6-occurrence price-creep threshold, and asserts both
// empty-state strings render verbatim.
func TestHandleInsights_AnomaliesAndPriceCreepSections_EmptyStates(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, testCSV())
	defer cleanup()

	req := httptest.NewRequest("GET", "/insights?start=2025-01-01&end=2025-04-30", nil)
	w := httptest.NewRecorder()

	handleInsights(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := w.Body.String()

	for _, want := range []string{
		"Anomalies",
		"Price Creep",
		"No anomalies in this period.",
		"No price increases detected.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected body to contain %q, it did not", want)
		}
	}
}
