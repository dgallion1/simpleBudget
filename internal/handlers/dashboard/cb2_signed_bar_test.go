package dashboard

import (
	"testing"
	"time"

	"budget2/internal/models"
)

// CB2 regression: buildBudgetVsActualChartData's Living and Healthcare bar
// traces must carry the SIGNED negated net of their month bucket, not
// math.Abs'd. A refund-dominant month must render as a NEGATIVE bar (a
// credit).
//
// Two-month fixture (amounts distinct from the CB2 oracle's 5000/300/800):
// Jan is ordinary; Feb is refund-dominant in both the living bucket (a
// furniture-store credit exceeds the month's grocery spend) and the
// healthcare bucket (an insurance overpayment return, the only HI row that
// month).
func TestBuildBudgetVsActualChartData_CB2_LivingAndHealthcareSigned(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	ts := models.NewTransactionSet([]models.Transaction{
		{Description: "Rent", Amount: -1200, Date: jan, TransactionType: models.Outflow, Category: "Housing"},
		{Description: "Groceries", Amount: -200, Date: jan, TransactionType: models.Outflow, Category: "Food"},
		{Description: "Health Premium", Amount: -300, Date: jan, TransactionType: models.Outflow, Category: "Health Insurance"},

		{Description: "Groceries", Amount: -200, Date: feb, TransactionType: models.Outflow, Category: "Food"},
		{Description: "Boutique Store Credit", Amount: 900, Date: feb, TransactionType: models.Outflow, Category: "Furniture"},
		{Description: "Insurance Overpayment Return", Amount: 600, Date: feb, TransactionType: models.Outflow, Category: "Health Insurance"},
	})

	result := buildBudgetVsActualChartData(ts, start, end, 1200, 300, start.AddDate(-1, 0, 0), true, nil)
	data := result["data"].([]map[string]interface{})

	livingY := data[0]["y"].([]float64)
	if data[0]["name"] != "Living" {
		t.Fatalf("harness error: data[0] is %v, want Living trace", data[0]["name"])
	}
	// Jan living = -1200-200 = -1400 -> signed +1400 (ordinary, unaffected).
	// Feb living = -200+900 = +700 -> signed -700 (refund-dominant, a credit).
	if len(livingY) != 2 || !floatEqual(livingY[0], 1400) || !floatEqual(livingY[1], -700) {
		t.Errorf("trace[0].y (Living) = %v, want [1400 -700] (Feb signed negative; per-month abs gives 700)", livingY)
	}

	healthcareY := data[1]["y"].([]float64)
	if data[1]["name"] != "Healthcare" {
		t.Fatalf("harness error: data[1] is %v, want Healthcare trace", data[1]["name"])
	}
	// Jan healthcare = -300 -> signed +300 (ordinary, unaffected).
	// Feb healthcare = +600 -> signed -600 (refund-dominant, a credit).
	if len(healthcareY) != 2 || !floatEqual(healthcareY[0], 300) || !floatEqual(healthcareY[1], -600) {
		t.Errorf("trace[1].y (Healthcare) = %v, want [300 -600] (Feb signed negative; per-month abs gives 600)", healthcareY)
	}
}
