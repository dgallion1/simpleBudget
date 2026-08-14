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
		t.Fatalf("by_month has %d entries, want 2: %+v", len(out.ByMonth), out.ByMonth)
	}
	// Pins the sign: MonthlyTotals sums the signed amount (expenses
	// negative), so byMonthRows must math.Abs it. Without that, this would
	// assert -15.99 and still pass -- the length check alone can't catch a
	// deleted Abs.
	byMonth := map[string]float64{}
	for _, m := range out.ByMonth {
		byMonth[m.Month] = m.Amount
	}
	if byMonth["2026-01"] != 220.09 { // Netflix 15.99 + Safeway 204.10
		t.Errorf("by_month[2026-01] = %v, want 220.09 (positive)", byMonth["2026-01"])
	}
	if byMonth["2026-02"] != 15.99 { // Netflix only; the Feb paycheck is income, not counted here
		t.Errorf("by_month[2026-02] = %v, want 15.99 (positive)", byMonth["2026-02"])
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

// TestSummarizeSpendingRejectsDefaultingAnEmptyLedger guards Finding 9 of
// the Phase 2 review: before the fix, an empty (post-suppression) ledger
// with no explicit dates defaulted from ts.MinDate()/MaxDate()'s zero time,
// reporting start/end as "0001-01-01" instead of surfacing that there was
// nothing to default from. get_trends already got this right
// (TestGetTrendsRejectsDefaultingAnEmptyLedger); this brings
// summarize_spending in line with it.
func TestSummarizeSpendingRejectsDefaultingAnEmptyLedger(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: &models.TransactionSet{}}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "summarize_spending",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("summarize_spending should have reported an empty ledger as a tool error when defaulting the window")
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

// TestSummarizeSpendingOmitsBudgetWhenTargetsAreZero is the case the brief
// called out explicitly: a settings manager IS wired and loads successfully,
// but the scenario has no living or healthcare target configured (both 0).
// This exercises the HasBudgetTarget||HasHealthcareTarget gate directly,
// distinct from TestSummarizeSpendingOmitsBudgetWhenNoSettingsAreWired above
// (which exercises deps.Settings == nil, a different code path).
func TestSummarizeSpendingOmitsBudgetWhenTargetsAreZero(t *testing.T) {
	sm := newSummaryTestManager(t, 0, 0)
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
	if out.Budget != nil {
		t.Errorf("budget = %+v, want nil when both targets are 0 (unset, not a real $0 target)", out.Budget)
	}
}

// TestSummarizeSpendingExcludesSuppressedTransactions guards the fix for a
// review finding: this tool wraps metrics.Calculate, the same function the
// dashboard uses, so a resolved duplicate left in would make this tool's
// totals contradict the dashboard screen for the same window.
func TestSummarizeSpendingExcludesSuppressedTransactions(t *testing.T) {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	ts := models.NewTransactionSet([]models.Transaction{
		{Date: day("2026-01-05"), Description: "NETFLIX", Category: "Entertainment", Amount: -15.99, TransactionType: models.Outflow},
		{Date: day("2026-01-05"), Description: "NETFLIX", Category: "Entertainment", Amount: -15.99, TransactionType: models.Outflow, Suppressed: true},
	})
	cs := connect(t, Deps{Transactions: stubTransactions{ts: ts}})

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
	if out.TotalExpenses != 15.99 {
		t.Errorf("total_expenses = %v, want 15.99 (the suppressed duplicate must be excluded)", out.TotalExpenses)
	}
	if len(out.ByMerchant) != 1 || out.ByMerchant[0].Count != 1 {
		t.Errorf("by_merchant = %+v, want one merchant with count 1", out.ByMerchant)
	}
}

// TestSummarizeSpendingCategoryAndMerchantSumsReconcileWithTotalExpenses is
// the durable guard for Finding 1 of the Phase 2 review: classifier.go
// deliberately records credits/refunds as Outflow transactions with a
// POSITIVE amount (see classifier_test.go), so any breakdown that sums
// math.Abs(amount) per transaction (the old byCategoryRows/byMerchantRows)
// silently DISAGREES with total_expenses (metrics.Calculate's
// math.Abs(outflows.SumAmount())) by 2x the refund total, because the old
// code adds a refund to a category/merchant's total while total_expenses
// subtracts it. by_category and by_merchant must sum the SIGNED amount and
// negate, exactly like total_expenses, so that summing either breakdown
// reproduces total_expenses for the same window. If this convention drifts
// apart again, this test must fail.
func TestSummarizeSpendingCategoryAndMerchantSumsReconcileWithTotalExpenses(t *testing.T) {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	ts := models.NewTransactionSet([]models.Transaction{
		{Date: day("2026-01-05"), Description: "NETFLIX", Category: "Entertainment", Amount: -15.99, TransactionType: models.Outflow},
		{Date: day("2026-01-10"), Description: "SAFEWAY", Category: "Groceries", Amount: -204.10, TransactionType: models.Outflow},
		// A refund: classifier.go records this as a POSITIVE-amount Outflow,
		// not Income (see classifier.go's "Positive amounts that aren't
		// income stay positive" rule). It belongs to the same merchant and
		// category as the Safeway charge above so both breakdowns exercise
		// the reconciliation, not just total_expenses.
		{Date: day("2026-01-20"), Description: "SAFEWAY", Category: "Groceries", Amount: 50.00, TransactionType: models.Outflow},
	})
	cs := connect(t, Deps{Transactions: stubTransactions{ts: ts}})

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

	// total_expenses = |(-15.99) + (-204.10) + 50.00| = 170.09. The refund
	// SUBTRACTS from the total rather than adding to it.
	if out.TotalExpenses != 170.09 {
		t.Fatalf("total_expenses = %v, want 170.09 (refund must subtract, not add)", out.TotalExpenses)
	}

	var catSum float64
	for _, c := range out.ByCategory {
		catSum += c.Amount
	}
	if round2(catSum) != out.TotalExpenses {
		t.Errorf("sum(by_category) = %v, want %v (must equal total_expenses)", round2(catSum), out.TotalExpenses)
	}
	// Groceries specifically: -204.10 + 50.00 = -154.10, negated = 154.10 --
	// NOT 204.10 + 50.00 = 254.10, which is what summing math.Abs per
	// transaction (the pre-fix bug) would have produced.
	for _, c := range out.ByCategory {
		if c.Category == "Groceries" && c.Amount != 154.10 {
			t.Errorf("by_category[Groceries] = %v, want 154.10 (refund must subtract)", c.Amount)
		}
	}

	var merchSum float64
	for _, m := range out.ByMerchant {
		merchSum += m.Amount
	}
	if round2(merchSum) != out.TotalExpenses {
		t.Errorf("sum(by_merchant) = %v, want %v (must equal total_expenses)", round2(merchSum), out.TotalExpenses)
	}
	for _, m := range out.ByMerchant {
		if m.Merchant == "safeway" && m.Amount != 154.10 {
			t.Errorf("by_merchant[safeway] = %v, want 154.10 (refund must subtract)", m.Amount)
		}
	}
}

// TestSummarizeSpendingByMonthReportsANetRefundMonthAsNegative guards the
// second half of Finding 1: byMonthRows used to math.Abs the month's signed
// total, which silently flips the sign of a month whose refunds exceed its
// spending (a large return, a chargeback) into a POSITIVE figure that reads
// as ordinary spending. It must instead negate the signed total, letting a
// genuinely negative (net-refund) month through.
func TestSummarizeSpendingByMonthReportsANetRefundMonthAsNegative(t *testing.T) {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	ts := models.NewTransactionSet([]models.Transaction{
		// A small charge and a much larger refund in the same month: the
		// signed total is net POSITIVE (+80 = 100 - 20), which the old
		// math.Abs code would have reported as +80 ("$80 of spending")
		// instead of the true -80 (a net refund of $80).
		{Date: day("2026-03-05"), Description: "WIDGET CO", Category: "Shopping", Amount: -20.00, TransactionType: models.Outflow},
		{Date: day("2026-03-15"), Description: "WIDGET CO REFUND", Category: "Shopping", Amount: 100.00, TransactionType: models.Outflow},
	})
	cs := connect(t, Deps{Transactions: stubTransactions{ts: ts}})

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
	if len(out.ByMonth) != 1 {
		t.Fatalf("by_month has %d entries, want 1: %+v", len(out.ByMonth), out.ByMonth)
	}
	if out.ByMonth[0].Amount != -80 {
		t.Errorf("by_month[2026-03] = %v, want -80 (a net-refund month must report negative, not +80)", out.ByMonth[0].Amount)
	}
}
