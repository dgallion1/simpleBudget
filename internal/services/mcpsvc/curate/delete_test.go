package curate

import (
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
