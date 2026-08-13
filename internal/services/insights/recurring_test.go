package insights

import (
	"fmt"
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

func keys(m map[string][]models.Transaction) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
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

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("Lucid", 1580, 5),
			txn("Lucidmotors.com", 1580, 35),
			txn("Lucid", 1580, 65),
		},
	}
	recurring := DetectRecurring(ts)

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

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("netflix", 15.99, 5),
			txn("netflix", 15.99, 35),
			txn("netflix", 15.99, 65),
			txn("netflix", 15.99, 95),
		},
	}
	recurring := DetectRecurring(ts)

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
			got := IsSubscription(rp)
			if got != tt.wantIsSub {
				t.Errorf("IsSubscription(%q, %q) = %v, want %v", tt.desc, tt.freq, got, tt.wantIsSub)
			}
		})
	}
}

func TestDetectRecurringPayments_AmountBasedGrouping(t *testing.T) {

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("Lucid", 1580.43, 5),
			txn("Check #996581", 1580.43, 28),
			txn("Check #996578", 1580.43, 56),
		},
	}
	recurring := DetectRecurring(ts)

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

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("coffee shop a", 3.50, 5),
			txn("coffee shop b", 3.50, 35),
			txn("coffee shop c", 3.50, 65),
		},
	}
	recurring := DetectRecurring(ts)

	for _, r := range recurring {
		if r.Amount == 3.50 {
			t.Error("should not match tiny amounts via amount-based grouping")
		}
	}
}

func TestDetectRecurringPayments_AmountBasedNoFalsePositivesDifferentAmounts(t *testing.T) {

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("vendor a", 100.00, 5),
			txn("vendor b", 200.00, 35),
			txn("vendor c", 300.00, 65),
		},
	}
	recurring := DetectRecurring(ts)

	if len(recurring) != 0 {
		t.Errorf("expected no recurring payments, got %d", len(recurring))
		for _, r := range recurring {
			t.Logf("  found: %q $%.2f (%d occurrences)", r.Description, r.Amount, r.Occurrences)
		}
	}
}

func TestDetectRecurringPayments_AmountBasedSkipsAlreadyMatched(t *testing.T) {

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("netflix", 15.99, 5),
			txn("netflix", 15.99, 35),
			txn("netflix", 15.99, 65),
			txn("netflix", 15.99, 95),
		},
	}
	recurring := DetectRecurring(ts)

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

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			txn("payment a", 500.00, 5),
			txn("payment b", 500.00, 10),
			txn("payment c", 500.00, 60),
		},
	}
	recurring := DetectRecurring(ts)

	for _, r := range recurring {
		if r.Amount == 500.00 {
			t.Errorf("should not match irregular intervals, got %q %s", r.Description, r.Frequency)
		}
	}
}

func TestDetectRecurringPayments_LongHistoryPattern(t *testing.T) {

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
	recurring := DetectRecurring(ts)

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

	recurring := DetectRecurringAt(ts, time.Now())

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

	recurring := DetectRecurringAt(ts, time.Now())

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

	recurring := DetectRecurring(ts)

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

	recurring := DetectRecurringAt(ts, time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC))

	for _, r := range recurring {
		if r.Description == "future club" {
			t.Fatalf("expected recurring detection to ignore transactions after reference date, got %+v", r)
		}
	}
}

// TestIsSubscription_UnknownFrequency covers the return false at end of isSubscription
func TestIsSubscription_UnknownFrequency(t *testing.T) {
	rp := models.RecurringPayment{Description: "some vendor", Frequency: "weekly"}
	if IsSubscription(rp) {
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
			if got := IsSubscription(rp); got != tt.want {
				t.Errorf("IsSubscription(%q, %q) = %v, want %v", tt.desc, tt.freq, got, tt.want)
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
		{-1, 90},
		{0, 90},
		{7, 21},
		{9, 21},
		{14, 45},
		{16, 45},
		{30, 90},
		{35, 90},
		{90, 180},
		{95, 180},
		{365, 455},
		{500, 455},
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
	result := ReferenceDate(nil, time.Time{})

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
	result := ReferenceDate(ts, time.Time{})
	if !result.Equal(time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("expected max date, got %v", result)
	}
}

func TestRecurringReferenceDate_ZeroRefEmptyTS(t *testing.T) {
	ts := &models.TransactionSet{}
	result := ReferenceDate(ts, time.Time{})

	if time.Since(result) > time.Second {
		t.Errorf("expected time close to now, got %v", result)
	}
}

// TestRecurringTransactionSet_NilTS covers the nil ts path
func TestRecurringTransactionSet_NilTS(t *testing.T) {
	result := TransactionSetForRecurring(nil, time.Now())
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
	result := TransactionSetForRecurring(ts, time.Time{})
	if result != ts {
		t.Error("expected original ts returned for zero reference date")
	}
}

// TestDetectRecurringPaymentsAt_TooFewOutflows covers the Len() < 2 early return
func TestDetectRecurringPaymentsAt_TooFewOutflows(t *testing.T) {
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "solo", Amount: -50, Date: time.Now(), TransactionType: models.Outflow},
		},
	}
	result := DetectRecurringAt(ts, time.Time{})
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
	recurring := DetectRecurring(ts)

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
	recurring := DetectRecurring(ts)

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
	recurring := DetectRecurring(ts)

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
	recurring := DetectRecurring(ts)

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
	recurring := DetectRecurring(ts)
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
	recurring := DetectRecurring(ts)
	for _, r := range recurring {
		if r.Description == "variable vendor" && r.Frequency != "ongoing" {
			t.Error("inconsistent amounts should not match strict criteria")
		}
	}
}

