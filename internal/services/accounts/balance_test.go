package accounts

import (
	"math"
	"testing"
	"time"

	"budget2/internal/models"
)

// moneyEq compares two float64 amounts within the package's 1e-9 tolerance,
// matching the convention used across the repo's existing tests (see
// internal/services/retirement/guardrails_test.go). float64 amounts are
// summed in stored precision; rounding is for presentation only.
func moneyEq(a, b float64) bool {
	return math.Abs(a-b) < floatTolerance
}

func mustDate(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
}

// anchorOn is a shorthand for building a single-anchor account with a few
// transactions, keeping the per-test setup terse.
func anchorOn(id string, when time.Time, amount float64) models.Account {
	return models.Account{
		ID:   id,
		Name: id,
		Anchors: []models.BalanceAnchor{
			{Date: when, Amount: amount},
		},
	}
}

func tx(acct string, when time.Time, amount float64) models.Transaction {
	return models.Transaction{AccountID: acct, Date: when, Amount: amount}
}

// TestBalanceAt_SingleAnchorRollsForwardTransactionsAfterIt is the baseline:
// one anchor plus transactions strictly after it sum into the balance.
func TestBalanceAt_SingleAnchorRollsForwardTransactionsAfterIt(t *testing.T) {
	acct := anchorOn("usaa", mustDate(2026, 8, 1), 4210.00)
	txs := []models.Transaction{
		tx("usaa", mustDate(2026, 8, 2), 500.00),   // after anchor
		tx("usaa", mustDate(2026, 8, 10), -300.00), // after anchor
	}

	got, err := BalanceAt(acct, txs, mustDate(2026, 8, 15))
	if err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	if !got.Available {
		t.Fatal("Available = false, want true (there is an anchor at or before)")
	}
	if want := 4210.00 + 500.00 - 300.00; !moneyEq(got.Amount, want) {
		t.Errorf("Amount = %.4f, want %.4f", got.Amount, want)
	}
	if !got.AnchorDate.Equal(mustDate(2026, 8, 1)) {
		t.Errorf("AnchorDate = %v, want 2026-08-01", got.AnchorDate)
	}
}

// TestBalanceAt_LatestAtOrBeforeAnchorWins pins that when multiple anchors
// exist, the latest one on or before `at` is the one rolled forward.
func TestBalanceAt_LatestAtOrBeforeAnchorWins(t *testing.T) {
	acct := models.Account{
		ID:   "usaa",
		Name: "usaa",
		Anchors: []models.BalanceAnchor{
			{Date: mustDate(2026, 8, 1), Amount: 1000.00},
			{Date: mustDate(2026, 8, 15), Amount: 3000.00},
			{Date: mustDate(2026, 8, 30), Amount: 9999.00}, // after `at`, ignored
		},
	}
	txs := []models.Transaction{
		tx("usaa", mustDate(2026, 8, 16), 100.00), // after the chosen anchor, before `at`
		tx("usaa", mustDate(2026, 8, 20), -50.00), // after the chosen anchor, before `at`
	}

	got, err := BalanceAt(acct, txs, mustDate(2026, 8, 20))
	if err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	// Latest anchor at or before 8/20 is the 8/15 one (3000.00); the 8/30
	// anchor is after `at` and must NOT be used.
	if !got.Available {
		t.Fatal("Available = false, want true")
	}
	if !got.AnchorDate.Equal(mustDate(2026, 8, 15)) {
		t.Errorf("AnchorDate = %v, want 2026-08-15", got.AnchorDate)
	}
	if want := 3000.00 + 100.00 - 50.00; !moneyEq(got.Amount, want) {
		t.Errorf("Amount = %.4f, want %.4f", got.Amount, want)
	}
}

