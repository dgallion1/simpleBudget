package curate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/models"
)

func seedForDelete(t *testing.T) (Deps, string) {
	t.Helper()
	deps, dir := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"mortgage"},
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	if _, err := deps.Pins.SetTransactionPins(map[string]string{roof.ComputeHash(): "me-mortgage"}); err != nil {
		t.Fatalf("seed pin: %v", err)
	}
	return deps, dir
}

func TestDeleteArchivesTheExpenseAndDetachesItsPins(t *testing.T) {
	deps, _ := seedForDelete(t)
	cs := connect(t, deps)

	out := decodeToolResult[deleteOutput](t, call(t, cs, "delete_major_expense",
		map[string]any{"id": "me-mortgage"}))
	if out.Restored || out.Name != "Mortgage" {
		t.Errorf("out = %+v, want an archive of Mortgage", out)
	}
	if out.PinsDetached != 1 {
		t.Errorf("pins_detached = %d, want 1", out.PinsDetached)
	}
	if len(out.SnapshotPaths) == 0 {
		t.Error("a write must report the snapshots taken before it")
	}
	active, _ := deps.Expenses.LoadMajorExpenses()
	if len(active) != 0 {
		t.Errorf("expense still active: %+v", active)
	}
	deleted, _ := deps.Expenses.LoadDeletedMajorExpenses()
	if len(deleted) != 1 || deleted[0].Expense.ID != "me-mortgage" {
		t.Errorf("archive = %+v, want the expense preserved for restore", deleted)
	}
	pins, _ := deps.Pins.LoadTransactionPins()
	if len(pins) != 0 {
		t.Errorf("pins survived the archive: %+v", pins)
	}
}

func TestDeleteWithRestoreBringsTheExpenseAndItsPinsBack(t *testing.T) {
	deps, _ := seedForDelete(t)
	cs := connect(t, deps)
	call(t, cs, "delete_major_expense", map[string]any{"id": "me-mortgage"})

	out := decodeToolResult[deleteOutput](t, call(t, cs, "delete_major_expense",
		map[string]any{"id": "me-mortgage", "restore": true}))
	if !out.Restored {
		t.Error("restored flag not set")
	}
	if out.PinsRestored != 1 {
		t.Errorf("pins_restored = %d, want 1", out.PinsRestored)
	}
	active, _ := deps.Expenses.LoadMajorExpenses()
	if len(active) != 1 || active[0].ID != "me-mortgage" {
		t.Errorf("active = %+v, want the expense back", active)
	}
	deleted, _ := deps.Expenses.LoadDeletedMajorExpenses()
	if len(deleted) != 0 {
		t.Errorf("archive = %+v, want it emptied", deleted)
	}
}

func TestDeleteRejectsAnUnknownID(t *testing.T) {
	deps, _ := seedForDelete(t)
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "delete_major_expense", map[string]any{"id": "nope"}))
	if !strings.Contains(msg, "nope") {
		t.Errorf("error should name the missing id, got: %s", msg)
	}
	restoreMsg := toolErrorText(t, call(t, cs, "delete_major_expense",
		map[string]any{"id": "nope", "restore": true}))
	if !strings.Contains(restoreMsg, "nope") {
		t.Errorf("restore error should name the missing id, got: %s", restoreMsg)
	}
}

func TestDeleteRequiresAnID(t *testing.T) {
	deps, _ := seedForDelete(t)
	cs := connect(t, deps)
	if msg := toolErrorText(t, call(t, cs, "delete_major_expense", map[string]any{})); msg == "" {
		t.Error("an empty id must be refused")
	}
}

// TestDeleteAbortsWhenAnExistingFileCannotBeBackedUp covers the fix: an
// Ensure failure that is NOT "file does not exist yet" must abort the whole
// operation before anything is written, not be tolerated the way a
// genuinely missing file is.
//
// The file chosen matters. A restore resolves the target's name by reading
// deleted_major_expenses.json, and loads transaction_pins.json for its
// before/after pin count, BEFORE the snapshot loop runs -- so either of
// those being unreadable would already fail the call for a reason that has
// nothing to do with the loop's own tolerance, and the old "continue past
// any error" code would produce an identical-looking failure. Only
// major_expenses.json is untouched until the loop (and, were the loop to
// tolerate the failure, again inside RestoreMajorExpense itself, which also
// reads it). Making THAT file unreadable is what actually distinguishes
// "the loop aborts up front with its own message" from "the loop shrugged
// and a downstream read failed for an unrelated reason" -- the old code's
// failure message would be a raw permission error with no "refusing to
// write" prefix.
//
// The archive itself is seeded by calling the dataloader directly, not by
// going through delete_major_expense first: the Snapshotter remembers a
// successful backup for the life of the session and short-circuits on a
// later Ensure of the same name without touching the file again, so routing
// the seed through the tool would let the chmod below go unnoticed --
// restore would hit the CACHED snapshot path from the first call rather
// than genuinely re-reading the now-unreadable file.
func TestDeleteAbortsWhenAnExistingFileCannotBeBackedUp(t *testing.T) {
	deps, dir := seedForDelete(t)
	if err := deps.Expenses.ArchiveMajorExpense("me-mortgage"); err != nil {
		t.Fatalf("ArchiveMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	majorExpensesPath := filepath.Join(dir, "major_expenses.json")
	before, err := os.ReadFile(majorExpensesPath)
	if err != nil {
		t.Fatalf("read major_expenses.json before: %v", err)
	}
	if err := os.Chmod(majorExpensesPath, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	res := call(t, cs, "delete_major_expense", map[string]any{"id": "me-mortgage", "restore": true})
	if err := os.Chmod(majorExpensesPath, 0o644); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}
	msg := toolErrorText(t, res)
	if !strings.Contains(msg, "refusing to write") || !strings.Contains(msg, majorExpensesFile) {
		t.Errorf("expected an explicit refusal naming %s, got: %s", majorExpensesFile, msg)
	}

	after, err := os.ReadFile(majorExpensesPath)
	if err != nil {
		t.Fatalf("read major_expenses.json after: %v", err)
	}
	if string(after) != string(before) {
		t.Error("major_expenses.json changed despite the backup failing")
	}
	deleted, _ := deps.Expenses.LoadDeletedMajorExpenses()
	if len(deleted) != 1 {
		t.Errorf("the restore must not have applied; the expense should still be archived: %+v", deleted)
	}
}

// TestDeleteRestoreDoesNotClobberAHashRepinnedSinceTheDelete is the
// documented don't-clobber rule: if a transaction that was pinned to the
// deleted expense gets pinned to a DIFFERENT expense before the restore, the
// restore must leave that newer pin alone.
func TestDeleteRestoreDoesNotClobberAHashRepinnedSinceTheDelete(t *testing.T) {
	deps, _ := seedForDelete(t)
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{ID: "me-other", Name: "Other"}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	cs := connect(t, deps)
	call(t, cs, "delete_major_expense", map[string]any{"id": "me-mortgage"})

	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-other",
		"hashes":     []any{roof.ComputeHash()},
	})

	out := decodeToolResult[deleteOutput](t, call(t, cs, "delete_major_expense",
		map[string]any{"id": "me-mortgage", "restore": true}))
	if out.PinsRestored != 0 {
		t.Errorf("pins_restored = %d, want 0 -- the hash is pinned elsewhere now and must not be clobbered", out.PinsRestored)
	}
	pins, _ := deps.Pins.LoadTransactionPins()
	if pins[roof.ComputeHash()] != "me-other" {
		t.Errorf("pins = %+v, want the newer pin to me-other left alone", pins)
	}
}
