package templates

import (
	"io/fs"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/web"
)

func renderMajorExpensesContent(t *testing.T, data map[string]any) string {
	t.Helper()
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	r, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS: %v", err)
	}
	html, err := r.RenderToString("major-expenses-content", data)
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return html
}

// TestRenderMajorExpenses_MultipleEntriesAllRender guards against the
// regression where $.PinnedHashes was referenced inside the per-item
// sub-template (where $ is the Summary, not the page). The template
// errored mid-loop and only the first entry rendered.
func TestRenderMajorExpenses_MultipleEntriesAllRender(t *testing.T) {
	type summary struct {
		Expense      models.MajorExpense
		Count        int
		Total        float64
		Transactions []models.Transaction
		PinnedHashes map[string]bool
	}
	now := time.Now()
	mkSum := func(name string) summary {
		return summary{
			Expense:      models.MajorExpense{ID: name, Name: name, Keywords: []string{name}},
			Count:        1,
			Total:        100,
			Transactions: []models.Transaction{{Date: now, Amount: -100, Description: name + " txn", Hash: "h-" + name}},
			PinnedHashes: map[string]bool{"h-" + name: true},
		}
	}
	html := renderMajorExpensesContent(t, map[string]any{
		"Title":     "Major Expenses",
		"ActiveTab": "major-expenses",
		"Expenses":  []models.MajorExpense{},
		"Summaries": []summary{mkSum("Lucid"), mkSum("Hyundai"), mkSum("Wegmans")},
		"Match": struct {
			Exceptions models.ExceptionsReport
		}{Exceptions: models.ExceptionsReport{}},
		"PinnedHashes": map[string]bool{},
		"Threshold":    100.0,
		"WindowDays":   30,
	})
	for _, name := range []string{"Lucid", "Hyundai", "Wegmans"} {
		if !strings.Contains(html, `id="major-expense-item-`+name+`"`) {
			t.Errorf("expected entry %q to render, but it was missing — sub-template likely errored mid-loop", name)
		}
	}
	// The 📌 prefix relies on .PinnedHashes (the Summary's, not page-level).
	// If the template wires it through correctly, every entry's matched
	// txn should be flagged as pinned.
	if !strings.Contains(html, `📌`) {
		t.Errorf("expected pinned marker in matched-transactions disclosure")
	}
}

func TestRenderMajorExpenses_EmptyState(t *testing.T) {
	html := renderMajorExpensesContent(t, map[string]any{
		"Title":      "Major Expenses",
		"ActiveTab":  "major-expenses",
		"Expenses":   []models.MajorExpense{},
		"Summaries":  []struct{}{},
		"Match":      map[string]any{"Exceptions": models.ExceptionsReport{}},
		"Threshold":  100.0,
		"WindowDays": 30,
	})
	if !strings.Contains(html, "No major expenses declared yet") {
		t.Errorf("expected empty-state copy, got: %s", html)
	}
	if !strings.Contains(html, "No exceptions in current dataset") {
		t.Errorf("expected empty-exceptions copy, got: %s", html)
	}
}

