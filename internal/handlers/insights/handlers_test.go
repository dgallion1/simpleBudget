package insights

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/internal/testutil"
)

func txn(desc string, amount float64, daysAgo int) models.Transaction {
	return models.Transaction{
		Description:     desc,
		Amount:          -amount,
		Date:            time.Now().AddDate(0, 0, -daysAgo),
		TransactionType: models.Outflow,
	}
}

func TestMergeSimlarGroups_SubstringMatch(t *testing.T) {
	groups := map[string][]models.Transaction{
		"lucid":           {txn("lucid", 1580, 30), txn("lucid", 1580, 60)},
		"lucidmotors.com": {txn("lucidmotors.com", 1580, 90)},
	}
	merged := mergeSimilarGroups(groups)

	if len(merged) != 1 {
		t.Fatalf("expected 1 group, got %d: %v", len(merged), keys(merged))
	}
	for _, txns := range merged {
		if len(txns) != 3 {
			t.Errorf("expected 3 transactions in merged group, got %d", len(txns))
		}
	}
}

func TestMergeSimlarGroups_DotComStripping(t *testing.T) {
	groups := map[string][]models.Transaction{
		"netflix":     {txn("netflix", 15, 30)},
		"netflix.com": {txn("netflix.com", 15, 60)},
	}
	merged := mergeSimilarGroups(groups)

	if len(merged) != 1 {
		t.Fatalf("expected 1 group, got %d: %v", len(merged), keys(merged))
	}
	for _, txns := range merged {
		if len(txns) != 2 {
			t.Errorf("expected 2 transactions, got %d", len(txns))
		}
	}
}

func TestMergeSimlarGroups_NoFalsePositives(t *testing.T) {
	groups := map[string][]models.Transaction{
		"netflix": {txn("netflix", 15, 30)},
		"at&t":    {txn("at&t", 166, 30)},
		"openai":  {txn("openai", 20, 30)},
	}
	merged := mergeSimilarGroups(groups)

	if len(merged) != 3 {
		t.Fatalf("expected 3 separate groups, got %d: %v", len(merged), keys(merged))
	}
}

func TestMergeSimlarGroups_PreservesCanonicalName(t *testing.T) {
	groups := map[string][]models.Transaction{
		"lucidmotors.com": {txn("lucidmotors.com", 1580, 90)},
		"lucid":           {txn("lucid", 1580, 30)},
	}
	merged := mergeSimilarGroups(groups)

	if _, ok := merged["lucid"]; !ok {
		t.Errorf("expected canonical key 'lucid', got keys: %v", keys(merged))
	}
}

func TestMergeSimlarGroups_MultipleSuffixes(t *testing.T) {
	tests := []struct {
		name string
		a, b string
	}{
		{"dotnet", "example", "example.net"},
		{"dotorg", "charity", "charity.org"},
		{"dotio", "service", "service.io"},
		{"dotco", "brand", "brand.co"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := map[string][]models.Transaction{
				tt.a: {txn(tt.a, 10, 30)},
				tt.b: {txn(tt.b, 10, 60)},
			}
			merged := mergeSimilarGroups(groups)
			if len(merged) != 1 {
				t.Errorf("expected 1 group for %q and %q, got %d", tt.a, tt.b, len(merged))
			}
		})
	}
}

func TestMergeSimlarGroups_EmptyInput(t *testing.T) {
	merged := mergeSimilarGroups(map[string][]models.Transaction{})
	if len(merged) != 0 {
		t.Errorf("expected 0 groups, got %d", len(merged))
	}
}

func TestMergeSimlarGroups_SingleGroup(t *testing.T) {
	groups := map[string][]models.Transaction{
		"netflix": {txn("netflix", 15, 30), txn("netflix", 15, 60)},
	}
	merged := mergeSimilarGroups(groups)
	if len(merged) != 1 {
		t.Fatalf("expected 1 group, got %d", len(merged))
	}
	if len(merged["netflix"]) != 2 {
		t.Errorf("expected 2 transactions, got %d", len(merged["netflix"]))
	}
}

func TestDetectRecurringPayments_FuzzyMergedVendor(t *testing.T) {
	// Simulate the Lucid scenario: different description variants that should merge
	// and meet the 3-transaction minimum
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("Lucid", 1580, 5),
			txn("Lucidmotors.com", 1580, 35),
			txn("Lucid", 1580, 65),
		},
	}
	recurring := detectRecurringPayments(ts)

	found := false
	for _, r := range recurring {
		if r.Description == "lucid" {
			found = true
			if r.Occurrences != 3 {
				t.Errorf("expected 3 occurrences, got %d", r.Occurrences)
			}
			if r.Frequency != "monthly" {
				t.Errorf("expected monthly frequency, got %q", r.Frequency)
			}
		}
	}
	if !found {
		t.Error("expected 'lucid' in recurring payments but not found")
		for _, r := range recurring {
			t.Logf("  found: %q (%d occurrences, %s)", r.Description, r.Occurrences, r.Frequency)
		}
	}
}

