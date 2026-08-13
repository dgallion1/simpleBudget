package spend

import (
	"context"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// insightsTxn builds a Transaction with Hash and derived fields populated,
// matching how the real loader stamps transactions (internal/services/
// anomalies' own tests use the identical construction, see fixture_test.go's
// newTxn).
func insightsTxn(desc string, date time.Time, amount float64, category string) models.Transaction {
	t := models.Transaction{
		Date:            date,
		Amount:          amount,
		Description:     desc,
		Category:        category,
		TransactionType: models.Outflow,
	}
	t.Hash = t.ComputeHash()
	t.ComputeDerivedFields()
	return t
}

func insightsDate(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// categoryBaseline returns 5 normal-amount rows plus 1 anomalous row
// (anomalyAmount, dated anomalyDate) all in category, each with a distinct
// description so none of them form a qualifying (n>=4) merchant group --
// otherwise rule (a) would exclude them from the category baseline entirely
// and route the anomaly through mad_merchant instead of mad_category.
func categoryBaseline(category string, anomalyAmount float64, anomalyDate time.Time) []models.Transaction {
	rows := make([]models.Transaction, 0, 6)
	for i := 1; i <= 5; i++ {
		desc := category + " Store " + string(rune('A'+i-1)) + " Purchase"
		rows = append(rows, insightsTxn(desc, insightsDate(2024, 1, i), -50, category))
	}
	rows = append(rows, insightsTxn(category+" Store Z Purchase", anomalyDate, anomalyAmount, category))
	return rows
}

// priceCreepSeries returns a 6-occurrence recurring-merchant series whose
// last-3 median is 30% above its first-3 median -- comfortably past
// pricecreep's 5% threshold -- one row per month so occurrences are
// unambiguous and chronologically ordered.
func priceCreepSeries() []models.Transaction {
	amounts := []float64{-100, -100, -100, -130, -130, -130}
	rows := make([]models.Transaction, 0, len(amounts))
	for i, amt := range amounts {
		rows = append(rows, insightsTxn("NETFLIX MONTHLY SUBSCRIPTION", insightsDate(2024, time.Month(i+1), 1), amt, "Subscriptions"))
	}
	return rows
}

// newInsightsDeps builds a Deps with the given fixture transactions
// installed as its Transactions source, so tool calls never touch a real
// dataloader/CSV directory.
func newInsightsDeps(txns []models.Transaction) Deps {
	return Deps{Transactions: stubTransactions{ts: models.NewTransactionSet(txns)}}
}

func callInsightsTool[T any](t *testing.T, deps Deps, name string, args any) (T, *mcp.CallToolResult) {
	t.Helper()
	ctx := context.Background()
	clientSession := connect(t, deps)
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) returned a transport-level error, want a tool result: %v", name, err)
	}
	if res.IsError {
		var zero T
		return zero, res
	}
	return decodeToolResult[T](t, res), res
}

// TestGetAnomaliesTool_PlantedAnomalyHasCorrectMethodAndSeverity plants a
// single category with 6 rows (5 normal + 1 at 8x, well past the >6
// severity-high threshold and the materiality floor) and asserts
// get_anomalies reports it with method mad_category and severity high.
func TestGetAnomaliesTool_PlantedAnomalyHasCorrectMethodAndSeverity(t *testing.T) {
	anomalyDate := insightsDate(2024, 3, 15)
	txns := categoryBaseline("Groceries", -400, anomalyDate)
	deps := newInsightsDeps(txns)

	out, res := callInsightsTool[anomaliesOutput](t, deps, "get_anomalies", anomaliesInput{})
	if res.IsError {
		t.Fatalf("get_anomalies returned an error: %+v", res.Content)
	}

	if out.Count != 1 {
		t.Fatalf("Count = %d, want 1; anomalies: %+v", out.Count, out.Anomalies)
	}
	got := out.Anomalies[0]
	if got.Category != "Groceries" {
		t.Errorf("Category = %q, want %q", got.Category, "Groceries")
	}
	if got.Method != "mad_category" {
		t.Errorf("Method = %q, want %q", got.Method, "mad_category")
	}
	if got.Severity != "high" {
		t.Errorf("Severity = %q, want %q (score %v)", got.Severity, "high", got.Score)
	}
	if got.Date != "2024-03-15" {
		t.Errorf("Date = %q, want %q", got.Date, "2024-03-15")
	}
	if got.Amount != -400 {
		t.Errorf("Amount = %v, want -400", got.Amount)
	}
}

