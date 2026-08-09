# Verification plan — RMD/IRMAA audit findings (2026-08-08)

Companion to `docs/rmd-tax-bomb-gap-analysis-2026-08-08.md`. That document is a
read-only audit produced without the ability to run the test suite. **Nothing
here has been executed.** Every claim below is a hypothesis with a stated
falsification test.

---

> ## STATUS as of 2026-08-09 — this plan has been executed; do not treat it as a to-do list
>
> Every finding below was worked with a failing test written first. All five
> executable findings were **confirmed**, none closed as wrong. The text below
> describes a tree that no longer exists — read it as a record of the
> investigation, not as pending work.
>
> | Finding | Verdict | Disposition |
> |---|---|---|
> | F-1 surplus RMD taxed twice | confirmed | fixed — `ac741bc` |
> | F-2 year-0 MAGI surcharged 3× | confirmed | fixed — `9182bb0` |
> | F-3 `Age65Count` never derived | confirmed | fixed — `2754370` |
> | F-4 RMD start age ignores ownership | confirmed as a modeling limitation | documented — `bc97a1c`, corrected `62cca91` |
> | F-5 IRMAA vs employer coverage | confirmed (two defects) | fixed — `d29f78e` |
> | F-6 surcharge dollars inflate at CPI | confirmed | fixed — `438c629` |
>
> **Measured effects.** F-1 destroyed `unmetRMD × marginalRate` of wealth every
> surplus-RMD month (−$18,592.67 on an $81,300 annual RMD). F-2 charged one
> year-0 event three times (5825.60 / 6355.20 / 6355.20 / 0 → 0 / 0 / 6355.20 / 0).
> F-3 overcharged a 68/67 MFJ household $422.73 every year. F-5 charged
> $6,355.20/yr for three years of employer coverage. F-6 understated the
> 20-year monthly surcharge at MAGI $400k by $901.61 → $1,388.33.
>
> **Corrections to this document's own analysis**, found while executing it:
> - F-2's `Load("")` guidance would have used `ActiveScenario()`, which returns a
>   display name, not a filename. The correct call is `ActiveFilename()`.
> - F-4's claim that the older member drives the *divisor* is only true under the
>   Uniform Lifetime Table. Where spouse-sole-beneficiary Joint Life Table II
>   applies, the divisor is a function of both ages and is larger. The start-year
>   half of the claim is correct. Fixed in the engine comment and the RMD card
>   in `62cca91`.
>
> **Still open.** The feature-gap list at the end of this document is untouched —
> survivor's penalty, IRMAA-threshold fill target, QCDs, effective marginal rate,
> `LifetimeTaxReal` excluding IRMAA, medical deduction/itemizing, enhanced senior
> deduction, muni interest. F-2's "design question" — a user-settable prior-year
> MAGI on `WhatIfSettings` — was deliberately deferred; the fix seeds the lookback
> from a discrete-event-free proxy instead.
>
> **Note on testing F-2.** The shipped `whatif.json` cannot exercise it: MAGI
> peaks at $377k while the MFJ tier-1 threshold inflates to roughly $514k by 2055,
> so that plan never crosses a tier. The falsification test builds a scenario
> specifically to cross one.

Work findings in the order given: each one is a failing test first, then a fix,
then the full green gate.

## Ground rules for this repo

Per `CLAUDE.md`:

- Before editing any function/method/type, run `LSP` `incomingCalls` (or
  `findReferences` for vars/consts) and report the blast radius. Anything in
  `internal/services/retirement/engine` is high blast radius — warn before
  proceeding.
- Never rename with find-and-replace; enumerate uses with `findReferences`.
- Gate: `go build ./... && go vet ./... && go test ./... && staticcheck ./...`
- **Run tests bare.** Never `go test ./... 2>&1 | grep FAIL | head` — the pipe
  reports the last command's exit code and a red suite reads as exit 0. If
  output must be trimmed, prefix `set -o pipefail;`.
- Analysis-package tests build inputs with `runProj(t, s)` / `engineInput(t, s)`
  (`analysis/helpers_test.go`), never the retirement `Calculator`.
- `prepare.From` recomputes ages from `Person.BirthMonth` + `StartDate`. Set age
  via `s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, age)`.
- Compute test oracles from `in.Prepared.Settings()`, not raw input.

---

## F-1 (highest confidence bug): surplus RMD may be taxed twice

**Claim.** In a month where the RMD exceeds cash need, roughly
`gross × marginalRate` of household wealth disappears.

