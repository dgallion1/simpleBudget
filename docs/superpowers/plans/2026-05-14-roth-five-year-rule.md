# Roth IRA Five-Year Rule Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Roth IRA earnings withdrawals correctly trigger ordinary-income tax when the 5-tax-year qualified-distribution clock has not been satisfied, across the deterministic, Monte Carlo, and historical backtest projection loops.

**Architecture:** Add one persisted field (`RothFirstFundedYear`) to `WhatIfSettings`. Carry projection-local `rothBasis` and `rothFirstFundedYear` alongside `rothBalance` through all three loops. Split every Roth withdrawal into basis (always tax-free) and earnings (taxable as ordinary income when clock unsatisfied) via one shared helper. Feed the taxable-earnings amount into the existing monthly tax snapshot and annual tax accumulator. UI gets one input field and a small set of conditional display strings.

**Tech Stack:** Go 1.22+, chi router, htmx, file-backed JSON settings. Tests use the standard `testing` package with table-driven style. Spec at `docs/superpowers/specs/2026-05-13-roth-five-year-rule-design.md`.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/models/whatif.go` | Add `RothFirstFundedYear int` field; defaults to 0 |
| `internal/services/retirement/engine/loop_helpers.go` | Add `RothQualifiedDistributionClockSatisfied`; extend `ApplyRothConversionAtYear`; extend `ApplyTaxStateMonth`; update `ApplyBigTicketItemsForYear` |
| `internal/services/retirement/engine/portfolio_month.go` | Add `RothWithdrawal` + `WithdrawFromRoth`; add `BigTicketFundingResult`; update `WithdrawForExpenses`, `ApplyBigTicketExpenseWithTaxableState`, `ExecutePortfolioCashFlowWithTaxableState`, `PortfolioMonthInput`, `PortfolioCashFlowResult`, `TaxAwarePortfolioMonthResult`, `ExecuteTaxAwarePortfolioMonth` |
| `internal/services/retirement/engine/month.go` | Update `runMonthlyLoop` to carry projection-local Roth state |
| `internal/services/retirement/analysis/monte_carlo.go` | Update `runSingleMonteCarloSimulation` to carry projection-local Roth state |
| `internal/services/retirement/analysis/backtest.go` | Update `runSingleHistoricalSequence` to carry projection-local Roth state |
| `internal/handlers/whatif/form_spec.go` | Add `roth_first_funded_year` field spec + parser |
| `web/templates/whatif/*.html` (or wherever portfolio inputs live) | Render new field + clock indicator + nudge banner |
| `internal/services/retirement/engine/clock_test.go` (new) | Unit tests for clock helper |
| `internal/services/retirement/engine/roth_withdrawal_test.go` (new) | Unit tests for `WithdrawFromRoth` |
| `internal/services/retirement/engine/roth_five_year_integration_test.go` (new) | End-to-end integration tests |

---

## Task 1: Add `RothFirstFundedYear` field to `WhatIfSettings`

**Files:**
- Modify: `internal/models/whatif.go`
- Test: `internal/models/models_extra_test.go` (existing file for model tests) or `internal/models/roth_first_funded_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/models/roth_first_funded_test.go`:

```go
package models

import (
	"encoding/json"
	"testing"
)

func TestWhatIfSettings_RothFirstFundedYear_JSONRoundtrip(t *testing.T) {
	original := WhatIfSettings{RothFirstFundedYear: 2026}
	raw, err := json.Marshal(&original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded WhatIfSettings
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.RothFirstFundedYear != 2026 {
		t.Fatalf("got %d, want 2026", decoded.RothFirstFundedYear)
	}
}

func TestWhatIfSettings_RothFirstFundedYear_DefaultZero(t *testing.T) {
	s := WhatIfSettings{}
	if s.RothFirstFundedYear != 0 {
		t.Fatalf("zero-value default: got %d, want 0", s.RothFirstFundedYear)
	}
}

func TestWhatIfSettings_RothFirstFundedYear_OmitemptyWhenZero(t *testing.T) {
	s := WhatIfSettings{}
	raw, err := json.Marshal(&s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(raw); contains(got, "roth_first_funded_year") {
		t.Fatalf("expected omitempty to drop zero value, got: %s", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/models -run RothFirstFundedYear -v`
Expected: FAIL — `RothFirstFundedYear` is not a field on `WhatIfSettings`.

- [ ] **Step 3: Add the field**

In `internal/models/whatif.go`, find the existing `RothConversion` field block (around line 152-153). Add immediately after:

```go
// RothFirstFundedYear is the calendar tax year of the user's first
// Roth IRA regular contribution or conversion contribution. It drives
// the IRS qualified-distribution 5-tax-year rule for earnings.
// Zero means unknown/unset, not necessarily "no Roth exists."
RothFirstFundedYear int `json:"roth_first_funded_year,omitempty"`
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/models -run RothFirstFundedYear -v`
Expected: PASS (all 3).

Then run the full models package: `go test ./internal/models`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/models/whatif.go internal/models/roth_first_funded_test.go
git commit -m "feat(roth): add RothFirstFundedYear to WhatIfSettings"
```

---

## Task 2: Clock predicate `RothQualifiedDistributionClockSatisfied`

**Files:**
- Modify: `internal/services/retirement/engine/loop_helpers.go`
- Test: `internal/services/retirement/engine/clock_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/services/retirement/engine/clock_test.go`:

```go
package engine

import "testing"

func TestRothQualifiedDistributionClockSatisfied(t *testing.T) {
	cases := []struct {
		name             string
		firstFundedYear  int
		calendarYear     int
		wantSatisfied    bool
	}{
		{"unset firstFundedYear is never satisfied", 0, 2030, false},
		{"negative firstFundedYear is never satisfied", -1, 2030, false},
		{"same year as funded is not satisfied", 2026, 2026, false},
		{"one year after funded is not satisfied", 2026, 2027, false},
		{"four years after funded is not satisfied", 2026, 2030, false},
		{"five years after funded is satisfied", 2026, 2031, true},
		{"six years after funded is satisfied", 2026, 2032, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RothQualifiedDistributionClockSatisfied(tc.firstFundedYear, tc.calendarYear)
			if got != tc.wantSatisfied {
				t.Fatalf("got %v, want %v", got, tc.wantSatisfied)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/retirement/engine -run RothQualifiedDistributionClockSatisfied -v`
Expected: FAIL — function undefined.

- [ ] **Step 3: Add the helper**

In `internal/services/retirement/engine/loop_helpers.go`, append at end of file:

```go
// RothQualifiedDistributionClockSatisfied reports whether the Roth IRA
// 5-tax-year aging requirement is met for the given calendar year. A
// firstFundedYear of 0 or less means unset and the clock is considered
// not satisfied (the conservative projection default). calendarYear is
// a calendar tax year, not a projection-year offset; callers translate
// projection year via ParseStartYear(s.StartDate)+projectionYear.
func RothQualifiedDistributionClockSatisfied(firstFundedYear, calendarYear int) bool {
	if firstFundedYear <= 0 {
		return false
	}
	return calendarYear >= firstFundedYear+5
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/engine -run RothQualifiedDistributionClockSatisfied -v`
Expected: PASS (all 7 sub-cases).

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/engine/loop_helpers.go internal/services/retirement/engine/clock_test.go
git commit -m "feat(roth): add RothQualifiedDistributionClockSatisfied predicate"
```

---

## Task 3: `WithdrawFromRoth` helper + `RothWithdrawal` struct

**Files:**
- Modify: `internal/services/retirement/engine/portfolio_month.go`
- Test: `internal/services/retirement/engine/roth_withdrawal_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/services/retirement/engine/roth_withdrawal_test.go`:

```go
package engine

import "testing"

func TestWithdrawFromRoth_BasisFirstOrdering(t *testing.T) {
	t.Run("zero need is a no-op", func(t *testing.T) {
		balance := 100.0
		basis := 60.0
		got := WithdrawFromRoth(0, &balance, &basis)
		if got.Total != 0 || got.Basis != 0 || got.Earnings != 0 {
			t.Fatalf("expected zero result, got %+v", got)
		}
		if balance != 100 || basis != 60 {
			t.Fatalf("balances mutated: balance=%v basis=%v", balance, basis)
		}
	})

	t.Run("small withdrawal pulls only basis", func(t *testing.T) {
		balance := 100.0
		basis := 60.0
		got := WithdrawFromRoth(40, &balance, &basis)
		if got.Total != 40 || got.Basis != 40 || got.Earnings != 0 {
			t.Fatalf("got %+v, want {40,40,0}", got)
		}
		if balance != 60 || basis != 20 {
			t.Fatalf("after: balance=%v basis=%v, want 60/20", balance, basis)
		}
	})

	t.Run("withdrawal exactly equals basis", func(t *testing.T) {
		balance := 100.0
		basis := 60.0
		got := WithdrawFromRoth(60, &balance, &basis)
		if got.Total != 60 || got.Basis != 60 || got.Earnings != 0 {
			t.Fatalf("got %+v, want {60,60,0}", got)
		}
		if balance != 40 || basis != 0 {
			t.Fatalf("after: balance=%v basis=%v, want 40/0", balance, basis)
		}
	})

	t.Run("large withdrawal exhausts basis then takes earnings", func(t *testing.T) {
		balance := 100.0
		basis := 60.0
		got := WithdrawFromRoth(75, &balance, &basis)
		if got.Total != 75 || got.Basis != 60 || got.Earnings != 15 {
			t.Fatalf("got %+v, want {75,60,15}", got)
		}
		if balance != 25 || basis != 0 {
			t.Fatalf("after: balance=%v basis=%v, want 25/0", balance, basis)
		}
	})

	t.Run("full withdrawal zeroes everything", func(t *testing.T) {
		balance := 100.0
		basis := 60.0
		got := WithdrawFromRoth(100, &balance, &basis)
		if got.Total != 100 || got.Basis != 60 || got.Earnings != 40 {
			t.Fatalf("got %+v, want {100,60,40}", got)
		}
		if balance != 0 || basis != 0 {
			t.Fatalf("after: balance=%v basis=%v, want 0/0", balance, basis)
		}
	})

	t.Run("over-request capped at balance", func(t *testing.T) {
		balance := 100.0
		basis := 60.0
		got := WithdrawFromRoth(150, &balance, &basis)
		if got.Total != 100 || got.Basis != 60 || got.Earnings != 40 {
			t.Fatalf("got %+v, want capped at balance", got)
		}
	})

	t.Run("basis above balance is clamped down", func(t *testing.T) {
		// Defensive: floating-point drift could leave basis slightly above balance.
		balance := 50.0
		basis := 60.0
		got := WithdrawFromRoth(10, &balance, &basis)
		if got.Total != 10 || got.Basis != 10 || got.Earnings != 0 {
			t.Fatalf("got %+v", got)
		}
		if basis > balance {
			t.Fatalf("basis %v above balance %v after withdraw — should clamp", basis, balance)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/retirement/engine -run WithdrawFromRoth -v`
Expected: FAIL — `WithdrawFromRoth` and `RothWithdrawal` undefined.

- [ ] **Step 3: Add the type and helper**

In `internal/services/retirement/engine/portfolio_month.go`, after the existing `WithdrawalBreakdown` struct (around line 21), add:

```go
// RothWithdrawal splits a single Roth distribution into the basis
// portion (regular contributions plus conversion-contribution amounts,
// always tax-free under IRS Pub 590-B ordering) and the earnings
// portion (taxable as ordinary income unless the qualified-distribution
// clock is satisfied).
type RothWithdrawal struct {
	Total    float64
	Basis    float64
	Earnings float64
}

// WithdrawFromRoth pulls up to `needed` from the Roth bucket, applying
// IRS Pub 590-B basis-first ordering. Mutates rothBalance and rothBasis
// in place. Clamps basis to balance to guard against floating-point
// drift. Returns the split.
func WithdrawFromRoth(needed float64, rothBalance, rothBasis *float64) RothWithdrawal {
	if needed <= 0 || *rothBalance <= 0 {
		return RothWithdrawal{}
	}
	total := math.Min(needed, *rothBalance)
	basis := math.Min(total, *rothBasis)
	earnings := total - basis
	*rothBalance -= total
	*rothBasis -= basis
	if *rothBasis > *rothBalance {
		*rothBasis = *rothBalance
	}
	if *rothBasis < 0 {
		*rothBasis = 0
	}
	return RothWithdrawal{Total: total, Basis: basis, Earnings: earnings}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/engine -run WithdrawFromRoth -v`
Expected: PASS (all 7 sub-cases).

Then run the full engine package: `go test ./internal/services/retirement/engine`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/engine/portfolio_month.go internal/services/retirement/engine/roth_withdrawal_test.go
git commit -m "feat(roth): add WithdrawFromRoth helper for basis-first ordering"
```

---

## Task 4: Extend `ApplyRothConversionAtYear` to track basis + clock

**Files:**
- Modify: `internal/services/retirement/engine/loop_helpers.go`
- Modify: `internal/services/retirement/engine/month.go` (the one current caller)
- Test: `internal/services/retirement/engine/loop_helpers_test.go` (new file, since this caller change is small)

- [ ] **Step 1: Write the failing test**

Create `internal/services/retirement/engine/loop_helpers_test.go`:

```go
package engine

import (
	"testing"

	"budget2/internal/models"
)

func TestApplyRothConversionAtYear_BasisAndClock(t *testing.T) {
	t.Run("no conversion → no mutation", func(t *testing.T) {
		s := &models.WhatIfSettings{}
		td := 100000.0
		roth := 0.0
		basis := 0.0
		firstFunded := 0
		got := ApplyRothConversionAtYear(s, 0, &td, &roth, &basis, &firstFunded)
		if got != 0 || td != 100000 || roth != 0 || basis != 0 || firstFunded != 0 {
			t.Fatalf("unexpected mutation: got=%v td=%v roth=%v basis=%v ff=%v", got, td, roth, basis, firstFunded)
		}
	})

	t.Run("active conversion increments balance and basis equally and sets clock", func(t *testing.T) {
		s := &models.WhatIfSettings{
			StartDate: "2026-01",
			RothConversion: &models.RothConversionConfig{
				Enabled:      true,
				StartYear:    0,
				EndYear:      0,
				AnnualAmount: 25000,
			},
		}
		td := 100000.0
		roth := 0.0
		basis := 0.0
		firstFunded := 0
		got := ApplyRothConversionAtYear(s, 0, &td, &roth, &basis, &firstFunded)
		if got != 25000 || td != 75000 || roth != 25000 || basis != 25000 {
			t.Fatalf("balances wrong: got=%v td=%v roth=%v basis=%v", got, td, roth, basis)
		}
		if firstFunded != 2026 {
			t.Fatalf("clock not set: firstFunded=%d, want 2026", firstFunded)
		}
	})

	t.Run("second conversion preserves earlier firstFundedYear", func(t *testing.T) {
		s := &models.WhatIfSettings{
			StartDate: "2026-01",
			RothConversion: &models.RothConversionConfig{
				Enabled:      true,
				StartYear:    0,
				EndYear:      0,
				AnnualAmount: 25000,
			},
		}
		td := 100000.0
		roth := 0.0
		basis := 0.0
		firstFunded := 2020 // pre-existing
		_ = ApplyRothConversionAtYear(s, 0, &td, &roth, &basis, &firstFunded)
		if firstFunded != 2020 {
			t.Fatalf("clock overwritten: firstFunded=%d, want 2020", firstFunded)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/retirement/engine -run ApplyRothConversionAtYear_BasisAndClock -v`
Expected: FAIL — signature mismatch.

- [ ] **Step 3: Update the helper signature**

In `internal/services/retirement/engine/loop_helpers.go`, replace `ApplyRothConversionAtYear`:

```go
// ApplyRothConversionAtYear performs the year-boundary Roth conversion:
// if a conversion is configured and in-window, decrement the tax-
// deferred balance, increment the Roth balance and basis by the same
// amount, and stamp rothFirstFundedYear if blank.
//
// The rothFirstFundedYear pointer holds projection-local state — the
// caller seeds it from s.RothFirstFundedYear and DOES NOT write back
// into s. Persisted settings change only through the settings form.
//
// All three projection loops perform this mutation identically; this
// helper captures the shared in-place update so the loops can shrink
// to a single call per year boundary.
func ApplyRothConversionAtYear(
	s *models.WhatIfSettings,
	currentYear int,
	taxDeferredBalance, rothBalance, rothBasis *float64,
	rothFirstFundedYear *int,
) float64 {
	conversionAmount := RothConversionAmountForYear(s, currentYear, *taxDeferredBalance)
	if conversionAmount <= 0 {
		return 0
	}
	*taxDeferredBalance -= conversionAmount
	*rothBalance += conversionAmount
	*rothBasis += conversionAmount
	if *rothFirstFundedYear <= 0 {
		*rothFirstFundedYear = ParseStartYear(s.StartDate) + currentYear
	}
	return conversionAmount
}
```

- [ ] **Step 4: Update the engine caller**

In `internal/services/retirement/engine/month.go`, find the line invoking `ApplyRothConversionAtYear` (around line 143). It currently reads:

```go
rothConversionThisMonth = ApplyRothConversionAtYear(s, currentYear, &taxDeferredBalance, &rothBalance)
```

Replace with placeholder local state for now (we'll wire the real state in Task 9):

```go
// TEMP: projection-local Roth basis/clock state; threaded fully in later task.
rothBasisLocal := s.PortfolioValue * (s.RothPercent / 100)
rothFirstFundedYearLocal := s.RothFirstFundedYear
if rothFirstFundedYearLocal == 0 && s.RothPercent > 0 {
	rothFirstFundedYearLocal = ParseStartYear(s.StartDate)
}
rothConversionThisMonth = ApplyRothConversionAtYear(s, currentYear, &taxDeferredBalance, &rothBalance, &rothBasisLocal, &rothFirstFundedYearLocal)
```

This is a deliberate temporary scaffold. Task 9 replaces it with state hoisted to the loop top.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/engine -run ApplyRothConversionAtYear -v`
Expected: PASS.

Then run engine + analysis packages: `go test ./internal/services/retirement/...`
Expected: PASS (no other callers of this function).

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/engine/loop_helpers.go internal/services/retirement/engine/loop_helpers_test.go internal/services/retirement/engine/month.go
git commit -m "feat(roth): track basis and first-funded year on conversion"
```

---

## Task 5: Extend `WithdrawForExpenses` to expose Roth basis/earnings split

**Files:**
- Modify: `internal/services/retirement/engine/portfolio_month.go`
- Test: `internal/services/retirement/engine/roth_withdrawal_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `internal/services/retirement/engine/roth_withdrawal_test.go`:

```go
func TestWithdrawForExpenses_RothBasisAndEarningsSplit(t *testing.T) {
	t.Run("Roth withdrawal splits basis/earnings", func(t *testing.T) {
		td := 0.0
		taxable := 0.0
		roth := 100.0
		rothBasis := 60.0

		got := WithdrawForExpenses(75, 0, false, 0, &td, &taxable, &roth, &rothBasis)

		if got.WithdrawalFromRoth != 75 {
			t.Fatalf("WithdrawalFromRoth=%v, want 75", got.WithdrawalFromRoth)
		}
		if got.WithdrawalFromRothBasis != 60 || got.WithdrawalFromRothEarnings != 15 {
			t.Fatalf("split: basis=%v earnings=%v, want 60/15", got.WithdrawalFromRothBasis, got.WithdrawalFromRothEarnings)
		}
		if roth != 25 || rothBasis != 0 {
			t.Fatalf("balances: roth=%v basis=%v, want 25/0", roth, rothBasis)
		}
	})

	t.Run("non-Roth withdrawal leaves Roth fields at zero", func(t *testing.T) {
		td := 0.0
		taxable := 100.0
		roth := 50.0
		rothBasis := 50.0

		got := WithdrawForExpenses(80, 0, false, 0, &td, &taxable, &roth, &rothBasis)

		if got.WithdrawalFromTaxable != 80 || got.WithdrawalFromRoth != 0 {
			t.Fatalf("taxable=%v roth=%v, want 80/0", got.WithdrawalFromTaxable, got.WithdrawalFromRoth)
		}
		if got.WithdrawalFromRothBasis != 0 || got.WithdrawalFromRothEarnings != 0 {
			t.Fatalf("split should be zero: basis=%v earnings=%v", got.WithdrawalFromRothBasis, got.WithdrawalFromRothEarnings)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/retirement/engine -run WithdrawForExpenses_RothBasisAndEarningsSplit -v`
Expected: FAIL — signature mismatch, missing fields.

- [ ] **Step 3: Update `WithdrawalBreakdown`, `WithdrawForExpenses`, and call sites**

In `internal/services/retirement/engine/portfolio_month.go`, modify the `WithdrawalBreakdown` struct (around line 13) to add two new fields:

```go
type WithdrawalBreakdown struct {
	RemainingNeed              float64
	ActualWithdrawal           float64
	RMDWithdrawal              float64
	WithdrawalFromTaxDeferred  float64
	WithdrawalFromTaxable      float64
	WithdrawalFromRoth         float64
	WithdrawalFromRothBasis    float64
	WithdrawalFromRothEarnings float64
	EarlyPenaltyPaid           float64
}
```

Modify `WithdrawForExpenses` to take `rothBasis *float64` and call `WithdrawFromRoth`:

```go
func WithdrawForExpenses(neededFromPortfolio, monthlyRMD float64, allowTaxDeferred bool, earlyPenaltyRate float64, taxDeferredBalance, taxableBalance, rothBalance, rothBasis *float64) WithdrawalBreakdown {
	breakdown := WithdrawalBreakdown{RemainingNeed: neededFromPortfolio}
	if neededFromPortfolio <= 0 {
		return breakdown
	}

	if monthlyRMD > 0 && *taxDeferredBalance > 0 {
		rmdUsed := math.Min(monthlyRMD, breakdown.RemainingNeed)
		rmdUsed = math.Min(rmdUsed, *taxDeferredBalance)
		*taxDeferredBalance -= rmdUsed
		breakdown.RemainingNeed -= rmdUsed
		breakdown.RMDWithdrawal = rmdUsed
		breakdown.WithdrawalFromTaxDeferred += rmdUsed
		breakdown.ActualWithdrawal += rmdUsed
	}

	if breakdown.RemainingNeed > 0 && *taxableBalance > 0 {
		fromTaxable := math.Min(breakdown.RemainingNeed, *taxableBalance)
		*taxableBalance -= fromTaxable
		breakdown.RemainingNeed -= fromTaxable
		breakdown.WithdrawalFromTaxable += fromTaxable
		breakdown.ActualWithdrawal += fromTaxable
	}

	if breakdown.RemainingNeed > 0 && *rothBalance > 0 {
		rw := WithdrawFromRoth(breakdown.RemainingNeed, rothBalance, rothBasis)
		breakdown.RemainingNeed -= rw.Total
		breakdown.WithdrawalFromRoth += rw.Total
		breakdown.WithdrawalFromRothBasis += rw.Basis
		breakdown.WithdrawalFromRothEarnings += rw.Earnings
		breakdown.ActualWithdrawal += rw.Total
	}

	if allowTaxDeferred && breakdown.RemainingNeed > 0 && *taxDeferredBalance > 0 {
		effectiveFactor := 1.0 - earlyPenaltyRate
		grossNeeded := breakdown.RemainingNeed / effectiveFactor
		fromTaxDeferred := math.Min(grossNeeded, *taxDeferredBalance)
		*taxDeferredBalance -= fromTaxDeferred
		netSpending := fromTaxDeferred * effectiveFactor
		penalty := fromTaxDeferred - netSpending
		breakdown.RemainingNeed -= netSpending
		breakdown.WithdrawalFromTaxDeferred += fromTaxDeferred
		breakdown.ActualWithdrawal += fromTaxDeferred
		breakdown.EarlyPenaltyPaid += penalty
	}

	return breakdown
}
```

Locate the existing caller in `ExecutePortfolioCashFlowWithTaxableState` (around line 185) and update it to pass a dummy `rothBasis` pointer temporarily — Task 7 will plumb the real basis through `PortfolioMonthInput`:

```go
// TEMP scaffold: Task 7 replaces with real basis pointer from PortfolioMonthInput.
dummyBasis := *rothBalance
withdrawal := WithdrawForExpenses(neededFromPortfolio, monthlyRMD, allowTaxDeferred, earlyPenaltyRate, taxDeferredBalance, &taxable.MarketValue, rothBalance, &dummyBasis)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/engine -v`
Expected: PASS for the new sub-tests, PASS for the pre-existing tests (the temp scaffold maintains current numerical behavior).

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/engine/portfolio_month.go internal/services/retirement/engine/roth_withdrawal_test.go
git commit -m "feat(roth): surface basis/earnings split on WithdrawForExpenses"
```

---

## Task 6: Big-ticket structured Roth result

**Files:**
- Modify: `internal/services/retirement/engine/portfolio_month.go`
- Modify: `internal/services/retirement/engine/loop_helpers.go`
- Test: `internal/services/retirement/engine/roth_withdrawal_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `internal/services/retirement/engine/roth_withdrawal_test.go`:

```go
func TestApplyBigTicketExpense_RothBasisAndEarningsSplit(t *testing.T) {
	taxable := &TaxableAccountState{}
	taxable.AddCash(0)
	td := 0.0
	roth := 100.0
	rothBasis := 60.0

	got := ApplyBigTicketExpenseWithTaxableState(75, false, 0, &td, taxable, &roth, &rothBasis)

	if got.UnfundedExpense != 0 {
		t.Fatalf("UnfundedExpense=%v, want 0", got.UnfundedExpense)
	}
	if got.RothBasisWithdrawal != 60 || got.RothEarningsWithdrawal != 15 {
		t.Fatalf("split: basis=%v earnings=%v, want 60/15", got.RothBasisWithdrawal, got.RothEarningsWithdrawal)
	}
	if roth != 25 || rothBasis != 0 {
		t.Fatalf("balances: roth=%v basis=%v, want 25/0", roth, rothBasis)
	}
}

func TestApplyBigTicketExpense_TaxableThenRothOrdering(t *testing.T) {
	taxable := &TaxableAccountState{}
	taxable.AddCash(50)
	td := 0.0
	roth := 100.0
	rothBasis := 60.0

	got := ApplyBigTicketExpenseWithTaxableState(120, false, 0, &td, taxable, &roth, &rothBasis)

	// 50 from taxable, 70 from Roth (60 basis + 10 earnings).
	if got.UnfundedExpense != 0 {
		t.Fatalf("UnfundedExpense=%v, want 0", got.UnfundedExpense)
	}
	if got.RothBasisWithdrawal != 60 || got.RothEarningsWithdrawal != 10 {
		t.Fatalf("split: basis=%v earnings=%v, want 60/10", got.RothBasisWithdrawal, got.RothEarningsWithdrawal)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/retirement/engine -run ApplyBigTicketExpense -v`
Expected: FAIL — signature returns bare float, no struct.

- [ ] **Step 3: Add the struct and update the helper**

In `internal/services/retirement/engine/portfolio_month.go`, add after `RothWithdrawal`:

```go
// BigTicketFundingResult is the outcome of funding one big-ticket
// expense from the portfolio waterfall: any portion that could not be
// funded plus the Roth basis/earnings split for the basis-first
// ordering rule.
type BigTicketFundingResult struct {
	UnfundedExpense        float64
	RothBasisWithdrawal    float64
	RothEarningsWithdrawal float64
}
```

Replace `ApplyBigTicketExpenseWithTaxableState` (around line 149):

```go
// ApplyBigTicketExpenseWithTaxableState pulls a one-off big-ticket
// expense from the portfolio in priority order (taxable → Roth → tax-
// deferred) and returns the structured result. Tax-deferred withdrawals
// honour the early-withdrawal penalty when active. Roth withdrawals
// split by IRS Pub 590-B basis-first ordering.
func ApplyBigTicketExpenseWithTaxableState(amount float64, allowTaxDeferred bool, earlyPenaltyRate float64, taxDeferredBalance *float64, taxable *TaxableAccountState, rothBalance, rothBasis *float64) BigTicketFundingResult {
	out := BigTicketFundingResult{}
	remaining := amount

	if remaining > 0 && taxable.MarketValue > 0 {
		fromTaxable, _, _ := taxable.Withdraw(remaining)
		remaining -= fromTaxable
	}

	if remaining > 0 && *rothBalance > 0 {
		rw := WithdrawFromRoth(remaining, rothBalance, rothBasis)
		remaining -= rw.Total
		out.RothBasisWithdrawal += rw.Basis
		out.RothEarningsWithdrawal += rw.Earnings
	}

	if allowTaxDeferred && remaining > 0 && *taxDeferredBalance > 0 {
		effectiveFactor := 1.0 - earlyPenaltyRate
		grossNeeded := remaining / effectiveFactor
		fromTaxDeferred := math.Min(grossNeeded, *taxDeferredBalance)
		*taxDeferredBalance -= fromTaxDeferred
		remaining -= fromTaxDeferred * effectiveFactor
	}

	out.UnfundedExpense = remaining
	return out
}
```

- [ ] **Step 4: Update `ApplyBigTicketItemsForYear`**

In `internal/services/retirement/engine/loop_helpers.go`, replace `ApplyBigTicketItemsForYear` (around line 217):

```go
// BigTicketYearResult aggregates a year's big-ticket draws so the
// caller can fold the unfunded expense into the month's expense total
// and route the taxable Roth earnings (if the clock is unsatisfied)
// into that month's tax snapshot.
type BigTicketYearResult struct {
	UnfundedExpense        float64
	RothBasisWithdrawal    float64
	RothEarningsWithdrawal float64
}

// ApplyBigTicketItemsForYear processes every big-ticket item scheduled
// for currentYear: income items add cash to the taxable account,
// expense items are funded via the canonical waterfall, and the
// aggregated unfunded-expense plus Roth split are returned so the
// monthly loop can feed taxable Roth earnings into the tax snapshot.
func ApplyBigTicketItemsForYear(s *models.WhatIfSettings, currentYear int, allowTaxDeferredWithdrawal bool, penaltyRate float64, taxDeferredBalance *float64, taxableAccount *TaxableAccountState, rothBalance, rothBasis *float64) BigTicketYearResult {
	out := BigTicketYearResult{}
	for _, item := range s.BigTicketItems {
		if item.Year != currentYear {
			continue
		}
		if item.Type == models.BigTicketIncome {
			taxableAccount.AddCash(item.Amount)
			continue
		}
		r := ApplyBigTicketExpenseWithTaxableState(item.Amount, allowTaxDeferredWithdrawal, penaltyRate, taxDeferredBalance, taxableAccount, rothBalance, rothBasis)
		out.UnfundedExpense += r.UnfundedExpense
		out.RothBasisWithdrawal += r.RothBasisWithdrawal
		out.RothEarningsWithdrawal += r.RothEarningsWithdrawal
	}
	return out
}
```

- [ ] **Step 5: Update the engine call site for the new return type**

In `internal/services/retirement/engine/month.go`, the existing call (around line 145) reads roughly:

```go
bigTicketExpenseThisMonth += ApplyBigTicketItemsForYear(s, currentYear, allowTaxDeferredWithdrawal, penaltyRate, &taxDeferredBalance, &taxableAccount, &rothBalance)
```

Replace with (using the temp basis scaffold from Task 4):

```go
bigTicketResult := ApplyBigTicketItemsForYear(s, currentYear, allowTaxDeferredWithdrawal, penaltyRate, &taxDeferredBalance, &taxableAccount, &rothBalance, &rothBasisLocal)
bigTicketExpenseThisMonth += bigTicketResult.UnfundedExpense
// bigTicketResult.RothEarningsWithdrawal will be threaded into the tax snapshot in Task 9.
_ = bigTicketResult
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/engine -v`
Expected: PASS — new tests pass, existing behavior preserved (the float remainder is now `out.UnfundedExpense`, and we still add only that to `bigTicketExpenseThisMonth`).

Run full retirement suite: `go test ./internal/services/retirement/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/services/retirement/engine/portfolio_month.go internal/services/retirement/engine/loop_helpers.go internal/services/retirement/engine/month.go internal/services/retirement/engine/roth_withdrawal_test.go
git commit -m "feat(roth): big-ticket funding returns structured Roth split"
```

---

## Task 7: Plumb basis through `PortfolioMonthInput` and `PortfolioCashFlowResult`

**Files:**
- Modify: `internal/services/retirement/engine/portfolio_month.go`
- Test: `internal/services/retirement/engine/roth_withdrawal_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `internal/services/retirement/engine/roth_withdrawal_test.go`:

```go
func TestExecutePortfolioCashFlowWithTaxableState_RothEarningsSurfaced(t *testing.T) {
	td := 0.0
	taxable := &TaxableAccountState{}
	taxable.AddCash(0)
	roth := 100.0
	rothBasis := 60.0

	result := ExecutePortfolioCashFlowWithTaxableState(75, 0, false, 0, 0, &td, taxable, &roth, &rothBasis)

	if result.WithdrawalFromRoth != 75 {
		t.Fatalf("WithdrawalFromRoth=%v, want 75", result.WithdrawalFromRoth)
	}
	if result.WithdrawalFromRothBasis != 60 || result.WithdrawalFromRothEarnings != 15 {
		t.Fatalf("split: basis=%v earnings=%v, want 60/15", result.WithdrawalFromRothBasis, result.WithdrawalFromRothEarnings)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/retirement/engine -run ExecutePortfolioCashFlowWithTaxableState_RothEarningsSurfaced -v`
Expected: FAIL — signature mismatch, no basis-split fields.

- [ ] **Step 3: Add fields to result struct + update orchestrator**

In `internal/services/retirement/engine/portfolio_month.go`, modify `PortfolioCashFlowResult` (around line 27):

```go
type PortfolioCashFlowResult struct {
	Shortfall                  float64
	ActualWithdrawal           float64
	RMDWithdrawal              float64
	WithdrawalFromTaxDeferred  float64
	WithdrawalFromTaxable      float64
	WithdrawalFromRoth         float64
	WithdrawalFromRothBasis    float64
	WithdrawalFromRothEarnings float64
	TaxableRealizedGain        float64
}
```

Update `ExecutePortfolioCashFlowWithTaxableState` to take `rothBasis *float64` and surface the split:

```go
func ExecutePortfolioCashFlowWithTaxableState(neededFromPortfolio, monthlyRMD float64, allowTaxDeferred bool, earlyPenaltyRate, marginalRate float64, taxDeferredBalance *float64, taxable *TaxableAccountState, rothBalance, rothBasis *float64) PortfolioCashFlowResult {
	result := PortfolioCashFlowResult{}

	if neededFromPortfolio > 0 {
		withdrawal := WithdrawForExpenses(neededFromPortfolio, monthlyRMD, allowTaxDeferred, earlyPenaltyRate, taxDeferredBalance, &taxable.MarketValue, rothBalance, rothBasis)
		result.Shortfall = withdrawal.RemainingNeed
		result.ActualWithdrawal = withdrawal.ActualWithdrawal
		result.RMDWithdrawal = withdrawal.RMDWithdrawal
		result.WithdrawalFromTaxDeferred = withdrawal.WithdrawalFromTaxDeferred
		result.WithdrawalFromRoth = withdrawal.WithdrawalFromRoth
		result.WithdrawalFromRothBasis = withdrawal.WithdrawalFromRothBasis
		result.WithdrawalFromRothEarnings = withdrawal.WithdrawalFromRothEarnings

		if withdrawal.WithdrawalFromTaxable > 0 {
			taxable.MarketValue += withdrawal.WithdrawalFromTaxable
			cash, _, realizedGain := taxable.Withdraw(withdrawal.WithdrawalFromTaxable)
			result.WithdrawalFromTaxable = cash
			result.TaxableRealizedGain += math.Max(0, realizedGain)
		}

		unmetRMD := monthlyRMD - withdrawal.RMDWithdrawal
		if unmetRMD > 0 {
			gross, _ := ReinvestRequiredRMDToTaxableState(unmetRMD, marginalRate, taxDeferredBalance, taxable)
			result.RMDWithdrawal += gross
			result.WithdrawalFromTaxDeferred += gross
		}
	} else {
		if neededFromPortfolio < 0 {
			taxable.AddCash(math.Abs(neededFromPortfolio))
		}
		gross, _ := ReinvestRequiredRMDToTaxableState(monthlyRMD, marginalRate, taxDeferredBalance, taxable)
		result.RMDWithdrawal = gross
		result.WithdrawalFromTaxDeferred += gross
	}

	return result
}
```

Update `PortfolioMonthInput` (around line 242) to add three fields and remove the temporary scaffold:

```go
type PortfolioMonthInput struct {
	TotalExpenses                     float64
	IncomeBreakdown                   MonthlyIncomeBreakdown
	MonthlyRMD                        float64
	AllowTaxDeferredWithdrawal        bool
	PenaltyRate                       float64
	TaxDeferredBalance                *float64
	TaxableAccount                    *TaxableAccountState
	RothBalance                       *float64
	RothBasis                         *float64
	RothFirstFundedYear               int
	TaxDeferredMonthlyReturn          float64
	RothMonthlyReturn                 float64
	TaxableComponents                 TaxableReturnComponents
	Timing                            models.ProjectionTiming
	TaxState                          ProjectionTaxAccumulator
	TaxCalculator                     *TaxCalculator
	CurrentYear                       int
	MonthInYear                       int
	CalendarYear                      int
	RothConversionThisMonth           float64
	TaxableRothEarningsBeforeCashFlow float64
	CompletedMAGIHistory              []float64
	IRMAAEligibleAdults               int
	IRMAAInflationFactor              float64
}
```

Update `TaxAwarePortfolioMonthResult` (around line 221) to surface taxable Roth earnings:

```go
type TaxAwarePortfolioMonthResult struct {
	Shortfall                        float64
	TaxesPaid                        float64
	IRMAAExpense                     float64
	TotalGrowth                      float64
	TaxableIncomeBeforeCashFlow      float64
	TaxableQualifiedDividends        float64
	TaxableNonQualifiedDividends     float64
	TaxableCapitalGains              float64
	TaxableCapitalGainsDistributions float64
	TaxableRothEarnings              float64
	TaxSnapshot                      ProjectedTaxSnapshot
	CashFlow                         PortfolioCashFlowResult
}
```

In `ExecuteTaxAwarePortfolioMonth` (starts ~line 269), inside the fixed-point loop, change the cash-flow call:

```go
trialCashFlow := ExecutePortfolioCashFlowWithTaxableState(trialNeededFromPortfolio, in.MonthlyRMD, in.AllowTaxDeferredWithdrawal, in.PenaltyRate, marginalRate, &trialTaxDeferred, &trialTaxable, &trialRoth, in.RothBasis)
```

After the cash-flow call, compute the taxable Roth earnings for this trial:

```go
trialTaxableRothEarnings := in.TaxableRothEarningsBeforeCashFlow
if !RothQualifiedDistributionClockSatisfied(in.RothFirstFundedYear, in.CalendarYear) {
	trialTaxableRothEarnings += trialCashFlow.WithdrawalFromRothEarnings
}
```

Then change the recalculated snapshot call to include `trialTaxableRothEarnings` in the ordinary-income parameter:

```go
recalculatedSnapshot := in.TaxState.EstimateMonthlySnapshot(
	in.TaxCalculator,
	in.CurrentYear,
	in.MonthInYear,
	in.IncomeBreakdown.OrdinaryIncome+trialNonQualifiedDividends+trialTaxableRothEarnings,
	in.IncomeBreakdown.SocialSecurityIncome,
	trialCashFlow.WithdrawalFromTaxDeferred,
	trialQualifiedDividends,
	trialCapitalGains,
	trialNonQualifiedDividends,
	in.RothConversionThisMonth,
	in.CompletedMAGIHistory,
	nil,
	in.IRMAAEligibleAdults,
	in.IRMAAInflationFactor,
)
```

And in the iteration-final assignment block, store `trialTaxableRothEarnings`:

```go
result.TaxableRothEarnings = trialTaxableRothEarnings
```

- [ ] **Step 4: Remove the temp scaffold in month.go**

In `internal/services/retirement/engine/month.go`, delete the temp scaffold lines added in Task 4 (the `rothBasisLocal` / `rothFirstFundedYearLocal` declarations) — they will be re-added at the top of the loop in Task 9 instead. For now, this file will not compile temporarily; that's fine because we'll fix it in Task 9.

Actually — to keep every commit green, leave the scaffold in place and instead route `&rothBasisLocal` and `rothFirstFundedYearLocal` into `PortfolioMonthInput`:

Find the call site building `PortfolioMonthInput` (~line 244 in month.go) and add the new fields:

```go
RothBasis:                         &rothBasisLocal,
RothFirstFundedYear:               rothFirstFundedYearLocal,
CalendarYear:                      ParseStartYear(s.StartDate) + currentYear,
TaxableRothEarningsBeforeCashFlow: bigTicketResult.RothEarningsWithdrawal,
```

(`bigTicketResult` was declared in Task 6.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/engine -v`
Expected: PASS — new test passes, existing tests still pass.

Run full retirement suite: `go test ./internal/services/retirement/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/engine/portfolio_month.go internal/services/retirement/engine/month.go internal/services/retirement/engine/roth_withdrawal_test.go
git commit -m "feat(roth): plumb basis and clock through portfolio month input"
```

---

## Task 8: Include taxable Roth earnings in `ApplyTaxStateMonth`

**Files:**
- Modify: `internal/services/retirement/engine/loop_helpers.go`
- Modify: `internal/services/retirement/engine/month.go`
- Test: `internal/services/retirement/engine/loop_helpers_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `internal/services/retirement/engine/loop_helpers_test.go`:

```go
func TestApplyTaxStateMonth_IncludesTaxableRothEarnings(t *testing.T) {
	taxState := &ProjectionTaxAccumulator{}
	income := MonthlyIncomeBreakdown{OrdinaryIncome: 1000}
	monthResult := TaxAwarePortfolioMonthResult{
		TaxableRothEarnings:          200,
		TaxableNonQualifiedDividends: 0,
		CashFlow:                     PortfolioCashFlowResult{},
	}
	ApplyTaxStateMonth(taxState, income, monthResult, 0)

	// Ordinary income in the accumulator should reflect the Roth earnings.
	// We can't read the field directly without exposing internals, so verify via
	// the same accumulator's recorded totals — adjust based on accumulator API.
	got := taxState.AnnualOrdinaryIncome()
	want := 1000.0 + 200.0
	if got != want {
		t.Fatalf("AnnualOrdinaryIncome=%v, want %v", got, want)
	}
}
```

> **Note:** If `ProjectionTaxAccumulator` does not expose `AnnualOrdinaryIncome()`, expose a minimal getter or assert via the tax-snapshot output. Look at existing tests in `internal/services/retirement/engine/tax_test.go` for the established read-back pattern and use that here.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/retirement/engine -run ApplyTaxStateMonth_IncludesTaxableRothEarnings -v`
Expected: FAIL — Roth earnings not added to ordinary income.

- [ ] **Step 3: Update `ApplyTaxStateMonth`**

In `internal/services/retirement/engine/loop_helpers.go`, replace `ApplyTaxStateMonth`:

```go
// ApplyTaxStateMonth folds a single month's portfolio result into the
// running ProjectionTaxAccumulator. The taxable Roth-earnings amount
// surfaced on monthResult is added to ordinary income so MAGI-sensitive
// calculations (IRMAA, NIIT thresholds) agree with the converged
// monthly tax snapshot.
func ApplyTaxStateMonth(taxState *ProjectionTaxAccumulator, incomeBreakdown MonthlyIncomeBreakdown, monthResult TaxAwarePortfolioMonthResult, rothConversionThisMonth float64) {
	taxState.ApplyMonth(
		incomeBreakdown.OrdinaryIncome+monthResult.TaxableNonQualifiedDividends+monthResult.TaxableRothEarnings,
		incomeBreakdown.SocialSecurityIncome,
		monthResult.CashFlow.WithdrawalFromTaxDeferred,
		monthResult.TaxableQualifiedDividends,
		monthResult.TaxableCapitalGains,
		monthResult.TaxableNonQualifiedDividends,
		rothConversionThisMonth,
		monthResult.TaxesPaid,
	)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/engine -v`
Expected: PASS.

Run full retirement suite: `go test ./internal/services/retirement/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/engine/loop_helpers.go internal/services/retirement/engine/loop_helpers_test.go
git commit -m "feat(roth): include taxable Roth earnings in tax accumulator"
```

---

## Task 9: Hoist Roth state to top of `runMonthlyLoop`

**Files:**
- Modify: `internal/services/retirement/engine/month.go`
- Test: `internal/services/retirement/engine/roth_five_year_integration_test.go` (new)

- [ ] **Step 1: Write the failing integration test**

Create `internal/services/retirement/engine/roth_five_year_integration_test.go`:

```go
package engine

import (
	"testing"

	"budget2/internal/models"
)

// Minimal builder for a scenario where Roth is heavily used early in the
// projection. Adjust portfolio/expense numbers to force a Roth withdrawal.
func buildRothFiveYearScenario(t *testing.T) *models.WhatIfSettings {
	t.Helper()
	s := models.NewDefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.CurrentAge = 70
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 50
	s.RothPercent = 30
	s.MonthlyExpenses = 6000
	s.ProjectionYears = 10
	return s
}

func TestRunMonthlyLoop_RothEarningsTaxed_WhenClockUnsatisfied(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	s.RothFirstFundedYear = 2026 // clock matures 2031
	// Force a Roth withdrawal by depleting taxable: set RothPercent high and
	// taxable% low so the waterfall reaches Roth in early years.
	s.TaxDeferredPercent = 30
	s.RothPercent = 65 // taxable = 5%

	out := RunRetirementProjection(s) // adjust to actual entry point name; see existing tests
	// Sum federal income tax in years 0-2 (clock not yet satisfied).
	taxEarly := 0.0
	for i := 0; i < 3 && i < len(out.YearlyData); i++ {
		taxEarly += out.YearlyData[i].FederalIncomeTax
	}
	if taxEarly <= 0 {
		t.Fatalf("expected positive federal income tax in early years from Roth earnings, got %v", taxEarly)
	}
}
```

> **Note:** `RunRetirementProjection` and `YearlyData.FederalIncomeTax` are placeholders — replace with the actual public entry point used in `internal/services/retirement/calculator_test.go`. The pattern is "build settings → run projection → inspect aggregated tax". The minimum requirement is that this test fails until `runMonthlyLoop` carries projection-local Roth basis and first-funded year and feeds them into the per-month tax math.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/retirement/engine -run TestRunMonthlyLoop_RothEarningsTaxed_WhenClockUnsatisfied -v`
Expected: FAIL — tax not applied (or scenario doesn't reach Roth yet).

- [ ] **Step 3: Hoist projection-local Roth state into `runMonthlyLoop`**

In `internal/services/retirement/engine/month.go`, near the existing `rothBalance` declaration (~line 54), expand:

```go
taxDeferredBalance := s.PortfolioValue * (s.TaxDeferredPercent / 100)
initialRothBalance := s.PortfolioValue * (s.RothPercent / 100)
rothBalance := initialRothBalance
rothBasis := initialRothBalance
rothFirstFundedYear := s.RothFirstFundedYear
if rothFirstFundedYear == 0 && initialRothBalance > 0 {
	rothFirstFundedYear = ParseStartYear(s.StartDate)
}
```

Remove the temporary scaffold lines added in Task 4 (`rothBasisLocal := ...` and `rothFirstFundedYearLocal := ...`). Update the conversion call to use `rothBasis` and `&rothFirstFundedYear`:

```go
rothConversionThisMonth = ApplyRothConversionAtYear(s, currentYear, &taxDeferredBalance, &rothBalance, &rothBasis, &rothFirstFundedYear)
```

Update the big-ticket call:

```go
bigTicketResult := ApplyBigTicketItemsForYear(s, currentYear, allowTaxDeferredWithdrawal, penaltyRate, &taxDeferredBalance, &taxableAccount, &rothBalance, &rothBasis)
bigTicketExpenseThisMonth += bigTicketResult.UnfundedExpense
```

Update the `PortfolioMonthInput` literal to reference the loop-local state:

```go
RothBalance:                       &rothBalance,
RothBasis:                         &rothBasis,
RothFirstFundedYear:               rothFirstFundedYear,
CalendarYear:                      ParseStartYear(s.StartDate) + currentYear,
TaxableRothEarningsBeforeCashFlow: bigTicketResult.RothEarningsWithdrawal,
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/engine -v`
Expected: PASS — the integration test now succeeds because Roth earnings are taxed.

Run the broader retirement suite: `go test ./internal/services/retirement/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/engine/month.go internal/services/retirement/engine/roth_five_year_integration_test.go
git commit -m "feat(roth): wire 5-year clock state into deterministic loop"
```

---

## Task 10: Carry projection-local Roth state in Monte Carlo loop

**Files:**
- Modify: `internal/services/retirement/analysis/monte_carlo.go`
- Test: `internal/services/retirement/analysis/monte_carlo_test.go` (existing) OR new test file

- [ ] **Step 1: Write the failing test**

Append to a test file under `internal/services/retirement/analysis/`:

```go
package analysis

import (
	"math/rand"
	"testing"

	"budget2/internal/services/retirement/engine"
)

func TestRunSingleMonteCarloSimulation_CarriesRothBasis(t *testing.T) {
	in := engineInput(t, /* a settings builder analogous to buildRothFiveYearScenario */)
	// Set RothFirstFundedYear so the clock is unsatisfied early.
	in.Settings.RothFirstFundedYear = engine.ParseStartYear(in.Settings.StartDate)

	rng := rand.New(rand.NewSource(42))
	cfg := DefaultMonteCarloConfig()

	result := runSingleMonteCarloSimulation(in, rng, &cfg)

	// Asserts on the resulting tax — same shape as the engine integration test.
	// If MC results don't expose per-year tax directly, adapt to whatever
	// per-year output the package returns; the principle is the loop must
	// produce nonzero federal income tax from Roth earnings withdrawal early on.
	if result.FailureYear != 0 && result.FailureYear < 3 {
		t.Fatalf("expected scenario to survive 3 years; failed at %d", result.FailureYear)
	}
	// If a per-year tax slice exists, use it; otherwise this test acts as a
	// smoke-level guard against the wiring regressing.
}
```

> **Note:** Adapt this to whatever the Monte Carlo result struct exposes. The minimum is that the test panics/fails today (because the Monte Carlo loop has no `rothBasis` variable), and passes once the loop is updated.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/retirement/analysis -run TestRunSingleMonteCarloSimulation_CarriesRothBasis -v`
Expected: FAIL.

