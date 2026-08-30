package dashboard

import (
	"testing"
	"time"

	"budget2/internal/models"
)

// TestBuildBudgetVsActualChartData_PlanExclusionRemovesFlaggedSpendFromLivingAndCumulative
// is SY4 acceptance criterion 3/5(d): the budget-vs-actual chart's per-month
// Living bar values and cumulative-balance walk are a second surface
// (alongside metrics.Calculate) that computes a living actual directly from
// transactions -- it must consume the SAME plan-exclusion map, never
// re-classify locally.
func TestBuildBudgetVsActualChartData_PlanExclusionRemovesFlaggedSpendFromLivingAndCumulative(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1500, jan, models.Outflow, "Housing"),
		makeTransaction("Rent", -1500, feb, models.Outflow, "Housing"),
		makeTransaction("Premium", -400, jan, models.Outflow, "Health Insurance"),
		makeTransaction("Premium", -400, feb, models.Outflow, "Health Insurance"),
		models.Transaction{
			Description: "Lucid Loan Payment", Amount: -600, Date: jan,
			TransactionType: models.Outflow, Category: "Loan", Hash: "h-lucid",
		},
	)
	planExclusions := map[string]models.MajorExpense{
		"h-lucid": {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
	}

	result := buildBudgetVsActualChartData(ts, start, end, 1200, 350, start.AddDate(-1, 0, 0), true, planExclusions)

	data, ok := result["data"].([]map[string]interface{})
	if !ok || len(data) != 3 {
		t.Fatalf("data = %v, want 3 traces", result["data"])
	}

	// Living bar: Jan's raw living is 1500 (rent) + 600 (flagged Lucid) =
	// 2100, but the flagged $600 must drop out -- matching Feb, which never
	// had a flagged transaction.
	livingY := data[0]["y"].([]float64)
	if len(livingY) != 2 || !floatEqual(livingY[0], 1500) || !floatEqual(livingY[1], 1500) {
		t.Errorf("trace[0].y (Living) = %v, want [1500 1500] (Jan's flagged $600 Lucid payment must be excluded)", livingY)
	}

	// Cumulative balance walk recomputes spend from raw monthly outflows
	// independently of the Living bar -- it has its own subtraction to get
	// right. Combined target = 1550/mo; both months' true actual (excluding
	// the flag) is 1500 living + 400 healthcare = 1900, so both months
	// should show the SAME -350 per-month variance, cumulating to -700.
	cumY := data[2]["y"].([]float64)
	if len(cumY) != 2 || !floatEqual(cumY[0], -350) || !floatEqual(cumY[1], -700) {
		t.Errorf("trace[2].y (cumulative) = %v, want [-350 -700] (flagged spend excluded from the walk's spend term too)", cumY)
	}

	// Sanity check: without the exclusion, Jan's living figure would be
	// higher than Feb's by exactly the flagged amount -- proving this test
	// would actually catch a missing/broken exclusion, not just a tautology.
	withoutFlag := buildBudgetVsActualChartData(ts, start, end, 1200, 350, start.AddDate(-1, 0, 0), true, nil)
	livingYNoFlag := withoutFlag["data"].([]map[string]interface{})[0]["y"].([]float64)
	if !floatEqual(livingYNoFlag[0], 2100) {
		t.Fatalf("fixture sanity check: unflagged Jan living = %v, want 2100 (1500 rent + 600 Lucid, unexcluded)", livingYNoFlag[0])
	}
}

