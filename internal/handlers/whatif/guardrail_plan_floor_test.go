package whatif

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
)

// guardrailPlanFloorFixture mirrors the brief's live-plan scenario: a
// spending phase (No-Go) plans living spending BELOW the requested minimum
// by design, independent of guardrails or market risk. A large portfolio
// keeps depletion out of the picture so the effect under test isn't
// confounded with funding risk.
func guardrailPlanFloorFixture(t *testing.T, rm *retirement.SettingsManager) *models.WhatIfSettings {
	t.Helper()
	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.PortfolioValue = 3000000
	s.MonthlyLivingExpenses = 10000
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 0
	s.ProjectionYears = 3
	s.CurrentAge = 65
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.00},
			// 50% of 10000 = 5000/mo, well below the 7500 floor used below,
			// and reached in year 1 (age 66), inside the 3-year horizon.
			{Name: "No-Go", StartAge: 66, Multiplier: 0.50},
		},
	}
	s.Guardrails = nil
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	loaded, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

// guardrailFlatFixture is the same shape but WITHOUT a phase decline: every
// month plans the full 10000/mo, above the 7500 floor, so the plan-design
// notice must NOT appear.
func guardrailFlatFixture(t *testing.T, rm *retirement.SettingsManager) *models.WhatIfSettings {
	t.Helper()
	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.PortfolioValue = 3000000
	s.MonthlyLivingExpenses = 10000
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 0
	s.ProjectionYears = 3
	s.CurrentAge = 65
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: false}
	s.Guardrails = nil
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	loaded, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

// guardrailPlanFloorAllMissFixture is the same phase-decline shape as
// guardrailPlanFloorFixture (so the plan-design floor notice still fires),
// but with a starting portfolio far too small to sustain a 7500/mo absolute
// floor -- with no other income source -- across the Monte Carlo validation
// horizon (at least 10 years; see HorizonMinYears below). Every candidate,
// baselines and every grid candidate alike, misses the target in practice:
// this isolates the "adequately floored (Guardrails.MinMonthlySpendingReal
// == the requested floor) but still below target for funding reasons" case
// from the "plan design schedules below the floor" case, since every grid
// candidate in guardrailOptimizerPolicies sets MinMonthlySpendingReal to
// the requested floor exactly, so none of them may carry the plan-design
// explanation, while the two baselines (nil/disabled Guardrails) must. Note
// this is NOT mathematically airtight (Monte Carlo auto-seeds from the
// wall clock by design; see analysis.EffectiveSeed) -- it is a wide margin
// of funding failure verified empirically stable (0.00% for every row
// across repeated runs at this PortfolioValue), the same standard the
// optimizer's existing MC-driven tests already rely on.
func guardrailPlanFloorAllMissFixture(t *testing.T, rm *retirement.SettingsManager) *models.WhatIfSettings {
	t.Helper()
	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	// 150000: large enough that the deterministic base projection (used for
	// the plan-design floor notice; see analysis.LowestPlannedLivingReal)
	// survives past the year-1 No-Go transition without depleting -- an
	// under-funded portfolio truncates that projection at depletion (see
	// runMonthlyLoop's break-on-depleted), which would hide the No-Go
	// phase's lower planned figure entirely.
	s.PortfolioValue = 150000
	s.MonthlyLivingExpenses = 10000
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 0
	s.ProjectionYears = 3
	s.CurrentAge = 65
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.00},
			{Name: "No-Go", StartAge: 66, Multiplier: 0.50},
		},
	}
	s.Guardrails = nil
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	loaded, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

