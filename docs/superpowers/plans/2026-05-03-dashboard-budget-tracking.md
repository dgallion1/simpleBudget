# Dashboard Budget Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the dashboard's Savings Rate KPI with a Monthly Living Expenses card and repurpose the Net Savings slot as a cumulative Budget variance card. Both new cards source the target from the active what-if `MonthlyLivingExpenses` setting.

**Architecture:** Plumb `*retirement.SettingsManager` into the dashboard handler package; extend `DashboardMetrics` with `MonthsInRange`, `ActualMonthly`, `BudgetTarget`, `PerMonthDelta`, `CumulativeDelta`, `HasBudgetTarget`; extend `PeriodComparison` with `ActualMonthlyChange`, `CumulativeDeltaChange`; rewrite `web/templates/components/kpis.html` to show the new four-card layout.

**Tech Stack:** Go 1.x, chi router, html/template, htmx, Tailwind, existing `retirement.SettingsManager` for what-if persistence.

**Spec:** [`docs/superpowers/specs/2026-05-03-dashboard-budget-tracking-design.md`](../specs/2026-05-03-dashboard-budget-tracking-design.md)

---

## File Structure

| File | Change |
|---|---|
| `internal/models/dashboard.go` | Add fields to `DashboardMetrics` and `PeriodComparison` |
| `internal/handlers/dashboard/handlers.go` | New `Initialize` signature; `calculateMetrics` and `calculateComparison` take date range + budget target; both call sites updated |
| `internal/handlers/dashboard/handlers_test.go` | Update existing `calculateMetrics` callers; add tests for new fields |
| `internal/handlers/dashboard/handlers_http_test.go` | Update `Initialize` call to pass a `SettingsManager`; add tests asserting new card content |
| `cmd/server/main.go` | Pass `retirementMgr` into `dashboard.Initialize` |
| `web/templates/components/kpis.html` | Drop Savings Rate card; replace Net Savings with Budget; insert Monthly Living Expenses card |

---

## Task 1: Extend Dashboard Model Structs

**Files:**
- Modify: `internal/models/dashboard.go`

This task only adds fields to existing structs. No behavior changes. Existing tests continue to pass because the new fields default to zero values.

- [ ] **Step 1: Run impact analysis**

Per `CLAUDE.md`, run impact analysis before editing model structs:

Run: `gitnexus_impact({target: "DashboardMetrics", direction: "upstream"})` and `gitnexus_impact({target: "PeriodComparison", direction: "upstream"})`. Report blast radius. Expect callers in `internal/handlers/dashboard/`.

- [ ] **Step 2: Add new fields to `DashboardMetrics`**

Edit `internal/models/dashboard.go`. Replace the existing struct (lines 5–20) with:

```go
// DashboardMetrics contains the main KPI metrics for the dashboard
type DashboardMetrics struct {
	TotalIncome      float64   `json:"total_income"`
	TotalExpenses    float64   `json:"total_expenses"`
	NetSavings       float64   `json:"net_savings"`
	SavingsRate      float64   `json:"savings_rate"`
	TransactionCount int       `json:"transaction_count"`
	StartDate        time.Time `json:"start_date"`
	EndDate          time.Time `json:"end_date"`

	// Trends (for sparklines) - monthly values
	IncomeTrend   []float64 `json:"income_trend"`
	ExpensesTrend []float64 `json:"expenses_trend"`
	SavingsTrend  []float64 `json:"savings_trend"`
	TrendLabels   []string  `json:"trend_labels"` // Month labels

	// Budget tracking (compares actual outflows to the active what-if
	// MonthlyLivingExpenses target). All zero-valued when no target is set.
	MonthsInRange   float64 `json:"months_in_range"`   // average-calendar-month count for the date range
	ActualMonthly   float64 `json:"actual_monthly"`    // TotalExpenses / MonthsInRange
	BudgetTarget    float64 `json:"budget_target"`     // from what-if MonthlyLivingExpenses
	PerMonthDelta   float64 `json:"per_month_delta"`   // ActualMonthly - BudgetTarget; positive = over
	CumulativeDelta float64 `json:"cumulative_delta"`  // TotalExpenses - BudgetTarget*MonthsInRange; positive = over
	HasBudgetTarget bool    `json:"has_budget_target"` // BudgetTarget > 0
}
```

- [ ] **Step 3: Add new fields to `PeriodComparison`**

In the same file, replace the `PeriodComparison` struct with:

```go
// PeriodComparison holds metrics for two periods for comparison
type PeriodComparison struct {
	Current  *DashboardMetrics `json:"current"`
	Previous *DashboardMetrics `json:"previous"`
	HasData  bool              `json:"has_data"`

	// Percentage changes
	IncomeChange      float64 `json:"income_change_pct"`
	ExpensesChange    float64 `json:"expenses_change_pct"`
	SavingsChange     float64 `json:"savings_change_pct"`
	SavingsRateChange float64 `json:"savings_rate_change_pp"` // percentage points

	// Budget tracking deltas (current - previous; signed, in dollars)
	ActualMonthlyChange   float64 `json:"actual_monthly_change"`
	CumulativeDeltaChange float64 `json:"cumulative_delta_change"`
}
```

- [ ] **Step 4: Verify the package builds**

Run: `go build ./internal/models/...`
Expected: clean build, no output.

- [ ] **Step 5: Run all tests to confirm no regressions**

Run: `go test ./...`
Expected: all packages pass (existing tests unaffected because new fields default to zero).

- [ ] **Step 6: Commit**

