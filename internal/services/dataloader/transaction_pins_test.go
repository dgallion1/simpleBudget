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

func TestSetTransactionPins_BulkWrite(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if err := loader.SetTransactionPin("existing", "old-expense"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	updates := map[string]string{
		"hashA": "expense-1",
		"hashB": "expense-1",
		"hashC": "expense-2",
		"":      "skipped", // empty hash is ignored, not an error
	}
	n, err := loader.SetTransactionPins(updates)
	if err != nil {
		t.Fatalf("SetTransactionPins: %v", err)
	}
	if n != 3 {
		t.Errorf("changed = %d, want 3", n)
	}
	pins, _ := loader.LoadTransactionPins()
	if pins["existing"] != "old-expense" {
		t.Errorf("existing pin clobbered: %+v", pins)
	}
	if pins["hashA"] != "expense-1" || pins["hashB"] != "expense-1" || pins["hashC"] != "expense-2" {
		t.Errorf("bulk pins not applied: %+v", pins)
	}
	if _, ok := pins[""]; ok {
		t.Error("empty hash should not have been written")
	}
}

func TestSetTransactionPins_RemovesAndDedupes(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	loader.SetTransactionPin("keep", "x")
	loader.SetTransactionPin("drop", "x")

	n, err := loader.SetTransactionPins(map[string]string{
		"keep": "x",
		"drop": "",
		"new":  "y",
	})
	if err != nil {
		t.Fatalf("SetTransactionPins: %v", err)
	}
	if n != 2 {
		t.Errorf("changed = %d, want 2 (drop + new)", n)
	}
	pins, _ := loader.LoadTransactionPins()
	if pins["keep"] != "x" {
		t.Error("keep should remain")
	}
	if _, ok := pins["drop"]; ok {
		t.Error("drop should be removed")
	}
	if pins["new"] != "y" {
		t.Error("new should be added")
	}
}
