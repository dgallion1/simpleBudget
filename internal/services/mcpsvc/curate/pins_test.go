package curate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/models"
)

func mortgageExpense(t *testing.T, deps Deps) {
	t.Helper()
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"mortgage"},
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
}

func TestPinTransactionsAttachesNamedHashesAndSnapshotsFirst(t *testing.T) {
	deps, dir := newDeps(t, ledger())
	mortgageExpense(t, deps)
	// Seed the pins file so there is something to snapshot; Ensure treats a
	// missing source as an error the write must abort on.
	if _, err := deps.Pins.SetTransactionPins(map[string]string{"seed": "me-mortgage"}); err != nil {
		t.Fatalf("seed pins: %v", err)
	}
	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	cs := connect(t, deps)

	out := decodeToolResult[pinOutput](t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-mortgage",
		"hashes":     []any{roof.ComputeHash()},
	}))
	if out.Changed != 1 || out.Matched != 1 {
		t.Errorf("matched/changed = %d/%d, want 1/1", out.Matched, out.Changed)
	}
	if out.SnapshotPath == "" {
		t.Fatal("a write must report the snapshot taken before it")
	}
	if _, err := os.Stat(out.SnapshotPath); err != nil {
		t.Errorf("snapshot %s does not exist: %v", out.SnapshotPath, err)
	}
	pins, err := deps.Pins.LoadTransactionPins()
	if err != nil {
		t.Fatalf("LoadTransactionPins: %v", err)
	}
	if pins[roof.ComputeHash()] != "me-mortgage" {
		t.Errorf("pin not written: %+v", pins)
	}
	if _, err := os.Stat(filepath.Join(dir, "transaction_pins.json")); err != nil {
		t.Errorf("pins file missing: %v", err)
	}
}

func TestPinTransactionsUnpinsWithoutAnExpenseID(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	mortgageExpense(t, deps)
	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	if _, err := deps.Pins.SetTransactionPins(map[string]string{roof.ComputeHash(): "me-mortgage"}); err != nil {
		t.Fatalf("seed pin: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[pinOutput](t, call(t, cs, "pin_transactions", map[string]any{
		"unpin":  true,
		"hashes": []any{roof.ComputeHash()},
	}))
	if !out.Unpinned || out.Changed != 1 {
		t.Errorf("unpinned/changed = %v/%d, want true/1", out.Unpinned, out.Changed)
	}
	pins, _ := deps.Pins.LoadTransactionPins()
	if _, still := pins[roof.ComputeHash()]; still {
		t.Errorf("pin survived the unpin: %+v", pins)
	}
}

func TestPinTransactionsPinsEveryRowMatchingAFilter(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{ID: "me-home", Name: "Home Repair"}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	if _, err := deps.Pins.SetTransactionPins(map[string]string{"seed": "me-home"}); err != nil {
		t.Fatalf("seed pins: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[pinOutput](t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-home",
		"filter":     map[string]any{"search": "roofing", "unmatched_only": true},
	}))
	if out.Matched != 1 || out.Changed != 1 {
		t.Errorf("matched/changed = %d/%d, want 1/1", out.Matched, out.Changed)
	}
	if len(out.Hashes) != 1 {
		t.Errorf("hashes = %v, want the one row acted on", out.Hashes)
	}
}

func TestPinTransactionsRefusesAFilterWiderThanTheCap(t *testing.T) {
	txns := make([]models.Transaction, 0, maxBulkPin+5)
	for i := 0; i < maxBulkPin+5; i++ {
		txns = append(txns, models.Transaction{
			Date: day(2026, 1, 1).AddDate(0, 0, i), Description: "WIDE VENDOR", Category: "Misc",
			Amount: float64(-10 - i), TransactionType: models.Outflow,
		})
	}
	deps, _ := newDeps(t, txns)
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{ID: "me-wide", Name: "Wide"}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-wide",
		"filter":     map[string]any{"search": "wide"},
	}))
	if !strings.Contains(msg, "narrow") {
		t.Errorf("the refusal must tell the caller to narrow the filter, got: %s", msg)
	}
	pins, _ := deps.Pins.LoadTransactionPins()
	if len(pins) != 0 {
		t.Errorf("a refused bulk pin must write nothing, got %d pins", len(pins))
	}
}

func TestPinTransactionsRejectsAnUnknownExpense(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)
	msg := toolErrorText(t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "nope", "hashes": []any{"abc"},
	}))
	if !strings.Contains(msg, "nope") {
		t.Errorf("error should name the missing expense, got: %s", msg)
	}
}

func TestPinTransactionsRejectsAmbiguousOrEmptyTargeting(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	mortgageExpense(t, deps)
	cs := connect(t, deps)

	both := toolErrorText(t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-mortgage", "hashes": []any{"abc"},
		"filter": map[string]any{"search": "x"},
	}))
	if both == "" {
		t.Error("supplying both hashes and filter must be refused")
	}
	neither := toolErrorText(t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-mortgage",
	}))
	if neither == "" {
		t.Error("supplying neither hashes nor filter must be refused")
	}
}

