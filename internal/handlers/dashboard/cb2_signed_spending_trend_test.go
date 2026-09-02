package dashboard

import (
	"testing"
	"time"

	"budget2/internal/models"
)

// CB2 amendment CB2-c, site 9: buildSpendingTrendChartData's MoM %-change
// must use the SIGNED negated net of each month's outflow bucket, not
// math.Abs, so a refund-dominant CURRENT month can swing the %-change PAST
// zero into negative territory. Direct models.Transaction fixtures with
// TransactionType set explicitly (fixture gotcha does not apply here).
// Amounts distinct from the CB2 oracle's 5000/300/800.
func TestBuildSpendingTrendChartData_CB2_SignedCurrentMonth(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		// Jan ordinary: net -1800 -> signed total 1800.
		{Description: "Rent", Amount: -1600, Date: jan, TransactionType: models.Outflow, Category: "Housing"},
		{Description: "Groceries", Amount: -200, Date: jan, TransactionType: models.Outflow, Category: "Food"},
		// Feb refund-dominant: net +700 -> signed total -700.
		{Description: "Groceries", Amount: -200, Date: feb, TransactionType: models.Outflow, Category: "Food"},
		{Description: "Refund Check", Amount: 900, Date: feb, TransactionType: models.Outflow, Category: "Misc"},
	})

	result := buildSpendingTrendChartData(ts)
	data := result["data"].([]map[string]interface{})
	if len(data) == 0 {
		t.Fatalf("harness error: no trace returned")
	}
	y := data[0]["y"].([]float64)
	if len(y) != 1 {
		t.Fatalf("harness error: y has %d entries, want 1", len(y))
	}

	// Feb %change = (-700-1800)/1800*100 = -138.8889 (signed curr swings
	// past zero into net-refund territory); per-month abs on curr (700)
	// gives (700-1800)/1800*100 = -61.1111.
	if !floatEqual(y[0], -138.8889) {
		t.Errorf("Feb %%change = %v, want -138.8889 (signed curr); per-month abs gives -61.1111", y[0])
	}
}

// CB2 amendment CB2-c: the `prev > 0` guard is UNCHANGED -- a
// refund-dominant month used as the BASE (prev <= 0) must still render 0%,
// an honest degradation this fix does not touch.
func TestBuildSpendingTrendChartData_CB2_RefundDominantBaseStaysZeroGuard(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		// Jan refund-dominant (the BASE month): net +700 -> signed total -700.
		{Description: "Groceries", Amount: -200, Date: jan, TransactionType: models.Outflow, Category: "Food"},
		{Description: "Refund Check", Amount: 900, Date: jan, TransactionType: models.Outflow, Category: "Misc"},
		// Feb ordinary: net -1800 -> signed total 1800.
		{Description: "Rent", Amount: -1600, Date: feb, TransactionType: models.Outflow, Category: "Housing"},
		{Description: "Groceries", Amount: -200, Date: feb, TransactionType: models.Outflow, Category: "Food"},
	})

	result := buildSpendingTrendChartData(ts)
	data := result["data"].([]map[string]interface{})
	if len(data) == 0 {
		t.Fatalf("harness error: no trace returned")
	}
	y := data[0]["y"].([]float64)
	if len(y) != 1 {
		t.Fatalf("harness error: y has %d entries, want 1", len(y))
	}

	if !floatEqual(y[0], 0) {
		t.Errorf("Feb %%change with refund-dominant BASE = %v, want 0 (prev>0 guard, honest degradation)", y[0])
	}
}
