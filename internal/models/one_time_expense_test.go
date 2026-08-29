package models

import (
	"encoding/json"
	"testing"
)

// TestOneTimeExpense_JSONRoundTrip verifies OneTimeExpense and its home on
// WhatIfSettings survive a JSON marshal/unmarshal round trip losslessly —
// the same mechanism prepare.From's DeepCopy relies on.
func TestOneTimeExpense_JSONRoundTrip(t *testing.T) {
	s := DefaultWhatIfSettings()
	s.OneTimeExpenses = []OneTimeExpense{
		{ID: "ote-1", Description: "New roof", Year: 3, Amount: 50_000},
		{ID: "ote-2", Description: "Wedding", Year: 7, Amount: 25_000.50},
	}

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("Marshal produced invalid JSON")
	}

	var out WhatIfSettings
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(out.OneTimeExpenses) != 2 {
		t.Fatalf("OneTimeExpenses length = %d, want 2", len(out.OneTimeExpenses))
	}
	if out.OneTimeExpenses[0] != s.OneTimeExpenses[0] {
		t.Errorf("entry 0 = %+v, want %+v", out.OneTimeExpenses[0], s.OneTimeExpenses[0])
	}
	if out.OneTimeExpenses[1] != s.OneTimeExpenses[1] {
		t.Errorf("entry 1 = %+v, want %+v", out.OneTimeExpenses[1], s.OneTimeExpenses[1])
	}
}

// TestOneTimeExpense_JSONFieldNames locks down the wire format the spec
// requires: one_time_expenses on the settings object, and id/description/
// year/amount on each entry.
func TestOneTimeExpense_JSONFieldNames(t *testing.T) {
	s := &WhatIfSettings{
		OneTimeExpenses: []OneTimeExpense{
			{ID: "x", Description: "Car", Year: 2, Amount: 30000},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var generic map[string]interface{}
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("Unmarshal to generic map: %v", err)
	}
	items, ok := generic["one_time_expenses"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected one_time_expenses array with 1 entry, got %v", generic["one_time_expenses"])
	}
	entry, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("entry is not an object: %v", items[0])
	}
	for _, key := range []string{"id", "description", "year", "amount"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("expected key %q in entry, got %v", key, entry)
		}
	}
}

// TestOneTimeExpense_EmptyListOmitted verifies the omitempty tag: a nil or
// empty OneTimeExpenses slice does not appear in the marshaled JSON.
func TestOneTimeExpense_EmptyListOmitted(t *testing.T) {
	s := &WhatIfSettings{}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var generic map[string]interface{}
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, present := generic["one_time_expenses"]; present {
		t.Errorf("expected one_time_expenses omitted when empty, got %v", generic["one_time_expenses"])
	}
}
