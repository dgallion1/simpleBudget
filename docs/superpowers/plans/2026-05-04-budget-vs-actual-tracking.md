# Budget vs Actual Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Living, Healthcare, and the combined Budget visibly track actual-vs-target *over time* on the dashboard — both as enriched KPI sparklines and as a new full-width chart with stacked monthly bars and a cumulative running variance line.

**Architecture:** Server computes a new `CombinedCumulativeTrend []float64` series alongside the existing trends, keyed to the same monthly labels. KPI sparklines gain `data-target` and `data-mode` attributes that `renderSparkline` uses to overlay a dashed budget line and red/green fill. A new chart endpoint `/dashboard/charts/data/budget-vs-actual` returns Plotly subplots: stacked Living + Healthcare bars vs target line on top, cumulative variance line on bottom. The existing per-month variance card is removed since the new chart's top panel subsumes it.

**Tech Stack:** Go 1.x, chi router, html/template, Plotly.js (already loaded). Test discipline: TDD per existing patterns in `internal/handlers/dashboard/handlers_test.go`.

**Project memory hooks (per CLAUDE.md):** Each task that edits existing symbols must run `gitnexus_impact({target: "<symbol>", direction: "upstream"})` and report the blast radius. The final task must run `gitnexus_detect_changes()` before declaring done.

---

## Spec

This plan implements `docs/superpowers/specs/2026-05-04-budget-vs-actual-tracking-design.md`. Read it before starting if you don't have full context.

---

## Task 1: Add `CombinedCumulativeTrend` series

**Goal:** Server computes a per-month running cumulative variance ((Living[i]+Health[i])−CombinedTarget) alongside existing trends and exposes it on `DashboardMetrics`. The last value must equal the existing `CombinedCumulativeDelta` (invariant test).

**Files:**
- Modify: `internal/models/dashboard.go` (add field after `HealthcareTargetTotal`)
- Modify: `internal/handlers/dashboard/handlers.go` (in `calculateMetrics`, around lines 711–733)
- Test: `internal/handlers/dashboard/handlers_test.go` (append new test functions)

- [ ] **Step 1: Run impact analysis on `calculateMetrics`**

Run: `gitnexus_impact({target: "calculateMetrics", direction: "upstream"})`
Expected: Reports two upstream callers — `handleDashboard` and `handleKPIsPartial`, plus `calculateComparison`. Risk should be LOW since we're adding an output field, not changing inputs. If HIGH/CRITICAL is reported, stop and report to user.

- [ ] **Step 2: Write the failing test for the new field**

Append to `internal/handlers/dashboard/handlers_test.go`:

```go
// --- calculateMetrics: cumulative variance trend ---

func TestCalculateMetrics_CombinedCumulativeTrend_NoTargetReturnsNil(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1500, jan, models.Outflow, "Housing"),
		makeTransaction("Rent", -1500, feb, models.Outflow, "Housing"),
	)

	m := calculateMetrics(ts, ts.MinDate(), ts.MaxDate(), 0, 0)

	if m.CombinedCumulativeTrend != nil {
		t.Errorf("CombinedCumulativeTrend = %v, want nil when no combined target", m.CombinedCumulativeTrend)
	}
}

func TestCalculateMetrics_CombinedCumulativeTrend_AccumulatesMonthlyDelta(t *testing.T) {
	// Two months: Jan $1000 living, Feb $2000 living. Target $1500/mo combined,
	// no healthcare. Cumulative variance: Jan = -500 (under), Feb = -500+500 = 0.
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1000, jan, models.Outflow, "Housing"),
		makeTransaction("Rent", -2000, feb, models.Outflow, "Housing"),
	)

	m := calculateMetrics(ts, start, end, 1500, 0)

	if len(m.CombinedCumulativeTrend) != 2 {
		t.Fatalf("CombinedCumulativeTrend length = %d, want 2", len(m.CombinedCumulativeTrend))
	}
	if !floatEqual(m.CombinedCumulativeTrend[0], -500) {
		t.Errorf("CombinedCumulativeTrend[0] = %.2f, want -500", m.CombinedCumulativeTrend[0])
	}
	if !floatEqual(m.CombinedCumulativeTrend[1], 0) {
		t.Errorf("CombinedCumulativeTrend[1] = %.2f, want 0", m.CombinedCumulativeTrend[1])
	}
}

func TestCalculateMetrics_CombinedCumulativeTrend_LastEqualsCumulativeDelta(t *testing.T) {
	// Invariant: the last value of the cumulative trend, computed from the
	// per-month accumulator, must agree with CombinedCumulativeDelta which is
	// computed in closed form. If they diverge, the chart and KPI card will
	// show contradictory numbers.
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1500, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Rent", -1500, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Rent", -1500, time.Date(2025, 3, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Premium", -400, time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
		makeTransaction("Premium", -400, time.Date(2025, 2, 6, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
		makeTransaction("Premium", -400, time.Date(2025, 3, 6, 0, 0, 0, 0, time.UTC), models.Outflow, "Health Insurance"),
	)

	m := calculateMetrics(ts, start, end, 1200, 350) // combined target = 1550

	if len(m.CombinedCumulativeTrend) == 0 {
		t.Fatalf("CombinedCumulativeTrend empty; want non-empty when combined target is set")
	}
	last := m.CombinedCumulativeTrend[len(m.CombinedCumulativeTrend)-1]
	// Allow $5 slack: per-month series uses integer-month target (1550) while
	// CombinedCumulativeDelta uses fractional MonthsInRange (e.g. 2.96 mo for a
	// 90-day window). Difference is bounded by combinedTarget * |months - len(trend)|.
	if math.Abs(last-m.CombinedCumulativeDelta) > 5 {
		t.Errorf("trend tail %.2f vs CombinedCumulativeDelta %.2f — must agree (within $5 month-rounding slack)", last, m.CombinedCumulativeDelta)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/handlers/dashboard/ -run "TestCalculateMetrics_CombinedCumulativeTrend" -v`
Expected: 3 FAIL — `CombinedCumulativeTrend` field is undefined.

- [ ] **Step 4: Add the field to `DashboardMetrics`**

Edit `internal/models/dashboard.go`. Locate line 65 (the `HealthcareTargetTotal` field) and append immediately after it, still inside the `DashboardMetrics` struct:

```go
	// Per-month running cumulative variance for combined Living + Healthcare
	// against CombinedTarget. Element i = sum_{j<=i} ((LivingTrend[j] +
	// HealthcareTrend[j]) - CombinedTarget). Same length as TrendLabels when
	// HasCombinedTarget; nil when no combined target is configured. The last
	// element must agree with CombinedCumulativeDelta (within month-rounding
	// slack) so the Budget card text and the chart never diverge.
	CombinedCumulativeTrend []float64 `json:"combined_cumulative_trend"`
```

- [ ] **Step 5: Compute the series in `calculateMetrics`**

In `internal/handlers/dashboard/handlers.go`, locate the existing month loop (lines ~711–733). After the line `livingTrend = append(livingTrend, expAmt-hcAmt)` and **before** `trendLabels = append(...)`, insert nothing — we need a separate accumulator that runs only when there's a combined target. Modify the loop body so it builds the cumulative series:

Replace this block (lines 711–733):

```go
	for _, m := range months {
		incAmt := 0.0
		if inc, ok := monthlyIncome[m]; ok {
			incAmt = inc.SumAmount()
		}

		expAmt := 0.0
		if exp, ok := monthlyOutflows[m]; ok {
			expAmt = math.Abs(exp.SumAmount())
		}

		hcAmt := 0.0
		if hc, ok := monthlyHealthcare[m]; ok {
			hcAmt = math.Abs(hc.SumAmount())
		}

		incomeTrend = append(incomeTrend, incAmt)
		expensesTrend = append(expensesTrend, expAmt)
		savingsTrend = append(savingsTrend, incAmt-expAmt)
		healthcareTrend = append(healthcareTrend, hcAmt)
		livingTrend = append(livingTrend, expAmt-hcAmt)
		trendLabels = append(trendLabels, m)
	}
```

with:

```go
	var combinedCumulativeTrend []float64
	combinedTargetForTrend := budgetTarget + healthcareTarget
	hasCombinedTrend := combinedTargetForTrend > 0

	var runningCumVariance float64
	for _, m := range months {
		incAmt := 0.0
		if inc, ok := monthlyIncome[m]; ok {
			incAmt = inc.SumAmount()
		}

		expAmt := 0.0
		if exp, ok := monthlyOutflows[m]; ok {
			expAmt = math.Abs(exp.SumAmount())
		}

		hcAmt := 0.0
		if hc, ok := monthlyHealthcare[m]; ok {
			hcAmt = math.Abs(hc.SumAmount())
		}

		livingMonth := expAmt - hcAmt

		incomeTrend = append(incomeTrend, incAmt)
		expensesTrend = append(expensesTrend, expAmt)
		savingsTrend = append(savingsTrend, incAmt-expAmt)
		healthcareTrend = append(healthcareTrend, hcAmt)
		livingTrend = append(livingTrend, livingMonth)
		trendLabels = append(trendLabels, m)

		if hasCombinedTrend {
			runningCumVariance += (livingMonth + hcAmt) - combinedTargetForTrend
			combinedCumulativeTrend = append(combinedCumulativeTrend, runningCumVariance)
		}
	}
```

Then in the returned `&models.DashboardMetrics{...}` literal (around line 769), add the new field. Locate the line `HealthcareTargetTotal:     healthcareTarget * monthsInRange,` and append on the next line (before the closing `}`):

```go
		CombinedCumulativeTrend:   combinedCumulativeTrend,
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/handlers/dashboard/ -run "TestCalculateMetrics_CombinedCumulativeTrend" -v`
Expected: 3 PASS.

- [ ] **Step 7: Run full dashboard test suite to confirm no regressions**

Run: `go test ./internal/handlers/dashboard/ -count=1`
Expected: PASS, no failures from existing tests.

- [ ] **Step 8: Commit**

```bash
git add internal/models/dashboard.go internal/handlers/dashboard/handlers.go internal/handlers/dashboard/handlers_test.go
git commit -m "feat(dashboard): add CombinedCumulativeTrend series

Per-month running variance for Living + Healthcare against CombinedTarget.
Drives the new Budget vs Actual chart's bottom panel and the Budget card
sparkline. Last value matches CombinedCumulativeDelta (asserted in test)."
```

---

## Task 2: Add `buildBudgetVsActualChartData` and wire endpoint

**Goal:** New chart-data builder returns a Plotly subplot payload (top panel: stacked Living + Healthcare bars + horizontal target line; bottom panel: cumulative variance line with green/red area fill). New `case "budget-vs-actual"` in `handleChartData` serves it via the existing `/dashboard/charts/data/{chartType}` route.

**Files:**
- Modify: `internal/handlers/dashboard/handlers.go` (add builder and switch case)
- Test: `internal/handlers/dashboard/handlers_test.go` (builder unit tests)
- Test: `internal/handlers/dashboard/handlers_http_test.go` (HTTP-level test)

- [ ] **Step 1: Run impact analysis on `handleChartData`**

Run: `gitnexus_impact({target: "handleChartData", direction: "upstream"})`
Expected: Upstream callers are the chi router only. LOW risk — adding a switch case.

- [ ] **Step 2: Write failing unit tests for the builder**

Append to `internal/handlers/dashboard/handlers_test.go`:

