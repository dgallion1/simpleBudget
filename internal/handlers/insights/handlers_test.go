package insights

import (
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
		"lucid":          {txn("lucid", 1580, 30), txn("lucid", 1580, 60)},
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
			txn("payment b", 500.00, 10),  // 5 days later
			txn("payment c", 500.00, 60),  // 50 days later
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

func keys(m map[string][]models.Transaction) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