```bash
git add internal/models/dashboard.go
git commit -m "$(cat <<'EOF'
feat(dashboard): add budget-tracking fields to metrics structs

DashboardMetrics gains MonthsInRange, ActualMonthly, BudgetTarget,
PerMonthDelta, CumulativeDelta, HasBudgetTarget. PeriodComparison gains
ActualMonthlyChange and CumulativeDeltaChange. No behavior change yet —
fields are populated in subsequent tasks.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Compute Monthly + Budget Fields in `calculateMetrics`

**Files:**
- Modify: `internal/handlers/dashboard/handlers.go` (function `calculateMetrics`, lines ~509–579; both call sites at lines 62 and 112)
- Modify: `internal/handlers/dashboard/handlers_test.go` (3 existing tests call `calculateMetrics(ts)` directly)

This task changes the signature of `calculateMetrics` to accept a date range and a budget target, computes the new fields, and updates both production call sites. Target is wired from the SettingsManager in Task 4 — this task passes `0` from the call sites (handler-level wiring is not yet in place).

- [ ] **Step 1: Run impact analysis**

Per `CLAUDE.md`, run before editing the function:

Run: `gitnexus_impact({target: "calculateMetrics", direction: "upstream"})`. Report blast radius. The two known callers are `handleDashboard` (line 62) and `handleKPIsPartial` (line 112). If GitNexus shows additional callers, list them and update the plan to cover them before continuing.

- [ ] **Step 2: Write failing tests for the new computation**

Append to `internal/handlers/dashboard/handlers_test.go`:

```go
// --- calculateMetrics: budget tracking ---

func TestCalculateMetrics_MonthsInRange_ApproxFromDates(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -1000, time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC) // 90-day inclusive span

	m := calculateMetrics(ts, start, end, 0)

	// (90 - 0 + 1) days / 30.4375 ≈ 2.989
	if m.MonthsInRange < 2.95 || m.MonthsInRange > 3.05 {
		t.Errorf("MonthsInRange = %v, want ~2.99 (90-day span)", m.MonthsInRange)
	}
}

func TestCalculateMetrics_ActualMonthly_DividesExpensesByMonths(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Groceries", -3000, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		makeTransaction("Gas", -3000, time.Date(2025, 3, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Auto"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC) // ~3 months

	m := calculateMetrics(ts, start, end, 0)

	if !floatEqual(m.TotalExpenses, 9000) {
		t.Fatalf("precondition: TotalExpenses = %v, want 9000", m.TotalExpenses)
	}
	// 9000 / ~2.99 ≈ 3010
	if m.ActualMonthly < 2950 || m.ActualMonthly > 3050 {
		t.Errorf("ActualMonthly = %v, want ~3010", m.ActualMonthly)
	}
}

func TestCalculateMetrics_BudgetOverTarget(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -6000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Groceries", -6000, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		makeTransaction("Gas", -6000, time.Date(2025, 3, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Auto"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC) // ~3 months

	target := 5000.0
	m := calculateMetrics(ts, start, end, target)

	if !m.HasBudgetTarget {
		t.Errorf("HasBudgetTarget = false, want true (target=%v)", target)
	}
	if !floatEqual(m.BudgetTarget, target) {
		t.Errorf("BudgetTarget = %v, want %v", m.BudgetTarget, target)
	}
	// ActualMonthly ≈ 6020; PerMonthDelta = 6020 - 5000 = ~1020
	if m.PerMonthDelta < 950 || m.PerMonthDelta > 1100 {
		t.Errorf("PerMonthDelta = %v, want ~1020 (over)", m.PerMonthDelta)
	}
	// CumulativeDelta = 18000 - 5000 * 2.99 = 18000 - 14950 = ~3050
	if m.CumulativeDelta < 2900 || m.CumulativeDelta > 3200 {
		t.Errorf("CumulativeDelta = %v, want ~3050 (over)", m.CumulativeDelta)
	}
}

func TestCalculateMetrics_BudgetUnderTarget(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Groceries", -3000, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		makeTransaction("Gas", -3000, time.Date(2025, 3, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Auto"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)

	m := calculateMetrics(ts, start, end, 5000)

	// ActualMonthly ≈ 3010; PerMonthDelta = 3010 - 5000 = ~-1990
	if m.PerMonthDelta > -1900 || m.PerMonthDelta < -2050 {
		t.Errorf("PerMonthDelta = %v, want ~-1990 (under)", m.PerMonthDelta)
	}
	// CumulativeDelta = 9000 - 14950 = ~-5950
	if m.CumulativeDelta > -5800 || m.CumulativeDelta < -6100 {
		t.Errorf("CumulativeDelta = %v, want ~-5950 (under)", m.CumulativeDelta)
	}
}

func TestCalculateMetrics_NoBudgetTarget(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -3000, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	m := calculateMetrics(ts, start, end, 0)

	if m.HasBudgetTarget {
		t.Errorf("HasBudgetTarget = true, want false when target=0")
	}
	if !floatEqual(m.BudgetTarget, 0) {
		t.Errorf("BudgetTarget = %v, want 0", m.BudgetTarget)
	}
	// ActualMonthly should still be computed
	if m.ActualMonthly == 0 {
		t.Errorf("ActualMonthly = 0, want non-zero (TotalExpenses > 0 even without target)")
	}
	// PerMonthDelta = ActualMonthly - 0 = ActualMonthly
	if !floatEqual(m.PerMonthDelta, m.ActualMonthly) {
		t.Errorf("PerMonthDelta = %v, want ActualMonthly (%v) when target=0", m.PerMonthDelta, m.ActualMonthly)
	}
	// CumulativeDelta = TotalExpenses - 0 = TotalExpenses
	if !floatEqual(m.CumulativeDelta, m.TotalExpenses) {
		t.Errorf("CumulativeDelta = %v, want TotalExpenses (%v) when target=0", m.CumulativeDelta, m.TotalExpenses)
	}
}

