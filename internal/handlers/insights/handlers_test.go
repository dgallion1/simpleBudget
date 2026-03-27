package insights

import (
	"fmt"
	"math"
	"testing"
	"time"

	"budget2/internal/models"
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
	merged := mergeSimlarGroups(groups)

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
	merged := mergeSimlarGroups(groups)

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
	merged := mergeSimlarGroups(groups)

	if len(merged) != 3 {
		t.Fatalf("expected 3 separate groups, got %d: %v", len(merged), keys(merged))
	}
}

func TestMergeSimlarGroups_PreservesCanonicalName(t *testing.T) {
	groups := map[string][]models.Transaction{
		"lucidmotors.com": {txn("lucidmotors.com", 1580, 90)},
		"lucid":           {txn("lucid", 1580, 30)},
	}
	merged := mergeSimlarGroups(groups)

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
			merged := mergeSimlarGroups(groups)
			if len(merged) != 1 {
				t.Errorf("expected 1 group for %q and %q, got %d", tt.a, tt.b, len(merged))
			}
		})
	}
}

func TestMergeSimlarGroups_EmptyInput(t *testing.T) {
	merged := mergeSimlarGroups(map[string][]models.Transaction{})
	if len(merged) != 0 {
		t.Errorf("expected 0 groups, got %d", len(merged))
	}
}

func TestMergeSimlarGroups_SingleGroup(t *testing.T) {
	groups := map[string][]models.Transaction{
		"netflix": {txn("netflix", 15, 30), txn("netflix", 15, 60)},
	}
	merged := mergeSimlarGroups(groups)
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
