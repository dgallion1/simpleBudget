package whatif

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"budget2/internal/models"
)

// Every mutating what-if handler answers with "whatif-results-with-oob"
// (handlers.go:195 and :446), which re-renders the big-ticket and one-time
// lists out of band. That partial is a SECOND caller of the item templates,
// separate from the cards, so a change to an item template's data contract
// has to land in both places: when it does not, the mutation still returns
// 200 but swaps "Internal Server Error" into #bigticket-list and truncates,
// silently dropping the #onetime-list block that follows (ruling
// 2026-09-16b).
func TestMutationOOBPartial_RendersScheduleListsCompletely(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.UseCurrentMonth = false
	s.StartDate = "2026-10"
	s.ProjectionYears = 12
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	// Month 11 from a 2026-10 plan start is Sep 2027 — deliberately off a
	// year boundary, the state the monthly rollover produces.
	s.BigTicketItems = []models.BigTicketItem{
		{ID: "bt-1", Name: "CarPurchase", Amount: 5000, Month: 11, Type: models.BigTicketExpense},
	}
	s.OneTimeExpenses = []models.OneTimeExpense{
		{ID: "ote-1", Description: "RoofJob", Month: 11, Amount: 12000},
	}
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	rm.InvalidateCache()

	// Drive a REAL mutation handler so the assertion covers the response body
	// the browser actually swaps in, not a hand-assembled render.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(url.Values{
		"name":   {"BoatSale"},
		"amount": {"9000"},
		"year":   {"2"},
		"type":   {"income"},
	}))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()

	for _, bad := range []string{"Internal Server Error", "Error rendering", "can't evaluate", "incompatible types"} {
		if strings.Contains(body, bad) {
			t.Fatalf("template failure leaked into the OOB partial (%q); body: %s", bad, truncate(body, 1200))
		}
	}

	// Both pre-existing entries render with the calendar label...
	for _, want := range []string{"CarPurchase", "RoofJob", "Sep 2027"} {
		if !strings.Contains(body, want) {
			t.Errorf("want %q in the OOB partial; body: %s", want, truncate(body, 1500))
		}
	}
	if n := strings.Count(body, "Sep 2027"); n < 2 {
		t.Errorf("want the Sep 2027 label for BOTH the big-ticket and the one-time entry, got %d occurrence(s)", n)
	}
	// ...and the newly added item is there too.
	if !strings.Contains(body, "BoatSale") {
		t.Errorf("newly added big-ticket item missing from the OOB partial")
	}

	// The #onetime-list block follows #bigticket-list in the partial, so its
	// presence is what proves the response was not truncated by a failure in
	// the big-ticket block.
	if !strings.Contains(body, `id="bigticket-list"`) {
		t.Errorf(`OOB partial is missing id="bigticket-list"`)
	}
	if !strings.Contains(body, `id="onetime-list"`) {
		t.Errorf(`OOB partial is missing id="onetime-list" — the response truncated before it`)
	}
	if strings.Index(body, `id="onetime-list"`) < strings.Index(body, `id="bigticket-list"`) {
		t.Errorf("expected #onetime-list to follow #bigticket-list in the partial")
	}
}

// The full page is the other renderer of the same item templates.
func TestWhatIfPage_RendersScheduleListsCompletely(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.UseCurrentMonth = false
	s.StartDate = "2026-10"
	s.ProjectionYears = 12
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	s.BigTicketItems = []models.BigTicketItem{
		{ID: "bt-1", Name: "CarPurchase", Amount: 5000, Month: 11, Type: models.BigTicketExpense},
	}
	s.RemovedBigTicketItems = []models.BigTicketItem{
		{ID: "bt-old", Name: "OldBoat", Amount: 1000, Month: 11, Type: models.BigTicketIncome},
	}
	s.OneTimeExpenses = []models.OneTimeExpense{
		{ID: "ote-1", Description: "RoofJob", Month: 11, Amount: 12000},
	}
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	rm.InvalidateCache()

	w := httptest.NewRecorder()
	handleWhatIf(w, httptest.NewRequest("GET", "/whatif", nil))

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, bad := range []string{"Internal Server Error", "Error rendering", "can't evaluate", "incompatible types"} {
		if strings.Contains(body, bad) {
			t.Fatalf("template failure leaked into the page (%q); body: %s", bad, truncate(body, 1200))
		}
	}
	for _, want := range []string{"CarPurchase", "OldBoat", "RoofJob", "Sep 2027"} {
		if !strings.Contains(body, want) {
			t.Errorf("want %q on the page", want)
		}
	}
}
