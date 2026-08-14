package curate

import (
	"testing"
	"time"

	"budget2/internal/models"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ledger is a small fixture: two mortgage payments, one mortgage refund, one
// unmatched large charge, and one income row that mentions "mortgage" and
// must therefore NOT be counted against the mortgage expense.
func ledger() []models.Transaction {
	return []models.Transaction{
		{Date: day(2026, 1, 5), Description: "MORTGAGE PAYMENT", Category: "Housing", Amount: -2000, TransactionType: models.Outflow},
		{Date: day(2026, 2, 5), Description: "MORTGAGE PAYMENT", Category: "Housing", Amount: -2000, TransactionType: models.Outflow},
		{Date: day(2026, 2, 9), Description: "MORTGAGE ESCROW REFUND", Category: "Housing", Amount: 300, TransactionType: models.Outflow},
		{Date: day(2026, 2, 14), Description: "ACME ROOFING", Category: "Home", Amount: -4500, TransactionType: models.Outflow},
		{Date: day(2026, 2, 20), Description: "MORTGAGE ESCROW DEPOSIT", Category: "Income", Amount: 1200, TransactionType: models.Income},
	}
}

func TestListMajorExpensesReportsNetSpendAndMatchCounts(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"mortgage"},
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[listExpensesOutput](t, call(t, cs, "list_major_expenses",
		map[string]any{"include_transactions": true}))

	if len(out.Expenses) != 1 {
		t.Fatalf("got %d expenses, want 1", len(out.Expenses))
	}
	e := out.Expenses[0]
	// Three outflows match "mortgage"; the Income row does not, because the
	// outflow filter runs before matching.
	if e.Count != 3 {
		t.Errorf("count = %d, want 3 (two payments + one refund, income excluded)", e.Count)
	}
	// Net spend: -(-2000) + -(-2000) + -(300) = 3700.
	if e.Total != 3700 {
		t.Errorf("total = %v, want 3700 (net of the 300 refund)", e.Total)
	}
	// Per-transaction amounts stay signed exactly as stored.
	var sawRefund bool
	for _, r := range e.Transactions {
		if r.Amount == 300 {
			sawRefund = true
		}
	}
	if !sawRefund {
		t.Errorf("expected the +300 refund row reported with its stored sign, got %+v", e.Transactions)
	}
	if out.UnmatchedCount != 1 || out.UnmatchedTotal != 4500 {
		t.Errorf("unmatched = (%d, %v), want (1, 4500)", out.UnmatchedCount, out.UnmatchedTotal)
	}
	if out.TotalDeclared != 3700 {
		t.Errorf("total_declared = %v, want 3700", out.TotalDeclared)
	}
}

func TestListMajorExpensesCountsPinsAndHonorsTheWindow(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-roof", Name: "Roof", Keywords: nil,
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	if _, err := deps.Pins.SetTransactionPins(map[string]string{roof.ComputeHash(): "me-roof"}); err != nil {
		t.Fatalf("SetTransactionPins: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[listExpensesOutput](t, call(t, cs, "list_major_expenses", map[string]any{
		"start_date": "2026-02-01", "end_date": "2026-02-28",
	}))
	if len(out.Expenses) != 1 {
		t.Fatalf("got %d expenses, want 1", len(out.Expenses))
	}
	if out.Expenses[0].PinnedCount != 1 || out.Expenses[0].Count != 1 {
		t.Errorf("count/pinned = %d/%d, want 1/1", out.Expenses[0].Count, out.Expenses[0].PinnedCount)
	}
	if out.Start != "2026-02-01" || out.End != "2026-02-28" {
		t.Errorf("window = %s..%s, want 2026-02-01..2026-02-28", out.Start, out.End)
	}
}

func TestListMajorExpensesReportsTheSoftDeleteArchiveOnRequest(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-gone", Name: "Retired Thing", Keywords: []string{"nothing"},
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	if err := deps.Expenses.ArchiveMajorExpense("me-gone"); err != nil {
		t.Fatalf("ArchiveMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	without := decodeToolResult[listExpensesOutput](t, call(t, cs, "list_major_expenses", map[string]any{}))
	if len(without.Deleted) != 0 {
		t.Errorf("deleted archive returned without include_deleted: %+v", without.Deleted)
	}
	with := decodeToolResult[listExpensesOutput](t, call(t, cs, "list_major_expenses",
		map[string]any{"include_deleted": true}))
	if len(with.Deleted) != 1 || with.Deleted[0].ID != "me-gone" {
		t.Errorf("deleted = %+v, want one entry me-gone", with.Deleted)
	}
}

func TestListMajorExpensesRejectsABadDate(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)
	msg := toolErrorText(t, call(t, cs, "list_major_expenses", map[string]any{"start_date": "March 2026"}))
	if msg == "" {
		t.Fatal("expected an error naming the bad date")
	}
}
