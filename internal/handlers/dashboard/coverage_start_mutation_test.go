package dashboard

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"budget2/internal/services/dataloader"
	"budget2/internal/services/metrics"
	"budget2/internal/services/retirement"
	"budget2/internal/templates"
	"budget2/internal/testutil"

	"github.com/go-chi/chi/v5"
)

// This file carries the attempt-3 criterion-3b mutation-killing regressions
// for the THREE internal/handlers/dashboard/handlers.go call sites
// (handleDashboard, handleKPIsPartial, handleChartData's budget-vs-actual
// path). Each test function hits all three HTTP endpoints so that mutating
// the set passed to metrics.HealthcareCoverageStart at ANY ONE of the three
// sites -- to the window-filtered set or the duplicates-included (non-
// Active) set -- fails at least one assertion here, without needing three
// near-duplicate test functions. See HC-RUN-SPEC.md's Attempt-3 contract.

// setupCoverageMutationEnv writes rows to a temp CSV directory, wires a real
// template renderer plus a retirement settings manager with the given
// living/healthcare monthly targets, and returns the router, the DataLoader
// (so callers can layer duplicate-decision fixtures on top before issuing
// requests), and a cleanup func.
func setupCoverageMutationEnv(t *testing.T, rows [][]string, monthlyLiving, monthlyHealthcare float64) (chi.Router, *dataloader.DataLoader, func()) {
	t.Helper()

	tmpDir, dl, store, cleanup := writeTempCSV(t, rows)

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		cleanup()
		t.Fatalf("templates.New: %v", err)
	}

	rm := retirement.NewSettingsManager(tmpDir, store)
	settingsPath := filepath.Join(tmpDir, "whatif.json")
	settingsJSON := `{"monthly_living_expenses": ` + strconv.FormatFloat(monthlyLiving, 'f', -1, 64) +
		`, "monthly_healthcare": ` + strconv.FormatFloat(monthlyHealthcare, 'f', -1, 64) +
		`, "healthcare_start_years": 0}`
	if err := os.WriteFile(settingsPath, []byte(settingsJSON), 0o600); err != nil {
		cleanup()
		t.Fatalf("write settings: %v", err)
	}

	Initialize(dl, rend, rm, store)

	r := chi.NewRouter()
	RegisterRoutes(r)

	return r, dl, cleanup
}

// healthLineRE captures the healthcare target-total figure rendered on the
// Budget card's "Health: $actual of $target" breakdown line (kpis.html,
// verdict.go's BudgetVerdict.Healthcare.Configured gate).
var healthLineRE = regexp.MustCompile(`(?s)Health:.*?of\s*<span class="num">\$([0-9,]+\.[0-9]{2})</span>`)

// sinceLineRE captures the Monthly Healthcare card's coverage-start
// provenance note, shown only when HealthcareCoverageStartInRange is true.
var sinceLineRE = regexp.MustCompile(`since ([A-Za-z]+ \d+, \d{4})`)

func parseMoney(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	if err != nil {
		t.Fatalf("parseMoney(%q): %v", s, err)
	}
	return v
}

// healthTargetTotalFromBody extracts the rendered "Health: ... of $X" figure
// from a kpis.html render (used by both /dashboard and /dashboard/kpis).
func healthTargetTotalFromBody(t *testing.T, body string) float64 {
	t.Helper()
	m := healthLineRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("Health target-total line not found in body:\n%s", body)
	}
	return parseMoney(t, m[1])
}

