# What-If Page UI Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorganize the what-if page results into a 5-tab workspace topped by an always-visible sticky verdict bar, with grouped/collapsible settings and a monospace-numeral visual polish pass — no projection-engine changes.

**Architecture:** A new Go view-model (`BuildVerdict`) precomputes the verdict bar's headline/health/figures from existing `WhatIfAnalysis` data, so templates stay logic-light and the verdict is unit-testable. Results sections (today stacked) move unchanged into client-side show/hide tab panels; because every panel stays in the DOM, existing HTMX OOB swaps and the full `#whatif-results` re-render keep working untouched. A small `whatif-tabs.js` owns tab switching, per-scenario `localStorage` persistence, settings-card collapse, and re-sizing Plotly charts when a hidden tab becomes visible.

**Tech Stack:** Go `html/template`, HTMX, Tailwind (CDN), Plotly.js, vanilla JS. Tests: Go `testing` with the existing `RenderToString` / `setupTestEnvWithRenderer` render-test harness.

**Spec:** `docs/superpowers/specs/2026-06-12-whatif-ui-redesign-design.md`

---

## Key facts established during research (read before starting)

- **Page handler:** `internal/handlers/whatif/handlers.go`
  - `handleWhatIf` builds `pageData` (line ~606) and calls `renderer.Render(w, "base", pageData)`.
  - `handleWhatIfCalculate` builds `partialData` (line ~639) and calls `renderer.RenderPartial(w, "whatif-results", partialData)`.
  - Both maps already carry `"Settings"` (`*models.WhatIfSettings`) and `"Analysis"` (`*models.WhatIfAnalysis`).
- **Page template:** `web/templates/pages/whatif.html`. `{{define "whatif-content"}}` (settings column + results column) and `{{define "whatif-results"}}` (line 107; OOB `<template>` blocks + the 13 result-section includes at lines 224-236).
- **Base layout:** `web/templates/layouts/base.html`. Dispatches content by `ActiveTab` (line 131: `{{template "whatif-content" .}}`). Loads Tailwind CDN (line 18), `styles.css` (line 40), `htmx.min.js` (line 12), `plotly.min.js` (line 15), `charts.js` (line 158), `dashboard.js` (line 162). Dark mode is a `dark` class on `<html>` toggled by existing code (a `themechange` event is dispatched).
- **Charts:** `web/static/js/charts.js`. On `htmx:afterSettle` for `#whatif-results` it runs `initWhatIfProjectionCards(target)` + `loadAllCharts(target)` (lines 464-470). Chart containers have ids `chart-*`; Plotly stores data on the element. Plotly cannot size a chart inside `display:none` — must `Plotly.Plots.resize(el)` once visible.
- **Template funcs:** registered in `internal/templates/render.go` (`getFuncMap`). Existing helpers usable in templates: `formatMoney`, `formatNumber`, `dict`, `deref`, `percentOf`, `successRateTextClass`, `successRateBarClass`, `socialSecurityProjectionActive`.
- **Analysis fields (verbatim, `internal/models/whatif.go`):**
  - `WhatIfAnalysis.Projection *ProjectionResult`, `.BudgetFit *BudgetFitAnalysis`, `.MonteCarlo *MonteCarloAnalysis`, `.Tax *TaxAnalysis`, `.Sustainability *SustainabilityScore`.
  - `ProjectionResult{ Survives bool; DepletionMonth *int; LongevityYears *float64; FinalBalance float64 }`.
  - `BudgetFitAnalysis{ MonthlyGap float64; RequiredRate float64; ... }` (`MonthlyGap > 0` = shortfall).
  - `MonteCarloAnalysis.Stats *MonteCarloStats`; `MonteCarloStats.SuccessRate float64` (already 0-100 scale).
  - `TaxAnalysis.TotalTaxPaid float64`.
  - `WhatIfSettings.ProjectionYears int`, `.StartDate string` (parse with `models.ParseYearMonth(s.StartDate) (time.Time, error)`).
- **Render-test harness:** `internal/handlers/whatif/*_render_test.go`. Pattern: `_, cleanup := setupTestEnvWithRenderer(t); defer cleanup()`, then `renderer.RenderToString("<define-name>", map[string]any{...})`, assert with `strings.Contains`. `truncate(s, n)` helper exists in `gross_withdrawal_render_test.go`.

### Tab → section mapping (target)

| Tab | `{{define}}` templates moved into the panel |
|-----|---------------------------------------------|
| **Overview** | `whatif-completeness`, new verdict-hero context (verdict bar is separate/sticky), new KPI tiles, `whatif-projection-chart`, top-alerts block |
| **Cash Flow** | `whatif-budget-analysis`, `whatif-present-value`, `whatif-income-chart`, `whatif-projection-breakdown` |
| **Risk** | `whatif-sensitivity`, `whatif-failure-points`, `whatif-monte-carlo`, `whatif-historical-backtest`, `whatif-guardrail-events` |
| **Taxes & RMD** | `whatif-rmd` |
| **Strategies** | `whatif-social-security-results`, `whatif-tax-optimizer-results` |

(Projection chart appears on Overview only — single canvas, no duplicate Plotly instances.)

### Settings → group mapping (target)

| Group label | Cards (existing template includes) |
|-------------|------------------------------------|
| **Money In / Out** | `whatif-portfolio-settings`, `whatif-income-card`, `whatif-healthcare-card`, `whatif-bigticket-card`, `whatif-expense-card` |
| **Assumptions** | `whatif-rate-assumptions`, `whatif-spending-phases` |
| **Strategies** | `whatif-guardrails`, `whatif-roth-conversion`, `whatif-social-security-config`, `whatif-scenario-chain` |

