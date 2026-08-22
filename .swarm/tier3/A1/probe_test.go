package explorer

// probe_test.go — Tier 3 acceptance probe for A1 (StableID + sidecar
// migration), authored by the lead BEFORE dispatch and copied into each blind
// worktree by accept.sh. It is the oracle: it exercises the pinned API from
// the task brief and asserts behavior, so it must compile and pass against
// BOTH independent implementations.
//
// Per ruling 2026-08-16e, every loop-based assertion is preceded by a count
// assertion so it cannot pass vacuously on an empty set.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
)

func probeA1Dir(t *testing.T) (string, *storage.Storage) {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return dir, s
}

// Two rows identical in (date, amount) but differing in description, so they
// are distinct under the legacy Hash yet collide on the StableID triple
// (accountID, date, cents) and must be separated by the occurrence index.
const probeA1CollisionCSV = `Date,Description,Amount
2025-05-04,COFFEE ONE,-12.34
2025-05-04,COFFEE TWO,-12.34
2025-05-06,RENT,-900.00
`

// A 3-row all-positive card export: below the >=10-row heuristic, so only an
// explicit credit Kind flips it. Used to prove StableID uses the POST-flip
// amount.
const probeA1CardCSV = `Date,Description,Amount
2025-06-01,HARDWARE,25.00
2025-06-02,COFFEE,5.00
2025-06-03,FUEL,45.00
`

// TestProbeA1_StableIDFormat pins the identity string itself.
func TestProbeA1_StableIDFormat(t *testing.T) {
	d := time.Date(2025, 5, 4, 0, 0, 0, 0, time.UTC)
	got := models.StableIDFor("usaa-checking", d, -1234, 0)
	want := "usaa-checking|2025-05-04|-1234|0"
	if got != want {
		t.Errorf("StableIDFor = %q, want %q", got, want)
	}
	if second := models.StableIDFor("usaa-checking", d, -1234, 1); second == got {
		t.Error("occurrence index must change the StableID")
	}
	// Description is deliberately not part of the identity, so nothing about
	// the format may vary with it. Same inputs must be stable across calls.
	if again := models.StableIDFor("usaa-checking", d, -1234, 0); again != got {
		t.Errorf("StableIDFor is not deterministic: %q vs %q", again, got)
	}
}