// guardrailOptimizerRowsFromBody parses each result row's policy name (the
// <th scope="row"> cell) and its rendered "Below target"/"Meets target"
// wording (the <span> immediately following the Chance-of-maintaining
// <strong> cell) from a rendered whatif-guardrail-optimizer-results body,
// keyed by the trimmed policy name. It fails the test on any row whose
// shape doesn't match the template, so a shape change is caught here rather
// than silently skipped.
func guardrailOptimizerRowsFromBody(t *testing.T, body string) map[string]string {
	t.Helper()
	const thOpen = `<th scope="row" class="p-2">`
	rows := make(map[string]string)
	for pos := 0; ; {
		i := strings.Index(body[pos:], thOpen)
		if i < 0 {
			break
		}
		start := pos + i + len(thOpen)
		closeIdx := strings.Index(body[start:], "</th>")
		if closeIdx < 0 {
			t.Fatalf("unterminated <th scope=\"row\"> at byte %d", start)
		}
		name := strings.TrimSpace(body[start : start+closeIdx])
		rest := body[start+closeIdx:]
		spanIdx := strings.Index(rest, "<span>")
		if spanIdx < 0 {
			t.Fatalf("row %q missing its outcome <span>", name)
		}
		spanStart := spanIdx + len("<span>")
		spanEnd := strings.Index(rest[spanStart:], "</span>")
		if spanEnd < 0 {
			t.Fatalf("row %q has an unterminated <span>", name)
		}
		wording := strings.TrimSpace(rest[spanStart : spanStart+spanEnd])
		if _, dup := rows[name]; dup {
			t.Fatalf("duplicate row name %q; rows must be uniquely identifiable by name for this parse", name)
		}
		rows[name] = wording
		pos = start + closeIdx
	}
	if len(rows) == 0 {
		t.Fatal("no result rows parsed from body")
	}
	return rows
}

// TestGuardrailOptimizerRowWording_PlanDesignVsFundingRisk is GV3 item 1: a
// handler-level, per-row check that the plan-design "Below target" wording
// is reserved for candidates whose OWN Guardrails.MinMonthlySpendingReal is
// below the requested floor (or absent/disabled), and never appended to a
// candidate that already enforces the requested floor but still misses the
// target for funding reasons. guardrailPlanFloorAllMissFixture guarantees
// every row is non-qualifying, so this exercises every row's wording, not
// just one.
func TestGuardrailOptimizerRowWording_PlanDesignVsFundingRisk(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	guardrailPlanFloorAllMissFixture(t, rm)

	w := runGuardrailOptimizer(t, "7500", "50", "row-wording-plan-vs-funding")
	body := w.Body.String()

	if !strings.Contains(body, `data-guardrail-plan-floor-notice`) {
		t.Fatal("expected the plan-design floor notice with this fixture (No-Go phase plans below the floor)")
	}

	rows := guardrailOptimizerRowsFromBody(t, body)
	const planDesignWording = "Below target — planned spending is below your minimum from year 1"
	sawBaseline, sawGrid := 0, 0
	for name, wording := range rows {
		switch name {
		case "Current guardrails", "Guardrails disabled":
			sawBaseline++
			if wording != planDesignWording {
				t.Errorf("%q: wording = %q, want %q (Guardrails nil/disabled -> plan-design cause)", name, wording, planDesignWording)
			}
		default:
			sawGrid++
			if wording != "Below target" {
				t.Errorf("%q: wording = %q, want plain \"Below target\" (MinMonthlySpendingReal == the requested floor -> funding risk, not plan design)", name, wording)
			}
			if strings.Contains(wording, "is below your minimum from year") {
				t.Errorf("%q: wording %q unexpectedly carries the plan-design explanation", name, wording)
			}
		}
	}
	if sawBaseline != 2 {
		t.Errorf("expected exactly 2 baseline rows (current, no-guardrails), got %d", sawBaseline)
	}
	if sawGrid == 0 {
		t.Fatal("fixture invariant broken: expected at least one adequately-floored grid candidate row")
	}
}

func runGuardrailOptimizer(t *testing.T, floor, target, id string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"floor_monthly_real": {floor}, "target_success_pct": {target}, "request_id": {id}}
	req := httptest.NewRequest("POST", "/whatif/guardrails/optimize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleGuardrailOptimizer(w, req)
	if w.Code != 200 {
		t.Fatalf("optimizer status %d: %s", w.Code, w.Body.String())
	}
	return w
}