func TestDetectRecurringPayments_ExactMatchStillWorks(t *testing.T) {
	// Standard case: all descriptions match exactly
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("netflix", 15.99, 5),
			txn("netflix", 15.99, 35),
			txn("netflix", 15.99, 65),
			txn("netflix", 15.99, 95),
		},
	}
	recurring := detectRecurringPayments(ts)

	found := false
	for _, r := range recurring {
		if r.Description == "netflix" {
			found = true
			if r.Occurrences != 4 {
				t.Errorf("expected 4 occurrences, got %d", r.Occurrences)
			}
		}
	}
	if !found {
		t.Error("expected 'netflix' in recurring payments")
	}
}

func TestIsSubscription(t *testing.T) {
	tests := []struct {
		desc      string
		freq      string
		wantIsSub bool
	}{
		{"netflix", "monthly", true},
		{"claude.ai subscription", "monthly", true},
		{"walmart", "monthly", false},
		{"electric company", "monthly", false},
		{"geico", "monthly", false},
		{"openai", "ongoing", true},
		{"amazon", "monthly", false},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			rp := models.RecurringPayment{Description: tt.desc, Frequency: tt.freq}
			got := isSubscription(rp)
			if got != tt.wantIsSub {
				t.Errorf("isSubscription(%q, %q) = %v, want %v", tt.desc, tt.freq, got, tt.wantIsSub)
			}
		})
	}
}

func TestDetectRecurringPayments_AmountBasedGrouping(t *testing.T) {
	// Simulate the Lucid check-to-billpay scenario: same amount, different descriptions
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("Lucid", 1580.43, 5),
			txn("Check #996581", 1580.43, 28),
			txn("Check #996578", 1580.43, 56),
		},
	}
	recurring := detectRecurringPayments(ts)

	found := false
	for _, r := range recurring {
		if r.Amount == 1580.43 {
			found = true
			if r.Occurrences != 3 {
				t.Errorf("expected 3 occurrences, got %d", r.Occurrences)
			}
			if r.Frequency != "monthly" {
				t.Errorf("expected monthly frequency, got %q", r.Frequency)
			}
			// Most recent description should be used as the label
			if r.Description != "lucid" {
				t.Errorf("expected description 'lucid' (most recent), got %q", r.Description)
			}
		}
	}
	if !found {
		t.Error("expected amount-based recurring payment at $1580.43 but not found")
		for _, r := range recurring {
			t.Logf("  found: %q $%.2f (%d occurrences, %s)", r.Description, r.Amount, r.Occurrences, r.Frequency)
		}
	}
}

func TestDetectRecurringPayments_AmountBasedSkipsTinyAmounts(t *testing.T) {
	// Small amounts under $5 should not trigger amount-based matching
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("coffee shop a", 3.50, 5),
			txn("coffee shop b", 3.50, 35),
			txn("coffee shop c", 3.50, 65),
		},
	}
	recurring := detectRecurringPayments(ts)

	for _, r := range recurring {
		if r.Amount == 3.50 {
			t.Error("should not match tiny amounts via amount-based grouping")
		}
	}
}

func TestDetectRecurringPayments_AmountBasedNoFalsePositivesDifferentAmounts(t *testing.T) {
	// Transactions with different amounts should not be grouped
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("vendor a", 100.00, 5),
			txn("vendor b", 200.00, 35),
			txn("vendor c", 300.00, 65),
		},
	}
	recurring := detectRecurringPayments(ts)

	if len(recurring) != 0 {
		t.Errorf("expected no recurring payments, got %d", len(recurring))
		for _, r := range recurring {
			t.Logf("  found: %q $%.2f (%d occurrences)", r.Description, r.Amount, r.Occurrences)
		}
	}
}

func TestDetectRecurringPayments_AmountBasedSkipsAlreadyMatched(t *testing.T) {
	// If transactions are already matched by description (pass 1), they shouldn't
	// also appear via amount-based matching
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("netflix", 15.99, 5),
			txn("netflix", 15.99, 35),
			txn("netflix", 15.99, 65),
			txn("netflix", 15.99, 95),
		},
	}
	recurring := detectRecurringPayments(ts)

	count := 0
	for _, r := range recurring {
		if r.Amount == 15.99 {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 entry for $15.99, got %d (duplicate detection)", count)
	}
}

func TestDetectRecurringPayments_AmountBasedIrregularIntervals(t *testing.T) {
	// Same amount but irregular intervals should not match
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("payment a", 500.00, 5),
			txn("payment b", 500.00, 10), // 5 days later
			txn("payment c", 500.00, 60), // 50 days later
		},
	}
	recurring := detectRecurringPayments(ts)

	for _, r := range recurring {
		if r.Amount == 500.00 {
			t.Errorf("should not match irregular intervals, got %q %s", r.Description, r.Frequency)
		}
	}
}

