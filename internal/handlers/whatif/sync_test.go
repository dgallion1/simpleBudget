package whatif

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
)

// syncGuardFieldsRE extracts the expected_scenario/plan_hash hidden inputs
// from a rendered (renderer != nil) whatif-sync-preview partial.
var syncGuardFieldsRE = regexp.MustCompile(`name="(expected_scenario|plan_hash)" value="([^"]*)"`)

// extractSyncGuardFields pulls expected_scenario and plan_hash out of a real
// preview response body — HTML hidden fields (renderer != nil) or the JSON
// fallback (renderer == nil) — so a test can apply with exactly what the
// preview reported, not a value reconstructed independently of the handler.
func extractSyncGuardFields(t *testing.T, body string) (expectedScenario, planHash string) {
	t.Helper()

	for _, m := range syncGuardFieldsRE.FindAllStringSubmatch(body, -1) {
		switch m[1] {
		case "expected_scenario":
			expectedScenario = m[2]
		case "plan_hash":
			planHash = m[2]
		}
	}
	if expectedScenario != "" && planHash != "" {
		return expectedScenario, planHash
	}

	var parsed struct {
		ExpectedScenario string `json:"expected_scenario"`
		PlanHash         string `json:"plan_hash"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err == nil &&
		parsed.ExpectedScenario != "" && parsed.PlanHash != "" {
		return parsed.ExpectedScenario, parsed.PlanHash
	}

	t.Fatalf("could not find expected_scenario/plan_hash in preview body: %s", truncate(body, 800))
	return "", ""
}

// findIncomeSource returns the income source with the given ID, or nil.
func findIncomeSource(s *models.WhatIfSettings, id string) *models.IncomeSource {
	for i := range s.IncomeSources {
		if s.IncomeSources[i].ID == id {
			return &s.IncomeSources[i]
		}
	}
	return nil
}

// monthlyIncomeRows builds CSV rows for a monthly deposit that ran from
// firstMonthsAgo back through lastMonthsAgo (inclusive, both relative to now).
func monthlyIncomeRows(desc string, amount float64, firstMonthsAgo, lastMonthsAgo int) []string {
	now := time.Now()
	var rows []string
	for i := lastMonthsAgo; i <= firstMonthsAgo; i++ {
		d := now.AddDate(0, -i, 0)
		rows = append(rows, fmt.Sprintf("%s,%s,%.2f,Income,Employment", d.Format("2006-01-02"), desc, amount))
	}
	return rows
}

// A monthly deposit whose last occurrence is months in the past is ended
// income: it still sits inside the trailing-12-month window, but the sync
// must not project it forward for the whole plan.
func TestSyncSettingsFromDashboard_SkipsStaleEndedIncome(t *testing.T) {
	// Deposits 10 months ago through 5 months ago — regular monthly, ended.
	setupSyncEnvWithCSV(t, monthlyIncomeRows("Rebuild Payroll", 10339, 10, 5))

	s := models.DefaultWhatIfSettings()
	if err := syncSettingsFromDashboard(s); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if src := findIncomeSource(s, "insights-rebuild-payroll"); src != nil {
		t.Fatalf("ended payroll must not be projected forward, but sync injected %+v", *src)
	}
}

// A monthly deposit that is still arriving must keep being synced — the
// staleness cutoff must not swallow ongoing income.
func TestSyncSettingsFromDashboard_KeepsOngoingMonthlyIncome(t *testing.T) {
	setupSyncEnvWithCSV(t, monthlyIncomeRows("Pension", 2000, 5, 0))

	s := models.DefaultWhatIfSettings()
	if err := syncSettingsFromDashboard(s); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	src := findIncomeSource(s, "insights-pension")
	if src == nil {
		t.Fatalf("expected ongoing pension to sync, got %+v", s.IncomeSources)
	}
	if math.Abs(src.Amount-2000) > 0.01 {
		t.Errorf("pension amount = %.2f, want 2000", src.Amount)
	}
}

// When the plan already models Social Security via SocialSecurityConfig
// (gross benefit), syncing the NET SS bank deposits as an income source
// double-counts SS. The sync must skip SS-looking patterns in that case.
func TestSyncSettingsFromDashboard_SkipsSocialSecurityWhenModeled(t *testing.T) {
	setupSyncEnvWithCSV(t, monthlyIncomeRows("Social Security Admin", 3200, 5, 0))

	s := models.DefaultWhatIfSettings()
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 3800, FRA: 67}
	if err := syncSettingsFromDashboard(s); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	for _, src := range s.IncomeSources {
		if strings.Contains(src.ID, "social-security") {
			t.Fatalf("SS deposits must not be injected while SocialSecurity config is active, got %+v", src)
		}
	}
}

// Without an active SocialSecurityConfig there is nothing to double-count,
// so SS deposits must still sync like any other regular income.
func TestSyncSettingsFromDashboard_InjectsSocialSecurityWhenNotModeled(t *testing.T) {
	setupSyncEnvWithCSV(t, monthlyIncomeRows("Social Security Admin", 3200, 5, 0))

	s := models.DefaultWhatIfSettings()
	s.SocialSecurity = nil
	if err := syncSettingsFromDashboard(s); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if findIncomeSource(s, "insights-social-security-admin") == nil {
		t.Fatalf("expected SS deposits to sync when no SocialSecurity config is active, got %+v", s.IncomeSources)
	}
}

// A SocialSecurityConfig with no benefit configured is not "active" — it
// must not suppress SS deposit syncing.
func TestSyncSettingsFromDashboard_EmptySSConfigDoesNotSuppress(t *testing.T) {
	setupSyncEnvWithCSV(t, monthlyIncomeRows("Social Security Admin", 3200, 5, 0))

	s := models.DefaultWhatIfSettings()
	s.SocialSecurity = &models.SocialSecurityConfig{FRA: 67}
	if err := syncSettingsFromDashboard(s); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if findIncomeSource(s, "insights-social-security-admin") == nil {
		t.Fatalf("expected SS deposits to sync when SS config has no benefit, got %+v", s.IncomeSources)
	}
}

// setupSyncEnvWithCategorizedCSV wires a fresh env from 4-column rows
// (Date,Description,Amount,Category). The 5-column helper's "Type" header
// is a Category alias in the loader, which clobbers the Category column —
// this variant keeps categories intact for category-sensitive tests.
func setupSyncEnvWithCategorizedCSV(t *testing.T, rows []string) {
	t.Helper()

	csvDir := t.TempDir()
	lines := append([]string{"Date,Description,Amount,Category"}, rows...)
	if err := os.WriteFile(filepath.Join(csvDir, "test.csv"), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	wireWhatIfEnv(t, t.TempDir(), csvDir)
}

// Health Insurance outflows are modeled by the plan's healthcare persons;
// folding them into MonthlyLivingExpenses double-counts them. The sync must
// exclude that category, mirroring the spend summary's living/healthcare
// split.
func TestSyncSettingsFromDashboard_ExcludesHealthInsuranceFromExpenses(t *testing.T) {
	now := time.Now()
	var rows []string
	for i := 0; i <= 10; i++ {
		d := now.AddDate(0, -i, 0).Format("2006-01-02")
		rows = append(rows,
			fmt.Sprintf("%s,Rent,-2000,Housing", d),
			fmt.Sprintf("%s,Kaiser Premium,-800,Health Insurance", d),
		)
	}
	setupSyncEnvWithCategorizedCSV(t, rows)

	s := models.DefaultWhatIfSettings()
	if err := syncSettingsFromDashboard(s); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// Oracle mirrors the handler's month-count formula over the same dates.
	// CSV dates load at midnight, so the span (and the average) can drift by
	// up to a day vs this oracle; the tolerance stays far below the ~$870/mo
	// error that including the $800 Health Insurance rows would produce.
	yearAgo := now.AddDate(-1, 0, 0)
	minDate := now.AddDate(0, -10, 0)
	months := 12.0
	if minDate.After(yearAgo) {
		months = now.Sub(minDate).Hours() / 24 / 30
		if months < 1 {
			months = 1
		}
	}
	want := 2000.0 * 11 / months

	if math.Abs(s.MonthlyLivingExpenses-want) > 10.0 {
		t.Errorf("MonthlyLivingExpenses = %.2f, want %.2f (Health Insurance rows must be excluded)", s.MonthlyLivingExpenses, want)
	}
}

// The Health Insurance exclusion must match case-insensitively, mirroring
// TransactionSet.FilterByCategory (which the dashboard's healthcare split
// uses). A transaction categorized in any case variant of "Health
// Insurance" is healthcare on the dashboard, so it must be excluded from
// synced living expenses too — otherwise the exclusion above only works
// for the exact-cased category and the double count creeps back in.
func TestSyncSettingsFromDashboard_ExcludesHealthInsuranceCaseInsensitive(t *testing.T) {
	now := time.Now()
	var rows []string
	for i := 0; i <= 10; i++ {
		d := now.AddDate(0, -i, 0).Format("2006-01-02")
		rows = append(rows,
			fmt.Sprintf("%s,Rent,-2000,Housing", d),
			fmt.Sprintf("%s,Kaiser Premium,-800,HEALTH INSURANCE", d),
			fmt.Sprintf("%s,Dental Premium,-100,health insurance", d),
		)
	}
	setupSyncEnvWithCategorizedCSV(t, rows)

	s := models.DefaultWhatIfSettings()
	if err := syncSettingsFromDashboard(s); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// Oracle mirrors the handler's month-count formula over the same dates.
	// CSV dates load at midnight, so the span (and the average) can drift by
	// up to a day vs this oracle; the tolerance stays far below the ~$870/mo
	// error that including the Health Insurance rows (either case) would
	// produce.
	yearAgo := now.AddDate(-1, 0, 0)
	minDate := now.AddDate(0, -10, 0)
	months := 12.0
	if minDate.After(yearAgo) {
		months = now.Sub(minDate).Hours() / 24 / 30
		if months < 1 {
			months = 1
		}
	}
	want := 2000.0 * 11 / months

	if math.Abs(s.MonthlyLivingExpenses-want) > 10.0 {
		t.Errorf("MonthlyLivingExpenses = %.2f, want %.2f (HEALTH INSURANCE and health insurance rows must both be excluded)", s.MonthlyLivingExpenses, want)
	}
}

// POST /whatif/sync must PREVIEW the proposed changes without saving —
// the user confirms via /whatif/sync/apply. A silent save clobbers
// deliberately set MonthlyLivingExpenses and income sources.
func TestHandleWhatIfSync_PreviewsWithoutSaving(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	before, err := rm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("Load before: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/sync", nil)
	handleWhatIfSync(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, truncate(w.Body.String(), 300))
	}
	body := w.Body.String()
	if !strings.Contains(body, "/whatif/sync/apply") {
		t.Errorf("preview must offer an apply button posting to /whatif/sync/apply; got: %s", truncate(body, 800))
	}

	after, err := rm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("Load after: %v", err)
	}
	if after.MonthlyLivingExpenses != before.MonthlyLivingExpenses {
		t.Errorf("preview saved MonthlyLivingExpenses: %.2f -> %.2f", before.MonthlyLivingExpenses, after.MonthlyLivingExpenses)
	}
	for _, src := range after.IncomeSources {
		if strings.HasPrefix(src.ID, "insights-") {
			t.Errorf("preview saved income source %q", src.ID)
		}
	}
}

// syncApplyRequest builds a POST /whatif/sync/apply request carrying the
// given expected_scenario/plan_hash form values.
func syncApplyRequest(scenario, hash string) *http.Request {
	form := url.Values{"expected_scenario": {scenario}, "plan_hash": {hash}}
	req := httptest.NewRequest("POST", "/whatif/sync/apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// POST /whatif/sync/apply performs the actual sync: saves the recomputed
// settings and renders the standard results partial. The confirmation must
// carry the expected_scenario and plan_hash a real preview reported — this
// is the plan the user actually saw, not one reconstructed independently of
// the handler.
func TestHandleWhatIfSyncApply_SavesSyncedSettings(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	previewW := httptest.NewRecorder()
	handleWhatIfSync(previewW, httptest.NewRequest("POST", "/whatif/sync", nil))
	if previewW.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200. body: %s", previewW.Code, truncate(previewW.Body.String(), 300))
	}
	scenario, hash := extractSyncGuardFields(t, previewW.Body.String())

	w := httptest.NewRecorder()
	handleWhatIfSyncApply(w, syncApplyRequest(scenario, hash))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, truncate(w.Body.String(), 300))
	}

	saved, err := rm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("Load after apply: %v", err)
	}
	// setupTestEnvWithRenderer seeds two recent monthly Salary deposits.
	if findIncomeSource(saved, "insights-salary") == nil {
		t.Errorf("apply must save synced income sources, got %+v", saved.IncomeSources)
	}
}

// A missing or blank expected_scenario / plan_hash must be rejected with 400
// before any load or write — a client that skipped preview (or sent garbage)
// gets a clear error, not an unreviewed write.
func TestHandleWhatIfSyncApply_MissingGuardFieldsRejected(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	before, err := rm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("Load before: %v", err)
	}

	cases := []struct {
		name     string
		scenario string
		hash     string
	}{
		{"both missing", "", ""},
		{"blank expected_scenario", "   ", "deadbeef"},
		{"blank plan_hash", "whatif.json", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handleWhatIfSyncApply(w, syncApplyRequest(tc.scenario, tc.hash))

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400. body: %s", w.Code, truncate(w.Body.String(), 300))
			}
			assertRetargetHeader(t, w, "#whatif-sync-preview")
		})
	}

	after, err := rm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("Load after: %v", err)
	}
	if after.MonthlyLivingExpenses != before.MonthlyLivingExpenses {
		t.Errorf("400 must not write: MonthlyLivingExpenses %.2f -> %.2f", before.MonthlyLivingExpenses, after.MonthlyLivingExpenses)
	}
	for _, src := range after.IncomeSources {
		if strings.HasPrefix(src.ID, "insights-") {
			t.Errorf("400 must not write income source %q, got %+v", src.ID, after.IncomeSources)
		}
	}
}

// A wrong expected_scenario — the scenario switched between preview and
// apply — must be rejected with 409 and nothing written, even though the
// plan_hash is the real one from a genuine preview.
func TestHandleWhatIfSyncApply_WrongExpectedScenarioRejected(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	before, err := rm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("Load before: %v", err)
	}

	previewW := httptest.NewRecorder()
	handleWhatIfSync(previewW, httptest.NewRequest("POST", "/whatif/sync", nil))
	if previewW.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", previewW.Code)
	}
	_, hash := extractSyncGuardFields(t, previewW.Body.String())

	w := httptest.NewRecorder()
	handleWhatIfSyncApply(w, syncApplyRequest("some-other-scenario.json", hash))

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409. body: %s", w.Code, truncate(w.Body.String(), 300))
	}
	assertRetargetHeader(t, w, "#whatif-sync-preview")

	after, err := rm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("Load after: %v", err)
	}
	if after.MonthlyLivingExpenses != before.MonthlyLivingExpenses {
		t.Errorf("409 must not write: MonthlyLivingExpenses %.2f -> %.2f", before.MonthlyLivingExpenses, after.MonthlyLivingExpenses)
	}
	for _, src := range after.IncomeSources {
		if strings.HasPrefix(src.ID, "insights-") {
			t.Errorf("409 must not write income source %q, got %+v", src.ID, after.IncomeSources)
		}
	}
}

// This is the exploit's signature (attempt 1's TOCTOU, checker-second's
// Z2.1 FAIL): a scenario switch that lands AFTER handleWhatIfSyncApply's own
// (necessarily unlocked) expected_scenario/plan_hash checks pass, but BEFORE
// the save, must still be rejected with 409 -- and critically, the write
// must not land on the newly-active OTHER scenario's file. A sequential
// "switch, then call apply" test cannot exercise this: the handler's
// up-front fast-fail check alone catches a switch that happened before the
// call starts (see TestHandleWhatIfSyncApply_WrongExpectedScenarioRejected).
// syncApplyRaceTestHook (sync.go) lands the switch deterministically inside
// the window between that fast-fail check and the locked save, exactly
// reproducing the interleaving a second tab/MCP call performs concurrently.
func TestHandleWhatIfSyncApply_ScenarioSwitchDuringApplyWindowRejected(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	// Preview while whatif.json (the default scenario) is active.
	originalBefore, err := rm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("Load before: %v", err)
	}
	// whatif.json must exist on disk before SwitchScenario can name it later
	// in this test; Load alone does not persist unchanged defaults.
	if err := rm.Save(originalBefore); err != nil {
		t.Fatalf("Save whatif.json: %v", err)
	}
	previewW := httptest.NewRecorder()
	handleWhatIfSync(previewW, httptest.NewRequest("POST", "/whatif/sync", nil))
	if previewW.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200. body: %s", previewW.Code, truncate(previewW.Body.String(), 300))
	}
	scenario, hash := extractSyncGuardFields(t, previewW.Body.String())
	if scenario != "whatif.json" {
		t.Fatalf("expected the preview's scenario to be whatif.json, got %q", scenario)
	}

	// Create a second scenario. CreateScenario both writes the new file AND
	// switches the active scenario to it -- modeling a second tab/MCP call
	// that creates/switches to another scenario in the window between this
	// request's preview and its apply.
	if _, err := rm.CreateScenario("Other Scenario"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	otherFile := rm.ActiveFilename()
	if otherFile == "whatif.json" {
		t.Fatalf("CreateScenario did not switch the active scenario")
	}
	otherBefore, err := rm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("Load other before: %v", err)
	}

	// Switch back to whatif.json so the handler's own up-front fast-fail
	// check (ActiveFilename() == expectedScenario) sees a MATCH and proceeds
	// past it -- the hook below then switches to the other scenario again,
	// landing squarely in the window between that check and the save.
	if err := rm.SwitchScenario("whatif.json"); err != nil {
		t.Fatalf("SwitchScenario back: %v", err)
	}
	defer func() { syncApplyRaceTestHook = nil }()
	syncApplyRaceTestHook = func() {
		if err := rm.SwitchScenario(otherFile); err != nil {
			t.Fatalf("SwitchScenario (race hook): %v", err)
		}
	}

	w := httptest.NewRecorder()
	handleWhatIfSyncApply(w, syncApplyRequest(scenario, hash))

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409. body: %s", w.Code, truncate(w.Body.String(), 300))
	}
	assertRetargetHeader(t, w, "#whatif-sync-preview")

	// CRITICAL: the OTHER scenario's file must be byte-unchanged. This is the
	// exploit's actual signature -- attempt 1 returned 200 here and silently
	// overwrote the OTHER scenario's MonthlyLivingExpenses/income sources,
	// not the previewed one.
	otherAfter, err := rm.LoadScenarioSettings(otherFile)
	if err != nil {
		t.Fatalf("Load other after: %v", err)
	}
	if otherAfter.MonthlyLivingExpenses != otherBefore.MonthlyLivingExpenses {
		t.Errorf("the OTHER scenario %q was written to: MonthlyLivingExpenses %.2f -> %.2f",
			otherFile, otherBefore.MonthlyLivingExpenses, otherAfter.MonthlyLivingExpenses)
	}
	for _, src := range otherAfter.IncomeSources {
		if strings.HasPrefix(src.ID, "insights-") {
			t.Errorf("the OTHER scenario %q gained a synced income source %q, got %+v", otherFile, src.ID, otherAfter.IncomeSources)
		}
	}

	// The originally-previewed scenario (whatif.json) must also be untouched:
	// the rejected apply must write nothing anywhere.
	originalAfter, err := rm.LoadScenarioSettings("whatif.json")
	if err != nil {
		t.Fatalf("Load original after: %v", err)
	}
	if originalAfter.MonthlyLivingExpenses != originalBefore.MonthlyLivingExpenses {
		t.Errorf("the previewed scenario whatif.json was written to: MonthlyLivingExpenses %.2f -> %.2f",
			originalBefore.MonthlyLivingExpenses, originalAfter.MonthlyLivingExpenses)
	}
}

// A correct expected_scenario but a plan_hash that went stale — the
// transactions changed between preview and apply — must be rejected with
// 409 and nothing written.
func TestHandleWhatIfSyncApply_StalePlanHashRejected(t *testing.T) {
	settingsDir := t.TempDir()
	csvDir := t.TempDir()

	csvPath := filepath.Join(csvDir, "test.csv")
	original := "Date,Description,Amount,Type,Category\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",Salary,5000,Income,Employment\n" +
		time.Now().AddDate(0, -2, 0).Format("2006-01-02") + ",Salary,5000,Income,Employment\n"
	if err := os.WriteFile(csvPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	rm := wireWhatIfEnv(t, settingsDir, csvDir)

	before, err := rm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("Load before: %v", err)
	}

	previewW := httptest.NewRecorder()
	handleWhatIfSync(previewW, httptest.NewRequest("POST", "/whatif/sync", nil))
	if previewW.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", previewW.Code)
	}
	scenario, hash := extractSyncGuardFields(t, previewW.Body.String())

	// Mutate the transaction data the plan was computed from — same
	// scenario, but the recomputed plan (and its hash) now differs. An
	// outflow row changes NewMonthlyExpenses directly (a single new income
	// description would not: IncomePatterns requires 2+ occurrences of a
	// description before it forms a pattern at all).
	mutated := original + time.Now().Format("2006-01-02") + ",Emergency Repair,-1200,Outflow,Housing\n"
	if err := os.WriteFile(csvPath, []byte(mutated), 0o644); err != nil {
		t.Fatalf("mutate csv: %v", err)
	}

	w := httptest.NewRecorder()
	handleWhatIfSyncApply(w, syncApplyRequest(scenario, hash))

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409. body: %s", w.Code, truncate(w.Body.String(), 300))
	}
	assertRetargetHeader(t, w, "#whatif-sync-preview")

	after, err := rm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("Load after: %v", err)
	}
	if after.MonthlyLivingExpenses != before.MonthlyLivingExpenses {
		t.Errorf("409 must not write: MonthlyLivingExpenses %.2f -> %.2f", before.MonthlyLivingExpenses, after.MonthlyLivingExpenses)
	}
	for _, src := range after.IncomeSources {
		if strings.HasPrefix(src.ID, "insights-") {
			t.Errorf("409 must not write income source %q, got %+v", src.ID, after.IncomeSources)
		}
	}
}