func TestRenderMajorExpenses_WithEntriesAndExceptions(t *testing.T) {
	now := time.Now()
	type summary struct {
		Expense      models.MajorExpense
		Count        int
		Total        float64
		Transactions []models.Transaction
	}
	html := renderMajorExpensesContent(t, map[string]any{
		"Title":     "Major Expenses",
		"ActiveTab": "major-expenses",
		"Expenses": []models.MajorExpense{
			{ID: "rent", Name: "Rent", Keywords: []string{"landlord"}, ExpectedMin: 1500, ExpectedMax: 2000},
		},
		"Summaries": []summary{
			{Expense: models.MajorExpense{ID: "rent", Name: "Rent", Keywords: []string{"landlord"}, ExpectedMin: 1500, ExpectedMax: 2000}, Count: 3, Total: 4800},
		},
		"Match": struct {
			Exceptions models.ExceptionsReport
		}{
			Exceptions: models.ExceptionsReport{
				UnknownLarge: []models.ExceptionUnknownLargeTxn{
					{Transaction: models.Transaction{Date: now, Amount: -250, Description: "Random Big Purchase"}},
				},
				Anomalous: []models.ExceptionAnomalousAmount{
					{
						MajorExpenseID:   "rent",
						MajorExpenseName: "Rent",
						Transaction:      models.Transaction{Date: now, Amount: -3500, Description: "My Landlord LLC"},
						ExpectedMin:      1500,
						ExpectedMax:      2000,
					},
				},
				NewMerchants: []models.ExceptionNewMerchant{
					{Description: "brand new store", FirstSeen: now, Transaction: models.Transaction{Date: now, Amount: -75, Description: "Brand New Store"}},
				},
				Threshold:     100,
				NewWindowDays: 30,
			},
		},
		"Threshold":  100.0,
		"WindowDays": 30,
	})
	if !strings.Contains(html, "Rent") {
		t.Errorf("expected expense name in output")
	}
	if !strings.Contains(html, "Random Big Purchase") {
		t.Errorf("expected unknown-large transaction in output")
	}
	if !strings.Contains(html, "My Landlord LLC") {
		t.Errorf("expected anomalous transaction in output")
	}
	if !strings.Contains(html, "Brand New Store") {
		t.Errorf("expected new-merchant transaction in output")
	}
	if !strings.Contains(html, "3 matched") {
		t.Errorf("expected matched count in output")
	}
	// Click-to-fill plumbing
	if !strings.Contains(html, `data-fill-name="Random Big Purchase"`) {
		t.Errorf("expected unmatched row to expose data-fill-name attribute")
	}
	if !strings.Contains(html, `data-fill-name="Brand New Store"`) {
		t.Errorf("expected new-merchant row to expose data-fill-name attribute")
	}
	if !strings.Contains(html, `data-jump-expense="rent"`) {
		t.Errorf("expected anomalous expense-name link to expose data-jump-expense attribute")
	}
	// Anomalous rows now also expose data-fill-* so the row click moves
	// the transaction to a new expense (jump is on the inner name link).
	if !strings.Contains(html, `data-fill-name="My Landlord LLC"`) {
		t.Errorf("expected anomalous row to expose data-fill-name (so click moves to a new expense)")
	}
	if !strings.Contains(html, `id="major-expense-item-rent"`) {
		t.Errorf("expected list item to have id targetable by jump")
	}
	if !strings.Contains(html, `id="major-expenses-add-form"`) {
		t.Errorf("expected add form to have id used by click handler")
	}
	// Search input + per-row data-search
	if !strings.Contains(html, `id="major-expenses-exception-search"`) {
		t.Errorf("expected exception search input")
	}
	if !strings.Contains(html, `class="major-expenses-exception-row`) {
		t.Errorf("expected rows to carry the exception-row class targeted by the filter script")
	}
	if !strings.Contains(html, `data-search="Random Big Purchase $250.00 `) {
		t.Errorf("expected unmatched row to include description + amount in data-search, got html=%s", html)
	}
	if !strings.Contains(html, `data-search="Rent My Landlord LLC $3500.00 `) {
		t.Errorf("expected anomalous row to combine major-expense name, description, and amount, got html=%s", html)
	}
	if !strings.Contains(html, `data-search="Brand New Store $75.00 `) {
		t.Errorf("expected new-merchant row to include amount in data-search")
	}
}

func TestRenderMajorExpensesResults_IncludesOOBSwap(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	r, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS: %v", err)
	}
	type summary struct {
		Expense      models.MajorExpense
		Count        int
		Total        float64
		Transactions []models.Transaction
	}
	html, err := r.RenderToString("major-expenses-results", map[string]any{
		"Expenses":  []models.MajorExpense{},
		"Summaries": []summary{},
		"Match": struct {
			Exceptions models.ExceptionsReport
		}{Exceptions: models.ExceptionsReport{Threshold: 100, NewWindowDays: 30}},
		"Threshold":  100.0,
		"WindowDays": 30,
	})
	if err != nil {
		t.Fatalf("render results: %v", err)
	}
	if !strings.Contains(html, `hx-swap-oob="innerHTML"`) {
		t.Errorf("expected OOB swap markup in results partial, got: %s", html)
	}
	if !strings.Contains(html, "major-expenses-list-card") {
		t.Errorf("expected list-card OOB target id")
	}
}
