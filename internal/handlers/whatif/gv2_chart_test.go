package whatif

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/templates"
)

// gv2ChartClose is a tolerant float comparison for the GV2 chart tests (mirrors
// the equivalent helper the GV2 acceptance oracle uses).
func gv2ChartClose(a, b float64) bool {
	if a == b {
		return true
	}
	return math.Abs(a-b) <= 1e-6*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

// gv2ChartSettings mirrors gv2TriggerFixture in the engine package (kept
// independent per-package so each package's tests stand alone).
func gv2ChartSettings(guardrails *models.GuardrailConfig) *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 500000
	s.MonthlyLivingExpenses = 12000
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 0
	s.InflationRate = 2.5
	s.SpendingDeclineRate = 0
	s.InvestmentReturn = 1.0
	s.ProjectionYears = 8
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: false}
	s.Guardrails = guardrails
	s.IncomeSources = []models.IncomeSource{{ID: "gv2-pension", Name: "Pension", Amount: 30000, Type: models.IncomeDelayed, StartMonth: 36}}
	return s
}

func gv2RunChart(t *testing.T, s *models.WhatIfSettings) *models.ProjectionResult {
	t.Helper()
	in, _, err := buildEngineInput(s)
	if err != nil {
		t.Fatal(err)
	}
	return getEngine().Run(in)
}

func gv2ChartFindTrace(chart map[string]interface{}, name string) map[string]interface{} {
	traces, _ := chart["data"].([]map[string]interface{})
	for _, tr := range traces {
		if n, _ := tr["name"].(string); n == name {
			return tr
		}
	}
	return nil
}

// TestBuildProjectionChartDataGuardrailTriggerTraces is the GV2 criterion-3
// permanent test: with guardrails enabled, the chart carries dashed
// step-shaped "Cut trigger" / "Raise trigger" traces on the balance axis and
// "Planned" / "After guardrails" traces on yaxis y2, in both display modes,
// and the guardrail markers trace sits on the budget panel at the
// After-guardrails value for its month.
func TestBuildProjectionChartDataGuardrailTriggerTraces(t *testing.T) {
	guardrails := &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 10, CeilingRisePct: 20, CeilingRaisePct: 10, MinSpendingPct: 50, MaxSpendingPct: 150}
	s := gv2ChartSettings(guardrails)
	p := gv2RunChart(t, s)
	if len(p.GuardrailEvents) == 0 {
		t.Fatal("fixture defect: expected guardrail events")
	}

	for _, mode := range []string{"nominal", "real"} {
		chart := buildProjectionChartData(s, p, mode)

		cut := gv2ChartFindTrace(chart, "Cut trigger")
		raise := gv2ChartFindTrace(chart, "Raise trigger")
		if cut == nil || raise == nil {
			t.Fatalf("mode %s: missing Cut trigger / Raise trigger traces", mode)
		}
		for _, tr := range []map[string]interface{}{cut, raise} {
			line, _ := tr["line"].(map[string]interface{})
			if line["shape"] != "hv" || line["dash"] != "dash" {
				t.Fatalf("mode %s: %v line=%v, want hv dashed step", mode, tr["name"], line)
			}
			x, _ := tr["x"].([]float64)
			y, _ := tr["y"].([]float64)
			if len(x) != s.ProjectionYears+1 || len(y) != len(x) {
				t.Fatalf("mode %s: %v has %d x / %d y points, want %d (years + final extension point)", mode, tr["name"], len(x), len(y), s.ProjectionYears+1)
			}
		}

		planned := gv2ChartFindTrace(chart, "Planned")
		after := gv2ChartFindTrace(chart, "After guardrails")
		if planned == nil || after == nil {
			t.Fatalf("mode %s: missing Planned / After guardrails budget traces", mode)
		}
		if planned["yaxis"] != "y2" || after["yaxis"] != "y2" {
			t.Fatalf("mode %s: budget traces must be on yaxis y2, got planned=%v after=%v", mode, planned["yaxis"], after["yaxis"])
		}
		afterY, _ := after["y"].([]float64)
		if len(afterY) != len(p.Months) {
			t.Fatalf("mode %s: After guardrails has %d points, want %d months", mode, len(afterY), len(p.Months))
		}

		markers := gv2ChartFindTrace(chart, "Guardrail cuts / raises")
		if markers == nil {
			t.Fatalf("mode %s: missing guardrail markers trace", mode)
		}
		if markers["yaxis"] != "y2" {
			t.Fatalf("mode %s: markers must sit on the budget panel, got yaxis=%v", mode, markers["yaxis"])
		}
		mx, _ := markers["x"].([]float64)
		my, _ := markers["y"].([]float64)
		if len(mx) != len(p.GuardrailEvents) || len(my) != len(mx) {
			t.Fatalf("mode %s: markers %d/%d points, want %d events", mode, len(mx), len(my), len(p.GuardrailEvents))
		}
		for i, e := range p.GuardrailEvents {
			want := afterY[e.Year*12]
			if !gv2ChartClose(my[i], want) {
				t.Fatalf("mode %s: marker %d y=%.6f != After-guardrails value %.6f at month %d", mode, i, my[i], want, e.Year*12)
			}
		}

		layout, _ := chart["layout"].(map[string]interface{})
		if layout["yaxis2"] == nil {
			t.Fatalf("mode %s: layout missing yaxis2 for the budget panel", mode)
		}
	}
}

