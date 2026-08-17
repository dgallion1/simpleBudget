package explorer

// probe_test.go — Tier 3 acceptance probe for A2, authored by the lead BEFORE
// dispatch and copied into each blind worktree by accept.sh. It is the oracle:
// it exercises the pinned API from the task brief and asserts behavior, so it
// must compile and pass against BOTH independent implementations.
//
// Deliberately in package `explorer` only because that package already imports
// config/dataloader/storage and gives us a working test env; nothing here
// depends on explorer's own code.
//
// Output discipline: this file is run by accept.sh via `go test -run ProbeA2`,
// and accept.sh prints only its own CHECK lines. Never print timings or paths.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
)

func probeDataDir(t *testing.T) (string, *storage.Storage) {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return dir, s
}

// bank-convention file: outflows negative.
const probeBankCSV = `Date,Description,Amount
2025-01-05,GROCERY STORE,-52.10
2025-01-06,PAYCHECK,2000.00
2025-02-03,GROCERY STORE,-48.00
`

// A SECOND bank-convention file whose rows must not collide with
// probeBankCSV's. Transaction.Hash is sha256(date|lowercased description|
// amount) — it contains no file or account component — so two files with
// identical contents dedup down to one set and the attribution assertion
// below would be unsatisfiable. Distinct dates, descriptions and amounts keep
// both files' rows alive through the dedup stage.
const probeOtherBankCSV = `Date,Description,Amount
2025-03-11,HARDWARE DEPOT,-77.30
2025-03-12,DIVIDEND,15.00
2025-04-02,HARDWARE DEPOT,-19.95
`

// card-convention file: charges POSITIVE. Only 3 rows, so the >=10-row
// heuristic cannot fire; only an explicit credit Kind can flip it.
const probeCardCSV = `Date,Description,Amount
2025-01-07,HARDWARE STORE,31.25
2025-01-09,COFFEE,4.75
2025-01-11,FUEL,40.00
`

// TestProbeA2_AccountsStoreRoundTrip pins the sidecar store.
func TestProbeA2_AccountsStoreRoundTrip(t *testing.T) {
	dir, store := probeDataDir(t)
	_ = dir

	want := []models.Account{{
		ID:           "usaa-checking",
		Name:         "USAA Checking",
		Institution:  "USAA",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"usaa-check*.csv"},
		Anchors: []models.BalanceAnchor{
			{Date: time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC), Amount: 4210.00, Note: "statement"},
		},
	}}

	if err := accounts.Save(store, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := accounts.Load(store)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 account, got %d", len(got))
	}
	a := got[0]
	if a.ID != "usaa-checking" || a.Name != "USAA Checking" || a.Institution != "USAA" {
		t.Errorf("identity not round-tripped: %+v", a)
	}
	if a.Kind != models.AccountKindChecking {
		t.Errorf("kind not round-tripped: %q", a.Kind)
	}
	if len(a.FilePatterns) != 1 || a.FilePatterns[0] != "usaa-check*.csv" {
		t.Errorf("patterns not round-tripped: %v", a.FilePatterns)
	}
	if len(a.Anchors) != 1 || a.Anchors[0].Amount != 4210.00 {
		t.Errorf("anchors not round-tripped: %+v", a.Anchors)
	}
}

// TestProbeA2_LoadMissingAccountsFile: absent accounts.json is not an error.
func TestProbeA2_LoadMissingAccountsFile(t *testing.T) {
	_, store := probeDataDir(t)
	got, err := accounts.Load(store)
	if err != nil {
		t.Fatalf("Load on missing file must not error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty slice, got %d", len(got))
	}
}

// TestProbeA2_MatchFile pins attribution: first match wins by account ID sort
// order, and a non-matching file is unassigned.
func TestProbeA2_MatchFile(t *testing.T) {
	accts := []models.Account{
		{ID: "zeta", FilePatterns: []string{"*.csv"}},
		{ID: "alpha", FilePatterns: []string{"usaa-*.csv"}},
	}
	if got := accounts.MatchFile(accts, "usaa-checking.csv"); got != "alpha" {
		t.Errorf(`both patterns match "usaa-checking.csv"; want lowest ID "alpha", got %q`, got)
	}
	if got := accounts.MatchFile(accts, "random.csv"); got != "zeta" {
		t.Errorf(`want "zeta" via *.csv, got %q`, got)
	}
	if got := accounts.MatchFile(nil, "anything.csv"); got != "" {
		t.Errorf("no accounts must yield unassigned, got %q", got)
	}
	if got := accounts.MatchFile([]models.Account{{ID: "a", FilePatterns: []string{"nope-*.csv"}}}, "other.csv"); got != "" {
		t.Errorf("no pattern match must yield unassigned, got %q", got)
	}
}

