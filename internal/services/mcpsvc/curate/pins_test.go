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
