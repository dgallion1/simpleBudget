package spend

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubMajorExpenses is a canned MajorExpenseSource for exercising
// get_recurring's annotation path without a real *dataloader.DataLoader.
type stubMajorExpenses struct {
	defs    []models.MajorExpense
	defsErr error
	pins    map[string]string
	pinsErr error
}

func (s stubMajorExpenses) LoadMajorExpenses() ([]models.MajorExpense, error) {
	return s.defs, s.defsErr
}

func (s stubMajorExpenses) LoadTransactionPins() (map[string]string, error) {
	return s.pins, s.pinsErr
}

// recurringFixture is twelve monthly charges from one merchant plus one
// unrelated one-off, anchored to a fixed reference month so freshness does
// not depend on the wall clock.
func recurringFixture() *models.TransactionSet {
	var txns []models.Transaction
	for i := 0; i < 12; i++ {
		txns = append(txns, models.Transaction{
			Date:            time.Date(2025, time.Month(1+i), 5, 0, 0, 0, 0, time.UTC),
			Description:     "NETFLIX",
			Category:        "Entertainment",
			Amount:          -15.99,
			TransactionType: models.Outflow,
		})
	}
	txns = append(txns, models.Transaction{
		Date:            time.Date(2025, 6, 14, 0, 0, 0, 0, time.UTC),
		Description:     "ROOF REPAIR",
		Category:        "Home",
		Amount:          -8400,
		TransactionType: models.Outflow,
	})
	return models.NewTransactionSet(txns)
}

func TestGetRecurringFindsTheMonthlyChargeAndNotTheOneOff(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: recurringFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_recurring",
		Arguments: map[string]any{"reference_date": "2025-12-31"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_recurring returned an error: %+v", res.Content)
	}

	var out recurringOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}

	// merchants.DisplayLabel (used by insights.DetectRecurringAt to build
	// each row's Description) always lower-cases its result -- the same
	// documented behavior get_price_creep and summarize_spending already
	// rely on -- so a description of "NETFLIX" in the source transactions
	// comes back as "netflix" here, regardless of input casing.
	var netflix *recurringRow
	for i := range out.Payments {
		if out.Payments[i].Description == "netflix" {
			netflix = &out.Payments[i]
		}
		if out.Payments[i].Description == "roof repair" {
			t.Errorf("a single one-off charge was reported as recurring: %+v", out.Payments[i])
		}
	}
	if netflix == nil {
		t.Fatalf("NETFLIX not detected as recurring; got %+v", out.Payments)
	}
	// models.RecurringPayment.Frequency is lowercase ("weekly", "monthly",
	// "yearly") -- do not title-case it on the way out.
	if netflix.Frequency != "monthly" {
		t.Errorf("frequency = %q, want monthly", netflix.Frequency)
	}
	if netflix.Occurrences != 12 {
		t.Errorf("occurrences = %d, want 12", netflix.Occurrences)
	}
	if !netflix.IsSubscription {
		t.Error("a recurring 15.99 monthly charge should be flagged as a subscription")
	}
}

// The reference date is the freshness cutoff: a series that stopped long
// before it is no longer active, and must not be reported as current.
func TestGetRecurringHonorsTheReferenceDateAsAFreshnessCutoff(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: recurringFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_recurring",
		Arguments: map[string]any{"reference_date": "2027-06-30"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_recurring returned an error: %+v", res.Content)
	}

	var out recurringOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	for _, p := range out.Payments {
		// "netflix", not "NETFLIX" -- see the DisplayLabel note above.
		if p.Description == "netflix" {
			t.Errorf("a series that ended 18 months before the reference date is not active: %+v", p)
		}
	}
}

func TestGetRecurringRejectsAnInvalidDate(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: recurringFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_recurring",
		Arguments: map[string]any{"reference_date": "not-a-date"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("get_recurring should have reported the invalid reference_date as a tool error")
	}
}

func TestGetRecurringReportsALoadFailureAsAToolError(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{err: errors.New("storage is locked")}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_recurring",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("get_recurring should have reported the load failure as a tool error")
	}
}

