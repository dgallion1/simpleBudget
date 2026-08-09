# simpleBudget vs. "The RMD Tax Bomb" — gap analysis

**Source reviewed:** NotebookLM notebook *The RMD Tax Bomb: Managing Distributions and Medicare Cliff Costs* (single source: the YouTube video "When I Tell Retirees This, They No Longer Worry about RMDs"). Read via authenticated Chrome; the notebook is private so it could not be fetched directly.

**Code reviewed:** `/home/darrell/bin/ai/budget2` @ working tree, read-only. No files modified.

---

> ## STATUS as of 2026-08-09 — the defect list has been worked; the feature gaps have not
>
> Six of the eight defects in *Defects found while auditing* are closed. The
> article-derived analysis above them — the survivor's penalty, the all-in
> marginal cost framework, and the tools the app cannot express — is **untouched
> and still accurate**. Read this document for that analysis; do not read the
> defect list as pending work.
>
> | # | Defect | Disposition |
> |---|---|---|
> | 1 | Year-0 MAGI charged IRMAA three times | **fixed** — `9182bb0`. The lookback is now seeded once, at month 0, from a snapshot excluding the plan's discrete events. |
> | 2 | No user-settable prior-year MAGI | **open, deliberately deferred.** Needs a `WhatIfSettings` field, form input and handler. The fix for #1 uses a discrete-event-free proxy instead. |
> | 3 | Surplus RMD taxed twice | **confirmed and fixed** — `ac741bc`. You asked for this to be verified before acting on it; it was. A one-year projection lost $18,592.67 that conservation could not account for, and at the seam the leak is exactly `unmetRMD × marginalRate`. The gross is now deposited with gross basis. |
> | 4 | Steady-state IRMAA shows $0 for early years | **STILL OPEN.** `budget_fit.go` still passes `nil` for `steadyStateIRMALookbackMAGI` when `steadyStateMonth < 24`, so the steady-state column reports $0 IRMAA and contradicts both the Current column and the projection. Verified still present on 2026-08-09. |
> | 5 | Budget Fit and the engine seed IRMAA differently | **resolved in construction** by the #1 fix — the engine now strips RMD and conversions from its year-0 seed, matching what `budget_fit.go` always did. The three-way agreement assertion this document asked for was never written, so it is convention rather than a guarded invariant. |
> | 6 | RMD start age ignores account ownership | **confirmed as a modeling limitation, documented not fixed** — `bc97a1c`, corrected `62cca91`. Stated at the point of computation and surfaced on the RMD card for couples. Fixing it properly needs per-person ownership on tax-deferred balances. |
> | 7 | IRMAA eligibility ignores employer coverage | **fixed** — `d29f78e`. Two defects, both real: the employer-coverage contradiction, and `MedicareEligibleAge` being ignored in favour of a hardcoded 65. Eligibility still turns on the plan anniversary rather than the birthday — that is annual granularity throughout the IRMAA path, not an oversight. |
> | 8 | IRMAA surcharge dollars inflate at CPI | **fixed** — `438c629`. Surcharge dollars now grow at a 5.5% Medicare per-capita constant; thresholds still track CPI. |
>
> **One correction to this document.** Defect 6 says the older member drives both
> the start year *and* the divisor. The divisor half is only true under the
> Uniform Lifetime Table — where spouse-sole-beneficiary Joint Life Table II
> applies, the divisor is a function of both ages and is larger, giving smaller
> RMDs. The same error had propagated into an engine comment and the RMD card;
> both corrected in `62cca91`.
>
> **The priority order at the end still holds** for everything not struck above:
> the survivor's penalty remains the one change that moves recommendations rather
> than refining numbers.

## The article's thesis, compressed

RMDs are not the monster; the *unmeasured next dollar crossing a line* is. The article argues a planner has to track two separate gauges — taxable income (federal brackets) and MAGI (IRMAA) — because a deduction can move one without moving the other. It then lists the specific lines that matter: the §86 Social Security phase-in that makes a nominal 22% bracket behave like ~40.7%; the IRMAA cliff with its two-year lookback (a 2026 conversion sets 2028 premiums); the survivor's penalty, where filing status collapses to single and brackets plus the IRMAA line roughly halve while income does not; and the tools that respond to each gauge differently — QCDs cut AGI *and* MAGI, medical deductions cut taxable income but *not* MAGI, the enhanced senior deduction is temporary, and Roth conversions trade current income for permanent RMD reduction. The decision rule it lands on is an all-in marginal cost comparison: federal + state + attributable IRMAA per dollar converted now, versus the same per dollar distributed later, scored under **both** the married and the survivor dashboards.

