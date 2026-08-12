package pricecreep

import (
	"reflect"
	"testing"
	"time"

	"budget2/internal/models"
)

// txn builds an expense (Outflow, negative Amount) transaction dated
// monthsAgo months before a fixed reference date, so fixtures read
// top-to-bottom in chronological order regardless of when the test runs.
func txn(desc string, amount float64, monthsAgo int) models.Transaction {
	ref := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	return models.Transaction{
		Description:     desc,
		Amount:          -amount,
		Date:            ref.AddDate(0, -monthsAgo, 0),
		TransactionType: models.Outflow,
	}
}

// TestDetect_StepDrift plants an 18-month NETFLIX-style history that steps
// from $10.00 to $11.80 (+18%, inside the documented +17-20% band) between
// its first 3 and last 3 occurrences and asserts it is reported.
func TestDetect_StepDrift(t *testing.T) {
	var txns []models.Transaction
	// Oldest first: 18 months ago down to 0 months ago.
	for m := 17; m >= 3; m-- {
		txns = append(txns, txn("NETFLIX.COM", 10.00, m))
	}
	txns = append(txns, txn("NETFLIX.COM", 11.80, 2))
	txns = append(txns, txn("NETFLIX.COM", 11.80, 1))
	txns = append(txns, txn("NETFLIX.COM", 11.80, 0))

	ts := models.TransactionSet{Transactions: txns}
	creeps := Detect(ts)

	found := false
	for _, c := range creeps {
		if c.GroupKey == "NETFLIX.COM" {
			found = true
			if c.PctChange < 17 || c.PctChange > 20 {
				t.Errorf("expected PctChange in [17,20], got %.2f", c.PctChange)
			}
			if c.FirstAmount != 10.00 {
				t.Errorf("expected FirstAmount 10.00, got %.2f", c.FirstAmount)
			}
			if c.CurrentAmount != 11.80 {
				t.Errorf("expected CurrentAmount 11.80, got %.2f", c.CurrentAmount)
			}
			if c.Occurrences != 18 {
				t.Errorf("expected 18 occurrences, got %d", c.Occurrences)
			}
			if c.Merchant != "netflix.com" {
				t.Errorf("expected merchant label %q, got %q", "netflix.com", c.Merchant)
			}
		}
	}
	if !found {
		t.Fatalf("expected NETFLIX.COM price creep to be reported, got %#v", creeps)
	}
}

// TestDetect_FlatMerchantAbsent plants a flat-amount SPOTIFY history (no
// drift) and asserts it is never reported.
func TestDetect_FlatMerchantAbsent(t *testing.T) {
	var txns []models.Transaction
	for m := 7; m >= 0; m-- {
		txns = append(txns, txn("SPOTIFY", 9.99, m))
	}

	ts := models.TransactionSet{Transactions: txns}
	creeps := Detect(ts)

	for _, c := range creeps {
		if c.GroupKey == "SPOTIFY" {
			t.Fatalf("flat-amount merchant must not be reported as a creep: %#v", c)
		}
	}
}

// TestDetect_SingleOutlierAmongLastThreeNotReported plants a flat $50
// group of 6 occurrences where the 6th (final) transaction spikes to $120.
// Because that spike falls inside the "last 3" window, the median of the
// last 3 ([50,50,120] -> 50) must absorb it and NOT report a creep.
func TestDetect_SingleOutlierAmongLastThreeNotReported(t *testing.T) {
	txns := []models.Transaction{
		txn("ACME MONTHLY", 50, 5),
		txn("ACME MONTHLY", 50, 4),
		txn("ACME MONTHLY", 50, 3),
		txn("ACME MONTHLY", 50, 2),
		txn("ACME MONTHLY", 50, 1),
		txn("ACME MONTHLY", 120, 0), // single outlier, most recent
	}

	ts := models.TransactionSet{Transactions: txns}
	creeps := Detect(ts)

	for _, c := range creeps {
		if c.GroupKey == "ACME MONTHLY" {
			t.Fatalf("single last-window outlier must not fake a creep: %#v", c)
		}
	}
}

