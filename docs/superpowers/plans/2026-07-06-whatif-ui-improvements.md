# What-If UI Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the redundancy, mixed messaging, formatting inconsistency, and mobile breakage found in the 2026-07-06 browser survey of `/whatif`.

**Architecture:** All changes are Go `html/template` edits under `web/templates/`, small view-model additions in `internal/handlers/whatif/verdict.go`, one chart-data change in `internal/handlers/whatif/handlers.go`, and one new template func in `internal/templates/render.go`. Render tests live in `internal/handlers/whatif/*_render_test.go` and use `setupTestEnvWithRenderer(t)` + `renderer.RenderToString(templateName, data)`.

**Tech Stack:** Go 1.26, html/template, HTMX, Tailwind (CDN, class-based dark mode), Plotly.

## Global Constraints

- Cost items (taxes, IRMAA, NIIT) MUST keep red/rose styling — never neutral gray (standing user requirement).
- Verify before every commit: `go build ./... && go vet ./... && go test ./... && staticcheck ./...` — run bare, never piped through grep/head (pipe eats the exit code).
- Do not change the behavior of the existing `formatMoney` template func — it is used by every page in the app. Add a new func instead.
- Template define names referenced from Go tests must stay stable: `whatif-verdict-bar`, `whatif-budget-analysis`, `whatif-budget-steady-state`, `whatif-tax-summary`, `whatif-content`.
- Dark mode is class-based (`dark:` variants). Every new element needs both light and dark classes.
- Browser verification uses `scripts/whatif-verify.sh start` (port 8099) — never `go run` against real `data/`, never `pkill` (use `stop`).

## Explicitly Deferred (do NOT implement in this plan)

- Tailwind CDN → compiled CSS (build-system change; separate follow-up).
- Column-set toggles (Basic/Taxes/All) for the Year-by-Year table — this plan only adds a sticky Year column.
- Quick Adjust FAB show/hide behavior.
- "Empty left column" finding — already handled: `whatif-tabs.js` collapses the settings column to a 1/6 rail and persists state in localStorage.

---

### Task 1: Register `formatDollars` template func

The survey found cents noise on $1.6M totals (`-$1,624,993.75`) and inconsistent precision (`-$6,436` vs `$6,435.53`). `internal/templates/render.go` already has `formatWholeDollars(v float64) string` (renders `$X,XXX`, no cents, `-$` prefix for negatives, round-half-up) — it's just not registered in the func map.

**Files:**
- Modify: `internal/templates/render.go` (func map at `getFuncMap()`, ~line 70)
- Test: `internal/templates/render_helpers_test.go`

**Interfaces:**
- Produces: template func `formatDollars` usable in all templates: `{{formatDollars 1624993.75}}` → `$1,624,994`. Tasks 3 and 6 consume it.

- [ ] **Step 1: Write the failing test**

Add to `internal/templates/render_helpers_test.go`:

```go
func TestFormatDollarsTemplateFunc(t *testing.T) {
	fn, ok := getFuncMap()["formatDollars"].(func(float64) string)
	if !ok {
		t.Fatal("formatDollars not registered in template func map")
	}
	cases := []struct {
		in   float64
		want string
	}{
		{1624993.75, "$1,624,994"},
		{-6435.53, "-$6,436"},
		{0, "$0"},
		{342706.4, "$342,706"},
	}
	for _, c := range cases {
		if got := fn(c.in); got != c.want {
			t.Errorf("formatDollars(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

Also add `"formatDollars"` to the expected-function-name list in the existing func-map completeness test at `internal/templates/render_helpers_test.go:424` (the slice containing `"formatMoney", "conversionSummary", "formatNumber", ...`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/templates/ -run TestFormatDollarsTemplateFunc -v`
Expected: FAIL with "formatDollars not registered in template func map"

- [ ] **Step 3: Register the func**

In `getFuncMap()` in `internal/templates/render.go`, directly under the `"formatMoney"` entry, add:

```go
		"formatDollars":                       formatWholeDollars,
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/templates/ -v -run "TestFormatDollars|FuncMap"`
Expected: PASS

- [ ] **Step 5: Full verify and commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add internal/templates/render.go internal/templates/render_helpers_test.go
git commit -m "feat(templates): register formatDollars whole-dollar formatter"
```

---

### Task 2: Verdict detail stops contradicting the Monte Carlo result

Today a plan with 62.2% MC success renders "Funded through 2074 — spending covered for all 48 years". The health band is already amber (`mcStrongThreshold = 70`), but the words say "you're fine". Make the detail text carry the risk.

**Files:**
- Modify: `internal/handlers/whatif/verdict.go:69-79`
- Test: `internal/handlers/whatif/verdict_test.go` (extend the existing "funded but weak MC is amber" subtest at ~line 34)

**Interfaces:**
- Produces: `VerdictView.Detail` for the survives-but-weak-MC case becomes `"covers the median path — N% of market simulations fall short"` (N = 100 − SuccessRate, %.0f). Headline is unchanged. Task 3's render test asserts this text flows through the template.

- [ ] **Step 1: Write the failing test**

In `internal/handlers/whatif/verdict_test.go`, replace the body of the `t.Run("funded but weak MC is amber", ...)` subtest with:

```go
	t.Run("funded but weak MC is amber and says so", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 30, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: true},
			BudgetFit:  &models.BudgetFitAnalysis{MonthlyGap: 100},
			MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 55}},
		}
		v := BuildVerdict(a, s)
		if v.Health != models.HealthAmber {
			t.Errorf("Health = %q, want amber", v.Health)
		}
		if want := "covers the median path — 45% of market simulations fall short"; v.Detail != want {
			t.Errorf("Detail = %q, want %q", v.Detail, want)
		}
		if v.Headline != "Funded through 2056" {
			t.Errorf("Headline = %q, want \"Funded through 2056\"", v.Headline)
		}
	})