// TestBalanceAt_AnchorDayTransactionIsExcluded pins the end-of-day anchor
// boundary, which GLOSSARY.md calls "the single easiest thing to get wrong".
// A transaction dated on the anchor's own day is already reflected in the
// anchor's Amount and MUST NOT be added again. If the boundary were off by
// one day (e.g. comparing full timestamps, or using >= instead of >), this
// test fails: the same-day transaction would double-count.
func TestBalanceAt_AnchorDayTransactionIsExcluded(t *testing.T) {
	acct := anchorOn("usaa", mustDate(2026, 8, 1), 4210.00)
	txs := []models.Transaction{
		// Same calendar day as the anchor. The anchor already reflects the
		// end-of-day balance, so this row must be excluded. Including it
		// would report 4710.00, which is the bug this test guards against.
		tx("usaa", mustDate(2026, 8, 1), 500.00),
		// The day after the anchor is the first day that should be added.
		tx("usaa", mustDate(2026, 8, 2), 100.00),
	}

	got, err := BalanceAt(acct, txs, mustDate(2026, 8, 2))
	if err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	// Want 4210 + 100 = 4310, NOT 4210 + 500 + 100 = 4810.
	if want := 4310.00; !moneyEq(got.Amount, want) {
		t.Errorf("Amount = %.4f, want %.4f (same-day tx must be excluded from the anchor)", got.Amount, want)
	}

	// Off-by-one guard: if the same-day transaction were wrongly included
	// (e.g. full-timestamp comparison or a >= boundary), the amount would
	// be 4810. Asserting the exact expected value above already catches
	// that, but make the intent explicit so a future reader sees the trap.
	if moneyEq(got.Amount, 4810.00) {
		t.Error("Amount matched the off-by-one value 4810.00; the anchor-day transaction was double-counted")
	}
}

// TestBalanceAt_AnchorDayTransactionExcludedEvenWithTimeComponent ensures the
// day-granularity comparison holds when the transaction carries a time at
// or after the anchor's stored time. An anchor is end-of-day regardless of
// the time component its Date field happens to carry, so a same-day
// transaction at 23:59 is still excluded. A full-timestamp comparison would
// include it and fail this test.
func TestBalanceAt_AnchorDayTransactionExcludedEvenWithTimeComponent(t *testing.T) {
	anchorDate := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	acct := models.Account{
		ID:   "usaa",
		Name: "usaa",
		Anchors: []models.BalanceAnchor{
			{Date: anchorDate, Amount: 1000.00},
		},
	}
	txs := []models.Transaction{
		// Same calendar day, later in the day than the anchor's stored time.
		// End-of-day semantics say the anchor already reflects this row.
		{AccountID: "usaa", Date: time.Date(2026, 8, 1, 23, 59, 0, 0, time.UTC), Amount: 250.00},
		{AccountID: "usaa", Date: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), Amount: 100.00},
	}

	got, err := BalanceAt(acct, txs, mustDate(2026, 8, 2))
	if err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	if want := 1100.00; !moneyEq(got.Amount, want) {
		t.Errorf("Amount = %.4f, want %.4f (same-day 23:59 tx must be excluded from the anchor)", got.Amount, want)
	}
}

// TestBalanceAt_TransactionsAfterAtAreExcluded ensures transactions dated
// after the requested `at` day do not contribute to the balance.
func TestBalanceAt_TransactionsAfterAtAreExcluded(t *testing.T) {
	acct := anchorOn("usaa", mustDate(2026, 8, 1), 1000.00)
	txs := []models.Transaction{
		tx("usaa", mustDate(2026, 8, 2), 100.00), // included (on/before `at`)
		tx("usaa", mustDate(2026, 8, 4), 500.00), // excluded (after `at`)
		tx("usaa", mustDate(2026, 8, 5), 50.00),  // excluded (after `at`)
	}

	got, err := BalanceAt(acct, txs, mustDate(2026, 8, 3))
	if err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	if want := 1100.00; !moneyEq(got.Amount, want) {
		t.Errorf("Amount = %.4f, want %.4f (only txs on/before `at` count)", got.Amount, want)
	}
}