```go
// --- buildBudgetVsActualChartData ---

func TestBuildBudgetVsActualChartData_Empty(t *testing.T) {
	ts := makeTransactionSet()

	result := buildBudgetVsActualChartData(ts, time.Time{}, time.Time{}, 0, 0)

	data, ok := result["data"].([]map[string]interface{})
	if !ok {
		t.Fatalf("data field missing or wrong type")
	}
	if len(data) != 0 {
		t.Errorf("data length = %d, want 0 for empty target+empty txns", len(data))
	}
}

func TestBuildBudgetVsActualChartData_Structure(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1500, jan, models.Outflow, "Housing"),
		makeTransaction("Rent", -1500, feb, models.Outflow, "Housing"),
		makeTransaction("Premium", -400, jan, models.Outflow, "Health Insurance"),
		makeTransaction("Premium", -400, feb, models.Outflow, "Health Insurance"),
	)

	result := buildBudgetVsActualChartData(ts, start, end, 1200, 350)

	data, ok := result["data"].([]map[string]interface{})
	if !ok {
		t.Fatalf("data field missing")
	}
	// 3 traces: living bar, healthcare bar, cumulative line
	if len(data) != 3 {
		t.Fatalf("traces = %d, want 3 (living bar + healthcare bar + cumulative line)", len(data))
	}

	// Trace 0: living bar
	if data[0]["type"] != "bar" {
		t.Errorf("trace[0].type = %v, want bar", data[0]["type"])
	}
	if data[0]["name"] != "Living" {
		t.Errorf("trace[0].name = %v, want Living", data[0]["name"])
	}
	livingY := data[0]["y"].([]float64)
	if len(livingY) != 2 || !floatEqual(livingY[0], 1500) || !floatEqual(livingY[1], 1500) {
		t.Errorf("trace[0].y = %v, want [1500 1500]", livingY)
	}

	// Trace 1: healthcare bar
	if data[1]["name"] != "Healthcare" {
		t.Errorf("trace[1].name = %v, want Healthcare", data[1]["name"])
	}

	// Trace 2: cumulative line on subplot 2
	if data[2]["type"] != "scatter" {
		t.Errorf("trace[2].type = %v, want scatter", data[2]["type"])
	}
	if data[2]["yaxis"] != "y2" {
		t.Errorf("trace[2].yaxis = %v, want y2 (bottom subplot)", data[2]["yaxis"])
	}
	cumY := data[2]["y"].([]float64)
	// Combined target = 1550. Jan: 1900-1550 = +350. Feb: cum = +350 + 350 = 700.
	if len(cumY) != 2 || !floatEqual(cumY[0], 350) || !floatEqual(cumY[1], 700) {
		t.Errorf("trace[2].y = %v, want [350 700]", cumY)
	}

	// Layout has barmode=stack and a target line shape
	layout, ok := result["layout"].(map[string]interface{})
	if !ok {
		t.Fatalf("layout missing")
	}
	if layout["barmode"] != "stack" {
		t.Errorf("layout.barmode = %v, want stack", layout["barmode"])
	}
	shapes, ok := layout["shapes"].([]map[string]interface{})
	if !ok || len(shapes) == 0 {
		t.Fatalf("layout.shapes missing or empty; want target line + zero baseline")
	}
}

func TestBuildBudgetVsActualChartData_NoTarget(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -1500, jan, models.Outflow, "Housing"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	result := buildBudgetVsActualChartData(ts, start, end, 0, 0)

	data := result["data"].([]map[string]interface{})
	if len(data) != 0 {
		t.Errorf("traces = %d, want 0 when no combined target (front end shows empty state)", len(data))
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/handlers/dashboard/ -run "TestBuildBudgetVsActualChartData" -v`
Expected: 3 FAIL — `buildBudgetVsActualChartData` is undefined.

- [ ] **Step 4: Implement the builder**

Append to `internal/handlers/dashboard/handlers.go` after the existing `buildMonthlyVarianceChartData` function (after line 922):

```go
// buildBudgetVsActualChartData renders a two-panel Plotly chart showing
// monthly Living + Healthcare actuals stacked against a combined budget
// target line (top panel) and the per-month running cumulative variance
// (bottom panel). Returns a payload with empty data when the combined
// target is 0 so the front end can branch on len(data)==0 to show its
// "Set a budget in What-If →" empty state.
func buildBudgetVsActualChartData(ts *models.TransactionSet, rangeStart, rangeEnd time.Time, livingTarget, healthcareTarget float64) map[string]interface{} {
	combinedTarget := livingTarget + healthcareTarget
	if combinedTarget <= 0 {
		return map[string]interface{}{
			"data":   []map[string]interface{}{},
			"layout": map[string]interface{}{},
		}
	}

	outflows := ts.FilterByType(models.Outflow)
	healthcareOutflows := outflows.FilterByCategory(healthInsuranceCategory)
	monthlyOutflows := outflows.GroupByMonth()
	monthlyHealthcare := healthcareOutflows.GroupByMonth()

	monthSet := make(map[string]bool)
	for m := range monthlyOutflows {
		monthSet[m] = true
	}
	var months []string
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Strings(months)

	livingValues := make([]float64, 0, len(months))
	healthcareValues := make([]float64, 0, len(months))
	cumulativeValues := make([]float64, 0, len(months))

	var running float64
	for _, m := range months {
		expAmt := 0.0
		if exp, ok := monthlyOutflows[m]; ok {
			expAmt = math.Abs(exp.SumAmount())
		}
		hcAmt := 0.0
		if hc, ok := monthlyHealthcare[m]; ok {
			hcAmt = math.Abs(hc.SumAmount())
		}
		livingMonth := expAmt - hcAmt

		livingValues = append(livingValues, livingMonth)
		healthcareValues = append(healthcareValues, hcAmt)

		running += (livingMonth + hcAmt) - combinedTarget
		cumulativeValues = append(cumulativeValues, running)
	}

	livingTrace := map[string]interface{}{
		"type": "bar",
		"name": "Living",
		"x":    months,
		"y":    livingValues,
		"marker": map[string]interface{}{
			"color": "#9ca3af", // gray — matches Living card icon
		},
		"xaxis": "x",
		"yaxis": "y",
	}

	healthcareTrace := map[string]interface{}{
		"type": "bar",
		"name": "Healthcare",
		"x":    months,
		"y":    healthcareValues,
		"marker": map[string]interface{}{
			"color": "#e11d48", // rose — matches Healthcare card icon
		},
		"xaxis": "x",
		"yaxis": "y",
	}

	cumulativeTrace := map[string]interface{}{
		"type": "scatter",
		"mode": "lines+markers",
		"name": "Cumulative variance",
		"x":    months,
		"y":    cumulativeValues,
		"line": map[string]interface{}{
			"color": "#6366f1",
			"width": 2,
		},
		"fill":      "tozeroy",
		"fillcolor": "rgba(99, 102, 241, 0.2)",
		"xaxis":     "x2",
		"yaxis":     "y2",
	}

	layout := map[string]interface{}{
		"barmode":    "stack",
		"showlegend": true,
		"legend": map[string]interface{}{
			"orientation": "h",
			"y":           1.12,
		},
		"grid": map[string]interface{}{
			"rows":    2,
			"columns": 1,
			"pattern": "independent",
		},
		"xaxis": map[string]interface{}{
			"showticklabels": false,
		},
		"yaxis": map[string]interface{}{
			"title":  map[string]interface{}{"text": "Monthly $"},
			"domain": []float64{0.55, 1.0},
		},
		"xaxis2": map[string]interface{}{
			"anchor": "y2",
		},
		"yaxis2": map[string]interface{}{
			"title":         map[string]interface{}{"text": "Cumulative variance $"},
			"domain":        []float64{0.0, 0.42},
			"zeroline":      true,
			"zerolinecolor": "#6b7280",
			"zerolinewidth": 2,
		},
		"shapes": []map[string]interface{}{
			{
				// Combined target line on top subplot
				"type":  "line",
				"xref":  "paper",
				"x0":    0,
				"x1":    1,
				"yref":  "y",
				"y0":    combinedTarget,
				"y1":    combinedTarget,
				"line": map[string]interface{}{
					"color": "#6b7280",
					"width": 2,
					"dash":  "dash",
				},
			},
		},
		"annotations": []map[string]interface{}{
			{
				"xref":      "paper",
				"yref":      "y",
				"x":         1,
				"xanchor":   "right",
				"y":         combinedTarget,
				"yanchor":   "bottom",
				"text":      fmt.Sprintf("Target $%.0f", combinedTarget),
				"showarrow": false,
				"font": map[string]interface{}{
					"color": "#6b7280",
					"size":  11,
				},
			},
		},
	}

	return map[string]interface{}{
		"data":   []map[string]interface{}{livingTrace, healthcareTrace, cumulativeTrace},
		"layout": layout,
	}
}
```

