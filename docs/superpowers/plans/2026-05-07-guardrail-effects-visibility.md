# Guardrail Effects Visibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the simple drop/rise guardrail's effect on retirement spending visible in the What-If page — per-year planned vs. adjusted spending in the breakdown table, dollar deltas in the events panel, and an opt-in counterfactual chart overlay.

**Architecture:** Three independently shippable slices, all reading the same projection result. Slice A enriches the projection data structures (zero behavior change). Slice B surfaces the new fields in two existing templates. Slice C adds a new chart endpoint and a chart-overlay toggle that runs a second projection with guardrails disabled.

**Tech Stack:** Go 1.x, chi router, html/template, vanilla JS + Plotly for charts.

**Spec:** `docs/superpowers/specs/2026-05-07-guardrail-effects-visibility-design.md`

---

## File Structure

| File | Role | Slice |
|---|---|---|
| `internal/models/whatif.go` | Add 5 new fields to `ProjectionMonth`, `ProjectionYearSummary`, `GuardrailEvent` | A |
| `internal/services/retirement/calculator.go` | Populate new fields in monthly loop, yearly aggregator, and guardrail event emitter | A |
| `internal/services/retirement/projection_planned_test.go` | New unit-test file for planned-vs-adjusted invariants | A |
| `web/templates/components/whatif/projection-breakdown.html` | Stacked Spending cell + multiplier badge + row-fire accent | B |
| `web/templates/components/whatif/guardrails.html` | Add `$/mo → $/mo` to each event line | B |
| `internal/handlers/whatif/handlers.go` | New `handleWhatIfProjectionChartNoGuardrails`, route registration, optional 1-entry cache | C |
| `internal/handlers/whatif/handlers_test.go` | Tests for the new endpoint | C |
| `web/templates/components/whatif/projection-chart.html` | Add "Compare without guardrails" toggle button | C |
| `web/static/js/charts.js` | Wire up the toggle: fetch overlay, add dashed series, update caption | C |

---

## Slice A — Data Plumbing (no user-visible change)

### Task 1: Extend model types

**Files:**
- Modify: `internal/models/whatif.go:865-906` (`ProjectionMonth`, `ProjectionYearSummary`, `GuardrailEvent` blocks)

- [ ] **Step 1: Add the new fields**

In `internal/models/whatif.go`, locate the `ProjectionMonth` struct (around line 866). Inside the struct, after the existing `WithdrawalFromRoth` field and before the closing brace at line 895, add:

```go
	// Guardrail visibility (F-079)
	PlannedLivingExpenses float64 `json:"planned_living_expenses,omitempty"` // Pre-guardrail-multiplier living expense for the month
	GuardrailMultiplier   float64 `json:"guardrail_multiplier,omitempty"`    // Active guardrail spending multiplier (1.0 if disabled)
```

Locate `ProjectionYearSummary` (around line 909). After the existing `CumulativeInflation` field and before the closing brace at line 924, add:

```go
	// Guardrail visibility (F-079)
	PlannedExpenses     float64 `json:"planned_expenses,omitempty"`     // Total expenses recomputed with guardrail multiplier locked at 1.0
	GuardrailMultiplier float64 `json:"guardrail_multiplier,omitempty"` // Multiplier in effect at year-end (1.0 if disabled)
```

Locate `GuardrailEvent` (around line 659). Replace the entire struct with:

```go
// GuardrailEvent records when a guardrail triggered during projection
type GuardrailEvent struct {
	Year                  int     `json:"year"`
	Type                  string  `json:"type"`       // "cut" or "raise"
	Multiplier            float64 `json:"multiplier"` // New spending multiplier
	Portfolio             float64 `json:"portfolio"`  // Portfolio value at time
	MonthlySpendingBefore float64 `json:"monthly_spending_before,omitempty"`
	MonthlySpendingAfter  float64 `json:"monthly_spending_after,omitempty"`
}
```

- [ ] **Step 2: Compile to confirm types parse**

Run: `go build ./...`
Expected: PASS (no errors).

- [ ] **Step 3: Commit**

```bash
git add internal/models/whatif.go
git commit -m "feat(whatif): F-079 add guardrail-visibility fields to projection models"
```

---

### Task 2: Populate per-month planned-spending and multiplier fields

**Files:**
- Modify: `internal/services/retirement/calculator.go:1141-1175, 1305-1332` (monthly projection loop)
- Test: `internal/services/retirement/projection_planned_test.go` (new file)

- [ ] **Step 1: Write the failing test**

Create `internal/services/retirement/projection_planned_test.go` with:

```go
package retirement

import (
	"budget2/internal/models"
	"testing"
)

// With guardrails disabled, every ProjectionMonth must have:
//   GuardrailMultiplier == 1.0
//   PlannedLivingExpenses > 0 and finite
//   PlannedLivingExpenses == GeneralExpenses (they describe the same value)
func TestProjection_PlannedFields_NoGuardrails(t *testing.T) {
	s := defaultSettingsForTest()
	s.Guardrails = nil // explicitly disabled

	calc := NewCalculator(s)
	result := calc.RunFullAnalysis()

	if result.Projection == nil || len(result.Projection.Months) == 0 {
		t.Fatalf("expected projection months, got none")
	}

	for i, m := range result.Projection.Months {
		if m.GuardrailMultiplier != 1.0 {
			t.Errorf("month %d: GuardrailMultiplier = %v, want 1.0", i, m.GuardrailMultiplier)
		}
		if m.PlannedLivingExpenses <= 0 {
			t.Errorf("month %d: PlannedLivingExpenses = %v, want > 0", i, m.PlannedLivingExpenses)
		}
		if !almostEqual(m.PlannedLivingExpenses, m.GeneralExpenses) {
			t.Errorf("month %d: PlannedLivingExpenses (%v) != GeneralExpenses (%v)",
				i, m.PlannedLivingExpenses, m.GeneralExpenses)
		}
	}
}

// With guardrails enabled and a forced cut, post-cut months must show:
//   GuardrailMultiplier < 1.0
//   PlannedLivingExpenses unchanged (planned line is multiplier-independent)
func TestProjection_PlannedFields_WithCut(t *testing.T) {
	s := defaultSettingsForTest()
	s.Guardrails = &models.GuardrailConfig{
		Enabled:         true,
		FloorDropPct:    1,  // hair-trigger so a cut fires near year 1
		FloorCutPct:     10,
		CeilingRisePct:  500, // disabled in practice
		CeilingRaisePct: 10,
		MinSpendingPct:  50,
		MaxSpendingPct:  150,
	}

	calc := NewCalculator(s)
	result := calc.RunFullAnalysis()

	sawAdjusted := false
	for _, m := range result.Projection.Months {
		if m.GuardrailMultiplier < 1.0 {
			sawAdjusted = true
			if m.PlannedLivingExpenses <= 0 {
				t.Fatalf("PlannedLivingExpenses must remain > 0 even after a cut, got %v", m.PlannedLivingExpenses)
			}
		}
	}
	if !sawAdjusted {
		t.Fatalf("expected at least one month with GuardrailMultiplier < 1.0 given hair-trigger cut")
	}
}
```

`defaultSettingsForTest` is defined in `internal/services/retirement/calculator_coverage_test.go:10` and is reused by other tests in this package. Use it as-is.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/services/retirement/ -run 'TestProjection_PlannedFields' -v`
Expected: FAIL — `GuardrailMultiplier = 0, want 1.0` (the new fields are zero-valued because no code populates them yet).

- [ ] **Step 3: Wire the per-month fields in the projection loop**

In `internal/services/retirement/calculator.go`, locate the block at line 1171–1175 that reads:

```go
		// Apply guardrail spending multiplier
		adjustedLivingExpenses := currentLivingExpenses
		if grState != nil {
			adjustedLivingExpenses *= grState.multiplier()
		}
```

Capture the active multiplier into a local variable so we can both apply it and serialize it:

```go
		// Apply guardrail spending multiplier
		activeMultiplier := 1.0
		if grState != nil {
			activeMultiplier = grState.multiplier()
		}
		adjustedLivingExpenses := currentLivingExpenses * activeMultiplier
```

Then locate the `projection = append(projection, models.ProjectionMonth{...})` block at lines 1305–1332. Add two new fields just before the closing brace `})`:

```go
			PlannedLivingExpenses:     currentLivingExpenses,
			GuardrailMultiplier:       activeMultiplier,
```

The exact field placement: after `WithdrawalFromRoth: cashFlow.WithdrawalFromRoth,` (the current last line at 1331) and before `})`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/ -run 'TestProjection_PlannedFields' -v`
Expected: PASS.

- [ ] **Step 5: Run the full retirement test suite for regressions**

Run: `go test ./internal/services/retirement/ -count=1`
Expected: PASS (existing guardrail tests still pass; new fields are additive).

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/calculator.go internal/services/retirement/projection_planned_test.go
git commit -m "feat(whatif): F-079 populate per-month planned-spending and guardrail multiplier"
```

---

### Task 3: Populate yearly summary and guardrail event fields

**Files:**
- Modify: `internal/services/retirement/calculator.go:1058-1067, 1141-1175, 1162-1167, 1269-1276`
- Test: append to `internal/services/retirement/projection_planned_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/services/retirement/projection_planned_test.go`:

```go
// YearSummary.PlannedExpenses must equal Expenses when guardrails are disabled.
// When a cut fires mid-projection, PlannedExpenses must exceed Expenses for that year.
func TestProjectionYear_PlannedExpenses_NoGuardrails(t *testing.T) {
	s := defaultSettingsForTest()
	s.Guardrails = nil

	result := NewCalculator(s).RunFullAnalysis()
	for i, ys := range result.Projection.YearlySummaries {
		if !almostEqual(ys.PlannedExpenses, ys.Expenses) {
			t.Errorf("year %d: PlannedExpenses (%v) != Expenses (%v) when guardrails disabled",
				i, ys.PlannedExpenses, ys.Expenses)
		}
		if ys.GuardrailMultiplier != 1.0 {
			t.Errorf("year %d: GuardrailMultiplier = %v, want 1.0", i, ys.GuardrailMultiplier)
		}
	}
}