// TestGetAnomaliesTool_WindowFiltersDisplayOnly plants one anomaly inside a
// requested window and one outside it (different categories, so both are
// independently detected against the full history), and asserts the
// windowed call returns only the in-window flag while the full-history
// count backing detection is unaffected -- i.e. the out-of-window anomaly
// was still detected, just not returned.
func TestGetAnomaliesTool_WindowFiltersDisplayOnly(t *testing.T) {
	insideDate := insightsDate(2024, 3, 15)
	outsideDate := insightsDate(2024, 6, 15)

	var txns []models.Transaction
	txns = append(txns, categoryBaseline("Inside Category", -400, insideDate)...)
	txns = append(txns, categoryBaseline("Outside Category", -400, outsideDate)...)
	deps := newInsightsDeps(txns)

	// Sanity: an unwindowed call must detect both.
	full, res := callInsightsTool[anomaliesOutput](t, deps, "get_anomalies", anomaliesInput{})
	if res.IsError {
		t.Fatalf("unwindowed get_anomalies returned an error: %+v", res.Content)
	}
	if full.Count != 2 {
		t.Fatalf("unwindowed Count = %d, want 2 (both planted anomalies); got %+v", full.Count, full.Anomalies)
	}

	windowed, res := callInsightsTool[anomaliesOutput](t, deps, "get_anomalies", anomaliesInput{
		StartDate: "2024-03-01",
		EndDate:   "2024-03-31",
	})
	if res.IsError {
		t.Fatalf("windowed get_anomalies returned an error: %+v", res.Content)
	}
	if windowed.Count != 1 {
		t.Fatalf("windowed Count = %d, want 1; got %+v", windowed.Count, windowed.Anomalies)
	}
	if windowed.Anomalies[0].Category != "Inside Category" {
		t.Errorf("windowed result category = %q, want %q (the out-of-window anomaly must be excluded)",
			windowed.Anomalies[0].Category, "Inside Category")
	}
	if windowed.Window.Start == nil || *windowed.Window.Start != "2024-03-01" {
		t.Errorf("Window.Start = %v, want \"2024-03-01\"", windowed.Window.Start)
	}
	if windowed.Window.End == nil || *windowed.Window.End != "2024-03-31" {
		t.Errorf("Window.End = %v, want \"2024-03-31\"", windowed.Window.End)
	}
}

// TestGetAnomaliesTool_NoWindowReportsNullBounds asserts window.start/end
// are null (not empty strings) when the caller supplies neither param.
func TestGetAnomaliesTool_NoWindowReportsNullBounds(t *testing.T) {
	deps := newInsightsDeps(nil)
	out, res := callInsightsTool[anomaliesOutput](t, deps, "get_anomalies", anomaliesInput{})
	if res.IsError {
		t.Fatalf("get_anomalies returned an error: %+v", res.Content)
	}
	if out.Window.Start != nil {
		t.Errorf("Window.Start = %v, want nil", *out.Window.Start)
	}
	if out.Window.End != nil {
		t.Errorf("Window.End = %v, want nil", *out.Window.End)
	}
}

// TestGetAnomaliesTool_InvalidDateIsAToolError asserts a malformed
// start_date surfaces as a tool error (IsError), not a panic or a silently
// ignored parameter.
func TestGetAnomaliesTool_InvalidDateIsAToolError(t *testing.T) {
	deps := newInsightsDeps(nil)
	ctx := context.Background()
	clientSession := connect(t, deps)

	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_anomalies",
		Arguments: anomaliesInput{StartDate: "not-a-date"},
	})
	if err != nil {
		t.Fatalf("CallTool(get_anomalies) returned a transport-level error, want a tool result with IsError set: %v", err)
	}
	msg := toolErrorText(t, res)
	if !strings.Contains(msg, "not-a-date") {
		t.Errorf("error should name the offending value, got: %s", msg)
	}
}

