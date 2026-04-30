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