Measured against that, simpleBudget is in unusually good shape on the mechanics and has one structural hole.

---

## Already modeled, and modeled well

**The two gauges are genuinely separate.** `engine/tax.go:517` computes MAGI from gross components before the standard deduction, while `taxableOrdinaryIncome` at `:497` subtracts it — so MAGI is AGI-like and taxable income is post-deduction, exactly the distinction the article insists on. Only the §86-taxable portion of Social Security enters MAGI, which is correct (AGI contains only the taxable portion).

**The IRMAA two-year lookback is implemented.** `engine/projtax.go:161` `resolveIRMAALookbackMAGI` reaches back to `completedMAGIHistory[len-2]`, and `engine/stepper.go:185-188` appends exactly once per completed year using December's snapshot (where the annualization factor is 12/12, so it is a true full-year MAGI, not an extrapolation). This is the piece most planners skip entirely. The bundled table at `engine/tax.go:146-178` is 2026 CMS data with the correct `{109000}` single / `{218000}` MFJ first thresholds the article cites, and `PlannerIRMAAInflationFactorForYear` (`engine/loop_helpers.go:86`) correctly offsets the 2026 IRMAA base against the 2024 tax base — the factor works out to `(1+i)^(calendarYear−2026)`, which is right.

**IRMAA is a real cost, not a display number.** It is funded from the portfolio (`engine/portfolio_month.go:388`) and folded into total expenses (`engine/stepper.go:329`), so it depresses the ending balance the Roth optimizer ranks on. The optimizer therefore *does* feel the two-year IRMAA consequence of a conversion even though it never names it.

**The Social Security torpedo is modeled at the level the article demands.** `CalculateTaxableSocialSecurity` (`engine/tax.go:306`) implements §86 properly, including both MFS sub-cases under §86(c)(2). More impressively, the bracket-fill Roth solver does not make the naive mistake: `bracketFillConversion` (`analysis/tax_optimizer_strategies.go:256`) binary-searches for the conversion that lands on the ceiling *accounting for the fact that the conversion itself pushes more SS into taxable income* — the exact "$1 creates $1.85 of taxable income" effect. It then iterates against engine feedback for non-qualified Roth earnings. That is a more careful treatment than the article itself describes.

**RMD mechanics are correct.** SECURE 2.0 applicable age derived from birth year with the 1959/1960 cusp handled off `BirthMonth` rather than floor'd ages (`engine/rmd.go:30-76`); Uniform Lifetime Table III with automatic switch to Joint & Last Survivor Table II when the spouse is sole beneficiary and >10 years younger (`:275-299`); the RMD is computed on the prior year-end balance *before* that year's Roth conversion (`engine/stepper.go:206-208`), which is the IRS-correct ordering; surplus RMD is force-distributed and reinvested into taxable, so the snowball into future dividends and MAGI is real.

**The team already documents the gaps honestly.** `web/templates/components/whatif/tax-optimizer.html:170` tells the user outright that survivor filing-status changes are not modeled, and the budget-analysis tooltip at `budget-analysis.html:271` explains the two-year IRMAA lag and states that the withdrawal gross-up excludes it.

---

## The structural gap: the survivor's penalty

This is the article's central argument and the app does not model it at all.

There is **no mortality anywhere in the codebase** — zero hits for death, mortality, widow, deceased, or any filing-status transition. `models.Person` is `{ID, Name, BirthMonth, Role}` with no death age. `TaxConfig.FilingStatus` is a single scalar consumed at only two `NewTaxCalculator` sites (`engine/stepper.go:134` and `:201`), so brackets, standard deduction, §86 thresholds, NIIT and the IRMAA table are all frozen MFJ for the entire horizon.

The existing survivor code is benefit-amount display only. `SurvivorBenefitForClaimAge` (`analysis/ss.go:139`) applies the RIB-LIM rule correctly and fills a column in the SS claiming card, but it never constructs a `TaxCalculator`, never touches filing status, and never feeds the projection. Projection SS income comes from two perpetual streams in `social_security.go:110-127` with no end month — both spouses' checks run forever. The design spec says this is deliberate ("inform, not decide").

Supporting pieces are also absent: `IncomeSource` has no owner and no survivor election (`income_source.go:16-26`), so a 50%/75% joint-and-survivor pension cannot be expressed except by hand-splitting it into two rows with a guessed crossover month. Portfolio buckets are a single household pool. `MedicareEligibleAdultCountAtYear` (`engine/loop_helpers.go:66`) returns 2 forever once both are 65.