```

Add a companion assertion to the green case (`"funded full horizon with strong MC is green"` subtest, after the SuccessRate check):

```go
		if want := "spending covered for all 38 years"; v.Detail != want {
			t.Errorf("Detail = %q, want %q (strong MC keeps the plain detail)", v.Detail, want)
		}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/whatif/ -run TestBuildVerdict -v`
Expected: FAIL — Detail still reads "spending covered for all 30 years" in the amber subtest.

- [ ] **Step 3: Implement**

In `internal/handlers/whatif/verdict.go`, replace the survives branch (currently lines 70-80):

```go
	if survives {
		v.YearsCovered = s.ProjectionYears
		v.Headline = fmt.Sprintf("Funded through %d", startYear+s.ProjectionYears)
		v.Detail = fmt.Sprintf("spending covered for all %d years", s.ProjectionYears)
		if !v.HasMonteCarlo || v.SuccessRate >= mcStrongThreshold {
			v.Health = models.HealthGreen
		} else {
			// Median path survives but a material share of simulations fail.
			// The words must not out-promise the health band.
			v.Health = models.HealthAmber
			v.Detail = fmt.Sprintf("covers the median path — %.0f%% of market simulations fall short", 100-v.SuccessRate)
		}
		return v
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/handlers/whatif/ -run "TestBuildVerdict|TestVerdictBar_Render" -v`
Expected: PASS (the render test fixtures set Detail explicitly, so they are unaffected).

- [ ] **Step 5: Full verify and commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add internal/handlers/whatif/verdict.go internal/handlers/whatif/verdict_test.go
git commit -m "fix(whatif): verdict detail names the Monte Carlo failure share when MC is weak"
```

---

### Task 3: One summary strip — fold Est. Taxes / End Balance into the verdict bar, delete the duplicate KPI row

The Overview KPI row duplicates two of the verdict bar's three stats. Move the two non-duplicated tiles (Est. Taxes, End Balance) into the verdict bar and delete the KPI row entirely.

**Files:**
- Modify: `internal/handlers/whatif/verdict.go` (VerdictView struct + BuildVerdict)
- Modify: `web/templates/components/whatif/verdict-bar.html`
- Modify: `web/templates/pages/whatif.html` (remove `{{template "whatif-overview-kpis" .}}` at line 271 and the entire `{{define "whatif-overview-kpis"}}...{{end}}` block at lines 306-326)
- Test: `internal/handlers/whatif/verdict_test.go`, `internal/handlers/whatif/verdict_render_test.go`, `internal/handlers/whatif/tabs_render_test.go`

**Interfaces:**
- Consumes: `formatDollars` from Task 1.
- Produces: `VerdictView` gains `TotalTaxes float64`, `HasTaxes bool`, `EndBalance float64`, `HasEndBalance bool`. Template `whatif-overview-kpis` is DELETED — nothing may reference it afterward.

- [ ] **Step 1: Write the failing unit test**

Add to `TestBuildVerdict` in `internal/handlers/whatif/verdict_test.go`:

```go
	t.Run("carries lifetime taxes and end balance for the summary strip", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: true, FinalBalance: 342706.42},
			Tax:        &models.TaxAnalysis{TotalTaxPaid: 1624993.75},
		}
		v := BuildVerdict(a, s)
		if !v.HasTaxes || v.TotalTaxes != 1624993.75 {
			t.Errorf("taxes = (%v, %v), want (true, 1624993.75)", v.HasTaxes, v.TotalTaxes)
		}
		if !v.HasEndBalance || v.EndBalance != 342706.42 {
			t.Errorf("end balance = (%v, %v), want (true, 342706.42)", v.HasEndBalance, v.EndBalance)
		}
	})
	t.Run("nil analysis sections leave strip extras unset", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		v := BuildVerdict(&models.WhatIfAnalysis{}, s)
		if v.HasTaxes || v.HasEndBalance {
			t.Errorf("expected HasTaxes/HasEndBalance false, got %v/%v", v.HasTaxes, v.HasEndBalance)
		}
	})
```

Note: if `models.TaxAnalysis` is not the concrete type of `WhatIfAnalysis.Tax`, check `internal/models/whatif.go` for the field's actual type and use that (the template accesses `.Analysis.Tax.TotalTaxPaid`, so the field name is `TotalTaxPaid`).

- [ ] **Step 2: Write the failing render test**

Add to `TestVerdictBar_Render` in `internal/handlers/whatif/verdict_render_test.go`:

```go
	t.Run("strip shows lifetime taxes and end balance in whole dollars", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-verdict-bar", map[string]any{
			"Verdict": VerdictView{
				Health: models.HealthAmber, Headline: "Funded through 2074",
				Detail:     "covers the median path — 38% of market simulations fall short",
				MonthlyGap: 6435.53, GapIsShortfall: true, RequiredRate: 3.1,
				SuccessRate: 62.2, HasMonteCarlo: true,
				TotalTaxes: 1624993.75, HasTaxes: true,
				EndBalance: 342706.42, HasEndBalance: true,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"Est. Taxes", "$1,624,994", "End Balance", "$342,706"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in verdict strip; got: %s", want, truncate(out, 900))
			}
		}
		if strings.Contains(out, "1,624,993.75") {
			t.Errorf("taxes should be whole dollars, found cents: %s", truncate(out, 900))
		}
		if strings.Contains(out, "-$1,624,994") {
			t.Errorf("taxes tile must not carry a minus sign (red + label already encode cost)")
		}
	})
	t.Run("strip omits taxes and end balance when unavailable", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-verdict-bar", map[string]any{
			"Verdict": VerdictView{Health: models.HealthGreen, Headline: "Funded through 2046", Detail: "spending covered for all 20 years"},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, absent := range []string{"Est. Taxes", "End Balance"} {
			if strings.Contains(out, absent) {
				t.Errorf("did not expect %q without data", absent)
			}
		}
	})
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/handlers/whatif/ -run "TestBuildVerdict|TestVerdictBar_Render" -v`
Expected: FAIL — unknown fields `TotalTaxes`/`HasTaxes` (compile error). That counts as the red step for both.

- [ ] **Step 4: Extend VerdictView and BuildVerdict**

In `internal/handlers/whatif/verdict.go`, add to the `VerdictView` struct after `HasMonteCarlo`:

```go
	// Strip extras: lifetime income tax and projected end balance shown in
	// the sticky bar so the Overview tab needs no duplicate KPI row.
	TotalTaxes    float64
	HasTaxes      bool
	EndBalance    float64
	HasEndBalance bool
```

In `BuildVerdict`, after the MonteCarlo block (line ~67) and before the `survives :=` line, add:

```go
	if a.Tax != nil {
		v.HasTaxes = true
		v.TotalTaxes = a.Tax.TotalTaxPaid
	}
	if a.Projection != nil {
		v.HasEndBalance = true
		v.EndBalance = a.Projection.FinalBalance
	}
```

- [ ] **Step 5: Extend the verdict bar template**

In `web/templates/components/whatif/verdict-bar.html`, inside `<div class="flex items-center gap-6 ml-auto">`, after the Req. Rate block (before its closing `</div>`), add:

```html
        {{if $v.HasTaxes}}
        <div class="text-center" title="Lifetime income tax over the plan">
            <div class="text-[10px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Est. Taxes</div>
            <div class="num text-base font-bold text-rose-600 dark:text-rose-400">{{formatDollars $v.TotalTaxes}}<span class="sr-only"> lifetime cost</span></div>
        </div>
        {{end}}
        {{if $v.HasEndBalance}}
        <div class="text-center" title="Projected final balance (nominal)">
            <div class="text-[10px] uppercase tracking-wide text-gray-500 dark:text-gray-400">End Balance</div>
            <div class="num text-base font-bold {{if gt $v.EndBalance 0.0}}text-emerald-600 dark:text-emerald-400{{else}}text-gray-500 dark:text-gray-400{{end}}">{{formatDollars $v.EndBalance}}</div>
        </div>
        {{end}}
```

Also in the Gap block of the same file, add a screen-reader word after the amount (color-only encoding fix):

```html
            <div class="num text-base font-bold {{if $v.GapIsShortfall}}text-rose-600 dark:text-rose-400{{else}}text-emerald-600 dark:text-emerald-400{{end}}">
                {{if $v.GapIsShortfall}}-{{end}}${{formatNumber (abs $v.MonthlyGap)}}<span class="sr-only">{{if $v.GapIsShortfall}} shortfall{{else}} surplus{{end}}</span>
            </div>
```

- [ ] **Step 6: Delete the KPI row**

In `web/templates/pages/whatif.html`:
1. Delete line 271: `{{template "whatif-overview-kpis" .}}` (the Overview panel then starts with `whatif-projection-chart`).
2. Delete the whole `{{define "whatif-overview-kpis"}} ... {{end}}` block (lines 306-326).

In `internal/handlers/whatif/tabs_render_test.go`, delete `TestWhatIfOverviewKPIs_Render` (lines ~121-147) entirely — the verdict render test from Step 2 covers the strip.

Confirm nothing else references the deleted define: `grep -rn "whatif-overview-kpis" web/ internal/` must return nothing.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/handlers/whatif/ -v -run "TestBuildVerdict|TestVerdictBar_Render|TestWhatIf"`
Expected: PASS, and no test referencing `whatif-overview-kpis` remains.

- [ ] **Step 8: Full verify and commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add internal/handlers/whatif/verdict.go internal/handlers/whatif/verdict_test.go \
        internal/handlers/whatif/verdict_render_test.go internal/handlers/whatif/tabs_render_test.go \
        web/templates/components/whatif/verdict-bar.html web/templates/pages/whatif.html
git commit -m "refactor(whatif): verdict bar is the single summary strip; drop duplicate KPI row"
```

---

### Task 4: Cash Flow — taxes get their own group; kill the year-0 duplicate block

Two fixes in `web/templates/components/whatif/budget-analysis.html`:
(a) In the "Current (Today)" breakdown, the RMD/taxes/IRMAA rows render after the income items with income-style indentation — under the INCOME header. Give them a "Taxes & Deductions" group header.
(b) When the steady-state slider sits at year 0, the "At Year 0" block repeats the exact numbers of "Current (Today)" (expenses, income, taxes, gap tiles). Suppress the duplicated rows/tiles at year 0, keeping the slider and the Suggested Withdrawal Mix (which only exists in the steady block).

**Files:**
- Modify: `web/templates/components/whatif/budget-analysis.html`
- Test: `internal/handlers/whatif/budget_analysis_render_test.go` (create)

**Interfaces:**
- Consumes: `whatif-budget-analysis` / `whatif-budget-steady-state` defines; `.Analysis.BudgetFit` fields already used by the template (`MonthlyRMD`, `MonthlyTaxes`, `MonthlyIRMAA`, `SteadyStateYear`, `HasSteadyState`, ...).
- Produces: same define names; `whatif-budget-steady-state` renders its breakdown table and gap tiles only when `SteadyStateYear > 0`. `gross_withdrawal_render_test.go` renders `whatif-budget-steady-state` directly — check its fixtures' `SteadyStateYear`; any fixture relying on the tiles must set `SteadyStateYear` > 0.

- [ ] **Step 1: Write the failing render test**

Create `internal/handlers/whatif/budget_analysis_render_test.go`:

```go
package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func budgetFitFixture() *models.BudgetFitAnalysis {
	return &models.BudgetFitAnalysis{
		MonthlyExpenses: 10686, MonthlyIncome: 5300,
		MonthlyGap: 6435.53, RequiredRate: 3.1,
		MonthlyTaxes: 1049.53, MonthlyStateTax: 342.92,
		HasSteadyState: true, SteadyStateYear: 0,
		SteadyStateExpenses: 10686, SteadyStateIncome: 5300,
		SteadyStateGap: 6435.53, SteadyStateRate: 3.1,
		SteadyStateTaxes: 1049.53,
	}
}

func TestBudgetAnalysis_Render(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()

	t.Run("taxes sit under their own group header, not income", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": settings,
			"Analysis": &models.WhatIfAnalysis{BudgetFit: budgetFitFixture()},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, "Taxes &amp; Deductions") {
			t.Errorf("expected a Taxes & Deductions group header; got: %s", truncate(out, 1200))
		}
		incomeIdx := strings.Index(out, ">Income<")
		taxHeaderIdx := strings.Index(out, "Taxes &amp; Deductions")
		taxRowIdx := strings.Index(out, "Estimated Taxes")
		if incomeIdx == -1 || taxHeaderIdx == -1 || taxRowIdx == -1 {
			t.Fatalf("missing section markers (income=%d taxHeader=%d taxRow=%d)", incomeIdx, taxHeaderIdx, taxRowIdx)
		}
		if !(incomeIdx < taxHeaderIdx && taxHeaderIdx < taxRowIdx) {
			t.Errorf("Estimated Taxes row must come after its own group header, after the income section")
		}
	})

	t.Run("year 0 suppresses the duplicate steady-state breakdown", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": settings,
			"Analysis": &models.WhatIfAnalysis{BudgetFit: budgetFitFixture()},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if got := strings.Count(out, "Monthly Gap"); got != 1 {
			t.Errorf("expected exactly one Monthly Gap tile at year 0, got %d", got)
		}
		if !strings.Contains(out, "Suggested Withdrawal Mix") {
			t.Errorf("withdrawal mix must survive the year-0 dedup")
		}
	})

	t.Run("year above 0 keeps both blocks", func(t *testing.T) {
		bf := budgetFitFixture()
		bf.SteadyStateYear = 12
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": settings,
			"Analysis": &models.WhatIfAnalysis{BudgetFit: bf},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if got := strings.Count(out, "Monthly Gap"); got != 2 {
			t.Errorf("expected today + year-12 gap tiles, got %d", got)
		}
	})
}
```

Adjust `budgetFitFixture` field names against `internal/models/whatif.go` if any differ (the template references them exactly as written in the current markup: `MonthlyExpenses`, `MonthlyIncome`, `MonthlyGap`, `RequiredRate`, `MonthlyRMD`, `MonthlyTaxes`, `MonthlyStateTax`, `MonthlyNIIT`, `MonthlyIRMAA`, `TaxableSocialSecurityPct`, `HasSteadyState`, `SteadyState*`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/whatif/ -run TestBudgetAnalysis_Render -v`
Expected: FAIL — no "Taxes & Deductions" header, two "Monthly Gap" tiles at year 0.

- [ ] **Step 3: Restructure the Current block**

In `web/templates/components/whatif/budget-analysis.html`, the RMD/taxes rows currently start right after the income breakdown `{{end}}{{end}}` (line ~52). Insert a group header before them and re-indent:

```html
            {{if or (gt .Analysis.BudgetFit.MonthlyRMD 0.0) (gt .Analysis.BudgetFit.MonthlyTaxes 0.0) (gt .Analysis.BudgetFit.MonthlyIRMAA 0.0)}}
            <!-- Taxes & Deductions Section -->
            <div class="flex justify-between py-1.5 mt-2 border-b-2 border-gray-300 dark:border-gray-600">
                <span class="font-semibold text-gray-700 dark:text-gray-200 uppercase text-xs tracking-wide">Taxes &amp; Deductions</span>
                <span class="font-semibold text-red-600 dark:text-red-400">{{formatMoney (add .Analysis.BudgetFit.MonthlyTaxes .Analysis.BudgetFit.MonthlyIRMAA)}}</span>
            </div>
            {{end}}
```

Keep the existing `Required RMD`, `Estimated Taxes`, `Estimated IRMAA` rows at `pl-4`, and the `Includes State Tax`, `Includes NIIT`, `Taxable Social Security` rows at `pl-8` (Taxable Social Security is currently `pl-8` in this block — leave it; the steady block gets matching indentation in Step 4). Do not change any row's red/rose classes.

- [ ] **Step 4: Dedup the steady-state block at year 0**

In `{{define "whatif-budget-steady-state"}}`:

1. Wrap the breakdown table (`<!-- Breakdown table --> <div class="mb-4 text-sm">...</div>`) AND the gap-tiles grid (`<div class="grid grid-cols-2 gap-4 text-center">...</div>`) AND the "Values shown in nominal dollars…" note in a single guard:

```html
    {{if gt .Analysis.BudgetFit.SteadyStateYear 0.0}}
    <!-- Breakdown table -->
    ...existing table...
    ...existing gap tiles grid...
    ...existing nominal-dollars note...
    {{end}}
```

2. Inside the kept table, change the `Includes State Tax` / `Includes NIIT` rows from `pl-4` to `pl-8` so indentation matches the Current block.

The header (`At Year N` + slider) and the `Suggested Withdrawal Mix` section stay outside the guard — always rendered.

3. Check `internal/handlers/whatif/gross_withdrawal_render_test.go` (it renders `whatif-budget-steady-state` directly at lines 15/57/98): if any of its fixtures leave `SteadyStateYear` at 0 while asserting on withdrawal-mix strings, they still pass (mix is outside the guard). If one asserts on gap tiles, set `SteadyStateYear: 5` in that fixture.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/handlers/whatif/ -run "TestBudgetAnalysis_Render|GrossWithdrawal" -v`
Expected: PASS

- [ ] **Step 6: Full verify and commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add web/templates/components/whatif/budget-analysis.html internal/handlers/whatif/budget_analysis_render_test.go internal/handlers/whatif/gross_withdrawal_render_test.go
git commit -m "fix(whatif): taxes get their own budget group; year-0 steady-state dedup"
```

---

### Task 5: Cash Flow — slider shows year + age; withdrawal-mix explainer becomes a disclosure

**Files:**
- Modify: `web/templates/components/whatif/budget-analysis.html` (steady-state header + explainer paragraph)
- Test: `internal/handlers/whatif/budget_analysis_render_test.go` (extend)

**Interfaces:**
- Consumes: `.Settings.CurrentAge` (int), existing `add` template func, existing `steady-state-year-display` span and range input.
- Produces: new span `id="steady-state-age-display"`; range input gains `data-base-age`.

- [ ] **Step 1: Write the failing test**

Add to `TestBudgetAnalysis_Render`:

```go
	t.Run("slider header names the age alongside the year", func(t *testing.T) {
		bf := budgetFitFixture()
		bf.SteadyStateYear = 12
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 67
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": s,
			"Analysis": &models.WhatIfAnalysis{BudgetFit: bf},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{`id="steady-state-age-display"`, ">79<", `data-base-age="67"`} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in slider header; got: %s", want, truncate(out, 1200))
			}
		}
	})

	t.Run("withdrawal-mix explainer is a collapsed disclosure", func(t *testing.T) {
		bf := budgetFitFixture()
		bf.SteadyStateYear = 12
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": models.DefaultWhatIfSettings(),
			"Analysis": &models.WhatIfAnalysis{BudgetFit: bf},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, "How the withdrawal mix is calculated") {
			t.Errorf("expected disclosure summary; got: %s", truncate(out, 1200))
		}
		if !strings.Contains(out, "IRMAA keys off your MAGI") {
			t.Errorf("explainer body text must be preserved inside the disclosure")
		}
	})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/whatif/ -run TestBudgetAnalysis_Render -v`
Expected: FAIL on both new subtests.

- [ ] **Step 3: Implement the header + slider**

In `whatif-budget-steady-state`, replace the `<h4>`:

```html
        <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300">
            At Year <span id="steady-state-year-display">{{printf "%.0f" .Analysis.BudgetFit.SteadyStateYear}}</span>
            <span class="text-gray-400 dark:text-gray-500 font-normal">· Age <span id="steady-state-age-display">{{printf "%.0f" (add .Settings.CurrentAge .Analysis.BudgetFit.SteadyStateYear)}}</span></span>
        </h4>
