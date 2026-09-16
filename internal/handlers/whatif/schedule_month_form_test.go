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
		`aria-describedby="onetime-month-help whatif-add-onetime-error"`, `id="onetime-month-help"`,
		`aria-describedby="add-bigticket-month-help whatif-add-bigticket-error"`, `id="add-bigticket-month-help"`,
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

// writeSettingsFile persists settings WITHOUT going through Save, whose own
// resolve would re-anchor them first. Loading the file afterwards is what puts
// the plan through the real monthly rollover.
func writeSettingsFile(t *testing.T, rm *retirement.SettingsManager, s *models.WhatIfSettings) {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rm.SettingsDir(), "whatif.json"), raw, 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	rm.InvalidateCache()
}

// clampedFixture writes a plan anchored 20 months ago holding entries the
// rollover must clamp, and returns this month's first day. After the load:
//
//	exp-ended     StartMonth 0→0, EndMonth 3→0   (ended 17 months ago)
//	exp-clamped   StartMonth 4→0, EndMonth 30→10 (started before the plan)
//	inc-clamped   StartMonth 12→0, EndMonth 42→22 (running since the plan start)
//	inc-ended     StartMonth 0→0, EndMonth 2→0   (ended 18 months ago)
//
// The clamp is lossy on purpose (RC2: never delete user data, never charge a
// past entry), which is exactly why the rows below must not claim to know a
// month the clamp threw away.
func clampedFixture(t *testing.T, rm *retirement.SettingsManager) time.Time {
	t.Helper()
	s, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	now := time.Now()
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	s.UseCurrentMonth = true
	s.StartDate = thisMonth.AddDate(0, -20, 0).Format("2006-01")
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	endedEnd := 3
	clampedExpenseEnd := 30
	clampedIncomeEnd := 42
	endedIncomeEnd := 2
	s.ExpenseSources = []models.ExpenseSource{
		{ID: "exp-ended", Name: "ClampOldLease", Amount: 400, StartMonth: 0, EndMonth: &endedEnd},
		{ID: "exp-clamped", Name: "ClampGym", Amount: 100, StartMonth: 4, EndMonth: &clampedExpenseEnd, Inflation: true},
	}
	s.IncomeSources = []models.IncomeSource{
		{ID: "inc-clamped", Name: "ClampPension", Amount: 900, Type: models.IncomeFixed, StartMonth: 12, EndMonth: &clampedIncomeEnd},
		{ID: "inc-ended", Name: "ClampOldAnnuity", Amount: 300, Type: models.IncomeFixed, StartMonth: 0, EndMonth: &endedIncomeEnd},
	}
	s.OneTimeExpenses = nil
	s.BigTicketItems = nil
	s.RemovedBigTicketItems = nil
	writeSettingsFile(t, rm, s)

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load after write: %v", err)
	}
	if loaded.StartDate != thisMonth.Format("2006-01") {
		t.Fatalf("fixture: plan did not re-anchor to this month: %q", loaded.StartDate)
	}
	if loaded.ExpenseSources[0].StartMonth != 0 || loaded.ExpenseSources[0].EndMonth == nil || *loaded.ExpenseSources[0].EndMonth != 0 {
		t.Fatalf("fixture: ended expense not clamped to 0/0: %+v", loaded.ExpenseSources[0])
	}
	if loaded.ExpenseSources[1].StartMonth != 0 || loaded.ExpenseSources[1].EndMonth == nil || *loaded.ExpenseSources[1].EndMonth != 10 {
		t.Fatalf("fixture: clamped expense not 0/10: %+v", loaded.ExpenseSources[1])
	}
	if loaded.IncomeSources[0].StartMonth != 0 || loaded.IncomeSources[0].EndMonth == nil || *loaded.IncomeSources[0].EndMonth != 22 {
		t.Fatalf("fixture: clamped income not 0/22: %+v", loaded.IncomeSources[0])
	}
	if loaded.IncomeSources[1].StartMonth != 0 || loaded.IncomeSources[1].EndMonth == nil || *loaded.IncomeSources[1].EndMonth != 0 {
		t.Fatalf("fixture: ended income not clamped to 0/0: %+v", loaded.IncomeSources[1])
	}
	return thisMonth
}