**Mechanism.** `engine/portfolio_month.go:388` funds `taxesPaid` as a cash
outflow, and `taxesPaid` already includes tax on the full gross RMD because
`WithdrawalFromTaxDeferred` (gross) feeds `TaxableWithdrawals` at `:417`. But
`ReinvestRequiredRMDToTaxableState` (`:175-191`) *also* withholds
`gross × marginalRate` before depositing, and that withheld amount is never
spent on anything.

**Worked example to encode as the test.** $10k monthly surplus RMD, 22%
marginal, no expenses, no other income:

- tax-deferred should fall by $10,000
- $2,200 should leave the household as tax
- the taxable account should receive $7,800
- current code deposits `10000 × 0.78 = $7,800` only in the branch where the
  whole RMD is surplus; where taxes push `neededFromPortfolio` positive, the
  RMD is split and the reinvested slice is withheld a second time

**Falsification test.** New test in `internal/services/retirement/`. Build a
scenario with a large tax-deferred balance, RMD age reached, and living
expenses well below the RMD. Run one projection year. Assert the conservation
identity:

```
Δ(taxDeferred) == Δ(taxable) + taxesPaid + irmaaPaid + spendingFromPortfolio
```

If the identity holds, this finding is wrong — close it and say so. If it fails
by approximately `gross × marginalRate`, it is confirmed.

**Note before fixing.** The unit tests at `taxable_simulation_test.go:109-166`
deliberately pin the net-of-tax contract of the helper in isolation (F-049 /
F-073). The defect, if real, is at the integration seam, not in the helper. The
F-049 goal — correct cost basis for future LTCG — is achievable by depositing
gross with gross basis, since the tax is already an explicit expense line. So
the fix likely changes both the helper and those three tests. Run
`LSP incomingCalls` on `ReinvestRequiredRMDToTaxableState` first.

---

## F-2: year-0 MAGI is charged IRMAA three times

**Claim.** A year-0 Roth conversion or first RMD is surcharged in projection
years 0, 1 and 2, where reality surcharges it once (in year 2).

**Mechanism.** `engine/stepper.go:311` always passes a non-nil
`&st.AssumedLookbackMAGI`, so `resolveIRMAALookbackMAGI`
(`engine/projtax.go:161-169`) never reports "no lookback". Then `stepper.go:321-323`
overwrites the seed with the current month's own MAGI throughout year 0.

Resulting lookback distance:

| Projection year | MAGI used | Should be |
|---|---|---|
| 0, month 0 | 0 | year −2 |
| 0, months 1–11 | that same year, lagged one month | year −2 |
| 1 | December of year 0 | year −1 |
| 2 | year 0 (from history) | year 0 ✓ |

**Falsification test.** Two projections identical except for a large year-0 Roth
conversion. Assert the IRMAA delta appears in exactly one projection year. If it
appears in three, confirmed.

**Related.** `analysis/budget_fit.go:202` builds the seed with RMD and
conversions stripped; the engine does not strip them — so the Current column and
the projection disagree for the same plan. And `budget_fit.go:357-358` passes
`nil` for `steadyStateMonth < 24`, which yields a displayed $0 IRMAA that
contradicts both. Assert all three agree.

**Design question, not just a fix.** The real answer is a user-settable
prior-year MAGI on `WhatIfSettings` (there is no such field, form input, or
handler today). A household that just retired off a $300k salary is exactly the
group most exposed to years 0–1 IRMAA and cannot currently express it.

---

## F-3: `Age65Count` is never derived, so the engine drops the age-65 deduction

**Claim.** The engine omits the age-65 additional standard deduction for all
saved plans, while the tax optimizer applies it — so the optimizer sizes
bracket-fill conversions against a larger deduction than the engine then uses,
and over-converts slightly.

**Evidence.** `engine/tax.go:265-281` correctly adds `Age65Count × $1,550` (MFJ,
2024 base). But `Age65Count` (`models/whatif.go:1237`) is a static JSON field:
the only non-test writer is `analysis/tax_optimizer_strategies.go:129`
(`age65CountForYear`), there is no UI input anywhere (`grep -rn age_65 web/
internal/handlers/` returns nothing), and both shipped scenarios
(`data/settings/whatif.json:155`, `whatif_job-loss.json:345`) have
`"age_65_count": 0`.

**Falsification test.** Projection with both spouses over 65. Assert the
standard deduction actually applied includes the age-65 addition. Then assert
the optimizer's `bracketFillIncomeForYear` deduction equals the engine's for the
same year.

**Fix shape.** Derive per-year in the engine the way `age65CountForYear` already
does, at the `NewTaxCalculator` refresh point (`stepper.go:201`). `Age65Count`
becomes a fallback for legacy callers, not the source of truth. Run
`LSP findReferences` on `Age65Count` before changing it.