```

And extend the range input: add `data-base-age="{{.Settings.CurrentAge}}"` and replace its `oninput` with:

```html
                oninput="document.getElementById('steady-state-year-display').textContent = this.value; document.getElementById('steady-state-age-display').textContent = Number(this.dataset.baseAge) + Number(this.value);"
```

(If `add` returns a non-float for int+float64 inputs, check its definition in `internal/templates/render.go` — it goes through `toFloat`, so `printf "%.0f"` is correct.)

- [ ] **Step 4: Implement the disclosure**

Replace the gap>0 explainer paragraph (the long italic block starting "Each row is the after-tax contribution…") with:

```html
        {{if gt .Analysis.BudgetFit.SteadyStateGap 0.0}}
        <details class="mt-2">
            <summary class="text-xs text-gray-400 dark:text-gray-500 cursor-pointer hover:text-gray-600 dark:hover:text-gray-300">How the withdrawal mix is calculated</summary>
            <p class="text-xs italic text-gray-500 dark:text-gray-400 mt-2">
                Each row is the after-tax contribution from that bucket, split proportionally to your portfolio allocation. The three rows sum to the gap. Tax-Deferred shows the gross withdrawal needed because part is lost to income tax; Taxable shows the gross because capital gains have accrued. Marginal rates cover this year's income tax only — IRMAA keys off your MAGI from two years prior, so any Medicare surcharge tier this withdrawal triggers arrives two years later and is not included in the gross-up.
            </p>
        </details>
        {{else if gt .Analysis.BudgetFit.SteadyStateRMD 0.0}}