- [ ] **Step 5: Wire the new chart type into the handler**

In `internal/handlers/dashboard/handlers.go`, locate the switch in `handleChartData` (line 237). Add a case before the `default`:

```go
	case "budget-vs-actual":
		settings := currentBudgetSettings()
		livingTarget := phaseAdjustedMonthlyTarget(settings, startDate, endDate)
		healthTarget := currentHealthcareTarget(settings)
		chartData = buildBudgetVsActualChartData(filtered, startDate, endDate, livingTarget, healthTarget)
```

- [ ] **Step 6: Run unit tests to verify they pass**

Run: `go test ./internal/handlers/dashboard/ -run "TestBuildBudgetVsActualChartData" -v`
Expected: 3 PASS.

- [ ] **Step 7: Write the failing HTTP test**

Append to `internal/handlers/dashboard/handlers_http_test.go` (before the `// min helper for Go < 1.21` block):

```go
// ---------- /dashboard/charts/data/budget-vs-actual ----------

func TestHandleChartData_BudgetVsActual_Empty(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/charts/data/budget-vs-actual?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	data, ok := payload["data"].([]interface{})
	if !ok {
		t.Fatalf("response.data missing or wrong type")
	}
	// No retirement manager wired (Initialize(_, _, nil)) so no combined target;
	// builder returns empty data.
	if len(data) != 0 {
		t.Errorf("data length = %d, want 0 when no combined target configured", len(data))
	}
}

func TestHandleChartData_BudgetVsActual_BadType(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/charts/data/not-a-real-chart-type")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unknown chart type", rec.Code)
	}
}
```

- [ ] **Step 8: Run HTTP tests to verify they pass**

Run: `go test ./internal/handlers/dashboard/ -run "TestHandleChartData_BudgetVsActual" -v`
Expected: 2 PASS.

- [ ] **Step 9: Run the full dashboard suite to confirm no regressions**

Run: `go test ./internal/handlers/dashboard/ -count=1`
Expected: all PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/handlers/dashboard/handlers.go internal/handlers/dashboard/handlers_test.go internal/handlers/dashboard/handlers_http_test.go
git commit -m "feat(dashboard): add /dashboard/charts/data/budget-vs-actual endpoint

Two-panel Plotly subplot — stacked Living + Healthcare bars with
target line on top, cumulative variance line on bottom. Empty data
when combined target is 0 so front end can render its empty state."
```

---

## Task 3: Update `renderSparkline` to support target overlays and variance mode

**Goal:** `renderSparkline` accepts a fourth `options` argument: `{ target, mode }`. When `target` is a number, draws a dashed horizontal line and fills above red / below green. When `mode === "variance"`, the data is treated as cumulative variance: zero is the reference, fill above zero red and below green. `initSparklines` reads the new `data-target` and `data-mode` attributes and passes them through. Existing income/expenses sparklines (no `data-target`) keep current behavior.

**Files:**
- Modify: `web/static/js/charts.js` (extend `renderSparkline`, update `initSparklines`)

- [ ] **Step 1: Run impact analysis on `renderSparkline`**

Run: `gitnexus_impact({target: "renderSparkline", direction: "upstream"})`
Expected: Called only by `initSparklines`. LOW risk — adding optional parameter.

- [ ] **Step 2: Update `renderSparkline` to accept options**

In `web/static/js/charts.js`, replace the function (lines 167–211) with:

```javascript
/**
 * Render a sparkline chart with optional target overlay or variance mode.
 *
 * @param {string} containerId - The ID of the container element
 * @param {number[]} values - The data values (or cumulative variance values when mode==="variance")
 * @param {string} color - The line color (used when no target/mode customization applies)
 * @param {object} [options] - Optional rendering options
 * @param {number} [options.target] - When set, draws a dashed horizontal target line.
 *                                    Months above the line fill red, below fill green.
 * @param {string} [options.mode] - When "variance", values are cumulative deltas.
 *                                  Zero is the reference; fill above zero red, below green.
 *                                  Overrides options.target.
 */
