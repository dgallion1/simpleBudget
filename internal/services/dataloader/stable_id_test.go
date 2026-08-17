package dataloader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"budget2/internal/models"
)

// byDescription indexes a loaded set for assertions that must not depend on
// slice position. It fails the test if two rows share a description, so a
// lookup can never silently pick the wrong row.
func byDescription(t *testing.T, txns []models.Transaction) map[string]models.Transaction {
	t.Helper()
	out := make(map[string]models.Transaction, len(txns))
	for _, txn := range txns {
		if _, dup := out[txn.Description]; dup {
			t.Fatalf("fixture has two rows described %q; index would be ambiguous", txn.Description)
		}
		out[txn.Description] = txn
	}
	return out
}

// TestLoadData_AssignsStableIDs covers the assignment rule end to end: an
// assigned row's ID carries its AccountID, and two rows that agree on
// (account, date, cents) get distinct occurrence indices in file order --
// the only thing left distinguishing them once the description is out of the
// identity.
func TestLoadData_AssignsStableIDs(t *testing.T) {
	csv := `Date,Description,Category,Amount
2025-05-04,Corner Coffee,Dining,-12.34
2025-05-04,Corner Store,Groceries,-12.34
2025-05-05,Rochester Gas,Utilities,-100.00`

	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-2025.csv": csv,
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
	if len(ts.Transactions) != 3 {
		t.Fatalf("loaded %d rows, want 3", len(ts.Transactions))
	}

	rows := byDescription(t, ts.Transactions)
	want := map[string]string{
		"Corner Coffee": "usaa-checking|2025-05-04|-1234|0",
		"Corner Store":  "usaa-checking|2025-05-04|-1234|1",
		"Rochester Gas": "usaa-checking|2025-05-05|-10000|0",
	}
	if len(want) != 3 {
		t.Fatalf("expectation table has %d entries, want 3", len(want))
	}
	for desc, wantID := range want {
		got, ok := rows[desc]
		if !ok {
			t.Fatalf("row %q missing from the loaded set", desc)
		}
		if got.StableID != wantID {
			t.Errorf("%q: StableID = %q, want %q", desc, got.StableID, wantID)
		}
	}
}

// TestLoadData_UnassignedRowsUseFilePrefix pins the fallback slot for a file
// that matches no account. Usable, but not durable across a rename -- which
// is exactly why it is spelled out rather than left empty.
func TestLoadData_UnassignedRowsUseFilePrefix(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"mystery.csv": `Date,Description,Category,Amount
2026-01-02,Unknown Vendor,Misc,-5.00`,
	})
	defer cleanup()

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 1 {
		t.Fatalf("loaded %d rows, want 1", len(ts.Transactions))
	}
	got := ts.Transactions[0]
	if got.AccountID != "" {
		t.Fatalf("AccountID = %q, want empty (the fixture matches no account)", got.AccountID)
	}
	if want := "file:mystery.csv|2026-01-02|-500|0"; got.StableID != want {
		t.Errorf("StableID = %q, want %q", got.StableID, want)
	}
}

// TestLoadData_StableIDUsesPostFlipAmount is the reason StableID assignment
// sits after the sign decision. The fixture is four positive rows -- too few
// for the >=10-row heuristic to fire -- on a credit-kind account, which forces
// the flip. The identity must encode the flipped (negative) cents, because
// that is the amount every other part of the app works with.
func TestLoadData_StableIDUsesPostFlipAmount(t *testing.T) {
	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"visa-2026.csv": smallCardCSV,
	})
	defer cleanup()

	writeAccounts(t, dir, []models.Account{{
		ID:           "visa",
		Name:         "Visa",
		Kind:         models.AccountKindCredit,
		FilePatterns: []string{"visa*.csv"},
	}})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 4 {
		t.Fatalf("loaded %d rows, want 4", len(ts.Transactions))
	}

	rows := byDescription(t, ts.Transactions)
	wegmans, ok := rows["Wegmans"]
	if !ok {
		t.Fatal("row \"Wegmans\" missing from the loaded set")
	}
	if wegmans.Amount != -50 {
		t.Fatalf("Amount = %v, want -50 (credit kind forces the flip)", wegmans.Amount)
	}
	if want := "visa|2026-02-01|-5000|0"; wegmans.StableID != want {
		t.Errorf("StableID = %q, want %q (post-flip cents)", wegmans.StableID, want)
	}
}