// TestGetRecurringSubscriptionsOnlyFiltersOutBills plants an active
// recurring "bill" (a keyword in billKeywords, so isSubscription is false)
// alongside the NETFLIX subscription and confirms subscriptions_only drops
// it while keeping NETFLIX.
func TestGetRecurringSubscriptionsOnlyFiltersOutBills(t *testing.T) {
	ts := recurringFixture()
	var billTxns []models.Transaction
	for i := 0; i < 12; i++ {
		billTxns = append(billTxns, models.Transaction{
			Date:            time.Date(2025, time.Month(1+i), 10, 0, 0, 0, 0, time.UTC),
			Description:     "CITY WATER AND SEWER",
			Category:        "Utilities",
			Amount:          -60,
			TransactionType: models.Outflow,
		})
	}
	ts = models.NewTransactionSet(append(append([]models.Transaction{}, ts.Transactions...), billTxns...))

	cs := connect(t, Deps{Transactions: stubTransactions{ts: ts}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_recurring",
		Arguments: map[string]any{"reference_date": "2025-12-31", "subscriptions_only": true},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_recurring returned an error: %+v", res.Content)
	}

	var out recurringOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Count != len(out.Payments) {
		t.Errorf("count = %d, want len(payments) = %d", out.Count, len(out.Payments))
	}
	foundNetflix := false
	for _, p := range out.Payments {
		if !p.IsSubscription {
			t.Errorf("subscriptions_only returned a non-subscription row: %+v", p)
		}
		if p.Description == "water and sewer" || p.Description == "city water and sewer" {
			t.Errorf("a water/sewer bill should not be flagged as a subscription: %+v", p)
		}
		if p.Description == "netflix" {
			foundNetflix = true
		}
	}
	if !foundNetflix {
		t.Errorf("subscriptions_only should still return the netflix subscription; got %+v", out.Payments)
	}
}

// TestGetRecurringAnnotatesMajorExpenseNameWhenWired confirms the tool
// calls majorexpenses.AnnotateRecurringPayments through deps.MajorExpenses
// when both its loads succeed.
func TestGetRecurringAnnotatesMajorExpenseNameWhenWired(t *testing.T) {
	cs := connect(t, Deps{
		Transactions: stubTransactions{ts: recurringFixture()},
		MajorExpenses: stubMajorExpenses{
			defs: []models.MajorExpense{
				{ID: "streaming", Name: "Streaming Services", Keywords: []string{"netflix"}},
			},
		},
	})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_recurring",
		Arguments: map[string]any{"reference_date": "2025-12-31"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_recurring returned an error: %+v", res.Content)
	}

	var out recurringOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	found := false
	for _, p := range out.Payments {
		if p.Description == "netflix" {
			found = true
			if p.MajorExpenseName != "Streaming Services" {
				t.Errorf("major_expense_name = %q, want %q", p.MajorExpenseName, "Streaming Services")
			}
		}
	}
	if !found {
		t.Fatalf("netflix not found in %+v", out.Payments)
	}
}

// TestGetRecurringSkipsAnnotationWhenMajorExpensesLoadFails confirms a
// LoadMajorExpenses failure degrades to unannotated payments rather than
// failing the whole call -- the label is a convenience, not the answer.
func TestGetRecurringSkipsAnnotationWhenMajorExpensesLoadFails(t *testing.T) {
	cs := connect(t, Deps{
		Transactions:  stubTransactions{ts: recurringFixture()},
		MajorExpenses: stubMajorExpenses{defsErr: errors.New("major_expenses.json is corrupt")},
	})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_recurring",
		Arguments: map[string]any{"reference_date": "2025-12-31"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("a major-expenses load failure must not fail get_recurring: %+v", res.Content)
	}

	var out recurringOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	for _, p := range out.Payments {
		if p.Description == "netflix" && p.MajorExpenseName != "" {
			t.Errorf("major_expense_name should be empty when the definitions failed to load, got %q", p.MajorExpenseName)
		}
	}
}

// TestGetRecurringStillAnnotatesFromDefinitionsWhenTransactionPinsLoadFails
// mirrors the handler's own annotateRecurringWithMajorExpense, which
// ignores a LoadTransactionPins error as long as major-expense definitions
// loaded (owner ruling: match the app -- a pins failure should not cost the
// definition-derived label, since AnnotateRecurringPayments accepts a nil
// pins map and pins are only an override on top of keyword/amount
// matching). A pins-load failure alone must not degrade to unannotated.
func TestGetRecurringStillAnnotatesFromDefinitionsWhenTransactionPinsLoadFails(t *testing.T) {
	cs := connect(t, Deps{
		Transactions: stubTransactions{ts: recurringFixture()},
		MajorExpenses: stubMajorExpenses{
			defs:    []models.MajorExpense{{ID: "streaming", Name: "Streaming Services", Keywords: []string{"netflix"}}},
			pinsErr: errors.New("transaction_pins.json is corrupt"),
		},
	})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_recurring",
		Arguments: map[string]any{"reference_date": "2025-12-31"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("a transaction-pins load failure must not fail get_recurring: %+v", res.Content)
	}

	var out recurringOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	found := false
	for _, p := range out.Payments {
		if p.Description == "netflix" {
			found = true
			if p.MajorExpenseName != "Streaming Services" {
				t.Errorf("major_expense_name = %q, want %q (a pins-load failure should not cost the definition-derived label)",
					p.MajorExpenseName, "Streaming Services")
			}
		}
	}
	if !found {
		t.Fatalf("netflix not found in %+v", out.Payments)
	}
}