---

## Task 1: Verdict view-model (Go)

**Files:**
- Create: `internal/handlers/whatif/verdict.go`
- Test: `internal/handlers/whatif/verdict_test.go`

- [ ] **Step 1: Write the failing test**

```go
package whatif

import (
	"testing"

	"budget2/internal/models"
)

func intPtr(i int) *int { return &i }

func TestBuildVerdict(t *testing.T) {
	t.Run("funded full horizon with strong MC is green", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: true, FinalBalance: 410250},
			BudgetFit:  &models.BudgetFitAnalysis{MonthlyGap: -200, RequiredRate: 0},
			MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 85}},
		}
		v := BuildVerdict(a, s)
		if v.Health != VerdictGreen {
			t.Errorf("Health = %q, want green", v.Health)
		}
		if v.Headline != "Funded through 2064" {
			t.Errorf("Headline = %q, want \"Funded through 2064\"", v.Headline)
		}
		if v.GapIsShortfall {
			t.Errorf("GapIsShortfall = true, want false (surplus)")
		}
		if !v.HasMonteCarlo || v.SuccessRate != 85 {
			t.Errorf("MC = (%v,%v), want (true,85)", v.HasMonteCarlo, v.SuccessRate)
		}
	})

	t.Run("funded but weak MC is amber", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 30, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: true},
			BudgetFit:  &models.BudgetFitAnalysis{MonthlyGap: 100},
			MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 55}},
		}
		if v := BuildVerdict(a, s); v.Health != VerdictAmber {
			t.Errorf("Health = %q, want amber", v.Health)
		}
	})

	t.Run("early depletion is red with depletion-year headline", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: false, DepletionMonth: intPtr(72)}, // 6 years
			BudgetFit:  &models.BudgetFitAnalysis{MonthlyGap: 1601, RequiredRate: 3.1},
			MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 12}},
		}
		v := BuildVerdict(a, s)
		if v.Health != VerdictRed {
			t.Errorf("Health = %q, want red", v.Health)
		}
		if v.Headline != "Funds run out in 2032" {
			t.Errorf("Headline = %q, want \"Funds run out in 2032\"", v.Headline)
		}
		if v.YearsCovered != 6 {
			t.Errorf("YearsCovered = %d, want 6", v.YearsCovered)
		}
		if !v.GapIsShortfall {
			t.Errorf("GapIsShortfall = false, want true")
		}
	})

	t.Run("late depletion is amber", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: false, DepletionMonth: intPtr(300)}, // 25 years
			BudgetFit:  &models.BudgetFitAnalysis{MonthlyGap: 500},
			MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 60}},
		}
		if v := BuildVerdict(a, s); v.Health != VerdictAmber {
			t.Errorf("Health = %q, want amber", v.Health)
		}
	})

	t.Run("nil MonteCarlo degrades gracefully", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 20, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: true},
			BudgetFit:  &models.BudgetFitAnalysis{MonthlyGap: -50},
		}
		v := BuildVerdict(a, s)
		if v.HasMonteCarlo {
			t.Errorf("HasMonteCarlo = true, want false")
		}
		if v.Health != VerdictGreen {
			t.Errorf("Health = %q, want green (survives, no MC)", v.Health)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/whatif/ -run TestBuildVerdict -v`
Expected: FAIL — `undefined: BuildVerdict`, `undefined: VerdictGreen`, etc.

- [ ] **Step 3: Write minimal implementation**

```go
package whatif

import (
	"fmt"

	"budget2/internal/models"
)

// VerdictHealth classifies the overall plan outcome for the verdict bar tint.
type VerdictHealth string

const (
	VerdictGreen VerdictHealth = "green"
	VerdictAmber VerdictHealth = "amber"
	VerdictRed   VerdictHealth = "red"
)

// mcStrongThreshold is the Monte Carlo success rate (0-100) at or above which a
// fully-funded plan is considered green rather than amber.
const mcStrongThreshold = 70.0

// earlyDepletionYears: depleting within this many years is "red", later is "amber".
const earlyDepletionYears = 10

// VerdictView is the precomputed model the sticky verdict bar renders.
type VerdictView struct {
	Health         VerdictHealth
	Headline       string  // e.g. "Funded through 2064" / "Funds run out in 2032"
	Detail         string  // e.g. "spending covered for all 38 years"
	YearsCovered   int     // full horizon if survives, else years to depletion
	MonthlyGap     float64 // BudgetFit.MonthlyGap (>0 = shortfall)
	GapIsShortfall bool
	RequiredRate   float64
	SuccessRate    float64 // 0-100
	HasMonteCarlo  bool
}

// BuildVerdict derives the verdict bar model from analysis already computed by
// the engine. It performs no projection math of its own.
func BuildVerdict(a *models.WhatIfAnalysis, s *models.WhatIfSettings) VerdictView {
	v := VerdictView{}
	if a == nil || s == nil {
		return v
	}

	startYear := 0
	if t, err := models.ParseYearMonth(s.StartDate); err == nil {
		startYear = t.Year()
	}

	if a.BudgetFit != nil {
		v.MonthlyGap = a.BudgetFit.MonthlyGap
		v.GapIsShortfall = a.BudgetFit.MonthlyGap > 0
		v.RequiredRate = a.BudgetFit.RequiredRate
	}
	if a.MonteCarlo != nil && a.MonteCarlo.Stats != nil {
		v.HasMonteCarlo = true
		v.SuccessRate = a.MonteCarlo.Stats.SuccessRate
	}

	survives := a.Projection != nil && a.Projection.Survives
	if survives {
		v.YearsCovered = s.ProjectionYears
		v.Headline = fmt.Sprintf("Funded through %d", startYear+s.ProjectionYears)
		v.Detail = fmt.Sprintf("spending covered for all %d years", s.ProjectionYears)
		if !v.HasMonteCarlo || v.SuccessRate >= mcStrongThreshold {
			v.Health = VerdictGreen
		} else {
			v.Health = VerdictAmber
		}
		return v
	}

	// Depletes within the horizon.
	depletionYears := s.ProjectionYears
	if a.Projection != nil && a.Projection.DepletionMonth != nil {
		depletionYears = *a.Projection.DepletionMonth / 12
	}
	v.YearsCovered = depletionYears
	v.Headline = fmt.Sprintf("Funds run out in %d", startYear+depletionYears)
	v.Detail = fmt.Sprintf("covered for %d of %d years", depletionYears, s.ProjectionYears)
	if depletionYears < earlyDepletionYears {
		v.Health = VerdictRed
	} else {
		v.Health = VerdictAmber
	}
	return v
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/handlers/whatif/ -run TestBuildVerdict -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/whatif/verdict.go internal/handlers/whatif/verdict_test.go
git commit -m "feat(whatif): add verdict view-model for plan-health summary bar"
```

