package spend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/metrics"
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

// TestSummarizeSpendingRefundDominantWindowNegatesTotalExpensesAndReconciles
// is CB7's required refund-dominant WINDOW fixture: unlike
// TestSummarizeSpendingByMonthReportsANetRefundMonthAsNegative (one
// refund-dominant MONTH inside an otherwise-mixed window),
// here the WHOLE window (both months combined) nets refund-dominant, so
// total_expenses itself -- not just one by_month row -- must go negative.
// Before CB7, metrics.Calculate ran math.Abs on the range-level total, so
// total_expenses would have reported positive here even though by_month's
// own sum (never math.Abs'd, per Finding 1) was already negative --
// breaking the tool description's sum(by_month)==total_expenses contract
// (the old contract only promised this held "in MAGNITUDE").
func TestSummarizeSpendingRefundDominantWindowNegatesTotalExpensesAndReconciles(t *testing.T) {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	ts := models.NewTransactionSet([]models.Transaction{
		// Jan: ordinary spend.
		{Date: day("2026-01-05"), Description: "WIDGET CO", Category: "Shopping", Amount: -50.00, TransactionType: models.Outflow},
		// Feb: a refund far exceeding the window's whole ordinary spend.
		{Date: day("2026-02-15"), Description: "WIDGET CO REFUND", Category: "Shopping", Amount: 500.00, TransactionType: models.Outflow},
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

	// Window nets: -50 + 500 = +450 -- refund-dominant OVERALL.
	// total_expenses is the signed negated net: -450.
	if out.TotalExpenses != -450 {
		t.Fatalf("total_expenses = %v, want -450 (refund-dominant window; CB7)", out.TotalExpenses)
	}
	if out.TotalExpenses >= 0 {
		t.Fatalf("total_expenses = %v, want negative (refund-dominant window)", out.TotalExpenses)
	}

	if len(out.ByMonth) != 2 {
		t.Fatalf("by_month has %d entries, want 2: %+v", len(out.ByMonth), out.ByMonth)
	}
	var monthSum float64
	for _, m := range out.ByMonth {
		monthSum += m.Amount
	}
	if round2(monthSum) != out.TotalExpenses {
		t.Errorf("sum(by_month) = %v, want %v (must EQUAL total_expenses exactly now that both are signed, not just agree in magnitude)", round2(monthSum), out.TotalExpenses)
	}
}

// TestSummarizeSpendingSuppressesPhantomHealthcareWhenNoCoverage guards
// ruling 2026-08-30a: a plan can have a healthcare target CONFIGURED while
// the ledger has NO Health Insurance transactions in the queried window (or
// at all) -- metrics.Calculate then reports HasHealthcareTarget=false, but
// m.HealthcareTarget/HealthcareActual/HealthcarePerMonthDelta themselves are
// NOT zeroed by Calculate (only gated), so copying them into the budget
// block unconditionally leaked a phantom healthcare_monthly_target/delta
// (e.g. target:1000, delta:-1000) even though the dashboard correctly omits
// the Health line entirely (kpis.html gates on the same HasHealthcareTarget
// flag). Living fields must stay intact -- only the healthcare ones zero
// out.
func TestSummarizeSpendingSuppressesPhantomHealthcareWhenNoCoverage(t *testing.T) {
	// searchFixture has ordinary living-category outflows and no Health
	// Insurance transactions at all, so hasCoverage is false no matter the
	// window.
	sm := newSummaryTestManager(t, 200, 1000) // living target $200/mo, healthcare target $1000/mo
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
		t.Fatal("budget = nil, want populated (living target is configured, HasBudgetTarget alone must show the block)")
	}
	if out.Budget.LivingTarget != 200 {
		t.Errorf("living_monthly_target = %v, want 200 (living fields must stay intact)", out.Budget.LivingTarget)
	}
	// searchFixture: 15.99 + 204.10 + 15.99 = 236.08 total expenses, none of
	// it Health Insurance, over Jan 5 - Feb 5 (the ledger's own min/max
	// window, since no explicit dates were given).
	if out.Budget.LivingActual == 0 {
		t.Error("living_monthly_actual = 0, want the fixture's real living spend rate (living fields must stay intact)")
	}
	if out.Budget.HealthcareTarget != 0 {
		t.Errorf("healthcare_monthly_target = %v, want 0 -- no Health Insurance transactions means no coverage, "+
			"and the plan's configured target must not leak through when HasHealthcareTarget is false", out.Budget.HealthcareTarget)
	}
	if out.Budget.HealthcareActual != 0 {
		t.Errorf("healthcare_monthly_actual = %v, want 0", out.Budget.HealthcareActual)
	}
	if out.Budget.HealthcareDelta != 0 {
		t.Errorf("healthcare_monthly_delta = %v, want 0 (not -1000, the phantom credit this test guards against)", out.Budget.HealthcareDelta)
	}
}

// TestSummarizeSpendingDerivesCoverageStartFromFullLedgerNotWindow is a
// mutation-killing regression for ruling 2026-08-30b's "full-ledger
// derivation" gap: replacing metrics.HealthcareCoverageStart's argument at
// the summarize_spending call site with the window-filtered set (instead of
// the full active ledger, derived before the window filter is applied)
// leaves the rest of the suite green because every other fixture's earliest
// Health Insurance bill already sits inside its queried window, so a
// window-derived coverage start happens to equal the full-ledger one. Here
// the earliest bill is BEFORE the window and a second bill is inside it, so
// the two derivations produce materially different coverage starts (and
// therefore different coverageMonths / HealthcareActual / HealthcareDelta).
func TestSummarizeSpendingDerivesCoverageStartFromFullLedgerNotWindow(t *testing.T) {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	ts := models.NewTransactionSet([]models.Transaction{
		// Earliest Health Insurance bill: well BEFORE the queried window
		// below. A window-derived coverage start can never see this row.
		{Date: day("2025-11-01"), Description: "PREMIUM", Category: metrics.HealthInsuranceCategory, Amount: -1000, TransactionType: models.Outflow},
		// A second bill, inside the window -- the only one a (buggy)
		// window-derived coverage start would ever find.
		{Date: day("2026-01-15"), Description: "PREMIUM", Category: metrics.HealthInsuranceCategory, Amount: -1000, TransactionType: models.Outflow},
	})
	sm := newSummaryTestManager(t, 0, 2000) // no living target, healthcare target $2000/mo
	cs := connect(t, Deps{Transactions: stubTransactions{ts: ts}, Settings: sm})

	windowStart, windowEnd := day("2026-01-01"), day("2026-01-31")

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
		t.Fatal("budget = nil, want populated (healthcare target configured, coverage active)")
	}

	// Correct: coverage start (2025-11-01) predates the window entirely, so
	// the whole window is covered -- only the Jan 15 bill's $1000 falls
	// inside the window, spread over the FULL window's months.
	fullLedgerCoverageStart := day("2025-11-01")
	wantCoverageMonths := metrics.ClippedHealthcareMonths(windowStart, windowEnd, fullLedgerCoverageStart, true)
	wantHealthcareActual := round2(1000 / wantCoverageMonths)
	wantHealthcareDelta := round2(wantHealthcareActual - 2000)

	// What the mutation would produce: coverage start derived only from the
	// window-filtered set, which can't see the Nov bill and so anchors on
	// the Jan 15 one instead -- clipping coverageMonths down to the back
	// half of January and inflating the actual/delta.
	windowDerivedCoverageStart := day("2026-01-15")
	mutatedCoverageMonths := metrics.ClippedHealthcareMonths(windowStart, windowEnd, windowDerivedCoverageStart, true)
	mutatedHealthcareActual := round2(1000 / mutatedCoverageMonths)
	if wantHealthcareActual == mutatedHealthcareActual {
		t.Fatalf("test fixture precondition broken: full-ledger and window-derived coverage starts produce the "+
			"same HealthcareActual (%v) -- fixture can't distinguish them", wantHealthcareActual)
	}

	if out.Budget.HealthcareActual != wantHealthcareActual {
		t.Errorf("healthcare_monthly_actual = %v, want %v (coverage start must come from the FULL ledger, "+
			"not the window-derived %v which a mutated call site would produce)",
			out.Budget.HealthcareActual, wantHealthcareActual, mutatedHealthcareActual)
	}
	if out.Budget.HealthcareDelta != wantHealthcareDelta {
		t.Errorf("healthcare_monthly_delta = %v, want %v", out.Budget.HealthcareDelta, wantHealthcareDelta)
	}
}