// TestBuildProjectionChartDataGuardrailDisabledOmitsTraces is the disabled
// side of criterion 3: no trigger/budget/panel traces or layout key when
// guardrails are off, and the markers trace (empty here) is absent too.
func TestBuildProjectionChartDataGuardrailDisabledOmitsTraces(t *testing.T) {
	s := gv2ChartSettings(nil)
	p := gv2RunChart(t, s)
	if len(p.GuardrailEvents) != 0 {
		t.Fatalf("fixture defect: guardrails disabled but got events %+v", p.GuardrailEvents)
	}
	chart := buildProjectionChartData(s, p, "nominal")
	for _, name := range []string{"Cut trigger", "Raise trigger", "Planned", "After guardrails", "Guardrail cuts / raises"} {
		if gv2ChartFindTrace(chart, name) != nil {
			t.Fatalf("guardrails disabled but chart has trace %q", name)
		}
	}
	layout, _ := chart["layout"].(map[string]interface{})
	if layout["yaxis2"] != nil {
		t.Fatal("guardrails disabled but layout has yaxis2")
	}
	// GV2 attempt 2 (ruling GV-2026-09-09d, criterion 2): layout.height is
	// set ONLY when guardrails are enabled — the single-panel chart must
	// restore its pre-GV2 size, not carry the two-panel 520px height.
	if _, ok := layout["height"]; ok {
		t.Fatalf("guardrails disabled but layout has a height key: %v", layout["height"])
	}
}

// TestBuildGuardrailChartSummary is the GV2 criterion-4 permanent test: the
// rendered summary strings equal the SAME arithmetic the Guardrail Events
// list uses (guardrailEventHoverText / whatif-guardrail-events), asserted on
// a fixture with fractional cents so a stray %.0f or an un-rounded float
// would show up as a string mismatch (ruling 2026-08-29b: assert on
// rendered strings, not floats).
func TestBuildGuardrailChartSummary(t *testing.T) {
	s := &models.WhatIfSettings{Guardrails: &models.GuardrailConfig{Enabled: true}}
	projection := &models.ProjectionResult{
		GuardrailEvents: []models.GuardrailEvent{
			{
				Year:                  35,
				Type:                  "cut",
				Multiplier:            0.9,
				PreviousMultiplier:    1.0,
				Portfolio:             1_234_567.891,
				MonthlySpendingBefore: 13821.494999,
				MonthlySpendingAfter:  12439.345001,
			},
		},
		YearlySummaries: []models.ProjectionYearSummary{
			{Year: 0},
			{
				Year:                  35,
				GuardrailCutTrigger:   1_720_000.4949,
				GuardrailRaiseTrigger: 2_880_000.5051,
				CumulativeInflation:   1.333333,
			},
		},
	}

	got := buildGuardrailChartSummary(s, projection)
	if got == nil {
		t.Fatal("expected a non-nil summary with guardrails enabled")
	}

	wantEventsLine := fmt.Sprintf("1 cut (year 35: -10%%, %s/mo → %s/mo).",
		templates.FormatMoney(13821.494999), templates.FormatMoney(12439.345001))
	if got.EventsLine != wantEventsLine {
		t.Fatalf("EventsLine =\n%q\nwant\n%q", got.EventsLine, wantEventsLine)
	}

	last := projection.YearlySummaries[len(projection.YearlySummaries)-1]
	wantNextLine := fmt.Sprintf("Next cut if the balance falls below %s; next raise if it rises above %s (future dollars; today: %s / %s).",
		templates.FormatMoney(last.GuardrailCutTrigger), templates.FormatMoney(last.GuardrailRaiseTrigger),
		templates.FormatMoney(last.GuardrailCutTrigger/last.CumulativeInflation), templates.FormatMoney(last.GuardrailRaiseTrigger/last.CumulativeInflation))
	if got.NextLine != wantNextLine {
		t.Fatalf("NextLine =\n%q\nwant\n%q", got.NextLine, wantNextLine)
	}

	// Cross-check the event's percentage/money portion against the exact
	// arithmetic the Guardrail Events list itself uses (guardrails.html's
	// "whatif-guardrail-events" block: pct = |Multiplier-PreviousMultiplier|/
	// PreviousMultiplier*100, printf "%.0f"; money via formatMoney).
	e := projection.GuardrailEvents[0]
	listPct := (e.PreviousMultiplier - e.Multiplier) / e.PreviousMultiplier * 100
	listWant := fmt.Sprintf("year %d: -%.0f%%, %s/mo → %s/mo", e.Year, listPct, templates.FormatMoney(e.MonthlySpendingBefore), templates.FormatMoney(e.MonthlySpendingAfter))
	wantEventsLine2 := fmt.Sprintf("1 cut (%s).", listWant)
	if got.EventsLine != wantEventsLine2 {
		t.Fatalf("EventsLine does not match the events-list arithmetic:\n%q\nwant\n%q", got.EventsLine, wantEventsLine2)
	}
}

