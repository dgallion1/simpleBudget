package dataloader

import (
	"os"
	"path/filepath"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/storage"
)

// writeAccounts persists an account set into a loader's data directory
// through the real store, so these tests exercise the same sidecar path the
// app uses rather than a hand-rolled fixture.
func writeAccounts(t *testing.T, dir string, accts []models.Account) {
	t.Helper()
	s, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	if err := accounts.Save(s, accts); err != nil {
		t.Fatalf("accounts.Save: %v", err)
	}
}

// smallCardCSV is four positive rows: too few for the sign-convention
// heuristic, which needs >=10. Without an account of kind credit the loader
// must leave it exactly as exported.
const smallCardCSV = `Date,Description,Category,Amount
2026-02-01,Wegmans,Groceries,50.00
2026-02-02,Walgreens,Pharmacy,15.00
2026-02-03,Netflix,Television,20.00
2026-02-04,Spotify,Music,10.00`

// TestLoadData_StampsAccountIDAndCountsUnassigned covers the attribution
// behaviour end to end: a matched file's rows carry the account's ID, an
// unmatched file still loads with an empty AccountID, and those rows are
// what UnassignedCount reports.
func TestLoadData_StampsAccountIDAndCountsUnassigned(t *testing.T) {
	matched := `Date,Description,Category,Amount
2026-03-01,Rochester Gas,Utilities,-100.00
2026-03-02,Monroe Water,Utilities,-25.00
2026-03-03,Internet Bill,Utilities,-80.00`
	unmatched := `Date,Description,Category,Amount
2026-03-04,Vanguard Statement Fee,Investing,-12.00
2026-03-05,Coinbase Fee,Investing,-3.00`

	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-2026.csv": matched,
		"vanguard-2026.csv":      unmatched,
	})
	defer cleanup()

	writeAccounts(t, dir, []models.Account{{
		ID:           "usaa-checking",
		Name:         "USAA Checking",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"usaa-checking*.csv"},
	}})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}

	byFile := map[string][]models.Transaction{}
	for _, txn := range ts.Transactions {
		byFile[txn.SourceFile] = append(byFile[txn.SourceFile], txn)
	}

	if got := len(byFile["usaa-checking-2026.csv"]); got != 3 {
		t.Fatalf("matched file contributed %d rows, want 3", got)
	}
	for _, txn := range byFile["usaa-checking-2026.csv"] {
		if txn.AccountID != "usaa-checking" {
			t.Errorf("%q: AccountID = %q, want %q", txn.Description, txn.AccountID, "usaa-checking")
		}
	}

	if got := len(byFile["vanguard-2026.csv"]); got != 2 {
		t.Fatalf("unmatched file contributed %d rows, want 2 (files are never dropped)", got)
	}
	for _, txn := range byFile["vanguard-2026.csv"] {
		if txn.AccountID != "" {
			t.Errorf("%q: AccountID = %q, want empty (file matches no account)", txn.Description, txn.AccountID)
		}
	}

	if got := loader.UnassignedCount(); got != 2 {
		t.Errorf("UnassignedCount() = %d, want 2", got)
	}
}

// TestUnassignedCount_AllAssignedAndNoAccountsConfigured checks the two ends
// of the range, including that the count is recomputed per load rather than
// left stale from a previous one.
func TestUnassignedCount_AllAssignedAndNoAccountsConfigured(t *testing.T) {
	csv := `Date,Description,Category,Amount
2026-03-01,Rochester Gas,Utilities,-100.00
2026-03-02,Monroe Water,Utilities,-25.00`

	dir, loader, cleanup := setupTestDir(t, map[string]string{"usaa-checking.csv": csv})
	defer cleanup()

	// No accounts.json at all: every row is unassigned, and a missing
	// sidecar is not an error.
	if _, err := loader.LoadData(); err != nil {
		t.Fatalf("LoadData with no accounts.json: %v", err)
	}
	if got := loader.UnassignedCount(); got != 2 {
		t.Fatalf("UnassignedCount() with no accounts configured = %d, want 2", got)
	}

	writeAccounts(t, dir, []models.Account{{
		ID:           "usaa-checking",
		Name:         "USAA Checking",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"usaa-checking*.csv"},
	}})

	if _, err := loader.LoadData(); err != nil {
		t.Fatalf("LoadData after configuring accounts: %v", err)
	}
	if got := loader.UnassignedCount(); got != 0 {
		t.Errorf("UnassignedCount() after every file is assigned = %d, want 0 (stale count from the previous load?)", got)
	}
}

