package spend

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/metrics"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// planExclusionFixture is a one-month ledger with a flagged major expense
// (a Lucid loan payment) alongside ordinary living spend, used to exercise
// summarize_spending's budget block against SY4's plan-sync exclusion.
func planExclusionFixture() *models.TransactionSet {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	return models.NewTransactionSet([]models.Transaction{
		{Hash: "h-rent", Date: day("2026-01-05"), Description: "Rent", Category: "Housing", Amount: -1500, TransactionType: models.Outflow},
		{Hash: "h-lucid", Date: day("2026-01-20"), Description: "Lucid Loan Payment", Category: "Loan", Amount: -600, TransactionType: models.Outflow},
	})
}

// TestSummarizeSpendingBudgetBlockReflectsPlanSyncExclusion is SY4
// acceptance criterion 5(c): the MCP summarize_spending budget block's
// living_monthly_actual must exclude spend the plan already models
// separately (ExcludeFromPlanSync) -- the SAME exclusion the what-if
// dashboard sync applies -- wired through Deps.MajorExpenses via
// majorexpenses.ComputePlanSyncExclusions, never a local reclassification.
func TestSummarizeSpendingBudgetBlockReflectsPlanSyncExclusion(t *testing.T) {
	sm := newSummaryTestManager(t, 1200, 0)
	flaggedDefs := stubMajorExpenses{
		defs: []models.MajorExpense{
			{ID: "lucid", Name: "Lucid Loan", Keywords: []string{"Lucid"}, ExcludeFromPlanSync: true},
		},
	}
	cs := connect(t, Deps{
		Transactions:  stubTransactions{ts: planExclusionFixture()},
		Settings:      sm,
		MajorExpenses: flaggedDefs,
	})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "summarize_spending",
		Arguments: map[string]any{
			"start_date": "2026-01-01",
			"end_date":   "2026-01-31",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("summarize_spending returned an error: %+v", res.Content)
	}

	var out summaryOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Budget == nil {
		t.Fatal("budget = nil, want populated when a living target is configured")
	}

	// total_expenses (money really spent) is UNCHANGED by the flag -- only
	// the living-vs-target comparison excludes it.
	if out.TotalExpenses != 2100 {
		t.Errorf("total_expenses = %v, want 2100 (1500 rent + 600 Lucid; the flag never hides real spend)", out.TotalExpenses)
	}

	start, _ := time.Parse("2006-01-02", "2026-01-01")
	end, _ := time.Parse("2006-01-02", "2026-01-31")
	months := metrics.MonthsBetween(start, end)
	wantLivingActual := math.Round((1500/months)*100) / 100

	if math.Abs(out.Budget.LivingActual-wantLivingActual) > 0.01 {
		t.Errorf("budget.living_monthly_actual = %v, want ~%v (1500 rent only -- the flagged $600 Lucid payment excluded)",
			out.Budget.LivingActual, wantLivingActual)
	}

	// Sanity check (both-ends): without the MajorExpenses dependency wired,
	// the classifier never runs and living_monthly_actual includes the
	// full, unexcluded 2100 -- proving the assertion above would actually
	// catch a missing/broken wiring, not just restate a tautology.
	csNoFlag := connect(t, Deps{Transactions: stubTransactions{ts: planExclusionFixture()}, Settings: sm})
	resNoFlag, err := csNoFlag.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "summarize_spending",
		Arguments: map[string]any{"start_date": "2026-01-01", "end_date": "2026-01-31"},
	})
	if err != nil {
		t.Fatalf("CallTool (no MajorExpenses wired): %v", err)
	}
	var outNoFlag summaryOutput
	if err := json.Unmarshal(mustJSON(t, resNoFlag.StructuredContent), &outNoFlag); err != nil {
		t.Fatalf("decode structured content (no MajorExpenses wired): %v", err)
	}
	wantLivingActualNoFlag := math.Round((2100/months)*100) / 100
	if math.Abs(outNoFlag.Budget.LivingActual-wantLivingActualNoFlag) > 0.01 {
		t.Errorf("budget.living_monthly_actual (no MajorExpenses wired) = %v, want ~%v (2100, unexcluded)",
			outNoFlag.Budget.LivingActual, wantLivingActualNoFlag)
	}
}