```

(The two short `else` paragraphs stay as plain `<p>` — they're one line each.)

- [ ] **Step 5: Run tests, full verify, commit**

```bash
go test ./internal/handlers/whatif/ -run TestBudgetAnalysis_Render -v
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add web/templates/components/whatif/budget-analysis.html internal/handlers/whatif/budget_analysis_render_test.go
git commit -m "feat(whatif): steady-state slider shows age; withdrawal-mix explainer collapses"
```

---

### Task 6: Taxes & RMD — whole dollars, no redundant minus, highlight the RMD-start year

Three changes in `web/templates/components/whatif/tax-summary.html`: tiles drop `-` and cents; table cells drop cents; the row where RMDs begin gets an amber highlight + badge (the effective-rate jump at that age is the table's story).

**Files:**
- Modify: `web/templates/components/whatif/tax-summary.html`
- Test: `internal/handlers/whatif/tax_summary_render_test.go`

**Interfaces:**
- Consumes: `formatDollars` (Task 1); `.Analysis.RMD.StartAge` (int, `internal/models/whatif.go:995`); `.Analysis.Tax.YearlyTaxSummary` rows with `.Age` (int).
- Produces: unchanged define name `whatif-tax-summary`.

- [ ] **Step 1: Write the failing test**

Read `internal/handlers/whatif/tax_summary_render_test.go` first; extend (or add alongside its existing pattern):

```go
	t.Run("tiles are whole dollars with no minus sign", func(t *testing.T) {
		out := renderTaxSummary(t) // reuse/extract the file's existing fixture+render helper
		if strings.Contains(out, "-$") {
			t.Errorf("tax tiles must not render a minus sign (red styling already encodes cost): %s", truncate(out, 900))
		}
		if strings.Contains(out, ".75") || strings.Contains(out, ".88") {
			t.Errorf("tax figures must be whole dollars: %s", truncate(out, 900))
		}
	})
	t.Run("RMD start year row is badged", func(t *testing.T) {
		out := renderTaxSummaryWithRMDStartAge(t, 73) // fixture: YearlyTaxSummary containing an Age-73 row, Analysis.RMD.StartAge = 73
		if !strings.Contains(out, "RMDs begin") {
			t.Errorf("expected the age-73 row to carry the RMDs-begin badge: %s", truncate(out, 1200))
		}
	})
