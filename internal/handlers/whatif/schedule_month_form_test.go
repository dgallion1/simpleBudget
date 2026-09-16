package whatif

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
)

// Every scheduled date the What-If page shows or accepts is a CALENDAR MONTH
// (RC3): the forms are <input type="month"> anchored to the plan start, and
// the offsets the plan stores are converted with the one pair
// models.CalendarMonth / models.CalendarMonthLabel. These tests pin the
// behaviour that makes the monthly rollover safe — an untouched re-save can
// never move a date, and a date the rollover shifted off a year boundary is
// still shown as the month it really is.
//
// Fixture plan start is 2026-10, so offset 11 is Sep 2027, offset 23 is
// Sep 2028 and offset -1 is Sep 2026.

const (
	schedPlanStart  = "2026-10"
	schedStartValue = "2027-09" // offset 11
	schedThroughVal = "2028-09" // offset 23 = the last month of EndMonth 24
)

func scheduleFormFixture(t *testing.T, rm *retirement.SettingsManager) {
	t.Helper()
	s, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.UseCurrentMonth = false
	s.StartDate = schedPlanStart
	s.ProjectionYears = 12
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	incomeEnd := 24
	expenseEnd := 24
	s.IncomeSources = []models.IncomeSource{
		{ID: "sched-inc", Name: "SchedPension", Amount: 1000, Type: models.IncomeFixed, StartMonth: 11, EndMonth: &incomeEnd},
	}
	s.ExpenseSources = []models.ExpenseSource{
		{ID: "sched-exp", Name: "SchedBoat", Amount: 500, StartMonth: 11, EndMonth: &expenseEnd},
	}
	s.OneTimeExpenses = []models.OneTimeExpense{
		{ID: "sched-ote", Description: "SchedRoof", Month: 11, Amount: 12000},
		{ID: "sched-ote-past", Description: "SchedOldRoof", Month: -1, Amount: 5},
	}
	s.BigTicketItems = []models.BigTicketItem{
		{ID: "sched-bt", Name: "SchedCar", Amount: 5000, Month: 11, Type: models.BigTicketExpense, TaxTreatment: models.TaxNone},
	}
	s.RemovedBigTicketItems = []models.BigTicketItem{
		{ID: "sched-bt-old", Name: "SchedOldBoat", Amount: 900, Month: -1, Type: models.BigTicketIncome, TaxTreatment: models.TaxNone},
	}
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	rm.InvalidateCache()
}

// collapseWhitespace folds runs of whitespace (including the newlines the
// templates put between attributes) into single spaces, so an assertion can
// name a whole attribute sequence as the browser sees it.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func whatIfPageBody(t *testing.T) string {
	t.Helper()
	w := httptest.NewRecorder()
	handleWhatIf(w, httptest.NewRequest("GET", "/whatif", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /whatif = %d; body: %s", w.Code, truncate(w.Body.String(), 800))
	}
	return w.Body.String()
}

func postScheduleForm(t *testing.T, h http.HandlerFunc, path, id string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	params := map[string]string{}
	if id != "" {
		params["id"] = id
	}
	w := httptest.NewRecorder()
	h(w, chiRequest("POST", path, formBody(form), params))
	return w
}

