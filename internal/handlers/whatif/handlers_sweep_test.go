package whatif

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// ── conversionSweepAmounts ───────────────────────────────────────────────

// TestConversionSweepAmounts_CurrentAlreadyInBaseList covers acceptance
// criterion #3: with the saved plan's current amount already one of the
// base ladder values (e.g. $50k), the sweep evaluates exactly the 9 base
// amounts — no insertion, no duplicate.
func TestConversionSweepAmounts_CurrentAlreadyInBaseList(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.RothConversion = &models.RothConversionConfig{Enabled: true, AnnualAmount: 50_000}

	amounts := conversionSweepAmounts(settings)
	if len(amounts) != 9 {
		t.Fatalf("len(amounts) = %d, want 9: %v", len(amounts), amounts)
	}
	want := []float64{0, 25_000, 50_000, 75_000, 100_000, 125_000, 150_000, 175_000, 200_000}
	for i, w := range want {
		if amounts[i] != w {
			t.Errorf("amounts[%d] = %v, want %v (full: %v)", i, amounts[i], w, amounts)
		}
	}
}

// TestConversionSweepAmounts_InsertsCurrentAmount covers the insertion path:
// a current amount NOT in the base ladder is inserted once, in ascending
// order, with no duplicate.
func TestConversionSweepAmounts_InsertsCurrentAmount(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.RothConversion = &models.RothConversionConfig{Enabled: true, AnnualAmount: 60_000}

	amounts := conversionSweepAmounts(settings)
	if len(amounts) != 10 {
		t.Fatalf("len(amounts) = %d, want 10: %v", len(amounts), amounts)
	}
	want := []float64{0, 25_000, 50_000, 60_000, 75_000, 100_000, 125_000, 150_000, 175_000, 200_000}
	for i, w := range want {
		if amounts[i] != w {
			t.Errorf("amounts[%d] = %v, want %v (full: %v)", i, amounts[i], w, amounts)
		}
	}
}

// TestConversionSweepAmounts_NilRothConversionTreatsCurrentAsZero covers the
// unset-Roth-conversion case: current amount defaults to 0, which is
// already in the base ladder, so no insertion happens.
func TestConversionSweepAmounts_NilRothConversionTreatsCurrentAsZero(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.RothConversion = nil

	amounts := conversionSweepAmounts(settings)
	if len(amounts) != 9 {
		t.Fatalf("len(amounts) = %d, want 9: %v", len(amounts), amounts)
	}
}

// TestConversionSweepAmounts_InsertsAboveMax covers insertion at the tail
// when current exceeds every base amount.
func TestConversionSweepAmounts_InsertsAboveMax(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.RothConversion = &models.RothConversionConfig{Enabled: true, AnnualAmount: 250_000}

	amounts := conversionSweepAmounts(settings)
	if len(amounts) != 10 {
		t.Fatalf("len(amounts) = %d, want 10: %v", len(amounts), amounts)
	}
	if amounts[len(amounts)-1] != 250_000 {
		t.Errorf("last amount = %v, want 250000 (full: %v)", amounts[len(amounts)-1], amounts)
	}
}

// ── candidateSettingsForConversionAmount ─────────────────────────────────

// TestCandidateSettingsForConversionAmount_DoesNotMutateSaved covers the
// copy discipline mirrored from candidateSettingsForSS: building a candidate
// must never mutate the caller's settings, including its RothConversion
// pointer target.
func TestCandidateSettingsForConversionAmount_DoesNotMutateSaved(t *testing.T) {
	saved := models.DefaultWhatIfSettings()
	saved.RothConversion = &models.RothConversionConfig{Enabled: true, AnnualAmount: 50_000, StartYear: 2, EndYear: 10}
	savedPtr := saved.RothConversion

	candidate := candidateSettingsForConversionAmount(saved, 100_000)

	if saved.RothConversion != savedPtr {
		t.Fatal("saved.RothConversion pointer changed — candidate build mutated the caller's settings")
	}
	if saved.RothConversion.AnnualAmount != 50_000 {
		t.Errorf("saved.RothConversion.AnnualAmount = %v, want unchanged 50000", saved.RothConversion.AnnualAmount)
	}
	if candidate.RothConversion.AnnualAmount != 100_000 {
		t.Errorf("candidate.RothConversion.AnnualAmount = %v, want 100000", candidate.RothConversion.AnnualAmount)
	}
	if candidate.RothConversion == saved.RothConversion {
		t.Error("candidate.RothConversion aliases saved.RothConversion — must be a fresh copy")
	}
	// Start/end years are preserved from the saved config.
	if candidate.RothConversion.StartYear != 2 || candidate.RothConversion.EndYear != 10 {
		t.Errorf("candidate start/end years = %d/%d, want 2/10", candidate.RothConversion.StartYear, candidate.RothConversion.EndYear)
	}
}