---

## F-4: RMD start age ignores account ownership

Tax-deferred is a single household pool and the *older* member drives both the
start year and the divisor (`engine/rmd.go:83-115`). If only the younger spouse
holds the tax-deferred money, the model starts RMDs potentially a decade early
with a smaller divisor. No owner attribution exists to prevent it.

This is a modeling limitation, not a bug — worth a decision (add ownership, or
document the assumption) rather than a test.

---

## F-5: IRMAA eligibility contradicts the healthcare expense model

`MedicareEligibleAdultCountAtYear` (`engine/loop_helpers.go:66-79`) charges
IRMAA at 65 regardless of employer coverage, while the expense model correctly
keeps someone on an employer premium (`models/healthcare.go:147-152`). The two
contradict each other for exactly the household `EmployerCoverageYears` exists
to describe. Also: `HealthcarePerson.MedicareEligibleAge` is ignored, 65 is
hardcoded, and eligibility flips on the plan anniversary rather than the
birthday.

**Test.** Scenario with `EmployerCoverageYears` covering ages 65–67. Assert no
IRMAA is charged while employer coverage is active.

---

## F-6: IRMAA surcharge dollars inflate at CPI

`engine/tax.go:378-386` applies one factor to both thresholds and surcharge
amounts. Thresholds are CPI-indexed (correct); surcharge dollars track Medicare
per-capita cost growth, historically 5–6%. Future surcharges are systematically
understated. Needs a second rate, not a test.

Adjacent: `PlannerIRMAAInflationFactorForYear` is not floored at zero, unlike
`GetAdjustedBrackets` / `InflationFactor`, so a 2025 plan year deflates IRMAA
brackets below the 2026 table while tax brackets stay pinned at 2024. For IRMAA
this happens to be *more* accurate (2025 tier 1 computes to ≈$105.8k/$211.6k at
3% vs actual $106k/$212k) — probably fix the comment, not the code.

---

## Feature gaps (no test; design work)

Ranked by whether they change a recommendation or just refine a number.

1. **Survivor's penalty.** No mortality anywhere; `FilingStatus` is a scalar
   frozen for the horizon; the Roth optimizer scores 100% of candidates under
   permanent MFJ. Every ingredient exists — correct single-filer tables at
   `engine/tax.go:107-176`, a per-year `TaxCalculator` refresh at
   `stepper.go:201`, separable per-person SS streams at
   `social_security.go:110-127`, and a scenario-chain mechanism at
   `stepper.go:191` that already swaps `filing_status` mid-projection. Watch
   `chain.go:24`: it carries the spouse `Person` through the transition, so RMD
   joint-life eligibility and the Medicare head count need explicit handling.
   `tax-optimizer.html:170` already tells the user this is unmodeled.
2. **IRMAA-threshold fill target + headroom surfacing.** `taxOptimizerBracketFillTargets`
   is `{0.12, 0.22, 0.24}`; a 24% MFJ fill ($383,900) clears every IRMAA tier
   below the top. Nothing anywhere computes distance to the next threshold. Also
   `tax_optimizer_strategies.go:40` defines the "IRMAA" window as ending at 65 —
   it must end at 63, because MAGI at 63 drives premiums at 65.
3. **QCDs.** Wholly absent (exhaustive grep: `qcd`, `qualified charitable`,
   `charitab`, `charity`, `donat`, `philanthrop` → one unrelated test fixture).
   The only tool that improves both the taxable-income and MAGI gauges at once,
   and the optimizer has no way to weigh it against a bracket-fill conversion.
4. **Effective marginal rate.** `analysis/tax.go:76` reports the statutory
   bracket; `tax-summary.html:63` says so. Excludes the §86 phase-in (22%
   behaving like ~40.7%) and the IRMAA cliff — i.e. it is not the number a
   conversion decision turns on.
5. **`LifetimeTaxReal` excludes IRMAA** (`tax_optimizer.go:232`, acknowledged at
   `:200-205`), so the displayed cost omits its largest hidden component even
   though the ranking metric feels it.
6. **Medical expense deduction / itemizing** — no support at all (`grep itemiz`
   → nothing).
7. **Enhanced senior deduction** (2025–2028, $6,000/person, phases out above
   $75k/$150k MAGI) — absent.
8. **Tax-exempt muni interest** — no income-source type, no MAGI add-back, so
   IRMAA tier is understated for muni holders with no workaround.

Not modeled and reasonably so: the first-RMD April 1 deferral that stacks two
distributions into one calendar year (`RMDTiming` is intra-year only), and the
§4974 25%/10% missed-RMD excise (structurally unreachable — the model never
under-takes an RMD).
