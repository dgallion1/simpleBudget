package dataloader

import (
	"math"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/metrics"
	"budget2/internal/services/transfers"
)

// The two CSVs below are one Schwab -> USAA transfer, one Vanguard
// contribution whose receiving CSV is not imported, one dividend and one
// grocery run. Descriptions are chosen so the failure mode this task exists
// to end is visible: "TRANSFER IN FROM SCHWAB" hits classifier.IncomeKeywords
// ("transfer in"), so if the credit leg is ever re-classified it lands in
// Total Income, and the debit leg lands in Total Expenses.
const (
	schwabTransferCSV = `Date,Description,Category,Amount
2026-05-01,Dividend,Investing,12.50
2026-05-04,SCHWAB MONEYLINK TRANSFER,Transfer,-2000.00`

	usaaTransferCSV = `Date,Description,Category,Amount
2026-05-03,VANGUARD BUY INVESTMENT,Investing,-1500.00
2026-05-06,TRANSFER IN FROM SCHWAB,Deposit,2000.00
2026-05-07,Wegmans,Groceries,-84.12`
)

// The only non-transfer rows are the 12.50 dividend and the 84.12 grocery
// run, so these are the totals a correct load must produce. Every one of the
// three transfer rows is large enough that a single leaked leg moves a total
// by at least 1500 -- there is no rounding slack for a leak to hide in.
const (
	wantIncome   = 12.50
	wantExpenses = 84.12
)