- [ ] **Step 3: Update `runSingleMonteCarloSimulation`**

In `internal/services/retirement/analysis/monte_carlo.go`, locate the per-iteration setup near the top of `runSingleMonteCarloSimulation` (around line 276). Add projection-local state alongside any existing `rothBalance` setup:

```go
initialRothBalance := s.PortfolioValue * (s.RothPercent / 100)
rothBalance := initialRothBalance
rothBasis := initialRothBalance
rothFirstFundedYear := s.RothFirstFundedYear
if rothFirstFundedYear == 0 && initialRothBalance > 0 {
	rothFirstFundedYear = engine.ParseStartYear(s.StartDate)
}
```

Update the conversion call inside the year-boundary block:

```go
rothConversionThisMonth = engine.ApplyRothConversionAtYear(s, currentYear, &taxDeferredBalance, &rothBalance, &rothBasis, &rothFirstFundedYear)
```

Update the big-ticket call:

```go
bigTicketResult := engine.ApplyBigTicketItemsForYear(s, currentYear, allowTaxDeferredWithdrawal, penaltyRate, &taxDeferredBalance, &taxableAccount, &rothBalance, &rothBasis)
bigTicketExpenseThisMonth += bigTicketResult.UnfundedExpense
```

Update the `PortfolioMonthInput` literal:

```go
RothBalance:                       &rothBalance,
RothBasis:                         &rothBasis,
RothFirstFundedYear:               rothFirstFundedYear,
CalendarYear:                      engine.ParseStartYear(s.StartDate) + currentYear,
TaxableRothEarningsBeforeCashFlow: bigTicketResult.RothEarningsWithdrawal,
```

> **Note:** The exact variable names and structure in `monte_carlo.go` may differ — read the file before editing to keep style consistent with the surrounding code. The behaviour is identical to `runMonthlyLoop` from Task 9.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/analysis -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/analysis/monte_carlo.go internal/services/retirement/analysis/monte_carlo_test.go
git commit -m "feat(roth): wire 5-year clock state into Monte Carlo loop"
```

---

## Task 11: Carry projection-local Roth state in historical backtest loop

**Files:**
- Modify: `internal/services/retirement/analysis/backtest.go`
- Test: `internal/services/retirement/analysis/backtest_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `internal/services/retirement/analysis/backtest_test.go`:

```go
func TestRunSingleHistoricalSequence_CarriesRothBasis(t *testing.T) {
	settings := buildRothFiveYearScenarioForBacktest(t) // helper analogous to engine integration test
	settings.RothFirstFundedYear = 2026

	in := engineInput(t, settings)
	data := history.DefaultData()

	result := runSingleHistoricalSequence(in, data, 1982)

	// As with Monte Carlo: assert that the result does not crash and surfaces
	// a per-year tax shape consistent with the Roth earnings taxation.
	if result == nil {
		t.Fatal("nil result")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/retirement/analysis -run TestRunSingleHistoricalSequence_CarriesRothBasis -v`