// TestGuardrailOptimizerPlanFloorNoticeBelow is fixture (a): the plan's own
// No-Go phase plans below the requested floor. The notice must render, name
// the phase and year, and the affected rows' "Below target" wording must
// explain the plan-design cause instead of implying market risk.
func TestGuardrailOptimizerPlanFloorNoticeBelow(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	guardrailPlanFloorFixture(t, rm)

	w := runGuardrailOptimizer(t, "7500", "50", "plan-floor-below")
	body := w.Body.String()

	if !strings.Contains(body, `data-guardrail-plan-floor-notice`) {
		t.Fatal("missing plan-design floor notice")
	}
	for _, want := range []string{
		"Your plan itself schedules living spending down to $5,000.00/mo from year 1 (No-Go)",
		"not because of market risk",
		"Lower your minimum to $5,000.00 or below",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("notice missing %q\nbody:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "Below target — planned spending is below your minimum from year 1") {
		t.Errorf("row wording missing plan-design explanation\nbody:\n%s", body)
	}
}

// TestGuardrailOptimizerPlanFloorNoticeAbove is fixture (b): no phase decline
// below the floor, so no notice and plain "Below target"/"Meets target"
// wording only (no phase-caused explanation text).
func TestGuardrailOptimizerPlanFloorNoticeAbove(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	guardrailFlatFixture(t, rm)

	w := runGuardrailOptimizer(t, "7500", "50", "plan-floor-above")
	body := w.Body.String()

	if strings.Contains(body, `data-guardrail-plan-floor-notice`) {
		t.Fatalf("unexpected plan-design floor notice when the plan never plans below the floor\nbody:\n%s", body)
	}
	if strings.Contains(body, "is below your minimum from year") {
		t.Fatalf("unexpected plan-design row wording\nbody:\n%s", body)
	}
}

// guardrailOptimizerRenderedRow pairs a parsed row's trimmed <th scope="row">
// policy name with the body remaining immediately after that </th>, so a
// caller can locate a SPECIFIC later cell within THIS row only (never a
// different row's cell with the same text).
type guardrailOptimizerRenderedRow struct {
	Name string
	Rest string
}

// guardrailOptimizerRenderedRows parses every result row's policy name and
// following body from a rendered whatif-guardrail-optimizer-results body.
func guardrailOptimizerRenderedRows(t *testing.T, body string) []guardrailOptimizerRenderedRow {
	t.Helper()
	const thOpen = `<th scope="row" class="p-2">`
	var out []guardrailOptimizerRenderedRow
	for pos := 0; ; {
		i := strings.Index(body[pos:], thOpen)
		if i < 0 {
			break
		}
		start := pos + i + len(thOpen)
		closeIdx := strings.Index(body[start:], "</th>")
		if closeIdx < 0 {
			t.Fatalf("unterminated <th scope=\"row\"> at byte %d", start)
		}
		name := strings.TrimSpace(body[start : start+closeIdx])
		out = append(out, guardrailOptimizerRenderedRow{Name: name, Rest: body[start+closeIdx:]})
		pos = start + closeIdx
	}
	if len(out) == 0 {
		t.Fatal("no result rows parsed from body")
	}
	return out
}

// nthCell returns the trimmed content of the n-th (1-based) <td class="p-2">
// cell after this row's </th>, scoped to THIS row (a later row's matching
// <td class="p-2"> text can never be picked up, because callers only ever
// search within row.Rest, not the whole body).
func (row guardrailOptimizerRenderedRow) nthCell(t *testing.T, n int) string {
	t.Helper()
	const tdOpen = `<td class="p-2">`
	rest := row.Rest
	for i := 0; i < n; i++ {
		start := strings.Index(rest, tdOpen)
		if start < 0 {
			t.Fatalf("row %q missing cell %d", row.Name, i+1)
		}
		rest = rest[start+len(tdOpen):]
	}
	end := strings.Index(rest, "</td>")
	if end < 0 {
		t.Fatalf("row %q cell %d unterminated", row.Name, n)
	}
	return strings.TrimSpace(rest[:end])
}

// TestGuardrailOptimizerWorstAnnualCutSaturationExplained is GV3 item 2: a
// PER-ROW check (anchored on each row's own <th scope="row"> name, then
// scoped to that row's OWN Worst-annual-cut <td>) that the saturation
// explanation is appended if and only if that row's DISPLAYED cut reads
// exactly "100.00%". Includes a 99.995 row: the underlying float rounds up
// to "100.00%" under Go's %.2f (verified separately: the nearest float64 to
// 99.995 rounds up), so the explanation must follow the DISPLAYED string,
// not a raw >=100 comparison on the float. Anchoring per-row means neither
// moving the explanation to a different row NOR narrowing the negative
// row's search window can pass this test, unlike a whole-body substring
// check.
func TestGuardrailOptimizerWorstAnnualCutSaturationExplained(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	result := &models.GuardrailOptimizerResult{}
	rowsIn := []guardrailOptimizerRow{
		{Candidate: models.GuardrailOptimizerCandidate{ID: "saturated", Metrics: models.GuardrailOptimizerMetrics{P95WorstAnnualCutPct: 100.0}}},
		{Candidate: models.GuardrailOptimizerCandidate{ID: "not-saturated", Metrics: models.GuardrailOptimizerMetrics{P95WorstAnnualCutPct: 99.99}}},
		{Candidate: models.GuardrailOptimizerCandidate{ID: "rounds-up", Metrics: models.GuardrailOptimizerMetrics{P95WorstAnnualCutPct: 99.995}}},
	}
	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-guardrail-optimizer-results", map[string]any{"Optimizer": result, "Rows": rowsIn, "Target": "95", "RequestID": "test"}); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	const explanation = "no funded living spending for a full year in at least 5% of futures"

	want := map[string]struct {
		cutPrefix string
		explained bool
	}{
		"Alternative saturated":     {"100.00%", true},
		"Alternative not-saturated": {"99.99%", false},
		"Alternative rounds-up":     {"100.00%", true},
	}
	seen := map[string]bool{}
	for _, row := range guardrailOptimizerRenderedRows(t, body) {
		spec, ok := want[row.Name]
		if !ok {
			continue
		}
		seen[row.Name] = true
		cell := row.nthCell(t, 3)
		if !strings.HasPrefix(cell, spec.cutPrefix) {
			t.Errorf("%q: cut cell = %q, want prefix %q", row.Name, cell, spec.cutPrefix)
		}
		if got := strings.Contains(cell, explanation); got != spec.explained {
			t.Errorf("%q: cut cell explanation present = %v, want %v; cell = %q", row.Name, got, spec.explained, cell)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("row %q not found in rendered body", name)
		}
	}
}

// TestHandleWhatIfGuardrailPlanFloorHint_Present is GV3 item 1's end-to-end
// wiring test: with a phase-decline fixture (reusing guardrailPlanFloorFixture,
// whose No-Go phase plans 5000/mo in year 1), the full /whatif page must
// render the floor-field hint under #guardrail-optimizer-floor with the
// exact figure/year/phase, and the field's aria-describedby must reference
// the hint paragraph's id.
func TestHandleWhatIfGuardrailPlanFloorHint_Present(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	guardrailPlanFloorFixture(t, rm)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()

	want := "Your plan's lowest planned living spending is $5,000.00/mo in today's dollars (No-Go, year 1). A minimum above that is missed by design in every future without an absolute floor."
	if !strings.Contains(body, want) {
		t.Errorf("missing floor-field hint %q", want)
	}
	inputIdx := strings.Index(body, `id="guardrail-optimizer-floor"`)
	if inputIdx < 0 {
		t.Fatal("optimizer floor input not found")
	}
	inputTagEnd := strings.Index(body[inputIdx:], ">")
	if inputTagEnd < 0 {
		t.Fatal("could not find end of floor input tag")
	}
	inputTag := body[inputIdx : inputIdx+inputTagEnd]
	if !strings.Contains(inputTag, `aria-describedby="guardrail-optimizer-floor-help guardrail-optimizer-status guardrail-optimizer-floor-plan-note"`) {
		t.Errorf("floor input aria-describedby does not reference the hint paragraph: %s", inputTag)
	}
	if !strings.Contains(body, `id="guardrail-optimizer-floor-plan-note"`) {
		t.Error("hint paragraph missing its id")
	}
}

// TestHandleWhatIfGuardrailPlanFloorHint_AbsentWithoutValue confirms the
// deferred-wiring contract: when page data carries no GuardrailPlanFloor
// value (today's state for every OTHER page-data site, and this site
// before GV3's handlers.go wiring landed), the hint renders nothing and the floor
// input's aria-describedby does not reference it.
func TestHandleWhatIfGuardrailPlanFloorHint_AbsentWithoutValue(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-guardrail-optimizer", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if strings.Contains(body, "Your plan's lowest planned living spending") {
		t.Error("hint must not render when GuardrailPlanFloor is absent")
	}
	if strings.Contains(body, `id="guardrail-optimizer-floor-plan-note"`) {
		t.Error("hint paragraph must not be present when GuardrailPlanFloor is absent")
	}
	inputIdx := strings.Index(body, `id="guardrail-optimizer-floor"`)
	if inputIdx < 0 {
		t.Fatal("optimizer floor input not found")
	}
	inputTagEnd := strings.Index(body[inputIdx:], ">")
	inputTag := body[inputIdx : inputIdx+inputTagEnd]
	if !strings.Contains(inputTag, `aria-describedby="guardrail-optimizer-floor-help guardrail-optimizer-status"`) {
		t.Errorf("floor input aria-describedby must omit the hint id when absent: %s", inputTag)
	}
}
