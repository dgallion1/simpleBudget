package spend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newSummaryTestManager writes one scenario file with a spending target and
// returns a settings manager rooted at it, mirroring plan's newTestManager.
func newSummaryTestManager(t *testing.T, monthlyLivingExpenses, monthlyHealthcare float64) *retirement.SettingsManager {
	t.Helper()
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := map[string]any{
		"name":                    "Base",
		"portfolio_value":         1000000.0,
		"projection_years":        30,
		"monthly_living_expenses": monthlyLivingExpenses,
		"monthly_healthcare":      monthlyHealthcare,
		"healthcare_start_years":  0,
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "whatif.json"), data, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return retirement.NewSettingsManager(settingsDir, store)
}

// manyMerchantFixture is 12 outflows across 12 distinct merchants/categories
// plus one income row, used to exercise top_n truncation.
func manyMerchantFixture() *models.TransactionSet {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	names := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot",
		"Golf", "Hotel", "India", "Juliet", "Kilo", "Lima"}
	txns := make([]models.Transaction, 0, len(names)+1)
	for i, n := range names {
		amt := -float64((len(names) - i) * 10)
		txns = append(txns, models.Transaction{
			Date: day("2026-01-10"), Description: n, Category: n,
			Amount: amt, TransactionType: models.Outflow,
		})
	}
	txns = append(txns, models.Transaction{
		Date: day("2026-01-01"), Description: "PAYCHECK", Category: "Income",
		Amount: 5000, TransactionType: models.Income,
	})
	return models.NewTransactionSet(txns)
}

func TestSummarizeSpendingTotalsByCategoryAndMonth(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "summarize_spending",
		Arguments: map[string]any{},
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
	if out.TotalIncome != 5000 {
		t.Errorf("total_income = %v, want 5000", out.TotalIncome)
	}
	// 15.99 + 204.10 + 15.99, reported as a positive figure.
	if out.TotalExpenses != 236.08 {
		t.Errorf("total_expenses = %v, want 236.08", out.TotalExpenses)
	}

	byCat := map[string]float64{}
	for _, c := range out.ByCategory {
		byCat[c.Category] = c.Amount
	}
	if byCat["Entertainment"] != 31.98 {
		t.Errorf("Entertainment = %v, want 31.98", byCat["Entertainment"])
	}
	if len(out.ByMonth) != 2 {
		t.Errorf("by_month has %d entries, want 2: %+v", len(out.ByMonth), out.ByMonth)
	}
}

// With no settings manager there is no plan to compare against; the tool must
// still answer, omitting the budget block rather than failing or reporting a
// zero target as if it were real.
func TestSummarizeSpendingOmitsBudgetWhenNoSettingsAreWired(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "summarize_spending",
		Arguments: map[string]any{},
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
	if out.Budget != nil {
		t.Errorf("budget = %+v, want nil when no settings manager is wired", out.Budget)
	}
}

// TestSummarizeSpendingPopulatesBudgetWhenSettingsHaveATarget is the
// counterpart to the omission tests above: with a settings manager wired to
// a scenario that sets a spending target, the budget block appears and
// carries the plan's living target through metrics.BudgetTargets +
// metrics.Calculate.
func TestSummarizeSpendingPopulatesBudgetWhenSettingsHaveATarget(t *testing.T) {
	sm := newSummaryTestManager(t, 200, 0) // living target $200/mo, no healthcare target
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}, Settings: sm})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "summarize_spending",
		Arguments: map[string]any{},
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
	if out.Budget.LivingTarget != 200 {
		t.Errorf("living_monthly_target = %v, want 200", out.Budget.LivingTarget)
	}
	if out.Budget.HealthcareTarget != 0 {
		t.Errorf("healthcare_monthly_target = %v, want 0 (not configured)", out.Budget.HealthcareTarget)
	}
}

// TestSummarizeSpendingTopNTruncatesCategoryAndMerchantOnly verifies top_n
// limits by_category and by_merchant but never by_month, per the brief.
func TestSummarizeSpendingTopNTruncatesCategoryAndMerchantOnly(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: manyMerchantFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "summarize_spending",
		Arguments: map[string]any{"top_n": 3},
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
	if len(out.ByCategory) != 3 {
		t.Errorf("by_category has %d entries, want 3 (top_n)", len(out.ByCategory))
	}
	if len(out.ByMerchant) != 3 {
		t.Errorf("by_merchant has %d entries, want 3 (top_n)", len(out.ByMerchant))
	}
	// Highest-amount merchant ("Alpha", $120) must be first: sorted by
	// amount descending.
	if out.ByMerchant[0].Merchant != "alpha" {
		t.Errorf("by_merchant[0] = %q, want %q (highest amount first)", out.ByMerchant[0].Merchant, "alpha")
	}
	if len(out.ByMonth) == 0 {
		t.Errorf("by_month is empty, want the single month present regardless of top_n")
	}
}

// TestSummarizeSpendingRespectsExplicitWindow verifies start_date/end_date
// narrow both the totals and the window echoed back in the output.
func TestSummarizeSpendingRespectsExplicitWindow(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}})

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
	if out.Start != "2026-01-01" || out.End != "2026-01-31" {
		t.Errorf("window = [%s, %s], want [2026-01-01, 2026-01-31]", out.Start, out.End)
	}
	// Only the Jan Netflix ($15.99) and Safeway ($204.10) outflows fall in
	// this window; Feb's Netflix and the paycheck are excluded.
	if out.TotalIncome != 0 {
		t.Errorf("total_income = %v, want 0 (paycheck is in Feb, outside the window)", out.TotalIncome)
	}
	if out.TotalExpenses != 220.09 {
		t.Errorf("total_expenses = %v, want 220.09 (15.99 + 204.10)", out.TotalExpenses)
	}
}

// TestSummarizeSpendingRejectsAnInvalidDate mirrors search_transactions'
// invalid-date handling: a malformed date is a tool error, not a panic or a
// silently-ignored filter.
func TestSummarizeSpendingRejectsAnInvalidDate(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "summarize_spending",
		Arguments: map[string]any{"start_date": "01/05/2026"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("summarize_spending should have reported the invalid date as a tool error")
	}
}

// TestSummarizeSpendingReportsALoadFailureAsAToolError mirrors
// get_anomalies' load-failure handling.
func TestSummarizeSpendingReportsALoadFailureAsAToolError(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{err: errors.New("storage is locked")}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "summarize_spending",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("summarize_spending should have reported the load failure as a tool error")
	}
}