// transferFixture loads the two CSVs above with an account per file.
func transferFixture(t *testing.T, extra map[string]string) (*DataLoader, *models.TransactionSet) {
	t.Helper()
	files := map[string]string{
		"schwab-brokerage-2026.csv": schwabTransferCSV,
		"usaa-checking-2026.csv":    usaaTransferCSV,
	}
	for name, content := range extra {
		files[name] = content
	}
	dir, loader, cleanup := setupTestDir(t, files)
	t.Cleanup(cleanup)

	writeAccounts(t, dir, []models.Account{
		{ID: "schwab", Name: "Schwab Brokerage", Kind: models.AccountKindBrokerage, FilePatterns: []string{"schwab-*.csv"}},
		{ID: "usaa", Name: "USAA Checking", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-*.csv"}},
	})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	return loader, ts
}

func findByDescription(t *testing.T, ts *models.TransactionSet, desc string) models.Transaction {
	t.Helper()
	var found []models.Transaction
	for _, txn := range ts.Transactions {
		if txn.Description == desc {
			found = append(found, txn)
		}
	}
	if len(found) != 1 {
		t.Fatalf("found %d rows described %q, want exactly 1", len(found), desc)
	}
	return found[0]
}

// TestLoadData_TransfersAreClassifiedNotDropped is the end-to-end contract:
// the legs of a real transfer are typed and linked, and every row the old
// filter would have deleted is still in the ledger.
func TestLoadData_TransfersAreClassifiedNotDropped(t *testing.T) {
	loader, ts := transferFixture(t, nil)

	if got := ts.Len(); got != 5 {
		t.Fatalf("ledger holds %d rows, want 5 -- classification must not drop anything", got)
	}

	debit := findByDescription(t, ts, "SCHWAB MONEYLINK TRANSFER")
	credit := findByDescription(t, ts, "TRANSFER IN FROM SCHWAB")
	external := findByDescription(t, ts, "VANGUARD BUY INVESTMENT")

	for _, leg := range []models.Transaction{debit, credit} {
		if leg.TransactionType != models.Transfer {
			t.Errorf("%q: type = %q, want %q", leg.Description, leg.TransactionType, models.Transfer)
		}
		if leg.TransferClass != transfers.ClassPaired {
			t.Errorf("%q: class = %q, want %q", leg.Description, leg.TransferClass, transfers.ClassPaired)
		}
	}
	if debit.TransferPairKey == "" || debit.TransferPairKey != credit.TransferPairKey {
		t.Errorf("legs must share one non-empty pair key, got %q and %q", debit.TransferPairKey, credit.TransferPairKey)
	}

	if external.TransactionType != models.Transfer || external.TransferClass != transfers.ClassExternal {
		t.Errorf("Vanguard row: type/class = %q/%q, want %q/%q",
			external.TransactionType, external.TransferClass, models.Transfer, transfers.ClassExternal)
	}
	if external.TransferPairKey != "" {
		t.Errorf("external leg carries pair key %q, want none", external.TransferPairKey)
	}

	// FilteredTransfers keeps its name and signature for the existing UI
	// but now counts classified rows rather than deleted ones.
	if got := loader.FilteredTransfers(); got != 3 {
		t.Errorf("FilteredTransfers() = %d, want 3 (2 paired + 1 external)", got)
	}

	// Nothing else moved: the ordinary rows keep their old classification.
	if got := findByDescription(t, ts, "Dividend"); got.TransactionType != models.Income {
		t.Errorf("Dividend: type = %q, want %q", got.TransactionType, models.Income)
	}
	if got := findByDescription(t, ts, "Wegmans"); got.TransactionType != models.Outflow {
		t.Errorf("Wegmans: type = %q, want %q", got.TransactionType, models.Outflow)
	}
}

// TestLoadData_MetricsExcludeEveryTransferRow is the requirement that makes
// the rest worth anything: the numbers the user reads must change. It asserts
// exact totals built from the two non-transfer rows only, so any leg leaking
// back into Income or Outflow -- 2000.00, 2000.00 and 1500.00 -- fails it by
// orders of magnitude rather than by a rounding wobble.
func TestLoadData_MetricsExcludeEveryTransferRow(t *testing.T) {
	_, ts := transferFixture(t, nil)

	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	m := metrics.Calculate(ts.Active().FilterByDateRange(start, end), start, end, 0, 0)

	if m.TransactionCount != 5 {
		t.Fatalf("TransactionCount = %d, want 5 -- transfers stay in the ledger", m.TransactionCount)
	}
	if math.Abs(m.TotalIncome-wantIncome) > 0.005 {
		t.Errorf("TotalIncome = %.2f, want %.2f (the 2000.00 credit leg must be excluded)", m.TotalIncome, wantIncome)
	}
	if math.Abs(m.TotalExpenses-wantExpenses) > 0.005 {
		t.Errorf("TotalExpenses = %.2f, want %.2f (the 2000.00 debit and 1500.00 external legs must be excluded)",
			m.TotalExpenses, wantExpenses)
	}
	if want := wantIncome - wantExpenses; math.Abs(m.NetSavings-want) > 0.005 {
		t.Errorf("NetSavings = %.2f, want %.2f", m.NetSavings, want)
	}

	// The same rows must be absent from the by-type sets the trends and
	// every other consumer are built on, not merely from the three totals.
	for _, tt := range []models.TransactionType{models.Income, models.Outflow} {
		for _, txn := range ts.FilterByType(tt).Transactions {
			if txn.TransferClass != "" {
				t.Errorf("%q leaked into the %s set", txn.Description, tt)
			}
		}
	}
	if got := ts.FilterByType(models.Transfer).Len(); got != 3 {
		t.Errorf("Transfer set holds %d rows, want 3", got)
	}
}

// TestLoadData_SameAccountEqualAmountDoesNotPair guards the rule that makes a
// transfer a transfer: it crosses accounts.
func TestLoadData_SameAccountEqualAmountDoesNotPair(t *testing.T) {
	oneAccount := `Date,Description,Category,Amount
2026-06-01,USAA FUNDS TRANSFER,Transfer,-750.00
2026-06-02,REFUND OF FUNDS,Adjustment,750.00`

	dir, loader, cleanup := setupTestDir(t, map[string]string{"usaa-checking-2026.csv": oneAccount})
	defer cleanup()
	writeAccounts(t, dir, []models.Account{
		{ID: "usaa", Name: "USAA Checking", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-*.csv"}},
	})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if ts.Len() != 2 {
		t.Fatalf("ledger holds %d rows, want 2", ts.Len())
	}
	for _, txn := range ts.Transactions {
		if txn.TransferClass == transfers.ClassPaired {
			t.Errorf("%q paired inside one account", txn.Description)
		}
	}
	if got := len(loader.SuspectedTransfers()); got != 0 {
		t.Errorf("SuspectedTransfers() = %d, want 0 -- same-account rows are not candidates", got)
	}
}

// coincidenceCSVs is a $60 debit in one account and a $60 credit in another
// three days later, with nothing in either description suggesting a transfer.
var coincidenceCSVs = map[string]string{
	"schwab-brokerage-2026.csv": schwabTransferCSV + "\n2026-05-12,ZELLE FROM PAT,Other,60.00",
	"usaa-checking-2026.csv":    usaaTransferCSV + "\n2026-05-11,TARGET STORE 1123,Shopping,-60.00",
}

func TestLoadData_PatternlessCoincidenceIsSuggestedNotPaired(t *testing.T) {
	loader, ts := transferFixture(t, coincidenceCSVs)

	if ts.Len() != 7 {
		t.Fatalf("ledger holds %d rows, want 7", ts.Len())
	}

	target := findByDescription(t, ts, "TARGET STORE 1123")
	zelle := findByDescription(t, ts, "ZELLE FROM PAT")
	for _, txn := range []models.Transaction{target, zelle} {
		if txn.TransactionType == models.Transfer {
			t.Errorf("%q was auto-paired on an amount coincidence", txn.Description)
		}
	}

	queue := loader.SuspectedTransfers()
	if len(queue) != 1 {
		t.Fatalf("SuspectedTransfers() = %d entries, want 1: %+v", len(queue), queue)
	}
	if queue[0].Reason != transfers.ReasonAmountMatch {
		t.Errorf("Reason = %q, want %q", queue[0].Reason, transfers.ReasonAmountMatch)
	}
	want := transfers.PairKeyFor(target.StableID, zelle.StableID)
	if queue[0].PairKey != want {
		t.Errorf("PairKey = %q, want %q", queue[0].PairKey, want)
	}
	// The real transfer above is unaffected by the coincidence.
	if got := loader.FilteredTransfers(); got != 3 {
		t.Errorf("FilteredTransfers() = %d, want 3", got)
	}
}

func TestResolveTransfer_ConfirmSurvivesReload(t *testing.T) {
	loader, _ := transferFixture(t, coincidenceCSVs)

	queue := loader.SuspectedTransfers()
	if len(queue) != 1 {
		t.Fatalf("SuspectedTransfers() = %d entries, want 1", len(queue))
	}
	pairKey := queue[0].PairKey

	if err := loader.ResolveTransfer(pairKey, transfers.VerdictConfirm); err != nil {
		t.Fatalf("ResolveTransfer: %v", err)
	}

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if ts.Len() != 7 {
		t.Fatalf("ledger holds %d rows after reload, want 7", ts.Len())
	}

	target := findByDescription(t, ts, "TARGET STORE 1123")
	zelle := findByDescription(t, ts, "ZELLE FROM PAT")
	for _, txn := range []models.Transaction{target, zelle} {
		if txn.TransactionType != models.Transfer {
			t.Errorf("%q: type = %q, want %q after a confirm", txn.Description, txn.TransactionType, models.Transfer)
		}
		if txn.TransferClass != transfers.ClassPaired {
			t.Errorf("%q: class = %q, want %q", txn.Description, txn.TransferClass, transfers.ClassPaired)
		}
		if txn.TransferPairKey != pairKey {
			t.Errorf("%q: pair key = %q, want %q", txn.Description, txn.TransferPairKey, pairKey)
		}
	}
	if got := len(loader.SuspectedTransfers()); got != 0 {
		t.Errorf("SuspectedTransfers() = %d after confirming, want 0", got)
	}
	if got := loader.FilteredTransfers(); got != 5 {
		t.Errorf("FilteredTransfers() = %d, want 5 (4 paired + 1 external)", got)
	}

	// And the confirmed pair now sits outside the totals too.
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	m := metrics.Calculate(ts.Active().FilterByDateRange(start, end), start, end, 0, 0)
	if math.Abs(m.TotalIncome-wantIncome) > 0.005 {
		t.Errorf("TotalIncome = %.2f, want %.2f", m.TotalIncome, wantIncome)
	}
	if math.Abs(m.TotalExpenses-wantExpenses) > 0.005 {
		t.Errorf("TotalExpenses = %.2f, want %.2f", m.TotalExpenses, wantExpenses)
	}
}

func TestResolveTransfer_RejectIsNeverSuggestedAgain(t *testing.T) {
	loader, _ := transferFixture(t, coincidenceCSVs)

	queue := loader.SuspectedTransfers()
	if len(queue) != 1 {
		t.Fatalf("SuspectedTransfers() = %d entries, want 1", len(queue))
	}

	if err := loader.ResolveTransfer(queue[0].PairKey, transfers.VerdictReject); err != nil {
		t.Fatalf("ResolveTransfer: %v", err)
	}

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := len(loader.SuspectedTransfers()); got != 0 {
		t.Fatalf("SuspectedTransfers() = %d after rejecting, want 0", got)
	}
	for _, desc := range []string{"TARGET STORE 1123", "ZELLE FROM PAT"} {
		if txn := findByDescription(t, ts, desc); txn.TransactionType == models.Transfer {
			t.Errorf("%q was typed Transfer despite a reject", desc)
		}
	}
	if got := loader.FilteredTransfers(); got != 3 {
		t.Errorf("FilteredTransfers() = %d, want 3 (the real transfer is unaffected)", got)
	}
}

func TestResolveTransfer_RejectsBadInput(t *testing.T) {
	loader, _ := transferFixture(t, coincidenceCSVs)

	queue := loader.SuspectedTransfers()
	if len(queue) != 1 {
		t.Fatalf("SuspectedTransfers() = %d entries, want 1", len(queue))
	}

	if err := loader.ResolveTransfer("", transfers.VerdictConfirm); err == nil {
		t.Error("empty pair key was accepted")
	}
	if err := loader.ResolveTransfer(queue[0].PairKey, transfers.Verdict("maybe")); err == nil {
		t.Error("unknown verdict was accepted")
	}
	if err := loader.ResolveTransfer("deadbeef1234", transfers.VerdictConfirm); err == nil {
		t.Error("a pair key from no known suspected pair was accepted")
	}
}

// TestLoadData_TransfersLeaveNearDuplicateDetection documents a consequence
// of the new type: near-duplicate detection indexes outflows only, so a
// classified transfer drops out of it automatically.
func TestLoadData_TransferIsNotANearDuplicateCandidate(t *testing.T) {
	_, ts := transferFixture(t, nil)

	for _, txn := range ts.Transactions {
		if txn.TransactionType == models.Transfer && txn.DuplicatePairKey != "" {
			t.Errorf("%q was flagged as a near-duplicate candidate", txn.Description)
		}
	}
}