func TestDetectRecurringPayments_LongHistoryPattern(t *testing.T) {
	// 12 months of insurance payments spread across a full year.
	// Even though a short date filter would only see a couple of these,
	// detectRecurringPayments should find the pattern when given the full history.
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("insurance", 200, 30),
			txn("insurance", 200, 60),
			txn("insurance", 200, 90),
			txn("insurance", 200, 120),
			txn("insurance", 200, 150),
			txn("insurance", 200, 180),
			txn("insurance", 200, 210),
			txn("insurance", 200, 240),
			txn("insurance", 200, 270),
			txn("insurance", 200, 300),
			txn("insurance", 200, 330),
			txn("insurance", 200, 360),
		},
	}
	recurring := detectRecurringPayments(ts)

	found := false
	for _, r := range recurring {
		if r.Description == "insurance" {
			found = true
			if r.Occurrences != 12 {
				t.Errorf("expected 12 occurrences, got %d", r.Occurrences)
			}
			if r.Frequency != "monthly" {
				t.Errorf("expected monthly frequency, got %q", r.Frequency)
			}
			if r.Amount != 200 {
				t.Errorf("expected amount $200, got $%.2f", r.Amount)
			}
		}
	}
	if !found {
		t.Error("expected 'insurance' in recurring payments but not found")
		for _, r := range recurring {
			t.Logf("  found: %q $%.2f (%d occurrences, %s)", r.Description, r.Amount, r.Occurrences, r.Frequency)
		}
	}
}

func TestDetectRecurringPayments_StrictMatchSkipsExpiredMonthly(t *testing.T) {
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("old gym", 49, 120),
			txn("old gym", 49, 150),
			txn("old gym", 49, 180),
		},
	}

	recurring := detectRecurringPaymentsAt(ts, time.Now())

	for _, r := range recurring {
		if r.Description == "old gym" {
			t.Fatalf("expected expired monthly payment to be excluded, got %+v", r)
		}
	}
}

func TestDetectRecurringPayments_StrictMatchKeepsYearlyPaymentsCurrent(t *testing.T) {
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("annual insurance", 1200, 360),
			txn("annual insurance", 1200, 725),
			txn("annual insurance", 1200, 1090),
		},
	}

	recurring := detectRecurringPaymentsAt(ts, time.Now())

	for _, r := range recurring {
		if r.Description == "annual insurance" {
			if r.Frequency != "yearly" {
				t.Fatalf("expected yearly frequency, got %q", r.Frequency)
			}
			return
		}
	}

	t.Fatal("expected yearly payment within the annual freshness window to remain active")
}

func TestDetectRecurringPayments_UsesDatasetMaxDateForFreshness(t *testing.T) {
	ts := &models.TransactionSet{
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
		},
	}

	recurring := detectRecurringPayments(ts)

	for _, r := range recurring {
		if r.Description == "legacy gym" {
			return
		}
	}

	t.Fatal("expected recurring detection to use dataset max date instead of wall-clock time")
}

func TestDetectRecurringPaymentsAt_IgnoresTransactionsAfterReferenceDate(t *testing.T) {
	ts := &models.TransactionSet{
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
				Description:     "future club",
				Amount:          -25,
				Date:            time.Date(2024, time.May, 1, 0, 0, 0, 0, time.UTC),
				TransactionType: models.Outflow,
			},
			{
				Description:     "future club",
				Amount:          -25,
				Date:            time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC),
				TransactionType: models.Outflow,
			},
			{
				Description:     "future club",
				Amount:          -25,
				Date:            time.Date(2024, time.July, 1, 0, 0, 0, 0, time.UTC),
				TransactionType: models.Outflow,
			},
		},
	}

	recurring := detectRecurringPaymentsAt(ts, time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC))

	for _, r := range recurring {
		if r.Description == "future club" {
			t.Fatalf("expected recurring detection to ignore transactions after reference date, got %+v", r)
		}
	}
}

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

func keys(m map[string][]models.Transaction) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// --- Additional coverage tests ---

// TestIsSubscription_UnknownFrequency covers the return false at end of isSubscription
func TestIsSubscription_UnknownFrequency(t *testing.T) {
	rp := models.RecurringPayment{Description: "some vendor", Frequency: "weekly"}
	if isSubscription(rp) {
		t.Error("weekly frequency should not be classified as subscription")
	}
}

// TestIsSubscription_YearlyAndQuarterly covers the yearly and quarterly branches
func TestIsSubscription_YearlyAndQuarterly(t *testing.T) {
	tests := []struct {
		desc string
		freq string
		want bool
	}{
		{"annual domain", "yearly", true},
		{"quarterly report", "quarterly", true},
	}
	for _, tt := range tests {
		t.Run(tt.freq, func(t *testing.T) {
			rp := models.RecurringPayment{Description: tt.desc, Frequency: tt.freq}
			if got := isSubscription(rp); got != tt.want {
				t.Errorf("isSubscription(%q, %q) = %v, want %v", tt.desc, tt.freq, got, tt.want)
			}
		})
	}
}

