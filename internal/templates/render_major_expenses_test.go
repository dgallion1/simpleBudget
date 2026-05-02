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

// TestRenderMajorExpenses_UnmatchedBucketShowsAllRowsWithDimming
// verifies that AllUnmatched (the comprehensive list) drives the
// "Unmatched" exception bucket: every row renders, big rows are
// red, sub-threshold rows are dimmed (opacity-70 class), and the
// header badge anchors to the bucket.
func TestRenderMajorExpenses_UnmatchedBucketShowsAllRowsWithDimming(t *testing.T) {
	now := time.Now()
	html := renderMajorExpensesContent(t, map[string]any{
		"Title":          "Major Expenses",
		"ActiveTab":      "major-expenses",
		"Expenses":       []models.MajorExpense{},
		"Summaries":      []struct{}{},
		"Match":          map[string]any{"Exceptions": models.ExceptionsReport{}},
		"AllUnmatched": []models.Transaction{
			{Date: now, Amount: -250, Description: "Big Unknown Charge", Hash: "h-big"},
			{Date: now, Amount: -19.44, Description: "Tiny Coffee", Hash: "h-tiny"},
		},
		"Threshold":      100.0,
		"WindowDays":     30,
		"TotalDeclared":  0.0,
		"UnmatchedTotal": 269.44,
		"UnmatchedCount": 2,
	})
	if !strings.Contains(html, "Big Unknown Charge") {
		t.Errorf("expected over-threshold row in output, got: %s", html)
	}
	if !strings.Contains(html, "Tiny Coffee") {
		t.Errorf("expected sub-threshold row in output (was previously hidden), got: %s", html)
	}
	// Sub-threshold row should be dimmed; over-threshold should not be.
	if !strings.Contains(html, `data-fill-name="Tiny Coffee"`) {
		t.Errorf("expected tiny row data-fill-name attribute")
	}
	// Header badge is now an anchor to the bucket.
	if !strings.Contains(html, `href="#major-expenses-bucket-unknown-large"`) {
		t.Errorf("expected header Unmatched badge to anchor to the bucket id, got: %s", html)
	}
	// The bucket label is now generic 'Unmatched', not '… over $100'.
	if strings.Contains(html, "Unmatched over $") {
		t.Errorf("expected bucket title to be 'Unmatched' (no '… over $X'), got: %s", html)
	}
	// AllUnmatched render path: each row's Description must link back
	// to the Explorer for the bank text, with click-propagation stopped
	// so the row's pre-fill handler does not fire alongside navigation.
	if !strings.Contains(html, `<a href="/explorer?search=Big&#43;Unknown&#43;Charge&type=Outflow"`) {
		t.Errorf("expected AllUnmatched description to link to /explorer, got html=%s", html)
	}
	if !strings.Contains(html, `<a href="/explorer?search=Tiny&#43;Coffee&type=Outflow"`) {
		t.Errorf("expected sub-threshold AllUnmatched description to link to /explorer, got html=%s", html)
	}
	if got := strings.Count(html, `onclick="event.stopPropagation()"`); got < 2 {
		t.Errorf("expected stopPropagation handler on each AllUnmatched description anchor (2 rows), got %d. html=%s", got, html)
	}
}

// TestRenderMajorExpenses_UnmatchedBadgeAndDeletedPanel verifies the
// new visibility additions: the amber "Unmatched: $X · N txns" badge in
// the list-card header, and the Deleted panel that surfaces archived
// expenses with Restore/Discard affordances.
func TestRenderMajorExpenses_UnmatchedBadgeAndDeletedPanel(t *testing.T) {
	now := time.Now()
	deleted := []models.DeletedMajorExpense{
		{
			Expense:      models.MajorExpense{ID: "gone", Name: "Vanished Subscription"},
			DeletedAt:    now.AddDate(0, 0, -3),
			PinnedHashes: []string{"hh1", "hh2"},
		},
	}
	html := renderMajorExpensesContent(t, map[string]any{
		"Title":          "Major Expenses",
		"ActiveTab":      "major-expenses",
		"Expenses":       []models.MajorExpense{},
		"Summaries":      []struct{}{},
		"Match":          map[string]any{"Exceptions": models.ExceptionsReport{}},
		"Threshold":      100.0,
		"WindowDays":     30,
		"TotalDeclared":  0.0,
		"UnmatchedTotal": 1215.57,
		"UnmatchedCount": 42,
		"Deleted":        deleted,
	})
	if !strings.Contains(html, "Unmatched:") {
		t.Errorf("expected 'Unmatched:' badge in header, got: %s", html)
	}
	if !strings.Contains(html, "$1216") {
		t.Errorf("expected unmatched total '$1216' (rounded), got: %s", html)
	}
	if !strings.Contains(html, "42 txns") {
		t.Errorf("expected '42 txns' count in header, got: %s", html)
	}
	if !strings.Contains(html, "Vanished Subscription") {
		t.Errorf("expected deleted expense name in panel, got: %s", html)
	}
	if !strings.Contains(html, `hx-post="/major-expenses/gone/restore"`) {
		t.Errorf("expected restore form for archived id, got: %s", html)
	}
	if !strings.Contains(html, `hx-delete="/major-expenses/deleted/gone"`) {
		t.Errorf("expected discard form for archived id, got: %s", html)
	}
}

