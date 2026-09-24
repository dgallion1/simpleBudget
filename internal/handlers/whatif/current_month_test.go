package whatif

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
)

func TestCurrentMonthFormToggleAndRender(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	for _, enabled := range []bool{true, false} {
		vals := url.Values{
			"start_date": {"2020-01"}, "person_id[]": {"primary"},
			"person_name[]": {"Alex"}, "person_birth_month[]": {"1960-09"}, "person_role[]": {"primary"},
		}
		if enabled {
			vals.Set("use_current_month", "true")
		}
		req := httptest.NewRequest(http.MethodPost, "/whatif/settings", formBody(vals))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handleWhatIfSettings(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("save: %d: %s", w.Code, w.Body.String())
		}
		s, err := rm.Load()
		if err != nil {
			t.Fatal(err)
		}
		want := "2020-01"
		if enabled {
			want = time.Now().Format("2006-01")
		}
		if s.StartDate != want || s.UseCurrentMonth != enabled {
			t.Fatalf("toggle %v: date %s mode %v", enabled, s.StartDate, s.UseCurrentMonth)
		}
		out, err := renderer.RenderToString("whatif-rate-assumptions", map[string]interface{}{"Settings": s})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "Use current month") || strings.Contains(out, "readonly") != enabled {
			t.Fatal("form does not reflect selected mode")
		}
	}
}

// A manual Projection Start Date change posted through the real HTTP handler
// (the Rate Assumptions form always posts start_date on save) must keep
// every schedule on its calendar month end-to-end, the same re-anchoring
// resolveCurrentMonth already gives the monthly rollover (D2).
func TestHandleWhatIfSettings_ManualStartDateShiftsSchedules(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.UseCurrentMonth = false
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "primary", Name: "Alex", BirthMonth: "1960-05", Role: models.PersonRolePrimary},
	}
	incomeEnd := 24
	settings.IncomeSources = []models.IncomeSource{
		{ID: "i", Name: "Pension", Amount: 1000, Type: models.IncomeFixed, StartMonth: 12, EndMonth: &incomeEnd},
	}
	settings.OneTimeExpenses = []models.OneTimeExpense{
		{ID: "o", Description: "Roof", Month: 12, Amount: 12000},
	}
	settings.BigTicketItems = []models.BigTicketItem{
		{ID: "b", Name: "Car", Amount: 5000, Month: 12, Type: models.BigTicketExpense},
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	vals := url.Values{
		"start_date":           {"2026-05"}, // one calendar month later
		"person_id[]":          {"primary"},
		"person_name[]":        {"Alex"},
		"person_birth_month[]": {"1960-05"},
		"person_role[]":        {"primary"},
	}
	req := httptest.NewRequest(http.MethodPost, "/whatif/settings", formBody(vals))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StartDate != "2026-05" {
		t.Fatalf("StartDate = %s, want 2026-05", loaded.StartDate)
	}
	if got := loaded.IncomeSources[0].StartMonth; got != 11 {
		t.Errorf("income StartMonth = %d, want 11 (same calendar month as before the edit)", got)
	}
	if got := *loaded.IncomeSources[0].EndMonth; got != 23 {
		t.Errorf("income EndMonth = %d, want 23", got)
	}
	if got := loaded.OneTimeExpenses[0].Month; got != 11 {
		t.Errorf("one-time Month = %d, want 11", got)
	}
	if got := loaded.BigTicketItems[0].Month; got != 11 {
		t.Errorf("big-ticket Month = %d, want 11", got)
	}
}
