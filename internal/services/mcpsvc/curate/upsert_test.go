package curate

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestUpsertCreatesAnExpenseAndReturnsItsID(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)

	out := decodeToolResult[upsertOutput](t, call(t, cs, "upsert_major_expense", map[string]any{
		"name": "Mortgage", "keywords": []any{"mortgage"}, "expected_min": 1900.0, "expected_max": 2100.0,
	}))
	if !out.Created || out.ID == "" {
		t.Fatalf("created/id = %v/%q, want a new id", out.Created, out.ID)
	}
	list, err := deps.Expenses.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("LoadMajorExpenses: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Mortgage" || list[0].ExpectedMax != 2100 {
		t.Errorf("stored = %+v", list)
	}
}

// TestUpsertLeavesUnmentionedFieldsAlone is the load-bearing test of this
// task: UpdateMajorExpense copies every field from its argument, so a sparse
// update assembled from a zero struct would blank keywords and the range.
func TestUpsertLeavesUnmentionedFieldsAlone(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"mortgage", "escrow"},
		ExpectedMin: 1900, ExpectedMax: 2100, Notes: "original",
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[upsertOutput](t, call(t, cs, "upsert_major_expense", map[string]any{
		"id": "me-mortgage", "notes": "refinanced 2026",
	}))
	if out.Created {
		t.Error("naming an existing id must update, not create")
	}
	list, _ := deps.Expenses.LoadMajorExpenses()
	got := list[0]
	if got.Notes != "refinanced 2026" {
		t.Errorf("notes = %q, want the update applied", got.Notes)
	}
	if len(got.Keywords) != 2 || got.ExpectedMin != 1900 || got.ExpectedMax != 2100 || got.Name != "Mortgage" {
		t.Errorf("an update mentioning only notes changed something else: %+v", got)
	}
}

func TestUpsertClearsKeywordsOnlyWithAnExplicitEmptyList(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-pinonly", Name: "Pin Only", Keywords: []string{"amazon"},
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	call(t, cs, "upsert_major_expense", map[string]any{"id": "me-pinonly", "keywords": []any{}})
	list, _ := deps.Expenses.LoadMajorExpenses()
	if len(list[0].Keywords) != 0 {
		t.Errorf("keywords = %v, want cleared by the explicit empty list", list[0].Keywords)
	}
}

func TestUpsertRejectsTheSameConfigurationsThePageRejects(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"no name on create", map[string]any{"keywords": []any{"x"}}, "name"},
		{"negative min", map[string]any{"name": "A", "expected_min": -5.0}, "negative"},
		{"min above max", map[string]any{"name": "A", "expected_min": 100.0, "expected_max": 10.0}, "exceed"},
		{"half a range with no keyword", map[string]any{"name": "A", "expected_min": 100.0}, "BOTH"},
		{"transfer with nothing to match", map[string]any{"name": "A", "is_internal_transfer": true}, "internal-transfer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := toolErrorText(t, call(t, cs, "upsert_major_expense", tc.args))
			if !strings.Contains(msg, tc.want) {
				t.Errorf("error %q does not mention %q", msg, tc.want)
			}
		})
	}
}

func TestUpsertAcceptsAPinOnlyTarget(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)
	out := decodeToolResult[upsertOutput](t, call(t, cs, "upsert_major_expense", map[string]any{
		"name": "Amazon — Books",
	}))
	if !out.Created {
		t.Error("a pin-only target (no keywords, no range) is a valid configuration on the page and must be here")
	}
}

func TestUpsertCanCreateAndPinInOneCall(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	cs := connect(t, deps)

	out := decodeToolResult[upsertOutput](t, call(t, cs, "upsert_major_expense", map[string]any{
		"name": "Roof", "pin_hash": roof.ComputeHash(),
	}))
	if !out.Pinned {
		t.Fatal("pin_hash should have been pinned to the new expense")
	}
	pins, _ := deps.Pins.LoadTransactionPins()
	if pins[roof.ComputeHash()] != out.ID {
		t.Errorf("pins = %+v, want the hash attached to %s", pins, out.ID)
	}
}

func TestUpsertRejectsAnUnknownID(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)
	msg := toolErrorText(t, call(t, cs, "upsert_major_expense", map[string]any{"id": "nope", "notes": "x"}))
	if !strings.Contains(msg, "nope") {
		t.Errorf("error should name the missing id, got: %s", msg)
	}
}
