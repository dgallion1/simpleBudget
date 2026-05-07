# RMD Audit Hole Followup + Portfolio Range UX Fix

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close three RMD-projection holes left by the F-049/F-035/F-032 closures (surplus-RMD gross/net field bug, ignored RMDTiming setting, projection-vs-analysis start-age divergence) and fix the Portfolio Value range dropdown that resets every time the user picks a different range.

**Architecture:** Four independent PRs. Each begins with a failing test that pins the user-visible bug, then minimal implementation, then commit. PRs land off `dev` in order; F-076 has no dependency on the RMD work but is bundled because it surfaced during the same review.

**Tech Stack:** Go 1.26, chi router, HTMX, html/template, no external test deps.

---

## Pre-Flight (do once before PR 1)

**Files referenced throughout:**
- `internal/services/retirement/calculator.go`
- `internal/services/retirement/backtest.go`
- `internal/services/retirement/rmd.go`
- `internal/handlers/whatif/handlers.go`
- `web/templates/components/whatif/portfolio-settings.html`
- `web/templates/components/whatif/quick-adjust.html`
- `docs/audit/whatif-math-audit-2026-05-05.md` (closing notes)

- [ ] **Step P-1: Confirm baseline is green and on `dev`.**

```bash
cd /home/darrell/bin/ai/budget2
git status
git rev-parse --abbrev-ref HEAD   # expect: dev
go build ./...
go test ./internal/services/retirement/... -count=1
```

Expected: clean tree, on `dev`, build OK, tests PASS. If anything is dirty or red, stop and surface it before proceeding.

- [ ] **Step P-2: Create the working branch.**

```bash
git checkout -b feat/rmd-audit-followup
```

- [ ] **Step P-3: GitNexus impact-analysis on each symbol you'll touch.** Per `CLAUDE.md`, this is mandatory before editing any function. Run these calls (paste outputs into commit messages so the campaign is traceable):

  - `gitnexus_impact({target: "reinvestRequiredRMDToTaxableState", direction: "upstream"})`
  - `gitnexus_impact({target: "executePortfolioCashFlowWithTaxableState", direction: "upstream"})`
  - `gitnexus_impact({target: "RMDStartAge", direction: "upstream"})`
  - `gitnexus_impact({target: "EffectiveRMDStartAge", direction: "upstream"})`
  - `gitnexus_impact({target: "CalculateRMD", direction: "upstream"})`

Stop and ask the user before proceeding if any returns CRITICAL risk. MEDIUM/HIGH may proceed but must be called out in PR descriptions.

---

## PR 1 — F-073: Surplus RMD must be reported as gross taxable distribution

**Audit reference:** Extends F-049. F-049 fixed the *basis* of the after-tax deposit but did not fix the *return value* of `reinvestRequiredRMDToTaxableState`. Callers assign that net return into both `RMDWithdrawal` and `WithdrawalFromTaxDeferred`. Downstream tax math (`grossIncome` at calculator.go:1252, tax snapshots, RMD analysis aggregation) reads `WithdrawalFromTaxDeferred` as a gross taxable distribution — so every surplus-RMD month understates ordinary income, federal/state tax, MAGI (→ IRMAA), and the RMD analysis "actual withdrawn" total by exactly `gross × marginalRate`.

**Files:**
- Modify: `internal/services/retirement/calculator.go:776-801` (function signature)
- Modify: `internal/services/retirement/calculator.go:849-859` (callers)
- Modify: `internal/services/retirement/taxable_simulation_test.go:109-159` (existing F-049 tests use the return value)
- Test (new): `internal/services/retirement/calculator_rmd_gross_test.go`
- Modify: `docs/audit/whatif-math-audit-2026-05-05.md` (closing note)

### Step 1: Write the failing unit test for the surplus-path field assignment

Create `internal/services/retirement/calculator_rmd_gross_test.go`:

```go
package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// F-073: when the RMD is forced but cash is not needed (surplus path),
// the resulting cashFlow.RMDWithdrawal and cashFlow.WithdrawalFromTaxDeferred
// must reflect the GROSS distribution. The taxable-account deposit (basis)
// is correctly net (F-049), but the reported distribution drives downstream
// tax/MAGI math and must remain gross.
func TestExecutePortfolioCashFlow_F073_SurplusRMDReportedGross(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := newTaxableAccountState(s, 0)
	taxDeferred := 1_000_000.0
	rothBalance := 0.0
	monthlyRMD := 5_000.0
	marginalRate := 0.22

	result := executePortfolioCashFlowWithTaxableState(
		0.0, // neededFromPortfolio == 0 → surplus path (else-branch at line 853)
		monthlyRMD,
		true,        // allowTaxDeferred
		0.0,         // earlyPenaltyRate
		marginalRate,
		&taxDeferred,
		&taxable,
		&rothBalance,
	)

	if math.Abs(result.RMDWithdrawal-monthlyRMD) > 0.01 {
		t.Errorf("result.RMDWithdrawal = %.2f; want %.2f (gross)", result.RMDWithdrawal, monthlyRMD)
	}
	if math.Abs(result.WithdrawalFromTaxDeferred-monthlyRMD) > 0.01 {
		t.Errorf("result.WithdrawalFromTaxDeferred = %.2f; want %.2f (gross)", result.WithdrawalFromTaxDeferred, monthlyRMD)
	}

	// F-049 contract preserved: taxable account got NET deposit and basis.
	wantNet := monthlyRMD * (1 - marginalRate) // 3,900
	if math.Abs(taxable.MarketValue-wantNet) > 0.01 {
		t.Errorf("taxable.MarketValue = %.2f; want %.2f (net deposit)", taxable.MarketValue, wantNet)
	}
	if math.Abs(taxable.CostBasis-wantNet) > 0.01 {
		t.Errorf("taxable.CostBasis = %.2f; want %.2f (net basis)", taxable.CostBasis, wantNet)
	}

	// Tax-deferred decremented by GROSS (legal distribution).
	wantTaxDeferred := 1_000_000.0 - monthlyRMD
	if math.Abs(taxDeferred-wantTaxDeferred) > 0.01 {
		t.Errorf("taxDeferred = %.2f; want %.2f (decremented by gross)", taxDeferred, wantTaxDeferred)
	}
}

// F-073: same gross-reporting requirement on the partial-shortfall path
// (cash needed but RMD exceeds what was used to satisfy expenses).
func TestExecutePortfolioCashFlow_F073_PartialShortfallSurplusReportedGross(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := newTaxableAccountState(s, 0)
	taxable.MarketValue = 50_000
	taxable.CostBasis = 50_000
	taxDeferred := 1_000_000.0
	rothBalance := 0.0
	monthlyRMD := 5_000.0
	marginalRate := 0.22
	needed := 1_000.0 // small need; RMD will satisfy it and have surplus

	result := executePortfolioCashFlowWithTaxableState(
		needed,
		monthlyRMD,
		true,
		0.0,
		marginalRate,
		&taxDeferred,
		&taxable,
		&rothBalance,
	)

	// withdrawForExpenses uses RMD first to satisfy `needed`, so 1,000 of the
	// 5,000 RMD goes to expenses (gross). Remaining 4,000 is surplus — it
	// must be reinvested and reported as GROSS in the two fields.
	if math.Abs(result.RMDWithdrawal-monthlyRMD) > 0.01 {
		t.Errorf("result.RMDWithdrawal = %.2f; want %.2f (gross sum: 1000 used + 4000 surplus)", result.RMDWithdrawal, monthlyRMD)
	}
	if math.Abs(result.WithdrawalFromTaxDeferred-monthlyRMD) > 0.01 {
		t.Errorf("result.WithdrawalFromTaxDeferred = %.2f; want %.2f (gross)", result.WithdrawalFromTaxDeferred, monthlyRMD)
	}
}
```

- [ ] **Step 2: Run the new tests; expect FAIL.**

```bash
go test ./internal/services/retirement/ -run "F073" -v -count=1
```

Expected: both tests FAIL. The current return value is net (`5000 * 0.78 = 3900`) so `RMDWithdrawal` will be `3900`, not `5000`.

- [ ] **Step 3: Change the function signature to return both gross and net.**

In `internal/services/retirement/calculator.go`, replace the entire `reinvestRequiredRMDToTaxableState` function (currently calculator.go:776-801):

```go
// reinvestRequiredRMDToTaxableState moves an RMD from tax-deferred into the
// taxable account, with the after-tax amount as new basis. The pre-tax
// amount is decremented from tax-deferred (the gross RMD is the legal
// distribution); the after-tax portion (gross × (1 - marginalRate)) is
// added to the taxable account with that net amount as cost basis. Returns
// (gross, net) — gross is the IRS distribution amount that callers must
// report as ordinary taxable income; net is the cash actually deposited into
// the taxable account.
//
// F-049: prior implementation used gross as both reinvested amount and
// basis, which silently understated future LTCG on later withdrawals.
// F-073: prior implementation returned only the net amount; callers
// stored that net into RMDWithdrawal and WithdrawalFromTaxDeferred, which
// understated ordinary income, taxes, MAGI, and RMD-analysis totals by
// exactly gross × marginalRate every surplus-RMD month.
func reinvestRequiredRMDToTaxableState(monthlyRMD, marginalRate float64, taxDeferredBalance *float64, taxable *taxableAccountState) (gross, net float64) {
	if monthlyRMD <= 0 || *taxDeferredBalance <= 0 {
		return 0, 0
	}
	if marginalRate < 0 {
		marginalRate = 0
	}
	if marginalRate > 1 {
		marginalRate = 1
	}

	gross = math.Min(monthlyRMD, *taxDeferredBalance)
	*taxDeferredBalance -= gross
	net = gross * (1 - marginalRate)
	taxable.addCash(net)
	return gross, net
}
```

- [ ] **Step 4: Update both callers in `executePortfolioCashFlowWithTaxableState` to use `gross`.**

In `internal/services/retirement/calculator.go`, replace lines 847-860 (the `unmetRMD` block and the surplus `else` branch):

```go
		unmetRMD := monthlyRMD - withdrawal.RMDWithdrawal
		if unmetRMD > 0 {
			gross, _ := reinvestRequiredRMDToTaxableState(unmetRMD, marginalRate, taxDeferredBalance, taxable)
			result.RMDWithdrawal += gross
			result.WithdrawalFromTaxDeferred += gross
		}
	} else {
		if neededFromPortfolio < 0 {
			taxable.addCash(math.Abs(neededFromPortfolio))
		}
		gross, _ := reinvestRequiredRMDToTaxableState(monthlyRMD, marginalRate, taxDeferredBalance, taxable)
		result.RMDWithdrawal = gross
		result.WithdrawalFromTaxDeferred += gross
	}
```

- [ ] **Step 5: Update the existing F-049 tests to take both return values.**