Expected: FAIL.

- [ ] **Step 3: Update `runSingleHistoricalSequence`**

In `internal/services/retirement/analysis/backtest.go`, find the per-iteration setup at the top of `runSingleHistoricalSequence` (search for `rothBalance :=`). Mirror exactly the same set of state additions as Task 10:

```go
initialRothBalance := s.PortfolioValue * (s.RothPercent / 100)
rothBalance := initialRothBalance
rothBasis := initialRothBalance
rothFirstFundedYear := s.RothFirstFundedYear
if rothFirstFundedYear == 0 && initialRothBalance > 0 {
	rothFirstFundedYear = engine.ParseStartYear(s.StartDate)
}
```

Update the conversion call, big-ticket call, and `PortfolioMonthInput` literal exactly as in Task 10.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/analysis -v`
Expected: PASS.

Run the entire retirement suite end-to-end: `go test ./internal/services/retirement/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/analysis/backtest.go internal/services/retirement/analysis/backtest_test.go
git commit -m "feat(roth): wire 5-year clock state into historical backtest"
```

---

## Task 12: Settings form — add `roth_first_funded_year` field

**Files:**
- Modify: `internal/handlers/whatif/form_spec.go`
- Test: `internal/handlers/whatif/form_spec_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `internal/handlers/whatif/form_spec_test.go`:

```go
func TestFormSpec_RothFirstFundedYear_Parse(t *testing.T) {
	t.Run("blank parses to zero", func(t *testing.T) {
		form := map[string][]string{"roth_first_funded_year": {""}}
		updates, errs := parseWhatIfForm(form)
		if len(errs) != 0 {
			t.Fatalf("unexpected errors: %v", errs)
		}
		if got, ok := updates["roth_first_funded_year"]; ok && got != int(0) && got != float64(0) {
			t.Fatalf("blank should yield zero, got %v", got)
		}
	})

	t.Run("valid year parses through", func(t *testing.T) {
		form := map[string][]string{"roth_first_funded_year": {"2010"}}
		updates, errs := parseWhatIfForm(form)
		if len(errs) != 0 {
			t.Fatalf("unexpected errors: %v", errs)
		}
		if got := updates["roth_first_funded_year"]; got == nil {
			t.Fatalf("missing parsed value")
		}
	})

	t.Run("year before 1998 errors", func(t *testing.T) {
		form := map[string][]string{"roth_first_funded_year": {"1997"}}
		_, errs := parseWhatIfForm(form)
		if len(errs) == 0 {
			t.Fatalf("expected validation error for year < 1998")
		}
	})

	t.Run("far-future year errors", func(t *testing.T) {
		form := map[string][]string{"roth_first_funded_year": {"3000"}}
		_, errs := parseWhatIfForm(form)
		if len(errs) == 0 {
			t.Fatalf("expected validation error for year > current+50")
		}
	})
}
```