---

## Task 2: Wire Verdict into handler data maps

**Files:**
- Modify: `internal/handlers/whatif/handlers.go` (`handleWhatIf` ~line 606, `handleWhatIfCalculate` ~line 639)

- [ ] **Step 1: Add `Verdict` to `handleWhatIf` pageData**

In `handleWhatIf`, after `analysis` is obtained and before building `pageData`, the map already has `"Settings": settings` and `"Analysis": analysis`. Add a `"Verdict"` key. Edit the `pageData` literal to include:

```go
	pageData := map[string]interface{}{
		"Title":          "What-If Analysis",
		"ActiveTab":      "whatif",
		"Settings":       settings,
		"Analysis":       analysis,
		"Verdict":        BuildVerdict(analysis, settings),
		"Scenarios":      scenarios,
		"ActiveScenario": activeScenario,
		"ActiveFilename": activeFilename,
		"Findings":       findings,
	}
```

- [ ] **Step 2: Add `Verdict` to `handleWhatIfCalculate` partialData**

```go
	partialData := map[string]interface{}{
		"Settings": settings,
		"Analysis": analysis,
		"Verdict":  BuildVerdict(analysis, settings),
		"Findings": completeness.Check(settings),
	}
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: success (no template renders `Verdict` yet; that is Task 3).

- [ ] **Step 4: Commit**

```bash
git add internal/handlers/whatif/handlers.go
git commit -m "feat(whatif): pass verdict model to page and results-partial renders"
```

---

## Task 3: Verdict bar template + render test

**Files:**
- Create: `web/templates/components/whatif/verdict-bar.html`
- Test: `internal/handlers/whatif/verdict_render_test.go`

- [ ] **Step 1: Write the failing render test**

```go
package whatif

import (
	"strings"
	"testing"
)