```

Concretely: if the existing test builds its fixture inline, mirror it — construct `.Analysis.Tax` with a `YearlyTaxSummary` slice containing ages 72 and 73 and monetary values with cents (e.g. `FederalTax: 8575.91`), plus `.Analysis.RMD = &models.RMDAnalysis{StartAge: 73}` (use the actual struct name from `internal/models/whatif.go` around line 994). The tile assertion needs `TotalTaxPaid: 1624993.75` so `.75` would appear if cents leak.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/whatif/ -run TestTaxSummary -v`
Expected: FAIL — output contains `-$` and cents; no badge.

- [ ] **Step 3: Implement**

In `tax-summary.html`:

1. Tiles (lines 13/17/21): `-{{formatMoney $t.TotalTaxPaid}}` → `{{formatDollars $t.TotalTaxPaid}}` (same for Federal and State). Colors unchanged (rose).
2. Table cells (lines 49-52): `{{formatMoney .TaxableIncome}}` → `{{formatDollars .TaxableIncome}}`, and same for `.FederalTax`, `.StateTax`, `.TotalTax`.
3. RMD highlight — before the `{{range $t.YearlyTaxSummary}}` add:

```html
            {{$rmdStartAge := -1}}
            {{if $.Analysis.RMD}}{{$rmdStartAge = $.Analysis.RMD.StartAge}}{{end}}
```