// TestCandidateSettingsForConversionAmount_EnabledFollowsAmount covers the
// enabled = amount > 0 rule, including the amount == 0 case (which must
// disable conversions rather than leave a stale Enabled=true from the saved
// config).
func TestCandidateSettingsForConversionAmount_EnabledFollowsAmount(t *testing.T) {
	saved := models.DefaultWhatIfSettings()
	saved.RothConversion = &models.RothConversionConfig{Enabled: true, AnnualAmount: 50_000}

	zero := candidateSettingsForConversionAmount(saved, 0)
	if zero.RothConversion.Enabled {
		t.Error("amount=0 candidate should have Enabled=false")
	}

	nonZero := candidateSettingsForConversionAmount(saved, 75_000)
	if !nonZero.RothConversion.Enabled {
		t.Error("amount=75000 candidate should have Enabled=true")
	}
}

// TestCandidateSettingsForConversionAmount_NilSavedRothConversion covers
// building a candidate when the saved plan has no Roth conversion config at
// all (nil), which must not panic and must default start/end years to zero.
func TestCandidateSettingsForConversionAmount_NilSavedRothConversion(t *testing.T) {
	saved := models.DefaultWhatIfSettings()
	saved.RothConversion = nil

	candidate := candidateSettingsForConversionAmount(saved, 25_000)
	if candidate.RothConversion == nil || candidate.RothConversion.AnnualAmount != 25_000 {
		t.Fatalf("candidate.RothConversion = %+v, want AnnualAmount 25000", candidate.RothConversion)
	}
}

// ── handleWhatIfConversionSweep ──────────────────────────────────────────

// sweepScenarioSettings returns a deterministic scenario tuned so that
// amounts 0/25k/50k survive the 30-year horizon and 75k+ deplete it before
// the horizon ends — giving both branches of the survives/depletes render
// split real engine output to assert on, rather than a synthetic fixture.
// Discovered empirically (see task notes); PortfolioValue and
// MonthlyLivingExpenses are load-bearing for the split and must not be
// changed without re-verifying the split still holds.
func sweepScenarioSettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 65
	s.ProjectionYears = 30
	s.PortfolioValue = 750_000
	s.TaxDeferredPercent = 80
	s.RothPercent = 5
	s.MonthlyLivingExpenses = 2700
	s.MonthlyHealthcare = 300
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.RothConversion = &models.RothConversionConfig{Enabled: true, AnnualAmount: 50_000}
	return s
}