In `internal/services/retirement/taxable_simulation_test.go`, replace the three F-049 test bodies (lines 109-159) with versions that destructure `(gross, net)`:

```go
// F-049 + F-073: reinvestRequiredRMDToTaxableState reinvests the after-tax
// portion as basis (F-049) and returns the gross distribution amount that
// callers report as taxable income (F-073).
func TestReinvestRequiredRMD_F049_BasisIsAfterTax(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := newTaxableAccountState(s, 0)
	taxDeferred := 100000.0
	rmd := 10000.0
	marginalRate := 0.22 // 22%

	gross, net := reinvestRequiredRMDToTaxableState(rmd, marginalRate, &taxDeferred, &taxable)
	wantGross := 10000.0
	wantNet := 7800.0

	if math.Abs(gross-wantGross) > 0.01 {
		t.Errorf("gross = %.2f; want %.2f", gross, wantGross)
	}
	if math.Abs(net-wantNet) > 0.01 {
		t.Errorf("net = %.2f; want %.2f", net, wantNet)
	}
	if math.Abs(taxable.MarketValue-wantNet) > 0.01 {
		t.Errorf("taxable.MarketValue = %.2f; want %.2f", taxable.MarketValue, wantNet)
	}
	if math.Abs(taxable.CostBasis-wantNet) > 0.01 {
		t.Errorf("taxable.CostBasis = %.2f; want %.2f", taxable.CostBasis, wantNet)
	}
	if math.Abs(taxDeferred-90000.0) > 0.01 {
		t.Errorf("taxDeferred remaining = %.2f; want 90000", taxDeferred)
	}
}

func TestReinvestRequiredRMD_F049_ZeroMarginalRate(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := newTaxableAccountState(s, 0)
	taxDeferred := 100000.0
	gross, net := reinvestRequiredRMDToTaxableState(10000, 0.0, &taxDeferred, &taxable)
	if math.Abs(gross-10000.0) > 0.01 {
		t.Errorf("zero-marginal gross = %.2f; want 10000", gross)
	}
	if math.Abs(net-10000.0) > 0.01 {
		t.Errorf("zero-marginal net = %.2f; want 10000", net)
	}
}

func TestReinvestRequiredRMD_F049_MarginalRateClamped(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := newTaxableAccountState(s, 0)
	taxDeferred := 100000.0
	gross, net := reinvestRequiredRMDToTaxableState(10000, 1.5, &taxDeferred, &taxable)
	if math.Abs(gross-10000.0) > 0.01 {
		t.Errorf("marginal>1 gross = %.2f; want 10000 (gross unchanged by rate clamp)", gross)
	}
	if math.Abs(net-0.0) > 0.01 {
		t.Errorf("marginal>1 net = %.2f; want 0", net)
	}
	taxDeferred = 100000.0
	taxable = newTaxableAccountState(s, 0)
	gross, net = reinvestRequiredRMDToTaxableState(10000, -0.5, &taxDeferred, &taxable)
	if math.Abs(gross-10000.0) > 0.01 {
		t.Errorf("marginal<0 gross = %.2f; want 10000", gross)
	}
	if math.Abs(net-10000.0) > 0.01 {
		t.Errorf("marginal<0 net = %.2f; want 10000", net)
	}
}
```

- [ ] **Step 6: Run all retirement tests; expect PASS.**

```bash
go test ./internal/services/retirement/ -count=1
```

Expected: PASS, including the new F-073 tests and the rewritten F-049 tests. If any unrelated retirement test now fails, that test was depending on the buggy net-as-gross behavior — surface it for review before changing it.

- [ ] **Step 7: Run the full test suite to catch downstream consumers.**

```bash
go test ./... -count=1
```

Expected: PASS. If anything in `internal/handlers/whatif` or backtest fails, those tests were also pinning the bug; investigate per failure.

- [ ] **Step 8: Mark F-073 in audit doc and commit.**

Append a closing note to `docs/audit/whatif-math-audit-2026-05-05.md` (use the existing F-072 closure block as the template; add a new F-073 entry that references this PR).

