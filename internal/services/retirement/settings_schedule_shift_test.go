package retirement

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

// Every schedule in a plan is an offset from StartDate, and StartDate advances
// to the current month on every load and save. Unless those offsets move with
// it, an income scheduled twelve months out drifts one month further away
// every month. These tests pin the shift: the same calendar month stays the
// same calendar month across a rollover.

func shiftFixture() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.UseCurrentMonth = true
	s.StartDate = "2026-09"
	s.Persons = []models.Person{
		{ID: "you", Name: "You", Role: models.PersonRolePrimary, BirthMonth: "1961-09"},
	}
	incomeEnd := 24
	expenseEnd := 24
	s.IncomeSources = []models.IncomeSource{
		{ID: "i", Name: "Pension", Amount: 1000, Type: models.IncomeFixed, StartMonth: 12, EndMonth: &incomeEnd},
	}
	s.RemovedIncomeSources = []models.IncomeSource{
		{ID: "ri", Name: "Old job", Amount: 1, Type: models.IncomeFixed, StartMonth: 12},
	}
	s.ExpenseSources = []models.ExpenseSource{
		{ID: "e", Name: "Boat", Amount: 500, StartMonth: 12, EndMonth: &expenseEnd},
	}
	s.RemovedExpenseSources = []models.ExpenseSource{
		{ID: "re", Name: "Old gym", Amount: 1, StartMonth: 12},
	}
	s.OneTimeExpenses = []models.OneTimeExpense{
		{ID: "o", Description: "Roof", Month: 12, Amount: 12000},
	}
	s.BigTicketItems = []models.BigTicketItem{
		{ID: "b", Name: "Car", Amount: 5000, Month: 12, Type: models.BigTicketExpense},
	}
	s.RemovedBigTicketItems = []models.BigTicketItem{
		{ID: "rb", Name: "Old car", Amount: 1, Month: 12, Type: models.BigTicketExpense},
	}
	return s
}

// checkOffsets asserts the offsets of every one of the four entry kinds,
// including the Removed* lists — a removed entry the user may still restore
// must not have silently drifted while it sat in the bin.
func checkOffsets(t *testing.T, s *models.WhatIfSettings, label string, incStart, incEnd, expStart, expEnd, oneTime, bigTicket int) {
	t.Helper()
	i := s.IncomeSources[0]
	if i.StartMonth != incStart || i.EndMonth == nil || *i.EndMonth != incEnd {
		t.Errorf("%s: income start=%d end=%v, want %d/%d", label, i.StartMonth, i.EndMonth, incStart, incEnd)
	}
	e := s.ExpenseSources[0]
	if e.StartMonth != expStart || e.EndMonth == nil || *e.EndMonth != expEnd {
		t.Errorf("%s: expense start=%d end=%v, want %d/%d", label, e.StartMonth, e.EndMonth, expStart, expEnd)
	}
	if got := s.OneTimeExpenses[0].Month; got != oneTime {
		t.Errorf("%s: one-time month = %d, want %d", label, got, oneTime)
	}
	if got := s.BigTicketItems[0].Month; got != bigTicket {
		t.Errorf("%s: big-ticket month = %d, want %d", label, got, bigTicket)
	}
	if got := s.RemovedIncomeSources[0].StartMonth; got != incStart {
		t.Errorf("%s: removed income start = %d, want %d (removed entries shift alike)", label, got, incStart)
	}
	if got := s.RemovedExpenseSources[0].StartMonth; got != expStart {
		t.Errorf("%s: removed expense start = %d, want %d (removed entries shift alike)", label, got, expStart)
	}
	if got := s.RemovedBigTicketItems[0].Month; got != bigTicket {
		t.Errorf("%s: removed big-ticket month = %d, want %d (removed entries shift alike)", label, got, bigTicket)
	}
}

func atMonth(t *testing.T, month string) time.Time {
	t.Helper()
	now, err := time.Parse("2006-01", month)
	if err != nil {
		t.Fatalf("parse %q: %v", month, err)
	}
	return now
}