// TestBalanceAt_OtherAccountsAndUnassignedExcluded verifies that only
// transactions with tx.AccountID == acct.ID contribute. Other accounts' rows
// and unassigned rows (AccountID == "") are never included.
func TestBalanceAt_OtherAccountsAndUnassignedExcluded(t *testing.T) {
	acct := anchorOn("usaa", mustDate(2026, 8, 1), 1000.00)
	txs := []models.Transaction{
		tx("usaa", mustDate(2026, 8, 2), 100.00),   // ours
		tx("schwab", mustDate(2026, 8, 2), 500.00), // someone else's
		tx("", mustDate(2026, 8, 2), 250.00),       // unassigned
	}

	got, err := BalanceAt(acct, txs, mustDate(2026, 8, 3))
	if err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	if want := 1100.00; !moneyEq(got.Amount, want) {
		t.Errorf("Amount = %.4f, want %.4f (only this account's txs count)", got.Amount, want)
	}
}

// TestBalanceAt_NoAnchorReturnsUnavailableNotZero is the contract from
// GLOSSARY.md: with no anchor at or before `at`, the balance is
// unavailable -- NOT zero. The UI shows "set an anchor", not $0, because a
// zero balance and an unknown balance are different facts.
func TestBalanceAt_NoAnchorReturnsUnavailableNotZero(t *testing.T) {
	acct := models.Account{ID: "usaa", Name: "usaa"} // no anchors
	txs := []models.Transaction{
		tx("usaa", mustDate(2026, 8, 2), 100.00),
	}

	got, err := BalanceAt(acct, txs, mustDate(2026, 8, 3))
	if err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	if got.Available {
		t.Error("Available = true, want false (no anchor at or before `at`)")
	}
	if got.Amount != 0 {
		t.Errorf("Amount = %.4f, want 0 (unavailable balance carries no amount)", got.Amount)
	}
	if !got.AnchorDate.IsZero() {
		t.Errorf("AnchorDate = %v, want zero Time when unavailable", got.AnchorDate)
	}
}

// TestBalanceAt_AnchorOnlyFutureOfAtIsUnavailable: an anchor exists but all
// anchors are after `at`. The balance at `at` is still unavailable.
func TestBalanceAt_AnchorOnlyFutureOfAtIsUnavailable(t *testing.T) {
	acct := anchorOn("usaa", mustDate(2026, 9, 1), 1000.00) // after `at`
	txs := []models.Transaction{
		tx("usaa", mustDate(2026, 8, 2), 100.00),
	}

	got, err := BalanceAt(acct, txs, mustDate(2026, 8, 3))
	if err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	if got.Available {
		t.Error("Available = true, want false (the only anchor is after `at`)")
	}
}

// TestBalanceAt_AnchorExactlyOnAtIsIncluded: an anchor dated the same day as
// `at` is "at or before" and is the one rolled forward (transactions on that
// same day are excluded per the end-of-day rule).
func TestBalanceAt_AnchorExactlyOnAtIsIncluded(t *testing.T) {
	acct := anchorOn("usaa", mustDate(2026, 8, 5), 2000.00)
	txs := []models.Transaction{
		// Same day as both anchor and `at`; excluded (anchor reflects it).
		tx("usaa", mustDate(2026, 8, 5), 777.00),
	}

	got, err := BalanceAt(acct, txs, mustDate(2026, 8, 5))
	if err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	if !got.Available {
		t.Fatal("Available = false, want true (anchor is exactly on `at`)")
	}
	if !moneyEq(got.Amount, 2000.00) {
		t.Errorf("Amount = %.4f, want 2000.00 (same-day tx excluded, anchor stands)", got.Amount)
	}
}