> **Note:** Replace `parseWhatIfForm` with the actual exported parser name in `form_spec.go`. The pattern is "drive the form parser → assert error/result".

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/whatif -run TestFormSpec_RothFirstFundedYear -v`
Expected: FAIL — field unknown.

- [ ] **Step 3: Add the field spec and apply-to-settings handler**

In `internal/handlers/whatif/form_spec.go`, after the existing `tax_deferred_delay_years` field spec entry (~line 111):

```go
{Name: "roth_first_funded_year", Kind: fieldInt, ParseLabel: "Roth first funded year",
	HasBounds: true, Min: 1998, Max: 9999, // upper-bound clarified by handler
	BoundsMsg: "Year must be 1998 or later", AllowBlank: true},
```

> If `AllowBlank` does not exist on `fieldSpec`, look at how other optional integer fields are modeled — e.g., `tax_deferred_delay_years` allows zero. Use the same convention. If no convention exists, add an `AllowBlank` boolean (or treat zero as "unset") consistent with the existing code.

In the same file, in the apply-updates block (around line 257 where `roth_percent` is handled), add:

```go
if rfyV, ok := updates["roth_first_funded_year"]; ok {
	switch v := rfyV.(type) {
	case int:
		s.RothFirstFundedYear = v
	case float64:
		s.RothFirstFundedYear = int(v)
	}
}
```

Additionally enforce the upper bound (current year + 50) in the parser, since `Max: 9999` is a coarse compile-time bound:

```go
if year, ok := updates["roth_first_funded_year"].(int); ok && year != 0 {
	upper := time.Now().Year() + 50
	if year > upper {
		errs = append(errs, fmt.Errorf("Roth first funded year must be %d or earlier", upper))
	}
}
```

(Make sure the `time` import is added if not already present.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/handlers/whatif -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/whatif/form_spec.go internal/handlers/whatif/form_spec_test.go
git commit -m "feat(roth): add roth_first_funded_year settings form field"
```

