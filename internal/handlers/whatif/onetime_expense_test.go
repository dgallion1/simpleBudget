package whatif

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"budget2/internal/models"
)

// ── Add / Delete handlers ────────────────────────────────────────────────

func TestOneTimeExpense_HandlerAdd(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"description": {"New Roof"},
		"amount":      {"25000"},
		"year":        {"3"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/onetime", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddOneTime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestOneTimeExpense_HandlerAdd_MissingDescription(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"amount": {"1000"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/onetime", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddOneTime(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-add-onetime-error")
}

func TestOneTimeExpense_HandlerAdd_MissingAmount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"description": {"Roof"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/onetime", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddOneTime(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestOneTimeExpense_HandlerAdd_NegativeAmount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"description": {"Roof"}, "amount": {"-100"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/onetime", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddOneTime(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-add-onetime-error")
}

func TestOneTimeExpense_HandlerAdd_NegativeYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"description": {"Roof"}, "amount": {"1000"}, "year": {"-5"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/onetime", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddOneTime(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-add-onetime-error")
}

func TestOneTimeExpense_HandlerAdd_BadYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"description": {"Roof"}, "amount": {"1000"}, "year": {"abc"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/onetime", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddOneTime(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-add-onetime-error")
}

// TestOneTimeExpense_HandlerAdd_YearBeyondHorizonRejected covers the add
// UX seam: a year at or past ProjectionYears passes the handler's own
// non-negative check, so it must be caught by a handler-level "beyond the
// current horizon" rejection BEFORE the entry is persisted. This check is
// deliberately handler-only (not in the shared ValidateOneTimeExpenses),
// since attempt 3's spec change made an out-of-horizon entry DORMANT, not
// invalid, everywhere else in the system — a legitimate outcome of shrinking
// ProjectionYears underneath an existing entry.
//
// This regression-tests the attempt-1 defect where AddOneTimeExpense saved
// the out-of-horizon entry to disk before prepare.From ever ran, bricking
// every later GET /whatif (prepare.From runs unconditionally ahead of
// render, with no UI path to delete the bad row). Asserting only the POST's
// status code — as attempt 1's test did — cannot detect that: the response
// was already a 500, just an untimely one, issued after the write. So this
// test also reloads settings from disk and issues a follow-up GET /whatif.
func TestOneTimeExpense_HandlerAdd_YearBeyondHorizonRejected(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	beforeCount := len(settings.OneTimeExpenses)

	form := url.Values{
		"description": {"Roof"},
		"amount":      {"1000"},
		"year":        {strconv.Itoa(settings.ProjectionYears)}, // == ProjectionYears, out of range
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/onetime", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddOneTime(w, req)
	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("expected a 4xx error status for a year beyond the projection horizon, got %d. body: %s", w.Code, w.Body.String())
	}

	// The rejected entry must not have been persisted.
	reloaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load after rejected add: %v", err)
	}
	if len(reloaded.OneTimeExpenses) != beforeCount {
		t.Fatalf("expected no new one-time expense to be persisted, got %d entries (was %d): %+v",
			len(reloaded.OneTimeExpenses), beforeCount, reloaded.OneTimeExpenses)
	}
	for _, e := range reloaded.OneTimeExpenses {
		if e.Description == "Roof" {
			t.Fatalf("rejected entry %q was persisted anyway: %+v", e.Description, e)
		}
	}

	// The page must still be healthy: prepare.From must not choke on a
	// persisted invalid entry, because there must not be one.
	getW := httptest.NewRecorder()
	getReq := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GET /whatif after rejected add = %d, want 200. body: %s", getW.Code, getW.Body.String())
	}
}