// TestFreshness_WithTransactions returns the latest transaction date for the
// account and true.
func TestFreshness_WithTransactions(t *testing.T) {
	acct := models.Account{ID: "usaa", Name: "usaa"}
	txs := []models.Transaction{
		tx("usaa", mustDate(2026, 8, 2), 100.00),
		tx("usaa", mustDate(2026, 8, 12), 50.00),
		tx("schwab", mustDate(2026, 8, 20), 500.00), // other account, ignored
		tx("usaa", mustDate(2026, 8, 9), 25.00),
	}

	latest, ok := Freshness(acct, txs)
	if !ok {
		t.Fatal("Freshness ok = false, want true (account has transactions)")
	}
	if !latest.Equal(mustDate(2026, 8, 12)) {
		t.Errorf("latest = %v, want 2026-08-12", latest)
	}
}

// TestFreshness_WithoutTransactions returns the zero Time and false when no
// transaction belongs to the account.
func TestFreshness_WithoutTransactions(t *testing.T) {
	acct := models.Account{ID: "usaa", Name: "usaa"}
	txs := []models.Transaction{
		tx("schwab", mustDate(2026, 8, 12), 500.00), // other account only
	}

	latest, ok := Freshness(acct, txs)
	if ok {
		t.Error("Freshness ok = true, want false (no transactions for this account)")
	}
	if !latest.IsZero() {
		t.Errorf("latest = %v, want zero Time", latest)
	}
}

// TestFreshness_EmptyTransactionSlice covers the degenerate input.
func TestFreshness_EmptyTransactionSlice(t *testing.T) {
	acct := models.Account{ID: "usaa", Name: "usaa"}
	latest, ok := Freshness(acct, nil)
	if ok {
		t.Error("Freshness ok = true, want false on empty input")
	}
	if !latest.IsZero() {
		t.Errorf("latest = %v, want zero Time", latest)
	}
}

// TestDrift_MissingRowsBetweenAnchors: rolling the earlier anchor forward
// does not match the later anchor when transactions are missing between the
// exports. The difference is Actual - Predicted and is non-zero.
//
// Window note (the end-of-day rule, applied to drift): the roll-forward spans
// (earlierAnchorDay, laterAnchorDay] -- strictly after the earlier anchor's
// day (the earlier anchor already reflects its own day) AND on or before the
// later anchor's day (the later anchor is end-of-day, so it already reflects
// its own day's transactions, and the prediction must include them to be
// comparable). So a transaction dated on the LATER anchor's own day IS
// included in the prediction; only a transaction on the EARLIER anchor's day
// is excluded.
func TestDrift_MissingRowsBetweenAnchors(t *testing.T) {
	acct := models.Account{
		ID:   "usaa",
		Name: "usaa",
		Anchors: []models.BalanceAnchor{
			{Date: mustDate(2026, 8, 1), Amount: 1000.00},
			{Date: mustDate(2026, 8, 15), Amount: 1500.00}, // actual later balance
		},
	}
	// Recorded transactions after 8/1 through 8/15 sum to 100 + 200 = 300.
	// The later anchor states 1500, i.e. a +500 change; only 300 is
	// explained by recorded rows, so 200 is missing.
	txs := []models.Transaction{
		tx("usaa", mustDate(2026, 8, 2), 100.00),
		// A row on the LATER anchor's own day: INCLUDED (the later anchor
		// is end-of-day and already reflects it; the prediction must too).
		tx("usaa", mustDate(2026, 8, 15), 200.00),
		// A row on the EARLIER anchor's own day: EXCLUDED (earlier anchor
		// already reflects it). Including it would wrongly inflate the
		// prediction by 50.
		tx("usaa", mustDate(2026, 8, 1), 50.00),
	}

	reports, err := Drift(acct, txs)
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	r := reports[0]
	if !r.From.Equal(mustDate(2026, 8, 1)) || !r.To.Equal(mustDate(2026, 8, 15)) {
		t.Errorf("From/To = %v/%v, want 2026-08-01/2026-08-15", r.From, r.To)
	}
	// Predicted = 1000 + 100 + 200 = 1300 (the 8/1 same-day tx is excluded).
	if !moneyEq(r.Predicted, 1300.00) {
		t.Errorf("Predicted = %.4f, want 1300.00 (1000 + 100 + 200; 8/1 tx excluded, 8/15 tx included)", r.Predicted)
	}
	if !moneyEq(r.Actual, 1500.00) {
		t.Errorf("Actual = %.4f, want 1500.00", r.Actual)
	}
	if !moneyEq(r.Difference, 200.00) {
		t.Errorf("Difference = %.4f, want 200.00 (Actual - Predicted)", r.Difference)
	}
}