// TestRenderMajorExpenses_PinPickerNewSentinelAndCurrent verifies the
// pin picker's new behavior: it includes a "+ Create new from this…"
// sentinel option, and pre-selects an existing pin via CurrentPin.
func TestRenderMajorExpenses_PinPickerNewSentinelAndCurrent(t *testing.T) {
	now := time.Now()
	html := renderMajorExpensesContent(t, map[string]any{
		"Title":     "Major Expenses",
		"ActiveTab": "major-expenses",
		"Expenses": []models.MajorExpense{
			{ID: "rent", Name: "Rent", Keywords: []string{"landlord"}},
			{ID: "food", Name: "Food", Keywords: []string{"grocery"}},
		},
		"ExpenseOptions": []struct {
			ID    string
			Label string
		}{
			{ID: "rent", Label: "Rent"},
			{ID: "food", Label: "Food"},
		},
		"Summaries": []struct{}{},
		"Match": struct {
			Exceptions models.ExceptionsReport
		}{
			Exceptions: models.ExceptionsReport{
				UnknownLarge: []models.ExceptionUnknownLargeTxn{
					{Transaction: models.Transaction{Date: now, Amount: -250, Description: "Big Thing", Hash: "h-big"}},
				},
				NewMerchants: []models.ExceptionNewMerchant{
					{Description: "matched merchant", FirstSeen: now, Transaction: models.Transaction{Date: now, Amount: -50, Description: "Whole Foods", Hash: "h-matched"}},
				},
				Threshold:     100,
				NewWindowDays: 30,
			},
		},
		"PinMap": map[string]string{
			"h-big": "rent",
		},
		"MatchedHashToExpenseID": map[string]string{
			"h-matched": "food",
		},
		"ExpenseByID": map[string]models.MajorExpense{
			"rent": {ID: "rent", Name: "Rent"},
			"food": {ID: "food", Name: "Food"},
		},
		"TotalDeclared": 0.0,
		"Threshold":     100.0,
		"WindowDays":    30,
	})
	// Sentinel option present in any rendered picker.
	if !strings.Contains(html, `<option value="__new__"`) {
		t.Errorf("expected '+ Create new from this' sentinel option, got: %s", html)
	}
	// Current pin pre-selected for the unknown-large row pinned to rent.
	if !strings.Contains(html, `<option value="rent" title="Rent" selected>Rent</option>`) {
		t.Errorf("expected rent option to be pre-selected for h-big, got: %s", html)
	}
	// New-merchant matched row shows the badge instead of a dropdown.
	if !strings.Contains(html, "Whole Foods") {
		t.Errorf("expected new-merchant description in output")
	}
	// The matched-state branch renders the matched expense name as a link
	// and does NOT render a pin picker for that row.
	if !strings.Contains(html, `data-jump-expense="food"`) {
		t.Errorf("expected matched-state link with data-jump-expense=food, got: %s", html)
	}
}

