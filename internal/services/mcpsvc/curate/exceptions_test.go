package curate

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestListExceptionsSplitsTheThreeBuckets(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	// A range that the 2000 payments sit outside of, so both are anomalous.
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"mortgage"},
		ExpectedMin: 100, ExpectedMax: 500,
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[listExceptionsOutput](t, call(t, cs, "list_exceptions", map[string]any{}))

	if out.Unmatched == nil || out.Unmatched.Total != 1 {
		t.Fatalf("unmatched = %+v, want 1 row (ACME ROOFING)", out.Unmatched)
	}
	if !out.Unmatched.Rows[0].OverThreshold {
		t.Errorf("a 4500 unmatched outflow must be flagged over the %v threshold", out.Threshold)
	}
	if out.Unmatched.Rows[0].Hash == "" {
		t.Error("exception rows must carry the hash pin_transactions needs")
	}
	if out.Anomalous == nil || out.Anomalous.Total == 0 {
		t.Fatalf("anomalous = %+v, want the out-of-range mortgage payments", out.Anomalous)
	}
	if out.Anomalous.Rows[0].MajorExpenseName != "Mortgage" {
		t.Errorf("anomalous row names %q, want Mortgage", out.Anomalous.Rows[0].MajorExpenseName)
	}
	if out.NewMerchants == nil {
		t.Error("new_merchants bucket missing")
	}
}

func TestListExceptionsFiltersOneBucketByTextAndAmount(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)

	out := decodeToolResult[listExceptionsOutput](t, call(t, cs, "list_exceptions", map[string]any{
		"bucket": "unmatched",
		"search": "roofing",
	}))
	if out.Anomalous != nil || out.NewMerchants != nil {
		t.Error("naming one bucket must omit the others")
	}
	if out.Unmatched.Total != 1 || !strings.Contains(strings.ToLower(out.Unmatched.Rows[0].Description), "roofing") {
		t.Errorf("search did not narrow to the roofing row: %+v", out.Unmatched)
	}

	none := decodeToolResult[listExceptionsOutput](t, call(t, cs, "list_exceptions", map[string]any{
		"bucket": "unmatched", "min_amount": 100000,
	}))
	if none.Unmatched.Total != 0 {
		t.Errorf("min_amount 100000 should have matched nothing, got %d", none.Unmatched.Total)
	}
	if none.Note == "" {
		t.Error("an empty result must carry an explanatory note rather than looking like a bug")
	}
}

func TestListExceptionsCapsRowsAndReportsTheTrueTotal(t *testing.T) {
	txns := make([]models.Transaction, 0, 60)
	for i := 0; i < 60; i++ {
		txns = append(txns, models.Transaction{
			Date: day(2026, 1, 1).AddDate(0, 0, i), Description: "UNKNOWN VENDOR " + string(rune('A'+i%26)),
			Category: "Misc", Amount: float64(-10 - i), TransactionType: models.Outflow,
		})
	}
	deps, _ := newDeps(t, txns)
	cs := connect(t, deps)

	out := decodeToolResult[listExceptionsOutput](t, call(t, cs, "list_exceptions", map[string]any{
		"bucket": "unmatched", "limit": 5,
	}))
	if out.Unmatched.Total != 60 {
		t.Errorf("total = %d, want the full 60 matches, not the returned count", out.Unmatched.Total)
	}
	if out.Unmatched.Returned != 5 || len(out.Unmatched.Rows) != 5 {
		t.Errorf("returned = %d / %d rows, want 5", out.Unmatched.Returned, len(out.Unmatched.Rows))
	}
}

func TestListExceptionsRejectsAnUnknownBucket(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)
	msg := toolErrorText(t, call(t, cs, "list_exceptions", map[string]any{"bucket": "weird"}))
	if !strings.Contains(msg, "weird") {
		t.Errorf("error should name the bad bucket, got: %s", msg)
	}
}