// pinFixture loads a two-row assigned file with one major expense whose
// keywords match nothing, so the only way a row can acquire a
// MajorExpenseName is an explicit pin.
func pinFixture(t *testing.T) (*DataLoader, []models.Transaction, func()) {
	t.Helper()
	defs := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{ID: "me-vet", Name: "Vet Bills", Keywords: []string{"zzz-matches-nothing"}},
	}}
	defsJSON, err := json.Marshal(defs)
	if err != nil {
		t.Fatalf("marshal defs: %v", err)
	}

	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-2025.csv": `Date,Description,Category,Amount
2025-05-04,Animal Hospital,Pets,-240.00
2025-05-06,Corner Coffee,Dining,-12.34`,
		"major_expenses.json": string(defsJSON),
	})

	writeAccounts(t, dir, []models.Account{{
		ID:           "usaa-checking",
		Name:         "USAA Checking",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"usaa-checking*.csv"},
	}})

	ts, err := loader.LoadData()
	if err != nil {
		cleanup()
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 2 {
		cleanup()
		t.Fatalf("loaded %d rows, want 2", len(ts.Transactions))
	}
	return loader, ts.Transactions, cleanup
}

// TestPinFor_LegacyHashSidecarStillResolves is the migration guarantee: a
// pins file written before StableID existed, keyed on the content hash, keeps
// working with no migration step and no user action. PinFor resolves it, and
// a subsequent load still stamps the label from it.
//
// Delete the legacy fallback in models.ResolveByIdentity and this test fails
// on the first assertion -- the StableID is not in the file at all.
func TestPinFor_LegacyHashSidecarStillResolves(t *testing.T) {
	loader, txns, cleanup := pinFixture(t)
	defer cleanup()

	rows := byDescription(t, txns)
	vet, ok := rows["Animal Hospital"]
	if !ok {
		t.Fatal("row \"Animal Hospital\" missing from the loaded set")
	}
	if vet.Hash == "" || vet.StableID == "" {
		t.Fatalf("fixture row has Hash=%q StableID=%q; both are required", vet.Hash, vet.StableID)
	}

	// Stage the sidecar exactly as a pre-StableID release left it:
	// keyed on the content hash, verbatim, no rewriting.
	if err := loader.WriteTransactionPins(map[string]string{vet.Hash: "me-vet"}); err != nil {
		t.Fatalf("WriteTransactionPins: %v", err)
	}
	staged, err := loader.LoadTransactionPins()
	if err != nil {
		t.Fatalf("LoadTransactionPins: %v", err)
	}
	if _, ok := staged[vet.StableID]; ok {
		t.Fatal("WriteTransactionPins rewrote the key; it must persist the map verbatim")
	}
	if staged[vet.Hash] != "me-vet" {
		t.Fatalf("staged pins = %+v, want the legacy hash keyed to me-vet", staged)
	}

	id, ok := loader.PinFor(vet)
	if !ok {
		t.Fatal("PinFor did not resolve a legacy-hash-keyed pin")
	}
	if id != "me-vet" {
		t.Errorf("PinFor = %q, want %q", id, "me-vet")
	}

	// And the label stamping that reads the same store agrees.
	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	reloaded := byDescription(t, ts.Transactions)
	if got := reloaded["Animal Hospital"].MajorExpenseName; got != "Vet Bills" {
		t.Errorf("MajorExpenseName = %q, want %q (pin must survive on the legacy key)", got, "Vet Bills")
	}
	if got := reloaded["Corner Coffee"].MajorExpenseName; got != "" {
		t.Errorf("unpinned row was stamped %q", got)
	}
}

// TestSetTransactionPin_RekeysResolvedEntriesAndKeepsOrphans covers behavior 3:
// the next write moves entries it can identify onto StableIDs, and leaves a
// legacy key it cannot identify exactly where it is. The orphan is the point
// -- the row is probably just outside the loaded range, and dropping it would
// throw away a user decision.
func TestSetTransactionPin_RekeysResolvedEntriesAndKeepsOrphans(t *testing.T) {
	loader, txns, cleanup := pinFixture(t)
	defer cleanup()

	rows := byDescription(t, txns)
	vet := rows["Animal Hospital"]
	coffee := rows["Corner Coffee"]
	const orphan = "0123456789abcdef" // no loaded row has this hash

	if err := loader.WriteTransactionPins(map[string]string{
		vet.Hash: "me-vet",
		orphan:   "me-vet",
	}); err != nil {
		t.Fatalf("WriteTransactionPins: %v", err)
	}

	// Any ordinary write triggers the rekey pass.
	if err := loader.SetTransactionPin(coffee.Hash, "me-vet"); err != nil {
		t.Fatalf("SetTransactionPin: %v", err)
	}

	pins, err := loader.LoadTransactionPins()
	if err != nil {
		t.Fatalf("LoadTransactionPins: %v", err)
	}
	if len(pins) != 3 {
		t.Fatalf("pins file has %d entries, want 3: %+v", len(pins), pins)
	}
	if pins[vet.StableID] != "me-vet" {
		t.Errorf("resolved legacy entry was not rekeyed to %q: %+v", vet.StableID, pins)
	}
	if _, stillLegacy := pins[vet.Hash]; stillLegacy {
		t.Errorf("legacy key %q survived the rekey: %+v", vet.Hash, pins)
	}
	if pins[coffee.StableID] != "me-vet" {
		t.Errorf("new pin was not written under the StableID: %+v", pins)
	}
	if pins[orphan] != "me-vet" {
		t.Errorf("unresolvable legacy key was dropped rather than preserved: %+v", pins)
	}

	// The rekeyed store still resolves both rows, and the unpin path still
	// reaches an entry the UI names by its legacy hash.
	if id, ok := loader.PinFor(vet); !ok || id != "me-vet" {
		t.Errorf("PinFor after rekey = %q ok=%v, want me-vet true", id, ok)
	}
	if err := loader.ClearTransactionPin(vet.Hash); err != nil {
		t.Fatalf("ClearTransactionPin: %v", err)
	}
	if _, ok := loader.PinFor(vet); ok {
		t.Error("unpin by legacy hash did not clear the rekeyed entry")
	}
}

// TestApplyAmazonEnrichment_LegacyHashSidecarStillResolves is the same
// migration guarantee for the enrichment store.
func TestApplyAmazonEnrichment_LegacyHashSidecarStillResolves(t *testing.T) {
	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"amazon-card-2025.csv": `Date,Description,Category,Amount