// TestDetect_GenuineStepAtMinimumOccurrences plants exactly 6 occurrences
// (the minimum group size) stepping from $50 (first 3) to $60 (last 3), a
// clean +20% increase, and asserts it is reported.
func TestDetect_GenuineStepAtMinimumOccurrences(t *testing.T) {
	txns := []models.Transaction{
		txn("WIDGET CO", 50, 5),
		txn("WIDGET CO", 50, 4),
		txn("WIDGET CO", 50, 3),
		txn("WIDGET CO", 60, 2),
		txn("WIDGET CO", 60, 1),
		txn("WIDGET CO", 60, 0),
	}

	ts := models.TransactionSet{Transactions: txns}
	creeps := Detect(ts)

	found := false
	for _, c := range creeps {
		if c.GroupKey == "WIDGET CO" {
			found = true
			if c.PctChange != 20 {
				t.Errorf("expected PctChange 20, got %.4f", c.PctChange)
			}
			if c.Occurrences != 6 {
				t.Errorf("expected 6 occurrences, got %d", c.Occurrences)
			}
		}
	}
	if !found {
		t.Fatalf("expected WIDGET CO genuine step to be reported, got %#v", creeps)
	}
}

// TestDetect_FiveOccurrencesExcluded plants a 5-occurrence group with a
// drastic increase and asserts it is excluded purely for falling below the
// 6-occurrence minimum.
func TestDetect_FiveOccurrencesExcluded(t *testing.T) {
	txns := []models.Transaction{
		txn("FIVE TIMES CO", 50, 4),
		txn("FIVE TIMES CO", 50, 3),
		txn("FIVE TIMES CO", 50, 2),
		txn("FIVE TIMES CO", 100, 1),
		txn("FIVE TIMES CO", 100, 0),
	}

	ts := models.TransactionSet{Transactions: txns}
	creeps := Detect(ts)

	for _, c := range creeps {
		if c.GroupKey == "FIVE TIMES CO" {
			t.Fatalf("5-occurrence group must be excluded regardless of drift: %#v", c)
		}
	}
}

// TestDetect_DecreaseAbsent plants a group whose amount drops between the
// first-3 and last-3 windows and asserts decreases never report.
func TestDetect_DecreaseAbsent(t *testing.T) {
	txns := []models.Transaction{
		txn("SHRINKING CO", 100, 5),
		txn("SHRINKING CO", 100, 4),
		txn("SHRINKING CO", 100, 3),
		txn("SHRINKING CO", 80, 2),
		txn("SHRINKING CO", 80, 1),
		txn("SHRINKING CO", 80, 0),
	}

	ts := models.TransactionSet{Transactions: txns}
	creeps := Detect(ts)

	for _, c := range creeps {
		if c.GroupKey == "SHRINKING CO" {
			t.Fatalf("decreases must never be reported: %#v", c)
		}
	}
}

// TestDetect_EmptyInputNoPanic covers a zero-value and empty TransactionSet.
func TestDetect_EmptyInputNoPanic(t *testing.T) {
	if got := Detect(models.TransactionSet{}); len(got) != 0 {
		t.Errorf("expected no creeps from empty input, got %#v", got)
	}
	if got := Detect(models.TransactionSet{Transactions: nil}); len(got) != 0 {
		t.Errorf("expected no creeps from nil transactions, got %#v", got)
	}
}

// TestDetect_SmallInputNoPanic covers inputs too small to ever qualify.
func TestDetect_SmallInputNoPanic(t *testing.T) {
	txns := []models.Transaction{
		txn("ONE OFF", 10, 1),
		txn("ONE OFF", 10, 0),
	}
	ts := models.TransactionSet{Transactions: txns}
	got := Detect(ts)
	if len(got) != 0 {
		t.Errorf("expected no creeps from a 2-transaction group, got %#v", got)
	}
}

// TestDetect_Deterministic runs Detect twice on the same input and asserts
// identical results, including order.
func TestDetect_Deterministic(t *testing.T) {
	var txns []models.Transaction
	for m := 17; m >= 3; m-- {
		txns = append(txns, txn("NETFLIX.COM", 10.00, m))
	}
	txns = append(txns,
		txn("NETFLIX.COM", 11.80, 2),
		txn("NETFLIX.COM", 11.80, 1),
		txn("NETFLIX.COM", 11.80, 0),
	)
	for m := 5; m >= 0; m-- {
		txns = append(txns, txn("WIDGET CO", 50, m+1))
	}
	txns = append(txns,
		txn("SPOTIFY", 9.99, 3),
		txn("SPOTIFY", 9.99, 2),
		txn("SPOTIFY", 9.99, 1),
		txn("SPOTIFY", 9.99, 0),
	)

	ts := models.TransactionSet{Transactions: txns}
	got1 := Detect(ts)
	got2 := Detect(ts)

	if !reflect.DeepEqual(got1, got2) {
		t.Errorf("Detect not deterministic:\nfirst:  %#v\nsecond: %#v", got1, got2)
	}
}

