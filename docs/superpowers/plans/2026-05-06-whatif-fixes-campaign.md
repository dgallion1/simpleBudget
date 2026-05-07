# What-If Fix Campaign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) to implement this plan PR-by-PR. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Resolve the 10 user-visible findings from the what-if math audit (`docs/whatif-math-audit-2026-05-05.md`) — 7 formula bugs, 2 UI/doc clarity items, and 1 bundled config-gap fix — without introducing regressions.

**Architecture:** Linear commit history on `feat/whatif-fixes`. Each PR is a contiguous commit set: failing test(s) → fix → green. Each PR resolves one audit finding (or, for PR 10, a bundled trio of year-boundary issues). One subagent dispatch per PR with two-stage review (spec compliance + code quality).

**Tech Stack:** Go (services/retirement, models/whatif, handlers/whatif), HTML templates (web/templates/components/whatif). Read-only on web/static/js unless a UI fix requires it. Existing test framework (`testing` stdlib, `testify` not used).

**Spec:** `docs/superpowers/specs/2026-05-06-whatif-fixes-campaign-design.md` (commit `3124858`)

**Audit reference:** Each PR resolves a specific F-NNN finding. The audit doc has full evidence and recommended fix sketches for each.

---

## Conventions

These apply to every PR. Re-read this section if you switch agents mid-plan.

### Branch and commit hygiene

- All work on `feat/whatif-fixes` (current branch). No sub-branches.
- One commit per logical step (failing test, fix, etc.). Commits are *not* squashed before the user reviews — atomic history is the audit trail.
- Commit messages follow conventional commits: `fix(whatif): F-NNN <short description>` for fixes, `test(whatif): F-NNN <description>` for test-only commits, `refactor(whatif): F-NNN <description>` for restructuring.
- Pre-commit hook runs full Go test suite (~25s) and refreshes GitNexus index. Allow up to 90s per commit.
- Working tree may have unstaged AGENTS.md / CLAUDE.md (gitnexus stat regen) — leave them; don't stage them.

### Test conventions

- TDD strictly: write the failing test first, run it, confirm it fails for the *right reason* (not a syntax error).
- Test names: `Test<Function>_<What>` for unit tests; for audit-finding-specific regression tests use `Test<Function>_F<NNN>_<Aspect>` (e.g., `TestGetAdjustedStandardDeduction_F001_Age65Single`).
- Keep new tests in the existing test file for the function under test (e.g., `tax_test.go`, `rmd_tax_test.go`, `social_security_test.go`, `calculator_test.go`).
- For tests that depend on multiple findings being fixed (cross-finding), gate the new assertions with `t.Skip("Re-enabled by PR N: F-NNN")` until that PR lands. Per user feedback memory.

### Pre-commit verification

After each fix, run before committing:
```bash
cd /home/darrell/bin/ai/budget2
go test ./internal/services/retirement/... ./internal/handlers/whatif/... ./internal/models/...
```
All tests must pass. If a previously-passing test now fails, that is either:
- The test was wrong and should be updated to the new behavior (most likely for the fixes here, since the audit caught real bugs), or
- The fix introduced a regression.
Distinguish via `git blame` and the audit findings before changing any test.

### When to escalate (BLOCKED)

- A fix needs to touch a file not in the PR's scope.
- A test fails for an unrelated reason that requires another PR's fix to resolve.
- The audit's "Recommended fix sketch" turns out to not work as written.

In any of these cases, the implementer subagent reports BLOCKED with detail. Don't workaround silently.

---

## File map

The PRs touch these files:

| File | Modified by PR | Why |
|------|----------------|-----|
| `internal/services/retirement/tax.go` | 1, 7 | F-001 std deduction; F-018 MFS thresholds |
| `internal/services/retirement/tax_test.go` | 1, 7 | New tests |
| `internal/services/retirement/calculator.go` | 2, 3, 10 | F-065 rebase fix; F-049 reinvestment basis; F-035 RMD timing |
| `internal/services/retirement/calculator_test.go` | 2, 3, 10 | New tests |
| `internal/services/retirement/calculator_expense_test.go` | 2 | F-065 regression test |
| `internal/services/retirement/social_security.go` | 5, 6 | F-026 zero-COLA; F-029 display flag |
| `internal/services/retirement/social_security_test.go` | 5, 6 | New tests |
| `internal/services/retirement/backtest.go` | 4 | F-057 off-by-one |
| `internal/services/retirement/backtest_test.go` | 4 | New tests |
| `internal/services/retirement/rmd.go` | 10 | F-032 SECURE 2.0 age-75 transition |
| `internal/services/retirement/rmd_tax_test.go` | 10 | New tests |
| `internal/models/whatif.go` | 7, 10 | F-018 MFS field; F-035 timing field; F-067 healthcare boundary |
| `internal/handlers/whatif/handlers_rates.go` | 10 | F-035 form field handling |
| `internal/handlers/whatif/form_spec.go` | 10 | F-035 enum spec |
| `web/templates/components/whatif/rate-assumptions.html` | 10 | F-035 timing dropdown |
| `web/templates/components/whatif/guardrails.html` | 8 | F-063 label rename |
| `web/templates/components/whatif/social-security.html` | 5, 6 | F-026 zero-COLA tooltip; F-029 corrected flag |
| `docs/what-if-retirement-verification.md` | 9 | F-070 refresh |
| `docs/whatif-math-audit-2026-05-05.md` | every PR | Update finding status from "open" to "closed in PR N" — append a "Resolution" line to each finding's body |

---

## Branch state assumed

- Current branch: `feat/whatif-fixes`
- HEAD commit: `3124858` (campaign spec)
- `dev` HEAD: `e9500b9` (audit complete)
- The audit's `b978aa9` PV/RMD-rate fix is already on `dev`.

---

## PR 1: F-001 — Add age-65+ additional standard deduction

**Audit reference:** `docs/whatif-math-audit-2026-05-05.md` F-001 (Appendix C). MEDIUM. ~$429 over-tax for Single 65+ filer at $80K.

**Files:**
- Modify: `internal/services/retirement/tax.go`
- Modify: `internal/services/retirement/tax_test.go`
- Modify: `internal/models/whatif.go` (add `Age65Count` field if not present in TaxConfig)

### Step 1: Add the failing test

In `internal/services/retirement/tax_test.go`, append:

```go
// F-001: TY2024 additional standard deduction for taxpayers 65+ or blind.
// Source: IRS Rev. Proc. 2023-34 §3.16(2):
//   Single/HoH: $1,950 per qualifying person
//   MFJ/MFS:    $1,550 per qualifying person
func TestGetAdjustedStandardDeduction_F001_Age65Single(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingSingle,
		Age65Count:   1,
	}, 0)
	got := tc.GetAdjustedStandardDeduction(0)
	want := 14600.0 + 1950.0 // base + age-65+ for one person
	if math.Abs(got-want) > 0.01 {
		t.Errorf("Single 65+ deduction = %.2f; want %.2f", got, want)
	}
}

func TestGetAdjustedStandardDeduction_F001_Age65MFJBoth(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingMarriedJoint,
		Age65Count:   2,
	}, 0)
	got := tc.GetAdjustedStandardDeduction(0)
	want := 29200.0 + 2*1550.0 // base + 2 × MFJ-each
	if math.Abs(got-want) > 0.01 {
		t.Errorf("MFJ both 65+ deduction = %.2f; want %.2f", got, want)
	}
}

func TestGetAdjustedStandardDeduction_F001_Age65Zero(t *testing.T) {
	// Age65Count = 0 must equal the pre-fix base deduction, no addition.
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingSingle,
		Age65Count:   0,
	}, 0)
	got := tc.GetAdjustedStandardDeduction(0)
	want := 14600.0
	if math.Abs(got-want) > 0.01 {
		t.Errorf("Single non-65+ deduction = %.2f; want %.2f (must equal base)", got, want)
	}
}

// WE-1.1 from audit: regression guard. Single, $80K ordinary, year-0,
// not yet 65: should still be $9,441 (the original WE-1.1 value).
func TestCalculateFederalTax_F001_AuditWE1_1_PreservedForUnder65(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingSingle,
		Age65Count:   0,
	}, 0)
	tax, _, _ := tc.CalculateFederalTax(80000, 0)
	want := 9441.00
	if math.Abs(tax-want) > 0.01 {
		t.Errorf("WE-1.1 (under-65) tax = %.2f; want %.2f", tax, want)
	}
}

// New: same scenario at age 65+ should be lower by $1,950 × 22%.
func TestCalculateFederalTax_F001_Age65SingleLowersTax(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingSingle,
		Age65Count:   1,
	}, 0)
	tax, _, _ := tc.CalculateFederalTax(80000, 0)
	want := 9012.00 // 80000 - (14600 + 1950) = 63450 taxable; computed by hand
	if math.Abs(tax-want) > 0.01 {
		t.Errorf("Single 65+ tax at $80K = %.2f; want %.2f", tax, want)
	}
}
```