// sweepRowsFromJSON decodes the handler's JSON fallback body (renderer=nil)
// into a slice of row maps, keyed exactly as ConversionSweepRow's exported
// fields (encoding/json defaults to the Go field name when there is no json
// tag).
func sweepRowsFromJSON(t *testing.T, body []byte) []map[string]interface{} {
	t.Helper()
	var decoded struct {
		Rows []map[string]interface{} `json:"Rows"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal sweep response: %v\nbody: %s", err, body)
	}
	return decoded.Rows
}

// TestHandleWhatIfConversionSweep_RowCountOrderAndCurrent covers acceptance
// criteria #2 and #3: one row per candidate amount ordered ascending, 200
// status, and exactly 9 rows with the $50k row (and only that row) marked
// current when the saved plan's current amount is $50k.
func TestHandleWhatIfConversionSweep_RowCountOrderAndCurrent(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	if err := rm.Save(sweepScenarioSettings()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	start := time.Now()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/whatif/conversion-sweep", nil)
	handleWhatIfConversionSweep(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if elapsed > 2*time.Second {
		t.Errorf("handler took %s, want well under 2s (no RunFull/Monte Carlo)", elapsed)
	}

	rows := sweepRowsFromJSON(t, w.Body.Bytes())
	if len(rows) != 9 {
		t.Fatalf("len(rows) = %d, want 9: %v", len(rows), rows)
	}

	wantAmounts := []float64{0, 25_000, 50_000, 75_000, 100_000, 125_000, 150_000, 175_000, 200_000}
	currentCount := 0
	for i, row := range rows {
		amount, _ := row["Amount"].(float64)
		if amount != wantAmounts[i] {
			t.Errorf("rows[%d].Amount = %v, want %v (ascending order violated)", i, amount, wantAmounts[i])
		}
		current, _ := row["Current"].(bool)
		if current {
			currentCount++
			if amount != 50_000 {
				t.Errorf("unexpected current row at amount %v, want only the 50000 row marked current", amount)
			}
		}
	}
	if currentCount != 1 {
		t.Errorf("currentCount = %d, want exactly 1", currentCount)
	}
}

// TestHandleWhatIfConversionSweep_SurvivesVsDepletesSplit covers acceptance
// criterion #4: a surviving candidate reports Survives=true plus a positive
// ending real balance and no fabricated depletion month; a depleting
// candidate reports Survives=false plus a positive depletion month and no
// ending balance.
func TestHandleWhatIfConversionSweep_SurvivesVsDepletesSplit(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	if err := rm.Save(sweepScenarioSettings()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/whatif/conversion-sweep", nil)
	handleWhatIfConversionSweep(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	rows := sweepRowsFromJSON(t, w.Body.Bytes())
	if len(rows) != 9 {
		t.Fatalf("len(rows) = %d, want 9: %v", len(rows), rows)
	}

	// amounts 0, 25k, 50k (indices 0-2) survive the horizon.
	for i := 0; i <= 2; i++ {
		row := rows[i]
		survives, _ := row["Survives"].(bool)
		if !survives {
			t.Errorf("rows[%d] (amount %v): Survives = false, want true", i, row["Amount"])
			continue
		}
		endingReal, _ := row["EndingBalanceReal"].(float64)
		if endingReal <= 0 {
			t.Errorf("rows[%d] (amount %v): EndingBalanceReal = %v, want > 0 for a surviving row", i, row["Amount"], endingReal)
		}
		if depletionMonth, ok := row["DepletionMonth"].(float64); ok && depletionMonth != 0 {
			t.Errorf("rows[%d] (amount %v): DepletionMonth = %v, want 0 (no fabricated depletion month for a surviving row)", i, row["Amount"], depletionMonth)
		}
	}

	// amounts 75k..200k (indices 3-8) deplete before the horizon ends.
	for i := 3; i <= 8; i++ {
		row := rows[i]
		survives, _ := row["Survives"].(bool)
		if survives {
			t.Errorf("rows[%d] (amount %v): Survives = true, want false", i, row["Amount"])
			continue
		}
		depletionMonth, _ := row["DepletionMonth"].(float64)
		if depletionMonth <= 0 {
			t.Errorf("rows[%d] (amount %v): DepletionMonth = %v, want > 0 for a depleting row", i, row["Amount"], depletionMonth)
		}
		depletionYears, _ := row["DepletionYears"].(float64)
		if depletionYears <= 0 {
			t.Errorf("rows[%d] (amount %v): DepletionYears = %v, want > 0 for a depleting row", i, row["Amount"], depletionYears)
		}
		if endingReal, ok := row["EndingBalanceReal"].(float64); ok && endingReal != 0 {
			t.Errorf("rows[%d] (amount %v): EndingBalanceReal = %v, want 0 for a depleting row", i, row["Amount"], endingReal)
		}
	}
}

// TestHandleWhatIfConversionSweep_LifetimeTaxAndIRMAAPopulated covers that
// every row carries a non-negative lifetime tax figure (reused from
// analysis.BuildTax) and lifetime IRMAA figure (summed from the
// projection's per-year IRMAA), and that higher conversions — which push
// MAGI into IRMAA territory in this scenario — produce nonzero IRMAA.
func TestHandleWhatIfConversionSweep_LifetimeTaxAndIRMAAPopulated(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	if err := rm.Save(sweepScenarioSettings()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/whatif/conversion-sweep", nil)
	handleWhatIfConversionSweep(w, req)

	rows := sweepRowsFromJSON(t, w.Body.Bytes())
	if len(rows) != 9 {
		t.Fatalf("len(rows) = %d, want 9: %v", len(rows), rows)
	}

	sawNonzeroIRMAA := false
	for _, row := range rows {
		lifetimeTax, _ := row["LifetimeTax"].(float64)
		if lifetimeTax <= 0 {
			t.Errorf("row amount %v: LifetimeTax = %v, want > 0", row["Amount"], lifetimeTax)
		}
		lifetimeIRMAA, _ := row["LifetimeIRMAA"].(float64)
		if lifetimeIRMAA < 0 {
			t.Errorf("row amount %v: LifetimeIRMAA = %v, want >= 0", row["Amount"], lifetimeIRMAA)
		}
		if lifetimeIRMAA > 0 {
			sawNonzeroIRMAA = true
		}
	}
	if !sawNonzeroIRMAA {
		t.Error("expected at least one row with nonzero LifetimeIRMAA in this scenario")
	}
}

// TestHandleWhatIfConversionSweep_LifetimeTaxIsRealNotNominal covers the
// real-vs-nominal defect: the results template's caption claims "Ending
// balance, lifetime tax, and lifetime IRMAA are all in today's dollars", so
// LifetimeTax must be the per-year-deflated (real) sum, not the nominal
// analysis.BuildTax(proj, in).TotalTaxPaid figure. sweepScenarioSettings runs
// a 30-year horizon at 3% inflation, so nominal and real diverge materially
// (~29% on this fixture) — a fixture where the two figures are equal would
// not catch a regression back to the nominal total.
func TestHandleWhatIfConversionSweep_LifetimeTaxIsRealNotNominal(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := sweepScenarioSettings()
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reproduce one sweep candidate's projection independently of
	// buildConversionSweepRow/lifetimeRealTaxAndIRMAA, so the assertion
	// below checks production behavior against a separately-derived
	// expectation rather than restating the same formula.
	const amount = 100_000.0
	baseIn, _, err := buildEngineInput(settings)
	if err != nil {
		t.Fatalf("buildEngineInput: %v", err)
	}
	candidate := candidateSettingsForConversionAmount(settings, amount)
	prepared, err := prepare.From(candidate)
	if err != nil {
		t.Fatalf("prepare.From: %v", err)
	}
	in := engine.Input{Prepared: prepared, Chain: baseIn.Chain, Hooks: baseIn.Hooks}
	proj := getEngine().Run(in)
	if proj == nil || len(proj.YearlySummaries) == 0 {
		t.Fatal("expected a non-nil projection with yearly summaries")
	}

	nominal := analysis.BuildTax(proj, in)
	if nominal == nil {
		t.Fatal("expected non-nil BuildTax result")
	}

	var wantReal float64
	for _, ys := range proj.YearlySummaries {
		deflator := ys.CumulativeInflation
		if deflator <= 0 {
			deflator = 1
		}
		wantReal += ys.Taxes / deflator
	}

	if wantReal >= nominal.TotalTaxPaid {
		t.Fatalf("fixture does not exercise the defect: real (%v) should be strictly less than nominal (%v) under positive inflation", wantReal, nominal.TotalTaxPaid)
	}

	// Now run the actual sweep endpoint and find the amount=100000 row.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/whatif/conversion-sweep", nil)
	handleWhatIfConversionSweep(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	rows := sweepRowsFromJSON(t, w.Body.Bytes())
	var got float64
	found := false
	for _, row := range rows {
		rowAmount, _ := row["Amount"].(float64)
		if rowAmount == amount {
			got, _ = row["LifetimeTax"].(float64)
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no row for amount %v in response: %v", amount, rows)
	}

	const tol = 0.01
	if diff := got - wantReal; diff > tol || diff < -tol {
		t.Errorf("LifetimeTax = %v, want %v (per-year deflated sum)", got, wantReal)
	}
	if diff := got - nominal.TotalTaxPaid; diff < tol && diff > -tol {
		t.Errorf("LifetimeTax = %v equals nominal TotalTaxPaid %v — must report real, not nominal", got, nominal.TotalTaxPaid)
	}
}

// TestHandleWhatIfConversionSweep_LoadError covers the settings-load failure
// path, matching the precedent in coverage_gaps_test.go for the sibling
// tax-optimize endpoint.
func TestHandleWhatIfConversionSweep_LoadError(t *testing.T) {
	setupBrokenEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/whatif/conversion-sweep", nil)
	handleWhatIfConversionSweep(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Failed to load settings") {
		t.Errorf("body missing load failure message: %s", w.Body.String())
	}
}

// TestHandleWhatIfConversionSweep_BuildEngineInputError covers the
// engine-input build failure path (a dangling scenario chain link).
func TestHandleWhatIfConversionSweep_BuildEngineInputError(t *testing.T) {
	setupBrokenChainEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/whatif/conversion-sweep", nil)
	handleWhatIfConversionSweep(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Failed to build engine input") {
		t.Errorf("body missing engine-input failure message: %s", w.Body.String())
	}
}

// TestHandleWhatIfConversionSweep_RendersPartial covers the renderer path
// (as opposed to the JSON fallback used by the tests above): the real
// "whatif-conversion-sweep-results" partial renders with the expected
// heading and row count.
func TestHandleWhatIfConversionSweep_RendersPartial(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	if err := rm.Save(sweepScenarioSettings()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/whatif/conversion-sweep", nil)
	handleWhatIfConversionSweep(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Conversion Sweep") {
		t.Errorf("response missing 'Conversion Sweep' heading; body snippet: %s", truncate(body, 400))
	}
	if n := strings.Count(body, "<tr"); n != 10 { // 9 body rows + 1 <thead> row
		t.Errorf("expected 10 <tr> total (9 body rows + 1 header row), got %d", n)
	}
}

// ── markBestConversionSweepRows (T16) ────────────────────────────────────

// TestMarkBestConversionSweepRows_EmptyIsNoop covers the empty-slice guard:
// must not panic (no index 0 to touch).
func TestMarkBestConversionSweepRows_EmptyIsNoop(t *testing.T) {
	rows := []ConversionSweepRow{}
	markBestConversionSweepRows(rows) // must not panic
}

// TestMarkBestConversionSweepRows_DistinctWinners covers the base case: the
// row with the lowest lifetime tax and the row that lasts longest are
// different rows, each getting exactly its own marker.
func TestMarkBestConversionSweepRows_DistinctWinners(t *testing.T) {
	rows := []ConversionSweepRow{
		{Amount: 0, LifetimeTax: 500_000, Survives: true, EndingBalanceReal: 100_000},
		{Amount: 50_000, LifetimeTax: 300_000, Survives: true, EndingBalanceReal: 900_000},    // least tax
		{Amount: 100_000, LifetimeTax: 700_000, Survives: true, EndingBalanceReal: 1_500_000}, // longest-lasting
	}
	markBestConversionSweepRows(rows)

	if !rows[1].LeastLifetimeTax {
		t.Error("rows[1] (lowest LifetimeTax) should have LeastLifetimeTax = true")
	}
	if rows[0].LeastLifetimeTax || rows[2].LeastLifetimeTax {
		t.Error("only rows[1] should have LeastLifetimeTax = true")
	}
	if !rows[2].LongestLasting {
		t.Error("rows[2] (highest EndingBalanceReal among survivors) should have LongestLasting = true")
	}
	if rows[0].LongestLasting || rows[1].LongestLasting {
		t.Error("only rows[2] should have LongestLasting = true")
	}
}

// TestMarkBestConversionSweepRows_SurvivesBeatsAnyDepletion covers the rule
// that a surviving row always beats a depleting row for "longest-lasting",
// no matter how large the depleting row's DepletionMonth is.
func TestMarkBestConversionSweepRows_SurvivesBeatsAnyDepletion(t *testing.T) {
	rows := []ConversionSweepRow{
		{Amount: 0, Survives: false, DepletionMonth: 5000},     // huge depletion month, does not survive
		{Amount: 50_000, Survives: true, EndingBalanceReal: 1}, // survives, tiny balance
	}
	markBestConversionSweepRows(rows)

	if !rows[1].LongestLasting {
		t.Error("the surviving row must win LongestLasting even with a tiny ending balance")
	}
	if rows[0].LongestLasting {
		t.Error("the depleting row must not win LongestLasting no matter its DepletionMonth")
	}
}

// TestMarkBestConversionSweepRows_TieOnLifetimeTaxPrefersSmallerAmount
// covers the tie-break rule for the least-tax marker, using an
// amount-descending input order so the result cannot be an accident of
// iteration order.
func TestMarkBestConversionSweepRows_TieOnLifetimeTaxPrefersSmallerAmount(t *testing.T) {
	rows := []ConversionSweepRow{
		{Amount: 100_000, LifetimeTax: 500_000, Survives: true},
		{Amount: 50_000, LifetimeTax: 500_000, Survives: true}, // tied tax, smaller amount
	}
	markBestConversionSweepRows(rows)

	if !rows[1].LeastLifetimeTax {
		t.Error("the smaller-amount row should win the LifetimeTax tie")
	}
	if rows[0].LeastLifetimeTax {
		t.Error("the larger-amount row should not win the LifetimeTax tie")
	}
}

// TestMarkBestConversionSweepRows_TieOnSurvivorBalancePrefersSmallerAmount
// covers the tie-break rule among survivors with equal EndingBalanceReal,
// using an amount-descending input order.
func TestMarkBestConversionSweepRows_TieOnSurvivorBalancePrefersSmallerAmount(t *testing.T) {
	rows := []ConversionSweepRow{
		{Amount: 100_000, Survives: true, EndingBalanceReal: 900_000},
		{Amount: 50_000, Survives: true, EndingBalanceReal: 900_000}, // tied balance, smaller amount
	}
	markBestConversionSweepRows(rows)

	if !rows[1].LongestLasting {
		t.Error("the smaller-amount row should win the ending-balance tie")
	}
	if rows[0].LongestLasting {
		t.Error("the larger-amount row should not win the ending-balance tie")
	}
}

// TestMarkBestConversionSweepRows_TieOnDepletionMonthPrefersSmallerAmount
// covers the tie-break rule among depleting rows with equal DepletionMonth.
func TestMarkBestConversionSweepRows_TieOnDepletionMonthPrefersSmallerAmount(t *testing.T) {
	rows := []ConversionSweepRow{
		{Amount: 150_000, Survives: false, DepletionMonth: 300},
		{Amount: 75_000, Survives: false, DepletionMonth: 300}, // tied depletion month, smaller amount
	}
	markBestConversionSweepRows(rows)

	if !rows[1].LongestLasting {
		t.Error("the smaller-amount row should win the depletion-month tie")
	}
	if rows[0].LongestLasting {
		t.Error("the larger-amount row should not win the depletion-month tie")
	}
}

// TestMarkBestConversionSweepRows_CombinedMarkerWhenSameRowWinsBoth covers
// the "one combined marker" rule: a single row can end up with both
// LeastLifetimeTax and LongestLasting true.
func TestMarkBestConversionSweepRows_CombinedMarkerWhenSameRowWinsBoth(t *testing.T) {
	rows := []ConversionSweepRow{
		{Amount: 0, LifetimeTax: 900_000, Survives: false, DepletionMonth: 200},
		{Amount: 25_000, LifetimeTax: 100_000, Survives: true, EndingBalanceReal: 2_000_000}, // wins both
	}
	markBestConversionSweepRows(rows)

	if !rows[1].LeastLifetimeTax || !rows[1].LongestLasting {
		t.Errorf("rows[1] should win both markers, got LeastLifetimeTax=%v LongestLasting=%v", rows[1].LeastLifetimeTax, rows[1].LongestLasting)
	}
	if rows[0].LeastLifetimeTax || rows[0].LongestLasting {
		t.Error("rows[0] should win neither marker")
	}
}

// ── Apply flow (T16) ─────────────────────────────────────────────────────

// TestConversionSweepApply_ThroughRealRouter covers acceptance criterion #2:
// an Apply POST through the real router (same route/handler as the
// standalone Roth Conversion form, POST /whatif/roth-conversion) persists
// the amount — preserving the saved start/end years and setting
// enabled = amount > 0 — and the response is the re-rendered sweep table
// with the "current" marker moved to the newly-applied row. Also verifies
// persistence by reloading settings from disk and by independently
// re-running the sweep endpoint.
func TestConversionSweepApply_ThroughRealRouter(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := sweepScenarioSettings() // current amount 50,000
	settings.RothConversion.StartYear = 3
	settings.RothConversion.EndYear = 12
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r)
	ts := httptest.NewServer(r)
	defer ts.Close()

	form := url.Values{
		"apply_source":  {"conversion-sweep"},
		"annual_amount": {"100000"},
		"enabled":       {"on"},
		"start_year":    {"3"},
		"end_year":      {"12"},
	}
	resp, err := http.PostForm(ts.URL+"/whatif/roth-conversion", form)
	if err != nil {
		t.Fatalf("POST /whatif/roth-conversion: %v", err)
	}
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, body)
	}

	// The response must be the sweep results partial (not the standard
	// whatif-results-with-oob partial) with the aria-live confirmation, and
	// must mark the $100,000 row current — not the $50,000 row.
	if !strings.Contains(body, "Conversion Sweep") {
		t.Fatalf("expected the sweep results partial, got: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `aria-live="polite"`) || !strings.Contains(body, "$100,000.00 annual conversion") {
		t.Errorf("expected an aria-live confirmation naming the applied amount; body: %s", truncate(body, 2000))
	}
	if n := strings.Count(body, ">current<"); n != 1 {
		t.Fatalf("expected exactly one current marker, got %d; body: %s", n, truncate(body, 3000))
	}
	currentIdx := strings.Index(body, ">current<")
	rowStart := strings.LastIndex(body[:currentIdx], "<tr")
	rowEnd := strings.Index(body[currentIdx:], "</tr>")
	row := body[rowStart : currentIdx+rowEnd]
	if !strings.Contains(row, "$100,000.00") {
		t.Errorf("expected the current marker on the $100,000 row, got row: %s", row)
	}

	// Persistence: reload settings from disk (a fresh Load, not the
	// in-memory settings this test built) and confirm the saved plan now
	// has the applied amount with start/end years preserved.
	saved, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.RothConversion == nil || saved.RothConversion.AnnualAmount != 100_000 {
		t.Fatalf("saved.RothConversion = %+v, want AnnualAmount 100000", saved.RothConversion)
	}
	if saved.RothConversion.StartYear != 3 || saved.RothConversion.EndYear != 12 {
		t.Errorf("saved start/end years = %d/%d, want 3/12 (preserved)", saved.RothConversion.StartYear, saved.RothConversion.EndYear)
	}
	if !saved.RothConversion.Enabled {
		t.Error("saved.RothConversion.Enabled = false, want true (amount > 0)")
	}

	// A fresh, independent sweep run also marks $100,000 current — proves
	// the change is a real save, not just a one-off render artifact of the
	// apply response.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/whatif/conversion-sweep", nil)
	handleWhatIfConversionSweep(w2, req2)
	body2 := w2.Body.String()
	if n := strings.Count(body2, ">current<"); n != 1 {
		t.Fatalf("re-run sweep: expected exactly one current marker, got %d", n)
	}
	current2Idx := strings.Index(body2, ">current<")
	row2Start := strings.LastIndex(body2[:current2Idx], "<tr")
	row2End := strings.Index(body2[current2Idx:], "</tr>")
	row2 := body2[row2Start : current2Idx+row2End]
	if !strings.Contains(row2, "$100,000.00") {
		t.Errorf("re-run sweep: expected current marker on $100,000 row, got row: %s", row2)
	}
}

// TestConversionSweepApply_ZeroAmountDisablesConversion covers applying the
// $0 row: enabled must end up false (not just amount 0) per the
// enabled = amount > 0 rule, matching handleWhatIfRothConversion's own
// semantics for the standalone form.
func TestConversionSweepApply_ZeroAmountDisablesConversion(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := sweepScenarioSettings()
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	form := url.Values{
		"apply_source":  {"conversion-sweep"},
		"annual_amount": {"0"},
		"start_year":    {"0"},
		"end_year":      {"0"},
	}
	req := httptest.NewRequest(http.MethodPost, "/whatif/roth-conversion", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleWhatIfRothConversion(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	saved, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.RothConversion == nil {
		t.Fatal("expected a non-nil RothConversion after applying $0")
	}
	if saved.RothConversion.AnnualAmount != 0 {
		t.Errorf("saved.RothConversion.AnnualAmount = %v, want 0", saved.RothConversion.AnnualAmount)
	}
	if saved.RothConversion.Enabled {
		t.Error("saved.RothConversion.Enabled = true, want false for a $0 apply")
	}
}