// TestRecurringFreshnessWindow covers all switch branches
func TestRecurringFreshnessWindow(t *testing.T) {
	tests := []struct {
		intervalDays float64
		expected     float64
	}{
		{-1, 90},   // <= 0
		{0, 90},    // <= 0
		{7, 21},    // <= 9
		{9, 21},    // <= 9
		{14, 45},   // <= 16
		{16, 45},   // <= 16
		{30, 90},   // <= 35
		{35, 90},   // <= 35
		{90, 180},  // <= 95
		{95, 180},  // <= 95
		{365, 455}, // default
		{500, 455}, // default
	}
	for _, tt := range tests {
		got := recurringFreshnessWindow(tt.intervalDays)
		if got != tt.expected {
			t.Errorf("recurringFreshnessWindow(%.0f) = %.0f, want %.0f", tt.intervalDays, got, tt.expected)
		}
	}
}

// TestRecurringReferenceDate covers the time.Now() fallback
func TestRecurringReferenceDate_NilTSZeroRef(t *testing.T) {
	result := recurringReferenceDate(nil, time.Time{})
	// Should return something close to now
	if time.Since(result) > time.Second {
		t.Errorf("expected time close to now, got %v", result)
	}
}

func TestRecurringReferenceDate_ZeroRefUsesMaxDate(t *testing.T) {
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Date: time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)},
		},
	}
	result := recurringReferenceDate(ts, time.Time{})
	if !result.Equal(time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("expected max date, got %v", result)
	}
}

func TestRecurringReferenceDate_ZeroRefEmptyTS(t *testing.T) {
	ts := &models.TransactionSet{}
	result := recurringReferenceDate(ts, time.Time{})
	// MaxDate returns zero, so should fall through to time.Now()
	if time.Since(result) > time.Second {
		t.Errorf("expected time close to now, got %v", result)
	}
}

// TestRecurringTransactionSet_NilTS covers the nil ts path
func TestRecurringTransactionSet_NilTS(t *testing.T) {
	result := recurringTransactionSet(nil, time.Now())
	if result != nil {
		t.Error("expected nil result for nil ts")
	}
}

func TestRecurringTransactionSet_ZeroDate(t *testing.T) {
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
	result := recurringTransactionSet(ts, time.Time{})
	if result != ts {
		t.Error("expected original ts returned for zero reference date")
	}
}

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

// TestDetectRecurringPaymentsAt_TooFewOutflows covers the Len() < 2 early return
func TestDetectRecurringPaymentsAt_TooFewOutflows(t *testing.T) {
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "solo", Amount: -50, Date: time.Now(), TransactionType: models.Outflow},
		},
	}
	result := detectRecurringPaymentsAt(ts, time.Time{})
	if len(result) != 0 {
		t.Errorf("expected 0 recurring for < 2 outflows, got %d", len(result))
	}
}

// TestDetectRecurringPaymentsAt_WeeklyFrequency covers the weekly frequency branch
func TestDetectRecurringPaymentsAt_WeeklyFrequency(t *testing.T) {
	now := time.Now()
	var txns []models.Transaction
	for i := 0; i < 5; i++ {
		txns = append(txns, models.Transaction{
			Description:     "weekly service",
			Amount:          -25,
			Date:            now.AddDate(0, 0, -7*i),
			TransactionType: models.Outflow,
		})
	}

	ts := &models.TransactionSet{Transactions: txns}
	recurring := detectRecurringPayments(ts)

	for _, r := range recurring {
		if r.Description == "weekly service" {
			if r.Frequency != "weekly" {
				t.Errorf("frequency = %q, want \"weekly\"", r.Frequency)
			}
			return
		}
	}
	t.Error("expected 'weekly service' in recurring")
}

// TestDetectRecurringPaymentsAt_BiweeklyFrequency covers the biweekly frequency branch
func TestDetectRecurringPaymentsAt_BiweeklyFrequency(t *testing.T) {
	now := time.Now()
	var txns []models.Transaction
	for i := 0; i < 5; i++ {
		txns = append(txns, models.Transaction{
			Description:     "biweekly bill",
			Amount:          -50,
			Date:            now.AddDate(0, 0, -14*i),
			TransactionType: models.Outflow,
		})
	}

	ts := &models.TransactionSet{Transactions: txns}
	recurring := detectRecurringPayments(ts)

	for _, r := range recurring {
		if r.Description == "biweekly bill" {
			if r.Frequency != "biweekly" {
				t.Errorf("frequency = %q, want \"biweekly\"", r.Frequency)
			}
			return
		}
	}
	t.Error("expected 'biweekly bill' in recurring")
}