// TestPinTransactionsRejectsAnIncomeRowHash covers the fix's headline
// scenario: a hash for an income row (never a matched or unmatched outflow,
// since pageView filters to outflows before matching) must be skipped rather
// than silently pinned and inertly ignored forever.
func TestPinTransactionsRejectsAnIncomeRowHash(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	mortgageExpense(t, deps)
	if _, err := deps.Pins.SetTransactionPins(map[string]string{"seed": "me-mortgage"}); err != nil {
		t.Fatalf("seed pins: %v", err)
	}
	income := models.Transaction{Date: day(2026, 2, 20), Description: "MORTGAGE ESCROW DEPOSIT", Amount: 1200}
	cs := connect(t, deps)

	out := decodeToolResult[pinOutput](t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-mortgage",
		"hashes":     []any{income.ComputeHash()},
	}))
	if out.Matched != 0 || out.Changed != 0 {
		t.Errorf("matched/changed = %d/%d, want 0/0 -- an income row is never pinnable", out.Matched, out.Changed)
	}
	if len(out.UnknownHashes) != 1 || out.UnknownHashes[0] != income.ComputeHash() {
		t.Errorf("unknown_hashes = %v, want the income hash reported back", out.UnknownHashes)
	}
	if out.SnapshotPath != "" {
		t.Error("nothing was written, so nothing should have been snapshotted")
	}
	pins, _ := deps.Pins.LoadTransactionPins()
	if _, ok := pins[income.ComputeHash()]; ok {
		t.Error("the income row must not have been pinned")
	}
}

// TestPinTransactionsRejectsANonexistentHash covers a mistyped or
// hallucinated hash that matches no transaction at all.
func TestPinTransactionsRejectsANonexistentHash(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	mortgageExpense(t, deps)
	cs := connect(t, deps)

	out := decodeToolResult[pinOutput](t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-mortgage",
		"hashes":     []any{"not-a-real-hash"},
	}))
	if out.Matched != 0 {
		t.Errorf("matched = %d, want 0", out.Matched)
	}
	if len(out.UnknownHashes) != 1 || out.UnknownHashes[0] != "not-a-real-hash" {
		t.Errorf("unknown_hashes = %v, want the bogus hash reported back", out.UnknownHashes)
	}
	if out.Note == "" {
		t.Error("expected a note explaining nothing was targeted")
	}
}

// TestPinTransactionsPinsTheValidHashesInAMixAndReportsTheRest covers a
// call naming a blend of a real pinnable outflow, an income row, and a
// nonexistent hash: the valid one must still be pinned, and the other two
// reported back rather than silently dropped.
func TestPinTransactionsPinsTheValidHashesInAMixAndReportsTheRest(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	mortgageExpense(t, deps)
	if _, err := deps.Pins.SetTransactionPins(map[string]string{"seed": "me-mortgage"}); err != nil {
		t.Fatalf("seed pins: %v", err)
	}
	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	income := models.Transaction{Date: day(2026, 2, 20), Description: "MORTGAGE ESCROW DEPOSIT", Amount: 1200}
	cs := connect(t, deps)

	out := decodeToolResult[pinOutput](t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-mortgage",
		"hashes":     []any{roof.ComputeHash(), income.ComputeHash(), "bogus"},
	}))
	if out.Matched != 1 || out.Changed != 1 {
		t.Errorf("matched/changed = %d/%d, want 1/1 -- only the roofing hash is a pinnable outflow", out.Matched, out.Changed)
	}
	if len(out.UnknownHashes) != 2 {
		t.Errorf("unknown_hashes = %v, want the income row and the bogus hash", out.UnknownHashes)
	}
	if out.Note == "" {
		t.Error("expected a note about the skipped hashes even though some were pinned")
	}
	pins, _ := deps.Pins.LoadTransactionPins()
	if pins[roof.ComputeHash()] != "me-mortgage" {
		t.Errorf("the valid hash must still have been pinned: %+v", pins)
	}
}

// TestPinTransactionsSucceedsOnAFreshInstallWithNoPinsFileYet covers the fix:
// transaction_pins.json is not created until the first pin is ever written,
// so Ensure's "missing source is an error" behavior must not block that very
// first write -- pin_transactions, the package's primary curation flow, was
// unusable on a fresh install before this fix. Unlike the other pin tests in
// this file, this one deliberately does NOT seed transaction_pins.json first.
func TestPinTransactionsSucceedsOnAFreshInstallWithNoPinsFileYet(t *testing.T) {
	deps, dir := newDeps(t, ledger())
	mortgageExpense(t, deps)
	if _, err := os.Stat(filepath.Join(dir, "transaction_pins.json")); err == nil {
		t.Fatal("test setup: transaction_pins.json must not exist yet")
	}
	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	cs := connect(t, deps)

	res := call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-mortgage",
		"hashes":     []any{roof.ComputeHash()},
	})
	if res.IsError {
		t.Fatalf("FRESH-INSTALL PIN FAILED: %s", toolErrorText(t, res))
	}
	out := decodeToolResult[pinOutput](t, res)
	if out.Changed != 1 || out.Matched != 1 {
		t.Errorf("matched/changed = %d/%d, want 1/1", out.Matched, out.Changed)
	}
	if out.SnapshotPath != "" {
		t.Errorf("snapshot_path = %q, want empty -- there was no prior file to back up", out.SnapshotPath)
	}
	pins, err := deps.Pins.LoadTransactionPins()
	if err != nil {
		t.Fatalf("LoadTransactionPins: %v", err)
	}
	if pins[roof.ComputeHash()] != "me-mortgage" {
		t.Fatalf("pin not written: %+v", pins)
	}
}