// TestRenderMajorExpenses_MultipleEntriesAllRender guards against the
// regression where $.PinnedHashes was referenced inside the per-item
// sub-template (where $ is the Summary, not the page). The template
// errored mid-loop and only the first entry rendered.
func TestRenderMajorExpenses_MultipleEntriesAllRender(t *testing.T) {
	type summary struct {
		Expense      models.MajorExpense
		Count        int
		PinnedCount  int
		Total        float64
		Transactions []models.Transaction
		PinnedHashes map[string]bool
	}
	now := time.Now()
	mkSum := func(name string) summary {
		return summary{
			Expense:      models.MajorExpense{ID: name, Name: name, Keywords: []string{name}},
			Count:        1,
			PinnedCount:  1,
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
		"PinnedHashes":  map[string]bool{},
		"Threshold":     100.0,
		"WindowDays":    30,
		"TotalDeclared": 300.0,
	})
	for _, name := range []string{"Lucid", "Hyundai", "Wegmans"} {
		if !strings.Contains(html, `id="major-expense-item-`+name+`"`) {
			t.Errorf("expected summary row id %q to render", name)
		}
		if !strings.Contains(html, `data-expense-id="`+name+`"`) {
			t.Errorf("expected tbody for %q", name)
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
		"Title":         "Major Expenses",
		"ActiveTab":     "major-expenses",
		"Expenses":      []models.MajorExpense{},
		"Summaries":     []struct{}{},
		"Match":         map[string]any{"Exceptions": models.ExceptionsReport{}},
		"Threshold":     100.0,
		"WindowDays":    30,
		"TotalDeclared": 0.0,
	})
	if !strings.Contains(html, "No major expenses declared yet") {
		t.Errorf("expected empty-state copy, got: %s", html)
	}
	if !strings.Contains(html, "Click the + above") {
		t.Errorf("expected empty-state to point at the new add icon, got: %s", html)
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
		PinnedCount  int
		Total        float64
		Transactions []models.Transaction
		PinnedHashes map[string]bool
	}
	html := renderMajorExpensesContent(t, map[string]any{
		"Title":     "Major Expenses",
		"ActiveTab": "major-expenses",
		"Expenses": []models.MajorExpense{
			{ID: "rent", Name: "Rent", Keywords: []string{"landlord"}, ExpectedMin: 1500, ExpectedMax: 2000},
		},
		"Summaries": []summary{
			{
				Expense:      models.MajorExpense{ID: "rent", Name: "Rent", Keywords: []string{"landlord"}, ExpectedMin: 1500, ExpectedMax: 2000},
				Count:        3,
				Total:        4800,
				Transactions: []models.Transaction{{Date: now, Amount: -1700, Description: "Landlord LLC", MajorExpenseName: "Rent", Hash: "h-rent-1"}},
			},
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
		"Threshold":     100.0,
		"WindowDays":    30,
		"TotalDeclared": 4800.0,
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
	// Summary row keeps the stable jump target id.
	if !strings.Contains(html, `id="major-expense-item-rent"`) {
		t.Errorf("expected summary row to keep id used by jump links")
	}
	// Edit form moved out of the row form-wrapping into the detail cell
	// and got its own id.
	if !strings.Contains(html, `id="major-expense-edit-rent"`) {
		t.Errorf("expected edit form id=major-expense-edit-rent inside detail row")
	}
	// Each expense renders one tbody[data-expense-id] containing a
	// summary tr + detail tr.
	if !strings.Contains(html, `data-expense-id="rent"`) {
		t.Errorf("expected one tbody[data-expense-id=rent] per declared expense")
	}
	if !strings.Contains(html, `data-open="false"`) {
		t.Errorf("expected initial collapsed state data-open=\"false\" on tbody")
	}
	// Detail row carries id used by aria-controls and the JS toggle
	// selector.
	if !strings.Contains(html, `id="major-expense-detail-rent"`) {
		t.Errorf("expected detail row id used by aria-controls")
	}
	if !strings.Contains(html, `class="major-expense-detail-row`) {
		t.Errorf("expected detail row class used by CSS toggle")
	}
	// Chevron button must expose aria-expanded so accessibility tests
	// and the JS toggle can find it.
	if !strings.Contains(html, `aria-expanded="false"`) {
		t.Errorf("expected chevron button with aria-expanded=false")
	}
	if !strings.Contains(html, `aria-controls="major-expense-detail-rent"`) {
		t.Errorf("expected chevron aria-controls referencing detail row id")
	}
	// Add form is wrapped in a details panel toggled by the [+] icon.
	if !strings.Contains(html, `id="major-expenses-add-panel"`) {
		t.Errorf("expected add form to be wrapped in details#major-expenses-add-panel")
	}
	if !strings.Contains(html, `id="major-expenses-add-form"`) {
		t.Errorf("expected add form to keep id used by click-to-prefill handler")
	}
	if !strings.Contains(html, `id="major-expenses-add-toggle"`) {
		t.Errorf("expected [+] toggle button id used by header click handler")
	}
	// The summary row carries the row class so the unified search JS
	// can iterate it (the form no longer wraps the row in this layout).
	if !strings.Contains(html, `class="major-expense-item-row`) {
		t.Errorf("expected summary tr to carry major-expense-item-row class")
	}
	// Matched-txn rows still carry the row class so the unified search
	// JS can match them inside the same tbody group.
	if !strings.Contains(html, `class="major-expense-matched-row`) {
		t.Errorf("expected matched-txn row to carry major-expense-matched-row class")
	}
	// Header surfaces the total declared. The data attribute carries
	// the precise value (printf "%.2f") so a regression in the template
	// expression or format verb is caught here, not just at runtime.
	if !strings.Contains(html, `data-total-declared="4800.00"`) {
		t.Errorf("expected data-total-declared=\"4800.00\" in header, got html=%s", html)
	}
	// Unified search input (was per-card exception search; now page-level).
	if !strings.Contains(html, `id="major-expenses-search"`) {
		t.Errorf("expected unified search input id=major-expenses-search")
	}
	if strings.Contains(html, `id="major-expenses-exception-search"`) {
		t.Errorf("old per-card exception search input must be removed once the unified bar lands")
	}
	if !strings.Contains(html, `class="major-expenses-exception-row`) {
		t.Errorf("expected rows to carry the exception-row class targeted by the filter script")
	}
	// Each Major Expense item exposes a data-search built from name +
	// keywords + notes so the unified filter can match the item itself.
	if !strings.Contains(html, `data-search="Rent landlord`) {
		t.Errorf("expected expense item data-search to include name and keywords, got html=%s", html)
	}
	// Matched-txn rows inside an item carry their own data-search so the
	// unified filter can locate the transaction inside the item.
	if !strings.Contains(html, `data-search="Landlord LLC $1700.00 `) {
		t.Errorf("expected matched-txn row data-search to include label + amount + date")
	}
	// Matched-txn description must link to the explorer filtered to that
	// bank text so the user can drill into other references for it.
	// Go html/template encodes "+" in URL contexts as "&#43;".
	if !strings.Contains(html, `href="/explorer?search=Landlord&#43;LLC&type=Outflow"`) {
		t.Errorf("expected matched-txn description to link to /explorer?search=<desc>&type=Outflow, got html=%s", html)
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
	// The persist-open-state JS relies on stable disclosure IDs to
	// snapshot/restore <details> across HTMX swaps. If a future edit
	// drops an ID the bucket will close on every pin again.
	for _, id := range []string{
		`id="major-expenses-bucket-unknown-large"`,
		`id="major-expenses-bucket-anomalous"`,
		`id="major-expenses-bucket-new-merchants"`,
	} {
		if !strings.Contains(html, id) {
			t.Errorf("expected disclosure ID %s for HTMX-swap open-state persistence", id)
		}
	}
	// Each exception row exposes the transaction hash so the bulk-pin
	// toolbar can collect them without parsing form values.
	if !strings.Contains(html, `data-hash=`) {
		t.Errorf("expected exception rows to expose data-hash for bulk pinning")
	}
	// Each exception bucket's Description column links to the Explorer
	// pre-filtered to that bank text (Outflow), giving users a path back
	// to the underlying bank transaction. The anchor must stop click
	// propagation so the row-level click handler (which pre-fills the
	// add form) does not also fire. Spaces are URL-encoded as "+" and
	// then HTML-escaped as "&#43;" by html/template in attribute context.
	if !strings.Contains(html, `<a href="/explorer?search=Random&#43;Big&#43;Purchase&type=Outflow"`) {
		t.Errorf("expected unknown-large description to link to /explorer, got html=%s", html)
	}
	if !strings.Contains(html, `<a href="/explorer?search=My&#43;Landlord&#43;LLC&type=Outflow"`) {
		t.Errorf("expected anomalous description to link to /explorer, got html=%s", html)
	}
	if !strings.Contains(html, `<a href="/explorer?search=Brand&#43;New&#43;Store&type=Outflow"`) {
		t.Errorf("expected new-merchant description to link to /explorer, got html=%s", html)
	}
	// One stopPropagation per exception description anchor (3 in this
	// fixture: UnknownLarge, Anomalous, NewMerchants). The matched-row
	// anchor does NOT use stopPropagation (no row click handler there),
	// so the count is a tight lower bound on the exception anchors.
	if got := strings.Count(html, `onclick="event.stopPropagation()"`); got < 3 {
		t.Errorf("expected at least 3 stopPropagation handlers on exception description anchors, got %d. html=%s", got, html)
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
		PinnedCount  int
		Total        float64
		Transactions []models.Transaction
		PinnedHashes map[string]bool
	}
	html, err := r.RenderToString("major-expenses-results", map[string]any{
		"Expenses":  []models.MajorExpense{},
		"Summaries": []summary{},
		"Match": struct {
			Exceptions models.ExceptionsReport
		}{Exceptions: models.ExceptionsReport{Threshold: 100, NewWindowDays: 30}},
		"Threshold":     100.0,
		"WindowDays":    30,
		"TotalDeclared": 0.0,
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
