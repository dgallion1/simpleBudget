package dataloader

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"budget2/internal/models"
)

func TestMajorExpensesPath(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	expected := filepath.Join(loader.CSVDirectory, "major_expenses.json")
	if loader.majorExpensesPath() != expected {
		t.Errorf("majorExpensesPath() = %q, want %q", loader.majorExpensesPath(), expected)
	}
}

func TestLoadMajorExpenses_NoFile(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	list, err := loader.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestLoadMajorExpenses_ValidFile(t *testing.T) {
	store := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{ID: "a", Name: "Mortgage", Keywords: []string{"mortgage"}, ExpectedMin: 1500, ExpectedMax: 2000},
		{ID: "b", Name: "Gym", Keywords: []string{"planet fitness"}},
	}}
	data, _ := json.Marshal(store)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json": string(data),
	})
	defer cleanup()

	list, err := loader.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}
	if list[0].Name != "Mortgage" || list[1].Name != "Gym" {
		t.Errorf("unexpected entries: %+v", list)
	}
}

func TestLoadMajorExpenses_InvalidJSON(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json": "not valid json{{{",
	})
	defer cleanup()

	_, err := loader.LoadMajorExpenses()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSaveMajorExpenses_RoundTrip(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	in := []models.MajorExpense{
		{ID: "x", Name: "Rent", Keywords: []string{"landlord"}, ExpectedMin: 1200, ExpectedMax: 1200, Notes: "n"},
	}
	if err := loader.SaveMajorExpenses(in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := loader.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out) != 1 || out[0].Name != "Rent" || out[0].Notes != "n" {
		t.Errorf("round-trip mismatch: %+v", out)
	}
}

func TestSaveMajorExpenses_NilTreatedAsEmpty(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if err := loader.SaveMajorExpenses(nil); err != nil {
		t.Fatalf("save nil: %v", err)
	}
	out, err := loader.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty list, got %d", len(out))
	}
}

func TestAddMajorExpense_AssignsTimestamps(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	before := time.Now().UTC().Add(-time.Second)
	list, err := loader.AddMajorExpense(models.MajorExpense{
		ID: "1", Name: "Mortgage", Keywords: []string{"mortgage"}, ExpectedMin: 1500, ExpectedMax: 2000,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}
	if list[0].CreatedAt.Before(before) || list[0].UpdatedAt.Before(before) {
		t.Errorf("timestamps not stamped: created=%v updated=%v", list[0].CreatedAt, list[0].UpdatedAt)
	}

	// Adding another entry preserves the existing one
	list2, err := loader.AddMajorExpense(models.MajorExpense{ID: "2", Name: "Gym", Keywords: []string{"gym"}})
	if err != nil {
		t.Fatalf("add 2: %v", err)
	}
	if len(list2) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list2))
	}
}

func TestUpdateMajorExpense_Success(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	list, err := loader.AddMajorExpense(models.MajorExpense{
		ID: "1", Name: "Mortgage", Keywords: []string{"mortgage"}, ExpectedMin: 1500, ExpectedMax: 2000,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	originalCreated := list[0].CreatedAt

	time.Sleep(2 * time.Millisecond) // ensure UpdatedAt advances

	list2, err := loader.UpdateMajorExpense("1", models.MajorExpense{
		Name: "Mortgage v2", Keywords: []string{"mortgage", "bk"}, ExpectedMin: 1600, ExpectedMax: 2100, Notes: "refi",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(list2) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list2))
	}
	if list2[0].Name != "Mortgage v2" || list2[0].ExpectedMin != 1600 || list2[0].ExpectedMax != 2100 {
		t.Errorf("update did not apply: %+v", list2[0])
	}
	if list2[0].Notes != "refi" {
		t.Errorf("notes not updated")
	}
	if !list2[0].CreatedAt.Equal(originalCreated) {
		t.Errorf("CreatedAt should be preserved")
	}
	if !list2[0].UpdatedAt.After(originalCreated) {
		t.Errorf("UpdatedAt should advance")
	}
}

func TestUpdateMajorExpense_NotFound(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	_, err := loader.UpdateMajorExpense("missing", models.MajorExpense{Name: "X"})
	if err == nil {
		t.Error("expected error for missing ID")
	}
}

func TestDeleteMajorExpense_Success(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "1", Name: "A"}); err != nil {
		t.Fatalf("add 1: %v", err)
	}
	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "2", Name: "B"}); err != nil {
		t.Fatalf("add 2: %v", err)
	}

	list, err := loader.DeleteMajorExpense("1")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(list) != 1 || list[0].ID != "2" {
		t.Errorf("expected only entry 2 to remain, got %+v", list)
	}
}