func TestProjectionYear_PlannedExpenses_WithCut(t *testing.T) {
	s := defaultSettingsForTest()
	s.Guardrails = &models.GuardrailConfig{
		Enabled: true, FloorDropPct: 1, FloorCutPct: 10,
		CeilingRisePct: 500, CeilingRaisePct: 10,
		MinSpendingPct: 50, MaxSpendingPct: 150,
	}

	result := NewCalculator(s).RunFullAnalysis()
	sawCutYear := false
	for _, ys := range result.Projection.YearlySummaries {
		if ys.GuardrailMultiplier < 1.0 {
			sawCutYear = true
			if ys.PlannedExpenses <= ys.Expenses {
				t.Errorf("year %d: planned (%v) must exceed adjusted (%v) when multiplier < 1.0",
					ys.Year, ys.PlannedExpenses, ys.Expenses)
			}
		}
	}
	if !sawCutYear {
		t.Fatalf("expected at least one year with GuardrailMultiplier < 1.0")
	}
}

// GuardrailEvent.MonthlySpendingBefore/After must be populated and consistent with the multiplier change.
func TestGuardrailEvent_DollarFields(t *testing.T) {
	s := defaultSettingsForTest()
	s.Guardrails = &models.GuardrailConfig{
		Enabled: true, FloorDropPct: 1, FloorCutPct: 10,
		CeilingRisePct: 500, CeilingRaisePct: 10,
		MinSpendingPct: 50, MaxSpendingPct: 150,
	}

	result := NewCalculator(s).RunFullAnalysis()
	if len(result.Projection.GuardrailEvents) == 0 {
		t.Fatalf("expected at least one guardrail event")
	}
	for _, e := range result.Projection.GuardrailEvents {
		if e.MonthlySpendingBefore <= 0 {
			t.Errorf("year %d: MonthlySpendingBefore = %v, want > 0", e.Year, e.MonthlySpendingBefore)
		}
		if e.MonthlySpendingAfter <= 0 {
			t.Errorf("year %d: MonthlySpendingAfter = %v, want > 0", e.Year, e.MonthlySpendingAfter)
		}
		if e.Type == "cut" && e.MonthlySpendingAfter >= e.MonthlySpendingBefore {
			t.Errorf("year %d: cut should reduce spending, got %v -> %v", e.Year, e.MonthlySpendingBefore, e.MonthlySpendingAfter)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/services/retirement/ -run 'TestProjectionYear_PlannedExpenses|TestGuardrailEvent_DollarFields' -v`
Expected: FAIL — `PlannedExpenses (0)` and `MonthlySpendingBefore = 0`.

- [ ] **Step 3: Aggregate planned-expenses per year**

In `internal/services/retirement/calculator.go`, locate line 1179:

```go
		totalExpenses := adjustedLivingExpenses + activeHealthcare + bigTicketExpenseThisMonth
```

Immediately above it, compute the planned counterpart:

```go
		plannedTotalExpenses := currentLivingExpenses + activeHealthcare + bigTicketExpenseThisMonth
```

Locate the year-aggregation block at lines 1271–1275:

```go
		currentYearSummary.Growth += totalGrowth
		currentYearSummary.GrossIncome += grossIncome
		currentYearSummary.Taxes += taxesPaid
		currentYearSummary.Expenses += totalExpenses
		currentYearSummary.Withdrawals += cashFlow.ActualWithdrawal
```

After `currentYearSummary.Expenses += totalExpenses`, also accumulate the planned:

```go
		currentYearSummary.PlannedExpenses += plannedTotalExpenses
```

Note: `totalExpenses` later picks up `monthResult.IRMAAExpense` via `+=` at line 1255 *before* this aggregation runs. To keep planned ↔ adjusted comparable (IRMAA is identical in both worlds), also add IRMAA into `plannedTotalExpenses` at the same point:

In the block around line 1255 (`totalExpenses += monthResult.IRMAAExpense`), add:

```go
		totalExpenses += monthResult.IRMAAExpense
		plannedTotalExpenses += monthResult.IRMAAExpense
```

(Move the `plannedTotalExpenses` declaration from above line 1179 to *before* the `totalExpenses += monthResult.IRMAAExpense` line so both can be incremented together. Initialize it as `plannedTotalExpenses := currentLivingExpenses + activeHealthcare + bigTicketExpenseThisMonth` at the same point where `totalExpenses` is initialized.)

- [ ] **Step 4: Set per-year multiplier in the finalizer**

Locate the `finalizeCurrentYear` closure at lines 1058–1067:

```go
	finalizeCurrentYear := func(month models.ProjectionMonth) {
		currentYearSummary.MAGI = currentYearTaxSnapshot.AnnualMAGI
		currentYearSummary.NIIT = currentYearTaxSnapshot.AnnualNIIT
		currentYearSummary.IRMAA = currentYearTaxSnapshot.AnnualIRMAA
		currentYearSummary.TaxableSocialSecurityPct = currentYearTaxSnapshot.TaxableSocialSecurityPct
		currentYearSummary.EndingBalance = month.PortfolioBalance
		currentYearSummary.EndingBalanceReal = month.PortfolioBalanceReal
		currentYearSummary.CumulativeInflation = month.CumulativeInflation
		yearlySummaries = append(yearlySummaries, currentYearSummary)
	}
```

Add the multiplier line just before the `append`:

```go
		currentYearSummary.GuardrailMultiplier = month.GuardrailMultiplier
		yearlySummaries = append(yearlySummaries, currentYearSummary)
```

- [ ] **Step 5: Populate event before/after dollar fields**

Locate the guardrail-event emitter at lines 1151–1169:

```go
		// Evaluate spending guardrails at year boundaries
		if grState != nil && m%12 == 0 {
			totalPortfolio := taxDeferredBalance + taxableAccount.MarketValue + rothBalance
			prevMult := grState.multiplier()
			grState.evaluate(s.Guardrails, totalPortfolio)
			newMult := grState.multiplier()
			if newMult != prevMult {
				eventType := "cut"
				if newMult > prevMult {
					eventType = "raise"
				}
				guardrailEvents = append(guardrailEvents, models.GuardrailEvent{
					Year:       currentYear,
					Type:       eventType,
					Multiplier: newMult,
					Portfolio:  totalPortfolio,
				})
			}
		}
```

Replace the `guardrailEvents = append(...)` block with:

```go
				guardrailEvents = append(guardrailEvents, models.GuardrailEvent{
					Year:                  currentYear,
					Type:                  eventType,
					Multiplier:            newMult,
					Portfolio:             totalPortfolio,
					MonthlySpendingBefore: currentLivingExpenses * prevMult,
					MonthlySpendingAfter:  currentLivingExpenses * newMult,
				})
```

`currentLivingExpenses` here is already the planned monthly figure (post-phase × inflation, pre-multiplier), so multiplying by `prevMult`/`newMult` produces the actual monthly spending in dollars before and after the trigger.

- [ ] **Step 6: Run the new tests to verify they pass**

Run: `go test ./internal/services/retirement/ -run 'TestProjectionYear_PlannedExpenses|TestGuardrailEvent_DollarFields' -v`
Expected: PASS.

- [ ] **Step 7: Run the full retirement test suite for regressions**

Run: `go test ./internal/services/retirement/ -count=1`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/services/retirement/calculator.go internal/services/retirement/projection_planned_test.go
git commit -m "feat(whatif): F-079 populate yearly planned-expenses and event dollar fields"
```

---

## Slice B — UI surfacing

### Task 4: Enrich Year-by-Year table

**Files:**
- Modify: `web/templates/components/whatif/projection-breakdown.html:30-49`
- Test: `internal/handlers/whatif/handlers_test.go` (new function)

- [ ] **Step 1: Write the failing handler test**

Append to `internal/handlers/whatif/handlers_test.go` (at the end, before the final closing brace if any):

```go
// The breakdown table must surface the planned-vs-adjusted spending stack
// and the guardrail multiplier badge when guardrails are enabled and a cut fires.
func TestHandleWhatIf_BreakdownShowsGuardrailEffect(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	settings.Guardrails = &models.GuardrailConfig{
		Enabled: true, FloorDropPct: 1, FloorCutPct: 10,
		CeilingRisePct: 500, CeilingRaisePct: 10,
		MinSpendingPct: 50, MaxSpendingPct: 150,
	}
	if err := retirementMgr.Save(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	cache.mu.Lock()
	cache.hash = ""
	cache.mu.Unlock()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	// Multiplier badge text — printed only when mult ≠ 1.00
	if !strings.Contains(body, "×0.9") && !strings.Contains(body, "×0.90") {
		t.Errorf("expected ×0.9 multiplier badge in breakdown body, not found")
	}
	// Planned-spending suffix — relies on the data-planned-suffix marker class we add in the template
	if !strings.Contains(body, "data-planned-spending") {
		t.Errorf("expected data-planned-spending marker in breakdown body, not found")
	}
}
```

`handleWhatIf` is defined at `internal/handlers/whatif/handlers.go:556` and renders the full What-If page including the breakdown table.

- [ ] **Step 2: Run the test to verify failure**

Run: `go test ./internal/handlers/whatif/ -run 'TestHandleWhatIf_BreakdownShowsGuardrailEffect' -v`
Expected: FAIL — markers not found in body.

- [ ] **Step 3: Update the breakdown template**

Open `web/templates/components/whatif/projection-breakdown.html`. Locate the `<tbody>` row block at lines 31–48. Replace the entire `{{range .YearlySummaries}}…{{end}}` block with:

```html
                {{range $idx, $ys := .YearlySummaries}}
                {{$mult := $ys.GuardrailMultiplier}}
                {{$accent := ""}}
                {{$accentTitle := ""}}
                {{if and $mult (lt $mult 1.0)}}
                  {{$accent = "border-l-4 border-l-red-500"}}
                  {{$accentTitle = "Guardrail cut active this year"}}
                {{else if and $mult (gt $mult 1.0)}}
                  {{$accent = "border-l-4 border-l-green-500"}}
                  {{$accentTitle = "Guardrail raise active this year"}}
                {{end}}
                <tr title="{{$accentTitle}}" class="{{$accent}} border-b border-gray-100 align-top odd:bg-white even:bg-gray-50/70 dark:border-gray-700/60 dark:odd:bg-gray-800 dark:even:bg-gray-900/30">
                    <td class="py-2.5 pr-4 pl-3 font-medium text-gray-700 dark:text-gray-200">{{$ys.Year}}</td>
                    <td class="py-2.5 pr-4 whitespace-nowrap tabular-nums text-gray-600 dark:text-gray-300">{{formatMoney $ys.StartingBalance}}</td>
                    <td class="py-2.5 pr-4 whitespace-nowrap tabular-nums {{if ge $ys.Growth 0.0}}text-green-600 dark:text-green-400{{else}}text-red-600 dark:text-red-400{{end}}">{{formatMoney $ys.Growth}}</td>
                    <td class="py-2.5 pr-4 whitespace-nowrap tabular-nums text-gray-600 dark:text-gray-300">{{formatMoney $ys.GrossIncome}}</td>
                    <td class="py-2.5 pr-4 whitespace-nowrap tabular-nums text-gray-600 dark:text-gray-300">{{formatMoney $ys.MAGI}}</td>
                    <td class="py-2.5 pr-4 whitespace-nowrap tabular-nums text-red-600 dark:text-red-400">{{formatMoney $ys.Taxes}}</td>
                    <td class="py-2.5 pr-4 whitespace-nowrap tabular-nums text-red-500 dark:text-red-300">{{formatMoney $ys.NIIT}}</td>
                    <td class="py-2.5 pr-4 whitespace-nowrap tabular-nums text-gray-600 dark:text-gray-300">{{formatMoney $ys.IRMAA}}</td>
                    <td class="py-2.5 pr-4 whitespace-nowrap tabular-nums text-gray-600 dark:text-gray-300">{{printf "%.0f%%" $ys.TaxableSocialSecurityPct}}</td>
                    <td class="py-2.5 pr-4 whitespace-nowrap tabular-nums text-gray-600 dark:text-gray-300">
                        <div>{{formatMoney $ys.Expenses}}</div>
                        {{if and $mult (ne $mult 1.0) (gt $ys.PlannedExpenses 0.0)}}
                        <div data-planned-spending class="text-[11px] text-gray-400 dark:text-gray-500">
                            {{formatMoney $ys.PlannedExpenses}} ·
                            <span class="{{if lt $mult 1.0}}text-red-500 dark:text-red-300{{else}}text-green-600 dark:text-green-300{{end}}">×{{printf "%.2f" $mult}}</span>
                        </div>
                        {{end}}
                    </td>
                    <td class="py-2.5 pr-4 whitespace-nowrap tabular-nums text-amber-600 dark:text-amber-400">{{formatMoney $ys.Withdrawals}}</td>
                    <td class="py-2.5 pr-3">
                        <div class="whitespace-nowrap tabular-nums font-medium text-gray-800 dark:text-gray-100">{{formatMoney $ys.EndingBalance}}</div>
                        <div class="whitespace-nowrap text-xs tabular-nums text-sky-700 dark:text-sky-300">Real {{formatMoney $ys.EndingBalanceReal}}</div>
                    </td>
                </tr>
                {{end}}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/handlers/whatif/ -run 'TestHandleWhatIf_BreakdownShowsGuardrailEffect' -v`
Expected: PASS.

- [ ] **Step 5: Run the full handlers test suite**

Run: `go test ./internal/handlers/whatif/ -count=1`
Expected: PASS.

- [ ] **Step 6: Manual smoke (server already running or `make run`)**

Open `http://localhost:8080/whatif`. Enable guardrails with default thresholds (Drop 20 / Cut 10 / Rise 20 / Raise 10). Click Apply. In the Year-by-Year table, verify:
- Years where a guardrail fired show two stacked lines in the Spending column: adjusted on top, planned + multiplier badge underneath.
- The same rows have a colored left border (red for cut, green for raise).

If no guardrail fires with default values for the current settings, temporarily set Drop to 1 to force one, verify, then revert.

- [ ] **Step 7: Commit**

```bash
git add web/templates/components/whatif/projection-breakdown.html internal/handlers/whatif/handlers_test.go
git commit -m "feat(whatif): F-079 surface planned vs adjusted spending in breakdown table"
```

---

### Task 5: Enrich Guardrail Events panel

**Files:**
- Modify: `web/templates/components/whatif/guardrails.html:85-106` (the `whatif-guardrail-events` block)
- Test: `internal/handlers/whatif/handlers_test.go` (new function)

- [ ] **Step 1: Write the failing test**

Append to `internal/handlers/whatif/handlers_test.go`:

```go
func TestHandleWhatIf_EventsPanelShowsDollarDelta(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	settings.Guardrails = &models.GuardrailConfig{
		Enabled: true, FloorDropPct: 1, FloorCutPct: 10,
		CeilingRisePct: 500, CeilingRaisePct: 10,
		MinSpendingPct: 50, MaxSpendingPct: 150,
	}
	if err := retirementMgr.Save(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	cache.mu.Lock()
	cache.hash = ""
	cache.mu.Unlock()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "data-event-spending-delta") {
		t.Errorf("expected data-event-spending-delta marker in events panel, not found")
	}
}
```

- [ ] **Step 2: Run the test to verify failure**

Run: `go test ./internal/handlers/whatif/ -run 'TestHandleWhatIf_EventsPanelShowsDollarDelta' -v`
Expected: FAIL.

- [ ] **Step 3: Update the events template**

Open `web/templates/components/whatif/guardrails.html`. Locate the inner row at lines 91–101 of the `whatif-guardrail-events` block:

```html
        <div class="flex items-center justify-between text-sm py-1 px-2 rounded {{if eq .Type "cut"}}bg-red-50 dark:bg-red-900/20{{else}}bg-green-50 dark:bg-green-900/20{{end}}">
            <span class="text-gray-700 dark:text-gray-300">
                Year {{.Year}}:
                {{if eq .Type "cut"}}
                <span class="text-red-600 dark:text-red-400">Cut to {{printf "%.0f" (mul .Multiplier 100)}}%</span>
                {{else}}
                <span class="text-green-600 dark:text-green-400">Raise to {{printf "%.0f" (mul .Multiplier 100)}}%</span>
                {{end}}
            </span>
            <span class="text-xs text-gray-500 dark:text-gray-400">Portfolio: {{formatMoney .Portfolio}}</span>
        </div>
```

Replace it with:

```html
        <div class="flex flex-wrap items-center justify-between gap-2 text-sm py-1 px-2 rounded {{if eq .Type "cut"}}bg-red-50 dark:bg-red-900/20{{else}}bg-green-50 dark:bg-green-900/20{{end}}">
            <span class="text-gray-700 dark:text-gray-300">
                Year {{.Year}}:
                {{if eq .Type "cut"}}
                <span class="text-red-600 dark:text-red-400">Cut to {{printf "%.0f" (mul .Multiplier 100)}}%</span>
                {{else}}
                <span class="text-green-600 dark:text-green-400">Raise to {{printf "%.0f" (mul .Multiplier 100)}}%</span>
                {{end}}
            </span>
            {{if and (gt .MonthlySpendingBefore 0.0) (gt .MonthlySpendingAfter 0.0)}}
            <span data-event-spending-delta class="text-xs tabular-nums {{if eq .Type "cut"}}text-red-600 dark:text-red-300{{else}}text-green-600 dark:text-green-300{{end}}">
                {{formatMoney .MonthlySpendingBefore}}/mo → {{formatMoney .MonthlySpendingAfter}}/mo
            </span>
            {{end}}
            <span class="text-xs text-gray-500 dark:text-gray-400">Portfolio: {{formatMoney .Portfolio}}</span>
        </div>
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/handlers/whatif/ -run 'TestHandleWhatIf_EventsPanelShowsDollarDelta' -v`
Expected: PASS.

- [ ] **Step 5: Run the full handlers test suite**

Run: `go test ./internal/handlers/whatif/ -count=1`
Expected: PASS.

- [ ] **Step 6: Manual smoke**

On `/whatif` with guardrails enabled and a cut event triggered, verify each event line shows `$X/mo → $Y/mo` between the multiplier label and the portfolio value, in red for cuts and green for raises.

- [ ] **Step 7: Commit**

```bash
git add web/templates/components/whatif/guardrails.html internal/handlers/whatif/handlers_test.go
git commit -m "feat(whatif): F-079 add monthly dollar delta to guardrail events panel"
```

---

## Slice C — Counterfactual chart overlay

### Task 6: New `/whatif/chart/projection/no-guardrails` endpoint

**Files:**
- Modify: `internal/handlers/whatif/handlers.go` (route registration around line 536, new handler)
- Test: `internal/handlers/whatif/handlers_test.go` (new function)

- [ ] **Step 1: Write the failing tests**

Append to `internal/handlers/whatif/handlers_test.go`:

```go
func TestHandleWhatIfProjectionChartNoGuardrails(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	settings.Guardrails = &models.GuardrailConfig{
		Enabled: true, FloorDropPct: 1, FloorCutPct: 10,
		CeilingRisePct: 500, CeilingRaisePct: 10,
		MinSpendingPct: 50, MaxSpendingPct: 150,
	}
	if err := retirementMgr.Save(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	cache.mu.Lock()
	cache.hash = ""
	cache.mu.Unlock()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/chart/projection/no-guardrails?display_dollars=nominal", nil)
	handleWhatIfProjectionChartNoGuardrails(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if data["data"] == nil {
		t.Fatal("expected chart data array")
	}
}

// The no-guardrails projection must be insensitive to the configured guardrail thresholds —
// both should produce identical balance series when guardrails are disabled in the run.
func TestHandleWhatIfProjectionChartNoGuardrails_IndependentOfThresholds(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}

	hashOf := func(cfg *models.GuardrailConfig) string {
		settings.Guardrails = cfg
		if err := retirementMgr.Save(settings); err != nil {
			t.Fatalf("save: %v", err)
		}
		cache.mu.Lock()
		cache.hash = ""
		cache.mu.Unlock()

		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/whatif/chart/projection/no-guardrails", nil)
		handleWhatIfProjectionChartNoGuardrails(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		return w.Body.String()
	}

	a := hashOf(&models.GuardrailConfig{Enabled: true, FloorDropPct: 1, FloorCutPct: 50, CeilingRisePct: 1, CeilingRaisePct: 50, MinSpendingPct: 50, MaxSpendingPct: 200})
	b := hashOf(&models.GuardrailConfig{Enabled: true, FloorDropPct: 30, FloorCutPct: 5, CeilingRisePct: 30, CeilingRaisePct: 5, MinSpendingPct: 80, MaxSpendingPct: 120})
	if a != b {
		t.Fatalf("no-guardrails endpoint output should be identical regardless of configured thresholds")
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/handlers/whatif/ -run 'TestHandleWhatIfProjectionChartNoGuardrails' -v`
Expected: FAIL — `handleWhatIfProjectionChartNoGuardrails` is undefined.

- [ ] **Step 3: Add the handler**

In `internal/handlers/whatif/handlers.go`, immediately after the `handleWhatIfProjectionChart` function (which ends at line 639), add:

```go
func handleWhatIfProjectionChartNoGuardrails(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Build a copy with guardrails forced off; do NOT mutate the saved settings.
	clone := *settings
	clone.Guardrails = nil

	calc := retirement.NewCalculator(&clone)
	projection := calc.RunProjection()

	displayDollars := normalizeDisplayDollars(r.URL.Query().Get("display_dollars"))
	chartData := buildProjectionChartData(&clone, projection, displayDollars)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chartData)
}
```

`Calculator.RunProjection()` is defined at `internal/services/retirement/calculator.go:1013` and returns `*models.ProjectionResult` directly.

- [ ] **Step 4: Register the route**

Locate the route block at line 536 in `handlers.go`:

```go
	r.Get("/whatif/chart/projection", handleWhatIfProjectionChart)
```

Add a sibling registration immediately below it:

```go
	r.Get("/whatif/chart/projection/no-guardrails", handleWhatIfProjectionChartNoGuardrails)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/handlers/whatif/ -run 'TestHandleWhatIfProjectionChartNoGuardrails' -v`
Expected: PASS.

- [ ] **Step 6: Run the full handlers test suite**

Run: `go test ./internal/handlers/whatif/ -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/handlers/whatif/handlers.go internal/handlers/whatif/handlers_test.go
git commit -m "feat(whatif): F-079 add no-guardrails projection chart endpoint"
```

---

### Task 7: Chart toggle UI + dashed overlay

**Files:**
- Modify: `web/templates/components/whatif/projection-chart.html` (toggle button + caption marker)
- Modify: `web/static/js/charts.js` (toggle handler + overlay logic)

- [ ] **Step 1: Add the toggle button to the chart card**

Open `web/templates/components/whatif/projection-chart.html`. Locate the toggle group block at lines 14–22:

```html
        <div class="inline-flex rounded-md shadow-sm border border-gray-200 dark:border-gray-600 overflow-hidden" role="group" aria-label="Display dollars mode">
            <button type="button" data-display-dollars="nominal" aria-pressed="true"
                class="projection-display-toggle px-3 py-1.5 text-xs font-medium bg-indigo-600 text-white">
                Nominal
            </button>
            <button type="button" data-display-dollars="real" aria-pressed="false"
                class="projection-display-toggle px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-700 text-gray-600 dark:text-gray-200">
                Today's Dollars
            </button>
        </div>
```

Wrap the existing toggle group plus a new compare button into a flex container. Replace the block above with:

```html
        <div class="flex items-center gap-2">
            <div class="inline-flex rounded-md shadow-sm border border-gray-200 dark:border-gray-600 overflow-hidden" role="group" aria-label="Display dollars mode">
                <button type="button" data-display-dollars="nominal" aria-pressed="true"
                    class="projection-display-toggle px-3 py-1.5 text-xs font-medium bg-indigo-600 text-white">
                    Nominal
                </button>
                <button type="button" data-display-dollars="real" aria-pressed="false"
                    class="projection-display-toggle px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-700 text-gray-600 dark:text-gray-200">
                    Today's Dollars
                </button>
            </div>
            {{if and .Settings.Guardrails .Settings.Guardrails.Enabled}}
            <button type="button"
                data-projection-compare-toggle
                aria-pressed="false"
                class="px-3 py-1.5 text-xs font-medium rounded-md border border-gray-200 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-600 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-600">
                + Compare without guardrails
            </button>
            {{end}}
        </div>
```

- [ ] **Step 2: Wire the toggle in JS**

Open `web/static/js/charts.js`. Locate the end of `updateProjectionDisplayMode` (around line 425) and the call to `loadChart` it makes. Add a new function and an event delegation block at the end of the file (or near `updateProjectionDisplayMode`):

```javascript
// Guardrail-comparison overlay: when toggled on, fetch the no-guardrails projection
// and add a dashed series alongside the primary balance line.
function toggleGuardrailCompareOverlay(card, button) {
    if (!card || !button) return;
    const chart = card.querySelector('#chart-projection');
    if (!chart || !chart._fullData) return; // Plotly hasn't rendered yet

    const isPressed = button.getAttribute('aria-pressed') === 'true';
    if (isPressed) {
        // Remove overlay
        const overlayIdx = chart._fullData.findIndex(t => t.name === 'Without guardrails');
        if (overlayIdx >= 0) {
            Plotly.deleteTraces(chart, overlayIdx);
        }
        button.setAttribute('aria-pressed', 'false');
        button.classList.remove('bg-indigo-600', 'text-white');
        button.classList.add('bg-white', 'dark:bg-gray-700', 'text-gray-600', 'dark:text-gray-200');
        return;
    }

    // Add overlay
    const baseUrl = '/whatif/chart/projection/no-guardrails';
    const primaryUrl = chart.getAttribute('data-chart-url') || '';
    const mode = primaryUrl.includes('display_dollars=real') ? 'real' : 'nominal';
    fetch(`${baseUrl}?display_dollars=${mode}`)
        .then(r => {
            if (!r.ok) throw new Error('HTTP ' + r.status);
            return r.json();
        })
        .then(data => {
            if (!data || !Array.isArray(data.data) || data.data.length === 0) return;
            const balanceTrace = data.data.find(t => t.name === 'Portfolio Balance') || data.data[0];
            const overlay = {
                type: 'scatter',
                mode: 'lines',
                name: 'Without guardrails',
                x: balanceTrace.x,
                y: balanceTrace.y,
                line: { color: '#9ca3af', width: 2, dash: 'dash' },
                hoverinfo: 'x+y+name'
            };
            Plotly.addTraces(chart, overlay);
            button.setAttribute('aria-pressed', 'true');
            button.classList.remove('bg-white', 'dark:bg-gray-700', 'text-gray-600', 'dark:text-gray-200');
            button.classList.add('bg-indigo-600', 'text-white');
        })
        .catch(err => {
            console.error('Failed to load no-guardrails overlay:', err);
        });
}

document.addEventListener('click', function(e) {
    const btn = e.target.closest('[data-projection-compare-toggle]');
    if (!btn) return;
    const card = btn.closest('[data-whatif-projection-card]');
    toggleGuardrailCompareOverlay(card, btn);
});
```

- [ ] **Step 3: Make the display-mode switch refresh the overlay if active**

In the same file, find `updateProjectionDisplayMode` (around line 383). After the existing logic that resets `data-chart-url` and re-renders the primary chart, add at the end of the function (before the closing `}`):

```javascript
    // If the compare-without-guardrails overlay is active, re-fetch it in the new mode.
    const compareBtn = card.querySelector('[data-projection-compare-toggle]');
    if (compareBtn && compareBtn.getAttribute('aria-pressed') === 'true') {
        // Force off, then back on to re-fetch in the new display mode.
        compareBtn.setAttribute('aria-pressed', 'false');
        toggleGuardrailCompareOverlay(card, compareBtn);
    }
```

- [ ] **Step 4: Manual smoke**

Reload `/whatif` (cache-bust the JS). With guardrails enabled:
1. Verify the new "+ Compare without guardrails" button appears next to the Nominal/Today's Dollars toggle.
2. Click it. A dashed grey line labelled "Without guardrails" should appear on the chart, button toggles to indigo/pressed.
3. Click again. Overlay disappears, button reverts.
4. Toggle on, then switch Nominal ↔ Today's Dollars. Both lines should redraw consistently in the new mode.

Disable guardrails and reload — the compare button must not render.

- [ ] **Step 5: Commit**

```bash
git add web/templates/components/whatif/projection-chart.html web/static/js/charts.js
git commit -m "feat(whatif): F-079 add no-guardrails comparison overlay to projection chart"
```

---

## Verification

After all tasks complete, run from the repo root:

```bash
go vet ./... && \
  go test ./... -count=1 && \
  go build ./...
```

Expected: all green. Then load `/whatif` and run the full smoke flow:
1. Disable guardrails — table has no multiplier badges, no row accents, no compare button.
2. Enable guardrails with a hair-trigger configuration (Drop=1, Cut=10) — events panel shows `$X/mo → $Y/mo`, table shows stacked spending cells with red/green border accents, compare button is visible.
3. Toggle the compare overlay on, verify dashed line, switch Nominal/Today's Dollars, verify both redraw correctly, toggle off.