// Ruling 2026-09-16e, criterion 3. The rollover clamps a past start AND a past
// end to 0, discarding both original months. A row must not invent them: an
// ENDED entry (EndMonth 0) is display-only, and a clamped START says "Since
// plan start" rather than claiming the entry was scheduled for that month.
func TestScheduleForms_ClampedRowsAreHonestAboutLostMonths(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	thisMonth := clampedFixture(t, rm)

	planStart := thisMonth.Format("2006-01")
	planLabel := thisMonth.Format("Jan 2006")
	monthBefore := thisMonth.AddDate(0, -1, 0).Format("2006-01")
	body := collapseWhitespace(whatIfPageBody(t))

	// (a) The ended row: what happened, plus Remove — and nothing else.
	for _, want := range []string{
		"ClampOldLease",
		"Ended before the plan start (" + planLabel + ")",
		`hx-delete="/whatif/expense/exp-ended"`,
		`aria-label="Delete expense ClampOldLease"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("ended row missing %q", want)
		}
	}
	for _, bad := range []string{
		`hx-put="/whatif/expense/exp-ended"`,
		`id="expense-start-exp-ended"`,
		`id="expense-end-exp-ended"`,
		`id="expense-schedule-exp-ended"`,
		// throughMonth(&0) names the month BEFORE the plan start; no input
		// may carry a value its own min forbids.
		`value="` + monthBefore + `"`,
	} {
		if strings.Contains(body, bad) {
			t.Errorf("ended row must not render %q", bad)
		}
	}

	// (b) The clamped-start rows keep their form, say "Since plan start", and
	// carry the plan-start month in the start input.
	for _, want := range []string{
		"Since plan start (" + planLabel + ")",
		`id="income-start-inc-clamped" name="start_month" value="` + planStart + `" min="` + planStart + `"`,
		`id="income-end-inc-clamped" name="end_month" value="` + thisMonth.AddDate(0, 21, 0).Format("2006-01") + `" min="` + planStart + `"`,
		`id="expense-start-exp-clamped" name="start_month" value="` + planStart + `" min="` + planStart + `"`,
		`id="expense-end-exp-clamped" name="end_month" value="` + thisMonth.AddDate(0, 9, 0).Format("2006-01") + `" min="` + planStart + `"`,
		`hx-put="/whatif/income/inc-clamped"`,
		`hx-put="/whatif/expense/exp-clamped"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("clamped-start row missing %q", want)
		}
	}
	if strings.Contains(body, "Starts "+planLabel) {
		t.Errorf("a clamped start must not claim the entry was scheduled for %s", planLabel)
	}

	// (c) The INCOME branch of the same rule (attempt 2 pinned only the
	// expense branch — ruling 2026-09-16h named that gap).
	for _, want := range []string{
		"ClampOldAnnuity",
		`hx-delete="/whatif/income/inc-ended"`,
		`aria-label="Delete income source ClampOldAnnuity"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("ended income row missing %q", want)
		}
	}
	for _, bad := range []string{
		`hx-put="/whatif/income/inc-ended"`,
		`id="income-start-inc-ended"`,
		`id="income-end-inc-ended"`,
		`id="income-schedule-inc-ended"`,
	} {
		if strings.Contains(body, bad) {
			t.Errorf("ended income row must not render %q", bad)
		}
	}
	if n := strings.Count(body, "Ended before the plan start ("+planLabel+")"); n != 2 {
		t.Errorf("want the ended wording on BOTH the ended income and the ended expense row, got %d", n)
	}

	// (d) No OTHER surface on this page may invent a month for an ended entry
	// or announce a start for one that is already running.
	invented := thisMonth.AddDate(0, -1, 0).Format("Jan 2006")
	for _, bad := range []string{
		"(through " + invented + ")",
		"(starts " + invented + ")",
		"ClampOldLease (through",
		"ClampPension starts",
		"ClampPension (starts",
	} {
		if strings.Contains(body, bad) {
			t.Errorf("a surface still renders %q for a rollover-clamped entry", bad)
		}
	}
	// The Budget Fit breakdown OMITS the ended expense. Counting is what makes
	// this specific: the ended entry appears only in its source-list row (name
	// + the delete control's accessible name), while a still-running sibling
	// also appears in each rendering of the breakdown, so a higher count for
	// the running one proves the card rendered and the omission is targeted.
	endedHits := strings.Count(body, "ClampOldLease")
	runningHits := strings.Count(body, "ClampGym")
	if endedHits != 2 {
		t.Errorf("the ended expense should appear only in its own row (name + delete label), got %d hits", endedHits)
	}
	if runningHits <= endedHits {
		t.Errorf("the still-running expense should also appear in the Budget Fit breakdown: running %d vs ended %d", runningHits, endedHits)
	}
	if !strings.Contains(body, `<span class="text-gray-600 dark:text-gray-400"> ClampGym`) {
		t.Errorf("the still-running expense is missing from the Budget Fit breakdown, so the omission test above proves nothing")
	}
	if strings.Contains(body, `<span class="text-gray-600 dark:text-gray-400"> ClampOldLease`) {
		t.Error("the ended expense is still a Budget Fit breakdown row")
	}
}

// Ruling 2026-09-16h, contract 2 (timeline events). A source already running
// at the plan start produces no "starts" event: for a clamped entry the real
// start month is gone, and for one scheduled at month 0 there is nothing to
// announce. A source that genuinely starts later still gets its event, so the
// rule is selective rather than a blanket suppression.
func TestClampedSchedules_TimelineSkipsAlreadyRunningSources(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 60
	settings.ProjectionYears = 15
	settings.Persons[0].BirthMonth = models.BirthMonthForAge(settings.StartDate, settings.CurrentAge)
	settings.IncomeSources = []models.IncomeSource{
		{ID: "clamped", Name: "Clamped Pension", Amount: 900, StartMonth: 0},
		{ID: "later", Name: "Deferred Pension", Amount: 500, StartMonth: 36},
		{ID: "ss-clamped", Name: "Social Security", Amount: 2000, StartMonth: 0},
	}

	events := buildProjectionChartEvents(settings, sampleProjectionForChart())

	var labels []string
	for _, e := range events {
		labels = append(labels, e.Label)
	}
	pensionStarts := 0
	for _, e := range events {
		if e.Label == "Social Security starts" {
			t.Errorf("a Social Security source running since the plan start must not announce a start: %v", labels)
		}
		if e.Label == "Pension starts" {
			pensionStarts++
			if e.Year != 3 {
				t.Errorf("the only pension event should be the deferred one at year 3, got year %v", e.Year)
			}
		}
	}
	if pensionStarts != 1 {
		t.Errorf("want exactly one pension start event (the deferred source), got %d in %v", pensionStarts, labels)
	}
}

// Ruling 2026-09-16h, contract 2 (removed/restore lists). Restoring an entry
// the rollover had already ended puts it back in the ended state, so the
// restored row is display-only too — the restore path cannot resurrect a
// month the clamp discarded.
func TestScheduleForms_RestoredEndedExpenseRendersAsEnded(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	thisMonth := clampedFixture(t, rm)
	planLabel := thisMonth.Format("Jan 2006")

	if _, err := rm.RemoveExpenseSource("exp-ended"); err != nil {
		t.Fatalf("RemoveExpenseSource: %v", err)
	}
	rm.InvalidateCache()
	if body := whatIfPageBody(t); !strings.Contains(body, "Recently Removed") {
		t.Fatalf("the removed expense is not in the restore list")
	}

	w := postScheduleForm(t, handleWhatIfRestoreExpense, "/whatif/expense/exp-ended/restore", "exp-ended", url.Values{})
	if w.Code != http.StatusOK {
		t.Fatalf("restore = %d; body: %s", w.Code, truncate(w.Body.String(), 400))
	}

	rm.InvalidateCache()
	s, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var restored *models.ExpenseSource
	for i := range s.ExpenseSources {
		if s.ExpenseSources[i].ID == "exp-ended" {
			restored = &s.ExpenseSources[i]
		}
	}
	if restored == nil || restored.EndMonth == nil || *restored.EndMonth != 0 {
		t.Fatalf("restored entry is not in the ended state: %+v", restored)
	}

	body := collapseWhitespace(whatIfPageBody(t))
	if !strings.Contains(body, "Ended before the plan start ("+planLabel+")") {
		t.Error("the restored row does not render as ended")
	}
	for _, bad := range []string{
		`hx-put="/whatif/expense/exp-ended"`,
		`id="expense-start-exp-ended"`,
		"ClampOldLease (through",
	} {
		if strings.Contains(body, bad) {
			t.Errorf("the restored row renders %q", bad)
		}
	}
}

// Ruling 2026-09-16e, criterion 4. htmx re-posts the whole row whenever any
// control in it changes, so every row that still HAS a form must accept its
// own rendered values back unchanged — a 400 on a date the user never touched
// makes the row unusable (a cola/inflation toggle could not be saved at all).
func TestScheduleForms_ClampedRowRoundTripsOnAnUnrelatedToggle(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	thisMonth := clampedFixture(t, rm)

	planStart := thisMonth.Format("2006-01")
	incomeThrough := thisMonth.AddDate(0, 21, 0).Format("2006-01") // EndMonth 22
	expenseThrough := thisMonth.AddDate(0, 9, 0).Format("2006-01") // EndMonth 10

	if w := postScheduleForm(t, handleWhatIfUpdateIncome, "/whatif/income/inc-clamped", "inc-clamped",
		url.Values{"start_month": {planStart}, "end_month": {incomeThrough}, "cola": {"on"}}); w.Code != http.StatusOK {
		t.Fatalf("untouched re-save of the clamped income row = %d; body: %s", w.Code, truncate(w.Body.String(), 500))
	}
	if w := postScheduleForm(t, handleWhatIfUpdateExpense, "/whatif/expense/exp-clamped", "exp-clamped",
		url.Values{"start_month": {planStart}, "end_month": {expenseThrough}, "inflation": {"on"}, "discretionary": {"on"}}); w.Code != http.StatusOK {
		t.Fatalf("untouched re-save of the clamped expense row = %d; body: %s", w.Code, truncate(w.Body.String(), 500))
	}

	rm.InvalidateCache()
	s, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.IncomeSources[0].StartMonth != 0 || s.IncomeSources[0].EndMonth == nil || *s.IncomeSources[0].EndMonth != 22 {
		t.Errorf("re-save moved the clamped income row: %d/%v, want 0/22",
			s.IncomeSources[0].StartMonth, s.IncomeSources[0].EndMonth)
	}
	if s.IncomeSources[0].COLARate == 0 {
		t.Error("the unrelated toggle (cola) did not take effect")
	}
	var gym *models.ExpenseSource
	for i := range s.ExpenseSources {
		if s.ExpenseSources[i].ID == "exp-clamped" {
			gym = &s.ExpenseSources[i]
		}
	}
	if gym == nil || gym.StartMonth != 0 || gym.EndMonth == nil || *gym.EndMonth != 10 {
		t.Errorf("re-save moved the clamped expense row: %+v, want 0/10", gym)
	}
	if gym != nil && !gym.Discretionary {
		t.Error("the unrelated toggle (discretionary) did not take effect")
	}

	// The ended row is still there, untouched and still display-only: the
	// rollover keeps user data, it just never charges it.
	if len(s.ExpenseSources) != 2 || s.ExpenseSources[0].ID != "exp-ended" {
		t.Fatalf("the ended row disappeared: %+v", s.ExpenseSources)
	}
	if s.ExpenseSources[0].EndMonth == nil || *s.ExpenseSources[0].EndMonth != 0 {
		t.Errorf("the ended row changed: %+v", s.ExpenseSources[0])
	}
}

// Ruling 2026-09-16f, criterion 7 (WCAG 4.1.3). A rejected month is swapped
// into the add form's error container by HX-Retarget + HX-Reswap:innerHTML, so
// that container must be a live region for the message to be announced — on
// the page AND in the OOB partial that clears it after every mutation, which
// replaces the element outright and would otherwise strip the role.
func TestScheduleForms_AddErrorsAreAnnouncedAndAssociated(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	scheduleFormFixture(t, rm)

	body := collapseWhitespace(whatIfPageBody(t))
	kinds := []struct{ kind, input, help string }{
		{"income", "add-income-start-month", "add-income-schedule-help"},
		{"income", "add-income-end-month", "add-income-schedule-help"},
		{"expense", "add-expense-start-month", "add-expense-schedule-help"},
		{"expense", "add-expense-end-month", "add-expense-schedule-help"},
		{"onetime", "onetime-month", "onetime-month-help"},
		{"bigticket", "add-bigticket-month", "add-bigticket-month-help"},
	}
	for _, k := range kinds {
		container := "whatif-add-" + k.kind + "-error"
		if !strings.Contains(body, `<div id="`+container+`" role="alert"`) {
			t.Errorf("%s error container is not a live region", container)
		}
		if !strings.Contains(body, `id="`+k.input+`" name=`) {
			t.Fatalf("month input %s not rendered", k.input)
		}
		want := `aria-describedby="` + k.help + ` ` + container + `"`
		if !strings.Contains(body, want) {
			t.Errorf("month input %s should carry %s", k.input, want)
		}
	}

	// The message really does land inside that container.
	w := postScheduleForm(t, handleWhatIfAddOneTime, "/whatif/onetime", "",
		url.Values{"description": {"X"}, "amount": {"1"}, "month": {"nope"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if got := w.Header().Get("HX-Retarget"); got != "#whatif-add-onetime-error" {
		t.Errorf("HX-Retarget = %q, want #whatif-add-onetime-error", got)
	}
	if got := w.Header().Get("HX-Reswap"); got != "innerHTML" {
		t.Errorf("HX-Reswap = %q, want innerHTML (an outerHTML swap would drop role=\"alert\")", got)
	}

	// ...and a SUCCESSFUL mutation's OOB partial, which clears the containers
	// by replacing them, keeps the role.
	ok := postScheduleForm(t, handleWhatIfAddOneTime, "/whatif/onetime", "",
		url.Values{"description": {"Fine"}, "amount": {"1"}, "month": {schedStartValue}})
	if ok.Code != http.StatusOK {
		t.Fatalf("add one-time = %d; body: %s", ok.Code, truncate(ok.Body.String(), 400))
	}
	oob := collapseWhitespace(ok.Body.String())
	for _, kind := range []string{"income", "expense", "onetime", "bigticket"} {
		want := `<div id="whatif-add-` + kind + `-error" role="alert" hx-swap-oob="true">`
		if !strings.Contains(oob, want) {
			t.Errorf("the OOB partial drops role=\"alert\" from the %s error container", kind)
		}
	}
}