// TestDetectRecurringPaymentsAt_UnmatchedIntervalSkipped covers the default continue in frequency switch
func TestDetectRecurringPaymentsAt_UnmatchedIntervalSkipped(t *testing.T) {
	now := time.Now()

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "odd interval", Amount: -100, Date: now.AddDate(0, 0, -5), TransactionType: models.Outflow},
			{Description: "odd interval", Amount: -100, Date: now.AddDate(0, 0, -50), TransactionType: models.Outflow},
			{Description: "odd interval", Amount: -100, Date: now.AddDate(0, 0, -95), TransactionType: models.Outflow},
		},
	}
	recurring := DetectRecurring(ts)
	for _, r := range recurring {
		if r.Description == "odd interval" && r.Frequency != "ongoing" {
			t.Errorf("45-day interval should not match any strict frequency, got %q", r.Frequency)
		}
	}
}

// TestDetectRecurringPaymentsAt_LowConfidenceSkipped covers confidence < 0.5
func TestDetectRecurringPaymentsAt_LowConfidence(t *testing.T) {

	now := time.Now()
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "noisy", Amount: -100, Date: now.AddDate(0, 0, -1), TransactionType: models.Outflow},
			{Description: "noisy", Amount: -100, Date: now.AddDate(0, 0, -31), TransactionType: models.Outflow},
			{Description: "noisy", Amount: -100, Date: now.AddDate(0, 0, -55), TransactionType: models.Outflow},
			{Description: "noisy", Amount: -100, Date: now.AddDate(0, 0, -90), TransactionType: models.Outflow},
		},
	}
	_ = DetectRecurring(ts)
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
	recurring := DetectRecurring(ts)
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
	recurring := DetectRecurring(ts)
	for _, r := range recurring {
		if r.Description == "short biweekly" && r.Frequency == "biweekly" {
			t.Error("biweekly should require >= 4 occurrences")
		}
	}
}

// TestDetectRecurringPaymentsAt_OngoingPayment covers the second pass (ongoing detection)
func TestDetectRecurringPaymentsAt_OngoingPaymentRecentActivity(t *testing.T) {
	now := time.Now()

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "cloud service", Amount: -15, Date: now.AddDate(0, 0, -5), TransactionType: models.Outflow},
			{Description: "cloud service", Amount: -22, Date: now.AddDate(0, 0, -35), TransactionType: models.Outflow},
			{Description: "cloud service", Amount: -18, Date: now.AddDate(0, 0, -70), TransactionType: models.Outflow},
		},
	}
	recurring := DetectRecurring(ts)
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

	refDate := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "api billing", Amount: -30, Date: refDate.AddDate(0, 0, -45), TransactionType: models.Outflow},
			{Description: "api billing", Amount: -25, Date: refDate.AddDate(0, 0, -75), TransactionType: models.Outflow},
			{Description: "api billing", Amount: -35, Date: refDate.AddDate(0, 0, -110), TransactionType: models.Outflow},
		},
	}
	recurring := DetectRecurringAt(ts, refDate)
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

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "old service", Amount: -50, Date: refDate.AddDate(0, 0, -65), TransactionType: models.Outflow},
			{Description: "old service", Amount: -45, Date: refDate.AddDate(0, 0, -80), TransactionType: models.Outflow},
			{Description: "old service", Amount: -55, Date: refDate.AddDate(0, 0, -130), TransactionType: models.Outflow},
		},
	}
	recurring := DetectRecurringAt(ts, refDate)
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
	recurring := DetectRecurring(ts)
	for _, r := range recurring {
		if r.Description == "short span svc" && r.Frequency == "ongoing" {
			t.Error("span < 60 days should not trigger ongoing detection")
		}
	}
}