// TestBuildBudgetVsActualChartData_NetRefundGroupAddsBackNotSubtracts is the
// sign-divergent fixture required by ruling SY-2026-08-30c: Jan's flagged
// group is a NET REFUND ($800 payment + $1200 refund = net +$400). The
// attempt-1 math.Abs defect would over-subtract; the Living bar and the
// cumulative walk must both show Jan matching Feb's unflagged $1500.
func TestBuildBudgetVsActualChartData_NetRefundGroupAddsBackNotSubtracts(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	ts := models.NewTransactionSet([]models.Transaction{
		{Description: "Rent", Amount: -1500, Date: jan, TransactionType: models.Outflow, Category: "Housing"},
		{Description: "Rent", Amount: -1500, Date: feb, TransactionType: models.Outflow, Category: "Housing"},
		{Description: "Premium", Amount: -400, Date: jan, TransactionType: models.Outflow, Category: "Health Insurance"},
		{Description: "Premium", Amount: -400, Date: feb, TransactionType: models.Outflow, Category: "Health Insurance"},
		{Description: "Lucid Loan Payment", Amount: -800, Date: jan, TransactionType: models.Outflow, Category: "Loan", Hash: "h-jan-pay"},
		{Description: "Lucid Loan Refund", Amount: 1200, Date: jan, TransactionType: models.Outflow, Category: "Loan", Hash: "h-jan-refund"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-jan-pay":    {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
		"h-jan-refund": {ID: "lucid", Name: "Lucid Loan", ExcludeFromPlanSync: true},
	}

	result := buildBudgetVsActualChartData(ts, start, end, 1200, 350, start.AddDate(-1, 0, 0), true, planExclusions)
	data := result["data"].([]map[string]interface{})

	livingY := data[0]["y"].([]float64)
	if len(livingY) != 2 || !floatEqual(livingY[0], 1500) || !floatEqual(livingY[1], 1500) {
		t.Errorf("trace[0].y (Living) = %v, want [1500 1500] (Jan's net-refund flagged group must not reduce living below the unflagged rent)", livingY)
	}

	cumY := data[2]["y"].([]float64)
	if len(cumY) != 2 || !floatEqual(cumY[0], -350) || !floatEqual(cumY[1], -700) {
		t.Errorf("trace[2].y (cumulative) = %v, want [-350 -700] (walk's spend subtraction must also use the signed net)", cumY)
	}
}

// TestBuildBudgetVsActualChartData_RemainderNetsRefundLivingEqualsAbsRemainder
// is the required remainder-sign-divergent fixture (ruling SY-2026-08-30d):
// Jan's REMAINDER (non-flagged, non-HI spend) nets a refund -- a $1000
// grocery outflow plus a $4000 outflow-typed credit (net +$3000) -- beside
// an ordinary flagged $500 car payment. The Living bar must show |remainder|
// = 3000 exactly, not the attempt-2 defect's 2000.
func TestBuildBudgetVsActualChartData_RemainderNetsRefundLivingEqualsAbsRemainder(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	ts := models.NewTransactionSet([]models.Transaction{
		{Description: "Groceries", Amount: -1000, Date: jan, TransactionType: models.Outflow, Category: "Food", Hash: "h-jan-grocery"},
		{Description: "Misc Credit", Amount: 4000, Date: jan, TransactionType: models.Outflow, Category: "Misc", Hash: "h-jan-credit"},
		{Description: "Car Payment", Amount: -500, Date: jan, TransactionType: models.Outflow, Category: "Loan", Hash: "h-jan-car"},
		{Description: "Rent", Amount: -1500, Date: feb, TransactionType: models.Outflow, Category: "Housing", Hash: "h-feb-rent"},
	})
	planExclusions := map[string]models.MajorExpense{
		"h-jan-car": {ID: "car", Name: "Car Loan", ExcludeFromPlanSync: true},
	}

	result := buildBudgetVsActualChartData(ts, start, end, 1200, 0, start.AddDate(-1, 0, 0), true, planExclusions)
	data := result["data"].([]map[string]interface{})

	livingY := data[0]["y"].([]float64)
	if len(livingY) != 2 || !floatEqual(livingY[0], 3000) || !floatEqual(livingY[1], 1500) {
		t.Errorf("trace[0].y (Living) = %v, want [3000 1500] (Jan's |remainder| exactly, not the attempt-2 defect's 2000)", livingY)
	}
}