and change the row + Age cell:

```html
                <tr class="text-gray-700 dark:text-gray-300 {{if eq .Age $rmdStartAge}}bg-amber-50 dark:bg-amber-900/20{{end}}">
                    <td class="py-2 font-medium whitespace-nowrap">{{.Age}}{{if eq .Age $rmdStartAge}}<span class="ml-1.5 inline-block rounded bg-amber-100 dark:bg-amber-800/60 px-1.5 py-0.5 text-[10px] font-semibold text-amber-700 dark:text-amber-300 align-middle">RMDs begin</span>{{end}}</td>
```

If `.Age` and `StartAge` types differ (int vs int64), coerce with `eq (toFloat .Age) (toFloat $rmdStartAge)`.

4. Update any existing assertions in `tax_summary_render_test.go` that expect `-$` or cents to the new whole-dollar strings.

- [ ] **Step 4: Run tests, full verify, commit**

```bash
go test ./internal/handlers/whatif/ -run TestTaxSummary -v
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add web/templates/components/whatif/tax-summary.html internal/handlers/whatif/tax_summary_render_test.go
git commit -m "fix(whatif): tax summary whole dollars, no double negative, RMD-start row badge"
```

---

### Task 7: Year-by-Year table — sticky Year column

The 13-column table scrolls horizontally; the Year column scrolls out of view.

**Files:**
- Modify: `web/templates/components/whatif/projection-breakdown.html`
- Test: none beyond existing render tests (pure CSS classes); verified in Task 11's browser pass.

- [ ] **Step 1: Implement**

In `projection-breakdown.html`:

Header `<th class="py-2.5 pr-4 pl-3">Year</th>` (line 16) →

```html
                    <th class="py-2.5 pr-4 pl-3 sticky left-0 z-20 bg-gray-50 dark:bg-gray-900/95">Year</th>
```

Body year cell (line 44) →

```html
                    <td class="py-2.5 pr-4 pl-3 font-medium text-gray-700 dark:text-gray-200 sticky left-0 z-10 bg-white dark:bg-gray-800 border-r border-gray-100 dark:border-gray-700/60">{{$ys.Year}}</td>
```

Note: the sticky cell needs an opaque background; the odd/even row striping will not show through it — that's the accepted tradeoff (the divider border keeps rows scannable). The guardrail `border-l-4` accent lives on the `<tr>`, and a left-sticky first cell covers it while scrolled — move the accent onto the year `<td>` instead: take the `{{$accent}}` class off the `<tr>` and append `{{$accent}}` to the year `<td>`'s class list (keep the `title` on the `<tr>`).

- [ ] **Step 2: Verify render tests still pass, commit**

```bash
go build ./... && go vet ./... && go test ./internal/handlers/whatif/ && staticcheck ./...
git add web/templates/components/whatif/projection-breakdown.html
git commit -m "feat(whatif): sticky Year column in year-by-year projection table"
```

---

### Task 8: Projection chart — key-event labels stop clipping at the top edge

"Medicare: Christine" renders half-cut against the plot's top border. Two-part fix in `buildProjectionChartData` (`internal/handlers/whatif/handlers.go:444-558`): let event text draw outside the clip area, and give the y-axis headroom when events exist.

**Files:**
- Modify: `internal/handlers/whatif/handlers.go`
- Test: `internal/handlers/whatif/chart_helpers_test.go`

**Interfaces:**
- Consumes: `sampleProjectionForChart()` fixture already in `chart_helpers_test.go`.
- Produces: events trace gains `"cliponaxis": false`; layout yaxis gains `"range": []float64{0, maxBalance * 1.18}` when events exist and maxBalance > 0.

- [ ] **Step 1: Write the failing test**

Add to `chart_helpers_test.go` (reuse the settings fixture from `TestBuildProjectionChartData_AddsKeyEventMarkers`):

```go
func TestBuildProjectionChartData_EventLabelsGetHeadroom(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 70
	settings.ProjectionYears = 15
	settings.IncomeSources = []models.IncomeSource{{Name: "Social Security", Amount: 2000, StartMonth: 24}}
	projection := sampleProjectionForChart()

	chartData := buildProjectionChartData(settings, projection, "nominal")

	traces := chartData["data"].([]map[string]interface{})
	if len(traces) < 2 {
		t.Fatalf("expected event trace, got %d traces", len(traces))
	}
	if clip, ok := traces[1]["cliponaxis"].(bool); !ok || clip {
		t.Errorf("event trace cliponaxis = %v, want false so labels can render at the top edge", traces[1]["cliponaxis"])
	}

	layout := chartData["layout"].(map[string]interface{})
	yaxis := layout["yaxis"].(map[string]interface{})
	rng, ok := yaxis["range"].([]float64)
	if !ok || len(rng) != 2 {
		t.Fatalf("expected explicit y-axis range for headroom, got %#v", yaxis["range"])
	}
	maxBalance := 0.0
	for _, m := range projection.Months {
		if m.PortfolioBalance > maxBalance {
			maxBalance = m.PortfolioBalance
		}
	}
	if rng[0] != 0 || rng[1] <= maxBalance {
		t.Errorf("y range = %v, want [0, >%v] headroom above the peak balance", rng, maxBalance)
	}
}

func TestBuildProjectionChartData_NoEventsMeansAutoRange(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.IncomeSources = nil
	settings.HealthcarePersons = nil
	settings.ScenarioChain = nil
	settings.CurrentAge = 80 // past RMD start; no RMD event ahead
	projection := sampleProjectionForChart()

	chartData := buildProjectionChartData(settings, projection, "nominal")
	layout := chartData["layout"].(map[string]interface{})
	yaxis := layout["yaxis"].(map[string]interface{})
	if _, present := yaxis["range"]; present {
		t.Errorf("without events the y-axis should keep Plotly auto-range")
	}
}
```

If the no-events fixture still produces an event (check `buildProjectionChartEvents` output in the failure message), adjust the settings until `len(events) == 0` — the intent is one test with events, one without.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/whatif/ -run TestBuildProjectionChartData -v`
Expected: FAIL — no `cliponaxis` key, no `range` key.

- [ ] **Step 3: Implement**

In `buildProjectionChartData`:

1. In the events trace map (after `"hoverinfo": "skip",`), add:

```go
			"cliponaxis": false,