// TestGetAnomaliesTool_EmptyDataReturnsZeroCountNoError asserts an empty
// transaction history is not an error condition: count 0, empty slice.
func TestGetAnomaliesTool_EmptyDataReturnsZeroCountNoError(t *testing.T) {
	deps := newInsightsDeps(nil)
	out, res := callInsightsTool[anomaliesOutput](t, deps, "get_anomalies", anomaliesInput{})
	if res.IsError {
		t.Fatalf("get_anomalies returned an error on empty data: %+v", res.Content)
	}
	if out.Count != 0 {
		t.Errorf("Count = %d, want 0", out.Count)
	}
	if len(out.Anomalies) != 0 {
		t.Errorf("Anomalies = %+v, want empty", out.Anomalies)
	}
}

// errBoom stands in for a data-access failure -- locked/encrypted storage or
// a missing data directory -- so TestGetAnomaliesTool_LoadFailureIsAToolError
// can assert the tool surfaces it clearly instead of panicking or reporting
// an empty result as if nothing were wrong.
var errBoom = &staticError{"boom: storage is unavailable"}

type staticError struct{ msg string }

func (e *staticError) Error() string { return e.msg }

func TestGetAnomaliesTool_LoadFailureIsAToolError(t *testing.T) {
	deps := Deps{Transactions: stubTransactions{err: errBoom}}

	ctx := context.Background()
	clientSession := connect(t, deps)
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_anomalies", Arguments: anomaliesInput{}})
	if err != nil {
		t.Fatalf("CallTool(get_anomalies) returned a transport-level error, want a tool result with IsError set: %v", err)
	}
	msg := toolErrorText(t, res)
	if !strings.Contains(msg, "boom") {
		t.Errorf("error should carry the underlying load failure, got: %s", msg)
	}
}

// TestGetPriceCreepTool_ReturnsPlantedCreepInBand plants a 6-occurrence
// stepped series (median of last 3 = 30% above median of first 3) and
// asserts get_price_creep reports it with the right band and occurrence
// count.
func TestGetPriceCreepTool_ReturnsPlantedCreepInBand(t *testing.T) {
	deps := newInsightsDeps(priceCreepSeries())

	out, res := callInsightsTool[priceCreepOutput](t, deps, "get_price_creep", priceCreepInput{})
	if res.IsError {
		t.Fatalf("get_price_creep returned an error: %+v", res.Content)
	}
	if out.Count != 1 {
		t.Fatalf("Count = %d, want 1; items: %+v", out.Count, out.Items)
	}
	got := out.Items[0]
	if got.Occurrences != 6 {
		t.Errorf("Occurrences = %d, want 6", got.Occurrences)
	}
	if got.FirstAmount != 100 {
		t.Errorf("FirstAmount = %v, want 100", got.FirstAmount)
	}
	if got.CurrentAmount != 130 {
		t.Errorf("CurrentAmount = %v, want 130", got.CurrentAmount)
	}
	if got.PctChange != 30 {
		t.Errorf("PctChange = %v, want 30", got.PctChange)
	}
	if got.FirstDate != "2024-01-01" {
		t.Errorf("FirstDate = %q, want %q", got.FirstDate, "2024-01-01")
	}
	if got.LastDate != "2024-06-01" {
		t.Errorf("LastDate = %q, want %q", got.LastDate, "2024-06-01")
	}
}

// TestGetPriceCreepTool_EmptyDataReturnsZeroCountNoError mirrors the
// anomalies empty-data case for price-creep.
func TestGetPriceCreepTool_EmptyDataReturnsZeroCountNoError(t *testing.T) {
	deps := newInsightsDeps(nil)
	out, res := callInsightsTool[priceCreepOutput](t, deps, "get_price_creep", priceCreepInput{})
	if res.IsError {
		t.Fatalf("get_price_creep returned an error on empty data: %+v", res.Content)
	}
	if out.Count != 0 {
		t.Errorf("Count = %d, want 0", out.Count)
	}
	if len(out.Items) != 0 {
		t.Errorf("Items = %+v, want empty", out.Items)
	}
}