2025-07-01,AMZN Mktp US*A1B2C,Shopping,-31.50`,
	})
	defer cleanup()

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 1 {
		t.Fatalf("loaded %d rows, want 1", len(ts.Transactions))
	}
	row := ts.Transactions[0]

	// Written the way a pre-StableID enrichment run left it.
	legacy, err := json.Marshal(map[string]string{row.Hash: "Amazon: USB-C cable"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "amazon_enrichment.json"), legacy, 0644); err != nil {
		t.Fatalf("stage enrichment: %v", err)
	}

	ts, err = loader.LoadData()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(ts.Transactions) != 1 {
		t.Fatalf("reloaded %d rows, want 1", len(ts.Transactions))
	}
	if got := ts.Transactions[0].EnrichedDescription; got != "Amazon: USB-C cable" {
		t.Errorf("EnrichedDescription = %q, want the legacy-keyed label", got)
	}

	// A save rekeys what it can identify.
	current, err := loader.LoadAmazonEnrichment()
	if err != nil {
		t.Fatalf("LoadAmazonEnrichment: %v", err)
	}
	if err := loader.SaveAmazonEnrichment(current); err != nil {
		t.Fatalf("SaveAmazonEnrichment: %v", err)
	}
	saved, err := loader.LoadAmazonEnrichment()
	if err != nil {
		t.Fatalf("reload enrichment: %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("enrichment has %d entries, want 1: %+v", len(saved), saved)
	}
	if saved[row.StableID] != "Amazon: USB-C cable" {
		t.Errorf("save did not rekey to the StableID: %+v", saved)
	}
}

// TestApplyDuplicateDetection_LegacyPairKeyStillResolves covers the third
// store. The decision was recorded under the pair key the rows had before
// StableID existed -- derived from their content hashes -- and must still
// suppress the losing side.
func TestApplyDuplicateDetection_LegacyPairKeyStillResolves(t *testing.T) {
	billPay := makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay")
	check := makeTx("2026-03-20", -1580.43, "Check #996583", "Posted")
	billPay.AccountID = "usaa-checking"
	check.AccountID = "usaa-checking"
	billPay.StableID = models.StableIDFor("usaa-checking", billPay.Date, -158043, 0)
	check.StableID = models.StableIDFor("usaa-checking", check.Date, -158043, 0)

	legacyKey := pairKey(billPay.Hash, check.Hash)
	doc := duplicateDecisionsDoc{Decisions: map[string]DuplicateDecision{
		legacyKey: {
			KeptHash:       billPay.Hash,
			SuppressedHash: check.Hash,
			Outcome:        DuplicateOutcomeKeptWinner,
			DecidedAt:      time.Now().UTC(),
		},
	}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"duplicate_decisions.json": string(data),
	})
	defer cleanup()

	txns := loader.applyDuplicateDetection([]models.Transaction{billPay, check})
	if len(txns) != 2 {
		t.Fatalf("applyDuplicateDetection returned %d rows, want 2", len(txns))
	}
	if n := loader.UnresolvedDuplicateCount(); n != 0 {
		t.Fatalf("%d pairs unresolved, want 0 (the legacy decision must be found)", n)
	}
	if resolved := loader.ResolvedDuplicates(); len(resolved) != 1 {
		t.Fatalf("%d resolved pairs, want 1", len(resolved))
	}
	if !txns[1].Suppressed {
		t.Error("the suppressed side was not suppressed via the legacy pair key")
	}
	if txns[0].Suppressed {
		t.Error("the kept side was suppressed")
	}

	// Re-deciding under the current key must not leave the legacy entry
	// behind, or both would apply on the next load.
	current := pairKey(billPay.StableID, check.StableID)
	if current == legacyKey {
		t.Fatal("fixture is degenerate: the StableID key equals the legacy key")
	}
	if err := loader.SaveDuplicateDecision(current, DuplicateDecision{
		Outcome: DuplicateOutcomeKeptBoth,
	}); err != nil {
		t.Fatalf("SaveDuplicateDecision: %v", err)
	}
	decisions, err := loader.LoadDuplicateDecisions()
	if err != nil {
		t.Fatalf("LoadDuplicateDecisions: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions file has %d entries, want 1: %+v", len(decisions), decisions)
	}
	if _, ok := decisions[current]; !ok {
		t.Errorf("decision was not stored under the StableID-derived key: %+v", decisions)
	}
}

// TestDeduplicateTransactions_IgnoresStableID is the regression guard the
// task calls for: StableID is an identity for user decisions, not a dedup
// key, so exact dedup must key on Hash alone and behave exactly as before.
func TestDeduplicateTransactions_IgnoresStableID(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	// Same content hash, deliberately different StableIDs: still one row.
	same := []models.Transaction{
		{Hash: "h1", Description: "Wegmans", Amount: -20, StableID: "a|2026-01-01|-2000|0"},
		{Hash: "h1", Description: "Wegmans", Amount: -20, StableID: "b|2026-01-01|-2000|7"},
	}
	if got := loader.deduplicateTransactions(same); len(got) != 1 {
		t.Errorf("identical hashes with different StableIDs produced %d rows, want 1", len(got))
	}

	// Different content hashes, deliberately identical StableIDs: still two.
	collide := []models.Transaction{
		{Hash: "h1", Description: "Wegmans", Amount: -20, StableID: "a|2026-01-01|-2000|0"},
		{Hash: "h2", Description: "Walgreens", Amount: -20, StableID: "a|2026-01-01|-2000|0"},
	}
	if got := loader.deduplicateTransactions(collide); len(got) != 2 {
		t.Errorf("distinct hashes sharing a StableID produced %d rows, want 2", len(got))
	}
}

// TestLoadData_DedupUnchangedAcrossOverlappingExports is the same regression
// at the load level: a row exported twice is still collapsed to one, and the
// survivor is the first occurrence in file order.
func TestLoadData_DedupUnchangedAcrossOverlappingExports(t *testing.T) {
	overlap := `Date,Description,Category,Amount
2025-05-04,Corner Coffee,Dining,-12.34
2025-05-05,Rochester Gas,Utilities,-100.00`

	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-a.csv": overlap,
		"usaa-checking-b.csv": overlap,
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
	if len(ts.Transactions) != 2 {
		t.Fatalf("loaded %d rows, want 2 (the second export is an exact duplicate)", len(ts.Transactions))
	}
	rows := byDescription(t, ts.Transactions)
	if got := rows["Corner Coffee"].StableID; got != "usaa-checking|2025-05-04|-1234|0" {
		t.Errorf("survivor StableID = %q, want the first occurrence's", got)
	}
	if got := rows["Corner Coffee"].SourceFile; got != "usaa-checking-a.csv" {
		t.Errorf("survivor came from %q, want usaa-checking-a.csv", got)
	}
}