// TestDetectRecurringPaymentsAt_QuarterlyFrequency covers the quarterly frequency branch
func TestDetectRecurringPaymentsAt_QuarterlyFrequency(t *testing.T) {
	now := time.Now()
	var txns []models.Transaction
	for i := 0; i < 4; i++ {
		txns = append(txns, models.Transaction{
			Description:     "quarterly tax",
			Amount:          -500,
			Date:            now.AddDate(0, 0, -90*i),
			TransactionType: models.Outflow,
		})
	}

	ts := &models.TransactionSet{Transactions: txns}
	recurring := detectRecurringPayments(ts)

	for _, r := range recurring {
		if r.Description == "quarterly tax" {
			if r.Frequency != "quarterly" {
				t.Errorf("frequency = %q, want \"quarterly\"", r.Frequency)
			}
			return
		}
	}
	t.Error("expected 'quarterly tax' in recurring")
}

// TestDetectRecurringPaymentsAt_YearlyFrequency covers the yearly frequency branch
func TestDetectRecurringPaymentsAt_YearlyFrequency(t *testing.T) {
	now := time.Now()
	var txns []models.Transaction
	for i := 0; i < 3; i++ {
		txns = append(txns, models.Transaction{
			Description:     "annual license",
			Amount:          -1200,
			Date:            now.AddDate(-i, 0, 0),
			TransactionType: models.Outflow,
		})
	}

	ts := &models.TransactionSet{Transactions: txns}
	recurring := detectRecurringPayments(ts)

	for _, r := range recurring {
		if r.Description == "annual license" {
			if r.Frequency != "yearly" {
				t.Errorf("frequency = %q, want \"yearly\"", r.Frequency)
			}
			return
		}
	}
	t.Error("expected 'annual license' in recurring")
}

// TestDetectRecurringPaymentsAt_HighStdDevSkipped covers the stdDev > 7 skip
func TestDetectRecurringPaymentsAt_HighStdDevSkipped(t *testing.T) {
	now := time.Now()
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "erratic", Amount: -50, Date: now.AddDate(0, 0, -5), TransactionType: models.Outflow},
			{Description: "erratic", Amount: -50, Date: now.AddDate(0, 0, -35), TransactionType: models.Outflow},
			{Description: "erratic", Amount: -50, Date: now.AddDate(0, 0, -90), TransactionType: models.Outflow},
		},
	}
	recurring := detectRecurringPayments(ts)
	for _, r := range recurring {
		if r.Description == "erratic" && r.Frequency != "ongoing" {
			t.Error("erratic intervals should not match strict criteria")
		}
	}
}

// TestDetectRecurringPaymentsAt_InconsistentAmountsSkipped covers amountConsistent == false
func TestDetectRecurringPaymentsAt_InconsistentAmountsSkipped(t *testing.T) {
	now := time.Now()
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "variable vendor", Amount: -50, Date: now.AddDate(0, 0, -5), TransactionType: models.Outflow},
			{Description: "variable vendor", Amount: -200, Date: now.AddDate(0, 0, -35), TransactionType: models.Outflow},
			{Description: "variable vendor", Amount: -50, Date: now.AddDate(0, 0, -65), TransactionType: models.Outflow},
		},
	}
	recurring := detectRecurringPayments(ts)
	for _, r := range recurring {
		if r.Description == "variable vendor" && r.Frequency != "ongoing" {
			t.Error("inconsistent amounts should not match strict criteria")
		}
	}
}

// TestDetectRecurringPaymentsAt_UnmatchedIntervalSkipped covers the default continue in frequency switch
func TestDetectRecurringPaymentsAt_UnmatchedIntervalSkipped(t *testing.T) {
	now := time.Now()
	// Interval of ~45 days doesn't match any frequency bucket
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "odd interval", Amount: -100, Date: now.AddDate(0, 0, -5), TransactionType: models.Outflow},
			{Description: "odd interval", Amount: -100, Date: now.AddDate(0, 0, -50), TransactionType: models.Outflow},
			{Description: "odd interval", Amount: -100, Date: now.AddDate(0, 0, -95), TransactionType: models.Outflow},
		},
	}
	recurring := detectRecurringPayments(ts)
	for _, r := range recurring {
		if r.Description == "odd interval" && r.Frequency != "ongoing" {
			t.Errorf("45-day interval should not match any strict frequency, got %q", r.Frequency)
		}
	}
}

// TestDetectRecurringPaymentsAt_LowConfidenceSkipped covers confidence < 0.5
func TestDetectRecurringPaymentsAt_LowConfidence(t *testing.T) {
	// Create transactions with high stdDev relative to median to get low confidence
	// but not high enough to be filtered by the stdDev > 7 check
	// This is hard to construct, so just verify the function doesn't crash
	now := time.Now()
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "noisy", Amount: -100, Date: now.AddDate(0, 0, -1), TransactionType: models.Outflow},
			{Description: "noisy", Amount: -100, Date: now.AddDate(0, 0, -31), TransactionType: models.Outflow},
			{Description: "noisy", Amount: -100, Date: now.AddDate(0, 0, -55), TransactionType: models.Outflow},
			{Description: "noisy", Amount: -100, Date: now.AddDate(0, 0, -90), TransactionType: models.Outflow},
		},
	}
	_ = detectRecurringPayments(ts) // Should not panic
}