// TestDetectRecurringPaymentsAt_OngoingStaleActivitySkipped covers the
// ongoing pass's own recurringPaymentIsActive(lastDate, 0, now) freshness
// check (the "not active" continue branch, distinct from
// OngoingShortSpanSkipped's spanDays < 60 check above): a variable-amount
// series that spans well over 60 days but whose last occurrence is more
// than 90 days (recurringFreshnessWindow's default for intervalDays <= 0)
// before the reference date must not be reported as an active "ongoing"
// recurring payment.
func TestDetectRecurringPaymentsAt_OngoingStaleActivitySkipped(t *testing.T) {
	refDate := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "old api billing", Amount: -30, Date: refDate.AddDate(0, 0, -95), TransactionType: models.Outflow},
			{Description: "old api billing", Amount: -25, Date: refDate.AddDate(0, 0, -140), TransactionType: models.Outflow},
			{Description: "old api billing", Amount: -35, Date: refDate.AddDate(0, 0, -200), TransactionType: models.Outflow},
		},
	}
	recurring := DetectRecurringAt(ts, refDate)
	for _, r := range recurring {
		if r.Description == "old api billing" {
			t.Errorf("a series last seen 95 days before the reference date is not active: %+v", r)
		}
	}
}

// TestDetectRecurringPaymentsAt_Max20Results covers the truncation to 20 results
func TestDetectRecurringPaymentsAt_Max20Results(t *testing.T) {
	now := time.Now()
	var txns []models.Transaction

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
	recurring := DetectRecurring(ts)
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
	recurring := DetectRecurring(ts)
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
	recurring := DetectRecurring(ts)
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

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "odd-a", Amount: -800, Date: now.AddDate(0, 0, -5), TransactionType: models.Outflow},
			{Description: "odd-b", Amount: -800, Date: now.AddDate(0, 0, -65), TransactionType: models.Outflow},
			{Description: "odd-c", Amount: -800, Date: now.AddDate(0, 0, -125), TransactionType: models.Outflow},
		},
	}
	recurring := DetectRecurring(ts)
	for _, r := range recurring {
		if r.Amount == 800 && (r.Frequency == "monthly" || r.Frequency == "quarterly" || r.Frequency == "yearly") {
			t.Error("60-day interval should not match any amount-based frequency bucket")
		}
	}
}

// TestMergeSimilarGroups_ShorterCanonicalReplacement covers the re-mapping branch
// where a new key's stripped name is shorter than an existing canonical.
func TestMergeSimilarGroups_ShorterCanonicalReplacement(t *testing.T) {

	groups := map[string][]models.Transaction{
		"abc":    {txn("abc", 10, 30)},
		"ab.com": {txn("ab.com", 10, 60)},
	}
	merged := mergeSimilarGroups(groups)

	if len(merged) != 1 {
		t.Fatalf("expected 1 group, got %d: %v", len(merged), keys(merged))
	}

	if txns, ok := merged["ab.com"]; ok {
		if len(txns) != 2 {
			t.Errorf("expected 2 transactions, got %d", len(txns))
		}
	} else if txns, ok := merged["abc"]; ok {

		if len(txns) != 2 {
			t.Errorf("expected 2 transactions, got %d", len(txns))
		}
	} else {
		t.Errorf("unexpected keys: %v", keys(merged))
	}
}

// TestDetectRecurringPaymentsAt_LowConfidenceFiltered covers confidence < 0.5 in strict pass.
// For weekly (medianInterval ~7), confidence = 1 - (stdDev/7). With stdDev=5, confidence=0.29 < 0.5.
func TestDetectRecurringPaymentsAt_LowConfidenceFiltered(t *testing.T) {
	now := time.Now()

	ts := &models.TransactionSet{
		Transactions: []models.Transaction{
			{Description: "lowconf svc", Amount: -20, Date: now.AddDate(0, 0, 0), TransactionType: models.Outflow},
			{Description: "lowconf svc", Amount: -20, Date: now.AddDate(0, 0, -7), TransactionType: models.Outflow},
			{Description: "lowconf svc", Amount: -20, Date: now.AddDate(0, 0, -14), TransactionType: models.Outflow},
			{Description: "lowconf svc", Amount: -20, Date: now.AddDate(0, 0, -16), TransactionType: models.Outflow},
			{Description: "lowconf svc", Amount: -20, Date: now.AddDate(0, 0, -29), TransactionType: models.Outflow},
		},
	}
	recurring := DetectRecurring(ts)
	for _, r := range recurring {
		if r.Description == "lowconf svc" && r.Frequency == "weekly" {
			t.Errorf("low confidence weekly should be filtered out, got confidence %.2f", r.Confidence)
		}
	}
}
