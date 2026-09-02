package templates

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/web"
)

func renderKPIMonthDetail(t *testing.T, data map[string]any) string {
	t.Helper()
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	r, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS: %v", err)
	}
	html, err := r.RenderToString("kpi-month-detail", data)
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return html
}

func kpiMonthDetailFixture() map[string]any {
	return map[string]any{
		"Type":       "living",
		"Title":      "Monthly Living Expenses",
		"Month":      "2026-08",
		"MonthLabel": "August 2026",
		"Transactions": []models.Transaction{
			{Date: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), Amount: -136.75, Description: "grocery", Category: "Groceries"},
			{Date: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC), Amount: 65, Description: "Amazon", Category: "Shopping"},
		},
		"Count":        2,
		"Total":        71.75,
		"TotalLabel":   "Living Spent",
		"AvgAmount":    35.88,
		"IncomeTotal":  0.0,
		"ExpenseTotal": 0.0,
		"IsSavings":    false,
	}
}

// The transaction table's columns are sortable, so every header is a real
// button inside a scoped <th> (ACCESSIBILITY.md #2), keyed by the same column
// name the rows carry.
func TestRenderKPIMonthDetail_SortableHeaders(t *testing.T) {
	html := renderKPIMonthDetail(t, kpiMonthDetailFixture())

	if !strings.Contains(html, `id="kpi-month-txn-table"`) {
		t.Errorf("expected the transaction table to be identified for the sorter, got: %s", html)
	}
	for _, col := range []string{"date", "description", "category", "amount"} {
		th := fmt.Sprintf(`<th scope="col" data-sort=%q`, col)
		if !strings.Contains(html, th) {
			t.Errorf("expected scoped sortable header for %q (%s)", col, th)
		}
		btn := fmt.Sprintf(`<button type="button" data-sort-btn=%q`, col)
		if !strings.Contains(html, btn) {
			t.Errorf("expected header button for %q (%s)", col, btn)
		}
		arrow := fmt.Sprintf(`data-sort-arrow=%q`, col)
		if !strings.Contains(html, arrow) {
			t.Errorf("expected sort-direction arrow slot for %q", col)
		}
	}
}

// Sort keys ride on the rows as typed data-* attributes so the client never
// parses the rendered strings: ISO dates and signed two-decimal amounts.
func TestRenderKPIMonthDetail_RowsCarrySortKeys(t *testing.T) {
	html := renderKPIMonthDetail(t, kpiMonthDetailFixture())

	for _, want := range []string{
		`data-date="2026-08-05"`,
		`data-description="grocery"`,
		`data-category="Groceries"`,
		`data-amount="-136.75"`,
		`data-date="2026-08-09"`,
		`data-amount="65.00"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("expected row sort key %s in output, got: %s", want, html)
		}
	}
}

// CB6-4: the Total tile's color must track the kind's polarity, not paint
// every non-income kind red. Expense-like kinds (expenses/living/healthcare)
// are money OUT when positive (red) and a net refund/credit when <= 0
// (green) — the else-branch used to hardcode red for every one of them.
func TestRenderKPIMonthDetail_ExpensesNegativeTotalIsGreen(t *testing.T) {
	data := kpiMonthDetailFixture()
	data["Type"] = "expenses"
	data["Total"] = -42.50
	html := renderKPIMonthDetail(t, data)

	if !strings.Contains(html, `text-green-700 dark:text-green-400">`+formatMoney(-42.50)) {
		t.Errorf("expected a negative expenses Total (net refund) to render green, got: %s", html)
	}
	if strings.Contains(html, `text-red-600 dark:text-red-400">`+formatMoney(-42.50)) {
		t.Errorf("negative expenses Total must not render red, got: %s", html)
	}
}

func TestRenderKPIMonthDetail_ExpensesPositiveTotalIsRed(t *testing.T) {
	data := kpiMonthDetailFixture()
	data["Type"] = "expenses"
	data["Total"] = 42.50
	html := renderKPIMonthDetail(t, data)

	if !strings.Contains(html, `text-red-600 dark:text-red-400">`+formatMoney(42.50)) {
		t.Errorf("expected a positive expenses Total (money out) to render red, got: %s", html)
	}
	if strings.Contains(html, `text-green-700 dark:text-green-400">`+formatMoney(42.50)) {
		t.Errorf("positive expenses Total must not render green, got: %s", html)
	}
}

func TestRenderKPIMonthDetail_SavingsPositiveTotalIsGreen(t *testing.T) {
	data := kpiMonthDetailFixture()
	data["Type"] = "savings"
	data["IsSavings"] = true
	data["Total"] = 100.0
	html := renderKPIMonthDetail(t, data)

	if !strings.Contains(html, `text-green-700 dark:text-green-400">`+formatMoney(100.0)) {
		t.Errorf("expected a positive savings Total to render green, got: %s", html)
	}
}

// An empty month renders the no-transactions message, not an empty sortable
// table with headers that do nothing.
func TestRenderKPIMonthDetail_EmptyMonthHasNoTable(t *testing.T) {
	data := kpiMonthDetailFixture()
	data["Transactions"] = []models.Transaction{}
	data["Count"] = 0
	html := renderKPIMonthDetail(t, data)

	if strings.Contains(html, `id="kpi-month-txn-table"`) {
		t.Errorf("expected no transaction table for an empty month, got: %s", html)
	}
	if !strings.Contains(html, "No transactions in August 2026") {
		t.Errorf("expected the empty-state message, got: %s", html)
	}
}