// TestDetect_ExpensesOnly ignores income (positive Amount) transactions,
// even when they share a merchant description and would otherwise show
// drift.
func TestDetect_ExpensesOnly(t *testing.T) {
	txns := []models.Transaction{
		{Description: "REFUND CO", Amount: 50, Date: time.Now().AddDate(0, -5, 0), TransactionType: models.Income},
		{Description: "REFUND CO", Amount: 50, Date: time.Now().AddDate(0, -4, 0), TransactionType: models.Income},
		{Description: "REFUND CO", Amount: 50, Date: time.Now().AddDate(0, -3, 0), TransactionType: models.Income},
		{Description: "REFUND CO", Amount: 100, Date: time.Now().AddDate(0, -2, 0), TransactionType: models.Income},
		{Description: "REFUND CO", Amount: 100, Date: time.Now().AddDate(0, -1, 0), TransactionType: models.Income},
		{Description: "REFUND CO", Amount: 100, Date: time.Now(), TransactionType: models.Income},
	}

	ts := models.TransactionSet{Transactions: txns}
	creeps := Detect(ts)

	for _, c := range creeps {
		if c.GroupKey == "REFUND CO" {
			t.Fatalf("income transactions must never be treated as expenses: %#v", c)
		}
	}
}

// TestDetect_OutflowPositiveAmountExcluded plants an Outflow-tagged group
// whose amounts are positive (e.g. a refund posted with the Outflow type
// but a non-negative Amount) and asserts it is excluded: the expense
// filter requires BOTH TransactionType == models.Outflow AND Amount < 0
// (app-native convention, matching anomalies post-ruling), so a positive
// Amount must never qualify even when TransactionType is Outflow.
func TestDetect_OutflowPositiveAmountExcluded(t *testing.T) {
	ref := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	txns := []models.Transaction{
		{Description: "REFUND POSTED CO", Amount: 50, Date: ref.AddDate(0, -5, 0), TransactionType: models.Outflow},
		{Description: "REFUND POSTED CO", Amount: 50, Date: ref.AddDate(0, -4, 0), TransactionType: models.Outflow},
		{Description: "REFUND POSTED CO", Amount: 50, Date: ref.AddDate(0, -3, 0), TransactionType: models.Outflow},
		{Description: "REFUND POSTED CO", Amount: 100, Date: ref.AddDate(0, -2, 0), TransactionType: models.Outflow},
		{Description: "REFUND POSTED CO", Amount: 100, Date: ref.AddDate(0, -1, 0), TransactionType: models.Outflow},
		{Description: "REFUND POSTED CO", Amount: 100, Date: ref, TransactionType: models.Outflow},
	}

	ts := models.TransactionSet{Transactions: txns}
	creeps := Detect(ts)

	for _, c := range creeps {
		if c.GroupKey == "REFUND POSTED CO" {
			t.Fatalf("Outflow-tagged rows with non-negative Amount must never be treated as expenses: %#v", c)
		}
	}
}

// TestDetect_MistaggedIncomeExcluded plants a group with negative Amounts
// (would otherwise look like a genuine expense drift) but tagged
// TransactionType == models.Income, and asserts it is excluded: the
// TransactionType == Outflow requirement must hold regardless of the
// Amount sign.
func TestDetect_MistaggedIncomeExcluded(t *testing.T) {
	ref := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	txns := []models.Transaction{
		{Description: "MISTAGGED CO", Amount: -50, Date: ref.AddDate(0, -5, 0), TransactionType: models.Income},
		{Description: "MISTAGGED CO", Amount: -50, Date: ref.AddDate(0, -4, 0), TransactionType: models.Income},
		{Description: "MISTAGGED CO", Amount: -50, Date: ref.AddDate(0, -3, 0), TransactionType: models.Income},
		{Description: "MISTAGGED CO", Amount: -100, Date: ref.AddDate(0, -2, 0), TransactionType: models.Income},
		{Description: "MISTAGGED CO", Amount: -100, Date: ref.AddDate(0, -1, 0), TransactionType: models.Income},
		{Description: "MISTAGGED CO", Amount: -100, Date: ref, TransactionType: models.Income},
	}

	ts := models.TransactionSet{Transactions: txns}
	creeps := Detect(ts)

	for _, c := range creeps {
		if c.GroupKey == "MISTAGGED CO" {
			t.Fatalf("Income-tagged rows must never be treated as expenses regardless of Amount sign: %#v", c)
		}
	}
}