// TestSummarizeSpendingCoverageStartExcludesSuppressedDuplicates is a
// mutation-killing regression for ruling 2026-08-30b's "duplicates
// excluded" gap: deriving coverage start from the raw (pre-duplicate-
// resolution) transaction set instead of ts.Active() leaves the rest of the
// suite green because no other fixture has a suppressed Health Insurance
// row earlier than an active one. Here the EARLIEST Health Insurance bill
// is duplicate-suppressed; the correct coverage start must come from the
// next-earliest ACTIVE bill instead, which lands INSIDE the window rather
// than before it, producing a materially different coverageMonths.
func TestSummarizeSpendingCoverageStartExcludesSuppressedDuplicates(t *testing.T) {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	ts := models.NewTransactionSet([]models.Transaction{
		// Earliest Health Insurance bill, but duplicate-suppressed: the
		// user has already resolved it as a near-duplicate. A coverage-
		// start derivation reading the raw set would still see this row.
		{Date: day("2025-06-01"), Description: "PREMIUM (DUP)", Category: metrics.HealthInsuranceCategory, Amount: -1000, TransactionType: models.Outflow, Suppressed: true},
		// The earliest ACTIVE Health Insurance bill -- this is what
		// coverage start must actually derive from.
		{Date: day("2026-01-15"), Description: "PREMIUM", Category: metrics.HealthInsuranceCategory, Amount: -1000, TransactionType: models.Outflow},
	})
	sm := newSummaryTestManager(t, 0, 2000) // no living target, healthcare target $2000/mo
	cs := connect(t, Deps{Transactions: stubTransactions{ts: ts}, Settings: sm})

	windowStart, windowEnd := day("2026-01-01"), day("2026-01-31")

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
		t.Fatal("budget = nil, want populated (healthcare target configured, coverage active)")
	}

	// Correct: coverage start is the earliest ACTIVE bill (2026-01-15),
	// which lands inside the window -- clipped to the back half of January.
	activeCoverageStart := day("2026-01-15")
	wantCoverageMonths := metrics.ClippedHealthcareMonths(windowStart, windowEnd, activeCoverageStart, true)
	wantHealthcareActual := round2(1000 / wantCoverageMonths)
	wantHealthcareDelta := round2(wantHealthcareActual - 2000)

	// What the mutation would produce: coverage start derived from the raw
	// (duplicates-included) set, anchoring on the suppressed 2025-06-01 row,
	// which predates the window -- the whole window would appear covered.
	rawCoverageStart := day("2025-06-01")
	mutatedCoverageMonths := metrics.ClippedHealthcareMonths(windowStart, windowEnd, rawCoverageStart, true)
	mutatedHealthcareActual := round2(1000 / mutatedCoverageMonths)
	if wantHealthcareActual == mutatedHealthcareActual {
		t.Fatalf("test fixture precondition broken: active-only and duplicates-included coverage starts produce "+
			"the same HealthcareActual (%v) -- fixture can't distinguish them", wantHealthcareActual)
	}

	if out.Budget.HealthcareActual != wantHealthcareActual {
		t.Errorf("healthcare_monthly_actual = %v, want %v (coverage start must exclude the suppressed duplicate, "+
			"not derive %v from it as a mutated call site would)",
			out.Budget.HealthcareActual, wantHealthcareActual, mutatedHealthcareActual)
	}
	if out.Budget.HealthcareDelta != wantHealthcareDelta {
		t.Errorf("healthcare_monthly_delta = %v, want %v", out.Budget.HealthcareDelta, wantHealthcareDelta)
	}
}