// TestDrift_ZeroDriftWhenTransactionsComplete: when the transactions between
// two anchors fully explain the change, the difference is zero.
func TestDrift_ZeroDriftWhenTransactionsComplete(t *testing.T) {
	acct := models.Account{
		ID:   "usaa",
		Name: "usaa",
		Anchors: []models.BalanceAnchor{
			{Date: mustDate(2026, 8, 1), Amount: 1000.00},
			{Date: mustDate(2026, 8, 15), Amount: 1100.00}, // = 1000 + 100 - 0
		},
	}
	txs := []models.Transaction{
		tx("usaa", mustDate(2026, 8, 2), 100.00),
		tx("usaa", mustDate(2026, 8, 14), 0.00), // zero amount, no effect
	}

	reports, err := Drift(acct, txs)
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	r := reports[0]
	if !moneyEq(r.Predicted, 1100.00) {
		t.Errorf("Predicted = %.4f, want 1100.00", r.Predicted)
	}
	if !moneyEq(r.Actual, 1100.00) {
		t.Errorf("Actual = %.4f, want 1100.00", r.Actual)
	}
	if !moneyEq(r.Difference, 0.00) {
		t.Errorf("Difference = %.4f, want 0.00", r.Difference)
	}
}

// TestDrift_FewerThanTwoAnchorsProducesNoReports: with zero or one anchor
// there is no consecutive pair to compare, so Drift returns no reports.
func TestDrift_FewerThanTwoAnchorsProducesNoReports(t *testing.T) {
	t.Run("zero anchors", func(t *testing.T) {
		acct := models.Account{ID: "usaa", Name: "usaa"}
		reports, err := Drift(acct, nil)
		if err != nil {
			t.Fatalf("Drift: %v", err)
		}
		if len(reports) != 0 {
			t.Errorf("got %d reports, want 0", len(reports))
		}
	})
	t.Run("one anchor", func(t *testing.T) {
		acct := anchorOn("usaa", mustDate(2026, 8, 1), 1000.00)
		txs := []models.Transaction{tx("usaa", mustDate(2026, 8, 2), 100.00)}
		reports, err := Drift(acct, txs)
		if err != nil {
			t.Fatalf("Drift: %v", err)
		}
		if len(reports) != 0 {
			t.Errorf("got %d reports, want 0", len(reports))
		}
	})
}