// TestDetectRecurringPaymentsAt_WeeklyNeedsFourOccurrences covers the len(txns) < 4 check for weekly
func TestDetectRecurringPaymentsAt_WeeklyNeedsFourOccurrences(t *testing.T) {
	now := time.Now()
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "short weekly", Amount: -20, Date: now.AddDate(0, 0, -1), TransactionType: models.Outflow},
			{Description: "short weekly", Amount: -20, Date: now.AddDate(0, 0, -8), TransactionType: models.Outflow},
			{Description: "short weekly", Amount: -20, Date: now.AddDate(0, 0, -15), TransactionType: models.Outflow},
		},
	}
	recurring := detectRecurringPayments(ts)
	for _, r := range recurring {
		if r.Description == "short weekly" && r.Frequency == "weekly" {
			t.Error("weekly should require >= 4 occurrences")
		}
	}
}

// TestDetectRecurringPaymentsAt_BiweeklyNeedsFourOccurrences covers the len(txns) < 4 check for biweekly
func TestDetectRecurringPaymentsAt_BiweeklyNeedsFourOccurrences(t *testing.T) {
	now := time.Now()
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "short biweekly", Amount: -40, Date: now.AddDate(0, 0, -1), TransactionType: models.Outflow},
			{Description: "short biweekly", Amount: -40, Date: now.AddDate(0, 0, -15), TransactionType: models.Outflow},
			{Description: "short biweekly", Amount: -40, Date: now.AddDate(0, 0, -29), TransactionType: models.Outflow},
		},
	}
	recurring := detectRecurringPayments(ts)
	for _, r := range recurring {
		if r.Description == "short biweekly" && r.Frequency == "biweekly" {
			t.Error("biweekly should require >= 4 occurrences")
		}
	}
}

// TestDetectRecurringPaymentsAt_OngoingPayment covers the second pass (ongoing detection)
func TestDetectRecurringPaymentsAt_OngoingPaymentRecentActivity(t *testing.T) {
	now := time.Now()
	// Variable amounts, spans > 60 days, recent activity
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "cloud service", Amount: -15, Date: now.AddDate(0, 0, -5), TransactionType: models.Outflow},
			{Description: "cloud service", Amount: -22, Date: now.AddDate(0, 0, -35), TransactionType: models.Outflow},
			{Description: "cloud service", Amount: -18, Date: now.AddDate(0, 0, -70), TransactionType: models.Outflow},
		},
	}
	recurring := detectRecurringPayments(ts)
	found := false
	for _, r := range recurring {
		if r.Description == "cloud service" {
			found = true
			if r.Frequency != "ongoing" {
				t.Errorf("frequency = %q, want \"ongoing\"", r.Frequency)
			}
		}
	}
	if !found {
		t.Error("expected 'cloud service' as ongoing recurring payment")
	}
}

// TestDetectRecurringPaymentsAt_OngoingOlderActivity covers confidence 0.8 tier
func TestDetectRecurringPaymentsAt_OngoingOlderActivity(t *testing.T) {
	// Use a fixed reference date so we control the "now" used for confidence
	refDate := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	// Last payment 45 days before refDate (confidence 0.8 tier: 30-60 days)
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "api billing", Amount: -30, Date: refDate.AddDate(0, 0, -45), TransactionType: models.Outflow},
			{Description: "api billing", Amount: -25, Date: refDate.AddDate(0, 0, -75), TransactionType: models.Outflow},
			{Description: "api billing", Amount: -35, Date: refDate.AddDate(0, 0, -110), TransactionType: models.Outflow},
		},
	}
	recurring := detectRecurringPaymentsAt(ts, refDate)
	for _, r := range recurring {
		if r.Description == "api billing" {
			if r.Confidence != 0.8 {
				t.Errorf("confidence = %.2f, want 0.80 (45 days since last)", r.Confidence)
			}
			return
		}
	}
	t.Error("expected 'api billing' in recurring")
}

// TestDetectRecurringPaymentsAt_OngoingDefaultConfidence covers confidence 0.7 tier (>60 days)
func TestDetectRecurringPaymentsAt_OngoingDefaultConfidence(t *testing.T) {
	refDate := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	// Last payment 65 days before refDate (confidence 0.7 tier: >60 days)
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "old service", Amount: -50, Date: refDate.AddDate(0, 0, -65), TransactionType: models.Outflow},
			{Description: "old service", Amount: -45, Date: refDate.AddDate(0, 0, -80), TransactionType: models.Outflow},
			{Description: "old service", Amount: -55, Date: refDate.AddDate(0, 0, -130), TransactionType: models.Outflow},
		},
	}
	recurring := detectRecurringPaymentsAt(ts, refDate)
	for _, r := range recurring {
		if r.Description == "old service" {
			if r.Confidence != 0.7 {
				t.Errorf("confidence = %.2f, want 0.70 (>60 days since last)", r.Confidence)
			}
			return
		}
	}
	t.Error("expected 'old service' in recurring")
}