func TestCalculateMetrics_SingleDayRange_NoDivideByZero(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Rent", -1000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)
	day := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("calculateMetrics panicked on single-day range: %v", r)
		}
	}()
	m := calculateMetrics(ts, day, day, 5000)

	// (0 + 1) / 30.4375 ≈ 0.0329
	if m.MonthsInRange < 0.03 || m.MonthsInRange > 0.04 {
		t.Errorf("MonthsInRange = %v, want ~0.033 (single-day span)", m.MonthsInRange)
	}
}
```

- [ ] **Step 3: Update the three existing `calculateMetrics` test calls**

In `internal/handlers/dashboard/handlers_test.go`, change each existing call site from `calculateMetrics(ts)` to `calculateMetrics(ts, ts.MinDate(), ts.MaxDate(), 0)`. Specifically:

- Line ~239 (`TestCalculateMetrics_BasicTotals`): `m := calculateMetrics(ts, ts.MinDate(), ts.MaxDate(), 0)`
- Line ~265 (`TestCalculateMetrics_ZeroIncome`): `m := calculateMetrics(ts, ts.MinDate(), ts.MaxDate(), 0)`
- Line ~288 (`TestCalculateMetrics_TrendsLimitedToSixMonths`): `m := calculateMetrics(ts, ts.MinDate(), ts.MaxDate(), 0)`

- [ ] **Step 4: Run the new tests to confirm they fail**

Run: `go test ./internal/handlers/dashboard/ -run TestCalculateMetrics_MonthsInRange_ApproxFromDates -v`
Expected: FAIL — `calculateMetrics` does not yet accept extra arguments. (The package will not compile until Step 5; that's OK — the failing-test gate is satisfied.)

- [ ] **Step 5: Update `calculateMetrics` signature and implementation**

In `internal/handlers/dashboard/handlers.go`, replace the existing `calculateMetrics` function (lines ~509–579) with:

```go
// avgDaysPerMonth is 365.25 / 12 — the standard average-calendar-month length.
const avgDaysPerMonth = 30.4375

// monthsBetween returns the average-calendar-month count between two
// inclusive dates. A single-day span returns 1/avgDaysPerMonth (~0.033),
// never zero, so callers can safely divide by the result.
func monthsBetween(start, end time.Time) float64 {
	days := end.Sub(start).Hours()/24 + 1
	if days < 1 {
		days = 1
	}
	return days / avgDaysPerMonth
}

func calculateMetrics(ts *models.TransactionSet, rangeStart, rangeEnd time.Time, budgetTarget float64) *models.DashboardMetrics {
	income := ts.FilterByType(models.Income)
	outflows := ts.FilterByType(models.Outflow)

	totalIncome := income.SumAmount()
	totalExpenses := math.Abs(outflows.SumAmount())
	netSavings := totalIncome - totalExpenses

	var savingsRate float64
	if totalIncome > 0 {
		savingsRate = (netSavings / totalIncome) * 100
	}

	// Budget tracking — uses the dashboard date range (not transaction min/max)
	// so a sparse range still divides expenses across the full window the user
	// selected.
	monthsInRange := monthsBetween(rangeStart, rangeEnd)
	actualMonthly := totalExpenses / monthsInRange
	perMonthDelta := actualMonthly - budgetTarget
	cumulativeDelta := totalExpenses - budgetTarget*monthsInRange
	hasBudgetTarget := budgetTarget > 0

	// Calculate monthly trends
	var incomeTrend, expensesTrend, savingsTrend []float64
	var trendLabels []string

	monthlyIncome := income.GroupByMonth()
	monthlyOutflows := outflows.GroupByMonth()

	// Get sorted months
	monthSet := make(map[string]bool)
	for m := range monthlyIncome {
		monthSet[m] = true
	}
	for m := range monthlyOutflows {
		monthSet[m] = true
	}

	var months []string
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Strings(months)

	// Take last 6 months
	if len(months) > 6 {
		months = months[len(months)-6:]
	}

	for _, m := range months {
		incAmt := 0.0
		if inc, ok := monthlyIncome[m]; ok {
			incAmt = inc.SumAmount()
		}

		expAmt := 0.0
		if exp, ok := monthlyOutflows[m]; ok {
			expAmt = math.Abs(exp.SumAmount())
		}

		incomeTrend = append(incomeTrend, incAmt)
		expensesTrend = append(expensesTrend, expAmt)
		savingsTrend = append(savingsTrend, incAmt-expAmt)
		trendLabels = append(trendLabels, m)
	}

	return &models.DashboardMetrics{
		TotalIncome:      totalIncome,
		TotalExpenses:    totalExpenses,
		NetSavings:       netSavings,
		SavingsRate:      savingsRate,
		TransactionCount: ts.Len(),
		StartDate:        ts.MinDate(),
		EndDate:          ts.MaxDate(),
		IncomeTrend:      incomeTrend,
		ExpensesTrend:    expensesTrend,
		SavingsTrend:     savingsTrend,
		TrendLabels:      trendLabels,
		MonthsInRange:    monthsInRange,
		ActualMonthly:    actualMonthly,
		BudgetTarget:     budgetTarget,
		PerMonthDelta:    perMonthDelta,
		CumulativeDelta:  cumulativeDelta,
		HasBudgetTarget:  hasBudgetTarget,
	}
}
```

- [ ] **Step 6: Update both production call sites**

In `internal/handlers/dashboard/handlers.go`:

- Line 62 (`handleDashboard`): change `metrics := calculateMetrics(filtered)` to `metrics := calculateMetrics(filtered, startDate, endDate, 0)`
- Line 112 (`handleKPIsPartial`): change `metrics := calculateMetrics(filtered)` to `metrics := calculateMetrics(filtered, startDate, endDate, 0)`

The literal `0` for the target is temporary; Task 4 wires this to the SettingsManager.

- [ ] **Step 7: Update both `calculateMetrics` calls inside `calculateComparison`**

`calculateComparison` (around line 581) calls `calculateMetrics` twice (lines ~604–605):

```go
currentMetrics := calculateMetrics(currentFiltered)
compMetrics := calculateMetrics(compFiltered)
```

Change to:

```go
currentMetrics := calculateMetrics(currentFiltered, start, end, 0)
compMetrics := calculateMetrics(compFiltered, compStart, compEnd, 0)
```

(Target wiring for the comparison comes in Task 4.)

- [ ] **Step 8: Run all dashboard tests**

Run: `go test ./internal/handlers/dashboard/... -v`
Expected: PASS for all `TestCalculateMetrics_*` tests, including the six new ones and the three updated ones.

- [ ] **Step 9: Run the full test suite**

Run: `go test ./...`
Expected: all packages pass.

- [ ] **Step 10: Commit**

```bash
git add internal/handlers/dashboard/handlers.go internal/handlers/dashboard/handlers_test.go
git commit -m "$(cat <<'EOF'
feat(dashboard): compute monthly + cumulative budget variance

