package dataloader

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestTransactionPinsPath(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	want := filepath.Join(loader.CSVDirectory, "transaction_pins.json")
	if got := loader.transactionPinsPath(); got != want {
		t.Errorf("transactionPinsPath() = %q, want %q", got, want)
	}
}

func TestLoadTransactionPins_NoFile(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	pins, err := loader.LoadTransactionPins()
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(pins) != 0 {
		t.Errorf("expected empty map, got %d entries", len(pins))
	}
}

func TestLoadTransactionPins_ValidFile(t *testing.T) {
	want := map[string]string{"hash1": "expense-a", "hash2": "expense-b"}
	data, _ := json.Marshal(want)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"transaction_pins.json": string(data),
	})
	defer cleanup()

	got, err := loader.LoadTransactionPins()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got["hash1"] != "expense-a" || got["hash2"] != "expense-b" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestLoadTransactionPins_InvalidJSON(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"transaction_pins.json": "{{{not json",
	})
	defer cleanup()

	if _, err := loader.LoadTransactionPins(); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSetTransactionPin_NewPin(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if err := loader.SetTransactionPin("hashA", "expense-1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	pins, err := loader.LoadTransactionPins()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if pins["hashA"] != "expense-1" {
		t.Errorf("expected expense-1 for hashA, got %q", pins["hashA"])
	}
}

func TestSetTransactionPin_OverwriteExisting(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if err := loader.SetTransactionPin("hashA", "first"); err != nil {
		t.Fatalf("set 1: %v", err)
	}
	if err := loader.SetTransactionPin("hashA", "second"); err != nil {
		t.Fatalf("set 2: %v", err)
	}
	pins, _ := loader.LoadTransactionPins()
	if pins["hashA"] != "second" {
		t.Errorf("expected second, got %q", pins["hashA"])
	}
}

func TestSetTransactionPin_EmptyExpenseRemoves(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	loader.SetTransactionPin("hashA", "expense-1")
	if err := loader.SetTransactionPin("hashA", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	pins, _ := loader.LoadTransactionPins()
	if _, ok := pins["hashA"]; ok {
		t.Errorf("expected hashA to be removed, still present: %+v", pins)
	}
}

func TestSetTransactionPin_EmptyHashRejected(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if err := loader.SetTransactionPin("", "x"); err == nil {
		t.Error("expected error for empty hash")
	}
}

func TestClearTransactionPin(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	loader.SetTransactionPin("hashA", "expense-1")
	loader.SetTransactionPin("hashB", "expense-2")

	if err := loader.ClearTransactionPin("hashA"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	pins, _ := loader.LoadTransactionPins()
	if _, ok := pins["hashA"]; ok {
		t.Error("hashA should be cleared")
	}
	if pins["hashB"] != "expense-2" {
		t.Error("hashB should still be pinned")
	}
}

func TestPrunePinsForMissingExpenses(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	loader.SetTransactionPin("hashA", "alive")
	loader.SetTransactionPin("hashB", "dead")
	loader.SetTransactionPin("hashC", "alive")

	if err := loader.PrunePinsForMissingExpenses(map[string]bool{"alive": true}); err != nil {
		t.Fatalf("prune: %v", err)
	}
	pins, _ := loader.LoadTransactionPins()
	if _, ok := pins["hashB"]; ok {
		t.Error("dead pin should be removed")
	}
	if pins["hashA"] != "alive" || pins["hashC"] != "alive" {
		t.Errorf("alive pins should remain: %+v", pins)
	}
}

func TestPrunePinsForMissingExpenses_NoChange(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	loader.SetTransactionPin("hashA", "x")
	if err := loader.PrunePinsForMissingExpenses(map[string]bool{"x": true}); err != nil {
		t.Fatalf("prune: %v", err)
	}
	pins, _ := loader.LoadTransactionPins()
	if pins["hashA"] != "x" {
		t.Error("should not have changed")
	}
}
