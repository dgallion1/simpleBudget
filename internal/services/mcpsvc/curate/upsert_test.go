package curate

import (
	"os"
	"path/filepath"
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

// TestUpsertSkipsThePinWhenItsSnapshotCannotBeTaken covers the failure
// scenario the fix targets: transaction_pins.json already exists (so a
// missing-file skip does not apply -- that case is covered separately by
// TestUpsertCanCreateAndPinInOneCall, which never seeds the file at all) but
// cannot be read. The pin write must be skipped, not silently proceed with no
// backup, while the definition update itself still goes through.
func TestUpsertSkipsThePinWhenItsSnapshotCannotBeTaken(t *testing.T) {
	deps, dir := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"mortgage"}, Notes: "original",
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	if _, err := deps.Pins.SetTransactionPins(map[string]string{"seed": "me-other"}); err != nil {
		t.Fatalf("seed pins: %v", err)
	}
	pinsPath := filepath.Join(dir, "transaction_pins.json")
	before, err := os.ReadFile(pinsPath)
	if err != nil {
		t.Fatalf("read pins before: %v", err)
	}

	// Swap the file for an empty directory of the same name instead of
	// chmod 0o000. Ensure's first move is os.ReadFile(src); a directory
	// there fails that call with EISDIR at any uid, including root -- unlike
	// chmod 0000, which root's CAP_DAC_OVERRIDE lets it read straight
	// through. EISDIR also deliberately does NOT satisfy errors.Is(err,
	// fs.ErrNotExist) (see upsert.go's check just below): a dangling-symlink
	// (ENOENT) injection would be indistinguishable from "no prior pins
	// file" and get tolerated instead of skipping the pin, which is the
	// opposite of what this test defends. The file genuinely exists on disk
	// throughout (as a directory standing in for it here, then restored to
	// its real content below), matching the test's premise that
	// transaction_pins.json "already exists... but cannot be read."
	if err := os.Remove(pinsPath); err != nil {
		t.Fatalf("remove transaction_pins.json: %v", err)
	}
	if err := os.Mkdir(pinsPath, 0o755); err != nil {
		t.Fatalf("mkdir transaction_pins.json placeholder: %v", err)
	}
	cs := connect(t, deps)

	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	out := decodeToolResult[upsertOutput](t, call(t, cs, "upsert_major_expense", map[string]any{
		"id": "me-mortgage", "notes": "refinanced 2026", "pin_hash": roof.ComputeHash(),
	}))
	if out.Pinned {
		t.Error("the pin must not be written when its snapshot cannot be taken")
	}
	if out.PinSnapshotPath != "" {
		t.Errorf("pin_snapshot_path = %q, want empty since nothing was snapshotted", out.PinSnapshotPath)
	}
	if out.Note == "" {
		t.Error("expected a note explaining the pin was skipped")
	}

	// The definition update must have gone through regardless.
	list, _ := deps.Expenses.LoadMajorExpenses()
	if list[0].Notes != "refinanced 2026" {
		t.Errorf("notes = %q, want the update applied despite the pin skip", list[0].Notes)
	}

	// Put the real file back before reading it again below.
	if err := os.Remove(pinsPath); err != nil {
		t.Fatalf("remove transaction_pins.json placeholder: %v", err)
	}
	if err := os.WriteFile(pinsPath, before, 0o644); err != nil {
		t.Fatalf("restore transaction_pins.json: %v", err)
	}
	after, err := os.ReadFile(pinsPath)
	if err != nil {
		t.Fatalf("read pins after: %v", err)
	}
	if string(after) != string(before) {
		t.Error("transaction_pins.json changed despite the backup failing")
	}
}

// TestUpsertMergesIsInternalTransferOnUpdate is the load-bearing test for
// merging a bool field that defaults false: an unconditional assignment from
// a zero-value input struct would blank it and pass unnoticed, since the
// existing fixtures never seed it true.
func TestUpsertMergesIsInternalTransferOnUpdate(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-transfer", Name: "Savings Transfer", Keywords: []string{"transfer"}, IsInternalTransfer: true,
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	call(t, cs, "upsert_major_expense", map[string]any{"id": "me-transfer", "notes": "checked"})
	list, _ := deps.Expenses.LoadMajorExpenses()
	if !list[0].IsInternalTransfer {
		t.Error("is_internal_transfer must survive an update that does not mention it")
	}
	if list[0].Notes != "checked" {
		t.Errorf("notes = %q, want the update applied", list[0].Notes)
	}
}

// TestUpsertClearsAnAmountBoundWithExplicitZero covers passing 0 explicitly,
// distinct from omitting the field (which leaves it alone).
func TestUpsertClearsAnAmountBoundWithExplicitZero(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"mortgage"}, ExpectedMin: 1900, ExpectedMax: 2100,
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	call(t, cs, "upsert_major_expense", map[string]any{"id": "me-mortgage", "expected_max": 0.0})
	list, _ := deps.Expenses.LoadMajorExpenses()
	if list[0].ExpectedMax != 0 {
		t.Errorf("expected_max = %v, want cleared by the explicit 0", list[0].ExpectedMax)
	}
	if list[0].ExpectedMin != 1900 {
		t.Errorf("expected_min = %v, want left alone", list[0].ExpectedMin)
	}
}
