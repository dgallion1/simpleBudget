package whatif

import (
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
	"budget2/internal/services/dataloader"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/internal/testutil"
)

// setupSyncExclusionEnv wires a fresh env from 4-column
// (Date,Description,Amount,Category) rows so Category survives, with a
// renderer wired (unlike wireWhatIfEnv's nil renderer) so preview-rendering
// tests can inspect real HTML output. It also seeds major_expenses.json with
// an unflagged keyword def BEFORE a flagged amount-only def — the order that
// makes the first-def-wins trap observable.
func setupSyncExclusionEnv(t *testing.T, rows []string) {
	t.Helper()

	settingsDir := t.TempDir()
	csvDir := t.TempDir()

	lines := append([]string{"Date,Description,Amount,Category"}, rows...)
	if err := os.WriteFile(filepath.Join(csvDir, "test.csv"), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	store, err := storage.New(settingsDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	rm := retirement.NewSettingsManager(settingsDir, store)
	dl := dataloader.New(csvDir, store)

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	Initialize(dl, rend, rm)

	cache.mu.Lock()
	cache.hash = ""
	cache.analysis = nil
	cache.cachedAt = time.Time{}
	cache.mu.Unlock()

	defs := []models.MajorExpense{
		{ID: "gym", Name: "Gym", Keywords: []string{"GYM"}},
		{ID: "carloan", Name: "Car Loan", ExpectedMin: 500, ExpectedMax: 500, ExcludeFromPlanSync: true},
	}
	if err := loader.SaveMajorExpenses(defs); err != nil {
		t.Fatalf("SaveMajorExpenses: %v", err)
	}
}

// syncExclusionFixtureRows builds the shared fixture: 10 grocery outflows
// (stay in living), 4 car-loan outflows (flagged, excluded), 1 gym outflow
// whose amount coincides with the car-loan def but whose description hits
// the EARLIER unflagged "gym" keyword def first (must stay in living), one
// Health-Insurance-category outflow (excluded via the existing HI rule),
// and one outflow that is BOTH Health-Insurance-category AND flag-matched
// by amount (must be excluded once, as HI, and never inflate the car-loan
// group's total).
func syncExclusionFixtureRows() []string {
	now := time.Now()
	d := func(monthsAgo int) string { return now.AddDate(0, -monthsAgo, 0).Format("2006-01-02") }

	var rows []string
	for i := 1; i <= 10; i++ {
		rows = append(rows, fmt.Sprintf("%s,Walmart Grocery,-1000,Groceries", d(i)))
	}
	for _, i := range []int{5, 6, 7, 8} {
		rows = append(rows, fmt.Sprintf("%s,Car Loan Payment,-500,Auto Payment", d(i)))
	}
	rows = append(rows, fmt.Sprintf("%s,GYM MEMBERSHIP,-500,Health & Fitness", d(9)))
	rows = append(rows, fmt.Sprintf("%s,Health Ins Premium,-900,Health Insurance", d(3)))
	rows = append(rows, fmt.Sprintf("%s,Health Ins Special,-500,Health Insurance", d(4)))
	return rows
}

// expectedSyncExclusionMonths mirrors computeDashboardSync's month-count
// formula over the same dates the fixture uses (the furthest-back row is
// the 10-months-ago grocery entry).
func expectedSyncExclusionMonths() float64 {
	now := time.Now()
	yearAgo := now.AddDate(-1, 0, 0)
	minDate := now.AddDate(0, -10, 0)
	months := 12.0
	if minDate.After(yearAgo) {
		months = now.Sub(minDate).Hours() / 24 / 30
		if months < 1 {
			months = 1
		}
	}
	return months
}

// A flagged major-expense group (car loan) must be pulled out of
// NewMonthlyExpenses, an unflagged group sharing its amount (gym, matched
// via an earlier def) must NOT be, and a row that is both HI-category and
// flag-matched must be excluded once — as HI — never inflating the group's
// displayed total.
func TestComputeDashboardSync_ExcludesFlaggedMajorExpenseGroup(t *testing.T) {
	setupSyncExclusionEnv(t, syncExclusionFixtureRows())

	s := models.DefaultWhatIfSettings()
	plan, err := computeDashboardSync(s)
	if err != nil {
		t.Fatalf("computeDashboardSync: %v", err)
	}

	months := expectedSyncExclusionMonths()

	// Living expenses = 10 groceries ($1000 each) + 1 gym ($500), everything
	// else (car loan x4, both HI rows) excluded.
	wantLiving := (10000.0 + 500.0) / months
	if math.Abs(plan.NewMonthlyExpenses-wantLiving) > 15.0 {
		t.Errorf("NewMonthlyExpenses = %.2f, want ~%.2f", plan.NewMonthlyExpenses, wantLiving)
	}

	if len(plan.ExcludedGroups) != 1 {
		t.Fatalf("expected exactly 1 excluded group (car loan), got %+v", plan.ExcludedGroups)
	}
	g := plan.ExcludedGroups[0]
	if g.Name != "Car Loan" {
		t.Errorf("excluded group name = %q, want %q", g.Name, "Car Loan")
	}
	if g.Count != 4 {
		t.Errorf("excluded group count = %d, want 4 (the HI-overlap row must NOT be counted here)", g.Count)
	}
	wantGroupTotal := 2000.0
	if math.Abs(g.Total-wantGroupTotal) > 0.01 {
		t.Errorf("excluded group total = %.2f, want %.2f", g.Total, wantGroupTotal)
	}
	wantGroupMonthly := wantGroupTotal / months
	if math.Abs(g.MonthlyAmount-wantGroupMonthly) > 5.0 {
		t.Errorf("excluded group monthly = %.2f, want ~%.2f", g.MonthlyAmount, wantGroupMonthly)
	}
}

// A refund (a positive-amount row still typed Outflow, e.g. a partial
// reversal) inside a flagged group must NET against payments in the
// displayed Total/MonthlyAmount, never Abs — ruling SY-2026-08-30a. Four
// -500.00 payments plus one +500.00 reversal must net to Total=1500.00, not
// Abs-summed to 2500.00, and Count must still count all five rows.
func TestComputeDashboardSync_ExcludedGroupRefundNets(t *testing.T) {
	now := time.Now()
	d := func(monthsAgo int) string { return now.AddDate(0, -monthsAgo, 0).Format("2006-01-02") }

	rows := []string{
		fmt.Sprintf("%s,Car Payment,-500.00,Auto Payment", d(1)),
		fmt.Sprintf("%s,Car Payment,-500.00,Auto Payment", d(2)),
		fmt.Sprintf("%s,Car Payment,-500.00,Auto Payment", d(3)),
		fmt.Sprintf("%s,Car Payment,-500.00,Auto Payment", d(4)),
		// A reversal: positive amount, but must NOT read as classifier
		// "income" (no income keyword/category) so it stays typed Outflow
		// and is counted here rather than dropped from the outflow set.
		fmt.Sprintf("%s,Car Payment Reversal,500.00,Auto Payment", d(2)),
	}
	setupSyncEnvWithCategorizedCSV(t, rows)

	defs := []models.MajorExpense{
		{ID: "car", Name: "Car Loan", ExpectedMin: 500, ExpectedMax: 500, ExcludeFromPlanSync: true},
	}
	if err := loader.SaveMajorExpenses(defs); err != nil {
		t.Fatalf("SaveMajorExpenses: %v", err)
	}

	s := models.DefaultWhatIfSettings()
	plan, err := computeDashboardSync(s)
	if err != nil {
		t.Fatalf("computeDashboardSync: %v", err)
	}

	if len(plan.ExcludedGroups) != 1 {
		t.Fatalf("expected exactly 1 excluded group, got %+v", plan.ExcludedGroups)
	}
	g := plan.ExcludedGroups[0]

	if g.Count != 5 {
		t.Errorf("Count = %d, want 5 (all four payments plus the reversal)", g.Count)
	}

	const wantNetTotal = 1500.00 // 4*500 payments minus the 500 reversal
	if math.Abs(g.Total-wantNetTotal) > 0.005 {
		t.Errorf("Total = %.2f, want net %.2f (must NET the refund, never math.Abs it)", g.Total, wantNetTotal)
	}

	months := now.Sub(now.AddDate(0, -4, 0)).Hours() / 24 / 30
	if months < 1 {
		months = 1
	}
	wantMonthly := wantNetTotal / months
	if math.Abs(g.MonthlyAmount-wantMonthly) > 5.0 {
		t.Errorf("MonthlyAmount = %.2f, want ~%.2f (Total/months)", g.MonthlyAmount, wantMonthly)
	}
}

// Two computeDashboardSync calls against unchanged data must produce
// byte-identical ExcludedGroups order (map-free, sorted by Name) so
// syncPlanHash stays stable between preview and apply.
func TestComputeDashboardSync_ExcludedGroupsOrderIsDeterministic(t *testing.T) {
	setupSyncExclusionEnv(t, syncExclusionFixtureRows())

	s := models.DefaultWhatIfSettings()
	plan1, err := computeDashboardSync(s)
	if err != nil {
		t.Fatalf("computeDashboardSync (1): %v", err)
	}
	plan2, err := computeDashboardSync(s)
	if err != nil {
		t.Fatalf("computeDashboardSync (2): %v", err)
	}

	hash1, err := syncPlanHash(plan1)
	if err != nil {
		t.Fatalf("syncPlanHash (1): %v", err)
	}
	hash2, err := syncPlanHash(plan2)
	if err != nil {
		t.Fatalf("syncPlanHash (2): %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("syncPlanHash differs across two calls against unchanged data: %s vs %s", hash1, hash2)
	}
}

// TestComputeDashboardSync_ExcludedGroupsOrderSurvivesNameCollision is
// ruling SY-2026-08-30b's determinism test: Name is NOT unique (two flagged
// defs share "Subscription" here, with different IDs), so a sort keyed on
// Name alone leaves their relative order to Go's map-iteration randomizer.
// 100 in-process iterations give that randomizer many chances to actually
// flip the order (and therefore syncPlanHash) if the ID tiebreaker is
// missing — a single pair of calls, as attempt 1's test did, can pass by
// luck even with the tiebreaker deleted.
//
// Verified by hand for this attempt: temporarily deleting the
// `return ids[i] < ids[j]` tiebreaker (leaving only the Name comparison) in
// internal/handlers/whatif/sync.go made this test fail (differing
// ExcludedGroups order / syncPlanHash within the 100 iterations); restoring
// it makes the test pass again.
func TestComputeDashboardSync_ExcludedGroupsOrderSurvivesNameCollision(t *testing.T) {
	now := time.Now()
	d := func(monthsAgo int) string { return now.AddDate(0, -monthsAgo, 0).Format("2006-01-02") }

	rows := []string{
		fmt.Sprintf("%s,NETFLIX MONTHLY,-12.99,Streaming", d(1)),
		fmt.Sprintf("%s,NETFLIX MONTHLY,-12.99,Streaming", d(2)),
		fmt.Sprintf("%s,SPOTIFY PREMIUM,-9.99,Streaming", d(1)),
		fmt.Sprintf("%s,SPOTIFY PREMIUM,-9.99,Streaming", d(2)),
	}
	setupSyncEnvWithCategorizedCSV(t, rows)

	// Same Name, different IDs — "sub-netflix" sorts before "sub-spotify"
	// lexically, so a correct ID tiebreaker must always put Netflix first.
	defs := []models.MajorExpense{
		{ID: "sub-spotify", Name: "Subscription", Keywords: []string{"SPOTIFY"}, ExcludeFromPlanSync: true},
		{ID: "sub-netflix", Name: "Subscription", Keywords: []string{"NETFLIX"}, ExcludeFromPlanSync: true},
	}
	if err := loader.SaveMajorExpenses(defs); err != nil {
		t.Fatalf("SaveMajorExpenses: %v", err)
	}

	const iterations = 100
	var refGroups []syncExcludedGroup
	var refHash string

	for i := 0; i < iterations; i++ {
		s := models.DefaultWhatIfSettings()
		plan, err := computeDashboardSync(s)
		if err != nil {
			t.Fatalf("computeDashboardSync (iteration %d): %v", i, err)
		}
		if len(plan.ExcludedGroups) != 2 {
			t.Fatalf("iteration %d: expected 2 excluded groups (same Name, different IDs), got %+v", i, plan.ExcludedGroups)
		}
		hash, err := syncPlanHash(plan)
		if err != nil {
			t.Fatalf("syncPlanHash (iteration %d): %v", i, err)
		}

		if i == 0 {
			refGroups = plan.ExcludedGroups
			refHash = hash
			// Assert the expected tiebroken order once, concretely.
			if refGroups[0].Count != 2 || refGroups[1].Count != 2 {
				t.Fatalf("iteration 0: expected both groups to have Count 2, got %+v", refGroups)
			}
			continue
		}

		if len(plan.ExcludedGroups) != len(refGroups) {
			t.Fatalf("iteration %d: ExcludedGroups length changed: %d vs %d", i, len(plan.ExcludedGroups), len(refGroups))
		}
		for idx := range plan.ExcludedGroups {
			if plan.ExcludedGroups[idx] != refGroups[idx] {
				t.Fatalf("iteration %d: ExcludedGroups order changed at index %d: %+v vs %+v (reference)",
					i, idx, plan.ExcludedGroups, refGroups)
			}
		}
		if hash != refHash {
			t.Fatalf("iteration %d: syncPlanHash changed: %s vs %s (reference) -- a same-Name collision without the ID tiebreaker would flip this and cause a spurious 409 between preview and apply", i, hash, refHash)
		}
	}
}

// No flagged major expenses (or none matched) must leave ExcludedGroups nil,
// and the preview must not render the "modeled separately" section at all.
func TestComputeDashboardSync_NoFlaggedMajorExpensesLeavesExcludedGroupsEmpty(t *testing.T) {
	setupSyncEnvWithCategorizedCSV(t, []string{
		fmt.Sprintf("%s,Rent,-2000,Housing", time.Now().AddDate(0, -1, 0).Format("2006-01-02")),
	})

	s := models.DefaultWhatIfSettings()
	plan, err := computeDashboardSync(s)
	if err != nil {
		t.Fatalf("computeDashboardSync: %v", err)
	}
	if len(plan.ExcludedGroups) != 0 {
		t.Errorf("expected no excluded groups, got %+v", plan.ExcludedGroups)
	}
}

// The rendered preview must show the excluded-groups section, with the
// group's name and transaction count, only when the plan actually has one,
// and the hidden guard fields must still round-trip untouched.
func TestHandleWhatIfSync_RendersExcludedGroupsSection(t *testing.T) {
	setupSyncExclusionEnv(t, syncExclusionFixtureRows())

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/sync", nil)
	handleWhatIfSync(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, truncate(w.Body.String(), 500))
	}
	body := w.Body.String()
	if !strings.Contains(body, "Excluded from living expenses") {
		t.Errorf("preview missing excluded-groups section heading; body: %s", truncate(body, 1500))
	}
	if !strings.Contains(body, "Car Loan") {
		t.Errorf("preview missing excluded group name; body: %s", truncate(body, 1500))
	}
	if !strings.Contains(body, "4 transactions") {
		t.Errorf("preview missing excluded group count; body: %s", truncate(body, 1500))
	}

	scenario, hash, revision := extractSyncGuardFields(t, body)
	if scenario == "" || hash == "" || revision == "" {
		t.Errorf("guard fields missing: scenario=%q hash=%q revision=%q", scenario, hash, revision)
	}
}