### Step 2: Run tests; confirm RED

```bash
cd /home/darrell/bin/ai/budget2
go test ./internal/services/retirement/ -run F001 -v
```

Expected output: 4 test failures (FAIL). The first failures are likely about `Age65Count` not being a field on `TaxConfig`. That's the cue for Step 3.

### Step 3: Add `Age65Count` field to `TaxConfig`

In `internal/models/whatif.go`, find the `TaxConfig` struct definition (use grep: `grep -n "type TaxConfig" internal/models/whatif.go`). Add field:

```go
type TaxConfig struct {
    // ... existing fields ...
    Age65Count int // F-001: number of filers 65 or older (0, 1, or 2 for MFJ).
}
```

Verify `DefaultTaxConfig()` still compiles; the new field zero-defaults so no change needed there.

### Step 4: Add the additional-deduction lookup table to `tax.go`

In `internal/services/retirement/tax.go`, after `StandardDeduction2024`:

```go
// AdditionalStandardDeduction2024Age65 — TY2024 amounts per qualifying
// person 65 or older. Source: IRS Rev. Proc. 2023-34 §3.16(2).
var AdditionalStandardDeduction2024Age65 = map[models.FilingStatus]float64{
	models.FilingSingle:          1950,
	models.FilingHeadOfHousehold: 1950,
	models.FilingMarriedJoint:    1550,
	models.FilingMarriedSeparate: 1550,
}
```

### Step 5: Modify `GetAdjustedStandardDeduction`

Locate `GetAdjustedStandardDeduction` (around line 238 of `tax.go`). Modify its body so the result includes the age-65+ supplement when applicable:

```go
func (tc *TaxCalculator) GetAdjustedStandardDeduction(yearsFromBase int) float64 {
	status := normalizeFilingStatus(tc.FilingStatus)
	base, ok := StandardDeduction2024[status]
	if !ok {
		base = StandardDeduction2024[models.FilingSingle]
	}
	addPerPerson := AdditionalStandardDeduction2024Age65[status]
	count := tc.Config.Age65Count
	if count < 0 {
		count = 0
	}
	if count > 2 {
		count = 2
	}
	additional := float64(count) * addPerPerson
	return (base + additional) * tc.inflationFactor(yearsFromBase)
}
```

(`tc.Config` access: confirm `TaxCalculator` retains the source `*TaxConfig`; if not, the field needs to be added — see existing struct around line 159.)

### Step 6: Run tests; confirm GREEN

```bash
go test ./internal/services/retirement/ -run F001 -v
go test ./internal/services/retirement/...
```

All tests pass. The latter command verifies no regressions in the existing test suite.

### Step 7: Commit

```bash
git add internal/services/retirement/tax.go internal/services/retirement/tax_test.go internal/models/whatif.go
git commit -m "fix(whatif): F-001 add age-65+ additional standard deduction

TY2024 amounts per IRS Rev. Proc. 2023-34 §3.16(2). New TaxConfig.Age65Count
field (0/1/2). For Single 65+ at \$80K, lowers projected tax from \$9,441 to
\$9,012 (~4.8% reduction). Closes audit finding F-001.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Step 8: Mark F-001 resolved in audit doc

Append to F-001's body in `docs/whatif-math-audit-2026-05-05.md` (in both the section-1 body AND Appendix C):

```markdown
**Resolution:** Closed by commit <SHA> on `feat/whatif-fixes`. Added
`AdditionalStandardDeduction2024Age65` table, `TaxConfig.Age65Count` field,
and modified `GetAdjustedStandardDeduction`. Settings UI surfacing of the
new field is the user's call (not in this PR; tracked by recommended fix in
F-001). Today the field defaults to 0 — non-65+ projection behavior
unchanged.
```

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): mark F-001 resolved in PR 1

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## PR 2: F-065 — Chain rebase uses net inflation, not full inflation

**Audit reference:** F-065 (MEDIUM). When `SpendingDeclineRate > 0` is configured, `rebaseLivingExpensesAtTransition` rebases using full inflation factor, ignoring the decline rate. ~$179K compounding error in 30-year scenario.

**Files:**
- Modify: `internal/services/retirement/calculator.go`
- Modify: `internal/services/retirement/calculator_expense_test.go`

### Step 1: Identify the bug location

Use grep / read:
```bash
grep -n "rebaseLivingExpensesAtTransition" internal/services/retirement/calculator.go
```

Read the function (around line 165). Understand the cumulative-inflation parameter currently passed to it from the projection loop. The bug: at transition, `cumulative_inflation` is computed assuming full inflation rate, but actual living-expense compounding inside the projection uses `inflation - SpendingDeclineRate` (net inflation). The rebase needs to use the same net rate, not full.

### Step 2: Write the failing regression test

In `internal/services/retirement/calculator_expense_test.go`, append:

```go
// F-065: rebaseLivingExpensesAtTransition must use net inflation
// (Inflation - SpendingDeclineRate), not full inflation, when computing
// the value at the phase boundary. Otherwise the post-transition trajectory
// drifts upward by the decline-rate compounding error.
func TestSpendingPhaseTransition_F065_DeclineRateRespected(t *testing.T) {
	settings := &models.WhatIfSettings{
		CurrentAge:           65,
		ProjectionYears:      30,
		MonthlyExpenses:      10000,
		Inflation:            3.0, // 3%/yr
		SpendingDeclineRate:  1.0, // 1%/yr decline
		SpendingPhases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 65, Multiplier: 1.0},
			{Name: "Slow-Go", StartAge: 75, Multiplier: 0.85},
		},
	}
	calc := NewCalculator(settings)

	// Net inflation rate: 3% - 1% = 2%/yr.
	// Pre-transition (just before age 75, month 119): expected
	//   10000 * 1.02^(119/12) = 10000 * 1.02^9.917 ≈ 12180.71
	// At transition (month 120, age 75 phase change): expected
	//   pre = 10000 * 1.02^10 ≈ 12189.94
	//   new = pre * 0.85 ≈ 10361.45
	// Post-transition compounding continues at NET 2%, not full 3%.

	// Run a short projection and inspect month-120 living expense.
	// (The exact API for inspecting per-month expenses is via `RunProjection`
	// then reading the projection result's Years[].LivingExpense values.)
	result := calc.RunProjection()
	if result == nil || len(result.Years) < 11 {
		t.Fatalf("expected at least 11 years of projection")
	}
	// Year 10 starts at month 120 (age 75 = phase change to Slow-Go 85%).
	// Year-10 monthly living expense should be ≈ 10361.45 (post-rebase, net inflation).
	got := result.Years[10].MonthlyLivingExpenses
	want := 10361.45
	if math.Abs(got-want) > 5.00 { // ±$5 tolerance for monthly compounding rounding
		t.Errorf("Year 10 (post-Slow-Go transition) monthly expense = %.2f; want %.2f (net inflation rebase)", got, want)
	}
}
```

If `WhatIfSettings.SpendingDeclineRate` doesn't exist as a field, locate it via `grep -n "SpendingDeclineRate" internal/models/whatif.go`. If absent, this finding's repro setup needs adjustment — escalate as BLOCKED.

### Step 3: Run test; confirm RED

```bash
go test ./internal/services/retirement/ -run F065 -v
```

Expected: FAIL with delta > $5.

### Step 4: Apply the fix in `rebaseLivingExpensesAtTransition`

Read the existing function (around `calculator.go:165`). Identify where it accepts `cumulativeInflation` as a parameter. The fix: ensure the value passed in is computed using the *net* inflation rate from the projection loop, not full inflation. Two ways to fix:

**Option A (recommended): adjust the caller.** In the projection loop where `rebaseLivingExpensesAtTransition` is invoked, pass `cumulativeInflation` derived from `(Inflation - SpendingDeclineRate)`. Audit the call sites via grep:
```bash
grep -n "rebaseLivingExpensesAtTransition" internal/services/retirement/calculator.go
```

If callers track full-rate cumulative inflation and don't have a net-rate variant available, compute it inline at the call site:
```go
netInflationRate := s.Inflation - s.SpendingDeclineRate
netCumulative := plannerInflationFactorForYear(netInflationRate, float64(year))
expensesAtTransition = rebaseLivingExpensesAtTransition(s, phaseAge, netCumulative)
```

**Option B: change the function signature.** Pass `inflation` and `decline` separately and compute net inside. More invasive — only do this if the call site already has separate variables.

### Step 5: Run test; confirm GREEN

```bash
go test ./internal/services/retirement/ -run F065 -v
go test ./internal/services/retirement/...
```

If a previously-passing test in `calculator_expense_test.go` now fails, examine its assertions: it may have been encoding the buggy behavior. Update only after verifying via `git blame` and the audit's evidence.

### Step 6: Commit

```bash
git add internal/services/retirement/calculator.go internal/services/retirement/calculator_expense_test.go
git commit -m "fix(whatif): F-065 use net inflation in spending-phase rebase