func TestDeleteMajorExpense_MissingIDIsNoOp(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "1", Name: "A"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	list, err := loader.DeleteMajorExpense("nope")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 entry to remain, got %d", len(list))
	}
}

func TestArchiveMajorExpense_CapturesDefinitionAndPins(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "doomed", Name: "Home Support", Keywords: []string{"home depot"}, ExpectedMin: 10, ExpectedMax: 200}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "keep", Name: "Rent"}); err != nil {
		t.Fatalf("add keep: %v", err)
	}
	if err := loader.SetTransactionPin("hashA", "doomed"); err != nil {
		t.Fatalf("pin A: %v", err)
	}
	if err := loader.SetTransactionPin("hashB", "doomed"); err != nil {
		t.Fatalf("pin B: %v", err)
	}
	if err := loader.SetTransactionPin("hashC", "keep"); err != nil {
		t.Fatalf("pin C: %v", err)
	}

	if err := loader.ArchiveMajorExpense("doomed"); err != nil {
		t.Fatalf("archive: %v", err)
	}

	active, err := loader.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("load active: %v", err)
	}
	if len(active) != 1 || active[0].ID != "keep" {
		t.Errorf("expected only 'keep' to remain active, got %+v", active)
	}

	deleted, err := loader.LoadDeletedMajorExpenses()
	if err != nil {
		t.Fatalf("load deleted: %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("expected 1 archived entry, got %d", len(deleted))
	}
	got := deleted[0]
	if got.Expense.ID != "doomed" || got.Expense.Name != "Home Support" {
		t.Errorf("archived definition mismatch: %+v", got.Expense)
	}
	if got.DeletedAt.IsZero() {
		t.Errorf("DeletedAt should be set")
	}
	if len(got.Expense.Keywords) != 1 || got.Expense.Keywords[0] != "home depot" {
		t.Errorf("keywords lost: %+v", got.Expense.Keywords)
	}
	if got.Expense.ExpectedMin != 10 || got.Expense.ExpectedMax != 200 {
		t.Errorf("expected amounts lost: min=%v max=%v", got.Expense.ExpectedMin, got.Expense.ExpectedMax)
	}
	gotHashes := map[string]bool{}
	for _, h := range got.PinnedHashes {
		gotHashes[h] = true
	}
	if !gotHashes["hashA"] || !gotHashes["hashB"] {
		t.Errorf("expected hashA and hashB in archive, got %+v", got.PinnedHashes)
	}
	if gotHashes["hashC"] {
		t.Errorf("hashC was pinned to a different expense; should not be archived: %+v", got.PinnedHashes)
	}

	pins, err := loader.LoadTransactionPins()
	if err != nil {
		t.Fatalf("load pins: %v", err)
	}
	if _, ok := pins["hashA"]; ok {
		t.Errorf("hashA pin should be removed from active pins")
	}
	if _, ok := pins["hashB"]; ok {
		t.Errorf("hashB pin should be removed from active pins")
	}
	if pins["hashC"] != "keep" {
		t.Errorf("hashC pin should survive: got %q", pins["hashC"])
	}
}