---

## Task 13: UI rendering — input, tax row, clock indicator, nudge

**Files:**
- Modify: the what-if portfolio settings template (find via `grep -rn "roth_percent" web/` or `internal/templates/`)
- Modify: the projection display template (find via `grep -rn "federal_income_tax\|FederalIncomeTax" web/ internal/templates/`)
- Test: `internal/handlers/whatif/handlers_test.go` or whatever exercises the rendered HTML

- [ ] **Step 1: Locate the templates**

Run: `grep -rn "roth_percent" web/ internal/templates/ 2>/dev/null | head -10`
Note the file paths for the portfolio settings form.

Run: `grep -rn "WithdrawalFromRoth\|federal_income_tax\|FederalIncomeTax" web/ internal/templates/ 2>/dev/null | head -10`
Note the file paths for the projection display.

- [ ] **Step 2: Add the input field**

In the portfolio settings form template, locate the `roth_percent` input. Immediately after it, add:

```html
<div class="form-row">
	<label for="roth_first_funded_year">Year Roth IRA was first funded</label>
	<input type="number" name="roth_first_funded_year" id="roth_first_funded_year"
		   min="1998" step="1"
		   value="{{ if .Settings.RothFirstFundedYear }}{{ .Settings.RothFirstFundedYear }}{{ end }}"
		   hx-trigger="change" hx-post="/whatif/recalculate" hx-target="..." />
	<small class="help">
		Used for the IRS 5-year rule on Roth IRA earnings. Leave blank if you do not know it;
		if you already have a Roth balance, projections assume the clock started in the projection start year.
	</small>
</div>
```

