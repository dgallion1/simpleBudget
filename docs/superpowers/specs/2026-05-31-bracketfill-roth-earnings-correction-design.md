# Bracket-Fill Roth-Earnings Correction — Design

**Date:** 2026-05-31
**Status:** Approved (ready for implementation plan)
**Area:** `internal/services/retirement/analysis` (Roth tax optimizer)

## Problem

Bracket-fill Roth-conversion sizing (`analysis/tax_optimizer_strategies.go`) must
mirror every taxable ordinary-income component the engine fills the ordinary
bracket with (per CLAUDE.md). The engine's ordinary-income bucket is:

```
OrdinaryIncome + TaxableNonQualifiedDividends + TaxableRothEarnings   (loop_helpers.go:290)
+ WithdrawalFromTaxDeferred + RothConversions                         (projtax.go:99)
```

`bracketFillIncomeForYear` reproduces fixed income + RMD + non-qualified
dividends, but **omits `TaxableRothEarnings`** — the taxable portion of
non-qualified Roth *earnings* withdrawals (under 59½ / inside the 5-year window,
per `portfolio_month.go:399–446` and big-ticket Roth-funded earnings,
`month.go:282`). When that component is nonzero in a conversion year, the solver
under-counts ordinary income and **oversizes the conversion**, so the engine
faithfully pushes actual taxable ordinary income past the target bracket ceiling.

### Quantified impact (measured)