// TestProbeA2_LoaderStampsAccountID is the core behavior: transactions carry
// the AccountID of the file they came from, and unmatched files are counted
// rather than dropped.
func TestProbeA2_LoaderStampsAccountID(t *testing.T) {
	dir, store := probeDataDir(t)
	if err := os.WriteFile(filepath.Join(dir, "usaa-checking.csv"), []byte(probeBankCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mystery.csv"), []byte(probeOtherBankCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Save(store, []models.Account{{
		ID: "usaa-checking", Name: "USAA Checking", Institution: "USAA",
		Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-check*.csv"},
	}}); err != nil {
		t.Fatal(err)
	}

	dl := dataloader.New(dir, store)
	ts, err := dl.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}

	var stamped, unassigned int
	for _, tx := range ts.Transactions {
		switch tx.AccountID {
		case "usaa-checking":
			stamped++
		case "":
			unassigned++
		default:
			t.Errorf("unexpected AccountID %q", tx.AccountID)
		}
	}
	if stamped == 0 {
		t.Error("no transaction was stamped with usaa-checking")
	}
	if unassigned == 0 {
		t.Error("mystery.csv rows should be present and unassigned, not dropped")
	}
	if n := dl.UnassignedCount(); n != unassigned {
		t.Errorf("UnassignedCount()=%d, want %d", n, unassigned)
	}
}

// TestProbeA2_CreditKindForcesSignFlip: a 3-row all-positive card export is
// below the >=10-row heuristic threshold, so only an explicit credit Kind can
// flip it. Charges must end up negative (bank convention).
func TestProbeA2_CreditKindForcesSignFlip(t *testing.T) {
	dir, store := probeDataDir(t)
	if err := os.WriteFile(filepath.Join(dir, "usaa-credit.csv"), []byte(probeCardCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Save(store, []models.Account{{
		ID: "usaa-credit", Name: "USAA Credit", Institution: "USAA",
		Kind: models.AccountKindCredit, FilePatterns: []string{"usaa-credit*.csv"},
	}}); err != nil {
		t.Fatal(err)
	}

	dl := dataloader.New(dir, store)
	ts, err := dl.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	// Guard against vacuous success: a loop over an empty set passes while
	// proving nothing. The fixture has 3 rows, so anything less means the
	// file did not load and the assertion below never ran.
	if len(ts.Transactions) != 3 {
		t.Fatalf("fixture did not load: want 3 transactions, got %d", len(ts.Transactions))
	}
	for _, tx := range ts.Transactions {
		if tx.Amount > 0 {
			t.Errorf("credit-kind file: charge %q stayed positive (%.2f); Kind must force the flip",
				tx.Description, tx.Amount)
		}
	}
}

// TestProbeA2_NonCreditKindLeavesHeuristicAlone is the guard against
// over-flipping: the same 3-row all-positive file under a checking account
// must NOT be flipped, because the heuristic does not fire below 10 rows.
func TestProbeA2_NonCreditKindLeavesHeuristicAlone(t *testing.T) {
	dir, store := probeDataDir(t)
	if err := os.WriteFile(filepath.Join(dir, "odd-checking.csv"), []byte(probeCardCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Save(store, []models.Account{{
		ID: "odd-checking", Name: "Odd Checking", Institution: "Bank",
		Kind: models.AccountKindChecking, FilePatterns: []string{"odd-check*.csv"},
	}}); err != nil {
		t.Fatal(err)
	}

	dl := dataloader.New(dir, store)
	ts, err := dl.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 3 {
		t.Fatalf("fixture did not load: want 3 transactions, got %d", len(ts.Transactions))
	}
	var positive int
	for _, tx := range ts.Transactions {
		if tx.Amount > 0 {
			positive++
		}
	}
	if positive == 0 {
		t.Error("checking-kind 3-row positive file must NOT be sign-flipped")
	}
}