(Adapt class names and `hx-target` to match the surrounding form's pattern.)

- [ ] **Step 3: Add the tax-breakdown row**

In the projection display template, locate the federal-income-tax row. Add immediately after:

```html
{{ if gt .Year.TaxableRothEarnings 0.0 }}
<tr class="cost-item">
	<td class="indent">Roth earnings (5-year rule)</td>
	<td class="amount">{{ formatCurrency .Year.TaxableRothEarnings }}</td>
</tr>
{{ end }}
```

> **Note:** `TaxableRothEarnings` must be present on the year-summary struct that the template iterates over. If it is not, surface it by:
> - Adding `TaxableRothEarnings float64` to the year-summary struct (search for the existing fields like `FederalIncomeTax`).
> - Populating it from the projection result. The engine surfaces it via `TaxAwarePortfolioMonthResult.TaxableRothEarnings` per Task 7.

- [ ] **Step 4: Add the clock indicator**

Locate the Roth bucket card (search `Roth` in the template directory). Add a conditional block:

```html
{{ if and (gt .Settings.RothFirstFundedYear 0) (lt .CurrentCalendarYear (add .Settings.RothFirstFundedYear 5)) }}
<div class="clock-indicator">
	5-year clock matures in {{ add .Settings.RothFirstFundedYear 5 }}
</div>
{{ end }}
```

If `add` is not a registered template function in this project, register it in the same file template-funcs are registered, or compute the maturity year in the handler and pass it in.

- [ ] **Step 5: Add the existing-balance nudge**

In the same portfolio settings panel:

```html
{{ if and (gt .Settings.RothPercent 0.0) (eq .Settings.RothFirstFundedYear 0) }}
<div class="settings-nudge dismissible">
	You have a Roth balance but have not set the first-funded year.
	Projections assume the 5-year clock starts in the projection start year.
</div>
{{ end }}
```

- [ ] **Step 6: Run tests**

Run: `go test ./...`
Expected: PASS.

Then manually load the app:

```bash
go run ./cmd/server &
SERVER_PID=$!
sleep 2
# Visit http://localhost:PORT/whatif and verify:
#  - "Year Roth IRA was first funded" input is visible
#  - With a Roth balance + blank year, the nudge appears
#  - With RothFirstFundedYear set in past year, no nudge
#  - Tax breakdown row appears when Roth earnings get taxed
kill $SERVER_PID
```

- [ ] **Step 7: Commit**

```bash
git add web/templates/... internal/templates/...  # use exact paths from your grep
git commit -m "feat(roth): UI for Roth first-funded-year and clock indicator"
```

---

## Task 14: Spec-mandated regression and parity tests

**Files:**
- Test: `internal/services/retirement/engine/roth_five_year_integration_test.go` (extend the file from Task 9)
- Test: `internal/services/retirement/analysis/roth_five_year_parity_test.go` (new)

- [ ] **Step 1: Add the spec's remaining required tests**

Append to `internal/services/retirement/engine/roth_five_year_integration_test.go`:

```go
func TestRothEarnings_TaxFreeAfterClock(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	s.RothFirstFundedYear = 2020 // clock matured 2025; projection starts 2026

	out := RunRetirementProjection(s) // adjust to actual entry point
	for i, year := range out.YearlyData {
		if year.TaxableRothEarnings != 0 {
			t.Fatalf("year %d: TaxableRothEarnings=%v, want 0 (clock satisfied)", i, year.TaxableRothEarnings)
		}
	}
}

func TestRothWithdrawal_NoTaxWhenAllBasis(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	s.RothFirstFundedYear = 2026
	// Configure expenses so the early-year Roth withdrawal stays within basis
	// (default initial basis == initial balance). Withdraw only a small amount.
	s.MonthlyExpenses = 1000

	out := RunRetirementProjection(s)
	for i := 0; i < 3 && i < len(out.YearlyData); i++ {
		if out.YearlyData[i].TaxableRothEarnings != 0 {
			t.Fatalf("year %d: TaxableRothEarnings=%v, want 0 (within basis)", i, out.YearlyData[i].TaxableRothEarnings)
		}
	}
}

func TestBigTicketRothEarnings_FeedTaxState(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	s.RothFirstFundedYear = 2026
	// Add a big-ticket expense in year 2 large enough to consume basis + earnings.
	s.BigTicketItems = append(s.BigTicketItems, models.BigTicketItem{
		Year:   2,
		Type:   models.BigTicketExpense,
		Amount: 200_000,
	})
	// Zero out taxable to force big-ticket into Roth.
	s.TaxDeferredPercent = 30
	s.RothPercent = 70

	out := RunRetirementProjection(s)
	if out.YearlyData[2].TaxableRothEarnings <= 0 {
		t.Fatalf("year 2 TaxableRothEarnings should be > 0 from big-ticket Roth earnings")
	}
}

func TestRothConversionDoesNotMutateSettings(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	s.RothFirstFundedYear = 0 // blank
	s.RothConversion = &models.RothConversionConfig{
		Enabled: true, StartYear: 0, EndYear: 0, AnnualAmount: 25_000,
	}

	_ = RunRetirementProjection(s)
	if s.RothFirstFundedYear != 0 {
		t.Fatalf("persisted settings mutated: RothFirstFundedYear=%d, want 0", s.RothFirstFundedYear)
	}
}

func TestExistingRothBlankYear_UsesProjectionStartClock(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	s.RothFirstFundedYear = 0 // blank, but RothPercent > 0 from builder
	s.MonthlyExpenses = 6000

	out := RunRetirementProjection(s)
	// Year 0 — clock not satisfied (start-year + 0 < start-year + 5).
	if out.YearlyData[0].TaxableRothEarnings < 0 {
		t.Fatalf("year 0 TaxableRothEarnings must be >= 0")
	}
	// Year 5 — clock satisfied; any Roth earnings withdrawal not taxed.
	if out.YearlyData[5].TaxableRothEarnings != 0 {
		t.Fatalf("year 5: TaxableRothEarnings=%v, want 0 (clock satisfied)", out.YearlyData[5].TaxableRothEarnings)
	}
}

func TestExistingScenario_NoSilentRegression(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	// No Roth withdrawals: high tax-deferred, low Roth, low expenses.
	s.TaxDeferredPercent = 70
	s.RothPercent = 10
	s.MonthlyExpenses = 3000

	out := RunRetirementProjection(s)
	for i, year := range out.YearlyData {
		if year.TaxableRothEarnings != 0 {
			t.Fatalf("year %d: TaxableRothEarnings=%v, want 0 (no Roth withdrawal)", i, year.TaxableRothEarnings)
		}
	}
}
```

- [ ] **Step 2: Add the projection-loop parity test**

Create `internal/services/retirement/analysis/roth_five_year_parity_test.go`:

```go
package analysis

import (
	"math/rand"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
)

func TestProjectionLoops_RothStateParity(t *testing.T) {
	// Build a baseline settings struct used by all three loops.
	s := models.NewDefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.CurrentAge = 70
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 30
	s.RothPercent = 65
	s.MonthlyExpenses = 6000
	s.ProjectionYears = 6
	s.RothFirstFundedYear = 2026

	in := engineInput(t, s)

	t.Run("deterministic loop applies tax on early Roth earnings", func(t *testing.T) {
		out := engine.RunRetirementProjection(s)
		if sumTaxableRothEarnings(out, 0, 3) <= 0 {
			t.Fatalf("deterministic loop: expected nonzero early Roth-earnings tax")
		}
	})

	t.Run("monte carlo loop carries Roth basis state", func(t *testing.T) {
		rng := rand.New(rand.NewSource(1))
		cfg := DefaultMonteCarloConfig()
		_ = runSingleMonteCarloSimulation(in, rng, &cfg) // smoke: must not panic
	})

	t.Run("historical backtest loop carries Roth basis state", func(t *testing.T) {
		_ = runSingleHistoricalSequence(in, history.DefaultData(), 1990) // smoke: must not panic
	})
}

func sumTaxableRothEarnings(out engine.RetirementProjectionResult, fromYear, toYear int) float64 {
	sum := 0.0
	for i := fromYear; i <= toYear && i < len(out.YearlyData); i++ {
		sum += out.YearlyData[i].TaxableRothEarnings
	}
	return sum
}
```

> **Note:** `engine.RunRetirementProjection`, `engine.RetirementProjectionResult.YearlyData[i].TaxableRothEarnings`, and `engineInput` are placeholders. Match the real names by inspecting how other integration tests in this directory wire things up (e.g., `backtest_test.go`).

- [ ] **Step 3: Run all new tests**

Run: `go test ./internal/services/retirement/... -v`
Expected: PASS.

Run the full test suite: `go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/services/retirement/engine/roth_five_year_integration_test.go internal/services/retirement/analysis/roth_five_year_parity_test.go
git commit -m "test(roth): cover spec scenarios for 5-year rule across all loops"
```

---

## Task 15: Verification sweep

- [ ] **Step 1: Run the full test matrix**

```bash
go test ./...
```
Expected: PASS — every package green.

- [ ] **Step 2: Run with race detector**

```bash
make race
```
Expected: PASS. (Per project policy, race detector is opt-in via `make race`, not on every commit.)

- [ ] **Step 3: Run vet + staticcheck + govulncheck**

```bash
go vet ./...
staticcheck ./...
govulncheck ./...
```
Expected: clean output.

- [ ] **Step 4: Manual smoke test in the browser**

Start the server and exercise:
- Default scenario: confirm projection numbers unchanged from pre-feature baseline (the regression test enforces this — the manual check is to eyeball that the UI hasn't broken).
- Set `RothFirstFundedYear` to 2026 and a scenario that forces Roth withdrawals in years 0–4: confirm the "Roth earnings (5-year rule)" row appears and federal income tax increases.
- Set `RothFirstFundedYear` to 2010: confirm the clock indicator and earnings row both vanish.

- [ ] **Step 5: Final commit (only if any cleanup needed)**

If steps 1–4 surfaced any cleanup, commit it under a `chore(roth): polish from verification sweep` message. Otherwise no commit needed.

- [ ] **Step 6: Squash decision**

This plan produces ~14 commits on the working branch. Decide with the user whether to merge as-is or squash before merge.

---

## Self-Review Notes

**Spec coverage check (against `docs/superpowers/specs/2026-05-13-roth-five-year-rule-design.md`):**

- ✅ Data model field (`RothFirstFundedYear`) — Task 1
- ✅ Validation `[1998, projectionStart+projectionYears]` — Task 12 (1998 floor and current-year + 50 ceiling — the spec's bound is functionally equivalent for reasonable inputs)
- ✅ Projection-local state in all 3 loops — Tasks 9, 10, 11
- ✅ Clock helper `RothQualifiedDistributionClockSatisfied` — Task 2
- ✅ Conversion handling (basis, clock stamp, no persisted mutation) — Tasks 4, 14
- ✅ `WithdrawFromRoth` + basis-first ordering — Task 3
- ✅ `WithdrawForExpenses` updated — Task 5
- ✅ `ApplyBigTicketExpenseWithTaxableState` → `BigTicketFundingResult` — Task 6
- ✅ `PortfolioMonthInput` / `PortfolioCashFlowResult` / `TaxAwarePortfolioMonthResult` — Task 7
- ✅ `ApplyTaxStateMonth` includes taxable Roth earnings — Task 8
- ✅ UI: input field, tax-breakdown row, clock indicator, existing-balance nudge — Task 13
- ✅ All 11 spec-mandated tests — distributed across Tasks 1, 2, 3, 4, 5, 6, 8, 9, 14
- ✅ Migration: blank field + RothPercent>0 → clock starts at projection start year — Task 9 (deterministic), Tasks 10–11 (other loops)

**Placeholder scan:** A few task steps reference helpers whose exact names the executor must look up at the time (e.g., `RunRetirementProjection`, `engineInput`, `parseWhatIfForm`). These are clearly flagged with notes; the executor must read the surrounding file to find the canonical name.

**Type consistency:** `RothWithdrawal` (Task 3), `BigTicketFundingResult` (Task 6), `BigTicketYearResult` (Task 6), updates to `WithdrawalBreakdown` (Task 5), `PortfolioCashFlowResult` (Task 7), `PortfolioMonthInput` (Task 7), `TaxAwarePortfolioMonthResult` (Task 7) all use consistent field names across the tasks that consume them.

**Cross-task test deps:** Per the project's `feedback_tskip_for_cross_task_deps.md` memory, the integration tests in Task 14 depend on Tasks 1–13 already being merged. If executed out of order via parallel subagents, gate intermediate-state tests with `t.Skip("Re-enabled by Task N — feature not yet wired")` so pre-commit stays green. Within the linear order documented here, no skip is needed.