When SpendingDeclineRate > 0, rebaseLivingExpensesAtTransition now uses
(Inflation - SpendingDeclineRate) for cumulative compounding to match the
projection-loop's per-month behavior. ~\$179K reduction in 30-year scenarios
where decline rate is set. Closes F-065.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Step 7: Mark F-065 resolved in audit doc

Append a "**Resolution:**" line to F-065's body in both section 10 and Appendix C, then commit.

---

## PR 3: F-049 — RMD reinvestment overstates taxable cost basis

**Audit reference:** F-049 (MEDIUM, PARTIAL). When an RMD is forced but cash is not needed, the gross (pre-tax) RMD is reinvested into the taxable account with the gross amount as new basis. Should use after-tax amount.

**Files:**
- Modify: `internal/services/retirement/calculator.go`
- Modify: `internal/services/retirement/calculator_test.go` (or `taxable_simulation_test.go`)

### Step 1: Failing test

In `internal/services/retirement/taxable_simulation_test.go`, append:

```go
// F-049: reinvestRequiredRMDToTaxableState must reinvest the after-tax
// portion of the RMD as basis, not the pre-tax amount. With marginal rate
// 22%, an RMD of $10,000 pays $2,200 tax → $7,800 reinvested with basis
// $7,800 (not $10,000).
func TestReinvestRequiredRMD_F049_BasisIsAfterTax(t *testing.T) {
	taxable := &taxableAccountState{
		MarketValue: 0,
		CostBasis:   0,
	}
	taxDeferred := 100000.0

	rmd := 10000.0
	marginalRate := 0.22

	// Expected: $7,800 added to MarketValue with $7,800 basis.
	addedBasis := reinvestRequiredRMDToTaxableState(rmd, marginalRate, &taxDeferred, taxable)
	wantNet := 7800.0
	if math.Abs(taxable.MarketValue-wantNet) > 0.01 {
		t.Errorf("MarketValue after reinvest = %.2f; want %.2f", taxable.MarketValue, wantNet)
	}
	if math.Abs(taxable.CostBasis-wantNet) > 0.01 {
		t.Errorf("CostBasis after reinvest = %.2f; want %.2f", taxable.CostBasis, wantNet)
	}
	if math.Abs(addedBasis-wantNet) > 0.01 {
		t.Errorf("addedBasis return = %.2f; want %.2f", addedBasis, wantNet)
	}
	if math.Abs(taxDeferred-90000.0) > 0.01 {
		t.Errorf("tax-deferred remaining = %.2f; want 90000", taxDeferred)
	}
}
```

If the existing `reinvestRequiredRMDToTaxableState` signature doesn't accept a `marginalRate` parameter, that's the API change to make.

### Step 2: Run test; confirm RED

```bash
go test ./internal/services/retirement/ -run F049 -v
```

Expected: signature mismatch or wrong basis.

### Step 3: Identify the existing function

```bash
grep -n "reinvestRequiredRMDToTaxableState" internal/services/retirement/calculator.go
```

Read the function. Understand its current signature and call sites.

### Step 4: Update signature and behavior

Modify `reinvestRequiredRMDToTaxableState` in `calculator.go`. Current likely signature:

```go
func reinvestRequiredRMDToTaxableState(monthlyRMD float64, taxDeferredBalance *float64, taxable *taxableAccountState) float64 {
    // ... reinvests gross monthlyRMD with gross basis ...
}
```

New signature:

```go
func reinvestRequiredRMDToTaxableState(monthlyRMD, marginalRate float64, taxDeferredBalance *float64, taxable *taxableAccountState) float64 {
	if marginalRate < 0 {
		marginalRate = 0
	}
	if marginalRate > 1 {
		marginalRate = 1
	}
	netAfterTax := monthlyRMD * (1 - marginalRate)
	taxable.MarketValue += netAfterTax
	taxable.CostBasis += netAfterTax
	*taxDeferredBalance -= monthlyRMD
	return netAfterTax
}
```

### Step 5: Update call sites

Grep for every caller and pass the appropriate marginal rate. Likely sources:
- The projection loop's tax accumulator has a marginal-rate getter.
- Or `tc.GetMarginalRate(...)` can be called at the appropriate point.

If the marginal rate is only known at year-end (after all income aggregated), that's a model concern — for monthly RMD reinvestment, an estimated marginal rate based on the prior year's bracket is acceptable. Document in code comment.

### Step 6: Run tests; confirm GREEN

```bash
go test ./internal/services/retirement/ -run F049 -v
go test ./internal/services/retirement/...
```

### Step 7: Commit