// TestBuildGuardrailChartSummaryNoEvents covers the enabled-but-quiet case.
func TestBuildGuardrailChartSummaryNoEvents(t *testing.T) {
	s := &models.WhatIfSettings{Guardrails: &models.GuardrailConfig{Enabled: true}}
	projection := &models.ProjectionResult{
		YearlySummaries: []models.ProjectionYearSummary{
			{Year: 0, GuardrailCutTrigger: 400000, GuardrailRaiseTrigger: 600000, CumulativeInflation: 1},
		},
	}
	got := buildGuardrailChartSummary(s, projection)
	if got == nil {
		t.Fatal("expected non-nil summary")
	}
	if got.EventsLine != "no cuts or raises in this projection." {
		t.Fatalf("EventsLine = %q", got.EventsLine)
	}
}

// TestBuildGuardrailChartSummaryNilWhenDisabled covers criterion 4's "hidden
// entirely when guardrails are disabled" requirement.
func TestBuildGuardrailChartSummaryNilWhenDisabled(t *testing.T) {
	projection := &models.ProjectionResult{YearlySummaries: []models.ProjectionYearSummary{{Year: 0}}}
	cases := []*models.WhatIfSettings{
		nil,
		{Guardrails: nil},
		{Guardrails: &models.GuardrailConfig{Enabled: false}},
	}
	for i, s := range cases {
		if got := buildGuardrailChartSummary(s, projection); got != nil {
			t.Fatalf("case %d: expected nil summary, got %+v", i, got)
		}
	}
	if got := buildGuardrailChartSummary(&models.WhatIfSettings{Guardrails: &models.GuardrailConfig{Enabled: true}}, nil); got != nil {
		t.Fatal("expected nil summary for nil projection")
	}
	empty := &models.ProjectionResult{}
	if got := buildGuardrailChartSummary(&models.WhatIfSettings{Guardrails: &models.GuardrailConfig{Enabled: true}}, empty); got != nil {
		t.Fatal("expected nil summary when there are no yearly summaries")
	}
}