func TestMonthsBetween(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		want     int
		wantOK   bool
	}{
		{"2026-09", "2026-09", 0, true},
		{"2026-09", "2026-10", 1, true},
		{"2026-09", "2027-09", 12, true},
		{"2026-09", "2027-10", 13, true},
		{"2026-09", "2026-08", -1, true},
		{"2027-01", "2026-12", -1, true},
		{"2026-09", "2024-09", -24, true},
		{"nope", "2027-10", 0, false},
		{"2026-09", "nope", 0, false},
		{"", "", 0, false},
		{"2026-13", "2026-09", 0, false},
	} {
		got, ok := monthsBetween(tc.from, tc.to)
		if ok != tc.wantOK || (tc.wantOK && got != tc.want) {
			t.Errorf("monthsBetween(%q,%q) = %d,%v; want %d,%v", tc.from, tc.to, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestShiftScheduleOffsetsZeroIsNoOp(t *testing.T) {
	s := shiftFixture()
	shiftScheduleOffsets(s, 0)
	checkOffsets(t, s, "zero shift", 12, 24, 12, 24, 12, 12)
}

func TestShiftScheduleOffsetsNilSettings(t *testing.T) {
	shiftScheduleOffsets(nil, 3) // must not panic
}

// Forward: a year passes one month at a time and the schedule stays put on
// the calendar. Starts and ends clamp at 0 for income and expense sources (an
// ended source stays in the plan, contributing nothing); one-time and
// big-ticket entries go negative instead, because a single-month event that
// has passed must be retained without ever being charged again.
func TestResolveCurrentMonthShiftsSchedulesForward(t *testing.T) {
	s := shiftFixture()

	resolveCurrentMonth(s, atMonth(t, "2026-10"))
	if s.StartDate != "2026-10" {
		t.Fatalf("StartDate = %s, want 2026-10", s.StartDate)
	}
	checkOffsets(t, s, "after one month", 11, 23, 11, 23, 11, 11)

	resolveCurrentMonth(s, atMonth(t, "2026-10"))
	checkOffsets(t, s, "same month again is a no-op", 11, 23, 11, 23, 11, 11)

	resolveCurrentMonth(s, atMonth(t, "2027-09"))
	checkOffsets(t, s, "the scheduled month arrives", 0, 12, 0, 12, 0, 0)

	resolveCurrentMonth(s, atMonth(t, "2027-10"))
	checkOffsets(t, s, "one month past: starts clamp, events go negative", 0, 11, 0, 11, -1, -1)

	resolveCurrentMonth(s, atMonth(t, "2029-01"))
	checkOffsets(t, s, "long past: ends clamp at 0, events keep counting down", 0, 0, 0, 0, -16, -16)
}

// Backwards (a file saved on a machine whose clock ran ahead) is symmetric:
// the schedule moves out, it is not clamped or dropped.
func TestResolveCurrentMonthShiftsSchedulesBackward(t *testing.T) {
	s := shiftFixture()
	resolveCurrentMonth(s, atMonth(t, "2026-08"))
	if s.StartDate != "2026-08" {
		t.Fatalf("StartDate = %s, want 2026-08", s.StartDate)
	}
	checkOffsets(t, s, "one month backwards", 13, 25, 13, 25, 13, 13)
}

// A fixed-date plan is never advanced, so its schedule is never shifted.
func TestResolveCurrentMonthLeavesFixedDatePlanAlone(t *testing.T) {
	s := shiftFixture()
	s.UseCurrentMonth = false

	resolveCurrentMonth(s, atMonth(t, "2030-01"))

	if s.StartDate != "2026-09" {
		t.Fatalf("fixed-date plan StartDate moved to %s", s.StartDate)
	}
	checkOffsets(t, s, "fixed-date plan", 12, 24, 12, 24, 12, 12)
}

// An unparseable anchor has no meaningful elapsed count, so nothing shifts
// (StartDate is still advanced, as it always was).
func TestResolveCurrentMonthUnparseableAnchorDoesNotShift(t *testing.T) {
	s := shiftFixture()
	s.StartDate = "garbage"

	resolveCurrentMonth(s, atMonth(t, "2027-10"))

	if s.StartDate != "2027-10" {
		t.Fatalf("StartDate = %s, want 2027-10", s.StartDate)
	}
	checkOffsets(t, s, "unparseable anchor", 12, 24, 12, 24, 12, 12)
}

// A perpetual source (nil EndMonth) stays perpetual: the shift must not
// invent an end month for it.
func TestShiftScheduleOffsetsKeepsPerpetualSourcesPerpetual(t *testing.T) {
	s := shiftFixture()
	s.IncomeSources[0].EndMonth = nil
	s.ExpenseSources[0].EndMonth = nil

	resolveCurrentMonth(s, atMonth(t, "2026-10"))

	if s.IncomeSources[0].EndMonth != nil {
		t.Errorf("income EndMonth = %d, want nil (still perpetual)", *s.IncomeSources[0].EndMonth)
	}
	if s.ExpenseSources[0].EndMonth != nil {
		t.Errorf("expense EndMonth = %d, want nil (still perpetual)", *s.ExpenseSources[0].EndMonth)
	}
	if s.IncomeSources[0].StartMonth != 11 || s.ExpenseSources[0].StartMonth != 11 {
		t.Errorf("starts did not shift: income %d expense %d", s.IncomeSources[0].StartMonth, s.ExpenseSources[0].StartMonth)
	}
}

// The persisted start_date is the anchor, and saveInternal resolves the month
// through the same function the load path uses. A load→save→load cycle must
// therefore shift exactly once: the file written back carries both the new
// anchor and the already-shifted offsets, and reading it again finds nothing
// left to do.
func TestManagerLoadSaveLoadDoesNotDoubleShift(t *testing.T) {
	root := t.TempDir()
	store, err := storage.New(root)
	if err != nil {
		t.Fatal(err)
	}
	sm := NewSettingsManager(root, store)

	now := time.Now()
	thisMonth := now.Format("2006-01")
	savedThreeMonthsAgo := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -3, 0).Format("2006-01")

	// Written with the LEGACY year keys, the shape a stored plan really has.
	doc := `{"use_current_month":true,"start_date":"` + savedThreeMonthsAgo + `","projection_years":5,
	 "persons":[{"id":"you","name":"You","role":"primary","birth_month":"1961-09"}],
	 "income_sources":[{"id":"i","name":"Pension","amount":1000,"income_type":"fixed","start_month":12}],
	 "expense_sources":[{"id":"e","name":"Boat","amount":500,"start_year":1,"end_year":2}],
	 "one_time_expenses":[{"id":"o","description":"Roof","year":1,"amount":12000}],
	 "big_ticket_items":[{"id":"b","name":"Car","amount":5000,"year":1,"type":"expense"}]}`
	path := filepath.Join(root, defaultWhatIfFilename)
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	// Three months have passed, so every 12-month offset is now 9 months out
	// and the expense's 24-month end is 21 months out.
	want := func(s *models.WhatIfSettings, label string) {
		t.Helper()
		if s.StartDate != thisMonth {
			t.Errorf("%s: StartDate = %s, want %s", label, s.StartDate, thisMonth)
		}
		if got := s.IncomeSources[0].StartMonth; got != 9 {
			t.Errorf("%s: income StartMonth = %d, want 9", label, got)
		}
		if got := s.ExpenseSources[0].StartMonth; got != 9 {
			t.Errorf("%s: expense StartMonth = %d, want 9", label, got)
		}
		if s.ExpenseSources[0].EndMonth == nil || *s.ExpenseSources[0].EndMonth != 21 {
			t.Errorf("%s: expense EndMonth = %v, want 21", label, s.ExpenseSources[0].EndMonth)
		}
		if got := s.OneTimeExpenses[0].Month; got != 9 {
			t.Errorf("%s: one-time Month = %d, want 9", label, got)
		}
		if got := s.BigTicketItems[0].Month; got != 9 {
			t.Errorf("%s: big-ticket Month = %d, want 9", label, got)
		}
	}

	first, err := sm.Load()
	if err != nil {
		t.Fatal(err)
	}
	want(first, "first load")

	if err := sm.Save(first); err != nil {
		t.Fatal(err)
	}
	sm.InvalidateCache()

	second, err := sm.Load()
	if err != nil {
		t.Fatal(err)
	}
	want(second, "reload after save")

	third, _, err := sm.LoadContextWithRevision(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want(third, "cached load")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk struct {
		StartDate     string `json:"start_date"`
		IncomeSources []struct {
			StartMonth int `json:"start_month"`
		} `json:"income_sources"`
		ExpenseSources []struct {
			StartMonth int  `json:"start_month"`
			EndMonth   *int `json:"end_month"`
		} `json:"expense_sources"`
		OneTimeExpenses []struct {
			Month int `json:"month"`
		} `json:"one_time_expenses"`
		BigTicketItems []struct {
			Month int `json:"month"`
		} `json:"big_ticket_items"`
	}
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.StartDate != thisMonth {
		t.Errorf("on-disk start_date = %s, want %s (the anchor must be written with the shifted offsets)", onDisk.StartDate, thisMonth)
	}
	if onDisk.IncomeSources[0].StartMonth != 9 || onDisk.ExpenseSources[0].StartMonth != 9 ||
		onDisk.ExpenseSources[0].EndMonth == nil || *onDisk.ExpenseSources[0].EndMonth != 21 ||
		onDisk.OneTimeExpenses[0].Month != 9 || onDisk.BigTicketItems[0].Month != 9 {
		t.Errorf("on-disk offsets not shifted: %+v", onDisk)
	}
}

// UpdateSettingsWithPersons is the manual-edit path (D2): the Rate
// Assumptions form posts a new StartDate whenever it saves, whether the user
// dragged the date picker or just changed some other field on the form. It
// must re-anchor every schedule offset with the exact rule the monthly
// rollover uses (shiftScheduleOffsets via monthsBetween) — not leave every
// scheduled item to silently drift by the delta.

func newTestSMForShift(t *testing.T) *SettingsManager {
	t.Helper()
	root := t.TempDir()
	store, err := storage.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return NewSettingsManager(root, store)
}

// Forward: pushing the fixed start date one month later must move every
// offset one month earlier so the calendar month is unchanged. Mutation (b)
// — shifting by +delta instead of -delta — turns 11 into 13 here.
func TestUpdateSettingsWithPersonsShiftsSchedulesForward(t *testing.T) {
	sm := newTestSMForShift(t)
	s := shiftFixture()
	s.UseCurrentMonth = false
	if err := sm.Save(s); err != nil {
		t.Fatal(err)
	}

	got, _, err := sm.UpdateSettingsWithPersons(map[string]interface{}{}, "2026-10", s.Persons)
	if err != nil {
		t.Fatalf("UpdateSettingsWithPersons: %v", err)
	}
	if got.StartDate != "2026-10" {
		t.Fatalf("StartDate = %s, want 2026-10", got.StartDate)
	}
	checkOffsets(t, got, "manual start date advanced one month", 11, 23, 11, 23, 11, 11)
}

// Backward is symmetric: an earlier manual start date pushes every offset
// later. Mutation (a) — removing the shift call entirely — leaves these
// offsets at the fixture's original 12/24/12 values instead.
func TestUpdateSettingsWithPersonsShiftsSchedulesBackward(t *testing.T) {
	sm := newTestSMForShift(t)
	s := shiftFixture()
	s.UseCurrentMonth = false
	if err := sm.Save(s); err != nil {
		t.Fatal(err)
	}

	got, _, err := sm.UpdateSettingsWithPersons(map[string]interface{}{}, "2026-08", s.Persons)
	if err != nil {
		t.Fatalf("UpdateSettingsWithPersons: %v", err)
	}
	if got.StartDate != "2026-08" {
		t.Fatalf("StartDate = %s, want 2026-08", got.StartDate)
	}
	checkOffsets(t, got, "manual start date moved one month earlier", 13, 25, 13, 25, 13, 13)
}

// Saving the form with the start date field unchanged — but some other field
// edited — must be a no-op for every schedule offset. Mutation (c) —
// shifting even when the start date did not change — would move these
// offsets even though StartDate posted back exactly what was already saved.
func TestUpdateSettingsWithPersonsUnchangedStartDateIsNoOp(t *testing.T) {
	sm := newTestSMForShift(t)
	s := shiftFixture()
	s.UseCurrentMonth = false
	if err := sm.Save(s); err != nil {
		t.Fatal(err)
	}

	got, _, err := sm.UpdateSettingsWithPersons(map[string]interface{}{"portfolio_value": 999.0}, s.StartDate, s.Persons)
	if err != nil {
		t.Fatalf("UpdateSettingsWithPersons: %v", err)
	}
	if got.PortfolioValue != 999 {
		t.Fatalf("portfolio_value not applied: %f", got.PortfolioValue)
	}
	if got.StartDate != "2026-09" {
		t.Fatalf("StartDate = %s, want unchanged 2026-09", got.StartDate)
	}
	checkOffsets(t, got, "unchanged start date, unrelated field changed", 12, 24, 12, 24, 12, 12)
}

// Ticking "Use current month" on a plan whose fixed start is in the past
// posts this same start_date field set to the current month (the page's own
// onchange handler does this — see rate-assumptions.html). It must drift
// through the identical shift as any other manual date edit, not silently
// skip it because resolveCurrentMonth, running again inside Save, sees the
// StartDate this function already adopted and has nothing left to do.
func TestUpdateSettingsWithPersonsShiftsSchedulesWhenTogglingUseCurrentMonth(t *testing.T) {
	sm := newTestSMForShift(t)
	s := shiftFixture()
	s.UseCurrentMonth = false
	s.StartDate = "2020-01"
	if err := sm.Save(s); err != nil {
		t.Fatal(err)
	}

	thisMonth := time.Now().Format("2006-01")
	elapsed, ok := monthsBetween("2020-01", thisMonth)
	if !ok {
		t.Fatal("monthsBetween(2020-01, thisMonth) failed")
	}

	got, _, err := sm.UpdateSettingsWithPersons(map[string]interface{}{"use_current_month": true}, thisMonth, s.Persons)
	if err != nil {
		t.Fatalf("UpdateSettingsWithPersons: %v", err)
	}
	if got.StartDate != thisMonth || !got.UseCurrentMonth {
		t.Fatalf("StartDate = %s UseCurrentMonth = %v, want %s / true", got.StartDate, got.UseCurrentMonth, thisMonth)
	}
	checkOffsets(t, got, "use-current-month toggled on a past fixed start",
		max(0, 12-elapsed), max(0, 24-elapsed), max(0, 12-elapsed), max(0, 24-elapsed), 12-elapsed, 12-elapsed)
}