```bash
git add internal/services/retirement/calculator.go internal/services/retirement/taxable_simulation_test.go
git commit -m "fix(whatif): F-049 reinvest RMD with after-tax basis

reinvestRequiredRMDToTaxableState now accepts marginalRate and reinvests
the after-tax amount (RMD × (1 - marginalRate)) with that as the new
taxable basis. Prior code overstated basis by the tax portion, silently
under-collecting LTCG on later withdrawals. ~\$375 per \$10K reinvested
at 22% marginal / 15% LTCG. Closes F-049.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Step 8: Mark F-049 resolved in audit doc, commit.

---

## PR 4: F-057 — Backtest off-by-one window count

**Audit reference:** F-057 (MEDIUM). `GetAvailableStartYears` produces 67 sequences for a 30-year horizon over 1928-2024 data (97 years), but should produce 68 (97 - 30 + 1 = 68). The 1995 starting year is excluded.

**Files:**
- Modify: `internal/services/retirement/backtest.go`
- Modify: `internal/services/retirement/backtest_test.go`
- Possibly: `internal/services/retirement/historical_data.go` if window math lives there.

### Step 1: Failing test

In `internal/services/retirement/backtest_test.go`, append:

```go
// F-057: GetAvailableStartYears for a 30-year horizon over 1928-2024 (97 yrs)
// should produce 97-30+1 = 68 starting years (1928 through 1995 inclusive).
// Pre-fix produced 67 (excluded 1995).
func TestGetAvailableStartYears_F057_OffByOne(t *testing.T) {
	years := GetAvailableStartYears(30)
	if len(years) != 68 {
		t.Errorf("30-year horizon: got %d start years, want 68", len(years))
	}
	// First year present: 1928. Last year present: 1995.
	if years[0] != 1928 {
		t.Errorf("first start year = %d; want 1928", years[0])
	}
	if years[len(years)-1] != 1995 {
		t.Errorf("last start year = %d; want 1995", years[len(years)-1])
	}
}

func TestGetAvailableStartYears_F057_FullHistoryHorizon(t *testing.T) {
	// 97-year horizon over 97 years of data: exactly 1 start year (1928).
	years := GetAvailableStartYears(97)
	if len(years) != 1 {
		t.Errorf("97-year horizon: got %d start years, want 1", len(years))
	}
	if years[0] != 1928 {
		t.Errorf("first start year = %d; want 1928", years[0])
	}
}

func TestGetAvailableStartYears_F057_LongerThanHistory(t *testing.T) {
	// Horizon longer than data: zero start years, no panic.
	years := GetAvailableStartYears(98)
	if len(years) != 0 {
		t.Errorf("98-year horizon: got %d start years, want 0", len(years))
	}
}
```

### Step 2: Run; confirm RED

```bash
go test ./internal/services/retirement/ -run F057 -v
```

### Step 3: Identify the bug

```bash
grep -n "GetAvailableStartYears" internal/services/retirement/backtest.go internal/services/retirement/historical_data.go
```

Read the function. Identify the off-by-one. Likely the loop bound is `< maxStart` where it should be `<= maxStart`, or `lastYear - horizon` where it should be `lastYear - horizon + 1`.

### Step 4: Apply fix

Replace the buggy line with the correct boundary. Document in a comment:

```go
// F-057: inclusive upper bound; for N-year horizon the last viable start
// year is (lastYear - N + 1). E.g., 30-year horizon over 1928-2024 = 1995.
```

### Step 5: Run; confirm GREEN

```bash
go test ./internal/services/retirement/ -run F057 -v
go test ./internal/services/retirement/...
```

### Step 6: Commit + mark resolved

```bash
git add internal/services/retirement/backtest.go internal/services/retirement/backtest_test.go
git commit -m "fix(whatif): F-057 backtest start-year off-by-one

GetAvailableStartYears was excluding the last viable start year. For a
30-year horizon over 1928-2024 the function now returns 68 start years
(1928-1995) instead of 67. Closes F-057.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Append Resolution to F-057, commit.

---

## PR 5: F-026 — Honor zero-COLA when explicitly set

**Audit reference:** F-026 (MEDIUM). `normalizedSSCOLARate` substitutes a 2% default when the input is 0, making the "0% COLA" scenario inexpressible.

**Files:**
- Modify: `internal/services/retirement/social_security.go`
- Modify: `internal/services/retirement/social_security_test.go`
- Modify: `internal/models/whatif.go` (rename or add explicit "set" tracking)
- Possibly: `web/templates/components/whatif/social-security.html` (tooltip)

### Design choice on default vs explicit-zero

Per the spec's risk note: preserve the 2% default for *unset* fields, only respect 0% when *explicitly entered*. The Go zero value of `float64` is 0, which collides with "user typed 0." Two ways to disambiguate:

**Option A (recommended):** Use a pointer field `COLARate *float64`. `nil` → use default 2%; non-nil → use the value (including 0).

**Option B:** Add a sibling boolean `COLARateSet bool`.

**Option C:** Use a sentinel value (e.g., -1) for "unset."

Option A is the cleanest Go idiom. Pick A.

### Step 1: Failing test

```go
// F-026: explicit zero COLA must be honored, not silently substituted.
func TestNormalizedSSCOLARate_F026_ExplicitZero(t *testing.T) {
	zero := 0.0
	got := normalizedSSCOLARate(&zero)
	if got != 0.0 {
		t.Errorf("explicit zero COLA = %.4f; want 0.0", got)
	}
}

func TestNormalizedSSCOLARate_F026_UnsetUsesDefault(t *testing.T) {
	got := normalizedSSCOLARate(nil)
	want := 2.0 // 2% default (per current behavior)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("unset COLA = %.4f; want %.4f (default)", got, want)
	}
}

func TestNormalizedSSCOLARate_F026_NegativeClamped(t *testing.T) {
	neg := -1.0
	got := normalizedSSCOLARate(&neg)
	if got != 0.0 {
		t.Errorf("negative COLA = %.4f; want 0.0 (SS COLA never negative)", got)
	}
}
```

### Step 2: Run; confirm RED

Will fail because signature doesn't take `*float64` yet.

### Step 3: Update signature and callers

Change `normalizedSSCOLARate(rate float64) float64` → `normalizedSSCOLARate(rate *float64) float64`. Update body:

```go
func normalizedSSCOLARate(rate *float64) float64 {
	if rate == nil {
		return 2.0 // F-026: default 2% only when caller did not supply a value
	}
	if *rate < 0 {
		return 0.0
	}
	return *rate
}
```

Update every caller. Use grep:
```bash
grep -n "normalizedSSCOLARate" internal/services/retirement/
```

### Step 4: Decide on settings field shape

Two paths:

**Path 1: Change `WhatIfSettings.SocialSecurity.COLARate` to `*float64`.** Requires JSON unmarshalling adjustments + UI handling. Higher blast radius.

**Path 2: Keep field as `float64`, add a sibling `COLARateSet bool`.** Simpler. UI just sets the bool whenever the input is touched.

Path 2 has less blast radius. Use it. Add field:

```go
type SocialSecurityConfig struct {
    // ... existing fields ...
    COLARate    float64
    COLARateSet bool // F-026: distinguishes explicit 0 from unset
}
```

In the call site that previously passed `s.SocialSecurity.COLARate`, change to:
```go
var rate *float64
if s.SocialSecurity.COLARateSet {
    rate = &s.SocialSecurity.COLARate
}
return normalizedSSCOLARate(rate)
```

UI form handler must set `COLARateSet = true` whenever the user submits the form (any submit means the user is acknowledging the value, even if 0).

### Step 5: Update the form spec

In `internal/handlers/whatif/form_spec.go` and/or `handlers_rates.go`, the `cola_rate` field handler must also set `COLARateSet = true`. Locate via grep:
```bash
grep -n "COLARate\|cola_rate" internal/handlers/whatif/
```

### Step 6: Run; confirm GREEN

```bash
go test ./internal/services/retirement/...
go test ./internal/handlers/whatif/...
```

### Step 7: Commit + mark resolved