// Criterion 1/3: the page renders calendar-month controls and calendar-month
// labels, and no year-offset wording survives for these four entry kinds.
func TestScheduleForms_PageShowsCalendarMonths(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	scheduleFormFixture(t, rm)

	body := collapseWhitespace(whatIfPageBody(t))

	for _, want := range []string{
		`id="income-start-sched-inc" name="start_month" value="2027-09" min="2026-10"`,
		`id="income-end-sched-inc" name="end_month" value="2028-09" min="2026-10"`,
		`id="expense-start-sched-exp" name="start_month" value="2027-09" min="2026-10"`,
		`id="expense-end-sched-exp" name="end_month" value="2028-09" min="2026-10"`,
		`<input type="month" id="add-income-start-month" name="start_month"`,
		`<input type="month" id="add-expense-start-month" name="start_month"`,
		`<input type="month" id="onetime-month" name="month"`,
		`<input type="month" id="add-bigticket-month" name="month"`,
		// The one-time card, the big-ticket list and the restore list name
		// the month, and a past entry says so.
		"Sep 2027",
		"Sep 2026 (past)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}

	// Every month input is programmatically labelled (no placeholder-as-label)
	// and points at its own help text.
	for _, want := range []string{
		`<label for="income-start-sched-inc"`, `<label for="income-end-sched-inc"`,
		`<label for="expense-start-sched-exp"`, `<label for="expense-end-sched-exp"`,
		`<label for="add-income-start-month"`, `<label for="add-income-end-month"`,
		`<label for="add-expense-start-month"`, `<label for="add-expense-end-month"`,
		`<label for="onetime-month"`, `<label for="add-bigticket-month"`,
		`aria-describedby="income-schedule-sched-inc"`, `id="income-schedule-sched-inc"`,
		`aria-describedby="expense-schedule-sched-exp"`, `id="expense-schedule-sched-exp"`,
		`aria-describedby="onetime-month-help"`, `id="onetime-month-help"`,
		`aria-describedby="add-bigticket-month-help"`, `id="add-bigticket-month-help"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing accessibility wiring %q", want)
		}
	}

	// Criterion 6: the year-offset wording is gone from these entries. The
	// Roth conversion card is year-granular and out of RC3's scope, so it
	// keeps exactly one start_year/end_year input — pinned by count so a
	// schedule form cannot quietly regain one.
	for _, bad := range []string{
		"Starts yr", "Ends yr", `name="year"`, "(yr 0)", "(yr 1)", "Year 0", "Year 1 (",
		"add-income-start-year", "add-expense-start-year", "add-bigticket-year", "onetime-year",
	} {
		if strings.Contains(body, bad) {
			t.Errorf("page still renders year-offset markup %q", bad)
		}
	}
	if got := strings.Count(body, `name="start_year"`); got != 1 {
		t.Errorf(`name="start_year" inputs = %d, want exactly 1 (the out-of-scope Roth card)`, got)
	}
	if got := strings.Count(body, `name="end_year"`); got != 1 {
		t.Errorf(`name="end_year" inputs = %d, want exactly 1 (the out-of-scope Roth card)`, got)
	}
}

// Criterion 4: re-submitting the rendered edit form UNCHANGED leaves the
// stored offsets exactly as they were. This is the whole point of the
// calendar-month input: the old year box truncated 11 months to 0 and moved
// the date on an untouched save.
func TestScheduleForms_EditRoundTripIsExact(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	scheduleFormFixture(t, rm)

	// The values below are the ones the page actually rendered.
	body := whatIfPageBody(t)
	if !strings.Contains(body, `value="`+schedStartValue+`"`) || !strings.Contains(body, `value="`+schedThroughVal+`"`) {
		t.Fatalf("fixture did not render the expected month values")
	}

	if w := postScheduleForm(t, handleWhatIfUpdateIncome, "/whatif/income/sched-inc", "sched-inc",
		url.Values{"start_month": {schedStartValue}, "end_month": {schedThroughVal}}); w.Code != http.StatusOK {
		t.Fatalf("update income = %d; body: %s", w.Code, truncate(w.Body.String(), 400))
	}
	if w := postScheduleForm(t, handleWhatIfUpdateExpense, "/whatif/expense/sched-exp", "sched-exp",
		url.Values{"start_month": {schedStartValue}, "end_month": {schedThroughVal}, "inflation": {"on"}}); w.Code != http.StatusOK {
		t.Fatalf("update expense = %d; body: %s", w.Code, truncate(w.Body.String(), 400))
	}

	rm.InvalidateCache()
	s, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.IncomeSources[0].StartMonth != 11 {
		t.Errorf("income StartMonth moved: %d, want 11", s.IncomeSources[0].StartMonth)
	}
	if s.IncomeSources[0].EndMonth == nil || *s.IncomeSources[0].EndMonth != 24 {
		t.Errorf("income EndMonth moved: %v, want 24", s.IncomeSources[0].EndMonth)
	}
	if s.ExpenseSources[0].StartMonth != 11 {
		t.Errorf("expense StartMonth moved: %d, want 11", s.ExpenseSources[0].StartMonth)
	}
	if s.ExpenseSources[0].EndMonth == nil || *s.ExpenseSources[0].EndMonth != 24 {
		t.Errorf("expense EndMonth moved: %v, want 24", s.ExpenseSources[0].EndMonth)
	}
}

// A blank "Through" month is perpetual, and re-saving a perpetual entry keeps
// it perpetual.
func TestScheduleForms_BlankThroughMonthIsPerpetual(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	scheduleFormFixture(t, rm)

	for _, pass := range []string{"clear", "re-save"} {
		if w := postScheduleForm(t, handleWhatIfUpdateIncome, "/whatif/income/sched-inc", "sched-inc",
			url.Values{"start_month": {schedStartValue}, "end_month": {""}}); w.Code != http.StatusOK {
			t.Fatalf("%s: update income = %d", pass, w.Code)
		}
		rm.InvalidateCache()
		s, err := rm.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if s.IncomeSources[0].EndMonth != nil {
			t.Fatalf("%s: blank Through month left EndMonth %v", pass, s.IncomeSources[0].EndMonth)
		}
		if s.IncomeSources[0].StartMonth != 11 {
			t.Fatalf("%s: start month moved to %d", pass, s.IncomeSources[0].StartMonth)
		}
	}
}

// Criterion 2/4: the add forms store the offset the submitted month names.
// One-time and big-ticket entries have no edit form (add/delete only), so the
// add path IS their round trip: the month goes in, the offset is stored, and
// the card names the same month back.
func TestScheduleForms_AddStoresTheSubmittedMonth(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	scheduleFormFixture(t, rm)

	if w := postScheduleForm(t, handleWhatIfAddIncome, "/whatif/income", "",
		url.Values{"name": {"SchedAnnuity"}, "amount": {"250"}, "start_month": {"2028-01"}, "end_month": {"2029-12"}}); w.Code != http.StatusOK {
		t.Fatalf("add income = %d; body: %s", w.Code, truncate(w.Body.String(), 400))
	}
	if w := postScheduleForm(t, handleWhatIfAddExpense, "/whatif/expense", "",
		url.Values{"name": {"SchedLease"}, "amount": {"300"}, "start_month": {"2027-01"}}); w.Code != http.StatusOK {
		t.Fatalf("add expense = %d; body: %s", w.Code, truncate(w.Body.String(), 400))
	}
	if w := postScheduleForm(t, handleWhatIfAddOneTime, "/whatif/onetime", "",
		url.Values{"description": {"SchedWedding"}, "amount": {"8000"}, "month": {schedStartValue}}); w.Code != http.StatusOK {
		t.Fatalf("add one-time = %d; body: %s", w.Code, truncate(w.Body.String(), 400))
	}
	if w := postScheduleForm(t, handleWhatIfAddBigTicket, "/whatif/bigticket", "",
		url.Values{"name": {"SchedSale"}, "amount": {"9000"}, "month": {schedStartValue}, "type": {"income"}, "tax_treatment": {"none"}}); w.Code != http.StatusOK {
		t.Fatalf("add big-ticket = %d; body: %s", w.Code, truncate(w.Body.String(), 400))
	}

	rm.InvalidateCache()
	s, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var annuity *models.IncomeSource
	for i := range s.IncomeSources {
		if s.IncomeSources[i].Name == "SchedAnnuity" {
			annuity = &s.IncomeSources[i]
		}
	}
	// 2028-01 is 15 months after 2026-10; "Through 2029-12" is the last month
	// paid, so the stored end-exclusive EndMonth is one month later (39).
	if annuity == nil || annuity.StartMonth != 15 {
		t.Fatalf("annuity start offset: %+v, want StartMonth 15", annuity)
	}
	if annuity.EndMonth == nil || *annuity.EndMonth != 39 {
		t.Fatalf("annuity EndMonth: %v, want 39 (Dec 2029 is the last month paid)", annuity.EndMonth)
	}

	var lease *models.ExpenseSource
	for i := range s.ExpenseSources {
		if s.ExpenseSources[i].Name == "SchedLease" {
			lease = &s.ExpenseSources[i]
		}
	}
	if lease == nil || lease.StartMonth != 3 || lease.EndMonth != nil {
		t.Fatalf("lease offsets: %+v, want StartMonth 3 and a nil EndMonth", lease)
	}

	foundOTE := false
	for _, e := range s.OneTimeExpenses {
		if e.Description == "SchedWedding" && e.Month == 11 {
			foundOTE = true
		}
	}
	foundBT := false
	for _, b := range s.BigTicketItems {
		if b.Name == "SchedSale" && b.Month == 11 {
			foundBT = true
		}
	}
	if !foundOTE {
		t.Errorf("one-time expense not stored at month 11: %+v", s.OneTimeExpenses)
	}
	if !foundBT {
		t.Errorf("big-ticket item not stored at month 11: %+v", s.BigTicketItems)
	}

	// The cards name the same month back — the add path's round trip.
	body := whatIfPageBody(t)
	for _, want := range []string{"SchedWedding", "SchedSale", "Sep 2027"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q after the adds", want)
		}
	}
}

// Criterion 2: a month before the plan start, an inverted range and an
// unparseable month are all 400s — on add AND on edit — and the plan-start
// rejection names the plan's own first month, which is what the form shows.
func TestScheduleForms_RejectMonthsOutsideThePlan(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	scheduleFormFixture(t, rm)

	cases := []struct {
		name     string
		h        http.HandlerFunc
		path, id string
		form     url.Values
		wantText string
	}{
		{"add income before plan start", handleWhatIfAddIncome, "/whatif/income", "",
			url.Values{"name": {"X"}, "amount": {"1"}, "start_month": {"2026-09"}}, "Oct 2026"},
		{"add income inverted range", handleWhatIfAddIncome, "/whatif/income", "",
			url.Values{"name": {"X"}, "amount": {"1"}, "start_month": {"2028-01"}, "end_month": {"2027-06"}}, ""},
		{"add income unparseable", handleWhatIfAddIncome, "/whatif/income", "",
			url.Values{"name": {"X"}, "amount": {"1"}, "start_month": {"whenever"}}, ""},
		{"add expense before plan start", handleWhatIfAddExpense, "/whatif/expense", "",
			url.Values{"name": {"X"}, "amount": {"1"}, "start_month": {"2001-01"}}, "Oct 2026"},
		{"add expense inverted range", handleWhatIfAddExpense, "/whatif/expense", "",
			url.Values{"name": {"X"}, "amount": {"1"}, "start_month": {"2028-01"}, "end_month": {"2027-06"}}, ""},
		{"add one-time before plan start", handleWhatIfAddOneTime, "/whatif/onetime", "",
			url.Values{"description": {"X"}, "amount": {"1"}, "month": {"2026-09"}}, "Oct 2026"},
		{"add one-time unparseable", handleWhatIfAddOneTime, "/whatif/onetime", "",
			url.Values{"description": {"X"}, "amount": {"1"}, "month": {"soon"}}, ""},
		{"add big-ticket before plan start", handleWhatIfAddBigTicket, "/whatif/bigticket", "",
			url.Values{"name": {"X"}, "amount": {"1"}, "month": {"2020-01"}, "type": {"expense"}, "tax_treatment": {"none"}}, "Oct 2026"},
		{"add big-ticket unparseable", handleWhatIfAddBigTicket, "/whatif/bigticket", "",
			url.Values{"name": {"X"}, "amount": {"1"}, "month": {"2027"}, "type": {"expense"}, "tax_treatment": {"none"}}, ""},
		{"edit income before plan start", handleWhatIfUpdateIncome, "/whatif/income/sched-inc", "sched-inc",
			url.Values{"start_month": {"2026-01"}}, "Oct 2026"},
		{"edit income inverted range", handleWhatIfUpdateIncome, "/whatif/income/sched-inc", "sched-inc",
			url.Values{"start_month": {"2028-01"}, "end_month": {"2027-06"}}, ""},
		{"edit expense before plan start", handleWhatIfUpdateExpense, "/whatif/expense/sched-exp", "sched-exp",
			url.Values{"start_month": {"2026-01"}}, "Oct 2026"},
		{"edit expense unparseable", handleWhatIfUpdateExpense, "/whatif/expense/sched-exp", "sched-exp",
			url.Values{"start_month": {"march"}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postScheduleForm(t, tc.h, tc.path, tc.id, tc.form)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", w.Code, truncate(w.Body.String(), 400))
			}
			if tc.wantText != "" && !strings.Contains(w.Body.String(), tc.wantText) {
				t.Errorf("error should name the plan-start month %q; got: %s", tc.wantText, truncate(w.Body.String(), 400))
			}
		})
	}

	// Nothing was written by any rejected request.
	rm.InvalidateCache()
	s, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.IncomeSources) != 1 || s.IncomeSources[0].StartMonth != 11 {
		t.Errorf("rejected requests changed the income sources: %+v", s.IncomeSources)
	}
	if len(s.ExpenseSources) != 1 || s.ExpenseSources[0].StartMonth != 11 {
		t.Errorf("rejected requests changed the expense sources: %+v", s.ExpenseSources)
	}
	if len(s.OneTimeExpenses) != 2 || len(s.BigTicketItems) != 1 {
		t.Errorf("rejected requests wrote an entry: %+v / %+v", s.OneTimeExpenses, s.BigTicketItems)
	}
}

// The one-time horizon rejection is now expressed in months.
func TestScheduleForms_OneTimeBeyondHorizonNamesTheHorizon(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	scheduleFormFixture(t, rm)

	// ProjectionYears is 12, so 2038-10 is offset 144 — the first month out.
	w := postScheduleForm(t, handleWhatIfAddOneTime, "/whatif/onetime", "",
		url.Values{"description": {"TooLate"}, "amount": {"1"}, "month": {"2038-10"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, truncate(w.Body.String(), 400))
	}
	for _, want := range []string{"Oct 2038", "12-year projection horizon"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("rejection should mention %q; got: %s", want, truncate(w.Body.String(), 400))
		}
	}

	// The last month INSIDE the horizon is accepted.
	if w := postScheduleForm(t, handleWhatIfAddOneTime, "/whatif/onetime", "",
		url.Values{"description": {"JustInside"}, "amount": {"1"}, "month": {"2038-09"}}); w.Code != http.StatusOK {
		t.Fatalf("last in-horizon month rejected: %d; body: %s", w.Code, truncate(w.Body.String(), 400))
	}
}

// Criterion 5: the rollover shift and the display agree on the UI path. A plan
// whose file was saved three months ago with a start_month of 12 must render
// as the month TWELVE months after that saved anchor — nine months from now —
// in both the form value and the label.
func TestScheduleForms_RolloverKeepsTheCalendarMonth(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	now := time.Now()
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	s.UseCurrentMonth = true
	s.StartDate = thisMonth.AddDate(0, -3, 0).Format("2006-01")
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	s.IncomeSources = []models.IncomeSource{
		{ID: "roll-inc", Name: "RollPension", Amount: 1000, Type: models.IncomeFixed, StartMonth: 12},
	}
	s.OneTimeExpenses = []models.OneTimeExpense{{ID: "roll-ote", Description: "RollRoof", Month: 12, Amount: 100}}

	// Write the file directly: going through Save would re-anchor it first,
	// which is exactly the shift this test wants to observe on load.
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rm.SettingsDir(), "whatif.json"), raw, 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	rm.InvalidateCache()

	body := whatIfPageBody(t)
	scheduled := thisMonth.AddDate(0, 9, 0) // 12 months after an anchor 3 months old
	wantValue := scheduled.Format("2006-01")
	wantLabel := scheduled.Format("Jan 2006")
	if !strings.Contains(body, `value="`+wantValue+`"`) {
		t.Errorf("income month input should carry %q after the rollover shift", wantValue)
	}
	if !strings.Contains(body, wantLabel) {
		t.Errorf("the page should name %q after the rollover shift", wantLabel)
	}
	// The min attribute is the (re-anchored) plan start, i.e. this month.
	if !strings.Contains(body, `min="`+thisMonth.Format("2006-01")+`"`) {
		t.Errorf("month inputs should allow this month (%s) at the earliest", thisMonth.Format("2006-01"))
	}
}