**Why this matters for the app's own recommendations:** the Roth optimizer scores 100% of candidates under permanent MFJ (`analysis/tax_optimizer_strategies.go:316, 394` — `bracketTopFor` always keyed on the saved status). The survivor's penalty is the single strongest argument *for* converting more aggressively, so the optimizer is systematically biased toward under-converting for couples with a meaningful age gap or health asymmetry.

The encouraging part: every ingredient exists. Correct single-filer brackets, standard deduction, §86 thresholds and the $109k IRMAA line are all in `engine/tax.go:107-176`. There is a per-year `TaxCalculator` refresh point at `stepper.go:201`. Separable per-person SS streams exist. And the scenario-chain mechanism at `stepper.go:191` already swaps settings mid-projection and does pick up a chained scenario's `filing_status` — so a "survivor scenario at age N" is closer to a wiring job than a rewrite. Note that `chain.go:24` carries the spouse `Person` through the transition, so RMD joint-life eligibility and the Medicare head count would need explicit handling.

---

## Tools the article recommends that the app cannot express

**QCDs — absent.** Exhaustive grep for `qcd`, `qualified charitable`, `charitab`, `charity`, `donat`, `philanthrop` across Go, HTML, JS and JSON returns one unrelated URL-classification test fixture. There is no QCD field, no RMD offset, no AGI-exclusion path, no UI. This is the article's *only* tool that improves both gauges at once, and it is the one the optimizer has no way to weigh against a bracket-fill conversion. For a charitably-inclined household this is a first-order omission.

**Medical expense deduction — absent.** No itemized deduction support of any kind; grep for `itemiz` returns nothing. The 7.5%-of-AGI floor, and the article's point that an RMD raises AGI and therefore raises the floor, cannot be represented.

**Enhanced senior deduction (2025–2028, $6,000/person, phasing out above $75k/$150k MAGI) — absent.** No hits for senior deduction, bonus deduction, or equivalents.

**The ordinary age-65 additional standard deduction is in the code but never applied.** `GetAdjustedStandardDeduction` (`engine/tax.go:265`) correctly adds `Age65Count × $1,550` (MFJ, 2024 base), but `Age65Count` is a static user-supplied JSON field with no UI input anywhere (`grep age_65 web/ internal/handlers/` → nothing), and both shipped scenarios have it at `0`. The engine therefore omits the deduction for saved plans. Worse, the tax optimizer *does* compute it per-year (`analysis/tax_optimizer_strategies.go:129`, `age65CountForYear`), so the optimizer sizes bracket-fill conversions against a larger standard deduction than the engine then applies — an internal inconsistency that makes the optimizer over-convert slightly.

**Tax-exempt muni interest — not modeled.** No income-source type and no MAGI add-back, so a muni-heavy portfolio's IRMAA tier is understated with no workaround.

---

## The all-in marginal cost framework

The article's decision rule is a per-dollar cost ratio: `(Δfederal + Δstate + attributable ΔIRMAA) ÷ amount converted`, computed now and compared against the same figure for the later distribution, under both dashboards.

The app has the raw material but never assembles the number.

`analysis/tax.go:76` reports `MarginalBracket` as `tc.GetMarginalRate(ys.MAGI, ...)` — the **statutory** bracket only. `tax-summary.html:63` says so explicitly: "Marginal is the top federal bracket reached that year." That is precisely the figure the article warns cannot answer the conversion question, because it excludes the §86 phase-in (the 22%-behaving-like-40.7% effect) and excludes the IRMAA cliff. Nothing anywhere computes an effective marginal rate.

Related surfacing gaps:

- **No IRMAA headroom anywhere.** `projection-breakdown.html` shows a MAGI column and an IRMAA column side by side with no threshold reference line. There is no tier label, no distance-to-next-threshold, no "crossing this costs $N/yr." At tier 1 that step is roughly $1,150/yr *per person* for one dollar of MAGI.
- **`LifetimeTaxReal` excludes IRMAA.** `analysis/tax_optimizer.go:232` sums only `ys.Taxes`; the doc comment at `:200-205` acknowledges this. The user sees a candidate's tax cost with its largest hidden component omitted, even though the ranking metric feels it.
- **No IRMAA-threshold fill target.** `taxOptimizerBracketFillTargets` is `{0.12, 0.22, 0.24}` (`tax_optimizer_strategies.go:16`). A 24%-bracket fill for MFJ ($383,900) clears every IRMAA tier below the top. There is no "fill to just under the IRMAA line" strategy — arguably the single most valuable candidate to offer.
- **The "IRMAA" strategy window is off by two years.** `tax_optimizer_strategies.go:40` defines the window as ending at age 65. Because of the lookback, MAGI at 63 drives premiums at 65 — a window ending at 65 still generates surcharges at 65 and 66. To actually anchor on IRMAA the window must end at 63.