func TestArchiveMajorExpense_NoPins(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "lonely", Name: "Solo"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := loader.ArchiveMajorExpense("lonely"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	deleted, err := loader.LoadDeletedMajorExpenses()
	if err != nil {
		t.Fatalf("load deleted: %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("expected 1 archived entry, got %d", len(deleted))
	}
	if len(deleted[0].PinnedHashes) != 0 {
		t.Errorf("expected empty PinnedHashes, got %+v", deleted[0].PinnedHashes)
	}
}

func TestArchiveMajorExpense_NotFound(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "exists", Name: "X"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	err := loader.ArchiveMajorExpense("nope")
	if err == nil {
		t.Fatal("expected error for unknown id")
	}
	active, err := loader.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("load active: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("active list should be unchanged on archive failure, got %+v", active)
	}
	deleted, err := loader.LoadDeletedMajorExpenses()
	if err != nil {
		t.Fatalf("load deleted: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("deleted list should remain empty on failure, got %+v", deleted)
	}
}

func TestRestoreMajorExpense_RestoresPinsAndDefinition(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "back", Name: "Back", Keywords: []string{"k1", "k2"}, ExpectedMin: 50, ExpectedMax: 75, Notes: "n"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	for _, h := range []string{"h1", "h2", "h3"} {
		if err := loader.SetTransactionPin(h, "back"); err != nil {
			t.Fatalf("pin %s: %v", h, err)
		}
	}
	if err := loader.ArchiveMajorExpense("back"); err != nil {
		t.Fatalf("archive: %v", err)
	}

	if err := loader.RestoreMajorExpense("back"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	active, err := loader.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(active) != 1 || active[0].ID != "back" {
		t.Fatalf("expected back to be restored, got %+v", active)
	}
	if active[0].Name != "Back" || len(active[0].Keywords) != 2 || active[0].Notes != "n" {
		t.Errorf("definition fields lost: %+v", active[0])
	}

	pins, err := loader.LoadTransactionPins()
	if err != nil {
		t.Fatalf("pins: %v", err)
	}
	for _, h := range []string{"h1", "h2", "h3"} {
		if pins[h] != "back" {
			t.Errorf("pin %s should be restored to back, got %q", h, pins[h])
		}
	}
	deleted, err := loader.LoadDeletedMajorExpenses()
	if err != nil {
		t.Fatalf("load deleted: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("archive should be empty after restore, got %+v", deleted)
	}
}

func TestRestoreMajorExpense_DoesNotClobberCurrentPins(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "A", Name: "A"}); err != nil {
		t.Fatalf("add A: %v", err)
	}
	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "B", Name: "B"}); err != nil {
		t.Fatalf("add B: %v", err)
	}
	if err := loader.SetTransactionPin("shared", "A"); err != nil {
		t.Fatalf("pin shared→A: %v", err)
	}
	if err := loader.ArchiveMajorExpense("A"); err != nil {
		t.Fatalf("archive A: %v", err)
	}
	// Pin the same hash to a different expense after archive.
	if err := loader.SetTransactionPin("shared", "B"); err != nil {
		t.Fatalf("pin shared→B: %v", err)
	}
	if err := loader.RestoreMajorExpense("A"); err != nil {
		t.Fatalf("restore A: %v", err)
	}
	pins, err := loader.LoadTransactionPins()
	if err != nil {
		t.Fatalf("pins: %v", err)
	}
	if pins["shared"] != "B" {
		t.Errorf("expected pin shared to remain on B, got %q", pins["shared"])
	}
}

func TestRestoreMajorExpense_NotFound(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if err := loader.RestoreMajorExpense("missing"); err == nil {
		t.Error("expected error restoring non-existent archived entry")
	}
}

func TestRestoreMajorExpense_RejectsDuplicateActiveID(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "dup", Name: "Original"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := loader.ArchiveMajorExpense("dup"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	// Re-create a new expense with the same ID before restoring.
	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "dup", Name: "Replacement"}); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if err := loader.RestoreMajorExpense("dup"); err == nil {
		t.Error("expected error: cannot restore over existing active id")
	}
}

func TestDiscardDeletedMajorExpense_PermanentRemoval(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "trash", Name: "T"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := loader.ArchiveMajorExpense("trash"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := loader.DiscardDeletedMajorExpense("trash"); err != nil {
		t.Fatalf("discard: %v", err)
	}
	deleted, err := loader.LoadDeletedMajorExpenses()
	if err != nil {
		t.Fatalf("load deleted: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("expected archive to be empty, got %+v", deleted)
	}
	if err := loader.RestoreMajorExpense("trash"); err == nil {
		t.Error("restore after discard should fail")
	}
}

func TestDiscardDeletedMajorExpense_NotFound(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if err := loader.DiscardDeletedMajorExpense("never"); err == nil {
		t.Error("expected error discarding unknown id")
	}
}