function renderSparkline(containerId, values, color, options) {
    const container = document.getElementById(containerId);
    if (!container || !values || values.length === 0) {
        return;
    }

    options = options || {};
    const isVariance = options.mode === 'variance';
    const hasTarget = !isVariance && typeof options.target === 'number' && isFinite(options.target);

    const data = [];
    const layout = {
        margin: { t: 0, r: 0, b: 0, l: 0 },
        paper_bgcolor: 'transparent',
        plot_bgcolor: 'transparent',
        xaxis: { visible: false },
        yaxis: { visible: false },
        showlegend: false
    };

    if (isVariance) {
        // Split the line into above-zero (red) and below-zero (green) segments
        // by clamping each direction. Using two filled traces against the zero
        // baseline gives the divergent fill.
        const above = values.map(v => v > 0 ? v : 0);
        const below = values.map(v => v < 0 ? v : 0);

        data.push({
            type: 'scatter',
            mode: 'lines',
            y: below,
            line: { color: '#22c55e', width: 1 },
            fill: 'tozeroy',
            fillcolor: 'rgba(34, 197, 94, 0.3)'
        });
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: above,
            line: { color: '#ef4444', width: 1 },
            fill: 'tozeroy',
            fillcolor: 'rgba(239, 68, 68, 0.3)'
        });
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: values,
            line: { color: color || '#6366f1', width: 2 }
        });

        layout.shapes = [{
            type: 'line',
            xref: 'paper',
            x0: 0,
            x1: 1,
            yref: 'y',
            y0: 0,
            y1: 0,
            line: { color: '#6b7280', width: 1, dash: 'dash' }
        }];
    } else if (hasTarget) {
        // Above-target fill (red) and below-target fill (green) achieved by
        // plotting two clamped series with fill: 'tonexty' relative to a flat
        // target baseline.
        const target = options.target;
        const targetSeries = values.map(() => target);
        const above = values.map(v => v > target ? v : target);
        const below = values.map(v => v < target ? v : target);

        data.push({
            type: 'scatter',
            mode: 'lines',
            y: targetSeries,
            line: { color: 'transparent', width: 0 }
        });
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: below,
            line: { color: 'transparent', width: 0 },
            fill: 'tonexty',
            fillcolor: 'rgba(34, 197, 94, 0.3)'
        });
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: targetSeries,
            line: { color: 'transparent', width: 0 }
        });
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: above,
            line: { color: 'transparent', width: 0 },
            fill: 'tonexty',
            fillcolor: 'rgba(239, 68, 68, 0.3)'
        });
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: values,
            line: { color: color || '#6366f1', width: 2 }
        });

        layout.shapes = [{
            type: 'line',
            xref: 'paper',
            x0: 0,
            x1: 1,
            yref: 'y',
            y0: target,
            y1: target,
            line: { color: '#6b7280', width: 1, dash: 'dash' }
        }];
    } else {
        // Original behavior — single filled line, no target reference.
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: values,
            line: { color: color || '#6366f1', width: 2 },
            fill: 'tozeroy',
            fillcolor: (color || '#6366f1') + '20'
        });
    }

    const config = {
        responsive: true,
        displayModeBar: false,
        staticPlot: true
    };

    Plotly.newPlot(containerId, data, layout, config);
}
```

- [ ] **Step 3: Update `initSparklines` to read the new attributes**

In the same file, replace `initSparklines` (lines 354–370) with:

```javascript
// Initialize sparklines from data attributes
function initSparklines() {
    document.querySelectorAll('[id^="sparkline-"]').forEach(function(el) {
        const valuesAttr = el.getAttribute('data-values');
        const color = el.getAttribute('data-color') || '#6366f1';
        const targetAttr = el.getAttribute('data-target');
        const mode = el.getAttribute('data-mode') || '';

        if (valuesAttr && valuesAttr !== 'null' && valuesAttr !== '[]') {
            try {
                const values = JSON.parse(valuesAttr);
                if (values && values.length > 0) {
                    const options = {};
                    if (mode) {
                        options.mode = mode;
                    }
                    if (targetAttr !== null && targetAttr !== '') {
                        const t = parseFloat(targetAttr);
                        if (isFinite(t) && t > 0) {
                            options.target = t;
                        }
                    }
                    renderSparkline(el.id, values, color, options);
                }
            } catch (e) {
                console.error('Error parsing sparkline data:', e);
            }
        }
    });
}
```

- [ ] **Step 4: Manual smoke check**

Open `web/static/js/charts.js` and visually confirm:
- `renderSparkline` accepts 4 args; existing 3-arg call sites still work because options defaults to `{}`.
- `initSparklines` reads `data-target` and `data-mode`; absence of either falls through to original line behavior.
- No syntax errors (run `node --check web/static/js/charts.js`).

Run: `node --check web/static/js/charts.js`
Expected: exits 0, no output.

- [ ] **Step 5: Commit**

```bash
git add web/static/js/charts.js
git commit -m "feat(dashboard): renderSparkline supports target line + variance mode

Optional 4th options arg: {target} draws a dashed reference and red/green
fill above/below; {mode: 'variance'} treats values as cumulative variance
with red-above-zero / green-below-zero fill against a zero baseline.
Existing 3-arg call sites unaffected."
```

---

## Task 4: Update `kpis.html` — add target attributes and Budget card sparkline

**Goal:** Living and Healthcare card sparklines pass `data-target` so the JS draws the budget reference line. The Budget card gets a new sparkline driven by `CombinedCumulativeTrend` with `data-mode="variance"`.

**Files:**
- Modify: `web/templates/components/kpis.html` (lines 81, 106; insertion before line 158)
- Test: `internal/handlers/dashboard/handlers_http_test.go` (assertions on rendered HTML)

- [ ] **Step 1: Write failing HTTP tests for the new attributes**

Append to `internal/handlers/dashboard/handlers_http_test.go` (before the `min` helper):

```go
// ---------- KPI sparkline target overlays ----------