// TestBuildProjectionChartDataGuardrailExactValues is the GV2 attempt-2
// mutation-catching permanent test. Unlike
// TestBuildProjectionChartDataGuardrailTriggerTraces (structure only — trace
// names, point counts, axis assignment), this asserts the ACTUAL numbers and
// hover strings buildProjectionChartData emits, in both display modes, on a
// fixture with InflationRate > 0 and >= 1 guardrail event. It exists because
// two mutations survived the whole permanent suite while only the ephemeral
// GV2 oracle caught them: the real-mode CPI index off by one month
// ((i+1)*12-1 -> (i+1)*12), and the "Planned" trace reading
// AdjustedLivingExpenses instead of PlannedLivingExpenses.
func TestBuildProjectionChartDataGuardrailExactValues(t *testing.T) {
	guardrails := &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 10, CeilingRisePct: 20, CeilingRaisePct: 10, MinSpendingPct: 50, MaxSpendingPct: 150}
	s := gv2ChartSettings(guardrails)
	if s.InflationRate <= 0 {
		t.Fatal("fixture defect: need InflationRate > 0 to exercise the real-mode CPI divisor")
	}
	p := gv2RunChart(t, s)
	if len(p.GuardrailEvents) == 0 {
		t.Fatal("fixture defect: expected >=1 guardrail event")
	}
	lastMonthIdx := len(p.Months) - 1

	for _, mode := range []string{"nominal", "real"} {
		chart := buildProjectionChartData(s, p, mode)

		// (1) Cut trigger / Raise trigger: exact y per year, x==i, trailing
		// point repeats the last y at the final month's Year.
		for _, tc := range []struct {
			name    string
			trigger func(models.ProjectionYearSummary) float64
			verb    string
			cmp     string
		}{
			{"Cut trigger", func(ys models.ProjectionYearSummary) float64 { return ys.GuardrailCutTrigger }, "Cut", "≤"},
			{"Raise trigger", func(ys models.ProjectionYearSummary) float64 { return ys.GuardrailRaiseTrigger }, "Raise", "≥"},
		} {
			tr := gv2ChartFindTrace(chart, tc.name)
			if tr == nil {
				t.Fatalf("mode %s: missing trace %q", mode, tc.name)
			}
			x, _ := tr["x"].([]float64)
			y, _ := tr["y"].([]float64)
			text, _ := tr["text"].([]string)
			n := len(p.YearlySummaries)
			if len(x) != n+1 || len(y) != n+1 || len(text) != n+1 {
				t.Fatalf("mode %s: %q has %d x / %d y / %d text points, want %d", mode, tc.name, len(x), len(y), len(text), n+1)
			}
			for i, ys := range p.YearlySummaries {
				if x[i] != float64(i) {
					t.Fatalf("mode %s: %q x[%d]=%v, want %d", mode, tc.name, i, x[i], i)
				}
				divisor := 1.0
				if mode == "real" {
					idx := (i+1)*12 - 1
					if idx > lastMonthIdx {
						idx = lastMonthIdx
					}
					divisor = p.Months[idx].CumulativeInflation
				}
				want := tc.trigger(ys) / divisor
				if !gv2ChartClose(y[i], want) {
					t.Fatalf("mode %s: %q y[%d]=%.6f, want %.6f (mutation i: off-by-one CPI index would read month %d instead of %d)", mode, tc.name, i, y[i], want, (i+1)*12, (i+1)*12-1)
				}
				// (3) Hover text exact wording for the first and last point.
				if i == 0 || i == n-1 {
					wantText := fmt.Sprintf("%s if balance %s %s at the year %d check", tc.verb, tc.cmp, templates.FormatMoney(want), i+1)
					if text[i] != wantText {
						t.Fatalf("mode %s: %q text[%d] = %q, want %q", mode, tc.name, i, text[i], wantText)
					}
				}
			}
			// Trailing extension point: repeats the last year's y, x is the
			// final month's Year.
			if x[n] != p.Months[lastMonthIdx].Year {
				t.Fatalf("mode %s: %q trailing x=%v, want final month Year %v", mode, tc.name, x[n], p.Months[lastMonthIdx].Year)
			}
			if !gv2ChartClose(y[n], y[n-1]) {
				t.Fatalf("mode %s: %q trailing y=%.6f, want repeat of y[%d]=%.6f", mode, tc.name, y[n], n-1, y[n-1])
			}
		}

		// (2) Planned / After guardrails: exact y per month against the
		// month's OWN field — mutation (ii) (Planned reading
		// AdjustedLivingExpenses) is caught here because the two traces
		// would then be numerically identical whenever a guardrail
		// multiplier is not 1.0, which this fixture's cut/raise events
		// guarantee for at least one month.
		for _, tc := range []struct {
			name  string
			field func(models.ProjectionMonth) float64
		}{
			{"Planned", func(m models.ProjectionMonth) float64 { return m.PlannedLivingExpenses }},
			{"After guardrails", func(m models.ProjectionMonth) float64 { return m.AdjustedLivingExpenses }},
		} {
			tr := gv2ChartFindTrace(chart, tc.name)
			if tr == nil {
				t.Fatalf("mode %s: missing trace %q", mode, tc.name)
			}
			x, _ := tr["x"].([]float64)
			y, _ := tr["y"].([]float64)
			if len(x) != len(p.Months) || len(y) != len(p.Months) {
				t.Fatalf("mode %s: %q has %d x / %d y points, want %d months", mode, tc.name, len(x), len(y), len(p.Months))
			}
			for i, m := range p.Months {
				divisor := 1.0
				if mode == "real" {
					divisor = m.CumulativeInflation
				}
				want := tc.field(m) / divisor
				if x[i] != m.Year {
					t.Fatalf("mode %s: %q x[%d]=%v, want month Year %v", mode, tc.name, i, x[i], m.Year)
				}
				if !gv2ChartClose(y[i], want) {
					t.Fatalf("mode %s: %q y[%d]=%.6f, want %.6f (mutation ii: field swap between Planned/AdjustedLivingExpenses would fail here)", mode, tc.name, i, y[i], want)
				}
			}
		}

		// Sanity: Planned and After-guardrails must actually DIFFER at the
		// events' months — otherwise mutation (ii) (both traces reading the
		// same field) would pass the per-point checks above vacuously.
		plannedTr := gv2ChartFindTrace(chart, "Planned")
		afterTr := gv2ChartFindTrace(chart, "After guardrails")
		plannedY, _ := plannedTr["y"].([]float64)
		afterY, _ := afterTr["y"].([]float64)
		differed := false
		for i := range plannedY {
			if !gv2ChartClose(plannedY[i], afterY[i]) {
				differed = true
				break
			}
		}
		if !differed {
			t.Fatalf("mode %s: Planned and After guardrails never differ — fixture defect or the two traces are reading the same field", mode)
		}
	}
}