```bash
git add internal/services/retirement/calculator.go \
        internal/services/retirement/taxable_simulation_test.go \
        internal/services/retirement/calculator_rmd_gross_test.go \
        docs/audit/whatif-math-audit-2026-05-05.md
git commit -m "$(cat <<'EOF'
fix(whatif): F-073 report surplus RMD as gross taxable distribution

reinvestRequiredRMDToTaxableState now returns (gross, net). Callers in
executePortfolioCashFlowWithTaxableState use the gross value for both
RMDWithdrawal and WithdrawalFromTaxDeferred, restoring correct ordinary
income, federal/state tax, MAGI/IRMAA, and RMD-analysis totals during
surplus-RMD years. F-049 basis contract (taxable deposit = after-tax) is
preserved unchanged.

Closes F-073. Extends F-049.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 9: Run `gitnexus_detect_changes()`** and confirm the affected scope matches expectations (calculator, retirement tests, audit doc only).

---

## PR 2 — F-074: Apply RMDTiming setting to projection, Monte Carlo, and backtest

**Audit reference:** Extends F-035. The RMDTiming model field, normalizer, form parser, settings storage, legacy migration, and UI dropdown all exist. The projection engine never reads it: `monthlyRMD = annualRMD / 12` at year start and the same monthly amount is withdrawn every month regardless of `s.RMDTiming`. Start/mid/end-of-year all produce identical projections — the user-facing control is a lie.

**Files:**
- Modify: `internal/services/retirement/rmd.go` (add helper)
- Modify: `internal/services/retirement/calculator.go:1101-1108` (projection year-boundary)
- Modify: `internal/services/retirement/calculator.go:1212-1233` (projection month loop — pass per-month monthlyRMD)
- Modify: `internal/services/retirement/calculator.go:2402-2408` (Monte Carlo year-boundary)
- Modify: `internal/services/retirement/backtest.go:244-250` (backtest year-boundary)
- Test (new): `internal/services/retirement/rmd_timing_test.go`
- Modify: `docs/audit/whatif-math-audit-2026-05-05.md`

### Step 1: Add the trigger-month helper

Add this to `internal/services/retirement/rmd.go` (near the existing `EffectiveRMDStartAge`, around line 25):

```go
// rmdTriggerMonth returns the month-of-year (0-11) at which the full
// annual RMD is withdrawn for the given timing. F-074: the projection
// applies the entire year's RMD as a single monthly amount in the trigger
// month and zero in the others, so user-selected timing actually shapes
// portfolio growth (early withdrawal = more years lost to growth drag).
func rmdTriggerMonth(timing models.RMDTiming) int {
	switch models.NormalizeRMDTiming(timing) {
	case models.RMDTimingStartOfYear:
		return 0
	case models.RMDTimingEndOfYear:
		return 11
	default:
		// RMDTimingMidYear and any unknown value
		return 6
	}
}
```

### Step 2: Write failing tests

Create `internal/services/retirement/rmd_timing_test.go`:

```go
package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// F-074: rmdTriggerMonth maps each timing to a single month-of-year.
func TestRMDTriggerMonth_F074_AllTimings(t *testing.T) {
	cases := []struct {
		timing models.RMDTiming
		want   int
	}{
		{models.RMDTimingStartOfYear, 0},
		{models.RMDTimingMidYear, 6},
		{models.RMDTimingEndOfYear, 11},
		{models.RMDTiming(""), 6}, // empty → mid-year (matches NormalizeRMDTiming default)
	}
	for _, c := range cases {
		if got := rmdTriggerMonth(c.timing); got != c.want {
			t.Errorf("rmdTriggerMonth(%q) = %d; want %d", c.timing, got, c.want)
		}
	}
}

// F-074: end_of_year timing produces a higher year-end portfolio than
// start_of_year because tax-deferred grows on the full balance for 11
// months before the RMD haircut. Run a 1-year projection with no other
// expenses/income, age 73 (RMD active).
func TestProjection_F074_TimingAffectsYearEndBalance(t *testing.T) {
	make := func(timing models.RMDTiming) *Calculator {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 73
		s.SpouseAge = 0
		s.PortfolioValue = 1_000_000
		s.TaxDeferredPercent = 100
		s.RothPercent = 0
		s.TaxablePercent = 0
		s.MonthlyLivingExpenses = 0
		s.HealthcareCosts = 0
		s.SocialSecurityIncome = 0
		s.SpouseSocialSecurityIncome = 0
		s.PensionIncome = 0
		s.SpousePensionIncome = 0
		s.PartTimeIncome = 0
		s.InvestmentReturn = 7
		s.InflationRate = 0
		s.SpendingDeclineRate = 0
		s.ProjectionYears = 1
		s.RMDTiming = timing
		s.StartDate = "2026-01"
		return NewCalculator(s)
	}

	startProj := make(models.RMDTimingStartOfYear).CalculateProjection()
	midProj := make(models.RMDTimingMidYear).CalculateProjection()
	endProj := make(models.RMDTimingEndOfYear).CalculateProjection()

	if startProj == nil || midProj == nil || endProj == nil {
		t.Fatal("nil projection")
	}

	// month 11 = December of year 1
	startEOY := startProj.Months[11].PortfolioBalance
	midEOY := midProj.Months[11].PortfolioBalance
	endEOY := endProj.Months[11].PortfolioBalance

	if !(endEOY > midEOY && midEOY > startEOY) {
		t.Errorf("expected end_of_year > mid_year > start_of_year; got start=%.2f mid=%.2f end=%.2f",
			startEOY, midEOY, endEOY)
	}

	// All three must withdraw the same total annual RMD (year-start balance ÷ 26.5 at age 73).
	annualRMD, _ := CalculateRMD(1_000_000, 73)
	for _, c := range []struct {
		name string
		proj *models.ProjectionResult
	}{
		{"start_of_year", startProj},
		{"mid_year", midProj},
		{"end_of_year", endProj},
	} {
		var total float64
		for m := 0; m < 12; m++ {
			total += c.proj.Months[m].RMDWithdrawal
		}
		if math.Abs(total-annualRMD) > 1.0 { // ±$1 for float
			t.Errorf("%s: sum(RMDWithdrawal) over year = %.2f; want %.2f", c.name, total, annualRMD)
		}
	}
}

