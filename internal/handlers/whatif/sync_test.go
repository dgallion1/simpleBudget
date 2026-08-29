package whatif

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
)

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

// POST /whatif/sync/apply performs the actual sync: saves the recomputed
// settings and renders the standard results partial.
func TestHandleWhatIfSyncApply_SavesSyncedSettings(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/sync/apply", nil)
	handleWhatIfSyncApply(w, req)

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