// TestGuardrailFieldsOmittedFromJSONWhenDisabled is the GV2 attempt-2
// permanent test for criterion 4: with guardrails disabled, marshaling a
// year summary must contain none of the four guardrail_* JSON keys (proves
// the `omitempty` struct tags on models.ProjectionYearSummary, so a
// strip-omitempty mutation on any of the four fields is caught here).
func TestGuardrailFieldsOmittedFromJSONWhenDisabled(t *testing.T) {
	s := gv2ChartSettings(nil)
	p := gv2RunChart(t, s)
	if len(p.YearlySummaries) == 0 {
		t.Fatal("fixture defect: no yearly summaries")
	}
	for _, ys := range p.YearlySummaries {
		raw, err := json.Marshal(ys)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{`"guardrail_peak"`, `"guardrail_baseline"`, `"guardrail_cut_trigger"`, `"guardrail_raise_trigger"`} {
			if strings.Contains(string(raw), key) {
				t.Fatalf("guardrails disabled but year %d JSON contains %s: %s", ys.Year, key, raw)
			}
		}
	}
}

// --- GV2 attempt 2 (ruling GV-2026-09-09d): trace tones, contrast, and
// conditional chart height. ---

// srgbToLinear and relativeLuminance implement the WCAG 2.x sRGB relative
// luminance formula (the same math the checker's manual contrast walk used)
// so the assertions below prove the ACTUAL emitted colors clear the 3:1
// floor numerically, rather than merely matching a hard-coded hex string.
func srgbToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func relativeLuminance(hex string) float64 {
	hex = strings.TrimPrefix(hex, "#")
	r, _ := strconv.ParseInt(hex[0:2], 16, 0)
	g, _ := strconv.ParseInt(hex[2:4], 16, 0)
	b, _ := strconv.ParseInt(hex[4:6], 16, 0)
	R := srgbToLinear(float64(r) / 255)
	G := srgbToLinear(float64(g) / 255)
	B := srgbToLinear(float64(b) / 255)
	return 0.2126*R + 0.7152*G + 0.0722*B
}