// F-074: each timing pins the withdrawal to exactly one month within the year.
func TestProjection_F074_TriggerMonthIsExclusive(t *testing.T) {
	cases := []struct {
		timing  models.RMDTiming
		trigger int
	}{
		{models.RMDTimingStartOfYear, 0},
		{models.RMDTimingMidYear, 6},
		{models.RMDTimingEndOfYear, 11},
	}
	for _, c := range cases {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 73
		s.PortfolioValue = 1_000_000
		s.TaxDeferredPercent = 100
		s.MonthlyLivingExpenses = 0
		s.InvestmentReturn = 7
		s.InflationRate = 0
		s.ProjectionYears = 1
		s.RMDTiming = c.timing
		s.StartDate = "2026-01"
		proj := NewCalculator(s).CalculateProjection()
		if proj == nil {
			t.Fatalf("%s: nil projection", c.timing)
		}
		for m := 0; m < 12; m++ {
			rmd := proj.Months[m].RMDWithdrawal
			if m == c.trigger {
				if rmd <= 0 {
					t.Errorf("%s: month %d (trigger) RMDWithdrawal = %.2f; want > 0", c.timing, m, rmd)
				}
			} else if rmd != 0 {
				t.Errorf("%s: month %d RMDWithdrawal = %.2f; want 0 (only trigger month %d should withdraw)",
					c.timing, m, rmd, c.trigger)
			}
		}
	}
}
```

- [ ] **Step 3: Run the new tests; expect FAIL.**

```bash
go test ./internal/services/retirement/ -run "F074" -v -count=1
```

Expected: `TestRMDTriggerMonth_F074_AllTimings` PASSES (helper exists), the two `TestProjection_F074_*` tests FAIL — current behavior spreads RMD evenly across all 12 months, so trigger-month exclusivity and end > mid > start ordering both fail.

- [ ] **Step 4: Wire `RMDTiming` into the projection's month loop.**

In `internal/services/retirement/calculator.go`, replace the year-boundary RMD block (currently lines 1101-1108):

```go
			// F-074: compute annualRMD once per year on year-start tax-deferred
			// balance (matches IRS "December 31 prior year" rule). Per-month
			// monthlyRMD is set inside the month loop based on RMDTiming.
			if olderAge >= RMDStartAge && taxDeferredBalance > 0 {
				annualRMD, _ = CalculateRMD(taxDeferredBalance, olderAge)
			} else {
				annualRMD = 0
			}
			monthlyRMD = 0
