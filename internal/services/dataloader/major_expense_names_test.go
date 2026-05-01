package dataloader

import (
	"encoding/json"
	"testing"

	"budget2/internal/models"
)

func TestApplyMajorExpenseNames_NoFiles(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "BOFA HOMELOANS 0123", Amount: -1500},
	}
	got := loader.applyMajorExpenseNames(txns)
	if got[0].MajorExpenseName != "" {
		t.Errorf("expected empty MajorExpenseName when no defs file; got %q", got[0].MajorExpenseName)
	}
}

func TestApplyMajorExpenseNames_KeywordMatch(t *testing.T) {
	defs := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{ID: "me1", Name: "Mortgage", Keywords: []string{"homeloans"}},
	}}
	defsJSON, _ := json.Marshal(defs)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json": string(defsJSON),
	})
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "BOFA HOMELOANS 0123", Amount: -1500},
		{Hash: "h2", Description: "Whole Foods", Amount: -42},
	}
	got := loader.applyMajorExpenseNames(txns)
	if got[0].MajorExpenseName != "Mortgage" {
		t.Errorf("expected MajorExpenseName 'Mortgage'; got %q", got[0].MajorExpenseName)
	}
	if got[1].MajorExpenseName != "" {
		t.Errorf("expected empty MajorExpenseName for unmatched; got %q", got[1].MajorExpenseName)
	}
}

func TestApplyMajorExpenseNames_PinOverridesMatching(t *testing.T) {
	defs := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"homeloans"}},
		{ID: "me-rare", Name: "Rare Pin Target"},
	}}
	defsJSON, _ := json.Marshal(defs)
	pins := map[string]string{"h1": "me-rare"}
	pinsJSON, _ := json.Marshal(pins)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json":   string(defsJSON),
		"transaction_pins.json": string(pinsJSON),
	})
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "BOFA HOMELOANS 0123", Amount: -1500},
	}
	got := loader.applyMajorExpenseNames(txns)
	if got[0].MajorExpenseName != "Rare Pin Target" {
		t.Errorf("pin should override keyword match; got %q", got[0].MajorExpenseName)
	}
}

func TestApplyMajorExpenseNames_PinToDeletedExpenseFallsThrough(t *testing.T) {
	defs := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"homeloans"}},
	}}
	defsJSON, _ := json.Marshal(defs)
	pins := map[string]string{"h1": "deleted-id"}
	pinsJSON, _ := json.Marshal(pins)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json":   string(defsJSON),
		"transaction_pins.json": string(pinsJSON),
	})
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "BOFA HOMELOANS 0123", Amount: -1500},
	}
	got := loader.applyMajorExpenseNames(txns)
	if got[0].MajorExpenseName != "Mortgage" {
		t.Errorf("expected fall-through to keyword match 'Mortgage'; got %q", got[0].MajorExpenseName)
	}
}

func TestApplyMajorExpenseNames_RangeAmount(t *testing.T) {
	defs := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{ID: "me-rent", Name: "Rent", ExpectedMin: 1000, ExpectedMax: 2000},
	}}
	defsJSON, _ := json.Marshal(defs)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json": string(defsJSON),
	})
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "Anything", Amount: -1500},
		{Hash: "h2", Description: "Anything", Amount: -50},
	}
	got := loader.applyMajorExpenseNames(txns)
	if got[0].MajorExpenseName != "Rent" {
		t.Errorf("expected 'Rent' for in-range amount; got %q", got[0].MajorExpenseName)
	}
	if got[1].MajorExpenseName != "" {
		t.Errorf("expected empty for out-of-range amount; got %q", got[1].MajorExpenseName)
	}
}

func TestApplyMajorExpenseNames_InvalidJSONFallsBack(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json": "broken json{{{",
	})
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "Whatever", Amount: -10},
	}
	got := loader.applyMajorExpenseNames(txns)
	if len(got) != 1 || got[0].MajorExpenseName != "" {
		t.Error("expected empty MajorExpenseName when defs file is corrupt (graceful no-op)")
	}
}

func TestApplyMajorExpenseNames_InvalidPinsFallsBackToMatching(t *testing.T) {
	defs := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"homeloans"}},
	}}
	defsJSON, _ := json.Marshal(defs)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json":   string(defsJSON),
		"transaction_pins.json": "broken json{{{",
	})
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "BOFA HOMELOANS 0123", Amount: -1500},
	}
	got := loader.applyMajorExpenseNames(txns)
	if got[0].MajorExpenseName != "Mortgage" {
		t.Errorf("expected fall-through to keyword match 'Mortgage' when pins file is corrupt; got %q", got[0].MajorExpenseName)
	}
}