// wcagContrastRatio is the WCAG 2.x contrast-ratio formula between two hex
// colors, lighter-over-darker.
func wcagContrastRatio(hex1, hex2 string) float64 {
	l1, l2 := relativeLuminance(hex1), relativeLuminance(hex2)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

const gv2CardBackgroundLight = "#ffffff"

// gv2AssertToneColorContrast asserts a single hex color clears the ACCESSIBILITY.md
// A-3 / WCAG 1.4.11 3:1 floor for meaning-carrying graphics against the light-theme
// card background, using the numeric WCAG formula (not a literal-hex-only check).
func gv2AssertToneColorContrast(t *testing.T, label, hex string) {
	t.Helper()
	ratio := wcagContrastRatio(hex, gv2CardBackgroundLight)
	if ratio < 3.0 {
		t.Fatalf("%s color %s contrast vs %s = %.2f:1, want >= 3:1", label, hex, gv2CardBackgroundLight, ratio)
	}
}

// TestBuildProjectionChartDataTraceTonesAndContrast is the GV2 attempt-2
// permanent test for criterion 1: with guardrails enabled, in both display
// modes, every relocated/new trace carries the meta tone the client keys its
// theme palette on, and the light-theme default color(s) the server emits
// clear the 3:1 floor against the white card background.
func TestBuildProjectionChartDataTraceTonesAndContrast(t *testing.T) {
	guardrails := &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 10, CeilingRisePct: 20, CeilingRaisePct: 10, MinSpendingPct: 50, MaxSpendingPct: 150}
	s := gv2ChartSettings(guardrails)
	p := gv2RunChart(t, s)
	if len(p.GuardrailEvents) == 0 {
		t.Fatal("fixture defect: expected guardrail events")
	}

	wantTone := map[string]string{
		"Cut trigger":      "negative",
		"Raise trigger":    "positive",
		"Planned":          "planned",
		"After guardrails": "after",
	}

	for _, mode := range []string{"nominal", "real"} {
		chart := buildProjectionChartData(s, p, mode)

		for name, tone := range wantTone {
			tr := gv2ChartFindTrace(chart, name)
			if tr == nil {
				t.Fatalf("mode %s: missing trace %q", mode, name)
			}
			meta, _ := tr["meta"].(map[string]interface{})
			if meta == nil || meta["tone"] != tone {
				t.Fatalf("mode %s: trace %q meta.tone = %v, want %q", mode, name, meta, tone)
			}
			line, _ := tr["line"].(map[string]interface{})
			hex, _ := line["color"].(string)
			if hex == "" {
				t.Fatalf("mode %s: trace %q has no line.color", mode, name)
			}
			gv2AssertToneColorContrast(t, name, hex)
		}

		markers := gv2ChartFindTrace(chart, "Guardrail cuts / raises")
		if markers == nil {
			t.Fatalf("mode %s: missing guardrail markers trace", mode)
		}
		meta, _ := markers["meta"].(map[string]interface{})
		tones, _ := meta["tones"].([]string)
		if len(tones) != len(p.GuardrailEvents) {
			t.Fatalf("mode %s: markers meta.tones has %d entries, want %d (one per GuardrailEvents, same order)", mode, len(tones), len(p.GuardrailEvents))
		}
		marker, _ := markers["marker"].(map[string]interface{})
		colors, _ := marker["color"].([]string)
		if len(colors) != len(tones) {
			t.Fatalf("mode %s: marker.color has %d entries, want %d", mode, len(colors), len(tones))
		}
		for i, ge := range p.GuardrailEvents {
			wantEventTone := "positive"
			if ge.Type == "cut" {
				wantEventTone = "negative"
			}
			if tones[i] != wantEventTone {
				t.Fatalf("mode %s: marker %d (event type %q) tone = %q, want %q", mode, i, ge.Type, tones[i], wantEventTone)
			}
			gv2AssertToneColorContrast(t, fmt.Sprintf("marker %d", i), colors[i])
		}

		layout, _ := chart["layout"].(map[string]interface{})
		if layout["height"] != 520 {
			t.Fatalf("mode %s: guardrails enabled but layout.height = %v, want 520", mode, layout["height"])
		}
	}
}

// TestProjectionChartTemplateChartContainerTall is the GV2 attempt-2
// permanent test for criterion 2's template half: the chart container gets
// chart-container-tall (and the matching min-height CSS) ONLY when
// guardrails are enabled.
func TestProjectionChartTemplateChartContainerTall(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	guardrails := &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 10, CeilingRisePct: 20, CeilingRaisePct: 10, MinSpendingPct: 50, MaxSpendingPct: 150}
	for _, tc := range []struct {
		name       string
		guardrails *models.GuardrailConfig
		wantTall   bool
	}{
		{"enabled", guardrails, true},
		{"disabled", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := gv2ChartSettings(tc.guardrails)
			p := gv2RunChart(t, s)
			out, err := renderer.RenderToString("whatif-projection-chart", map[string]any{
				"Settings": s,
				"Analysis": &models.WhatIfAnalysis{Projection: p},
			})
			if err != nil {
				t.Fatalf("RenderToString: %v", err)
			}
			hasTall := strings.Contains(out, "chart-container-tall")
			if hasTall != tc.wantTall {
				t.Fatalf("guardrails=%v: chart-container-tall present=%v, want %v", tc.guardrails != nil, hasTall, tc.wantTall)
			}
		})
	}
}