```bash
git add internal/services/retirement/social_security.go internal/services/retirement/social_security_test.go internal/models/whatif.go internal/handlers/whatif/
git commit -m "fix(whatif): F-026 honor explicit zero SS COLA rate

normalizedSSCOLARate takes *float64; nil means unset (defaults to 2%);
non-nil zero means user explicitly chose 0% (no COLA). New
SocialSecurityConfig.COLARateSet field distinguishes the two cases. Form
handlers set the flag on every submit. Closes F-026.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Mark resolved in audit doc.

---

## PR 6: F-029 — Spousal-benefit display flag fix

**Audit reference:** F-029 (MEDIUM). In `RunSSAnalysis` (line 484 area), `SpouseUsingSpousalBenefit` flag and dollar amount use `ss.FRABenefit * 0.5` instead of `primaryPIA * 0.5`. Wrong when primary is already claiming at non-FRA age.

**Files:**
- Modify: `internal/services/retirement/social_security.go`
- Modify: `internal/services/retirement/social_security_test.go`

### Step 1: Failing test

```go
// F-029: When primary is already claiming at age 62 (non-FRA), spousal
// computation must use derived primary PIA (the at-FRA equivalent), not
// the primary's actual reduced FRABenefit.
func TestRunSSAnalysis_F029_SpousalUsesPrimaryPIA(t *testing.T) {
	settings := &models.WhatIfSettings{
		CurrentAge: 67,
		SpouseAge:  62,
		SocialSecurity: models.SocialSecurityConfig{
			Enabled:        true,
			ClaimAge:       62,           // primary already claiming at 62
			FRA:            67,
			FRABenefit:     1400.0,        // 70% of $2,000 PIA
			SpouseEnabled:  true,
			SpouseClaimAge: 62,
			SpouseFRA:      67,
			SpouseFRABenefit: 700.0,       // spouse own < spousal
		},
	}
	calc := NewCalculator(settings)
	analysis := calc.RunSSAnalysis()
	if analysis == nil {
		t.Fatal("expected non-nil SS analysis")
	}
	// derived primary PIA from FRABenefit $1400 at claim 62 / FRA 67 is $2,000.
	// Spousal at-FRA = $1,000. Spouse at 62 reduces by 35% → $650.
	// Top-up = max(0, 650 - 700) = 0 ... so SpouseUsingSpousalBenefit = false here.
	// Adjust the test to actually exercise the bug case:
	// give spouse a much smaller own benefit so spousal kicks in.
	settings.SocialSecurity.SpouseFRABenefit = 300.0
	calc = NewCalculator(settings)
	analysis = calc.RunSSAnalysis()
	// Spouse own at 62 = 300 * 0.70 = 210.
	// Spousal at 62 (using correct PIA $2,000) = $1,000 * 0.65 = $650.
	// Top-up = $650 - $210 = $440. Total spouse = $650.
	// Pre-fix used FRABenefit $1,400 instead of PIA $2,000:
	//   buggy spousal at FRA = $1,400 * 0.5 = $700 → at 62 = $700 * 0.65 = $455
	//   buggy total = max($210, $455) = $455.
	// Post-fix should be $650.
	wantTotalSpouse := 650.0
	if math.Abs(analysis.SpouseTotalMonthly-wantTotalSpouse) > 1.0 {
		t.Errorf("spouse total monthly = %.2f; want %.2f", analysis.SpouseTotalMonthly, wantTotalSpouse)
	}
	if !analysis.SpouseUsingSpousalBenefit {
		t.Errorf("expected SpouseUsingSpousalBenefit = true (spousal $650 > own $210)")
	}
}
```

(Field names like `SpouseTotalMonthly`, `SpouseUsingSpousalBenefit`, `FRABenefit` may differ slightly — read the actual `models.SSComparisonAnalysis` struct and adjust to match.)

### Step 2: Run; confirm RED

### Step 3: Apply fix at `social_security.go:484`

Read the surrounding context. The buggy line:

```go
spousalAtFRA := ss.FRABenefit * 0.5  // F-029 bug: uses reduced benefit
```

should be:

```go
primaryPIA := DerivedPIA(ss.FRABenefit, ss.FRA, ss.ClaimAge)
spousalAtFRA := primaryPIA * 0.5
```

(Confirm that when primary is *not* yet claiming, the existing code uses the primary PIA correctly elsewhere — only fix the already-claiming branch.)

### Step 4: Run; confirm GREEN

### Step 5: Commit + mark resolved

```bash
git add internal/services/retirement/social_security.go internal/services/retirement/social_security_test.go
git commit -m "fix(whatif): F-029 derive primary PIA for spousal computation

When primary is already claiming at non-FRA age, spousal benefit is
computed from the derived primary PIA (DerivedPIA(FRABenefit, FRA,
ClaimAge)) rather than the actual reduced FRABenefit. Spouse total
monthly and SpouseUsingSpousalBenefit flag now correct. Closes F-029.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Mark resolved in audit doc.

---

## PR 7: F-018 — MFS taxable Social Security thresholds

**Audit reference:** F-018 (MEDIUM). `socialSecurityTaxThresholds` map lacks an MFS entry. MFS taxpayers fall through to a default that may apply 85% to all SS regardless of income; § 86 actually has two MFS sub-cases:
- MFS lived with spouse at any time during year: $0/$0 thresholds (entire SS may be taxable up to 85% cap)
- MFS lived apart entire year: same as Single ($25,000 / $34,000)

**Files:**
- Modify: `internal/services/retirement/tax.go`
- Modify: `internal/models/whatif.go` (new field for MFS sub-case)
- Modify: `internal/services/retirement/tax_test.go`

### Step 1: Failing tests

```go
// F-018: MFS lived-with-spouse — both thresholds $0, full 85% applies above 0.
func TestCalculateTaxableSocialSecurity_F018_MFSLivedWithSpouse(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:        models.FilingMarriedSeparate,
		MFSLivedWithSpouse:  true,
	}, 0)
	taxable := tc.CalculateTaxableSocialSecurity(20000, 0, 0, 0)
	want := 0.85 * 20000 // $17,000 (85% cap immediately applies)
	if math.Abs(taxable-want) > 0.01 {
		t.Errorf("MFS-lived-with-spouse: taxable SS = %.2f; want %.2f", taxable, want)
	}
}

// F-018: MFS lived-apart — uses Single thresholds.
func TestCalculateTaxableSocialSecurity_F018_MFSLivedApart(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:        models.FilingMarriedSeparate,
		MFSLivedWithSpouse:  false,
	}, 0)
	// MFS lived apart with $20K SS, $30K other income:
	// Provisional = $30K + $10K = $40K.
	// Above upper threshold $34K. Replicate Single logic:
	// Step 1: ($40K - $34K) * 85% = $5,100
	// Step 2: 50% × min(($40K - $25K), ($34K - $25K)) = 50% × $9K = $4,500
	// Sum: $9,600. Cap: 85% × $20K = $17,000. Take lesser: $9,600.
	taxable := tc.CalculateTaxableSocialSecurity(20000, 30000, 0, 0)
	want := 9600.0
	if math.Abs(taxable-want) > 0.01 {
		t.Errorf("MFS-lived-apart: taxable SS = %.2f; want %.2f", taxable, want)
	}
}
```

### Step 2: Run; confirm RED (probably needs `MFSLivedWithSpouse` field)

### Step 3: Add field

In `internal/models/whatif.go`:

```go
type TaxConfig struct {
    // ... existing ...
    MFSLivedWithSpouse bool // F-018: distinguishes MFS § 86(c)(2) sub-cases
}
```

### Step 4: Update threshold lookup

In `tax.go`, add MFS thresholds and a sub-case dispatcher:

```go
var socialSecurityTaxThresholdsMFSLivedApart = socialSecurityTaxThreshold{
	BaseThreshold: 25000, UpperThreshold: 34000, BaseTaxableAmount: 4500,
}
var socialSecurityTaxThresholdsMFSLivedTogether = socialSecurityTaxThreshold{
	BaseThreshold: 0, UpperThreshold: 0, BaseTaxableAmount: 0,
}
```

Modify `CalculateTaxableSocialSecurity` (and the method on `TaxCalculator`) to dispatch on filing status + `MFSLivedWithSpouse`:

```go
// inside the function, after status normalization:
var thresholds socialSecurityTaxThreshold
if status == models.FilingMarriedSeparate {
	if config.MFSLivedWithSpouse {
		thresholds = socialSecurityTaxThresholdsMFSLivedTogether
	} else {
		thresholds = socialSecurityTaxThresholdsMFSLivedApart
	}
} else {
	thresholds = socialSecurityTaxThresholds[status]
}
```

(Adjust API: pass `*TaxConfig` or carry the bool through context. The free function signature may need to add a parameter; the method version reads from `tc.Config`.)

### Step 5: Run; confirm GREEN

### Step 6: Commit + mark resolved

```bash
git add internal/services/retirement/tax.go internal/services/retirement/tax_test.go internal/models/whatif.go
git commit -m "fix(whatif): F-018 split MFS taxable SS by lived-with-spouse

26 USC § 86(c)(2) gives MFS filers two threshold sub-cases. Code now
dispatches on TaxConfig.MFSLivedWithSpouse: true → \$0/\$0 thresholds
(85% cap immediate); false → Single thresholds (\$25K/\$34K). Closes
F-018.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Mark resolved in audit doc.

---

## PR 8: F-063 — Rename guardrails label

**Audit reference:** F-063 (INFO, elevated to action). Code is not Guyton-Klinger; rename UI to honest label and add follow-up ticket for full G-K.

**Files:**
- Modify: `web/templates/components/whatif/guardrails.html` (or wherever the toggle's label lives — find via grep)
- Possibly `web/templates/components/whatif/rate-assumptions.html` if guardrails are surfaced there
- Modify: `docs/whatif-math-audit-2026-05-05.md` (mark F-063 resolved)
- Create: `docs/superpowers/specs/2026-05-XX-full-gk-guardrails-followup.md` — a one-page follow-up ticket for full G-K implementation if user usage data later justifies it

### Step 1: Find the guardrails template / label

```bash
grep -rn "Guyton" web/templates/ internal/templates/
```

### Step 2: Rename label and add tooltip

Replace `Guyton-Klinger guardrails` with `Drop/rise guardrails (simple)` everywhere on the UI. Add a tooltip / help text:

```html
<span class="help-text">
  Cuts withdrawal 10% if portfolio drops 25%; raises 10% if portfolio rises 30%.
  This is a simplified guardrail — not the four-rule Guyton-Klinger 2006 model.
</span>
```

### Step 3: Optionally update settings field name

If `Guardrails.Enabled` field is named to imply G-K, leave the field name unchanged (no migration cost) but update its godoc comment to clarify.

### Step 4: Verify rendering

```bash
cd /home/darrell/bin/ai/budget2
make build && ./budget2 &
# Then in browser: load /whatif and check the guardrails section's label and tooltip.
# Or: write a template-render test if the codebase has them. The handlers_test.go
# package may already render the guardrails partial via a test request.
```

### Step 5: Commit

```bash
git add web/templates/components/whatif/
git commit -m "fix(whatif): F-063 rename guardrails — not actual Guyton-Klinger

The implementation is a portfolio-drop/rise rule, not the four-rule
Guyton & Klinger (2006) model. Renamed 'Guyton-Klinger guardrails' to
'Drop/rise guardrails (simple)' with honest tooltip. Math unchanged.
Full G-K implementation tracked separately. Closes F-063.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Step 6: File the follow-up ticket

Create `docs/superpowers/specs/2026-05-06-full-gk-guardrails-followup.md`:

```markdown
# Full Guyton-Klinger guardrails — follow-up

**Status:** Open. Filed during whatif fix campaign; gated on usage data.

## Background

The whatif page exposes a "guardrails" toggle. Pre-2026-05-06 the UI
labeled it as Guyton-Klinger; the code implements a simpler portfolio-drop/
rise rule. PR 8 of the fix campaign (commit <SHA>) renamed the UI label
to be honest. This ticket tracks implementing the actual G-K rules if
usage data justifies the work.

## Scope

Implement Guyton & Klinger (2006), *Decision Rules and Maximum Initial
Withdrawal Rates*. Four rules:

1. Capital Preservation Rule (CPR)
2. Prosperity Rule (PR)
3. Inflation Rule
4. Withdrawal Rule

## Estimated effort

2-3 days TDD: rule logic + tests + UI integration + scenario regression.

## Trigger to start

Either:
- User feedback explicitly requests G-K, OR
- Telemetry shows non-trivial usage of the simple guardrails toggle.
```

Commit the follow-up ticket along with the rename:

```bash
git add docs/superpowers/specs/2026-05-06-full-gk-guardrails-followup.md
git commit -m "docs: file follow-up ticket for full G-K implementation

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Mark F-063 resolved in audit doc.

---

## PR 9: F-070 — Refresh verification doc

**Audit reference:** F-070 (INFO). `docs/what-if-retirement-verification.md` reference numbers are stale post-`b978aa9`.

**Files:**
- Modify: `docs/what-if-retirement-verification.md`

### Step 1: Re-run the verification scenario

The doc describes a specific scenario with concrete settings. Reproduce:

1. Read `docs/what-if-retirement-verification.md` — note the exact scenario settings.
2. Load `data/settings/whatif.json` (the saved Current Plan).
3. Run `retirement.NewCalculator(settings).RunFullAnalysis()` and capture the new values.
4. The post-fix campaign code (PRs 1-8 already applied) is the new baseline.

Practical approach: write a temp Go test that exercises the documented scenario and prints all the values mentioned in the doc:

```go
// audit_we10_4_test.go (TEMPORARY — do not commit)
package retirement

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"budget2/internal/models"
)

func TestF070_RefreshVerificationDocValues(t *testing.T) {
	data, err := os.ReadFile("../../../data/settings/whatif.json")
	if err != nil {
		t.Skipf("settings file not available: %v", err)
	}
	var settings models.WhatIfSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	calc := NewCalculator(&settings)
	analysis := calc.RunFullAnalysis()

	// Print every value the doc references; the test never asserts.
	fmt.Printf("BudgetFit current monthly expenses: %.2f\n", analysis.BudgetFit.CurrentExpenses)
	fmt.Printf("BudgetFit current monthly income: %.2f\n", analysis.BudgetFit.CurrentIncome)
	// ... and so on for each documented field ...
	fmt.Printf("PV total resources: %.2f\n", analysis.PresentValue.TotalResources)
	fmt.Printf("PV income: %.2f\n", analysis.PresentValue.PVIncome)
	fmt.Printf("PV expenses: %.2f\n", analysis.PresentValue.PVExpenses)
	fmt.Printf("PV coverage ratio: %.2f\n", analysis.PresentValue.CoverageRatio)
	fmt.Printf("PV surplus/deficit: %.2f\n", analysis.PresentValue.Surplus)
	fmt.Printf("Final balance nominal: %.2f\n", analysis.Projection.FinalBalanceNominal)
	fmt.Printf("Final balance real: %.2f\n", analysis.Projection.FinalBalanceReal)
	fmt.Printf("Taxes consumed pct: %.1f%%\n", analysis.ProjectionExplainability.TaxesConsumedPct*100)
	fmt.Printf("Cumulative inflation pct: %.1f%%\n", analysis.ProjectionExplainability.CumulativeInflationPct*100)
}
```

Run with `go test ./internal/services/retirement -run F070 -v 2>&1 | tee /tmp/verify_values.txt`. Read the output.

Delete the temp test before commit.

### Step 2: Update the verification doc

Replace every numeric value in `docs/what-if-retirement-verification.md` with the new value. Keep the structure / prose unchanged.

Add a top-of-doc note:

```markdown
**Last refreshed:** 2026-05-06 — post-`b978aa9` compounding fix and
post-`feat/whatif-fixes` PRs 1-8 (audit findings F-001, F-018, F-026,
F-029, F-049, F-057, F-063, F-065 closed).
```

### Step 3: Commit

```bash
git add docs/what-if-retirement-verification.md
git commit -m "docs(verify): F-070 refresh verification reference values

