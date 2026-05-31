# Code-Review Follow-ups — 2026-05-31

**Source:** Code review of branch `feat/ss-survivor-benefits`.
**Branch HEAD at review:** `5e10a48`.
**Status of the review's four findings:**

- **Finding #1 (ss.go `SpouseEarlyClaimGapPct`)** — real bug, **fixed** on
  `feat/ss-survivor-benefits` (see commit adding
  `TestSSAnalysis_SpouseEarlyClaimGap_SpouseAlreadyClaiming`). It was
  *pre-existing* (identical in `master`), not a regression, and contrary to the
  review **no test was deleted** — the branch only added survivor tests.
- **Findings #2/#3/#4 (below)** — the review framed these as regressions from
  "today's diff." **They are not.** All three touch files this branch never
  changed (`git diff master...HEAD` does not list them). They are real-or-debatable
  *pre-existing* issues on code unrelated to SS survivor benefits, originally
  deferred off this branch on 2026-05-31. Each has since been triaged and fixed
  with TDD on its own branch (see the per-item resolution notes below).

**Resolution summary (2026-05-31):**

| Item | Status | Branch / PR |
|------|--------|-------------|
| FU-1 (#4) IRMAA lookback not seeded in MC/backtest | **Resolved** | `feat/ss-survivor-benefits` (this branch) |
| FU-2 (#2) Monthly-stream PV annuity-due timing | **Resolved** | `fix/pv-annuity-timing`, PR #12 |
| FU-3 (#3) Bracket-fill ceilings not inflated | **Partially resolved** (inflation fixed; gross-ish estimator left by decision) | `fix/bracket-fill-inflate-ceilings`, PR #13 |

Each item below is verified against the actual code, not the review's framing.

---

## FU-1 (#4, Medium) — IRMAA lookback MAGI not seeded in Monte Carlo / backtest loops

> **RESOLVED (2026-05-31)** on `feat/ss-survivor-benefits`. Both loops now seed
> `AssumedIRMALookbackMAGI` from their own year-0 MAGI, mirroring
> `engine/month.go`. IRMAA is now observable per run via the new `TotalIRMAA`
> field on `models.MonteCarloResult` and `HistoricalSequenceResult`; regression
> tests `TestMonteCarlo_SeedsEarlyYearIRMAA` and `TestBacktest_SeedsEarlyYearIRMAA`
> (a 2-year high-MAGI Medicare household, where the whole run lives inside the
> seed window) assert non-zero early-year IRMAA.

**Files:** `internal/services/retirement/analysis/monte_carlo.go` (~line 512),
`internal/services/retirement/analysis/backtest.go` (~line 377).

**What the code does.** The canonical engine loop seeds the IRMAA two-year MAGI
lookback for projection years 0–1:

- `engine/month.go:286` passes `AssumedIRMALookbackMAGI: &assumedLookbackMAGI`,
  and `month.go` updates `assumedLookbackMAGI` from year-0 MAGI
  (`monthResult.TaxSnapshot.AnnualMAGI`).

The Monte Carlo and backtest loops build their own `PortfolioMonthInput` and
**do not** set `AssumedIRMALookbackMAGI` at all (the field is simply absent from
both struct literals).

**Effect.** For high-MAGI, Medicare-eligible households, MC/backtest report ~$0
IRMAA in the early plan years (no lookback seed) while the deterministic
projection charges it. The two diverge in years 0–1.

**Provenance.** `AssumedIRMALookbackMAGI` was introduced in commit `b3eb796`
("seed early IRMAA") and added **only** to `engine/month.go` — it was never in
`monte_carlo.go` / `backtest.go`. Pre-existing inconsistency, not a regression.

**Why this matters / CLAUDE.md note.** The project's own guidance flags exactly
this hazard: *"Three projection loops build `PortfolioMonthInput` independently
… Per-month tax/IRMAA input changes must be replicated across all three (or
centralized in `ExecuteTaxAwarePortfolioMonth`)."*

**Suggested fix.** Replicate the year-0 seed: declare an `assumedLookbackMAGI`
in each loop, set it from the year-0 MAGI snapshot, and pass
`AssumedIRMALookbackMAGI: &assumedLookbackMAGI` in both struct literals — or
better, centralize the seed so all three loops can't drift again.

**Effort/risk:** Low–moderate. Analysis package only; no signature changes.
Most contained of the three. **TDD:** add an MC (or backtest) test with a
high-MAGI Medicare-eligible household asserting non-zero early-year IRMAA
matching the deterministic projection.

---

## FU-2 (#2, High-ish) — Monthly-stream PV uses annuity-due timing; closed-form legs use ordinary-annuity timing

> **RESOLVED (2026-05-31)** on `fix/pv-annuity-timing` (off `master`), PR #12.
> Decided to standardize on the **ordinary-annuity** convention (align the
> stream to the closed-form legs, not vice-versa): `presentValueOfMonthlyStream`
> now discounts the month-*m* payment by `(1+r)^(m+1)`, matching
> `engine.PresentValueAnnuity` and the end-of-month projection default. This
> removes the spurious `(1+monthlyRate)` shift that appeared when toggling
> spending phases or the SS optimizer. Regression tests pin a constant-payment
> stream (with and without a start offset) to `PresentValueAnnuity`.

**File:** `internal/services/retirement/analysis/present_value.go` (~line 171,
`presentValueOfMonthlyStream`).

**What the code does.**

```go
for m := range months {
    ...
    pv += amt / math.Pow(1+monthlyRate, float64(m)) // m = 0 → exponent 0 → undiscounted "today"
}
```

This is an **annuity-due** convention: the first payment (m=0) is valued at
*today* with no discount.

By contrast `engine.PresentValueAnnuity` (`engine/math.go:61`,
`presentValueAnnuity`) uses the **ordinary-annuity** formula
`payment * (1 - (1+r)^-n) / r`, which values the first payment one month out
(discounted by `(1+r)^-1`). The `presentValueOfMonthlyStream` doc comment even
claims the two "stay consistent," but the timing differs by one month.

**Effect.** Stream-based legs (SS-optimizer income, taxes, phased expenses) are
discounted one month *less* than closed-form legs, slightly **overstating** their
PV relative to annuity legs. Magnitude is small per leg but systematic.

**Provenance.** `present_value.go` is **unchanged on this branch** (empty
`git diff master...HEAD`). The review's "Restore `m+1`" is incorrect — it was
never `m+1` here. This is a pre-existing design/timing inconsistency.

**Open question (decide before fixing).** Which convention is intended? Aligning
the stream to ordinary-annuity (`m+1`) makes it consistent with
`PresentValueAnnuity`, but the *correct* answer depends on whether these cash
flows occur at the start or end of the month in the projection model. Do **not**
flip this blindly.

**Effort/risk:** **High blast radius** — affects all of `BudgetFit` and
`PresentValue`. Needs a deliberate decision on timing convention plus regression
tests pinning PV of a known stream against a hand-computed oracle under the
chosen convention. Own branch.

---

## FU-3 (#3, debatable) — Bracket-fill `estimateOtherTaxableIncome` is a gross-ish approximation; bracket tops are not inflated

> **PARTIALLY RESOLVED (2026-05-31)** on `fix/bracket-fill-inflate-ceilings`
> (off `master`), PR #13. Fixed the **un-inflated bracket tops** only: new
> `inflatedBracketTop` scales the 2024 ceilings by
> `(1+InflationRate)^YearsFromTaxBase` (mirroring the engine's own bracket
> inflation), used in both the candidate gate and per-year sizing. The
> remaining approximations (gross vs taxable SS, no standard-deduction
> subtraction, rough 4% RMD) were **deliberately left as-is**: a measurement
> probe on a representative MFJ plan showed the ladder family beats the best
> bracket-fill candidate regardless, the optimizer re-ranks on the real engine
> projection, and those errors are conservative (they under-convert, never
> over-convert past a bracket). Improving them would duplicate engine tax logic
> for no change to the reported recommendation.

**File:** `internal/services/retirement/analysis/tax_optimizer_strategies.go`
(~line 108 `estimateOtherTaxableIncome`, ~line 396 `bracketTopFor` usage).

**What the code does.** `estimateOtherTaxableIncome` sums gross SS, fixed-income
sources (including SS-typed), qualified dividends, and a rough ~4% RMD. It does
**not** compute taxable-SS, subtract the standard deduction, use the SECURE 2.0
RMD start age, or the Uniform Lifetime divisor. Separately, the bracket-fill
`RothStrategyBracketFill` path uses `bracketTopFor(...)` with frozen 2024 bracket
tops for all future years (no inflation).

**Effect.** Bracket-fill conversion *candidate* sizing can be materially off,
especially later in the plan (un-inflated ceilings shrink in real terms; gross
units overstate taxable income).

**Important mitigating context.** The function's own doc says: *"Approximate by
design — the optimizer ranks on the engine's actual projection result, not on
this estimate."* The estimator only pre-sizes candidates; final ranking uses the
real tax-aware engine projection. So the practical impact is bounded to
candidate *generation*, not the reported result.

**Provenance.** `tax_optimizer_strategies.go` is **unchanged on this branch**
(empty `git diff master...HEAD`). The review's "regresses back to … Restore the
taxable-income estimator / age65CountForYear / inflated ceilings" describes code
that was not removed on this branch. This is a *feature/design* request, not a
regression fix.

**Suggested approach (if pursued).** Decide first whether the approximation is
good enough given the engine re-ranks. If improving: estimate taxable income
(taxable-SS + standard deduction), use SECURE 2.0 RMD age + Uniform Lifetime
divisor, and inflate bracket ceilings via the plan calendar year
(`engine.YearsFromTaxBase`, mirroring the tax/IRMAA inflation pattern). Lowest
priority of the three; own branch; TDD against worked examples.

---

## Note on the review framing

The original review labeled FU-1/2/3 as regressions and claimed a regression
test was deleted for #1. Neither is accurate against `git diff master...HEAD`:
the branch changed only `analysis/ss.go`, `ss_test.go`, `models/whatif.go`, and
the whatif UI templates, and added (never deleted) tests. Treat FU-1/2/3 as
independent pre-existing findings, prioritized FU-1 > FU-2 > FU-3.