// TestOneTimeExpense_HorizonShrinkKeepsPageAlive is the attempt-2 brick
// repro, inverted per the attempt-3 spec change: an entry that was perfectly
// valid when added (year 29, horizon 30) must not brick anything when a
// LATER, unrelated write shrinks ProjectionYears out from under it (here via
// the settings-manager path, rm.UpdateSettings — the same path the settings
// page and MCP apply_changes use). The entry goes dormant, not fatal:
// GET /whatif must still return 200, and the rendered card must carry the
// "beyond current horizon" note so the user can see why the expense stopped
// counting and can edit or delete it.
func TestOneTimeExpense_HorizonShrinkKeepsPageAlive(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.ProjectionYears != 30 {
		t.Fatalf("test assumes default ProjectionYears == 30, got %d", settings.ProjectionYears)
	}

	// Add a one-time expense at year 29 — valid under the 30-year horizon.
	expense := models.OneTimeExpense{ID: "ote-shrink", Description: "Roof", Year: 29, Amount: 5000}
	if _, err := rm.AddOneTimeExpense(expense); err != nil {
		t.Fatalf("AddOneTimeExpense: %v", err)
	}

	// Shrink the horizon to 20 via the settings-manager write path — the same
	// path the /whatif settings form and MCP apply_changes use. The entry at
	// year 29 is now beyond the new horizon.
	if _, _, err := rm.UpdateSettings(map[string]interface{}{"projection_years": 20}); err != nil {
		t.Fatalf("UpdateSettings(projection_years=20): %v", err)
	}

	getW := httptest.NewRecorder()
	getReq := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GET /whatif after horizon shrink = %d, want 200. body: %s", getW.Code, getW.Body.String())
	}
	body := getW.Body.String()
	if !strings.Contains(body, "Roof") {
		t.Errorf("expected the dormant entry still rendered; got: %s", truncate(body, 2000))
	}
	if !strings.Contains(body, "beyond current horizon") {
		t.Errorf("expected a visible beyond-current-horizon note on the dormant entry; got: %s", truncate(body, 2000))
	}
}

func TestOneTimeExpense_HandlerDelete(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	expense := models.OneTimeExpense{ID: "ote-1", Description: "Test", Amount: 1000, Year: 1}
	if _, err := rm.AddOneTimeExpense(expense); err != nil {
		t.Fatalf("AddOneTimeExpense: %v", err)
	}

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/onetime/ote-1", nil, map[string]string{"id": "ote-1"})
	handleWhatIfDeleteOneTime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, e := range settings.OneTimeExpenses {
		if e.ID == "ote-1" {
			t.Fatalf("expected ote-1 to be removed, still present: %+v", e)
		}
	}
}

// ── Save flow: add persists to disk via SettingsManager ─────────────────

func TestOneTimeExpense_HandlerAdd_PersistsToSettings(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"description": {"Wedding"},
		"amount":      {"15000"},
		"year":        {"2"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/onetime", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddOneTime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	found := false
	for _, e := range settings.OneTimeExpenses {
		if e.Description == "Wedding" && e.Year == 2 && e.Amount == 15000 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected persisted Wedding one-time expense, got %+v", settings.OneTimeExpenses)
	}
}

// ── Render tests ──────────────────────────────────────────────────────────

func TestOneTimeExpenseCard_Renders(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-01"
	settings.OneTimeExpenses = []models.OneTimeExpense{
		{ID: "ote-1", Description: "New Roof", Year: 3, Amount: 50000},
	}

	out, err := renderer.RenderToString("whatif-onetime-card", map[string]any{
		"Settings": settings,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "One-Time Expenses") {
		t.Errorf("expected heading; got: %s", truncate(out, 500))
	}
	if !strings.Contains(out, "New Roof") {
		t.Errorf("expected item description rendered; got: %s", truncate(out, 800))
	}
	if !strings.Contains(out, "Year 3") {
		t.Errorf("expected year rendered; got: %s", truncate(out, 800))
	}
	if !strings.Contains(out, "50,000") {
		t.Errorf("expected formatted amount rendered; got: %s", truncate(out, 800))
	}
	// Accessibility: the remove control must be a labeled button, not an
	// icon-only control with no accessible name.
	if !strings.Contains(out, `aria-label="Remove one-time expense New Roof"`) {
		t.Errorf("expected labeled remove button; got: %s", truncate(out, 1000))
	}
	// Every text input in the add-row form must have an associated label.
	if !strings.Contains(out, `for="onetime-description"`) || !strings.Contains(out, `id="onetime-description"`) {
		t.Errorf("expected labeled description input; got: %s", truncate(out, 1500))
	}
	if !strings.Contains(out, `for="onetime-amount"`) || !strings.Contains(out, `id="onetime-amount"`) {
		t.Errorf("expected labeled amount input; got: %s", truncate(out, 1500))
	}
	if !strings.Contains(out, `for="onetime-year"`) || !strings.Contains(out, `id="onetime-year"`) {
		t.Errorf("expected labeled year input; got: %s", truncate(out, 1500))
	}
	// The add button must carry visible text, not just an icon.
	if !strings.Contains(out, "Add one-time expense") {
		t.Errorf("expected add button text; got: %s", truncate(out, 1500))
	}
}

func TestOneTimeExpenseCard_EmptyStateRenders(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.OneTimeExpenses = nil

	out, err := renderer.RenderToString("whatif-onetime-card", map[string]any{
		"Settings": settings,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "No one-time expenses added") {
		t.Errorf("expected empty-state copy; got: %s", truncate(out, 500))
	}
}