---

## Defects found while auditing (independent of the article)

**1. Year-0 MAGI is charged IRMAA three times.** `engine/stepper.go:311` always passes a non-nil `&st.AssumedLookbackMAGI`, and `:321-323` overwrites it with the current month's own MAGI throughout year 0. The result: year 0 pays IRMAA on year-0 income, year 1 pays on December-of-year-0 income, and year 2 correctly pays on year-0 income from history. A year-0 Roth conversion or first RMD is therefore surcharged three times where reality surcharges once. This biases the optimizer against year-0 conversions.

**2. No user-settable prior-year MAGI.** There is no `WhatIfSettings` field, form input or handler for it. A household that just retired off a $300k salary — exactly the group most exposed to years 0–1 IRMAA — cannot tell the planner, and instead gets the artifact above.

**3. Surplus RMD appears to be taxed twice — worth confirming with a test.** In `ExecuteTaxAwarePortfolioMonth`, `taxesPaid` is funded as a cash outflow from the portfolio (`portfolio_month.go:388`) and already includes tax on the full gross RMD, since `WithdrawalFromTaxDeferred` (gross) feeds `TaxableWithdrawals` at `:417`. But `ReinvestRequiredRMDToTaxableState` (`:175-191`) *also* withholds `gross × marginalRate` before depositing, and that withheld amount is never spent on anything. Worked example: $10k monthly surplus RMD at 22% marginal — tax-deferred drops $10,000, $2,200 leaves as taxes, and the taxable account receives $6,084 instead of $7,800. Roughly `gross × marginalRate` of household wealth evaporates every surplus-RMD month. The unit tests at `taxable_simulation_test.go:109-166` pin the function's net-of-tax contract in isolation, so the issue is at the integration seam, not in the helper. The F-049 comment's stated goal (correct cost basis for future LTCG) is achievable by depositing gross with gross basis, since the tax is already an explicit expense line. **I could not run a test to confirm this from here — please verify before acting on it.**

**4. Steady-state IRMAA shows $0 for early years.** `analysis/budget_fit.go:357-358` passes `nil` when `steadyStateMonth < 24`, which makes `resolveIRMAALookbackMAGI` return `hasIRMALookback=false` → $0 IRMAA displayed, contradicting both the Current column and the projection for the same plan.

**5. Budget Fit and the engine seed IRMAA differently.** `budget_fit.go:202` builds the year-0 seed with RMD and conversions stripped; the engine does not strip them. Same plan, two different year-0 IRMAA numbers.

**6. RMD start age ignores account ownership.** The tax-deferred pool is a single household bucket and the *older* member drives the start year and the divisor (`engine/rmd.go:83-115`). If only the younger spouse holds the tax-deferred money, the model starts RMDs potentially a decade early with a smaller divisor. There is no owner attribution to prevent this.

**7. IRMAA eligibility ignores employer coverage.** `MedicareEligibleAdultCountAtYear` charges IRMAA at 65 regardless, while the healthcare expense model correctly keeps someone on an employer premium (`models/healthcare.go:147-152`). The two models contradict each other for exactly the household `EmployerCoverageYears` exists to describe. Also, `HealthcarePerson.MedicareEligibleAge` is ignored, and eligibility flips on the plan anniversary rather than the birthday.

**8. IRMAA surcharge dollars inflate at CPI.** `engine/tax.go:378-386` applies one factor to both thresholds and surcharge amounts. Thresholds are CPI-indexed (correct); surcharges track Medicare per-capita cost growth, historically 5–6%. Future surcharges are systematically understated.

**Not modeled, but reasonably so:** the first-RMD April 1 deferral that stacks two distributions into one calendar year (`RMDTiming` is intra-year only: month 0/6/11), and the §4974 25%/10% missed-RMD excise. The latter is structurally unreachable anyway — the model never under-takes an RMD.

---

## If you want a priority order

The survivor's penalty is the one that changes recommendations rather than refining numbers, and the chain mechanism means it is more wiring than rewrite. After that, an IRMAA-threshold fill target plus headroom surfacing would let the app actually answer the article's question rather than the adjacent one. QCD support is a self-contained feature with a clear place to live. The year-0 IRMAA seeding bug and the possible surplus-RMD double-tax are small fixes with real numerical impact. And `Age65Count` should be derived per-year in the engine the way the optimizer already derives it, rather than read from a JSON field with no UI.
