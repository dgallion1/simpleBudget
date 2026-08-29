package whatif

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"budget2/internal/models"
)

// TestHandleWhatIfSettings_LivingExpensesRoundTripsOffGridValue guards the
// snap-trap fix (W2 spec Part B): a plan whose saved monthly_living_expenses
// sits off the slider's step=100 grid (e.g. 7386, from "Sync from
// Dashboard") must round-trip exactly when an unrelated field is changed and
// the slider itself is never touched.
//
// The fix moves the submitted field onto a hidden input that only changes on
// a genuine drag (see portfolio-settings.html); this test exercises the
// server side of that contract by posting the exact value the untouched
// hidden input would carry (as opposed to 7400, the value the OLD
// name="monthly_living_expenses" range input would have submitted after the
// browser snapped its step-mismatched initial value) and asserting the
// figure survives the round trip to the dollar.
func TestHandleWhatIfSettings_LivingExpensesRoundTripsOffGridValue(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-01"
	settings.MonthlyLivingExpenses = 7386
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	form := url.Values{
		// What the fixed hidden input submits when the slider was never
		// dragged: the exact saved value, not a step-snapped one.
		"monthly_living_expenses": {"7386.00"},
		// The unrelated change that triggers this form post.
		"monthly_property_tax": {"250"},
	}
	req := httptest.NewRequest(http.MethodPost, "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handleWhatIfSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.MonthlyLivingExpenses != 7386 {
		t.Fatalf("MonthlyLivingExpenses = %v, want 7386 unchanged (snap trap: an unrelated form submit must not silently mutate an untouched off-grid value)", loaded.MonthlyLivingExpenses)
	}
	if loaded.MonthlyPropertyTax != 250 {
		t.Fatalf("MonthlyPropertyTax = %v, want 250 (the unrelated field should still apply)", loaded.MonthlyPropertyTax)
	}
}

// TestHandleWhatIfSettings_LivingExpensesSnappedValueWouldPersist documents
// the counter-case: if the client ever again posts the step-snapped value
// (the pre-fix bug), the server has no way to distinguish that from a
// genuine user edit to 7400 and persists it as given. The fix belongs in the
// form (never submit the snapped value unless the user actually dragged),
// not in the handler — this test exists so a future change to this handler
// doesn't assume otherwise.
func TestHandleWhatIfSettings_LivingExpensesSnappedValueWouldPersist(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-01"
	settings.MonthlyLivingExpenses = 7386
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	form := url.Values{"monthly_living_expenses": {"7400"}}
	req := httptest.NewRequest(http.MethodPost, "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handleWhatIfSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.MonthlyLivingExpenses != 7400 {
		t.Fatalf("MonthlyLivingExpenses = %v, want 7400 (server trusts whatever the form posts; the fix is client-side)", loaded.MonthlyLivingExpenses)
	}
}