func TestDashboardKPIs_LivingSparkline_HasTargetAttribute(t *testing.T) {
	rows := [][]string{
		{"2025-01-15", "Salary", "5000", "Payroll"},
		{"2025-01-05", "Rent", "-1500", "Housing"},
	}
	tmpDir, dl, cleanup := writeTempCSV(t, rows)
	defer cleanup()

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	// Wire a retirement manager so a budget target is configured.
	mgr := retirementMgrWithLivingTarget(t, tmpDir, 2000)
	Initialize(dl, rend, mgr)

	router := chi.NewRouter()
	RegisterRoutes(router)

	rec := doGet(t, router, "/dashboard/kpis?start=2025-01-01&end=2025-01-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
	body := rec.Body.String()

	if !strings.Contains(body, `id="sparkline-monthly"`) {
		t.Errorf("response missing sparkline-monthly container")
	}
	if !strings.Contains(body, `data-target="2000"`) {
		t.Errorf("sparkline-monthly missing data-target=\"2000\"; body excerpt: %s", excerptAround(body, "sparkline-monthly", 200))
	}
}

func TestDashboardKPIs_BudgetSparkline_VarianceMode(t *testing.T) {
	rows := [][]string{
		{"2025-01-05", "Rent", "-1500", "Housing"},
	}
	tmpDir, dl, cleanup := writeTempCSV(t, rows)
	defer cleanup()

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	mgr := retirementMgrWithLivingTarget(t, tmpDir, 2000)
	Initialize(dl, rend, mgr)

	router := chi.NewRouter()
	RegisterRoutes(router)

	rec := doGet(t, router, "/dashboard/kpis?start=2025-01-01&end=2025-01-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
	body := rec.Body.String()

	if !strings.Contains(body, `id="sparkline-budget"`) {
		t.Errorf("response missing sparkline-budget container")
	}
	if !strings.Contains(body, `data-mode="variance"`) {
		t.Errorf("sparkline-budget missing data-mode=\"variance\"; body excerpt: %s", excerptAround(body, "sparkline-budget", 200))
	}
}

// excerptAround returns a substring of body centered on needle for diagnostics.
func excerptAround(body, needle string, around int) string {
	idx := strings.Index(body, needle)
	if idx < 0 {
		return "(needle not found)"
	}
	start := idx - around
	if start < 0 {
		start = 0
	}
	end := idx + around
	if end > len(body) {
		end = len(body)
	}
	return body[start:end]
}
```

Note: `retirementMgrWithLivingTarget` is a test helper used elsewhere in this file. Verify it exists with `grep -n retirementMgrWithLivingTarget internal/handlers/dashboard/handlers_http_test.go`. If absent, the existing `TestDashboardKPIs_RendersBudgetCards_WithTarget` test (around line 1361) shows the inline pattern for setting up a retirement manager — copy that approach instead.

- [ ] **Step 2: Verify tests fail**

Run: `go test ./internal/handlers/dashboard/ -run "TestDashboardKPIs_LivingSparkline_HasTargetAttribute|TestDashboardKPIs_BudgetSparkline_VarianceMode" -v`
Expected: FAIL — `data-target` and `sparkline-budget` not yet in template.

- [ ] **Step 3: Update Living Expenses card sparkline**

In `web/templates/components/kpis.html`, replace line 81:

```html
        <div id="sparkline-monthly" class="h-10 mt-2" data-values="{{toJSON .Metrics.LivingExpensesTrend}}" data-color="#9ca3af"></div>
```

with:

```html
        <div id="sparkline-monthly" class="h-10 mt-2"
             data-values="{{toJSON .Metrics.LivingExpensesTrend}}"
             data-color="#9ca3af"
             {{if .Metrics.HasBudgetTarget}}data-target="{{.Metrics.BudgetTarget}}"{{end}}></div>
```

- [ ] **Step 4: Update Healthcare card sparkline**

In the same file, replace line 106:

```html
        <div id="sparkline-healthcare" class="h-10 mt-2" data-values="{{toJSON .Metrics.HealthcareTrend}}" data-color="#e11d48"></div>
```

with:

```html
        <div id="sparkline-healthcare" class="h-10 mt-2"
             data-values="{{toJSON .Metrics.HealthcareTrend}}"
             data-color="#e11d48"
             {{if .Metrics.HasHealthcareTarget}}data-target="{{.Metrics.HealthcareTarget}}"{{end}}></div>
```

- [ ] **Step 5: Add the Budget card sparkline**

In the same file, locate the Budget card (the `<!-- Budget (combined cumulative variance...) -->` block starting around line 109). The card body ends around line 150 with the close of the inner `<div>` containing the targets and totals. Insert a sparkline div **before the closing of the outer card** but **after the `{{else}} ... Not set` empty branch's closing `{{end}}`** so it renders only when `HasCombinedTarget`. The cleanest place is just before the icon `<div class="p-3 ...">` that already exists at line 151.

Replace this block (around line 145–150):

```html
                {{else}}
                <p class="text-2xl font-bold text-gray-400 dark:text-gray-500 italic">Not set</p>
                <a href="/whatif" class="text-sm text-indigo-600 dark:text-indigo-400 hover:underline mt-1 inline-block">Set a budget in What-If →</a>
                {{end}}
            </div>
```

with:

```html
                {{else}}
                <p class="text-2xl font-bold text-gray-400 dark:text-gray-500 italic">Not set</p>
                <a href="/whatif" class="text-sm text-indigo-600 dark:text-indigo-400 hover:underline mt-1 inline-block">Set a budget in What-If →</a>
                {{end}}
                {{if .Metrics.HasCombinedTarget}}
                <div id="sparkline-budget" class="h-10 mt-2"
                     data-values="{{toJSON .Metrics.CombinedCumulativeTrend}}"
                     data-color="#6366f1"
                     data-mode="variance"></div>
                {{end}}
            </div>
```

- [ ] **Step 6: Run the failing tests to verify they now pass**

Run: `go test ./internal/handlers/dashboard/ -run "TestDashboardKPIs_LivingSparkline_HasTargetAttribute|TestDashboardKPIs_BudgetSparkline_VarianceMode" -v`
Expected: PASS.

- [ ] **Step 7: Run full dashboard suite**

Run: `go test ./internal/handlers/dashboard/ -count=1`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add web/templates/components/kpis.html internal/handlers/dashboard/handlers_http_test.go
git commit -m "feat(dashboard): KPI sparklines show target line + new Budget sparkline

Living and Healthcare cards now pass data-target so renderSparkline draws
the dashed budget reference and fills red/green for over/under months.
Budget card gains a cumulative variance sparkline (data-mode=variance)."
```

---

## Task 5: Add `budget-vs-actual` partial and insert into dashboard page

**Goal:** New full-width card sits between the KPI row and the chart grid, hosting the `chart-budget-vs-actual` Plotly container that the existing `loadAllCharts` flow auto-populates from `/dashboard/charts/data/budget-vs-actual`.

**Files:**
- Create: `web/templates/components/budget-vs-actual.html`
- Modify: `web/templates/pages/dashboard.html` (insert template ref between KPIs and chart grid)
- Test: `internal/handlers/dashboard/handlers_http_test.go` (assert the chart container appears on the dashboard page)

- [ ] **Step 1: Verify template registration pattern**

Run: `grep -rn "ParseGlob\|ParseFiles\|components/" internal/templates/`
Expected: template loader globs `web/templates/components/*.html`. If yes, the new partial will be auto-registered. If not, note where partials are individually registered and update accordingly.

- [ ] **Step 2: Write the failing HTTP test**

Append to `internal/handlers/dashboard/handlers_http_test.go` (before `min`):

```go
// ---------- Budget vs Actual chart card ----------

func TestHandleDashboard_RendersBudgetVsActualCard(t *testing.T) {
	router, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="chart-budget-vs-actual"`) {
		t.Errorf("dashboard page missing chart-budget-vs-actual container")
	}
	if !strings.Contains(body, `data-chart-url="/dashboard/charts/data/budget-vs-actual"`) {
		t.Errorf("chart container missing data-chart-url attribute")
	}
}
```

- [ ] **Step 3: Run the test to confirm it fails**

Run: `go test ./internal/handlers/dashboard/ -run "TestHandleDashboard_RendersBudgetVsActualCard" -v`
Expected: FAIL.

- [ ] **Step 4: Create the partial**

Create `web/templates/components/budget-vs-actual.html`:

```html
{{define "budget-vs-actual"}}
<div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4 mt-6">
    <div class="flex items-center justify-between mb-2">
        <div>
            <h3 class="text-lg font-semibold text-gray-800 dark:text-gray-100">Budget vs Actual Over Time</h3>
            <p class="text-xs text-gray-500 dark:text-gray-400">
                Top: monthly Living + Healthcare vs combined target. Bottom: cumulative running variance — green = saving, red = overspending.
            </p>
        </div>
    </div>
    {{if .Metrics.HasCombinedTarget}}
    <div id="chart-budget-vs-actual" class="chart-container" data-chart-url="/dashboard/charts/data/budget-vs-actual" style="height: 360px;">
        <div class="flex items-center justify-center h-64 text-gray-400 dark:text-gray-500">
            Loading chart...
        </div>
    </div>
    {{else}}
    <div class="flex items-center justify-center h-32 text-gray-400 dark:text-gray-500 text-sm">
        <span>No combined budget configured.&nbsp;</span>
        <a href="/whatif" class="text-indigo-600 dark:text-indigo-400 hover:underline">Set a budget in What-If →</a>
    </div>
    {{end}}
