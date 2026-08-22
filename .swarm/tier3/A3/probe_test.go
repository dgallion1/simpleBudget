package explorer

// probe_test.go — Tier 3 acceptance probe for A3 (transfer classification),
// authored by the lead BEFORE dispatch and copied into each blind worktree by
// accept.sh.
//
// Ruling 2026-08-16f is the governing constraint here: it is NOT enough to
// assert that a leg is labelled Transfer. A1 taught us that a task can relabel
// stored data correctly while every existing CONSUMER of that data keeps
// reading it the old way, silently. So the central check below asserts on
// metrics.Calculate's income and expense totals — the numbers the user
// actually reads — not merely on the classification fields.
//
// Ruling 2026-08-16e: every loop-based assertion is preceded by a count
// assertion so it cannot pass vacuously.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/metrics"
	"budget2/internal/services/storage"
	"budget2/internal/services/transfers"
)

func probeA3Dir(t *testing.T) (string, *storage.Storage) {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return dir, s
}

func probeA3Accounts(t *testing.T, store *storage.Storage) {
	t.Helper()
	if err := accounts.Save(store, []models.Account{
		{ID: "schwab-checking", Name: "Schwab Checking", Institution: "Schwab",
			Kind: models.AccountKindChecking, FilePatterns: []string{"schwab-*.csv"}},
		{ID: "usaa-checking", Name: "USAA Checking", Institution: "USAA",
			Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-*.csv"}},
	}); err != nil {
		t.Fatal(err)
	}
}

// A clean transfer: matching amount, opposite signs, different accounts, two
// days apart, and the description hits InternalTransferPatterns
// ("usaa funds transfer"). Plus ordinary spending either side so the metrics
// assertion has a non-trivial baseline.
const probeA3SchwabCSV = `Date,Description,Amount
2025-03-01,GROCERY MARKET,-120.00
2025-03-10,USAA FUNDS TRANSFER,-2000.00
2025-03-20,PAYCHECK,5000.00
`

const probeA3UsaaCSV = `Date,Description,Amount
2025-03-12,USAA FUNDS TRANSFER,2000.00
2025-03-15,ELECTRIC BILL,-90.00
`

// Same amount on both sides within the window, DIFFERENT accounts, but NO
// pattern hit on either leg — a coincidence, not a transfer. Must never
// auto-pair.
const probeA3CoincidenceSchwabCSV = `Date,Description,Amount
2025-06-02,CAMERA STORE REFUND,60.00
`

const probeA3CoincidenceUsaaCSV = `Date,Description,Amount
2025-06-03,HARDWARE STORE,-60.00
`

// A pattern-matching leg whose counterparty account is not loaded at all
// (Vanguard). Must be classified Transfer/external, not dropped and not
// counted as spending.
const probeA3ExternalCSV = `Date,Description,Amount
2025-07-05,VANGUARD BUY INVESTMENT,-1500.00
2025-07-06,COFFEE,-4.00
`

func probeA3Write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestProbeA3_CleanPairAutoPairs: both legs become Transfer/paired and share a
// non-empty pair key.
func TestProbeA3_CleanPairAutoPairs(t *testing.T) {
	dir, store := probeA3Dir(t)
	probeA3Accounts(t, store)
	probeA3Write(t, dir, "schwab-checking.csv", probeA3SchwabCSV)
	probeA3Write(t, dir, "usaa-checking.csv", probeA3UsaaCSV)

	ts, err := dataloader.New(dir, store).LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 5 {
		t.Fatalf("fixture did not load: want 5 transactions, got %d", len(ts.Transactions))
	}

	var legs []models.Transaction
	for _, tx := range ts.Transactions {
		if tx.TransactionType == models.Transfer {
			legs = append(legs, tx)
		}
	}
	if len(legs) != 2 {
		t.Fatalf("want exactly 2 Transfer legs, got %d", len(legs))
	}
	for _, leg := range legs {
		if leg.TransferClass != "paired" {
			t.Errorf("leg %q TransferClass = %q, want \"paired\"", leg.Description, leg.TransferClass)
		}
		if leg.TransferPairKey == "" {
			t.Errorf("leg %q has no TransferPairKey", leg.Description)
		}
	}
	if legs[0].TransferPairKey != legs[1].TransferPairKey {
		t.Errorf("legs do not share a pair key: %q vs %q",
			legs[0].TransferPairKey, legs[1].TransferPairKey)
	}
	if legs[0].AccountID == legs[1].AccountID {
		t.Errorf("both legs on the same account %q; a transfer crosses accounts", legs[0].AccountID)
	}
}

// TestProbeA3_MetricsExcludeTransfers is THE consumer assertion (ruling
// 2026-08-16f). The numbers the user reads must not move because money was
// shuffled between their own accounts.
func TestProbeA3_MetricsExcludeTransfers(t *testing.T) {
	dir, store := probeA3Dir(t)
	probeA3Accounts(t, store)
	probeA3Write(t, dir, "schwab-checking.csv", probeA3SchwabCSV)
	probeA3Write(t, dir, "usaa-checking.csv", probeA3UsaaCSV)

	ts, err := dataloader.New(dir, store).LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 5 {
		t.Fatalf("fixture did not load: want 5 transactions, got %d", len(ts.Transactions))
	}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	m := metrics.Calculate(ts, start, end, 0, 0)

	// Expected from the NON-transfer rows only:
	//   income   = 5000.00 (paycheck)
	//   expenses = 120.00 + 90.00 = 210.00
	// If either transfer leg leaks in, income becomes 7000 or expenses 2210.
	const wantIncome, wantExpenses = 5000.00, 210.00
	if diff := m.TotalIncome - wantIncome; diff > 0.005 || diff < -0.005 {
		t.Errorf("TotalIncome = %.2f, want %.2f — the +2000 transfer leg leaked into income",
			m.TotalIncome, wantIncome)
	}
	if diff := m.TotalExpenses - wantExpenses; diff > 0.005 || diff < -0.005 {
		t.Errorf("TotalExpenses = %.2f, want %.2f — the -2000 transfer leg leaked into expenses",
			m.TotalExpenses, wantExpenses)
	}
	if diff := m.NetSavings - (wantIncome - wantExpenses); diff > 0.005 || diff < -0.005 {
		t.Errorf("NetSavings = %.2f, want %.2f", m.NetSavings, wantIncome-wantExpenses)
	}
}

// TestProbeA3_TransfersRemainVisible: the old behavior DROPPED matching rows.
// They must now survive in the ledger even though metrics ignore them.
func TestProbeA3_TransfersRemainVisible(t *testing.T) {
	dir, store := probeA3Dir(t)
	probeA3Accounts(t, store)
	probeA3Write(t, dir, "schwab-checking.csv", probeA3SchwabCSV)
	probeA3Write(t, dir, "usaa-checking.csv", probeA3UsaaCSV)

	ts, err := dataloader.New(dir, store).LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 5 {
		t.Fatalf("transfers were dropped: want all 5 rows present, got %d", len(ts.Transactions))
	}
}

// TestProbeA3_CoincidenceNeverAutoPairs: equal amounts, opposite signs,
// different accounts, inside the window, but no pattern hit. Must NOT be
// auto-paired — coincidentally equal amounts are common.
func TestProbeA3_CoincidenceNeverAutoPairs(t *testing.T) {
	dir, store := probeA3Dir(t)
	probeA3Accounts(t, store)
	probeA3Write(t, dir, "schwab-coincidence.csv", probeA3CoincidenceSchwabCSV)
	probeA3Write(t, dir, "usaa-coincidence.csv", probeA3CoincidenceUsaaCSV)

	ts, err := dataloader.New(dir, store).LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 2 {
		t.Fatalf("fixture did not load: want 2 transactions, got %d", len(ts.Transactions))
	}
	for _, tx := range ts.Transactions {
		if tx.TransactionType == models.Transfer {
			t.Errorf("%q was auto-paired with no pattern hit; coincidental equal amounts must go to review",
				tx.Description)
		}
	}
}

// TestProbeA3_CoincidenceIsSuggested: the same coincidence must still be
// surfaced for the user to confirm or reject.
func TestProbeA3_CoincidenceIsSuggested(t *testing.T) {
	dir, store := probeA3Dir(t)
	probeA3Accounts(t, store)
	probeA3Write(t, dir, "schwab-coincidence.csv", probeA3CoincidenceSchwabCSV)
	probeA3Write(t, dir, "usaa-coincidence.csv", probeA3CoincidenceUsaaCSV)

	dl := dataloader.New(dir, store)
	if _, err := dl.LoadData(); err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	sus := dl.SuspectedTransfers()
	if len(sus) != 1 {
		t.Fatalf("want 1 suspected pair, got %d", len(sus))
	}
}

// TestProbeA3_ExternalLegClassified: a pattern-matching leg with no loaded
// counterparty is Transfer/external, and is excluded from expenses.
func TestProbeA3_ExternalLegClassified(t *testing.T) {
	dir, store := probeA3Dir(t)
	probeA3Accounts(t, store)
	probeA3Write(t, dir, "schwab-external.csv", probeA3ExternalCSV)

	ts, err := dataloader.New(dir, store).LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 2 {
		t.Fatalf("fixture did not load: want 2 transactions, got %d", len(ts.Transactions))
	}

	var external int
	for _, tx := range ts.Transactions {
		if tx.TransactionType == models.Transfer {
			external++
			if tx.TransferClass != "external" {
				t.Errorf("%q TransferClass = %q, want \"external\"", tx.Description, tx.TransferClass)
			}
			if tx.TransferPairKey != "" {
				t.Errorf("%q is external but carries a pair key %q", tx.Description, tx.TransferPairKey)
			}
		}
	}
	if external != 1 {
		t.Fatalf("want 1 external transfer leg, got %d", external)
	}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	m := metrics.Calculate(ts, start, end, 0, 0)
	if diff := m.TotalExpenses - 4.00; diff > 0.005 || diff < -0.005 {
		t.Errorf("TotalExpenses = %.2f, want 4.00 — the external transfer leg leaked into expenses",
			m.TotalExpenses)
	}
}

// TestProbeA3_ConfirmDecisionPersists: confirming a suspected pair makes it
// paired, and the decision survives a reload.
func TestProbeA3_ConfirmDecisionPersists(t *testing.T) {
	dir, store := probeA3Dir(t)
	probeA3Accounts(t, store)
	probeA3Write(t, dir, "schwab-coincidence.csv", probeA3CoincidenceSchwabCSV)
	probeA3Write(t, dir, "usaa-coincidence.csv", probeA3CoincidenceUsaaCSV)

	dl := dataloader.New(dir, store)
	if _, err := dl.LoadData(); err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	sus := dl.SuspectedTransfers()
	if len(sus) != 1 {
		t.Fatalf("want 1 suspected pair, got %d", len(sus))
	}
	if err := dl.ResolveTransfer(sus[0].PairKey, transfers.VerdictConfirm); err != nil {
		t.Fatalf("ResolveTransfer: %v", err)
	}

	ts, err := dataloader.New(dir, store).LoadData()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(ts.Transactions) != 2 {
		t.Fatalf("reload lost rows: got %d", len(ts.Transactions))
	}
	var paired int
	for _, tx := range ts.Transactions {
		if tx.TransactionType == models.Transfer && tx.TransferClass == "paired" {
			paired++
		}
	}
	if paired != 2 {
		t.Errorf("after confirm, want 2 paired legs on reload, got %d", paired)
	}
}

// TestProbeA3_RejectDecisionIsNotResuggested: rejecting a suspected pair keeps
// it out of the queue on later loads.
func TestProbeA3_RejectDecisionIsNotResuggested(t *testing.T) {
	dir, store := probeA3Dir(t)
	probeA3Accounts(t, store)
	probeA3Write(t, dir, "schwab-coincidence.csv", probeA3CoincidenceSchwabCSV)
	probeA3Write(t, dir, "usaa-coincidence.csv", probeA3CoincidenceUsaaCSV)

	dl := dataloader.New(dir, store)
	if _, err := dl.LoadData(); err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	sus := dl.SuspectedTransfers()
	if len(sus) != 1 {
		t.Fatalf("want 1 suspected pair, got %d", len(sus))
	}
	if err := dl.ResolveTransfer(sus[0].PairKey, transfers.VerdictReject); err != nil {
		t.Fatalf("ResolveTransfer: %v", err)
	}

	dl2 := dataloader.New(dir, store)
	if _, err := dl2.LoadData(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if again := dl2.SuspectedTransfers(); len(again) != 0 {
		t.Errorf("rejected pair was re-suggested: %d still queued", len(again))
	}
}
