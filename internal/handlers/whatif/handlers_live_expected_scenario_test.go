package whatif

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// Issue #24 / design decision D-2026-08-29a: expected_scenario stops being an
// optional client convention and becomes a required field, enforced before
// any load or write. TestHandleWhatIfApply_MatchingExpectedScenarioIsAccepted
// and TestHandleWhatIfApply_MismatchedExpectedScenarioIs409AndWritesNothing
// (handlers_live_test.go) already cover the correct-value and wrong-value
// paths; this file covers the missing-field rejection those did not.

// TestHandleWhatIfApply_MissingExpectedScenarioIs400AndWritesNothing is the
// new guard itself: a request that omits expected_scenario must be rejected
// before ApplyOverrides runs at all, not merely fall through to whatever the
// override validation happens to catch.
func TestHandleWhatIfApply_MissingExpectedScenarioIs400AndWritesNothing(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	before, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantExpenses := before.MonthlyLivingExpenses

	resp := postApply(t, `{"monthly_living_expenses": 9999}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "expected_scenario") {
		t.Fatalf("error %q does not name expected_scenario", body)
	}

	rm.InvalidateCache()
	after, err := rm.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.MonthlyLivingExpenses != wantExpenses {
		t.Fatalf("a rejected apply still wrote: monthly_living_expenses %v -> %v", wantExpenses, after.MonthlyLivingExpenses)
	}
}

// TestHandleWhatIfApply_BlankExpectedScenarioIs400AndWritesNothing covers the
// whitespace-only case: an empty string satisfies Go's JSON decode of a
// string field, so the guard must trim rather than compare against "".
func TestHandleWhatIfApply_BlankExpectedScenarioIs400AndWritesNothing(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	before, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantExpenses := before.MonthlyLivingExpenses

	resp := postApply(t, `{"monthly_living_expenses": 9999, "expected_scenario": "   "}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "expected_scenario") {
		t.Fatalf("error %q does not name expected_scenario", body)
	}

	rm.InvalidateCache()
	after, err := rm.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.MonthlyLivingExpenses != wantExpenses {
		t.Fatalf("a rejected apply still wrote: monthly_living_expenses %v -> %v", wantExpenses, after.MonthlyLivingExpenses)
	}
}