calculateMetrics now takes the dashboard date range and a budget target
and populates MonthsInRange, ActualMonthly, BudgetTarget, PerMonthDelta,
CumulativeDelta, and HasBudgetTarget. Target is hard-wired to 0 at the
call sites — wiring to the what-if SettingsManager comes next.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Populate Budget Comparison Deltas

**Files:**
- Modify: `internal/handlers/dashboard/handlers.go` (function `calculateComparison`, lines ~581–620)
- Modify: `internal/handlers/dashboard/handlers_test.go`

`PeriodComparison` already gained `ActualMonthlyChange` and `CumulativeDeltaChange` in Task 1. This task fills them in and tests them.

- [ ] **Step 1: Run impact analysis**

Run: `gitnexus_impact({target: "calculateComparison", direction: "upstream"})`. Report blast radius (expected: `handleDashboard` and `handleKPIsPartial`).

- [ ] **Step 2: Write failing test**

Append to `internal/handlers/dashboard/handlers_test.go`:

```go
func TestCalculateComparison_PopulatesBudgetDeltas(t *testing.T) {
	ts := makeTransactionSet(
		// Jan 2025: 1500 outflow → previous period
		makeTransaction("Rent", -1500, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		// Feb 2025: 2500 outflow → current period (1000 more)
		makeTransaction("Rent", -2500, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	pc := calculateComparison(ts, start, end, "previous")
	if pc == nil || !pc.HasData {
		t.Fatalf("expected non-nil comparison with HasData=true, got %+v", pc)
	}

	// Each period spans ~28 days ≈ 0.95 months.
	// current.ActualMonthly  ≈ 2500 / 0.95 ≈ 2632
	// previous.ActualMonthly ≈ 1500 / 0.95 ≈ 1579
	// ActualMonthlyChange    ≈ 2632 - 1579 ≈ 1053
	if pc.ActualMonthlyChange < 950 || pc.ActualMonthlyChange > 1150 {
		t.Errorf("ActualMonthlyChange = %v, want ~1053", pc.ActualMonthlyChange)
	}

	// CumulativeDeltaChange = current.CumulativeDelta - previous.CumulativeDelta
	// With target=0 (passed below), CumulativeDelta = TotalExpenses, so
	// CumulativeDeltaChange = 2500 - 1500 = 1000
	if pc.CumulativeDeltaChange < 950 || pc.CumulativeDeltaChange > 1050 {
		t.Errorf("CumulativeDeltaChange = %v, want ~1000", pc.CumulativeDeltaChange)
	}
}
```

- [ ] **Step 3: Run the new test to confirm it fails**

Run: `go test ./internal/handlers/dashboard/ -run TestCalculateComparison_PopulatesBudgetDeltas -v`
Expected: FAIL — both delta fields are zero (not yet populated).

- [ ] **Step 4: Wire the delta fields**

In `internal/handlers/dashboard/handlers.go`, find the existing `return &models.PeriodComparison{...}` block at the end of `calculateComparison` and add the two new fields. The block should look like:

```go
return &models.PeriodComparison{
    Current:               currentMetrics,
    Previous:              compMetrics,
    HasData:               true,
    IncomeChange:          incomeChange,
    ExpensesChange:        expensesChange,
    SavingsChange:         savingsChange,
    SavingsRateChange:     savingsRateChange,
    ActualMonthlyChange:   currentMetrics.ActualMonthly - compMetrics.ActualMonthly,
    CumulativeDeltaChange: currentMetrics.CumulativeDelta - compMetrics.CumulativeDelta,
}
```

(Field alignment: gofmt will fix tab alignment if needed; just keep the names exact.)

- [ ] **Step 5: Run the test to confirm it passes**

Run: `go test ./internal/handlers/dashboard/ -run TestCalculateComparison_PopulatesBudgetDeltas -v`
Expected: PASS.

- [ ] **Step 6: Run the full test suite**

Run: `go test ./...`
Expected: all packages pass.

- [ ] **Step 7: Commit**

```bash
git add internal/handlers/dashboard/handlers.go internal/handlers/dashboard/handlers_test.go
git commit -m "$(cat <<'EOF'
feat(dashboard): populate budget comparison deltas

PeriodComparison.ActualMonthlyChange and CumulativeDeltaChange are now
computed as the signed difference between current and previous period
metrics, matching the existing IncomeChange / ExpensesChange pattern.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Wire What-If Target into Dashboard Handlers

**Files:**
- Modify: `internal/handlers/dashboard/handlers.go` (package vars + `Initialize` + both handlers + `calculateComparison`)
- Modify: `cmd/server/main.go` (`dashboard.Initialize` call)
- Modify: `internal/handlers/dashboard/handlers_http_test.go` (`Initialize` call in test setup)

This task connects the dashboard to the what-if `SettingsManager` so the budget target is real, not 0.

- [ ] **Step 1: Run impact analysis on `Initialize`**

Run: `gitnexus_impact({target: "Initialize", direction: "upstream"})` scoped to the dashboard package (or filter the result to the dashboard package's `Initialize`). Report callers. Expected: `cmd/server/main.go` line 83 and the test setup in `handlers_http_test.go`.

- [ ] **Step 2: Update package vars and `Initialize` signature**

In `internal/handlers/dashboard/handlers.go`, edit the imports block to add the retirement package. The full import list should read:

```go
import (
    "bytes"
    "encoding/csv"
    "encoding/json"
    "fmt"
    "log"
    "math"
    "sort"
    "time"

    "github.com/go-chi/chi/v5"
    "net/http"

    "budget2/internal/models"
    "budget2/internal/services/dataloader"
    "budget2/internal/services/majorexpenses"
    "budget2/internal/services/retirement"
    "budget2/internal/templates"
)
```

Replace the package-vars + `Initialize` block (lines ~22–31) with:

```go
var (
    loader        *dataloader.DataLoader
    renderer      *templates.Renderer
    retirementMgr *retirement.SettingsManager
)