```

2. Extract the yaxis map into a variable before the return so it can be extended:

```go
	yaxis := map[string]interface{}{
		"title":      yAxisTitle,
		"tickformat": "$,.0f",
	}
	if len(events) > 0 && maxBalance > 0 {
		// Headroom so top-of-curve event labels don't clip at the plot edge.
		yaxis["range"] = []float64{0, maxBalance * 1.18}
	}
```

and use `"yaxis": yaxis` in the returned layout map. Note `events` is currently scoped inside the `if len(events) > 0` block builder — it is declared at function scope (`events := buildProjectionChartEvents(...)`), so it's available.

- [ ] **Step 4: Run tests, full verify, commit**

```bash
go test ./internal/handlers/whatif/ -run TestBuildProjectionChartData -v
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add internal/handlers/whatif/handlers.go internal/handlers/whatif/chart_helpers_test.go
git commit -m "fix(whatif): projection chart event labels get headroom instead of clipping"
```

---

### Task 9: Mobile navigation — hamburger menu in the site header

At 390px the nav links overflow and clip with no fallback. Add a standard disclosure hamburger; desktop unchanged.

**Files:**
- Modify: `web/templates/layouts/base.html`
- Test: manual browser verification in Task 11 (base layout has no render-test harness; the whatif render tests exercise `whatif-content`, not `base`).

- [ ] **Step 1: Implement the markup**

In `web/templates/layouts/base.html`, restructure the nav (lines 67-122):

1. On the links container (line 77) change `class="flex items-center space-x-6"` to:

```html
                <div class="hidden md:flex items-center space-x-6">
```

2. After that container's closing `</div>` (after the theme-toggle button block, line 119), still inside the `flex items-center justify-between h-16` row, add a mobile cluster:

```html
                <div class="flex items-center gap-1 md:hidden">
                    <button id="theme-toggle-mobile" class="p-2 rounded-md hover:bg-white/10 transition-colors" title="Toggle dark mode">
                        <svg class="w-5 h-5 hidden dark:block" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z"></path></svg>
                        <svg class="w-5 h-5 block dark:hidden" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z"></path></svg>
                    </button>
                    <button id="mobile-nav-toggle" class="p-2 rounded-md hover:bg-white/10 transition-colors" aria-label="Open navigation menu" aria-expanded="false" aria-controls="mobile-nav">
                        <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16"></path></svg>
                    </button>
                </div>
```

3. After the `h-16` row's closing `</div>` but before `</div></nav>`, add the dropdown panel (repeat the same links — the desktop list is `hidden` on mobile, so no duplication is visible):

```html
            <div id="mobile-nav" class="hidden md:hidden pb-3 space-y-1">
                <a href="/dashboard" class="block px-3 py-2 rounded-md text-sm font-medium hover:bg-white/10 {{if eq .ActiveTab "dashboard"}}bg-white/20{{end}}">Dashboard</a>
                <a href="/explorer" class="block px-3 py-2 rounded-md text-sm font-medium hover:bg-white/10 {{if eq .ActiveTab "explorer"}}bg-white/20{{end}}">Explorer</a>
                <a href="/whatif" class="block px-3 py-2 rounded-md text-sm font-medium hover:bg-white/10 {{if eq .ActiveTab "whatif"}}bg-white/20{{end}}">What-If</a>
                <a href="/insights" class="block px-3 py-2 rounded-md text-sm font-medium hover:bg-white/10 {{if eq .ActiveTab "insights"}}bg-white/20{{end}}">Insights</a>
                <a href="/major-expenses" class="block px-3 py-2 rounded-md text-sm font-medium hover:bg-white/10 {{if eq .ActiveTab "major-expenses"}}bg-white/20{{end}}">Major Expenses</a>
                {{if .UnresolvedDuplicateCount}}
                <a href="/duplicates" class="block px-3 py-2 rounded-md text-sm font-medium hover:bg-white/10 {{if eq .ActiveTab "duplicates"}}bg-white/20{{end}}">Duplicates <span class="ml-1 inline-flex items-center justify-center px-2 py-0.5 text-xs font-semibold rounded-full bg-amber-500 text-white">{{.UnresolvedDuplicateCount}}</span></a>
                {{end}}
                <a href="/filemanager" class="block px-3 py-2 rounded-md text-sm font-medium hover:bg-white/10 {{if eq .ActiveTab "filemanager"}}bg-white/20{{end}}">File Manager</a>
            </div>
```

4. Also shrink the brand so it can't wrap: on the `<a href="/">` (line 72) change `text-2xl` to `text-xl md:text-2xl` and add `whitespace-nowrap`.

- [ ] **Step 2: Wire the JS**

In the inline script at the bottom of `base.html`, the theme-toggle handler is bound to one id. Generalize: replace the `var themeToggle = document.getElementById('theme-toggle'); if (themeToggle && !themeToggle._listenerAttached) {...}` block with:

```javascript
            ['theme-toggle', 'theme-toggle-mobile'].forEach(function (id) {
                var btn = document.getElementById(id);
                if (!btn || btn._listenerAttached) return;
                btn._listenerAttached = true;
                btn.addEventListener('click', function () {
                    var html = document.documentElement;
                    var isDark = html.classList.contains('dark');
                    if (isDark) {
                        html.classList.remove('dark');
                        html.classList.add('light');
                        localStorage.setItem('theme', 'light');
                    } else {
                        html.classList.remove('light');
                        html.classList.add('dark');
                        localStorage.setItem('theme', 'dark');
                    }
                    window.dispatchEvent(new CustomEvent('themechange', { detail: { dark: !isDark } }));
                });
            });

            var navToggle = document.getElementById('mobile-nav-toggle');
            var mobileNav = document.getElementById('mobile-nav');
            if (navToggle && mobileNav && !navToggle._listenerAttached) {
                navToggle._listenerAttached = true;
                navToggle.addEventListener('click', function () {
                    var open = mobileNav.classList.toggle('hidden') === false;
                    navToggle.setAttribute('aria-expanded', open ? 'true' : 'false');
                });
            }
