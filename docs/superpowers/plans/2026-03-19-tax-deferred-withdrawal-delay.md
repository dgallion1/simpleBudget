# Tax-Deferred Withdrawal Delay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to delay tax-deferred account withdrawals by N years, letting that account grow while expenses are covered by the existing withdrawal order: income sources, taxable account, then Roth. Tax-deferred remains the last resort until the delay expires.

**Architecture:** Add a `TaxDeferredDelayYears` field to `WhatIfSettings`. Every projection engine that models withdrawals must respect it: `RunProjection`, Monte Carlo simulation, and historical backtest. During the delay period, the final "withdraw from tax-deferred" step is skipped, while RMDs at age 73+ still execute normally. Big-ticket expenses must also avoid tax-deferred withdrawals during the delay window unless the withdrawal is an RMD. If taxable and Roth funds run out while tax-deferred assets are still locked by the delay, treat that month as a temporary accessibility shortfall rather than true portfolio depletion.

**Tech Stack:** Go, HTML templates, HTMX

---

### Task 1: Add field to WhatIfSettings model

**Files:**
- Modify: `internal/models/whatif.go:53` (add field after `ProjectionYears`)
- Modify: `internal/models/whatif.go:420` (add default value in `DefaultWhatIfSettings`)

- [ ] **Step 1: Add the field to WhatIfSettings struct**

In `internal/models/whatif.go`, add after line 54 (`SteadyStateOverrideYear`):

```go
TaxDeferredDelayYears int `json:"tax_deferred_delay_years"` // Years before tax-deferred withdrawals begin (0 = immediate)
```

- [ ] **Step 2: Add default value**

In `DefaultWhatIfSettings()` around line 420, add:

```go
TaxDeferredDelayYears: 0, // No delay by default
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 4: Commit**

```bash
git add internal/models/whatif.go
git commit -m "feat: add TaxDeferredDelayYears field to WhatIfSettings"
```

---

### Task 2: Wire up handler and settings manager

**Files:**
- Modify: `internal/handlers/whatif/handlers.go:260-340` (parse form field in `handleWhatIfSettings`)
- Modify: `internal/services/retirement/settings.go:483-485` (apply update in `UpdateSettings`)

- [ ] **Step 1: Add form parsing in handler**

In `internal/handlers/whatif/handlers.go`, in `handleWhatIfSettings`, add after the `projection_years` parsing block (search for `projection_years` in that function):

```go
if v, err := parseFormInt(r, "tax_deferred_delay_years"); err != nil {
    renderError(w, "Invalid tax-deferred delay: "+err.Error(), http.StatusBadRequest)
    return
} else if r.FormValue("tax_deferred_delay_years") != "" {
    if v < 0 || v > 30 {
        renderError(w, "Tax-deferred delay must be between 0 and 30 years", http.StatusBadRequest)
        return
    }
    updates["tax_deferred_delay_years"] = v
}
```

- [ ] **Step 2: Add settings application in UpdateSettings**

In `internal/services/retirement/settings.go`, in `UpdateSettings`, add after the `projection_years` case (around line 485):

```go
if v, ok := updates["tax_deferred_delay_years"].(int); ok {
    settings.TaxDeferredDelayYears = v
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 4: Commit**

```bash
git add internal/handlers/whatif/handlers.go internal/services/retirement/settings.go
git commit -m "feat: wire tax-deferred delay through handler and settings manager"
```

---

### Task 3: Implement withdrawal delay in deterministic + Monte Carlo calculators (TDD)

**Files:**
- Create: `internal/services/retirement/calculator_delay_test.go`
- Modify: `internal/services/retirement/calculator.go:394-431` (withdrawal logic in `RunProjection`)
- Modify: `internal/services/retirement/calculator.go:1525-1554` (withdrawal logic in `runSingleMonteCarloSimulation`)

- [ ] **Step 1: Write failing test — delay blocks tax-deferred withdrawals**

Create `internal/services/retirement/calculator_delay_test.go`:

```go
package retirement

import (
    "testing"

    "budget2/internal/models"
)

func TestTaxDeferredDelay_BlocksWithdrawalsDuringDelay(t *testing.T) {
    settings := models.DefaultWhatIfSettings()
    settings.PortfolioValue = 500000
    settings.TaxDeferredPercent = 80  // $400k tax-deferred
    settings.RothPercent = 0          // $0 Roth
    // Taxable = 20% = $100k
    settings.MonthlyLivingExpenses = 3000
    settings.ProjectionYears = 10
    settings.CurrentAge = 55 // Well below RMD age
    settings.TaxDeferredDelayYears = 5
    settings.InvestmentReturn = 0.001 // Near-zero to simplify math

    calc := NewCalculator(settings)
    result := calc.RunProjection()

    // During first 5 years (months 0-59), no tax-deferred withdrawals
    for _, pm := range result.Months {
        year := pm.Month / 12
        if year < 5 && pm.WithdrawalFromTaxDeferred > 0 {
            t.Errorf("month %d (year %d): expected no tax-deferred withdrawal during delay, got $%.2f",
                pm.Month, year, pm.WithdrawalFromTaxDeferred)
            break
        }
    }

    // After delay (year 5+), tax-deferred withdrawals should occur
    // (taxable will be depleted by then, so tax-deferred must kick in)
    hasPostDelayTDWithdrawal := false
    for _, pm := range result.Months {
        year := pm.Month / 12
        if year >= 5 && pm.WithdrawalFromTaxDeferred > 0 {
            hasPostDelayTDWithdrawal = true
            break
        }
    }
    if !hasPostDelayTDWithdrawal {
        t.Error("expected tax-deferred withdrawals after delay period ended")
    }
}

func TestTaxDeferredDelay_ZeroMeansNoDelay(t *testing.T) {
    settings := models.DefaultWhatIfSettings()
    settings.PortfolioValue = 500000
    settings.TaxDeferredPercent = 80
    settings.RothPercent = 0
    settings.MonthlyLivingExpenses = 5000
    settings.ProjectionYears = 5
    settings.CurrentAge = 55
    settings.TaxDeferredDelayYears = 0 // No delay
    settings.InvestmentReturn = 0.001

    calc := NewCalculator(settings)
    result := calc.RunProjection()

    // With high expenses and small taxable, tax-deferred should be tapped early
    hasTDWithdrawalYear0 := false
    for _, pm := range result.Months {
        if pm.Month/12 == 0 && pm.WithdrawalFromTaxDeferred > 0 {
            hasTDWithdrawalYear0 = true
            break
        }
    }
    // Taxable is $100k, expenses $5k/mo = depleted in ~20 months
    // But once taxable runs out in year 0-1, tax-deferred kicks in
    // With $5k/mo expenses and $100k taxable, should need tax-deferred within year 2
    hasTDWithdrawalEarly := false
    for _, pm := range result.Months {
        if pm.Month/12 <= 2 && pm.WithdrawalFromTaxDeferred > 0 {
            hasTDWithdrawalEarly = true
            break
        }
    }
    if !hasTDWithdrawalEarly {
        t.Error("with no delay and high expenses, expected early tax-deferred withdrawals")
    }
    _ = hasTDWithdrawalYear0 // may or may not happen in year 0 depending on taxable balance
}

func TestTaxDeferredDelay_RMDOverridesDelay(t *testing.T) {
    settings := models.DefaultWhatIfSettings()
    settings.PortfolioValue = 1000000
    settings.TaxDeferredPercent = 80
    settings.RothPercent = 0
    settings.MonthlyLivingExpenses = 2000
    settings.ProjectionYears = 15
    settings.CurrentAge = 65 // Will hit 73 at year 8
    settings.TaxDeferredDelayYears = 15 // Delay longer than RMD trigger
    settings.InvestmentReturn = 0.001

    calc := NewCalculator(settings)
    result := calc.RunProjection()

    // At age 73 (year 8), RMD should force tax-deferred withdrawals despite delay
    hasRMDWithdrawal := false
    for _, pm := range result.Months {
        year := pm.Month / 12
        // Age 73 = year 8, RMD should kick in
        if year >= 8 && pm.WithdrawalFromTaxDeferred > 0 {
            hasRMDWithdrawal = true
            break
        }
    }
    if !hasRMDWithdrawal {
        t.Error("expected RMD to override tax-deferred delay at age 73")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/retirement/ -run TestTaxDeferredDelay -v`
Expected: deterministic delay test FAILS first because tax-deferred withdrawals happen during delay; Monte Carlo behavior is still unchanged.

- [ ] **Step 3: Implement the delay logic in `RunProjection` and `runSingleMonteCarloSimulation`**

In `internal/services/retirement/calculator.go`, apply the same delay guard in both withdrawal sections. The key change is: during the delay period, skip the final tax-deferred withdrawal step. RMD withdrawals are unaffected and must still occur when legally required.

Prefer extracting the shared withdrawal-order decision into a helper used by both functions so deterministic and Monte Carlo projections cannot drift again. If you do that, add or update tests around the helper rather than relying on a separate Monte Carlo-only assertion.

Find this block (around line 424):

```go
// Fourth, withdraw additional from tax-deferred (taxed as ordinary income)
if neededFromPortfolio > 0 && taxDeferredBalance > 0 {
```

Replace with this pattern in both functions:

```go
// Fourth, withdraw additional from tax-deferred (taxed as ordinary income)
// Skip if within the tax-deferred delay period (RMD withdrawals above still apply)
taxDeferredDelayActive := s.TaxDeferredDelayYears > 0 && currentYear < s.TaxDeferredDelayYears
if neededFromPortfolio > 0 && taxDeferredBalance > 0 && !taxDeferredDelayActive {
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/ -run TestTaxDeferredDelay -v`
Expected: all delay tests PASS

- [ ] **Step 5: Run full test suite**

Run: `go test ./internal/services/retirement/ -v`
Expected: all existing tests still pass

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/calculator.go internal/services/retirement/calculator_delay_test.go
git commit -m "feat: implement tax-deferred withdrawal delay in calculator"
```

---

### Task 4: Apply delay to historical backtest and big-ticket withdrawals

**Files:**
- Modify: `internal/services/retirement/backtest.go:284-315` (withdrawal logic in historical backtest)
- Modify: `internal/services/retirement/calculator.go:294-325` (big-ticket expense withdrawal order)
- Modify: `internal/services/retirement/backtest.go:212-241` (big-ticket expense withdrawal order in backtest)

- [ ] **Step 1: Add delay guard to historical backtest monthly withdrawals**

In `internal/services/retirement/backtest.go`, apply the same `taxDeferredDelayActive` guard used in `RunProjection` so the backtest uses the same withdrawal policy. Match the deterministic and Monte Carlo paths by treating delay-window shortfalls as temporary whenever tax-deferred funds still exist but are intentionally inaccessible.

- [ ] **Step 2: Prevent big-ticket expenses from bypassing the delay**

In both deterministic and backtest year-boundary big-ticket handling, preserve the existing taxable → Roth → tax-deferred order, but skip the final tax-deferred deduction while the delay is active. If taxable and Roth are insufficient during the delay window, leave the remaining amount unpaid and let the month-level depletion logic surface the shortfall rather than silently tapping tax-deferred.

- [ ] **Step 3: Add regression coverage**

Add tests for:

- historical backtest honors the delay
- historical backtest does not fail solely because tax-deferred funds are temporarily locked
- a big-ticket expense during the delay uses taxable/Roth only and does not deduct from tax-deferred before the delay expires

- [ ] **Step 4: Run retirement service tests**

Run: `go test ./internal/services/retirement/ -v`
Expected: all existing and new tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/calculator.go internal/services/retirement/backtest.go internal/services/retirement/calculator_delay_test.go internal/services/retirement/backtest_test.go
git commit -m "feat: apply tax-deferred delay consistently across projection engines"
```

---

### Task 5: Add UI input to rate-assumptions template

**Files:**
- Modify: `web/templates/components/whatif/rate-assumptions.html:40-79` (add input after portfolio allocation section)

- [ ] **Step 1: Add the delay input to the template**

In `web/templates/components/whatif/rate-assumptions.html`, add after the portfolio allocation closing `</div>` and before the `<!-- Per-Account Asset Allocation -->` section (between lines 79 and 81):

```html
        <!-- Tax-Deferred Withdrawal Delay -->
        <div>
            <label class="block text-sm font-medium text-gray-700 dark:text-gray-200">Delay Tax-Deferred Withdrawals</label>
            <input type="range" name="tax_deferred_delay_years" value="{{.Settings.TaxDeferredDelayYears}}"
                min="0" max="30" step="1"
                class="w-full h-2 bg-gray-200 dark:bg-gray-600 rounded-lg appearance-none cursor-pointer"
                oninput="this.nextElementSibling.textContent = this.value == 0 ? 'No delay' : this.value + ' years'">
            <span class="text-sm text-gray-500 dark:text-gray-300">{{if eq .Settings.TaxDeferredDelayYears 0}}No delay{{else}}{{.Settings.TaxDeferredDelayYears}} years{{end}}</span>
            <p class="text-xs text-gray-400 dark:text-gray-500 mt-1">Cover expenses from income, taxable, and Roth first. RMDs at 73 still apply.</p>
        </div>
```

- [ ] **Step 2: Verify build and manual test**

Run: `go build ./... && go run .`
Expected: clean build. Navigate to What-If page, see new slider, change value triggers recalculation and the projection / Monte Carlo / backtest cards all update consistently.

- [ ] **Step 3: Commit**

```bash
git add web/templates/components/whatif/rate-assumptions.html
git commit -m "feat: add tax-deferred delay slider to what-if UI"
```

---

### Task 6: Update docs

**Files:**
- Modify: `CHANGELOG.md` (add entry under Improvements)
- Modify: `README.md` (update What-If Planner feature description if needed)

- [ ] **Step 1: Update CHANGELOG**

Add under `### Improvements` in `CHANGELOG.md`:

```markdown
- Tax-deferred withdrawal delay — set how many years to wait before tapping tax-deferred accounts, letting them grow while expenses are covered by income, taxable, and Roth first (RMDs at 73 still enforced)
```

- [ ] **Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: add tax-deferred withdrawal delay to changelog"
```