</div>
{{end}}
```

- [ ] **Step 5: Insert the partial into the dashboard page**

In `web/templates/pages/dashboard.html`, locate line 128 (the close of `<div id="kpis-container">`). Add immediately after it:

```html

    <!-- Budget vs Actual Over Time -->
    {{template "budget-vs-actual" .}}
```

The new partial sits between the KPI row and the existing `<!-- Charts Grid -->` block.

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/handlers/dashboard/ -run "TestHandleDashboard_RendersBudgetVsActualCard" -v`
Expected: PASS.

- [ ] **Step 7: Run the full dashboard suite**

Run: `go test ./internal/handlers/dashboard/ -count=1`
Expected: all PASS.

- [ ] **Step 8: Manual browser smoke check**

Run: `make run` (or whatever starts the dev server — check `Makefile`).
Open `http://localhost:8080/dashboard` (or the configured port).
Confirm visually:
- New "Budget vs Actual Over Time" card appears below the KPIs.
- Top panel shows stacked bars + dashed target line labeled with the dollar amount.
- Bottom panel shows a line that tracks cumulative variance, ending at the same number printed in the Budget card.
- Living and Healthcare KPI sparklines now show a dashed target line and red/green fill.
- Budget KPI card has a new sparkline starting near zero and trending in the same direction as the bottom panel of the chart.

If the chart doesn't render but the container exists, open browser devtools — check console for Plotly errors (likely subplot layout). Report findings before proceeding.

- [ ] **Step 9: Commit**

```bash
git add web/templates/components/budget-vs-actual.html web/templates/pages/dashboard.html internal/handlers/dashboard/handlers_http_test.go
git commit -m "feat(dashboard): add Budget vs Actual Over Time chart card

Full-width card below the KPI row. Top panel: stacked Living + Healthcare
bars vs combined target line. Bottom panel: cumulative variance line.
Empty state when no combined target is set."
```

---

## Task 6: Remove the old "Monthly Variance vs Budget" chart

**Goal:** The new chart's top panel fully subsumes the existing per-month delta-bar card. Remove the chart container from `dashboard.html`, drop the `case "monthly"` switch arm in `handleChartData`, delete `buildMonthlyVarianceChartData` and its tests, and migrate the refund-regression assertion to the new builder.

**Files:**
- Modify: `web/templates/pages/dashboard.html` (delete the Monthly Variance vs Budget card)
- Modify: `internal/handlers/dashboard/handlers.go` (delete switch case + builder, lines 238–242, 833–922)
- Modify: `internal/handlers/dashboard/handlers_test.go` (delete `TestBuildMonthlyVariance*` tests; add a refund regression test for the new builder)

- [ ] **Step 1: Run impact analysis on `buildMonthlyVarianceChartData`**

Run: `gitnexus_impact({target: "buildMonthlyVarianceChartData", direction: "upstream"})`
Expected: Called only by `handleChartData` (case "monthly") and its 4 unit tests. LOW risk.

- [ ] **Step 2: Search for any other references to confirm we're not missing one**

Run: `grep -rn "chart-monthly\|buildMonthlyVariance\|charts/data/monthly" --exclude-dir=node_modules --exclude-dir=.git .`
Expected: hits only in the files we'll edit (dashboard.html, handlers.go, handlers_test.go) and the spec/plan docs we just wrote. If hits appear elsewhere (e.g., docs, other handlers), record them and ask the user before proceeding.

- [ ] **Step 3: Delete the chart card from `dashboard.html`**

In `web/templates/pages/dashboard.html`, remove the entire block from line 132 to line 141:

```html
        <!-- Monthly Variance vs Budget -->
        <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
            <h3 class="text-lg font-semibold text-gray-800 dark:text-gray-100 mb-4">Monthly Variance vs Budget</h3>
            <p class="text-xs text-gray-500 dark:text-gray-400 -mt-3 mb-3">Each bar = month's outflows minus the combined Living + Healthcare target. Red = over, green = under.</p>
            <div id="chart-monthly" class="chart-container" data-chart-url="/dashboard/charts/data/monthly">
                <div class="flex items-center justify-center h-64 text-gray-400 dark:text-gray-500">
                    Loading chart...
                </div>
            </div>
        </div>
```

- [ ] **Step 4: Delete the switch case in `handleChartData`**

In `internal/handlers/dashboard/handlers.go`, remove these 5 lines from the switch (around 238–242):

```go
	case "monthly":
		settings := currentBudgetSettings()
		livingTarget := phaseAdjustedMonthlyTarget(settings, startDate, endDate)
		healthTarget := currentHealthcareTarget(settings)
		chartData = buildMonthlyVarianceChartData(filtered, livingTarget+healthTarget)
```

- [ ] **Step 5: Delete `buildMonthlyVarianceChartData`**