```

- [ ] **Step 3: Verify and commit**

Templates are parsed at startup, so a render smoke check is enough here (full browser pass happens in Task 11):

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add web/templates/layouts/base.html
git commit -m "feat(nav): responsive hamburger menu; header no longer clips on phones"
```

---

### Task 10: What-if mobile ordering + light-mode input borders

Two small template fixes: (a) on phones the expanded settings column stacks above the verdict, burying results below several screens of form; (b) the property-tax inputs have `border-gray-300` without the `border` class, so no border renders in light mode.

**Files:**
- Modify: `web/templates/pages/whatif.html` (grid children, lines 70 and 125)
- Modify: `web/templates/components/whatif/portfolio-settings.html` (lines 70 and 78)
- Test: `internal/handlers/whatif/tabs_render_test.go` (extend the whatif-content test)

- [ ] **Step 1: Write the failing test**

In the existing `whatif-content` render test in `tabs_render_test.go` (the one asserting settings-group labels, ~line 101), add:

```go
	if !strings.Contains(out, `id="whatif-settings-col" class="order-2 lg:order-none lg:col-span-2 space-y-4"`) {
		t.Errorf("settings column must order after results on small screens")
	}
	if !strings.Contains(out, `class="order-1 lg:order-none lg:col-span-4 space-y-4" id="whatif-results"`) {
		t.Errorf("results column must order before settings on small screens")
	}
```

(If attribute ordering in the template differs after editing, match the assertion to the actual attribute order you write — the point is asserting the `order-*` classes are present on both columns.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/whatif/ -run TestWhatIfContent -v` (use the actual test name containing the whatif-content render)
Expected: FAIL — no `order-2` / `order-1` classes yet.

- [ ] **Step 3: Implement**

In `web/templates/pages/whatif.html`:

Line 70: `<div id="whatif-settings-col" class="lg:col-span-2 space-y-4">` →

```html
    <div id="whatif-settings-col" class="order-2 lg:order-none lg:col-span-2 space-y-4">
```

Line 125: `<div class="lg:col-span-4 space-y-4" id="whatif-results">` →

```html
    <div class="order-1 lg:order-none lg:col-span-4 space-y-4" id="whatif-results">
```

(`whatif-tabs.js` toggles `lg:col-span-*` classes on these nodes by class name — it uses `classList.toggle`, which is order-independent, so adding classes is safe.)

In `web/templates/components/whatif/portfolio-settings.html`, add `border` to both number inputs (lines 70 and 78):

```html
                    class="mt-1 block w-full rounded-md border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm">
```

Then sweep for the same bug elsewhere in the what-if templates: `grep -rn 'rounded-md border-gray-300' web/templates/components/whatif/` — any hit lacking a standalone `border` class gets the same one-word fix.

- [ ] **Step 4: Run tests, full verify, commit**

```bash
go test ./internal/handlers/whatif/ -v
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add web/templates/pages/whatif.html web/templates/components/whatif/portfolio-settings.html
git commit -m "fix(whatif): results-first ordering on phones; visible input borders in light mode"
```

---

### Task 11: End-to-end browser verification

Every prior task was verified by render tests; this pass confirms the composed page in a real browser, both themes, both widths — using the checklist the whatif-verify skill exists for.

- [ ] **Step 1: Launch**

```bash
scripts/whatif-verify.sh start
```

Expected: `READY http://localhost:8099`

- [ ] **Step 2: Desktop pass (1440×900, dark), via Playwright MCP**

Load `http://localhost:8099/whatif` and verify:
- Verdict bar shows FIVE stats (Gap, Monte Carlo, Req. Rate, Est. Taxes, End Balance); taxes value is whole-dollar, red/rose, no minus sign; with the sample data (62.2% MC) the detail reads "covers the median path — 38% of market simulations fall short".
- Overview tab has NO KPI tile row; it starts with the Portfolio Longevity chart; the "Medicare: …" event label is fully visible (not clipped by the top edge).
- Cash Flow tab: exactly one Monthly Gap tile while the slider is at year 0; "Taxes & Deductions" header present; drag the slider to a later year → "At Year N · Age M" updates and the year-N block appears with its own gap tiles; "How the withdrawal mix is calculated" is a collapsed disclosure that opens.
- Cash Flow year-by-year table: scroll it horizontally → Year column stays pinned.
- Taxes & RMD tab: tiles/table whole dollars, still red; the RMD-start age row is amber-highlighted with an "RMDs begin" badge.
- Tab scenario survives partial refresh: switch tab, move a slider, confirm the tab didn't reset (existing regression).

- [ ] **Step 3: Light-mode pass**

Toggle the theme; confirm the verdict strip, budget groups, tax badge, and the property-tax inputs (now bordered) all render sensibly in light mode. Cost items remain red/rose.

- [ ] **Step 4: Phone pass (390×844)**

Resize; verify: header shows brand + moon + hamburger with nothing clipped; hamburger opens the link panel and navigates; on `/whatif` the verdict bar and tabs appear ABOVE the settings accordions.

- [ ] **Step 5: Teardown, final suite, ship**

```bash
scripts/whatif-verify.sh stop
go build ./... && go vet ./... && go test ./... && staticcheck ./...
```

Fix anything the browser pass surfaced (with a test where feasible), commit, then use superpowers:finishing-a-development-branch to decide merge/PR.

---

## Self-Review Notes

- **Coverage:** survey findings → tasks: verdict contradiction (T2), duplicate KPIs (T3), Cash Flow duplicate block + taxes-under-income + slider labeling + explainer density (T4, T5), number formatting/sign convention (T1, T3, T6), RMD-story highlight (T6), wide table (T7), chart clipping (T8), mobile nav + ordering (T9, T10), light-mode inputs (T10), color-only encoding (sr-only additions in T3). Deferred items are listed up top by design.
- **Known fixture risk:** field names in `budgetFitFixture` and the tax/RMD analysis structs were transcribed from template usage; implementers must reconcile against `internal/models/whatif.go` if the compiler disagrees — the template usages quoted in each task are the source of truth.
- **Type consistency:** `formatDollars` = `func(float64) string` (T1) matches all call sites in T3/T6; `VerdictView.TotalTaxes/EndBalance` (T3) match the render test literals; `data-base-age` + `steady-state-age-display` ids match between markup and JS in T5.