// TestPinTransactionsAbortsWhenAnExistingPinsFileCannotBeBackedUp covers the
// fix's other half: a non-not-found Ensure failure must still abort the
// write, unlike a genuinely missing file.
//
// The failure is engineered at the SNAPSHOT DESTINATION, not the data file:
// a regular file is planted where Ensure needs to create the snapshot
// directory, so os.MkdirAll fails with "not a directory" (never
// fs.ErrNotExist, on any OS) while transaction_pins.json itself stays fully
// readable and writable throughout. That is deliberate. An earlier version
// of this test instead chmod'd the DATA file unreadable to fail the
// snapshot, but the subsequent write path (deps.Pins.SetTransactionPins)
// reads that very same file before it writes, so it failed on its own even
// with the abort branch disabled -- the test passed whether or not the
// abort actually ran, because both the correct and the broken build wrote
// nothing, for different reasons. Blocking only the snapshot side makes the
// two builds diverge: correct code sees the Ensure failure, is not
// fs.ErrNotExist, and aborts before writing; broken code ignores that
// failure and calls SetTransactionPins, which succeeds normally because the
// data file was never touched.
//
// The pins file is seeded by calling deps.Pins.SetTransactionPins directly,
// not by routing a pin through pin_transactions or upsert_major_expense's
// pin_hash first: either of those would call
// Snapshots.Ensure(transactionPinsFile, ...) itself and the Snapshotter
// remembers a successful backup for the life of the process, short-circuiting
// a later Ensure of the same name without touching the file again -- so the
// blocker planted below would go unnoticed and this test would pass
// vacuously against the CACHED snapshot path from the seeding call rather
// than genuinely re-attempting the now-blocked snapshot.
func TestPinTransactionsAbortsWhenAnExistingPinsFileCannotBeBackedUp(t *testing.T) {
	deps, dir := newDeps(t, ledger())
	mortgageExpense(t, deps)
	if _, err := deps.Pins.SetTransactionPins(map[string]string{"seed": "me-mortgage"}); err != nil {
		t.Fatalf("seed pins: %v", err)
	}
	cs := connect(t, deps)

	pinsPath := filepath.Join(dir, "transaction_pins.json")
	before, err := os.ReadFile(pinsPath)
	if err != nil {
		t.Fatalf("read pins before: %v", err)
	}

	// newDeps points this Deps' Snapshotter at filepath.Join(dir,
	// "snapshots"). Plant a plain file there so Ensure's os.MkdirAll of that
	// same path fails with ENOTDIR -- a non-fs.ErrNotExist error -- without
	// touching transaction_pins.json at all.
	snapshotDirPath := filepath.Join(dir, "snapshots")
	if err := os.WriteFile(snapshotDirPath, []byte("blocking the snapshot directory"), 0o644); err != nil {
		t.Fatalf("plant snapshot-dir blocker: %v", err)
	}

	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	res := call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-mortgage",
		"hashes":     []any{roof.ComputeHash()},
	})
	msg := toolErrorText(t, res)
	if !strings.Contains(msg, "snapshot") {
		t.Errorf("expected the refusal to mention the failed snapshot, got: %s", msg)
	}

	// The discriminating assertion: the data file's bytes must be BYTE-FOR-
	// BYTE unchanged. "The tool returned an error" alone does not prove the
	// abort fired -- that was exactly the shape of the vacuous version of
	// this test.
	after, err := os.ReadFile(pinsPath)
	if err != nil {
		t.Fatalf("read pins after: %v", err)
	}
	if string(after) != string(before) {
		t.Error("transaction_pins.json changed despite the backup failing")
	}
	pins, err := deps.Pins.LoadTransactionPins()
	if err != nil {
		t.Fatalf("LoadTransactionPins: %v", err)
	}
	if pins[roof.ComputeHash()] != "" {
		t.Errorf("pin must not have been written: %+v", pins)
	}
}

func TestPinTransactionsReportsAFilterThatMatchedNothing(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	mortgageExpense(t, deps)
	cs := connect(t, deps)

	out := decodeToolResult[pinOutput](t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-mortgage",
		"filter":     map[string]any{"search": "no such merchant"},
	}))
	if out.Matched != 0 || out.Changed != 0 {
		t.Errorf("matched/changed = %d/%d, want 0/0", out.Matched, out.Changed)
	}
	if out.Note == "" {
		t.Error("a filter that matched nothing must say so rather than looking like a silent success")
	}
	if out.SnapshotPath != "" {
		t.Error("nothing was written, so nothing should have been snapshotted")
	}
}