A throwaway harness sized real bracket-fill conversions, ran the engine, and read
back per-year `TaxableRothEarnings` (≈ the overshoot the estimator can't see):

| Scenario | Conversions | Overshoot |
|---|---|---|
| Realistic (ample Roth, 12%) | yes | **$0 every year** |
| Aggressive / extreme drain (Roth → $0) | yes | **$0** |
| Control: small Roth, **no** conversions | no | $7.3k–$10.6k (proves mechanism) |
| **Adversarial overlap: small Roth + tight bracket room + heavy spend** | yes | **$23,339 @ age 53**, $4,415 @ age 54 |

**Structural finding:** in normal scenarios the overshoot is *exactly* $0 — a
conversion injects fresh Roth **basis**, and Pub 590-B basis-first ordering
consumes that basis before any earnings, so the marginal Roth dollar is never
earnings. The overshoot appears only when a **small Roth is drained past
(basis + cumulative conversions) during the conversion window while under 59½**
(small Roth + tight bracket room so conversions can't replenish + heavy spending
+ a ~5-year tax-deferred bridge). Narrow, but realistic and configurable, and
when it fires it can blow through an entire bracket (the $23k overshoot ≈ the
full width of the 12% bracket). The 6-year-bridge variant fires identically, so
it is not an artifact of extreme settings.

### Why the obvious cheaper fixes fail in the corner that matters

- **Closed-form** (estimate the drawdown inside the solver): requires importing
  the engine's whole spending/withdrawal-waterfall model into a function built to
  avoid it — the "three projection loops" duplication CLAUDE.md warns against —
  and is wrong precisely in the messy cases.
- **Baseline engine-feedback** (read earnings from the no-conversion baseline):
  mis-estimates the corner. The no-conversion baseline drains the Roth with *less
  basis*, reaching earnings sooner and larger than the real with-conversion path.
  Earnings are conversion-dependent, so a baseline proxy over-corrects →
  under-converts.

Only an **iterative** correction that re-runs the engine *with the conversions in
place* is self-consistent.

## Design

### Core idea

Thread the engine's per-year `TaxableRothEarnings` back into the existing
closed-form solver as a **known ordinary-income component**, and loop
size → run → re-size until it stabilizes. The solver already does the bracket and
§86-SS math correctly; it only lacks that ordinary term, and the term can't be
computed up front because it depends on the conversions themselves. Iteration
resolves the circularity using the engine as ground truth.

### Loop (inside `scoreCandidate`, bracket-fill candidates only)

```
feedback := nil                               // pass 0 == today's behavior exactly
var best ...                                  // smallest-positive-overshoot iterate
for iter := 0; iter < maxIter; iter++ {
    cloned := cloneSettingsWithSSAndRoth(settings, ss, strat, feedback)
    proj   := eng.Run(cloned)
    earned := harvestRothEarnings(proj)        // proj-year -> TaxableRothEarnings, conversion years only
    track best by overshoot
    if maxAbsDelta(feedback, earned) < tol { converged; break }
    feedback = relax(feedback, earned)         // damped update
}
use converged (or best) feedback for the scored projection AND for disclosure
```

`feedback map[int]float64` (projection-year → Roth earnings) threads down through
`cloneSettingsWithSSAndRoth → rothStrategyToConfig → strategyYearlyConversions →
bracketFillIncomeForYear`, where it is **added into `ordinary`** so it both fills
the bracket and enters §86 provisional income — matching the engine.

### Critical safety property

In the common case, pass 0 yields zero earnings in every conversion year →
`earned` all zero → loop breaks after **one** engine run → behavior and cost are
**byte-identical to today**. Behavior changes only in the small-Roth-drain corner.

### Convergence handling

Coupling is mildly adversarial: shrinking a conversion removes basis, which can
*increase* the next pass's earnings (slope of earnings w.r.t. conversion ∈ [−1, 0]).
So:

- cap iterations (~4),
- damp the feedback update (relaxation factor; start near full, back off on
  oscillation),
- tolerance ~$50 (sub-dollar bracket precision is pointless),
- on non-convergence keep the iterate with the **smallest positive overshoot** —
  never fall back to the overshooting original.

Cross-year coupling (less converted → different drawdown in other years) is
captured automatically because every pass re-runs the whole engine and feeds back
*all* conversion years at once.

### Disclosure consistency

`scoreCandidate` re-derives `PerYearConversions` for display
(`tax_optimizer.go:250`). It must use the **converged** feedback, or the displayed
amounts won't match what the engine applied.

### Scope / blast radius

All changes are in the **analysis** package — **no engine changes**, because
`ProjectionYearSummary.TaxableRothEarnings` already exists and the optimizer
already holds the `ProjectionResult`.

Functions touched:

- `tax_optimizer_strategies.go`: `bracketFillIncomeForYear`, the `bracketFillIncome`
  struct + `taxableOrdinaryIncome`/`bracketFillConversion` methods,
  `strategyYearlyConversions`, `rothStrategyToConfig`.
- `tax_optimizer.go`: `cloneSettingsWithSSAndRoth`, `scoreCandidate`.

The candidate **gate** (`bracketFillProducesNonZero`, `enumerateBracketFillStrategies`)
keeps passing `feedback=nil` — it is a pre-engine heuristic and does not need
correcting. `incomingCalls` will be run on each touched function before editing
(CLAUDE.md mandate).

### Performance

Iteration adds engine runs **only** for bracket-fill candidates that actually hit
the corner. Ladder candidates and the common bracket-fill case stay at one run.
CPU is explicitly not a constraint here.

## Testing (TDD)

1. **Solver unit** — `bracketFillIncomeForYear` with a nonzero earnings term sizes
   the conversion so ordinary income *including* earnings lands on the ceiling
   (and stays below it once SS taxability is consumed).
2. **Convergence-helper unit** — relaxation + tolerance + max-iter +
   pick-best-on-non-convergence behave as specified, including the
   shrink-increases-earnings oscillation case.
3. **Integration (the corner)** — reproduce the adversarial-overlap scenario;
   assert the engine's actual taxable ordinary income in conversion years lands
   ≤ ceiling + tol after iteration (the ~$23k overshoot is eliminated).
4. **Regression (the common case)** — an ample-Roth scenario produces *identical*
   per-year conversions and a single engine run (proves no behavior change).

## Verification

`go build ./... && go vet ./... && go test ./... && staticcheck ./...` green;
`git diff` limited to the two analysis files + new tests.