// TestProbeA1_OccurrenceIndexSeparatesCollisions is the core of the scheme:
// two rows sharing (account, date, cents) get distinct StableIDs by file order.
func TestProbeA1_OccurrenceIndexSeparatesCollisions(t *testing.T) {
	dir, store := probeA1Dir(t)
	if err := os.WriteFile(filepath.Join(dir, "usaa-checking.csv"), []byte(probeA1CollisionCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Save(store, []models.Account{{
		ID: "usaa-checking", Name: "USAA Checking", Institution: "USAA",
		Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-check*.csv"},
	}}); err != nil {
		t.Fatal(err)
	}

	ts, err := dataloader.New(dir, store).LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 3 {
		t.Fatalf("fixture did not load: want 3 transactions, got %d", len(ts.Transactions))
	}

	seen := map[string]int{}
	for _, tx := range ts.Transactions {
		if tx.StableID == "" {
			t.Fatalf("transaction %q has no StableID", tx.Description)
		}
		seen[tx.StableID]++
	}
	if len(seen) != 3 {
		t.Errorf("want 3 distinct StableIDs, got %d: %v", len(seen), seen)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("StableID %q assigned to %d transactions; must be unique", id, n)
		}
	}
}

// TestProbeA1_UnassignedUsesFilePrefix: rows from a file matching no account
// fall back to file:<basename> in the accountID slot.
func TestProbeA1_UnassignedUsesFilePrefix(t *testing.T) {
	dir, store := probeA1Dir(t)
	if err := os.WriteFile(filepath.Join(dir, "mystery.csv"), []byte(probeA1CollisionCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	ts, err := dataloader.New(dir, store).LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 3 {
		t.Fatalf("fixture did not load: want 3 transactions, got %d", len(ts.Transactions))
	}
	for _, tx := range ts.Transactions {
		if !strings.HasPrefix(tx.StableID, "file:mystery.csv|") {
			t.Errorf("unassigned row StableID = %q, want prefix %q", tx.StableID, "file:mystery.csv|")
		}
	}
}

// TestProbeA1_StableIDUsesPostFlipAmount: the amount component must be the
// normalized one, so a credit-kind flip is reflected in the identity.
func TestProbeA1_StableIDUsesPostFlipAmount(t *testing.T) {
	dir, store := probeA1Dir(t)
	if err := os.WriteFile(filepath.Join(dir, "usaa-credit.csv"), []byte(probeA1CardCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Save(store, []models.Account{{
		ID: "usaa-credit", Name: "USAA Credit", Institution: "USAA",
		Kind: models.AccountKindCredit, FilePatterns: []string{"usaa-credit*.csv"},
	}}); err != nil {
		t.Fatal(err)
	}

	ts, err := dataloader.New(dir, store).LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 3 {
		t.Fatalf("fixture did not load: want 3 transactions, got %d", len(ts.Transactions))
	}
	for _, tx := range ts.Transactions {
		if tx.Amount > 0 {
			t.Fatalf("premise broken: credit-kind charge %q not flipped (%.2f)", tx.Description, tx.Amount)
		}
		cents := int64(tx.Amount*100 + copysignHalf(tx.Amount))
		want := models.StableIDFor(tx.AccountID, tx.Date, cents, 0)
		// Occurrence 0 is right here: all three rows differ in (date, amount).
		if tx.StableID != want {
			t.Errorf("StableID %q does not encode the post-flip amount; want %q",
				tx.StableID, want)
		}
	}
}

func copysignHalf(v float64) float64 {
	if v < 0 {
		return -0.5
	}
	return 0.5
}

// TestProbeA1_LegacyPinStillResolves: a pins file written under the OLD
// content hash must keep working after the migration.
func TestProbeA1_LegacyPinStillResolves(t *testing.T) {
	dir, store := probeA1Dir(t)
	if err := os.WriteFile(filepath.Join(dir, "usaa-checking.csv"), []byte(probeA1CollisionCSV), 0o644); err != nil {
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
	if len(ts.Transactions) != 3 {
		t.Fatalf("fixture did not load: want 3, got %d", len(ts.Transactions))
	}
	target := ts.Transactions[0]
	if target.Hash == "" {
		t.Fatal("premise broken: transaction has no legacy Hash")
	}

	// Write the sidecar the OLD way: keyed by content hash.
	legacy := map[string]string{target.Hash: "expense-legacy"}
	if err := dl.WriteTransactionPins(legacy); err != nil {
		t.Fatalf("WriteTransactionPins: %v", err)
	}

	dl2 := dataloader.New(dir, store)
	ts2, err := dl2.LoadData()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	var found bool
	for _, tx := range ts2.Transactions {
		if tx.Hash != target.Hash {
			continue
		}
		found = true
		got, ok := dl2.PinFor(tx)
		if !ok || got != "expense-legacy" {
			t.Errorf("legacy-hash pin did not resolve: PinFor = (%q, %v), want (%q, true)",
				got, ok, "expense-legacy")
		}
	}
	if !found {
		t.Fatal("target transaction vanished on reload")
	}
}

// TestProbeA1_PinRewritesToStableID: once resolved, the next save must rekey
// the sidecar to the StableID so the legacy dependency decays.
func TestProbeA1_PinRewritesToStableID(t *testing.T) {
	dir, store := probeA1Dir(t)
	if err := os.WriteFile(filepath.Join(dir, "usaa-checking.csv"), []byte(probeA1CollisionCSV), 0o644); err != nil {
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
	if len(ts.Transactions) != 3 {
		t.Fatalf("fixture did not load: want 3, got %d", len(ts.Transactions))
	}
	target := ts.Transactions[0]

	if err := dl.WriteTransactionPins(map[string]string{target.Hash: "expense-legacy"}); err != nil {
		t.Fatalf("WriteTransactionPins: %v", err)
	}

	// A normal pin write is the migration trigger.
	if err := dl.SetTransactionPin(target.StableID, "expense-new"); err != nil {
		t.Fatalf("SetTransactionPin: %v", err)
	}

	pins, err := dl.LoadTransactionPins()
	if err != nil {
		t.Fatalf("LoadTransactionPins: %v", err)
	}
	if len(pins) == 0 {
		t.Fatal("pins file is empty after a write")
	}
	if _, stillLegacy := pins[target.Hash]; stillLegacy {
		t.Errorf("pins file still keyed by legacy hash %q after save; want rekey to StableID",
			target.Hash)
	}
	if got := pins[target.StableID]; got != "expense-new" {
		t.Errorf("pins[StableID] = %q, want %q", got, "expense-new")
	}
}