// TestDetect_SameDayTieBrokenByHash plants a 6-occurrence group whose
// middle two rows (indices 2 and 3 in date order — exactly the boundary
// between the first-3 and last-3 windows at the minimum group size) share
// the SAME date but different Hash values and different amounts. Without
// a deterministic secondary sort key, which of the two lands in the
// first-3 window versus the last-3 window is unspecified for a same-day
// tie, so the computed FirstAmount/CurrentAmount (and therefore whether a
// creep is reported at all) could silently depend on input slice order.
//
// The ruled fix sorts equal dates by Hash ascending. Hash "AAA" < "ZZZ",
// so the row with Hash "AAA" (amount 60) must always land in the
// first-3 window and the row with Hash "ZZZ" (amount 150) in the
// last-3 window, regardless of the order the six transactions are handed
// to Detect in. This test feeds two different input orderings of the
// same six transactions and asserts identical results.
func TestDetect_SameDayTieBrokenByHash(t *testing.T) {
	ref := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	tieDate := ref.AddDate(0, -3, 0)

	// idx0, idx1: earliest two dates, distinct amounts (10, 100).
	idx0 := models.Transaction{Description: "TIE CO", Amount: -10, Date: ref.AddDate(0, -5, 0), TransactionType: models.Outflow, Hash: "idx0"}
	idx1 := models.Transaction{Description: "TIE CO", Amount: -100, Date: ref.AddDate(0, -4, 0), TransactionType: models.Outflow, Hash: "idx1"}
	// idx2, idx3: same date (tieDate), Hash "AAA" < "ZZZ" so idx2 (Hash
	// AAA, amount 60) must sort before idx3 (Hash ZZZ, amount 150).
	idx2 := models.Transaction{Description: "TIE CO", Amount: -60, Date: tieDate, TransactionType: models.Outflow, Hash: "AAA"}
	idx3 := models.Transaction{Description: "TIE CO", Amount: -150, Date: tieDate, TransactionType: models.Outflow, Hash: "ZZZ"}
	// idx4, idx5: latest two dates, distinct amounts (500, 1000).
	idx4 := models.Transaction{Description: "TIE CO", Amount: -500, Date: ref.AddDate(0, -2, 0), TransactionType: models.Outflow, Hash: "idx4"}
	idx5 := models.Transaction{Description: "TIE CO", Amount: -1000, Date: ref.AddDate(0, -1, 0), TransactionType: models.Outflow, Hash: "idx5"}

	// Expected deterministic windows: first3 = {10,100,60} -> median 60;
	// last3 = {150,500,1000} -> median 500.
	wantFirst := 60.0
	wantCurrent := 500.0

	findCreep := func(t *testing.T, txns []models.Transaction) *Creep {
		t.Helper()
		ts := models.TransactionSet{Transactions: txns}
		creeps := Detect(ts)
		for i := range creeps {
			if creeps[i].GroupKey == "TIE CO" {
				return &creeps[i]
			}
		}
		t.Fatalf("expected TIE CO creep to be reported, got %#v", creeps)
		return nil
	}

	orderings := map[string][]models.Transaction{
		"forward":  {idx0, idx1, idx2, idx3, idx4, idx5},
		"shuffled": {idx3, idx1, idx4, idx0, idx2, idx5},
		"reverse":  {idx5, idx4, idx3, idx2, idx1, idx0},
	}

	for name, txns := range orderings {
		t.Run(name, func(t *testing.T) {
			c := findCreep(t, txns)
			if c.FirstAmount != wantFirst {
				t.Errorf("FirstAmount = %.2f, want %.2f (tie-break must place Hash %q in the first-3 window)", c.FirstAmount, wantFirst, idx2.Hash)
			}
			if c.CurrentAmount != wantCurrent {
				t.Errorf("CurrentAmount = %.2f, want %.2f (tie-break must place Hash %q in the last-3 window)", c.CurrentAmount, wantCurrent, idx3.Hash)
			}
		})
	}
}

// TestDetect_RespectsSuppressed excludes Suppressed transactions via
// Active() before grouping: without the 2 suppressed rows the group would
// only have 4 occurrences and fall below the minimum.
func TestDetect_RespectsSuppressed(t *testing.T) {
	base := []models.Transaction{
		txn("DUP CO", 50, 5),
		txn("DUP CO", 50, 4),
		txn("DUP CO", 50, 3),
		txn("DUP CO", 60, 2),
	}
	suppressed := []models.Transaction{
		txn("DUP CO", 60, 1),
		txn("DUP CO", 60, 0),
	}
	for i := range suppressed {
		suppressed[i].Suppressed = true
	}

	txns := append(append([]models.Transaction{}, base...), suppressed...)
	ts := models.TransactionSet{Transactions: txns}
	creeps := Detect(ts)

	for _, c := range creeps {
		if c.GroupKey == "DUP CO" {
			t.Fatalf("suppressed transactions must be excluded by Active(), leaving only 4 occurrences: %#v", c)
		}
	}
}