// TestDrift_ThreeAnchorsProducesTwoReports: one report per consecutive
// pair, in order. Also verifies Drift ignores other accounts' transactions.
func TestDrift_ThreeAnchorsProducesTwoReports(t *testing.T) {
	acct := models.Account{
		ID:   "usaa",
		Name: "usaa",
		Anchors: []models.BalanceAnchor{
			{Date: mustDate(2026, 8, 1), Amount: 1000.00},
			{Date: mustDate(2026, 8, 10), Amount: 1200.00},
			{Date: mustDate(2026, 8, 20), Amount: 900.00},
		},
	}
	txs := []models.Transaction{
		tx("usaa", mustDate(2026, 8, 3), 200.00),   // in pair 1 window (8/1, 8/10]
		tx("schwab", mustDate(2026, 8, 5), 500.00), // other account, ignored
		tx("usaa", mustDate(2026, 8, 12), -300.00), // in pair 2 window (8/10, 8/20]
		tx("usaa", mustDate(2026, 8, 10), 0.00),    // same day as the LATER anchor of pair 1; INCLUDED (zero effect)
	}

	reports, err := Drift(acct, txs)
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want 2", len(reports))
	}

	// Pair 1: 1000 -> 1200. Window is (8/1, 8/10]. Recorded rows: 200 + 0
	// = 200. Predicted = 1200, diff 0.
	if !moneyEq(reports[0].Predicted, 1200.00) {
		t.Errorf("pair 1 Predicted = %.4f, want 1200.00", reports[0].Predicted)
	}
	if !moneyEq(reports[0].Actual, 1200.00) {
		t.Errorf("pair 1 Actual = %.4f, want 1200.00", reports[0].Actual)
	}
	if !moneyEq(reports[0].Difference, 0.00) {
		t.Errorf("pair 1 Difference = %.4f, want 0.00", reports[0].Difference)
	}

	// Pair 2: 1200 -> 900. Window is (8/10, 8/20]. Recorded rows: -300.
	// Predicted = 1200 - 300 = 900, diff 0.
	if !moneyEq(reports[1].Predicted, 900.00) {
		t.Errorf("pair 2 Predicted = %.4f, want 900.00", reports[1].Predicted)
	}
	if !moneyEq(reports[1].Actual, 900.00) {
		t.Errorf("pair 2 Actual = %.4f, want 900.00", reports[1].Actual)
	}
	if !moneyEq(reports[1].Difference, 0.00) {
		t.Errorf("pair 2 Difference = %.4f, want 0.00", reports[1].Difference)
	}
}

// TestDrift_UnsortedAnchorsProducesSortedReports: anchors are not assumed to
// be stored sorted; Drift must sort them before pairing.
func TestDrift_UnsortedAnchorsProducesSortedReports(t *testing.T) {
	acct := models.Account{
		ID:   "usaa",
		Name: "usaa",
		Anchors: []models.BalanceAnchor{
			{Date: mustDate(2026, 8, 15), Amount: 1100.00},
			{Date: mustDate(2026, 8, 1), Amount: 1000.00}, // earlier, stored second
		},
	}
	txs := []models.Transaction{
		tx("usaa", mustDate(2026, 8, 3), 100.00),
	}

	reports, err := Drift(acct, txs)
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if !reports[0].From.Equal(mustDate(2026, 8, 1)) || !reports[0].To.Equal(mustDate(2026, 8, 15)) {
		t.Errorf("From/To = %v/%v, want 2026-08-01/2026-08-15 (anchors must be sorted)", reports[0].From, reports[0].To)
	}
	if !moneyEq(reports[0].Predicted, 1100.00) {
		t.Errorf("Predicted = %.4f, want 1100.00", reports[0].Predicted)
	}
}

// TestBalanceAt_DoesNotMutateAnchors guards that BalanceAt does not reorder
// the caller's account anchors slice (it scans a copy internally for sorting
// only in Drift; BalanceAt scans in place but never sorts the caller's slice).
func TestBalanceAt_DoesNotMutateAnchors(t *testing.T) {
	acct := models.Account{
		ID:   "usaa",
		Name: "usaa",
		Anchors: []models.BalanceAnchor{
			{Date: mustDate(2026, 8, 15), Amount: 3000.00},
			{Date: mustDate(2026, 8, 1), Amount: 1000.00}, // out of order
		},
	}
	before := append([]models.BalanceAnchor(nil), acct.Anchors...)
	if _, err := BalanceAt(acct, nil, mustDate(2026, 8, 20)); err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	for i := range before {
		if !acct.Anchors[i].Date.Equal(before[i].Date) || acct.Anchors[i].Amount != before[i].Amount {
			t.Errorf("BalanceAt mutated anchor %d: got %+v, want %+v", i, acct.Anchors[i], before[i])
		}
	}
}
