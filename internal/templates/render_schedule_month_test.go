package templates

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

// Schedules are stored as month offsets from the plan's StartDate. Every card
// that shows one renders it as a CALENDAR MONTH through
// models.CalendarMonthLabel, so no surface can print a stale or fractional
// "year N" once the monthly rollover moves an entry off a year boundary.

func scheduleRenderSettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.UseCurrentMonth = false
	s.StartDate = "2026-10"
	s.ProjectionYears = 5
	return s
}

func TestRenderOneTimeCard_ShowsCalendarMonth(t *testing.T) {
	r := newWhatIfRenderer(t)

	render := func(t *testing.T, month int) string {
		t.Helper()
		s := scheduleRenderSettings()
		s.OneTimeExpenses = []models.OneTimeExpense{{ID: "o", Description: "New Roof", Month: month, Amount: 12000}}
		out, err := r.RenderToString("whatif-onetime-card", map[string]any{"Settings": s})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		return collapse(out)
	}

	t.Run("year-aligned month", func(t *testing.T) {
		html := render(t, 12)
		if !strings.Contains(html, "Oct 2027") {
			t.Errorf("want Oct 2027 in: %s", html)
		}
	})

	// The rollover case: an entry shifted off a year boundary must still name
	// a real month, never a fraction.
	t.Run("non-year-aligned month", func(t *testing.T) {
		html := render(t, 11)
		if !strings.Contains(html, "Sep 2027") {
			t.Errorf("want Sep 2027 in: %s", html)
		}
		if strings.Contains(html, "0.91") || strings.Contains(html, "2026.9") {
			t.Errorf("fractional year leaked into the card: %s", html)
		}
	})

	t.Run("past month still names its month", func(t *testing.T) {
		html := render(t, -1)
		if !strings.Contains(html, "Sep 2026") {
			t.Errorf("want Sep 2026 in: %s", html)
		}
	})

	// The beyond-horizon warning uses whole projection years: month 59 is
	// inside a 5-year horizon, month 60 is not.
	t.Run("horizon warning is per whole year", func(t *testing.T) {
		const warning = "beyond current horizon"
		if html := render(t, 59); strings.Contains(html, warning) {
			t.Errorf("month 59 must be inside a 5-year horizon: %s", html)
		}
		if html := render(t, 60); !strings.Contains(html, warning) {
			t.Errorf("month 60 must be beyond a 5-year horizon: %s", html)
		}
	})
}

func TestRenderBigTicketCard_ShowsCalendarMonth(t *testing.T) {
	r := newWhatIfRenderer(t)

	s := scheduleRenderSettings()
	s.BigTicketItems = []models.BigTicketItem{
		{ID: "b", Name: "New Car", Amount: 30000, Month: 11, Type: models.BigTicketExpense},
	}
	s.RemovedBigTicketItems = []models.BigTicketItem{
		{ID: "rb", Name: "Old Boat", Amount: 9000, Month: 23, Type: models.BigTicketIncome},
	}

	out, err := r.RenderToString("whatif-bigticket-card", map[string]any{"Settings": s})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	html := collapse(out)

	for _, want := range []string{"New Car", "Sep 2027", "Old Boat", "Sep 2028"} {
		if !strings.Contains(html, want) {
			t.Errorf("want %q in: %s", want, html)
		}
	}
	// The item's own fields still render after the card started receiving
	// both the settings and the item.
	if !strings.Contains(html, `aria-label="Delete big ticket New Car"`) {
		t.Errorf("item controls lost their accessible name: %s", html)
	}
	if strings.Contains(html, "yr 0.9") || strings.Contains(html, "Year 0.9") {
		t.Errorf("fractional year leaked into the card: %s", html)
	}
}

// The expense list's inline edit inputs still speak whole years (RC3 replaces
// them with calendar-month inputs); this pins the offset→year conversion so a
// year-aligned source round-trips unchanged.
func TestRenderExpenseSourcesList_YearInputsFromMonthOffsets(t *testing.T) {
	r := newWhatIfRenderer(t)

	s := scheduleRenderSettings()
	end := 60
	s.ExpenseSources = []models.ExpenseSource{
		{ID: "e1", Name: "Boat", Amount: 500, StartMonth: 24, EndMonth: &end},
		{ID: "e2", Name: "Gym", Amount: 100, StartMonth: 0, EndMonth: nil},
	}

	out, err := r.RenderToString("whatif-expense-sources-list", map[string]any{"Settings": s})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	html := collapse(out)

	for _, want := range []string{
		`name="start_year" min="0" max="50" value="2"`,
		`name="end_year" min="0" max="50" value="5"`,
		`name="end_year" min="0" max="50" value=""`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("want %q in: %s", want, html)
		}
	}
}