func TestVerdictBar_Render(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	t.Run("green funded plan shows headline and figures", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-verdict-bar", map[string]any{
			"Verdict": VerdictView{
				Health: VerdictGreen, Headline: "Funded through 2064",
				Detail: "spending covered for all 38 years",
				MonthlyGap: -200, GapIsShortfall: false,
				RequiredRate: 0, SuccessRate: 85, HasMonteCarlo: true,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"Funded through 2064", "spending covered for all 38 years", "85.0%", "verdict-green"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, truncate(out, 600))
			}
		}
	})

	t.Run("red plan shows shortfall styling and run-out headline", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-verdict-bar", map[string]any{
			"Verdict": VerdictView{
				Health: VerdictRed, Headline: "Funds run out in 2032",
				Detail: "covered for 6 of 38 years",
				MonthlyGap: 1601.38, GapIsShortfall: true,
				RequiredRate: 3.1, SuccessRate: 12, HasMonteCarlo: true,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"Funds run out in 2032", "verdict-red", "1,601"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, truncate(out, 600))
			}
		}
	})

	t.Run("no monte carlo hides the MC figure", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-verdict-bar", map[string]any{
			"Verdict": VerdictView{
				Health: VerdictGreen, Headline: "Funded through 2046",
				Detail: "spending covered for all 20 years", HasMonteCarlo: false,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if strings.Contains(out, "Monte Carlo") {
			t.Errorf("did not expect Monte Carlo figure when HasMonteCarlo is false; got: %s", truncate(out, 600))
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/whatif/ -run TestVerdictBar_Render -v`
Expected: FAIL — template `whatif-verdict-bar` not defined.

- [ ] **Step 3: Write the template**

Create `web/templates/components/whatif/verdict-bar.html`. The `.num` class is added in Task 5. Tints keyed by `.Verdict.Health`:

```html
{{/* Sticky Verdict Bar — overall plan-health summary */}}
{{/* Expects: .Verdict (whatif.VerdictView) */}}
{{define "whatif-verdict-bar"}}
{{- $v := .Verdict -}}
<div id="whatif-verdict-bar"
    class="verdict-{{$v.Health}} sticky top-0 z-30 rounded-xl border px-4 py-3 mb-4 flex flex-wrap items-center gap-x-6 gap-y-2
    {{if eq $v.Health "green"}}bg-emerald-50 dark:bg-emerald-900/20 border-emerald-300 dark:border-emerald-700
    {{else if eq $v.Health "amber"}}bg-amber-50 dark:bg-amber-900/20 border-amber-300 dark:border-amber-700
    {{else}}bg-rose-50 dark:bg-rose-900/20 border-rose-300 dark:border-rose-700{{end}}">

    <div class="flex-1 min-w-[14rem]">
        <div class="text-[10px] font-semibold tracking-widest uppercase
            {{if eq $v.Health "green"}}text-emerald-700 dark:text-emerald-300
            {{else if eq $v.Health "amber"}}text-amber-700 dark:text-amber-300
            {{else}}text-rose-700 dark:text-rose-300{{end}}">Plan Verdict</div>
        <div class="text-lg font-bold text-gray-900 dark:text-gray-100">
            {{$v.Headline}}
            <span class="text-sm font-normal text-gray-500 dark:text-gray-400">— {{$v.Detail}}</span>
        </div>
    </div>

    <div class="flex items-center gap-6 ml-auto">
        <div class="text-center">
            <div class="text-[10px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Monthly Gap</div>
            <div class="num text-base font-bold {{if $v.GapIsShortfall}}text-rose-600 dark:text-rose-400{{else}}text-emerald-600 dark:text-emerald-400{{end}}">
                {{if $v.GapIsShortfall}}-{{end}}${{formatNumber (abs $v.MonthlyGap)}}
            </div>
        </div>
        {{if $v.HasMonteCarlo}}
        <div class="text-center" title="Monte Carlo success rate">
            <div class="text-[10px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Monte Carlo</div>
            <div class="num text-base font-bold {{successRateTextClass $v.SuccessRate}}">{{printf "%.1f" $v.SuccessRate}}%</div>
        </div>
        {{end}}
        <div class="text-center">
            <div class="text-[10px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Req. Rate</div>
            <div class="num text-base font-bold text-gray-700 dark:text-gray-200">{{printf "%.1f" $v.RequiredRate}}%</div>
        </div>
    </div>
</div>
{{end}}
```

Note: `abs` is a registered funcmap helper (confirmed in `internal/templates/render.go`). The template formats the magnitude with `formatNumber (abs ...)` and supplies the sign itself via the `GapIsShortfall` leading `-`, so a surplus shows `$X` and a shortfall shows `-$X` regardless of how `formatNumber` treats negatives.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/handlers/whatif/ -run TestVerdictBar_Render -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/templates/components/whatif/verdict-bar.html internal/handlers/whatif/verdict_render_test.go
git commit -m "feat(whatif): sticky verdict bar template with health tint"
```

---

## Task 4: `.num` numeral utility (visual token)

**Files:**
- Modify: `web/static/css/styles.css`

- [ ] **Step 1: Append the `.num` utility**

Add to the end of `web/static/css/styles.css`:

```css
/* Tabular monospace numerals for aligned financial figures */
.num {
    font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
    font-variant-numeric: tabular-nums;
    font-feature-settings: "tnum";
}
```

- [ ] **Step 2: Verify the file is loaded (already linked at base.html:40) and build**

Run: `go build ./...`
Expected: success (CSS is static; this just confirms nothing broke).

- [ ] **Step 3: Commit**

```bash
git add web/static/css/styles.css
git commit -m "feat(whatif): add .num tabular-monospace numeral utility"
```

---

## Task 5: Tab + collapse + chart-resize JavaScript

**Files:**
- Create: `web/static/js/whatif-tabs.js`
- Modify: `web/templates/layouts/base.html` (add `<script>` include near line 158)

This JS targets DOM produced in Task 6/7 (tabs nav with `[data-wf-tab]`, panels `[data-wf-panel]`, container `#whatif-tabs[data-scenario]`, collapsible cards `[data-wf-collapse]`). Writing it first is fine — it is inert until that markup exists.

- [ ] **Step 1: Write `whatif-tabs.js`**

```javascript
// What-If page: tab switching, per-scenario persistence, settings-card
// collapse, and Plotly resize when a hidden tab becomes visible.
(function () {
  'use strict';

  function scenarioKey() {
    var c = document.getElementById('whatif-tabs');
    var sc = c ? (c.getAttribute('data-scenario') || 'default') : 'default';
    return 'whatifActiveTab:' + sc;
  }

  function resizeChartsIn(panel) {
    if (!panel || !window.Plotly) return;
    panel.querySelectorAll('[id^="chart-"]').forEach(function (el) {
      try { window.Plotly.Plots.resize(el); } catch (e) { /* not yet rendered */ }
    });
  }

  function activateTab(name, persist) {
    var container = document.getElementById('whatif-tabs');
    if (!container) return;
    var panels = container.querySelectorAll('[data-wf-panel]');
    var tabs = container.querySelectorAll('[data-wf-tab]');
    var matched = false;

    panels.forEach(function (p) {
      var on = p.getAttribute('data-wf-panel') === name;
      p.classList.toggle('hidden', !on);
      if (on) { matched = true; resizeChartsIn(p); }
    });
    tabs.forEach(function (t) {
      var on = t.getAttribute('data-wf-tab') === name;
      t.classList.toggle('wf-tab-active', on);
      t.setAttribute('aria-selected', on ? 'true' : 'false');
    });

    if (!matched) { return activateTab('overview', persist); }
    if (persist && window.localStorage) {
      try { window.localStorage.setItem(scenarioKey(), name); } catch (e) {}
    }
  }

  function restoreTab() {
    var name = 'overview';
    if (window.localStorage) {
      try { name = window.localStorage.getItem(scenarioKey()) || 'overview'; } catch (e) {}
    }
    activateTab(name, false);
  }

  // Settings-card collapse with persistence.
  function applyCollapse(card) {
    var id = card.getAttribute('data-wf-collapse');
    var body = card.querySelector('[data-wf-collapse-body]');
    if (!body) return;
    var collapsed = false;
    if (window.localStorage) {
      try { collapsed = window.localStorage.getItem('whatifCollapse:' + id) === '1'; } catch (e) {}
    }
    body.classList.toggle('hidden', collapsed);
    var chevron = card.querySelector('[data-wf-chevron]');
    if (chevron) chevron.classList.toggle('rotate-180', !collapsed);
  }

  function toggleCollapse(card) {
    var id = card.getAttribute('data-wf-collapse');
    var body = card.querySelector('[data-wf-collapse-body]');
    if (!body) return;
    var nowCollapsed = !body.classList.contains('hidden');
    body.classList.toggle('hidden', nowCollapsed);
    var chevron = card.querySelector('[data-wf-chevron]');
    if (chevron) chevron.classList.toggle('rotate-180', !nowCollapsed);
    if (window.localStorage) {
      try { window.localStorage.setItem('whatifCollapse:' + id, nowCollapsed ? '1' : '0'); } catch (e) {}
    }
  }

  function wire() {
    var container = document.getElementById('whatif-tabs');
    if (container && !container.__wfWired) {
      container.__wfWired = true;
      container.addEventListener('click', function (e) {
        var tab = e.target.closest('[data-wf-tab]');
        if (tab) { e.preventDefault(); activateTab(tab.getAttribute('data-wf-tab'), true); }
      });
    }
    document.querySelectorAll('[data-wf-collapse]').forEach(function (card) {
      applyCollapse(card);
      var header = card.querySelector('[data-wf-collapse-toggle]');
      if (header && !header.__wfWired) {
        header.__wfWired = true;
        header.addEventListener('click', function () { toggleCollapse(card); });
      }
    });
  }

  document.addEventListener('DOMContentLoaded', function () { wire(); restoreTab(); });

  // After the results partial re-renders, re-wire tabs and re-apply active tab
  // (charts.js handles chart (re)creation on the same afterSettle event).
  document.body.addEventListener('htmx:afterSettle', function (evt) {
    var t = evt.detail && evt.detail.target;
    if (t && t.id === 'whatif-results') { wire(); restoreTab(); }
  });
})();
```

- [ ] **Step 2: Include the script in `base.html`**

Add after the `charts.js` include (line ~158):

```html
<script src="/static/js/whatif-tabs.js"></script>
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add web/static/js/whatif-tabs.js web/templates/layouts/base.html
git commit -m "feat(whatif): tab switching, collapse persistence, chart resize JS"
```

---

## Task 6: Restructure results into the tab workspace

**Files:**
- Modify: `web/templates/pages/whatif.html` (`{{define "whatif-results"}}`, lines 107-237)
- Test: `internal/handlers/whatif/tabs_render_test.go`

- [ ] **Step 1: Write the failing render test**

```go
package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestWhatIfResults_TabStructure(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		t.Fatalf("runAnalysisWithCache: %v", err)
	}
	out, err := renderer.RenderToString("whatif-results", map[string]any{
		"Settings": settings,
		"Analysis": analysis,
		"Verdict":  BuildVerdict(analysis, settings),
		"Findings": nil,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	// Verdict bar present.
	if !strings.Contains(out, `id="whatif-verdict-bar"`) {
		t.Errorf("expected verdict bar in results")
	}
	// Tab container + the five tab buttons.
	if !strings.Contains(out, `id="whatif-tabs"`) {
		t.Errorf("expected tab container")
	}
	for _, tab := range []string{`data-wf-tab="overview"`, `data-wf-tab="cashflow"`, `data-wf-tab="risk"`, `data-wf-tab="taxes"`, `data-wf-tab="strategies"`} {
		if !strings.Contains(out, tab) {
			t.Errorf("expected tab button %q", tab)
		}
	}
	// Five panels.
	for _, panel := range []string{`data-wf-panel="overview"`, `data-wf-panel="cashflow"`, `data-wf-panel="risk"`, `data-wf-panel="taxes"`, `data-wf-panel="strategies"`} {
		if !strings.Contains(out, panel) {
			t.Errorf("expected panel %q", panel)
		}
	}
	// Section landed in its mapped tab: Monte Carlo heading still renders.
	if !strings.Contains(out, "Monte Carlo Simulation") {
		t.Errorf("expected Monte Carlo section to render inside Risk panel")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/whatif/ -run TestWhatIfResults_TabStructure -v`
Expected: FAIL — no `whatif-tabs` / `data-wf-tab` markup yet.

- [ ] **Step 3: Rewrite the results content section of `whatif.html`**

Keep lines 107-221 of `{{define "whatif-results"}}` (the `whatif-completeness-wrapper` and both OOB `<template>` blocks) **unchanged**. Replace the final "Main Results Content" block (current lines 223-236, the 13 bare `{{template ...}}` includes) with the verdict bar + tab shell:

```html
{{/* Sticky verdict bar — always visible above the tabs */}}
{{template "whatif-verdict-bar" .}}

{{/* Tabbed results workspace */}}
<div id="whatif-tabs" data-scenario="{{.ActiveFilename}}">
    <div role="tablist" class="flex flex-wrap gap-1 border-b border-gray-200 dark:border-gray-700 mb-4">
        <button type="button" role="tab" data-wf-tab="overview" class="wf-tab px-4 py-2 -mb-px text-sm font-medium text-gray-500 dark:text-gray-400 border-b-2 border-transparent hover:text-gray-800 dark:hover:text-gray-200">Overview</button>
        <button type="button" role="tab" data-wf-tab="cashflow" class="wf-tab px-4 py-2 -mb-px text-sm font-medium text-gray-500 dark:text-gray-400 border-b-2 border-transparent hover:text-gray-800 dark:hover:text-gray-200">Cash Flow</button>
        <button type="button" role="tab" data-wf-tab="risk" class="wf-tab px-4 py-2 -mb-px text-sm font-medium text-gray-500 dark:text-gray-400 border-b-2 border-transparent hover:text-gray-800 dark:hover:text-gray-200">Risk</button>
        <button type="button" role="tab" data-wf-tab="taxes" class="wf-tab px-4 py-2 -mb-px text-sm font-medium text-gray-500 dark:text-gray-400 border-b-2 border-transparent hover:text-gray-800 dark:hover:text-gray-200">Taxes &amp; RMD</button>
        <button type="button" role="tab" data-wf-tab="strategies" class="wf-tab px-4 py-2 -mb-px text-sm font-medium text-gray-500 dark:text-gray-400 border-b-2 border-transparent hover:text-gray-800 dark:hover:text-gray-200">Strategies</button>
    </div>

    {{/* Overview */}}
    <div data-wf-panel="overview" class="space-y-4">
        {{template "whatif-overview-kpis" .}}
        {{template "whatif-projection-chart" .}}
        {{template "whatif-failure-points" .}}
    </div>

    {{/* Cash Flow */}}
    <div data-wf-panel="cashflow" class="space-y-4 hidden">
        {{template "whatif-budget-analysis" .}}
        {{template "whatif-present-value" .}}
        {{template "whatif-income-chart" .}}
        {{template "whatif-projection-breakdown" .}}
    </div>

    {{/* Risk */}}
    <div data-wf-panel="risk" class="space-y-4 hidden">
        {{template "whatif-sensitivity" .}}
        {{template "whatif-monte-carlo" .}}
        {{template "whatif-historical-backtest" .}}
        {{template "whatif-guardrail-events" .}}
    </div>

    {{/* Taxes & RMD */}}
    <div data-wf-panel="taxes" class="space-y-4 hidden">
        {{template "whatif-rmd" .}}
    </div>

    {{/* Strategies */}}
    <div data-wf-panel="strategies" class="space-y-4 hidden">
        {{template "whatif-social-security-results" .}}
        {{template "whatif-tax-optimizer-results" .}}
    </div>
</div>
```

Notes:
- `failure-points` lives in Overview (it drives the alerts). It is removed from Risk above to avoid a duplicate id — if `whatif-failure-points` contains element ids that other code targets, keep it in Overview only.
- Tab buttons are written literally (Go templates' builtin `slice` slices an existing collection — it cannot construct one from string literals, so a `range slice "a" "b"` loop does NOT work here).
- `whatif-overview-kpis` is created in Task 8.

- [ ] **Step 4: Temporarily stub the KPI template so the test runs**

Until Task 8, add a placeholder define at the bottom of `whatif.html`:

```html
{{define "whatif-overview-kpis"}}{{end}}
```

(Removed/replaced in Task 8.)

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/handlers/whatif/ -run TestWhatIfResults_TabStructure -v`
Expected: PASS.

- [ ] **Step 6: Run the full whatif render suite to catch regressions**

Run: `go test ./internal/handlers/whatif/ -run Render -v`
Expected: PASS — OOB ids and section defines are unchanged, only their wrapping moved.

- [ ] **Step 7: Commit**

```bash
git add web/templates/pages/whatif.html internal/handlers/whatif/tabs_render_test.go
git commit -m "feat(whatif): move results into 5-tab workspace under verdict bar"
```

---

## Task 7: Group settings column into collapsible groups

**Files:**
- Modify: `web/templates/pages/whatif.html` (`{{define "whatif-content"}}` left column, lines 67-89)
- Test: `internal/handlers/whatif/tabs_render_test.go` (add a test)

- [ ] **Step 1: Add the failing test**

Append to `tabs_render_test.go`:

```go
func TestWhatIfSettings_Groups(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		t.Fatalf("runAnalysisWithCache: %v", err)
	}
	out, err := renderer.RenderToString("whatif-content", map[string]any{
		"Settings": settings, "Analysis": analysis,
		"Verdict": BuildVerdict(analysis, settings),
		"Scenarios": nil, "ActiveFilename": "whatif.json", "Findings": nil,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	for _, label := range []string{"Money In / Out", "Assumptions", "Strategies"} {
		if !strings.Contains(out, label) {
			t.Errorf("expected settings group label %q", label)
		}
	}
	for _, attr := range []string{`data-wf-collapse="money"`, `data-wf-collapse="assumptions"`, `data-wf-collapse="strategies"`} {
		if !strings.Contains(out, attr) {
			t.Errorf("expected collapsible group %q", attr)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/whatif/ -run TestWhatIfSettings_Groups -v`
Expected: FAIL — no group labels yet.

- [ ] **Step 3: Wrap the left column in three groups**

Replace the left settings column (`<div class="space-y-4"> ... </div>`, lines 69-89) with three collapsible group wrappers. A group:

```html
<div class="space-y-4">

    {{/* Group: Money In / Out */}}
    <div data-wf-collapse="money" class="bg-white dark:bg-gray-800 rounded-lg shadow">
        <button type="button" data-wf-collapse-toggle
            class="w-full flex items-center justify-between px-4 py-3 text-left">
            <span class="text-xs font-semibold tracking-widest uppercase text-gray-500 dark:text-gray-400">Money In / Out</span>
            <svg data-wf-chevron class="w-4 h-4 text-gray-400 transition-transform rotate-180" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"></path>
            </svg>
        </button>
        <div data-wf-collapse-body class="px-1 pb-2 space-y-4">
            <div id="whatif-portfolio-settings-card">{{template "whatif-portfolio-settings" .}}</div>
            {{template "whatif-income-card" .}}
            {{template "whatif-healthcare-card" .}}
            {{template "whatif-bigticket-card" .}}
            {{template "whatif-expense-card" .}}
        </div>
    </div>

    {{/* Group: Assumptions */}}
    <div data-wf-collapse="assumptions" class="bg-white dark:bg-gray-800 rounded-lg shadow">
        <button type="button" data-wf-collapse-toggle
            class="w-full flex items-center justify-between px-4 py-3 text-left">
            <span class="text-xs font-semibold tracking-widest uppercase text-gray-500 dark:text-gray-400">Assumptions</span>
            <svg data-wf-chevron class="w-4 h-4 text-gray-400 transition-transform rotate-180" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"></path>
            </svg>
        </button>
        <div data-wf-collapse-body class="px-1 pb-2 space-y-4">
            <div id="whatif-rate-assumptions-card">{{template "whatif-rate-assumptions" .}}</div>
            <div id="whatif-spending-phases-card">{{template "whatif-spending-phases" .}}</div>
        </div>
    </div>

    {{/* Group: Strategies */}}
    <div data-wf-collapse="strategies" class="bg-white dark:bg-gray-800 rounded-lg shadow">
        <button type="button" data-wf-collapse-toggle
            class="w-full flex items-center justify-between px-4 py-3 text-left">
            <span class="text-xs font-semibold tracking-widest uppercase text-gray-500 dark:text-gray-400">Strategies</span>
            <svg data-wf-chevron class="w-4 h-4 text-gray-400 transition-transform rotate-180" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"></path>
            </svg>
        </button>
        <div data-wf-collapse-body class="px-1 pb-2 space-y-4">
            {{template "whatif-guardrails" .}}
            {{template "whatif-roth-conversion" .}}
            {{template "whatif-scenario-chain" .}}
            <div id="whatif-social-security-card">{{template "whatif-social-security-config" .}}</div>
        </div>
    </div>

</div>
```

CRITICAL: the ids `whatif-portfolio-settings-card`, `whatif-rate-assumptions-card`, `whatif-spending-phases-card`, `whatif-social-security-card` MUST be preserved exactly — the OOB `<template>` block in `whatif-results` (lines 117-128) targets them with `hx-swap-oob`. Render tests assert nothing about these here, but the OOB swap depends on them.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/handlers/whatif/ -run "TestWhatIfSettings_Groups|Render" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/templates/pages/whatif.html internal/handlers/whatif/tabs_render_test.go
git commit -m "feat(whatif): group settings into collapsible Money/Assumptions/Strategies"
```

---

## Task 8: Overview KPI tiles

**Files:**
- Modify: `web/templates/pages/whatif.html` (replace the `whatif-overview-kpis` stub)
- Test: `internal/handlers/whatif/tabs_render_test.go` (add a test)

- [ ] **Step 1: Add the failing test**

```go
func TestWhatIfOverviewKPIs_Render(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		t.Fatalf("runAnalysisWithCache: %v", err)
	}
	out, err := renderer.RenderToString("whatif-overview-kpis", map[string]any{
		"Settings": settings, "Analysis": analysis,
		"Verdict": BuildVerdict(analysis, settings),
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	for _, want := range []string{"Monthly Gap", "Success", "End Balance", `class="num`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in KPI tiles; got: %s", want, truncate(out, 800))
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/whatif/ -run TestWhatIfOverviewKPIs_Render -v`
Expected: FAIL — stub define renders nothing, assertions miss.

- [ ] **Step 3: Replace the stub define with the KPI tiles**

Replace `{{define "whatif-overview-kpis"}}{{end}}` with:

```html
{{define "whatif-overview-kpis"}}
{{- $v := .Verdict -}}
<div class="grid grid-cols-2 lg:grid-cols-4 gap-3">
    <div class="rounded-xl border p-3 {{if $v.GapIsShortfall}}bg-rose-50 dark:bg-rose-900/20 border-rose-200 dark:border-rose-800{{else}}bg-emerald-50 dark:bg-emerald-900/20 border-emerald-200 dark:border-emerald-800{{end}}">
        <div class="text-[10px] uppercase tracking-wide {{if $v.GapIsShortfall}}text-rose-600 dark:text-rose-300{{else}}text-emerald-600 dark:text-emerald-300{{end}}">Monthly Gap</div>
        <div class="num text-lg font-bold {{if $v.GapIsShortfall}}text-rose-600 dark:text-rose-400{{else}}text-emerald-600 dark:text-emerald-400{{end}}">{{if $v.GapIsShortfall}}-{{end}}${{formatNumber (abs $v.MonthlyGap)}}</div>
    </div>
    <div class="rounded-xl border p-3 bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700">
        <div class="text-[10px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Success</div>
        <div class="num text-lg font-bold {{if $v.HasMonteCarlo}}{{successRateTextClass $v.SuccessRate}}{{else}}text-gray-400{{end}}">{{if $v.HasMonteCarlo}}{{printf "%.1f" $v.SuccessRate}}%{{else}}—{{end}}</div>
    </div>
    <div class="rounded-xl border p-3 bg-rose-50 dark:bg-rose-900/20 border-rose-200 dark:border-rose-800">
        <div class="text-[10px] uppercase tracking-wide text-rose-600 dark:text-rose-300">Est. Taxes (total)</div>
        <div class="num text-lg font-bold text-rose-600 dark:text-rose-400">{{if .Analysis.Tax}}-${{formatNumber .Analysis.Tax.TotalTaxPaid}}{{else}}—{{end}}</div>
    </div>
    <div class="rounded-xl border p-3 bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700">
        <div class="text-[10px] uppercase tracking-wide text-gray-500 dark:text-gray-400">End Balance</div>
        <div class="num text-lg font-bold {{if and .Analysis.Projection (gt .Analysis.Projection.FinalBalance 0.0)}}text-emerald-600 dark:text-emerald-400{{else}}text-gray-500 dark:text-gray-400{{end}}">{{if .Analysis.Projection}}${{formatNumber .Analysis.Projection.FinalBalance}}{{else}}—{{end}}</div>
    </div>
</div>
{{end}}
```

(Taxes tile shows total federal+state tax over the projection — `Tax.TotalTaxPaid` — labeled "total" to avoid implying it is annual.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/handlers/whatif/ -run TestWhatIfOverviewKPIs_Render -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/templates/pages/whatif.html internal/handlers/whatif/tabs_render_test.go
git commit -m "feat(whatif): overview KPI tiles (gap, success, taxes, end balance)"
```

---

## Task 9: Comprehension subtitles on result sections

**Files:**
- Modify: result section templates in `web/templates/components/whatif/` that lack a plain-English one-liner.

Add a single muted subtitle line directly under the section heading where missing. Budget Analysis and Monte Carlo already have one (`budget-analysis.html:6`, `monte-carlo.html:26`) — leave them. Add to the others.

- [ ] **Step 1: Add subtitle to `historical-backtest.html`**

Under its `<h3>` heading, add:

```html
<p class="text-xs text-gray-500 dark:text-gray-300 mb-3">Replays your plan through every historical market sequence since 1928 — the share that never run out is the success rate.</p>
```

- [ ] **Step 2: Add subtitle to `sensitivity.html`**

```html
<p class="text-xs text-gray-500 dark:text-gray-300 mb-3">How portfolio survival shifts when one assumption (return, inflation, spending) moves up or down.</p>
```

- [ ] **Step 3: Add subtitle to `rmd.html`**

```html
<p class="text-xs text-gray-500 dark:text-gray-300 mb-3">Required Minimum Distributions the IRS forces from tax-deferred accounts starting at your RMD age.</p>
```

- [ ] **Step 4: Add subtitle to `present-value.html`**

```html
<p class="text-xs text-gray-500 dark:text-gray-300 mb-3">Your future plan expressed in today's dollars, so the numbers are comparable to what money buys now.</p>
```

(Place each under the existing heading element; match the existing heading wrapper structure in each file. If a file's heading is inside a flex row with a button, insert the `<p>` after the closing `</div>` of that row.)

- [ ] **Step 5: Verify renders**

Run: `go test ./internal/handlers/whatif/ -run Render -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/templates/components/whatif/
git commit -m "feat(whatif): plain-English subtitles on result sections"
```

---

## Task 10: Verification & manual check

**Files:** none (verification only)

- [ ] **Step 1: Full verification suite (per CLAUDE.md)**

Run: `go build ./... && go vet ./... && go test ./... && staticcheck ./...`
Expected: all green.

- [ ] **Step 2: Confirm the diff scope**

Run: `git diff --stat master`
Expected: only `internal/handlers/whatif/*`, `web/templates/pages/whatif.html`, `web/templates/components/whatif/*`, `web/static/js/whatif-tabs.js`, `web/static/css/styles.css`, `web/templates/layouts/base.html`, and `docs/superpowers/*`.

- [ ] **Step 3: Manual smoke test (both themes)**

Start the app (project `run` skill or the usual server command). In the browser:
- What-if page loads with the verdict bar on top, Overview tab active, projection chart sized correctly.
- Click each tab (Cash Flow → income chart sizes correctly after switch; Risk → Monte Carlo renders; Taxes; Strategies). Reload — last tab restored.
- Collapse a settings group, reload — stays collapsed.
- Change a setting that triggers an HTMX recalculation — verdict bar + tiles update, active tab is preserved, charts re-render.
- Toggle dark/light — colors and numerals remain legible; costs stay red.

- [ ] **Step 4: Commit any fixes from the smoke test, then finish**

```bash
git add -A
git commit -m "fix(whatif): smoke-test corrections for tabbed redesign"
```

---

## Self-review notes

- **Spec coverage:** structure (T6), tab mapping (T6), settings groups (T7), verdict bar (T1-3), visual system `.num`+tints (T3,T4,T8), client-side tabs + persistence + chart resize (T5), comprehension subtitles (T9), testing (T1,T3,T6,T7,T8,T10). Out-of-scope items (other pages, slider-linked gap) intentionally excluded.
- **Deviation from spec §4:** the verdict bar's "Monthly Gap" uses `BudgetFit.MonthlyGap` (current) rather than the steady-state slider's selected year, to avoid coupling the server-rendered bar to client-side slider state. Flagged for the reviewer; revisit if the user wants slider-linkage.
- **Deviation:** `failure-points` placed in Overview (alerts source) and removed from Risk to prevent duplicate element ids; if alerts are later split from the full failure-points section, reconsider.
- **Funcmap (verified in `internal/templates/render.go`):** `dict`, `abs`, `formatNumber`, `successRateTextClass` are registered and used. Tab buttons are literal (builtin `slice` cannot construct a list from literals). `abs` is used for gap magnitude in T3/T8.
- **Risk — Plotly in hidden tabs:** mitigated by `Plotly.Plots.resize` on tab activation (T5); charts are created by existing `loadAllCharts` on `afterSettle`.