// negZeroJSONPattern is the MCP-surface twin of metrics_test.go's helper of
// the same name: matches a JSON-serialized IEEE negative zero token
// ("-0", "-0.0", etc.) immediately followed by a JSON delimiter, so it
// only flags a genuine whole negative-zero value, not a substring of a
// larger negative number.
var negZeroJSONPattern = regexp.MustCompile(`-0(\.0+)?[,}\]]`)

// TestSummarizeSpendingHealthcareActualNoNegativeZeroWhenWindowHasNoHIRows
// is CB7-2026-09-03c's required MCP fixture: a healthcare target is
// configured AND coverage has started (an earlier Health Insurance bill
// establishes coverageStart, so HasHealthcareTarget is true), but the
// QUERIED WINDOW itself has zero Health Insurance transactions -- exactly
// the shape of the live ledger (every month/year has zero HI rows in most
// windows). Before the fix, healthcareTotal was IEEE -0.0 for the window's
// empty HI set, and HealthcareActual (=healthcareTotal/coverageMonths)
// inherited it, serializing as the literal JSON token "-0" for
// healthcare_monthly_actual.
func TestSummarizeSpendingHealthcareActualNoNegativeZeroWhenWindowHasNoHIRows(t *testing.T) {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	ts := models.NewTransactionSet([]models.Transaction{
		// Establishes coverageStart well BEFORE the queried window --
		// hasCoverage=true for the whole ledger, but this row itself falls
		// outside the window below.
		{Date: day("2025-11-01"), Description: "PREMIUM", Category: metrics.HealthInsuranceCategory, Amount: -1000, TransactionType: models.Outflow},
		// The queried window's only transaction: ordinary living spend, NO
		// Health Insurance rows at all.
		{Date: day("2026-01-10"), Description: "RENT", Category: "Housing", Amount: -1200, TransactionType: models.Outflow},
	})
	sm := newSummaryTestManager(t, 500, 200) // living target $500/mo, healthcare target $200/mo
	cs := connect(t, Deps{Transactions: stubTransactions{ts: ts}, Settings: sm})

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

	raw := mustJSON(t, res.StructuredContent)
	if loc := negZeroJSONPattern.FindString(string(raw)); loc != "" {
		t.Errorf("structured content contains a negative-zero token %q: %s", loc, raw)
	}

	var out summaryOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Budget == nil {
		t.Fatal("budget = nil, want populated")
	}
	if out.Budget.HealthcareActual != 0 {
		t.Errorf("healthcare_monthly_actual = %v, want 0 (zero Health Insurance rows this window)", out.Budget.HealthcareActual)
	}
}