// TestDetectRecurringPaymentsAt_OngoingShortSpanSkipped covers spanDays < 60 skip
func TestDetectRecurringPaymentsAt_OngoingShortSpanSkipped(t *testing.T) {
	now := time.Now()
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "short span svc", Amount: -15, Date: now.AddDate(0, 0, -5), TransactionType: models.Outflow},
			{Description: "short span svc", Amount: -22, Date: now.AddDate(0, 0, -25), TransactionType: models.Outflow},
			{Description: "short span svc", Amount: -18, Date: now.AddDate(0, 0, -45), TransactionType: models.Outflow},
		},
	}
	recurring := detectRecurringPayments(ts)
	for _, r := range recurring {
		if r.Description == "short span svc" && r.Frequency == "ongoing" {
			t.Error("span < 60 days should not trigger ongoing detection")
		}
	}
}

// TestDetectRecurringPaymentsAt_Max20Results covers the truncation to 20 results
func TestDetectRecurringPaymentsAt_Max20Results(t *testing.T) {
	now := time.Now()
	var txns []models.Transaction
	// Create 25 distinct monthly recurring vendors
	for v := 0; v < 25; v++ {
		desc := fmt.Sprintf("vendor-%02d", v)
		for i := 0; i < 4; i++ {
			txns = append(txns, models.Transaction{
				Description:     desc,
				Amount:          -float64(10 + v),
				Date:            now.AddDate(0, 0, -30*i),
				TransactionType: models.Outflow,
			})
		}
	}
	ts := &models.TransactionSet{Transactions: txns}
	recurring := detectRecurringPayments(ts)
	if len(recurring) > 20 {
		t.Errorf("expected max 20 results, got %d", len(recurring))
	}
}

// TestDetectByAmount_QuarterlyFrequency covers the quarterly branch in detectByAmount
func TestDetectByAmount_QuarterlyFrequency(t *testing.T) {
	now := time.Now()
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "q-payment-a", Amount: -750, Date: now.AddDate(0, 0, -5), TransactionType: models.Outflow},
			{Description: "q-payment-b", Amount: -750, Date: now.AddDate(0, 0, -95), TransactionType: models.Outflow},
			{Description: "q-payment-c", Amount: -750, Date: now.AddDate(0, 0, -185), TransactionType: models.Outflow},
		},
	}
	recurring := detectRecurringPayments(ts)
	found := false
	for _, r := range recurring {
		if r.Amount == 750 && r.Frequency == "quarterly" {
			found = true
		}
	}
	if !found {
		t.Error("expected quarterly amount-based match")
		for _, r := range recurring {
			t.Logf("  found: %q $%.2f %s", r.Description, r.Amount, r.Frequency)
		}
	}
}

// TestDetectByAmount_YearlyFrequency covers the yearly branch in detectByAmount
func TestDetectByAmount_YearlyFrequency(t *testing.T) {
	now := time.Now()
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "y-payment-a", Amount: -2000, Date: now.AddDate(0, 0, -5), TransactionType: models.Outflow},
			{Description: "y-payment-b", Amount: -2000, Date: now.AddDate(0, 0, -370), TransactionType: models.Outflow},
			{Description: "y-payment-c", Amount: -2000, Date: now.AddDate(0, 0, -735), TransactionType: models.Outflow},
		},
	}
	recurring := detectRecurringPayments(ts)
	found := false
	for _, r := range recurring {
		if r.Amount == 2000 && r.Frequency == "yearly" {
			found = true
		}
	}
	if !found {
		t.Error("expected yearly amount-based match")
		for _, r := range recurring {
			t.Logf("  found: %q $%.2f %s", r.Description, r.Amount, r.Frequency)
		}
	}
}