func TestUnassignedCount_NoCSVFiles(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if _, err := loader.LoadData(); err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if got := loader.UnassignedCount(); got != 0 {
		t.Errorf("UnassignedCount() with no CSV files = %d, want 0", got)
	}
}

// TestCreditKind_ForcesFlipOnFileTooSmallForHeuristic is the core of the
// override: the same four-row file that the heuristic will never fire on
// (len < minSignConventionSample) is flipped when — and only when — its
// account is kind credit.
func TestCreditKind_ForcesFlipOnFileTooSmallForHeuristic(t *testing.T) {
	// Guard the premise. If this ever stops holding, the two cases below
	// would agree for the wrong reason and silently stop testing anything.
	dirPremise, loaderPremise, cleanupPremise := setupTestDir(t, map[string]string{"card.csv": smallCardCSV})
	defer cleanupPremise()
	raw, err := loaderPremise.loadCSVFile(filepath.Join(dirPremise, "card.csv"))
	if err != nil {
		t.Fatalf("loadCSVFile: %v", err)
	}
	if usesCreditCardSignConvention(raw) {
		t.Fatal("premise broken: the heuristic fires on this file, so the override proves nothing")
	}
	if raw[0].Amount != 50.00 {
		t.Fatalf("premise broken: unowned file was flipped, first amount = %v, want 50.00", raw[0].Amount)
	}

	tests := []struct {
		name       string
		kind       models.AccountKind
		wantAmount float64
	}{
		{"credit forces the flip", models.AccountKindCredit, -50.00},
		{"checking leaves the heuristic alone", models.AccountKindChecking, 50.00},
		{"savings leaves the heuristic alone", models.AccountKindSavings, 50.00},
		{"brokerage leaves the heuristic alone", models.AccountKindBrokerage, 50.00},
		{"other leaves the heuristic alone", models.AccountKindOther, 50.00},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, loader, cleanup := setupTestDir(t, map[string]string{"card.csv": smallCardCSV})
			defer cleanup()

			writeAccounts(t, dir, []models.Account{{
				ID:           "the-card",
				Name:         "The Card",
				Kind:         tt.kind,
				FilePatterns: []string{"card*.csv"},
			}})

			ts, err := loader.LoadData()
			if err != nil {
				t.Fatalf("LoadData: %v", err)
			}
			if len(ts.Transactions) != 4 {
				t.Fatalf("loaded %d rows, want 4", len(ts.Transactions))
			}

			var wegmans *models.Transaction
			for i := range ts.Transactions {
				if ts.Transactions[i].Description == "Wegmans" {
					wegmans = &ts.Transactions[i]
				}
				if ts.Transactions[i].AccountID != "the-card" {
					t.Errorf("%q: AccountID = %q, want %q",
						ts.Transactions[i].Description, ts.Transactions[i].AccountID, "the-card")
				}
			}
			if wegmans == nil {
				t.Fatal("Wegmans row missing")
			}
			if wegmans.Amount != tt.wantAmount {
				t.Errorf("kind %q: Wegmans amount = %v, want %v", tt.kind, wegmans.Amount, tt.wantAmount)
			}
		})
	}
}