In the same file, delete the entire function (the doc comment plus body, currently lines 833–922 — the function ending with the closing brace of the `return map[string]interface{}{...}`).

- [ ] **Step 6: Delete the existing unit tests for the removed builder**

In `internal/handlers/dashboard/handlers_test.go`, delete:
- `TestBuildMonthlyVarianceChartData_RefundReducesMonthOutflow` (around line 312)
- `TestBuildMonthlyVarianceChartData_OverAndUnder` (around line 449)
- `TestBuildMonthlyVarianceChartData_NoTargetFallback` (around line 478)
- `TestBuildMonthlyVarianceChartData_IncomeIgnored` (around line 496)

Use `grep -n "^func TestBuildMonthlyVarianceChartData" internal/handlers/dashboard/handlers_test.go` to confirm exact line numbers before editing.

- [ ] **Step 7: Add a refund regression test for the new builder**

The refund-handling behavior is invariant: if a month has $1500 of purchases and a $300 refund (a positive-signed Outflow row), the month's effective outflow is $1200. Append to `internal/handlers/dashboard/handlers_test.go`:

```go
// Regression: refund rows (opposite-signed Outflow) must reduce the month's
// effective outflow used by the new Budget vs Actual chart. Pre-fix on the
// previous chart, $1500 purchases plus a $300 refund produced $1800 instead
// of $1200. Same invariant applies on the new builder.
func TestBuildBudgetVsActualChartData_RefundReducesMonthLiving(t *testing.T) {
	jan := time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, jan, models.Income, "Payroll"),
		makeTransaction("Rent", -1500, jan, models.Outflow, "Housing"),
		makeTransaction("Refund", 300, jan, models.Outflow, "Housing"),
	)

	result := buildBudgetVsActualChartData(ts, start, end, 1200, 0)

	data := result["data"].([]map[string]interface{})
	if len(data) == 0 {
		t.Fatalf("expected traces; got empty data")
	}
	livingY := data[0]["y"].([]float64)
	if !floatEqual(livingY[0], 1200) {
		t.Errorf("Jan living = %.2f, want 1200 (refund of +300 must subtract)", livingY[0])
	}
	cumY := data[2]["y"].([]float64)
	if !floatEqual(cumY[0], 0) {
		t.Errorf("Jan cumulative variance = %.2f, want 0 (1200 actual = 1200 target)", cumY[0])
	}
}
```

- [ ] **Step 8: Run dashboard tests**

Run: `go test ./internal/handlers/dashboard/ -count=1 -v`
Expected: all PASS. The 4 deleted tests are gone; the 1 new test passes; HTTP tests for the removed `/charts/data/monthly` endpoint, if any (search with `grep -n "data/monthly" internal/handlers/dashboard/handlers_http_test.go`), should also be deleted in this step. If you find one, delete it now and re-run.

- [ ] **Step 9: Commit**

```bash
git add web/templates/pages/dashboard.html internal/handlers/dashboard/handlers.go internal/handlers/dashboard/handlers_test.go internal/handlers/dashboard/handlers_http_test.go
git commit -m "refactor(dashboard): remove standalone Monthly Variance chart

Subsumed by the top panel of the new Budget vs Actual card. Refund
regression assertion migrated to the new builder."
```

---

## Task 7: Final verification and detect changes

**Goal:** Run the full project test suite, GitNexus change detection, and a manual dashboard smoke pass.

**Files:** none (verification only)

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -count=1`
Expected: all PASS across all packages.

- [ ] **Step 2: Run GitNexus change detection**

Run: `gitnexus_detect_changes()`
Expected: reports the symbols you actually edited (`calculateMetrics`, `handleChartData`, new `buildBudgetVsActualChartData`, deleted `buildMonthlyVarianceChartData`, `renderSparkline`, `initSparklines`) plus the new templates. If unexpected symbols appear in the change set, investigate before declaring done.

- [ ] **Step 3: Run vet, staticcheck, govulncheck via the pre-commit gate**

The pre-commit hook already runs these on each commit. Confirm by inspecting the last commit's hook output, or rerun:

Run: `make check` (or whatever the project's lint target is — check Makefile).
Expected: `✓ all checks passed`.

- [ ] **Step 4: Manual dashboard verification**

Start the dev server, load `/dashboard`, change the date filter to a range with multiple months, then walk through:

1. Living KPI card: dashed target line visible on sparkline, fill split red/green by month.
2. Healthcare KPI card: same treatment with the healthcare target.
3. Budget KPI card: small cumulative variance sparkline ends at approximately the same dollar value printed as "$X under/over."
4. New "Budget vs Actual Over Time" card: top panel stacked bars with target line; bottom panel cumulative line; both share x-axis months.
5. Date filter change refreshes everything (KPI partial swap + chart data fetch).
6. With no budget configured, both the Budget KPI card and the new chart card show their empty-state link.

- [ ] **Step 5: Final smoke commit (only if any docs/follow-up tweaks were needed)**

If verification surfaced no issues, no commit is needed.
If a copy tweak or template fix came up, commit it as a focused change with a `polish(dashboard):` prefix.

---

## Self-Review

**Spec coverage check:**
- ✅ Server-side `CombinedCumulativeTrend` series → Task 1
- ✅ New chart endpoint with subplots → Task 2
- ✅ Sparkline target overlay + variance mode JS → Task 3
- ✅ KPI template updates (target attrs + Budget sparkline) → Task 4
- ✅ Full-width chart card + dashboard insertion → Task 5
- ✅ Replacing existing variance chart → Task 6
- ✅ Empty states for no-target + no-data → Tasks 2, 4, 5
- ✅ Refund regression preserved → Task 6 step 7
- ✅ Last-trend-value invariant test → Task 1 step 2
- ✅ GitNexus impact + detect-changes per CLAUDE.md → Tasks 1, 2, 6, 7

**Placeholder scan:** All steps contain literal code or exact commands. One conditional in Task 4 step 1 (use existing helper `retirementMgrWithLivingTarget` if present, else copy inline pattern from `TestDashboardKPIs_RendersBudgetCards_WithTarget`) — this is documented context, not a placeholder.

**Type consistency:** `CombinedCumulativeTrend []float64` field name is identical across model, handler, template (`{{toJSON .Metrics.CombinedCumulativeTrend}}`), and tests. JSON tag `combined_cumulative_trend`. Endpoint name `budget-vs-actual` consistent across handler case, partial `data-chart-url`, and HTTP tests.