```

Then, immediately before the `monthResult := executeTaxAwarePortfolioMonth(` call (currently at line 1212), insert:

```go
		// F-074: apply the full annual RMD only in the trigger month for
		// the user's selected timing. Other months withdraw 0.
		monthlyRMD = 0
		if annualRMD > 0 && monthInYear == rmdTriggerMonth(s.RMDTiming) {
			monthlyRMD = math.Min(annualRMD, taxDeferredBalance)
		}

```

> **Note for the implementing agent:** verify `monthInYear` is in scope at the insertion point. If the variable name is `m % 12` or similar, use that. The existing code at calculator.go:1228 uses `monthInYear` — confirm it equals `m % 12` at line ~1212. If not, compute it inline: `monthOfYear := m % 12`.

- [ ] **Step 5: Mirror the same change in Monte Carlo (calculator.go:2402-2408) and backtest (backtest.go:244-250).**

In `internal/services/retirement/calculator.go`, replace lines 2402-2408 (Monte Carlo year-boundary):

```go
			// F-074: see PR 2 — annualRMD computed once per year, applied
			// only in the trigger month inside the month loop.
			if olderAge >= RMDStartAge && taxDeferredBalance > 0 {
				annualRMD, _ = CalculateRMD(taxDeferredBalance, olderAge)
			} else {
				annualRMD = 0
			}
			monthlyRMD = 0
```

Then locate the per-month execute call inside the Monte Carlo loop (in the same function, after line 2428) and insert the same `monthlyRMD = 0; if annualRMD > 0 && m%12 == rmdTriggerMonth(s.RMDTiming) { monthlyRMD = math.Min(annualRMD, taxDeferredBalance) }` block immediately before it.

> **Note:** the Monte Carlo function declares `annualRMD` locally inside the inner block at line 2404 (`annualRMD, _ := CalculateRMD(...)`). When you hoist computation out of the conditional, declare `var annualRMD float64` near the top of the per-year scope so it persists across months.

In `internal/services/retirement/backtest.go`, replace lines 244-250:

```go
			// F-074: see PR 2 — annualRMD computed once per year, applied
			// only in the trigger month inside the month loop.
			if olderAge >= RMDStartAge && taxDeferredBalance > 0 {
				annualRMD, _ = CalculateRMD(taxDeferredBalance, olderAge)
			} else {
				annualRMD = 0
			}
			monthlyRMD = 0
```

And insert the trigger-month block before the per-month execute call inside backtest's loop, identical pattern.

> **Note:** backtest.go also needs `var annualRMD float64` hoisted to the per-year scope if it was declared inside the conditional. Check existing scope and adjust.

- [ ] **Step 6: Run the new tests; expect PASS.**

```bash
go test ./internal/services/retirement/ -run "F074" -v -count=1
```

Expected: all three F-074 tests PASS.

- [ ] **Step 7: Run all retirement tests; expect PASS or surface any pre-existing test that pinned the legacy uniform-spread behavior.**

```bash
go test ./internal/services/retirement/ -count=1
```

If a pre-existing test fails because it baked in a per-month RMD value, the test is wrong (it pinned the bug). Update the test to assert on annual totals or trigger-month behavior, document the change in the test comment, and re-run.

- [ ] **Step 8: Run the full suite.**

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 9: Mark F-074 in audit doc and commit.**

```bash
git add internal/services/retirement/rmd.go \
        internal/services/retirement/calculator.go \
        internal/services/retirement/backtest.go \
        internal/services/retirement/rmd_timing_test.go \
        docs/audit/whatif-math-audit-2026-05-05.md
git commit -m "$(cat <<'EOF'
fix(whatif): F-074 honor RMDTiming setting in projection, MC, and backtest

The RMDTiming field (start_of_year/mid_year/end_of_year) was previously
saved, rendered, and migrated but never read by the projection engine —
all timings produced identical projections with annual RMD spread evenly
across 12 months. Projection, Monte Carlo, and historical backtest now
compute annualRMD once at year boundary (year-start balance, matches IRS
rule) and apply the full amount in a single trigger month: 0/6/11 for
start/mid/end-of-year. Annual totals match CalculateRMD; portfolio growth
trajectory now responds to the dropdown.

Closes F-074. Extends F-035.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 10: Run `gitnexus_detect_changes()`** and confirm scope (rmd.go, calculator.go, backtest.go, new test).

---

## PR 3 — F-075: Projection and event timeline must use `EffectiveRMDStartAge`

**Audit reference:** Extends F-032. `BuildRMDAnalysis` correctly calls `EffectiveRMDStartAge(s)` (returns 75 for `StartDate ≥ 2033`, else 73), but every projection-side gate still uses the constant `RMDStartAge = 73`. For 2033+ scenarios the panel says "RMDs start at 75" while the projection actually withdraws at 73, and the Whatif event-timeline label uses 73. Six call sites need updating.

**Call sites (verified by grep):**
- `internal/services/retirement/calculator.go:1102` — main projection year-boundary
- `internal/services/retirement/calculator.go:1453` — budget-fit RMD calculation
- `internal/services/retirement/calculator.go:1569` — steady-state RMD
- `internal/services/retirement/calculator.go:1588` — IRMAA lookback RMD
- `internal/services/retirement/calculator.go:2403` — Monte Carlo year-boundary
- `internal/services/retirement/backtest.go:245` — backtest year-boundary
- `internal/handlers/whatif/handlers.go:219-220` — event timeline label

**Files:**
- Modify: all of the above
- Test (new): `internal/services/retirement/rmd_start_age_projection_test.go`
- Test (modify): `internal/handlers/whatif/handlers_test.go` (or wherever the event timeline is tested) — only if existing test pins age 73
- Modify: `docs/audit/whatif-math-audit-2026-05-05.md`

### Step 1: Write failing tests

Create `internal/services/retirement/rmd_start_age_projection_test.go`:

```go
package retirement

import (
	"testing"

	"budget2/internal/models"
)

// F-075: a projection starting in 2033 with the older spouse at age 73
// must NOT trigger RMD that year — SECURE 2.0 raises the start age to 75
// for distributions taken in 2033 or later.
func TestProjection_F075_2033StartAge73NoRMD(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 73
	s.SpouseAge = 0
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 1
	s.StartDate = "2033-01"
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := NewCalculator(s).CalculateProjection()
	if proj == nil || len(proj.Months) < 12 {
		t.Fatal("nil/short projection")
	}
	for m := 0; m < 12; m++ {
		if proj.Months[m].RMDWithdrawal != 0 {
			t.Errorf("month %d RMDWithdrawal = %.2f; want 0 (age 73 in 2033 → no RMD per SECURE 2.0)",
				m, proj.Months[m].RMDWithdrawal)
		}
	}
}

// F-075: a projection starting in 2033 with the older spouse at age 75
// MUST trigger RMD that year (effective start age is 75 for 2033+).
func TestProjection_F075_2033StartAge75DoesRMD(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 75
	s.SpouseAge = 0
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 1
	s.StartDate = "2033-01"
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := NewCalculator(s).CalculateProjection()
	if proj == nil || len(proj.Months) < 12 {
		t.Fatal("nil/short projection")
	}
	var total float64
	for m := 0; m < 12; m++ {
		total += proj.Months[m].RMDWithdrawal
	}
	if total <= 0 {
		t.Errorf("annual RMDWithdrawal = %.2f; want > 0 (age 75 in 2033 must trigger RMD)", total)
	}
}

// F-075: pre-2033 scenarios still trigger RMD at age 73 (legacy behavior).
func TestProjection_F075_2026StartAge73DoesRMD(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 73
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 1
	s.StartDate = "2026-01"
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := NewCalculator(s).CalculateProjection()
	if proj == nil {
		t.Fatal("nil projection")
	}
	var total float64
	for m := 0; m < 12; m++ {
		total += proj.Months[m].RMDWithdrawal
	}
	if total <= 0 {
		t.Errorf("annual RMDWithdrawal = %.2f; want > 0 (pre-2033 age 73 must trigger)", total)
	}
}
```

- [ ] **Step 2: Run the new tests; expect the 2033/age-73 case to FAIL** (it currently fires RMD because projection still uses constant 73), age-75 PASSES, age-73 pre-2033 PASSES.

```bash
go test ./internal/services/retirement/ -run "F075" -v -count=1
```

- [ ] **Step 3: Replace `RMDStartAge` with `EffectiveRMDStartAge(s)` at every projection site.**

In `internal/services/retirement/calculator.go`:

Line 1102 — replace:
```go
			if olderAge >= RMDStartAge && taxDeferredBalance > 0 {
```
with:
```go
			if olderAge >= EffectiveRMDStartAge(s) && taxDeferredBalance > 0 {
```

Line 1453 — replace:
```go
	if olderAge >= RMDStartAge && s.TaxDeferredPercent > 0 {
```
with:
```go
	if olderAge >= EffectiveRMDStartAge(s) && s.TaxDeferredPercent > 0 {
```

Line 1569 — replace:
```go
		if steadyStateOlderAge >= RMDStartAge && s.TaxDeferredPercent > 0 {
```
with:
```go
		if steadyStateOlderAge >= EffectiveRMDStartAge(s) && s.TaxDeferredPercent > 0 {
```

Line 1588 — replace:
```go
			if lookbackOlderAge >= RMDStartAge && s.TaxDeferredPercent > 0 {
```
with:
```go
			if lookbackOlderAge >= EffectiveRMDStartAge(s) && s.TaxDeferredPercent > 0 {
```

Line 2403 — replace:
```go
			if olderAge >= RMDStartAge && taxDeferredBalance > 0 {
```
with:
```go
			if olderAge >= EffectiveRMDStartAge(s) && taxDeferredBalance > 0 {
```

In `internal/services/retirement/backtest.go` line 245 — replace:
```go
			if olderAge >= RMDStartAge && taxDeferredBalance > 0 {
```
with:
```go
			if olderAge >= EffectiveRMDStartAge(s) && taxDeferredBalance > 0 {
```

In `internal/handlers/whatif/handlers.go` lines 219-220 — replace:
```go
	if olderAge < retirement.RMDStartAge {
		appendEvent(float64(retirement.RMDStartAge-olderAge), "RMD starts")
	}
```
with:
```go
	effectiveStart := retirement.EffectiveRMDStartAge(settings)
	if olderAge < effectiveStart {
		appendEvent(float64(effectiveStart-olderAge), "RMD starts")
	}
```

> **Important:** do NOT delete the `RMDStartAge` constant. The test at `internal/services/retirement/calculator_expense_test.go:728` references it as `// Above RMDStartAge (73)` documentation, and removing it could break compatibility for any scenario reader depending on the symbol. Leave the constant in place as the legal floor.

### Step 4: Add an event-timeline test for the handler change

Append to `internal/handlers/whatif/handlers_test.go` (or whichever file tests `buildEventTimeline` — confirm via `grep -rn "RMD starts" internal/handlers/whatif/`):

```go
// F-075: event-timeline "RMD starts" label uses EffectiveRMDStartAge so
// 2033+ scenarios show 75 - olderAge instead of 73 - olderAge.
func TestBuildEventTimeline_F075_RMDStartsUsesEffectiveAge(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge: 70,
		StartDate:  "2033-01",
	}
	events := buildEventTimeline(s) // adjust to actual function name and signature

	var found bool
	for _, ev := range events {
		if ev.Label == "RMD starts" {
			found = true
			if ev.Year != 5 { // 75 - 70 = 5
				t.Errorf("RMD starts year = %v; want 5 (2033+ effective start age 75 minus current 70)", ev.Year)
			}
		}
	}
	if !found {
		t.Error("RMD starts event not found in timeline")
	}
}
```

> **Note for the implementing agent:** check the actual exported function name and signature for `buildEventTimeline`. If it's unexported, place this test in the same package; otherwise adjust accordingly. If the test infrastructure differs (table-driven, helper, etc.), match the surrounding style.

- [ ] **Step 5: Run the new tests and the full retirement+handlers suite.**

```bash
go test ./internal/services/retirement/ ./internal/handlers/whatif/ -count=1
```

Expected: PASS, including the three F-075 retirement tests and the new handler test.

- [ ] **Step 6: Run the full suite.**

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 7: Mark F-075 in audit doc and commit.**

```bash
git add internal/services/retirement/calculator.go \
        internal/services/retirement/backtest.go \
        internal/services/retirement/rmd_start_age_projection_test.go \
        internal/handlers/whatif/handlers.go \
        internal/handlers/whatif/handlers_test.go \
        docs/audit/whatif-math-audit-2026-05-05.md
git commit -m "$(cat <<'EOF'
fix(whatif): F-075 projection and event timeline use EffectiveRMDStartAge

BuildRMDAnalysis already used EffectiveRMDStartAge for the panel; the
projection engine, Monte Carlo, backtest, and Whatif event-timeline label
all still gated on the constant RMDStartAge=73. For projections starting
2033+, the panel said "RMDs start at 75" while the projection withdrew at
73 and the timeline showed "RMD starts in N years" computed from 73.
Replaced six call sites with EffectiveRMDStartAge(s); legacy RMDStartAge
constant retained as the legal floor.

Closes F-075. Extends F-032.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 8: Run `gitnexus_detect_changes()`** and confirm scope.

---

## PR 4 — F-076: Portfolio Value range dropdown stops resetting on selection

**User-reported bug:** "I can't change the dropdown. It always resets." (See screenshot in conversation.) Selecting a wider range from the Portfolio Value dropdown immediately snaps back to the bucket that contains the current `PortfolioValue`.

**Root cause:** `updatePortfolioRange(this.value, { sourceEvent: event })` (portfolio-settings.html:33) sets the slider's min/max/step then dispatches a synthetic `change` event on the slider (line 106). The form's `hx-trigger="change delay:500ms"` catches that event and POSTs to `/whatif/settings`. The form contains `portfolio_value=<unchanged>`, so the server re-renders with the dropdown's `selected` derived from the unchanged value — collapsing the user's range pick back to the value-derived bucket. The mirror dropdown in `quick-adjust.html:82` has the same bug.

**Scope (intentionally minimal):** stop the auto-submit triggered by dropdown selection. The dropdown then expands the slider client-side and waits for the user to actually drag. After the next genuine slider drag, the form submits with the new value and the server-rendered dropdown will match the new value's bucket. Cross-render persistence of the *empty-bucket* case (user picks a wider range but never drags) is out of scope; revisit only if the user reports it as a separate bug.

**Files:**
- Modify: `web/templates/components/whatif/portfolio-settings.html:33` (canonical dropdown)
- Modify: `web/templates/components/whatif/quick-adjust.html:82` (mirror dropdown)
- Test: manual smoke (no Go test asset; this is template/JS).

### Step 1: Update the canonical dropdown to skip auto-submit

In `web/templates/components/whatif/portfolio-settings.html`, replace line 33:

```html
                <select id="portfolio-range" onchange="updatePortfolioRange(this.value, { sourceEvent: event })"
```

with:

```html
                <select id="portfolio-range" onchange="updatePortfolioRange(this.value, { sourceEvent: event, triggerChange: false })"
```

### Step 2: Update the mirror dropdown to do the same

In `web/templates/components/whatif/quick-adjust.html`, replace line 82:

```html
            onchange="updatePortfolioRange(this.value)">
```

with:

```html
            onchange="updatePortfolioRange(this.value, { triggerChange: false })">
```

### Step 3: Verify the JS still triggers HTMX on actual slider drag

No code change required — the slider input has its own `oninput` handler, and `change` events fire natively when the user releases the slider. Confirm this by reading `web/templates/components/whatif/portfolio-settings.html` lines 44-48 and the form's `hx-trigger="change delay:500ms"` at line 27. The `triggerChange: false` flag only suppresses the *synthetic* event from `updatePortfolioRange`, not real user-driven slider events.

### Step 4: Manual smoke test

```bash
go build -o /tmp/budget2 ./cmd/budget2
/tmp/budget2 &
SERVER_PID=$!
sleep 2
echo "Open http://localhost:8080/whatif and verify:"
echo "  1. Portfolio value at \$100,000, dropdown shows \$0-100K."
echo "  2. Pick \$0-500K from the dropdown."
echo "     — slider's max should expand to 500,000."
echo "     — dropdown should STAY on \$0-500K (no snap-back)."
echo "     — no network request should fire (check DevTools network)."
echo "  3. Drag slider to \$300,000."
echo "     — form posts after 500ms."
echo "     — re-render: dropdown shows \$0-500K (300K is in that bucket)."
echo "  4. Repeat with the quick-adjust mirror dropdown — same behavior."
echo "When done: kill $SERVER_PID"
```

> **Note for the implementing agent:** if you have access to a browser MCP, automate this with `mcp__plugin_playwright_playwright__browser_navigate` + `browser_select_option` + `browser_snapshot`. Otherwise, ask the user to verify the smoke once the change is staged.

- [ ] **Step 5: Confirm Go build still passes** (templates compile at startup; if Go can't start, the template syntax is wrong).

```bash
go build ./...
go vet ./...
```

Expected: clean. If there's an existing Go template parsing test, run it too.

- [ ] **Step 6: Commit.**

```bash
git add web/templates/components/whatif/portfolio-settings.html \
        web/templates/components/whatif/quick-adjust.html \
        docs/audit/whatif-math-audit-2026-05-05.md
git commit -m "$(cat <<'EOF'
fix(whatif): F-076 portfolio range dropdown no longer snaps back

The Portfolio Value range dropdown was firing a synthetic slider 'change'
event after expanding min/max/step, which made HTMX submit the form with
the unchanged portfolio_value. The server then re-rendered the dropdown
with 'selected' derived from the unchanged value bucket — visually
collapsing the user's range pick. Both the canonical and mirror dropdowns
now pass triggerChange:false to updatePortfolioRange so dropdown selection
expands the slider client-side and waits for the user's actual drag.

Closes F-076.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Wrap-up

- [ ] **Step W-1:** Run the full test suite one more time on the final tip of `feat/rmd-audit-followup`.

```bash
go test ./... -count=1
```

- [ ] **Step W-2:** Open a PR per closure (or one stacked PR with the four commits, matching prior campaign style — confirm with the user before opening).

- [ ] **Step W-3:** Update the `2026-05-06-whatif-fixes-campaign.md` "outstanding follow-ups" section if it has one, noting F-073/F-074/F-075/F-076 are closed in this branch.

---

## Self-Review

| Spec finding | Plan task | Status |
|---|---|---|
| #1 surplus RMD recorded as net | PR 1 / F-073 | ✓ test + fix in calculator.go:776-801, 849-859 |
| #2 RMDTiming setting unused | PR 2 / F-074 | ✓ helper in rmd.go, applied in calculator.go (proj+MC) and backtest.go |
| #3 projection vs analysis start age | PR 3 / F-075 | ✓ six call sites + handlers.go event timeline |
| Portfolio dropdown reset | PR 4 / F-076 | ✓ both dropdowns updated, smoke test included |

**Placeholder scan:** none found. Every code block contains literal Go/HTML, every command has expected output described, every commit has a HEREDOC message.

**Type/symbol consistency:**
- `reinvestRequiredRMDToTaxableState` returns `(gross, net float64)` consistently across function definition (PR1 Step 3), both call sites (PR1 Step 4), and three rewritten F-049 tests (PR1 Step 5).
- `rmdTriggerMonth` defined in PR2 Step 1, used in PR2 Steps 4-5 (projection, MC, backtest).
- `EffectiveRMDStartAge(s)` already exists in `rmd.go:15`; PR3 only adds call sites.

**Cross-task ordering:** PR1 → PR2 → PR3 → PR4 is the safe order. PR2 doesn't touch the function PR1 changes; PR3 doesn't touch the timing logic PR2 adds; PR4 is fully independent. No `t.Skip` gating needed.

**Spec coverage gap:** none.