// TestCreditKind_ForcedFlipRecomputesHash guards the identity consequence
// spelled out in GLOSSARY.md: the flip re-keys each row on the post-flip
// amount, so a forced flip must re-hash exactly like the heuristic one.
func TestCreditKind_ForcedFlipRecomputesHash(t *testing.T) {
	dir, loader, cleanup := setupTestDir(t, map[string]string{"card.csv": smallCardCSV})
	defer cleanup()

	acct := models.Account{
		ID:           "the-card",
		Name:         "The Card",
		Kind:         models.AccountKindCredit,
		FilePatterns: []string{"card*.csv"},
	}

	txns, err := loader.loadCSVFileForAccount(filepath.Join(dir, "card.csv"), &acct)
	if err != nil {
		t.Fatalf("loadCSVFileForAccount: %v", err)
	}
	if len(txns) != 4 {
		t.Fatalf("loaded %d rows, want 4", len(txns))
	}
	for i := range txns {
		if txns[i].Amount >= 0 {
			t.Errorf("row %d (%s): amount %v was not flipped", i, txns[i].Description, txns[i].Amount)
		}
		if want := txns[i].ComputeHash(); txns[i].Hash != want {
			t.Errorf("row %d (%s): Hash = %s, want %s — hash not recomputed after the forced flip",
				i, txns[i].Description, txns[i].Hash, want)
		}
	}
}

// TestCreditKind_DoesNotSuppressTheHeuristicForOtherKinds pins the "neither
// more nor less eager" half of the rule: a genuine CC-shaped file that the
// heuristic already flips keeps being flipped when it belongs to a
// non-credit account.
func TestCreditKind_DoesNotSuppressTheHeuristicForOtherKinds(t *testing.T) {
	ccCSV := `Date,Description,Category,Amount
2026-02-01,Wegmans,Groceries,50.00
2026-02-02,Amazon,Shopping,30.00
2026-02-03,Walgreens,Pharmacy,15.00
2026-02-04,Netflix,Television,20.00
2026-02-05,Bistro,Restaurants,40.00
2026-02-06,Spotify,Music,10.00
2026-02-07,Target,Shopping,25.00
2026-02-08,Wegmans,Groceries,60.00
2026-02-09,Lowes,Hardware,12.00
2026-02-10,Coffee,Food,5.00`

	dir, loader, cleanup := setupTestDir(t, map[string]string{"mystery.csv": ccCSV})
	defer cleanup()

	acct := models.Account{
		ID:           "mystery",
		Name:         "Mystery",
		Kind:         models.AccountKindOther,
		FilePatterns: []string{"mystery*.csv"},
	}
	txns, err := loader.loadCSVFileForAccount(filepath.Join(dir, "mystery.csv"), &acct)
	if err != nil {
		t.Fatalf("loadCSVFileForAccount: %v", err)
	}
	if txns[0].Amount != -50.00 {
		t.Errorf("first amount = %v, want -50.00 — the heuristic must still fire for a non-credit account",
			txns[0].Amount)
	}
	if txns[0].AccountID != "mystery" {
		t.Errorf("AccountID = %q, want %q", txns[0].AccountID, "mystery")
	}
}

// TestLoadCSVFile_UnownedFileLeavesAccountIDEmpty keeps the no-account path
// honest for the direct entry point the existing tests use.
func TestLoadCSVFile_UnownedFileLeavesAccountIDEmpty(t *testing.T) {
	dir, loader, cleanup := setupTestDir(t, map[string]string{"card.csv": smallCardCSV})
	defer cleanup()

	txns, err := loader.loadCSVFile(filepath.Join(dir, "card.csv"))
	if err != nil {
		t.Fatalf("loadCSVFile: %v", err)
	}
	for i := range txns {
		if txns[i].AccountID != "" {
			t.Errorf("row %d: AccountID = %q, want empty", i, txns[i].AccountID)
		}
	}
}

// TestLoadData_CorruptAccountsFileStillLoads: a broken sidecar degrades to
// "everything unassigned", it does not fail the load.
func TestLoadData_CorruptAccountsFileStillLoads(t *testing.T) {
	csv := `Date,Description,Category,Amount
2026-03-01,Rochester Gas,Utilities,-100.00
2026-03-02,Monroe Water,Utilities,-25.00`

	dir, loader, cleanup := setupTestDir(t, map[string]string{"usaa-checking.csv": csv})
	defer cleanup()

	if err := os.WriteFile(filepath.Join(dir, accounts.AccountsFile), []byte("{broken"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData with a corrupt accounts.json: %v", err)
	}
	if len(ts.Transactions) != 2 {
		t.Fatalf("loaded %d rows, want 2", len(ts.Transactions))
	}
	if got := loader.UnassignedCount(); got != 2 {
		t.Errorf("UnassignedCount() = %d, want 2", got)
	}
}