// sinceDateFromBody returns the coverage-start provenance date string (e.g.
// "Feb 5, 2025") if the "since ..." note is present, or "" if absent.
func sinceDateFromBody(body string) string {
	m := sinceLineRE.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

// chartTargetLineY0 issues a budget-vs-actual chart request and returns the
// dashed combined-target shape's y0 value.
func chartTargetLineY0(t *testing.T, router chi.Router, query string) float64 {
	t.Helper()
	rec := doGet(t, router, "/dashboard/charts/data/budget-vs-actual?"+query)
	if rec.Code != 200 {
		t.Fatalf("chart request status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("chart response not JSON: %v; body: %s", err, rec.Body.String())
	}
	layout, ok := payload["layout"].(map[string]interface{})
	if !ok {
		t.Fatalf("layout missing or wrong type; body: %s", rec.Body.String())
	}
	shapes, ok := layout["shapes"].([]interface{})
	if !ok || len(shapes) == 0 {
		t.Fatalf("layout.shapes missing or empty; body: %s", rec.Body.String())
	}
	shape, ok := shapes[0].(map[string]interface{})
	if !ok {
		t.Fatalf("shapes[0] wrong type; body: %s", rec.Body.String())
	}
	y0, ok := shape["y0"].(float64)
	if !ok {
		t.Fatalf("shapes[0].y0 missing or wrong type; body: %s", rec.Body.String())
	}
	return y0
}

func closeEnough(a, b float64) bool { return math.Abs(a-b) < 0.02 }

// TestCoverageStartFullLedgerNotWindow_AllThreeHandlerSites is a mutation-
// killing regression for the "full-ledger derivation" gap (ruling
// 2026-08-30b/c) at handleDashboard, handleKPIsPartial, and handleChartData's
// budget-vs-actual path. The earliest Health Insurance bill sits well before
// the queried window; a second bill sits inside it. A window-derived
// coverage start (mutating the call site's data.Active() argument to the
// range-filtered set) can only ever see the in-window bill, materially
// changing the rendered healthcare target figures at all three sites.
func TestCoverageStartFullLedgerNotWindow_AllThreeHandlerSites(t *testing.T) {
	rows := [][]string{
		// Pre-window bill: well before the queried window below. A
		// window-derived coverage start can never see this row.
		{"2024-11-05", "Health Premium Nov", "-900", "Health Insurance"},
		// In-window bill: the only one a (buggy) window-derived coverage
		// start could ever find.
		{"2025-02-05", "Health Premium Feb", "-900", "Health Insurance"},
		{"2025-01-10", "Rent", "-1500", "Housing"},
		{"2025-02-10", "Rent", "-1500", "Housing"},
		{"2025-03-10", "Rent", "-1500", "Housing"},
	}
	router, _, cleanup := setupCoverageMutationEnv(t, rows, 2000, 900)
	defer cleanup()

	rangeStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	rangeEnd := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)
	query := "start=2025-01-01&end=2025-03-31"

	// Correct: coverage start (2024-11-05) predates the window entirely, so
	// the whole window is covered.
	monthsInRange := metrics.MonthsBetween(rangeStart, rangeEnd)
	coverageStartCorrect := time.Date(2024, 11, 5, 0, 0, 0, 0, time.UTC)
	coverageMonthsCorrect := metrics.ClippedHealthcareMonths(rangeStart, rangeEnd, coverageStartCorrect, true)
	wantHealthTargetTotal := 900 * coverageMonthsCorrect
	wantProratedLine := (2000*monthsInRange + 900*coverageMonthsCorrect) / monthsInRange

	// What a window-filtered mutation would produce: coverage start
	// anchored on the Feb 5 bill instead, clipping the covered months down
	// to the back half of the window.
	coverageStartMutated := time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC)
	coverageMonthsMutated := metrics.ClippedHealthcareMonths(rangeStart, rangeEnd, coverageStartMutated, true)
	mutatedHealthTargetTotal := 900 * coverageMonthsMutated
	mutatedProratedLine := (2000*monthsInRange + 900*coverageMonthsMutated) / monthsInRange
	if closeEnough(wantHealthTargetTotal, mutatedHealthTargetTotal) || closeEnough(wantProratedLine, mutatedProratedLine) {
		t.Fatalf("test fixture precondition broken: full-ledger (%v/%v) and window-derived (%v/%v) figures "+
			"don't differ enough to distinguish", wantHealthTargetTotal, wantProratedLine, mutatedHealthTargetTotal, mutatedProratedLine)
	}

	// Site 1: handleDashboard.
	rec := doGet(t, router, "/dashboard?"+query)
	if rec.Code != 200 {
		t.Fatalf("/dashboard status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if got := healthTargetTotalFromBody(t, body); !closeEnough(got, wantHealthTargetTotal) {
		t.Errorf("/dashboard Health target total = %v, want %v (coverage start must come from the full "+
			"ledger, not the window-derived %v a mutated handleDashboard call site would produce)",
			got, wantHealthTargetTotal, mutatedHealthTargetTotal)
	}
	if since := sinceDateFromBody(body); since != "" {
		t.Errorf("/dashboard rendered a coverage-start provenance note (%q); coverage predates the window "+
			"so none should show (a window-derived mutation would incorrectly show \"since Feb 5, 2025\")", since)
	}

	// Site 2: handleKPIsPartial.
	recK := doGet(t, router, "/dashboard/kpis?"+query)
	if recK.Code != 200 {
		t.Fatalf("/dashboard/kpis status = %d; body: %s", recK.Code, recK.Body.String())
	}
	bodyK := recK.Body.String()
	if got := healthTargetTotalFromBody(t, bodyK); !closeEnough(got, wantHealthTargetTotal) {
		t.Errorf("/dashboard/kpis Health target total = %v, want %v (coverage start must come from the full "+
			"ledger, not the window-derived %v a mutated handleKPIsPartial call site would produce)",
			got, wantHealthTargetTotal, mutatedHealthTargetTotal)
	}
	if since := sinceDateFromBody(bodyK); since != "" {
		t.Errorf("/dashboard/kpis rendered a coverage-start provenance note (%q); want none", since)
	}

	// Site 3: handleChartData budget-vs-actual.
	y0 := chartTargetLineY0(t, router, query)
	if !closeEnough(y0, wantProratedLine) {
		t.Errorf("budget-vs-actual target line y0 = %v, want %v (coverage start must come from the full "+
			"ledger, not the window-derived %v a mutated handleChartData call site would produce)",
			y0, wantProratedLine, mutatedProratedLine)
	}
}

// TestCoverageStartExcludesSuppressedDuplicate_AllThreeHandlerSites is a
// mutation-killing regression for the "duplicates excluded" gap (ruling
// 2026-08-30b/c) at the same three handler sites. The earliest Health
// Insurance bill is duplicate-suppressed via the real dataloader duplicate-
// decision flow (dl.SaveDuplicateDecision -- the same mechanism
// accounts_card_test.go uses; no UI/HTTP duplicates-review flow needed).
// Its near-duplicate twin is kept but categorized OUTSIDE Health Insurance,
// so it never itself defines a coverage start. A duplicates-included
// mutation (passing the raw, non-Active set) would still anchor on the
// suppressed row.
func TestCoverageStartExcludesSuppressedDuplicate_AllThreeHandlerSites(t *testing.T) {
	rows := [][]string{
		// Near-duplicate pair (same amount, within the 7-day window; one
		// bill-pay-shaped description, one posted-check-shaped, per
		// near_duplicates.go's classify()) -- the bill-pay side gets
		// suppressed below. Well before the queried window.
		{"2024-11-05", "Health Ins Premium", "-900", "Health Insurance"},
		{"2024-11-07", "Check #1002", "-900", "Other"},
		// Genuine, un-suppressed in-window bill (distinct amount so it
		// never pairs with the rows above).
		{"2025-02-05", "Health Premium Feb", "-925", "Health Insurance"},
		{"2025-01-10", "Rent", "-1500", "Housing"},
		{"2025-02-10", "Rent", "-1500", "Housing"},
		{"2025-03-10", "Rent", "-1500", "Housing"},
	}
	router, dl, cleanup := setupCoverageMutationEnv(t, rows, 2000, 900)
	defer cleanup()

	// Discover the candidate pair's hashes (first load, before any decision
	// is recorded) and resolve it: keep the posted check, suppress the
	// bill-pay side.
	ts1, err := dl.LoadData()
	if err != nil {
		t.Fatalf("first LoadData: %v", err)
	}
	var pairKey, billHash, checkHash string
	for _, tr := range ts1.Transactions {
		if strings.HasPrefix(tr.Description, "Check") {
			checkHash = tr.Hash
		} else if tr.Description == "Health Ins Premium" {
			billHash = tr.Hash
		}
		if tr.DuplicatePairKey != "" {
			pairKey = tr.DuplicatePairKey
		}
	}
	if pairKey == "" || billHash == "" || checkHash == "" {
		t.Fatalf("setup: did not detect the expected candidate pair (pairKey=%q bill=%q check=%q)", pairKey, billHash, checkHash)
	}
	if err := dl.SaveDuplicateDecision(pairKey, dataloader.DuplicateDecision{
		KeptHash:       checkHash,
		SuppressedHash: billHash,
		Outcome:        dataloader.DuplicateOutcomeKeptWinner,
	}); err != nil {
		t.Fatalf("SaveDuplicateDecision: %v", err)
	}

	rangeStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	rangeEnd := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)
	query := "start=2025-01-01&end=2025-03-31"
	monthsInRange := metrics.MonthsBetween(rangeStart, rangeEnd)

	// Correct: coverage start is the earliest ACTIVE Health Insurance bill
	// (the suppressed 2024-11-05 row is excluded, and the kept twin is
	// categorized "Other") -- 2025-02-05, inside the window.
	coverageStartCorrect := time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC)
	coverageMonthsCorrect := metrics.ClippedHealthcareMonths(rangeStart, rangeEnd, coverageStartCorrect, true)
	wantHealthTargetTotal := 900 * coverageMonthsCorrect
	wantProratedLine := (2000*monthsInRange + 900*coverageMonthsCorrect) / monthsInRange

	// What a duplicates-included mutation would produce: coverage start
	// anchored on the suppressed 2024-11-05 row, which predates the window
	// entirely -- full-window accrual.
	mutatedHealthTargetTotal := 900 * monthsInRange
	mutatedProratedLine := 2000 + 900.0

	if closeEnough(wantHealthTargetTotal, mutatedHealthTargetTotal) || closeEnough(wantProratedLine, mutatedProratedLine) {
		t.Fatalf("test fixture precondition broken: active-only (%v/%v) and duplicates-included (%v/%v) figures "+
			"don't differ enough to distinguish", wantHealthTargetTotal, wantProratedLine, mutatedHealthTargetTotal, mutatedProratedLine)
	}

	// Site 1: handleDashboard.
	rec := doGet(t, router, "/dashboard?"+query)
	if rec.Code != 200 {
		t.Fatalf("/dashboard status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if got := healthTargetTotalFromBody(t, body); !closeEnough(got, wantHealthTargetTotal) {
		t.Errorf("/dashboard Health target total = %v, want %v (coverage start must exclude the suppressed "+
			"duplicate; a duplicates-included mutation would produce %v)", got, wantHealthTargetTotal, mutatedHealthTargetTotal)
	}
	if since := sinceDateFromBody(body); since != "Feb 5, 2025" {
		t.Errorf("/dashboard coverage-start note = %q, want \"Feb 5, 2025\" (a duplicates-included mutation "+
			"would anchor on the suppressed Nov 5, 2024 row instead and show no note at all)", since)
	}

	// Site 2: handleKPIsPartial.
	recK := doGet(t, router, "/dashboard/kpis?"+query)
	if recK.Code != 200 {
		t.Fatalf("/dashboard/kpis status = %d; body: %s", recK.Code, recK.Body.String())
	}
	bodyK := recK.Body.String()
	if got := healthTargetTotalFromBody(t, bodyK); !closeEnough(got, wantHealthTargetTotal) {
		t.Errorf("/dashboard/kpis Health target total = %v, want %v (coverage start must exclude the "+
			"suppressed duplicate; a duplicates-included mutation would produce %v)", got, wantHealthTargetTotal, mutatedHealthTargetTotal)
	}
	if since := sinceDateFromBody(bodyK); since != "Feb 5, 2025" {
		t.Errorf("/dashboard/kpis coverage-start note = %q, want \"Feb 5, 2025\"", since)
	}

	// Site 3: handleChartData budget-vs-actual.
	y0 := chartTargetLineY0(t, router, query)
	if !closeEnough(y0, wantProratedLine) {
		t.Errorf("budget-vs-actual target line y0 = %v, want %v (coverage start must exclude the suppressed "+
			"duplicate; a duplicates-included mutation would produce %v)", y0, wantProratedLine, mutatedProratedLine)
	}
}