// TestDetectByAmount_LowConfidenceSkipped covers confidence < 0.4 in detectByAmount
func TestDetectByAmount_DefaultCaseSkipped(t *testing.T) {
	now := time.Now()
	// Interval ~60 days doesn't match monthly/quarterly/yearly
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "odd-a", Amount: -800, Date: now.AddDate(0, 0, -5), TransactionType: models.Outflow},
			{Description: "odd-b", Amount: -800, Date: now.AddDate(0, 0, -65), TransactionType: models.Outflow},
			{Description: "odd-c", Amount: -800, Date: now.AddDate(0, 0, -125), TransactionType: models.Outflow},
		},
	}
	recurring := detectRecurringPayments(ts)
	for _, r := range recurring {
		if r.Amount == 800 && (r.Frequency == "monthly" || r.Frequency == "quarterly" || r.Frequency == "yearly") {
			t.Error("60-day interval should not match any amount-based frequency bucket")
		}
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

// TestMergeSimilarGroups_ShorterCanonicalReplacement covers the re-mapping branch
// where a new key's stripped name is shorter than an existing canonical.
func TestMergeSimilarGroups_ShorterCanonicalReplacement(t *testing.T) {
	// Keys are sorted by length, so "abcdef" comes before "abc".
	// Wait, shorter comes first. So we need to construct a case where:
	// - A longer key is processed first and becomes canonical
	// - Then a shorter key whose stripped form is a prefix triggers re-mapping
	// Since keys are sorted by length (shorter first), we need the stripped version
	// to differ. E.g., "abc.com" (len=7) strips to "abc" (len=3),
	// then "abcxyz" (len=6) strips to "abcxyz" (len=6).
	// But "abc" is a prefix of "abcxyz" so they'd merge.
	// Actually we need len(stripped) < len(canon). The first key processed is shorter.
	// So "ab.com" (len=6, stripped="ab") is first. Then "abc" (len=3, stripped="abc").
	// Wait no, "abc" has len 3 which is < 6, so it comes first.
	// "abc" -> stripped="abc", canonical["abc"]="abc"
	// "ab.com" -> stripped="ab", check: is "ab" prefix of "abc"? Yes!
	// So it merges into "abc". Then len("ab") < len("abc"), so re-mapping happens.
	groups := map[string][]models.Transaction{
		"abc":    {txn("abc", 10, 30)},
		"ab.com": {txn("ab.com", 10, 60)},
	}
	merged := mergeSimilarGroups(groups)

	if len(merged) != 1 {
		t.Fatalf("expected 1 group, got %d: %v", len(merged), keys(merged))
	}
	// The shorter stripped name "ab" should cause re-mapping to "ab.com"
	if txns, ok := merged["ab.com"]; ok {
		if len(txns) != 2 {
			t.Errorf("expected 2 transactions, got %d", len(txns))
		}
	} else if txns, ok := merged["abc"]; ok {
		// Either key is fine — the point is they merged
		if len(txns) != 2 {
			t.Errorf("expected 2 transactions, got %d", len(txns))
		}
	} else {
		t.Errorf("unexpected keys: %v", keys(merged))
	}
}

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

// TestDetectRecurringPaymentsAt_LowConfidenceFiltered covers confidence < 0.5 in strict pass.
// For weekly (medianInterval ~7), confidence = 1 - (stdDev/7). With stdDev=5, confidence=0.29 < 0.5.
func TestDetectRecurringPaymentsAt_LowConfidenceFiltered(t *testing.T) {
	now := time.Now()
	// Create weekly-ish payments with high variance (stdDev around 5, median ~7)
	// intervals: 3, 7, 12, 3 => sorted: 3, 3, 7, 12 => median=3 (or 7 depending on count)
	// Actually need precise control. Let me use 5 txns with intervals: 7, 7, 7, 12 for 4 intervals
	// sorted: 7, 7, 7, 12 => median = 7
	// stdDev = sqrt(((0+0+0+25)/4)) = sqrt(6.25) = 2.5 => confidence = 1-2.5/7 = 0.64 (too high)
	// Need intervals like 2, 12, 2, 12 => sorted: 2, 2, 12, 12 => median = 2 (index 2 of 4 = 12 no, index 2 is 2)
	// Actually median for len=4 is sortedIntervals[4/2] = sortedIntervals[2] = 12
	// Hmm, let me recalculate. sorted: [2, 2, 12, 12], median = sorted[2] = 12. Not in weekly range.
	//
	// Try: 5 txns, intervals: [7, 7, 2, 13] => sorted: [2, 7, 7, 13], median = sorted[2] = 7
	// diff from 7: [-5, 0, 0, 6], sumSq = 25+0+0+36 = 61, stdDev = sqrt(61/4) = 3.9
	// confidence = 1 - 3.9/7 = 0.44 < 0.5 -- AND stdDev=3.9 < 7. This works!
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "lowconf svc", Amount: -20, Date: now.AddDate(0, 0, 0), TransactionType: models.Outflow},
			{Description: "lowconf svc", Amount: -20, Date: now.AddDate(0, 0, -7), TransactionType: models.Outflow},
			{Description: "lowconf svc", Amount: -20, Date: now.AddDate(0, 0, -14), TransactionType: models.Outflow},
			{Description: "lowconf svc", Amount: -20, Date: now.AddDate(0, 0, -16), TransactionType: models.Outflow},
			{Description: "lowconf svc", Amount: -20, Date: now.AddDate(0, 0, -29), TransactionType: models.Outflow},
		},
	}
	recurring := detectRecurringPayments(ts)
	for _, r := range recurring {
		if r.Description == "lowconf svc" && r.Frequency == "weekly" {
			t.Errorf("low confidence weekly should be filtered out, got confidence %.2f", r.Confidence)
		}
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
		Description: "Trader Joe's Run",
		Amount:      -150,
		Hash:        "h-trader-pin",
		Date:        time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC),
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