Refresh the live-page reference values to reflect b978aa9 compounding
and PRs 1-8 of the fix campaign. New baseline for future verification
passes. Closes F-070.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Mark resolved in audit doc.

---

## PR 10: F-032 + F-035 + F-067 — Year-boundary config gaps

**Audit references:**
- F-032 (MEDIUM): SECURE 2.0 RMD start age 75 not modeled for projections crossing 2033.
- F-035 (MEDIUM): RMD timing fixed at start-of-year. Make configurable; default mid-year.
- F-067 (LOW): Healthcare ACA→Medicare transition uses year-based age (off by up to 11 months for mid-year birthdays).

**Files:** (largest PR)
- Modify: `internal/services/retirement/rmd.go`
- Modify: `internal/services/retirement/calculator.go`
- Modify: `internal/services/retirement/rmd_tax_test.go`
- Modify: `internal/services/retirement/calculator_expense_test.go` (healthcare timing)
- Modify: `internal/models/whatif.go` (add `RMDTiming` enum, `HealthcarePerson` already has `BirthMonth` — verify)
- Modify: `internal/handlers/whatif/handlers_rates.go` and `form_spec.go` (RMDTiming form field)
- Modify: `web/templates/components/whatif/rate-assumptions.html` (RMDTiming dropdown)

### Step 1: F-032 SECURE 2.0 — failing test

In `rmd_tax_test.go`:

```go
// F-032: RMD start age must transition to 75 for projections starting 2033+.
func TestRMDStartAge_F032_SECURE2_PostJan2033(t *testing.T) {
	// Projection starting 2033: RMD start age should be 75.
	settings := &models.WhatIfSettings{
		StartDate: time.Date(2033, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	got := EffectiveRMDStartAge(settings)
	want := 75
	if got != want {
		t.Errorf("RMD start age for 2033 projection = %d; want %d", got, want)
	}
}

func TestRMDStartAge_F032_SECURE2_Pre2033(t *testing.T) {
	settings := &models.WhatIfSettings{
		StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	got := EffectiveRMDStartAge(settings)
	want := 73
	if got != want {
		t.Errorf("RMD start age for 2026 projection = %d; want %d", got, want)
	}
}
```

### Step 2: Add `EffectiveRMDStartAge`

In `rmd.go`:

```go
// EffectiveRMDStartAge returns the SECURE 2.0 RMD start age for the
// projection's start year. 73 for projections starting before 2033;
// 75 for projections starting 2033 or later.
func EffectiveRMDStartAge(s *models.WhatIfSettings) int {
	if s == nil {
		return 73
	}
	year := s.StartDate.Year()
	if year >= 2033 {
		return 75
	}
	return 73
}
```

Replace usages of `RMDStartAge` constant with calls to `EffectiveRMDStartAge(s)` everywhere. Search via grep.

### Step 3: F-035 — failing test for configurable timing

In `rmd_tax_test.go`:

```go
// F-035: RMD timing modes — start_of_year, mid_year, end_of_year.
func TestCalculateRMDAnalysis_F035_TimingMidYear(t *testing.T) {
	settings := &models.WhatIfSettings{
		CurrentAge:           73,
		ProjectionYears:      1,
		PortfolioValue:       1000000,
		TaxDeferredAllocation: 1.0,
		Inflation:            0,
		ExpectedReturn:       7.0,
		RMDTiming:            models.RMDTimingMidYear,
	}
	calc := NewCalculator(settings)
	analysis := calc.CalculateRMDAnalysis()
	if analysis == nil {
		t.Fatal("nil analysis")
	}
	// At age 73 with $1M, factor 26.5 → annual RMD $37,735.85.
	// Mid-year timing: half-year of growth before RMD, half after.
	// Expected balance after year 1:
	//   midYear = 1M × 1.07^0.5 ≈ 1,034,408
	//   afterRMD = 1,034,408 - 37,735.85 = 996,672
	//   eoy = afterRMD × 1.07^0.5 ≈ 1,031,037
	want := 1031037.0
	got := analysis.YearEndBalance[0]
	if math.Abs(got-want) > 50.0 { // ±$50 for rounding
		t.Errorf("Year-1 EOY balance (mid-year RMD) = %.2f; want ~%.2f", got, want)
	}
}

func TestCalculateRMDAnalysis_F035_TimingDefaultIsMidYear(t *testing.T) {
	// When RMDTiming is the zero value, behavior should match mid_year.
	// (Old behavior was implicitly start_of_year; we accept new scenarios
	// default to mid_year — saved scenarios load with explicit start_of_year.)
	settings := &models.WhatIfSettings{
		CurrentAge:           73,
		ProjectionYears:      1,
		PortfolioValue:       1000000,
		TaxDeferredAllocation: 1.0,
		Inflation:            0,
		ExpectedReturn:       7.0,
		// RMDTiming unset → zero value
	}
	calc := NewCalculator(settings)
	analysis := calc.CalculateRMDAnalysis()
	want := 1031037.0
	got := analysis.YearEndBalance[0]
	if math.Abs(got-want) > 50.0 {
		t.Errorf("default-timing EOY balance = %.2f; want %.2f (mid_year)", got, want)
	}
}
```

### Step 4: Add `RMDTiming` enum and field

In `internal/models/whatif.go`:

```go
type RMDTiming string

const (
	RMDTimingStartOfYear RMDTiming = "start_of_year"
	RMDTimingMidYear     RMDTiming = "mid_year"
	RMDTimingEndOfYear   RMDTiming = "end_of_year"
)

func NormalizeRMDTiming(t RMDTiming) RMDTiming {
	switch t {
	case RMDTimingStartOfYear, RMDTimingMidYear, RMDTimingEndOfYear:
		return t
	default:
		return RMDTimingMidYear
	}
}

type WhatIfSettings struct {
    // ... existing ...
    RMDTiming RMDTiming
}
```

### Step 5: Update `CalculateRMDAnalysis` to honor timing

Read the current function. Modify the year-loop to apply growth in two halves around the RMD draw:

```go
// Inside the year loop, post-fix:
timing := models.NormalizeRMDTiming(s.RMDTiming)
beforeFraction, afterFraction := rmdGrowthFractions(timing) // returns 0/1 or 0.5/0.5 or 1/0
preRMDBalance := currentBalance * math.Pow(1+monthlyReturn, 12*beforeFraction)
preRMDBalance -= rmd
currentBalance = preRMDBalance * math.Pow(1+monthlyReturn, 12*afterFraction)
```

Add helper:

```go
func rmdGrowthFractions(timing models.RMDTiming) (before, after float64) {
	switch timing {
	case models.RMDTimingStartOfYear:
		return 0.0, 1.0
	case models.RMDTimingEndOfYear:
		return 1.0, 0.0
	default:
		return 0.5, 0.5
	}
}
```

### Step 6: Migration concern for saved scenarios

Per the spec's risk note: existing saved scenarios load with `RMDTiming = ""` (zero value). The `NormalizeRMDTiming` call returns `mid_year` for that. **This changes behavior for existing scenarios.** Two ways:

**Option A:** Migrate on load. In `settings.go`'s load path, if `RMDTiming == ""` and the file's saved-version pre-dates this PR, set it to `start_of_year` (preserving old behavior). New scenarios default to `mid_year` via the form.
**Option B:** Default normalization to `start_of_year`. Existing scenarios unchanged. New scenarios get `mid_year` only if the form explicitly sets it.

Pick A for cleaner forward path. Add to settings load:

```go
// Migration: if RMDTiming is empty, this is a pre-F-035 saved scenario.
// Preserve original start-of-year behavior.
if settings.RMDTiming == "" {
    settings.RMDTiming = models.RMDTimingStartOfYear
}
```

### Step 7: Form field handling for F-035

In `internal/handlers/whatif/form_spec.go`, add an enum-style entry for `rmd_timing`. Reference how `projection_timing` is handled (similar pattern). The field appears in the rate-assumptions section.

In the template `web/templates/components/whatif/rate-assumptions.html`, add a labeled select:

```html
<div class="form-row">
  <label for="rmd-timing">RMD timing</label>
  <select id="rmd-timing" name="rmd_timing">
    <option value="start_of_year"{{ if eq .Settings.RMDTiming "start_of_year" }} selected{{ end }}>Start of year (conservative)</option>
    <option value="mid_year"{{ if eq .Settings.RMDTiming "mid_year" }} selected{{ end }}>Mid year (recommended)</option>
    <option value="end_of_year"{{ if eq .Settings.RMDTiming "end_of_year" }} selected{{ end }}>End of year</option>
  </select>
</div>
```

### Step 8: F-067 healthcare transition — failing test

Find the function that determines per-month healthcare cost in projection. Likely in `WhatIfSettings.GetTotalHealthcareCost(month int)` or similar. Healthcare uses an integer-year age comparison; needs month precision when birth month is mid-year.

```go
// F-067: Healthcare ACA → Medicare transition must respect birth month.
// Person born in month 6 (July) of birth-year, currently age 64 at month 0:
// - Months 0-5 (age 64): ACA cost
// - Month 6 (turns 65): Medicare cost
// - Months 7+: Medicare cost
func TestHealthcareCost_F067_TransitionAtBirthMonth(t *testing.T) {
	settings := &models.WhatIfSettings{
		// ... configure a healthcare person with BirthMonth = 6, CurrentAge = 64 ...
		HealthcarePersons: []models.HealthcarePerson{{
			ID:           "p1",
			Name:         "Test",
			BirthMonth:   6,
			CurrentAge:   64,
			MonthlyACA:   1000,
			EmployerContribution: 400,
			MonthlyMedicare: 200,
		}},
	}

	got5 := settings.HealthcareMonthlyCost(5) // before birthday → ACA - employer
	want5 := 600.0
	if math.Abs(got5-want5) > 0.01 {
		t.Errorf("month 5 cost = %.2f; want %.2f", got5, want5)
	}
	got6 := settings.HealthcareMonthlyCost(6) // birthday month → Medicare
	want6 := 200.0
	if math.Abs(got6-want6) > 0.01 {
		t.Errorf("month 6 cost = %.2f; want %.2f", got6, want6)
	}
}
```

(Function names like `HealthcareMonthlyCost` may differ — find the actual one via `grep -n "func.*Healthcare" internal/models/whatif.go`.)

### Step 9: Apply F-067 fix

In the healthcare cost function, replace integer-year age comparison with month-precise comparison. Pseudocode:

```go
// Pre-fix:
ageInYears := person.CurrentAge + (month / 12)

// Post-fix (F-067):
totalMonths := person.CurrentAge*12 + person.BirthMonth + month
ageInMonths := totalMonths
medicareEligibleMonth := 65*12 // age 65 in months

isMedicareEligible := ageInMonths >= medicareEligibleMonth - person.BirthMonth
```

(The exact arithmetic depends on how `CurrentAge` is anchored. Read the existing code.)

### Step 10: Run all tests; confirm GREEN

```bash
go test ./internal/services/retirement/...
go test ./internal/handlers/whatif/...
go test ./internal/models/...
```

### Step 11: Commit

```bash
git add internal/services/retirement/ internal/models/whatif.go internal/handlers/whatif/ web/templates/components/whatif/rate-assumptions.html
git commit -m "fix(whatif): F-032 + F-035 + F-067 year-boundary config gaps

F-032: EffectiveRMDStartAge returns 75 for projections starting 2033+
       per SECURE 2.0; 73 otherwise. Replaces fixed RMDStartAge constant.
F-035: RMDTiming enum (start_of_year/mid_year/end_of_year), default
       mid_year for new scenarios, start_of_year for legacy. Surfaced in
       rate-assumptions UI as a select.
F-067: Healthcare ACA→Medicare transition is now month-precise (uses
       person.BirthMonth) instead of year-bucketed.

Closes F-032, F-035, F-067.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Mark all three resolved in audit doc.

---

## PR 11 (deferred): TY2024 → TY2025 constants bump

**Status:** Held until user approves timing.

**Files:**
- Modify: `internal/services/retirement/tax.go` — bump tables
- Modify: tests — recompute worked-example values
- Modify: `docs/what-if-retirement-verification.md` — refresh again
- Modify: `docs/whatif-math-audit-2026-05-05.md` — Appendix A status updates

Plan deferred. When the user signals go, add Tasks 1-N for each table cell update with new constants from IRS Rev. Proc. 2024-40.

---

## Final pass: audit doc cross-check

After PRs 1-10 land, verify the audit doc's findings ledger has Resolution lines for F-001, F-018, F-026, F-029, F-032, F-035, F-049, F-057, F-063, F-065, F-067, F-070. Eleven findings closed; remaining 60 (45 LOWs + 15 INFOs/MEDIUMs not in scope) stay open as backlog.

---

## Self-review checklist

1. **Spec coverage**
   - [ ] Every P1 finding (F-001, F-018, F-026, F-029, F-049, F-057, F-065) has a dedicated PR. ✓
   - [ ] P2 findings (F-063, F-070) have PRs. ✓
   - [ ] P3 findings (F-032, F-035, F-067) bundled in PR 10. ✓
   - [ ] Constants bump deferred to PR 11. ✓
   - [ ] Test-hardening LOWs explicitly out of scope. ✓
   - [ ] F-035 implemented as configurable per spec decision. ✓
   - [ ] F-063 rename-only per spec decision; follow-up ticket filed. ✓

2. **Placeholder scan**
   - [ ] No "TBD" / "TODO" markers.
   - [ ] Every step has concrete code or a concrete grep / commit command.
   - [ ] One soft-pointer remains: in PR 6 the test is conditional on field names — implementer must read `models.SSComparisonAnalysis` to confirm. The plan acknowledges this and instructs the implementer.

3. **Type / name consistency**
   - [ ] `Age65Count` (PR 1, used in PR 7 indirectly via `TaxConfig`) — same name throughout.
   - [ ] `RMDTiming` enum + `NormalizeRMDTiming` helper (PR 10) — same name throughout.
   - [ ] `EffectiveRMDStartAge` (PR 10) — same name everywhere.
   - [ ] `MFSLivedWithSpouse` field (PR 7) — same name.

---

## Execution

This plan is ready. Subagent-Driven execution recommended:

- One subagent dispatch per PR.
- After each PR: spec compliance review (verify the specific findings closed) + code quality review (verify no over-engineering, tests target real boundaries).
- Fix loops if either review finds issues.
- Mark task complete; move to next PR.

Total expected effort: 10 PRs × ~30-90 min implementer + ~10-30 min reviews = 6-15 hours wall-clock, multi-session.