// TestSummarizeSpendingBudgetBlockNetRefundGroupAddsBackNotSubtracts is the
// sign-divergent fixture required by ruling SY-2026-08-30c: the flagged
// Lucid group is a NET REFUND ($800 payment + $1200 refund = net +$400).
// living_monthly_actual must equal the unflagged rent spend exactly --
// the attempt-1 math.Abs defect would have over-subtracted here.
func TestSummarizeSpendingBudgetBlockNetRefundGroupAddsBackNotSubtracts(t *testing.T) {
	sm := newSummaryTestManager(t, 1200, 0)
	flaggedDefs := stubMajorExpenses{
		defs: []models.MajorExpense{
			{ID: "lucid", Name: "Lucid Loan", Keywords: []string{"Lucid"}, ExcludeFromPlanSync: true},
		},
	}
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-rent", Date: day("2026-01-05"), Description: "Rent", Category: "Housing", Amount: -1500, TransactionType: models.Outflow},
		{Hash: "h-lucid-pay", Date: day("2026-01-15"), Description: "Lucid Loan Payment", Category: "Loan", Amount: -800, TransactionType: models.Outflow},
		{Hash: "h-lucid-refund", Date: day("2026-01-20"), Description: "Lucid Loan Refund", Category: "Loan", Amount: 1200, TransactionType: models.Outflow},
	})
	cs := connect(t, Deps{Transactions: stubTransactions{ts: ts}, Settings: sm, MajorExpenses: flaggedDefs})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "summarize_spending",
		Arguments: map[string]any{"start_date": "2026-01-01", "end_date": "2026-01-31"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("summarize_spending returned an error: %+v", res.Content)
	}
	var out summaryOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Budget == nil {
		t.Fatal("budget = nil, want populated")
	}

	start, _ := time.Parse("2006-01-02", "2026-01-01")
	end, _ := time.Parse("2006-01-02", "2026-01-31")
	months := metrics.MonthsBetween(start, end)
	wantLivingActual := math.Round((1500/months)*100) / 100

	if math.Abs(out.Budget.LivingActual-wantLivingActual) > 0.01 {
		t.Errorf("budget.living_monthly_actual = %v, want ~%v (rent only -- a net-refund flagged group must never reduce living below the unflagged spend)",
			out.Budget.LivingActual, wantLivingActual)
	}
}

// TestSummarizeSpendingBudgetBlockRemainderNetsRefundLivingEqualsAbsRemainder
// is the required remainder-sign-divergent fixture (ruling SY-2026-08-30d):
// the checker-second probe verbatim through the MCP tool -- a $1000 grocery
// outflow plus a $4000 outflow-typed credit (remainder nets +$3000) beside
// an ordinary flagged $500 car payment. living_monthly_actual must equal
// |remainder| exactly, not the attempt-2 defect's 2000-class value.
func TestSummarizeSpendingBudgetBlockRemainderNetsRefundLivingEqualsAbsRemainder(t *testing.T) {
	sm := newSummaryTestManager(t, 1200, 0)
	flaggedDefs := stubMajorExpenses{
		defs: []models.MajorExpense{
			{ID: "car", Name: "Car Loan", Keywords: []string{"Car"}, ExcludeFromPlanSync: true},
		},
	}
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-grocery", Date: day("2026-01-05"), Description: "Groceries", Category: "Food", Amount: -1000, TransactionType: models.Outflow},
		{Hash: "h-credit", Date: day("2026-01-10"), Description: "Misc Credit", Category: "Misc", Amount: 4000, TransactionType: models.Outflow},
		{Hash: "h-car", Date: day("2026-01-20"), Description: "Car Payment", Category: "Loan", Amount: -500, TransactionType: models.Outflow},
	})
	cs := connect(t, Deps{Transactions: stubTransactions{ts: ts}, Settings: sm, MajorExpenses: flaggedDefs})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "summarize_spending",
		Arguments: map[string]any{"start_date": "2026-01-01", "end_date": "2026-01-31"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("summarize_spending returned an error: %+v", res.Content)
	}
	var out summaryOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Budget == nil {
		t.Fatal("budget = nil, want populated")
	}

	start, _ := time.Parse("2006-01-02", "2026-01-01")
	end, _ := time.Parse("2006-01-02", "2026-01-31")
	months := metrics.MonthsBetween(start, end)
	wantLivingActual := math.Round((3000/months)*100) / 100

	if math.Abs(out.Budget.LivingActual-wantLivingActual) > 0.01 {
		t.Errorf("budget.living_monthly_actual = %v, want ~%v (|remainder| exactly)",
			out.Budget.LivingActual, wantLivingActual)
	}
}