// Initialize sets up the dashboard package with required dependencies.
func Initialize(l *dataloader.DataLoader, r *templates.Renderer, rm *retirement.SettingsManager) {
    loader = l
    renderer = r
    retirementMgr = rm
}
```

- [ ] **Step 3: Add a small `currentBudgetTarget` helper**

Immediately after `Initialize` (so the helper sits with its consumer), add:

```go
// currentBudgetTarget returns the active what-if MonthlyLivingExpenses
// value, or 0 if the settings manager is unset or the load fails. The
// dashboard treats a zero target as "no budget configured" and renders
// the fallback card; loading errors are non-fatal.
func currentBudgetTarget() float64 {
    if retirementMgr == nil {
        return 0
    }
    settings, err := retirementMgr.Load()
    if err != nil || settings == nil {
        return 0
    }
    return settings.MonthlyLivingExpenses
}
```

- [ ] **Step 4: Pass the live target into `calculateMetrics` from both handlers**

In `internal/handlers/dashboard/handlers.go`:

- In `handleDashboard` (the line previously `metrics := calculateMetrics(filtered, startDate, endDate, 0)` from Task 2), change `0` to `currentBudgetTarget()`:

  ```go
  target := currentBudgetTarget()
  metrics := calculateMetrics(filtered, startDate, endDate, target)
  ```

- Do the same in `handleKPIsPartial`.

- [ ] **Step 5: Pass the live target into `calculateComparison`**

`calculateComparison` currently passes `0` to its two `calculateMetrics` calls. Change its signature to accept a `budgetTarget float64`:

```go
func calculateComparison(data *models.TransactionSet, start, end time.Time, compType string, budgetTarget float64) *models.PeriodComparison {
```

Inside the function, use `budgetTarget` in the two `calculateMetrics` calls (replacing the hard-wired `0`):

```go
currentMetrics := calculateMetrics(currentFiltered, start, end, budgetTarget)
compMetrics := calculateMetrics(compFiltered, compStart, compEnd, budgetTarget)
```

Update both call sites in the handlers (around lines 67 and 116) to pass the same target:

```go
periodComparison = calculateComparison(data, startDate, endDate, comparison, target)
```

(The variable `target` already exists in both handlers from Step 4.)

- [ ] **Step 6: Update the existing `TestCalculateComparison_*` tests**

In `internal/handlers/dashboard/handlers_test.go`, every existing call to `calculateComparison(...)` needs the new trailing `0` argument (no target). Add `, 0` before the closing paren on each call site. Example:

```go
pc := calculateComparison(ts, start, end, "previous")
```

becomes:

```go
pc := calculateComparison(ts, start, end, "previous", 0)
```

Apply this to **all** `calculateComparison(` test call sites in the file (including the new `TestCalculateComparison_PopulatesBudgetDeltas` from Task 3).

- [ ] **Step 7: Update `cmd/server/main.go`**

Find line 83 and change:

```go
dashboard.Initialize(loader, renderer)
```

to:

```go
dashboard.Initialize(loader, renderer, retirementMgr)
```

(The `retirementMgr` variable already exists at this point in `main.go` — it's used on the very next line for `whatif.Initialize`.)

- [ ] **Step 8: Update HTTP test setup**

Open `internal/handlers/dashboard/handlers_http_test.go` and find every call to `Initialize(...)`. Add a `nil` third argument (the tests don't exercise the budget path here — that's covered by Task 5):

```go
Initialize(loader, renderer, nil)
```

If there is no helper that wraps `Initialize` and several tests call it directly, update them all. If they share a setup helper, update only the helper.

Run: `grep -n "Initialize(" internal/handlers/dashboard/handlers_http_test.go` to find the call sites.

- [ ] **Step 9: Build the whole project**

Run: `go build ./...`
Expected: clean build. If any unrelated caller breaks, stop and re-check the impact analysis from Step 1.

- [ ] **Step 10: Run the full test suite**

Run: `go test ./...`
Expected: all packages pass.

- [ ] **Step 11: Commit**

```bash
git add internal/handlers/dashboard/handlers.go internal/handlers/dashboard/handlers_test.go internal/handlers/dashboard/handlers_http_test.go cmd/server/main.go
git commit -m "$(cat <<'EOF'
feat(dashboard): wire what-if MonthlyLivingExpenses as budget target

Dashboard.Initialize now takes a *retirement.SettingsManager. Both
handlers and calculateComparison forward the active what-if target
into calculateMetrics so the new budget-tracking fields reflect the
real user setting. Missing/zero target is treated as "no budget".

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Replace KPI Cards in the Template

**Files:**
- Modify: `web/templates/components/kpis.html`
- Modify: `internal/handlers/dashboard/handlers_http_test.go` (add card-content assertions)

This task replaces the two right-side cards: deletes Savings Rate, replaces Net Savings with the Budget card, and inserts the new Monthly Living Expenses card in slot 3.

- [ ] **Step 1: Write failing HTTP assertions**

Open `internal/handlers/dashboard/handlers_http_test.go`. After the existing setup helpers, append the following test (adapt the helper names — `serveDashboard`, `serveKPIs`, etc. — to whatever the file already provides; use `http.NewRequest` + `httptest.NewRecorder` with the package's `handleDashboard` / `handleKPIsPartial` if no helper exists):

```go
func TestDashboardKPIs_RendersBudgetCards(t *testing.T) {
    rows := [][]string{
        {"Date", "Description", "Amount", "Category"},
        {"2025-01-15", "Salary", "5000", "Payroll"},
        {"2025-01-05", "Rent", "-1500", "Housing"},
    }
    _, dl, cleanup := writeTempCSV(t, rows)
    defer cleanup()

    rendererInstance, err := templates.New(testutil.TemplateRoot())
    if err != nil {
        t.Fatalf("template renderer: %v", err)
    }
    Initialize(dl, rendererInstance, nil)

    req := httptest.NewRequest("GET", "/dashboard/kpis?start=2025-01-01&end=2025-01-31", nil)
    w := httptest.NewRecorder()
    handleKPIsPartial(w, req)

    body := w.Body.String()
    if !strings.Contains(body, "Monthly Living Expenses") {
        t.Errorf("response missing 'Monthly Living Expenses' card; body:\n%s", body)
    }
    if !strings.Contains(body, "Budget") {
        t.Errorf("response missing 'Budget' card; body:\n%s", body)
    }
    if strings.Contains(body, "Savings Rate") {
        t.Errorf("response still contains 'Savings Rate' card (should be removed); body:\n%s", body)
    }
    // No target loaded (nil retirementMgr) → fallback link
    if !strings.Contains(body, "Set a budget in What-If") {
        t.Errorf("response missing fallback link 'Set a budget in What-If'; body:\n%s", body)
    }
}
```

If `templates.New` / `testutil.TemplateRoot()` are not the actual API, mirror whatever the existing HTTP tests in this file use — there is already a working pattern to copy.

- [ ] **Step 2: Run the new test to confirm it fails**

Run: `go test ./internal/handlers/dashboard/ -run TestDashboardKPIs_RendersBudgetCards -v`
Expected: FAIL — the rendered HTML still contains "Savings Rate" and lacks "Monthly Living Expenses" and the fallback link.

- [ ] **Step 3: Rewrite `web/templates/components/kpis.html`**

Replace the entire file contents with:

```html
{{define "kpis"}}
<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
    <!-- Total Income -->
    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4 cursor-pointer hover:shadow-lg transition-shadow" onclick="openKPIDetail('income')">
        <div class="flex items-center justify-between">
            <div>
                <p class="text-sm font-medium text-gray-500 dark:text-gray-400">Total Income</p>
                <p class="text-2xl font-bold text-green-600 dark:text-green-400">{{formatMoney .Metrics.TotalIncome}}</p>
                {{if .PeriodComparison}}
                {{if .PeriodComparison.HasData}}
                <p class="text-sm {{if ge .PeriodComparison.IncomeChange 0.0}}text-green-500{{else}}text-red-500{{end}}">
                    {{formatPercent .PeriodComparison.IncomeChange}}%
                </p>
                {{end}}
                {{end}}
            </div>
            <div class="p-3 bg-green-100 dark:bg-green-900/50 rounded-full">
                <svg class="w-6 h-6 text-green-600 dark:text-green-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                        d="M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z">
                    </path>
                </svg>
            </div>
        </div>
        <div id="sparkline-income" class="h-10 mt-2" data-values="{{toJSON .Metrics.IncomeTrend}}" data-color="#22c55e"></div>
    </div>

    <!-- Total Expenses -->
    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4 cursor-pointer hover:shadow-lg transition-shadow" onclick="openKPIDetail('expenses')">
        <div class="flex items-center justify-between">
            <div>
                <p class="text-sm font-medium text-gray-500 dark:text-gray-400">Total Expenses</p>
                <p class="text-2xl font-bold text-red-600 dark:text-red-400">{{formatMoney (abs .Metrics.TotalExpenses)}}</p>
                {{if .PeriodComparison}}
                {{if .PeriodComparison.HasData}}
                <p class="text-sm {{if le .PeriodComparison.ExpensesChange 0.0}}text-green-500{{else}}text-red-500{{end}}">
                    {{formatPercent .PeriodComparison.ExpensesChange}}%
                </p>
                {{end}}
                {{end}}
            </div>
            <div class="p-3 bg-red-100 dark:bg-red-900/50 rounded-full">
                <svg class="w-6 h-6 text-red-600 dark:text-red-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                        d="M3 3h2l.4 2M7 13h10l4-8H5.4M7 13L5.4 5M7 13l-2.293 2.293c-.63.63-.184 1.707.707 1.707H17m0 0a2 2 0 100 4 2 2 0 000-4zm-8 2a2 2 0 11-4 0 2 2 0 014 0z">
                    </path>
                </svg>
            </div>
        </div>
        <div id="sparkline-expenses" class="h-10 mt-2" data-values="{{toJSON .Metrics.ExpensesTrend}}" data-color="#ef4444"></div>
    </div>

    <!-- Monthly Living Expenses -->
    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4 cursor-pointer hover:shadow-lg transition-shadow" onclick="openKPIDetail('expenses')">
        <div class="flex items-center justify-between">
            <div>
                <p class="text-sm font-medium text-gray-500 dark:text-gray-400">Monthly Living Expenses</p>
                <p class="text-2xl font-bold text-gray-900 dark:text-gray-100">{{formatMoney .Metrics.ActualMonthly}}</p>
                {{if .Metrics.HasBudgetTarget}}
                <p class="text-sm {{if gt .Metrics.PerMonthDelta 0.0}}text-red-600 dark:text-red-400{{else}}text-green-600 dark:text-green-400{{end}}">
                    Target {{formatMoney .Metrics.BudgetTarget}} · {{formatMoney (abs .Metrics.PerMonthDelta)}}/mo {{if gt .Metrics.PerMonthDelta 0.0}}over{{else}}under{{end}}
                </p>
                {{else}}
                <p class="text-sm text-gray-400 dark:text-gray-500 italic">No budget set</p>
                {{end}}
                {{if .PeriodComparison}}
                {{if .PeriodComparison.HasData}}
                <p class="text-xs text-gray-400 dark:text-gray-500 mt-1">
                    {{if ge .PeriodComparison.ActualMonthlyChange 0.0}}+{{else}}-{{end}}{{formatMoney (abs .PeriodComparison.ActualMonthlyChange)}}/mo vs prior
                </p>
                {{end}}
                {{end}}
            </div>
            <div class="p-3 bg-gray-100 dark:bg-gray-700 rounded-full">
                <svg class="w-6 h-6 text-gray-600 dark:text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                        d="M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1"></path>
                </svg>
            </div>
        </div>
        <div id="sparkline-monthly" class="h-10 mt-2" data-values="{{toJSON .Metrics.ExpensesTrend}}" data-color="#9ca3af"></div>
    </div>

    <!-- Budget (cumulative variance for the period) -->
    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4 cursor-pointer hover:shadow-lg transition-shadow" onclick="openKPIDetail('expenses')">
        <div class="flex items-center justify-between">
            <div>
                <p class="text-sm font-medium text-gray-500 dark:text-gray-400">Budget</p>
                {{if .Metrics.HasBudgetTarget}}
                {{if gt .Metrics.CumulativeDelta 1.0}}
                <p class="text-2xl font-bold text-red-600 dark:text-red-400">{{formatMoney (abs .Metrics.CumulativeDelta)}} over</p>
                {{else if lt .Metrics.CumulativeDelta -1.0}}
                <p class="text-2xl font-bold text-green-600 dark:text-green-400">{{formatMoney (abs .Metrics.CumulativeDelta)}} under</p>
                {{else}}
                <p class="text-2xl font-bold text-gray-900 dark:text-gray-100">On budget</p>
                {{end}}
                <p class="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    {{printf "%.1f" .Metrics.MonthsInRange}} mo @ {{if ge .Metrics.PerMonthDelta 0.0}}+{{else}}-{{end}}{{formatMoney (abs .Metrics.PerMonthDelta)}}/mo
                </p>
                {{if .PeriodComparison}}
                {{if .PeriodComparison.HasData}}
                <p class="text-xs text-gray-400 dark:text-gray-500 mt-1">
                    {{if ge .PeriodComparison.CumulativeDeltaChange 0.0}}+{{else}}-{{end}}{{formatMoney (abs .PeriodComparison.CumulativeDeltaChange)}} vs prior
                </p>
                {{end}}
                {{end}}
                {{else}}
                <p class="text-2xl font-bold text-gray-400 dark:text-gray-500 italic">Not set</p>
                <a href="/whatif" class="text-sm text-indigo-600 dark:text-indigo-400 hover:underline mt-1 inline-block">Set a budget in What-If →</a>
                {{end}}
            </div>
            <div class="p-3 {{if and .Metrics.HasBudgetTarget (gt .Metrics.CumulativeDelta 0.0)}}bg-red-100 dark:bg-red-900/50{{else if .Metrics.HasBudgetTarget}}bg-green-100 dark:bg-green-900/50{{else}}bg-gray-100 dark:bg-gray-700{{end}} rounded-full">
                <svg class="w-6 h-6 {{if and .Metrics.HasBudgetTarget (gt .Metrics.CumulativeDelta 0.0)}}text-red-600 dark:text-red-400{{else if .Metrics.HasBudgetTarget}}text-green-600 dark:text-green-400{{else}}text-gray-500 dark:text-gray-400{{end}}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                        d="M9 7h6m0 10v-3m-3 3h.01M9 17h.01M9 14h.01M12 14h.01M15 11h.01M12 11h.01M9 11h.01M7 21h10a2 2 0 002-2V5a2 2 0 00-2-2H7a2 2 0 00-2 2v14a2 2 0 002 2z"></path>
                </svg>
            </div>
        </div>
    </div>
</div>
{{end}}
```

Notes for the implementer:
- The Monthly Living Expenses card reuses `ExpensesTrend` for the sparkline (no new data wiring).
- Both new cards open `openKPIDetail('expenses')` so no JS changes are needed.
- The Budget card's icon container background and stroke color flip based on over/under/no-target to mirror the existing Net Savings color treatment.

- [ ] **Step 4: Run the new HTTP test to confirm it passes**

Run: `go test ./internal/handlers/dashboard/ -run TestDashboardKPIs_RendersBudgetCards -v`
Expected: PASS.

- [ ] **Step 5: Add an HTTP test for the with-target render path**

Append to `internal/handlers/dashboard/handlers_http_test.go`. This test stands up a real `*retirement.SettingsManager` rooted in a temp dir and writes a settings file with `monthly_living_expenses: 1000`, then asserts the Budget card shows "over" / "under" appropriately:

```go
func TestDashboardKPIs_RendersBudgetCards_WithTarget(t *testing.T) {
    rows := [][]string{
        {"Date", "Description", "Amount", "Category"},
        {"2025-01-05", "Rent", "-3000", "Housing"},
    }
    _, dl, cleanup := writeTempCSV(t, rows)
    defer cleanup()

    rendererInstance, err := templates.New(testutil.TemplateRoot())
    if err != nil {
        t.Fatalf("template renderer: %v", err)
    }

    settingsDir := t.TempDir()
    settingsPath := filepath.Join(settingsDir, "whatif_settings.json")
    if err := os.WriteFile(settingsPath, []byte(`{"monthly_living_expenses": 1000}`), 0o600); err != nil {
        t.Fatalf("write settings: %v", err)
    }
    store, err := storage.NewPlaintext(settingsDir)
    if err != nil {
        t.Fatalf("storage: %v", err)
    }
    rm := retirement.NewSettingsManager(settingsDir, store)

    Initialize(dl, rendererInstance, rm)

    req := httptest.NewRequest("GET", "/dashboard/kpis?start=2025-01-01&end=2025-01-31", nil)
    w := httptest.NewRecorder()
    handleKPIsPartial(w, req)

    body := w.Body.String()
    if strings.Contains(body, "Set a budget in What-If") {
        t.Errorf("budget loaded but fallback link still rendered; body:\n%s", body)
    }
    // 3000 actual vs 1000 target → ~2000 over for the month
    if !strings.Contains(body, "over") {
        t.Errorf("expected Budget card to show 'over'; body:\n%s", body)
    }
    if !strings.Contains(body, "Target") {
        t.Errorf("expected Monthly Living Expenses card to show 'Target'; body:\n%s", body)
    }
}
```

If `storage.NewPlaintext` is not the actual constructor name, run `grep -rn "func New" internal/services/storage/ | head` to find the correct constructor and adapt. The point of the test is real-target rendering — adapt the storage init to whatever the package supports.

- [ ] **Step 6: Run the new test to confirm it passes**

Run: `go test ./internal/handlers/dashboard/ -run TestDashboardKPIs_RendersBudgetCards_WithTarget -v`
Expected: PASS.

- [ ] **Step 7: Run the full test suite**

Run: `go test ./...`
Expected: all packages pass.

- [ ] **Step 8: Commit**

```bash
git add web/templates/components/kpis.html internal/handlers/dashboard/handlers_http_test.go
git commit -m "$(cat <<'EOF'
feat(dashboard): show Monthly Living Expenses and Budget KPI cards

Drops the Savings Rate KPI. The Net Savings slot becomes a Budget card
showing cumulative over/under variance for the selected period. A new
Monthly Living Expenses card sits between Total Expenses and Budget,
displaying the per-month average alongside the what-if target. When no
target is set, the Budget card shows a fallback link to /whatif.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Manual Verification

**Files:** none

This task is manual smoke-testing — the spec calls out that UI changes must be verified in a browser. No commit.

- [ ] **Step 1: Build and run the server**

Run: `make build` (or `go build -o budget2 ./cmd/server`) then run `./budget2` (or the project's standard run command from `Makefile` — check `make help`).

- [ ] **Step 2: Open the dashboard in a browser**

Navigate to `http://localhost:<port>/dashboard`. Verify:

- Top KPI row shows exactly four cards: **Total Income**, **Total Expenses**, **Monthly Living Expenses**, **Budget**.
- **Savings Rate** card is gone.
- Monthly Living Expenses headline equals Total Expenses ÷ months for the date range (eyeball check; roughly the right order of magnitude).
- If a what-if budget is set, Monthly Living Expenses sub-line reads `Target $X · $Y/mo over|under` and is red/green appropriately.
- Budget card shows `$Z over` or `$Z under` (red/green) with the `X.X mo @ ±$Y/mo` sub-line.
- If no budget is set in what-if, Budget card reads `Not set` with the `Set a budget in What-If →` link.

- [ ] **Step 3: Exercise the date filter**

Click each preset (1M, 2M, 3M, 6M, 12M, YTD, All). Verify the Monthly Living Expenses and Budget numbers update via htmx swap (network request to `/dashboard/kpis`).

- [ ] **Step 4: Exercise the comparison dropdown**

Set Compare to "Previous period". Verify both new cards show a `vs prior` line. Set to "Same period last year" and confirm the same.

- [ ] **Step 5: Exercise the no-target fallback**

If your local what-if settings have a non-zero `MonthlyLivingExpenses`, temporarily set it to `0` via the what-if page (or remove the settings file) and reload the dashboard. Confirm the Budget card switches to `Not set` and the link works.

- [ ] **Step 6: Run `gitnexus_detect_changes`**

Per `CLAUDE.md`:

Run: `gitnexus_detect_changes()`. Confirm only the symbols you intended (`calculateMetrics`, `calculateComparison`, `Initialize`, `currentBudgetTarget`, `monthsBetween`, the model structs, and the kpis template) appear as affected.

If unexpected symbols show up, investigate before considering the task complete.

- [ ] **Step 7: Final test run**

Run: `go test ./...`
Expected: all packages pass.

---

## Self-Review Checklist (for the planner)

This block is for the engineer reading the plan to know what was checked, not a step to execute.

**Spec coverage:** All seven acceptance criteria from the spec are covered:
1. Four-card layout — Task 5 template rewrite.
2. Monthly Living Expenses headline equals `TotalExpenses ÷ months` — Task 2 logic + Task 5 template.
3. Budget card cumulative figure with sign / color / "over | under | On budget" — Task 5 template (and Task 2/3 deltas).
4. No-target fallback — Task 5 template (and the nil-safe `currentBudgetTarget` from Task 4).
5. Date filter refresh — already wired via `/dashboard/kpis` htmx swap; Task 6 verifies.
6. Period comparison lines on the new cards — Task 3 deltas + Task 5 template.
7. All existing tests pass + new tests added + ceiling held — Task 2/3/5 each commit only with `go test ./...` green.

**Type/name consistency:** `currentBudgetTarget`, `monthsBetween`, `avgDaysPerMonth`, `MonthsInRange`, `ActualMonthly`, `BudgetTarget`, `PerMonthDelta`, `CumulativeDelta`, `HasBudgetTarget`, `ActualMonthlyChange`, `CumulativeDeltaChange`, `retirementMgr` — all defined exactly once and referenced consistently across tasks.

**No placeholders:** Every code block is literal. The only "adapt to real API" notes are flagged inline (Task 5 Step 1 / Step 5 storage init) and explain the fallback path the engineer should take.
