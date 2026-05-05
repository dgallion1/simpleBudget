# What-If Math Audit — Design

**Date:** 2026-05-05
**Author:** Claude (audit pass for Darrell)
**Status:** Draft, awaiting review

## Goal

Produce a written audit of every numerical computation that feeds the
What-If retirement planner page, verifying each formula and constant against
authoritative external sources, and reporting which boundary / edge-case
conditions are not covered by existing tests. This audit is the deliverable.
Fixes are explicitly out of scope for this engagement; each finding becomes a
candidate follow-up ticket.

## Non-goals

- No code changes during the audit pass. Every finding ends as a documented
  observation, not a commit.
- No bumping the tax-table base year (2024 → 2025 / 2026). Whether to bump
  is its own decision; the audit reports the current state and surfaces the
  staleness picture in a single appendix so it can be triaged separately.
- No re-architecture suggestions, no "this should be a method on X" comments,
  no test-style critiques. The audit is about math correctness and
  edge-case coverage, not engineering taste.
- No verification of UI rendering, formatting, or HTMX flow. The April 11
  review (`docs/what-if-page-review-2026-04-11.md`) and April 29 hardening
  pass (`docs/what-if-hardening-2026-04-29.md`) covered those.

## Deliverable

A single markdown file at:

```
docs/whatif-math-audit-2026-05-05.md
```

Approximate length: 3000–6000 words. The document follows the
"Approach 3" structure agreed during brainstorming:

1. **Executive summary** — three-paragraph overview: how many functions
   audited, how many findings by severity, top three concerns.
2. **Findings table** — one row per finding, sorted by severity then by
   area, with stable IDs (`F-001`, `F-002`, …) so each row can be cited
   from a follow-up ticket. Columns: ID, severity, area, location,
   one-line summary.
3. **Body, organized by math area** — ten sections, one per area
   (see "Math surface" below). Each function in scope appears under its
   area with: location, what it computes, source consulted, verification
   result (PASS / finding refs), worked example (when applicable), test
   coverage gaps.
4. **Constants-currency appendix** — every numeric constant the planner
   embeds, with as-coded value, most-current published value, year of
   each, and status (current / stale / N/A). Lets the reader decide
   whether to bump tables without re-reading the audit.
5. **Method appendix** — short prose describing how the audit was
   conducted, so the audit is reproducible in future passes.

## Math surface in scope

Ten areas, ~50–60 functions. Concrete enumeration so we know up front
what counts as "covered."

### 1. Federal & state income tax (`internal/services/retirement/tax.go`)

- `CalculateFederalTax`
- `calculateFederalTaxOnTaxableIncome`
- `CalculateStateTax`
- `CalculateTotalTax`
- `CalculateTaxWithInvestmentIncome`
- `calculateTaxWithInvestmentIncomeInternal`
- `CalculateTaxWithInvestmentIncomeBreakdown`
- `EstimateRothConversionTax`
- `GetMarginalRate`
- `GetAdjustedBrackets`
- `GetAdjustedLongTermCapitalGainsBrackets`
- `GetAdjustedStandardDeduction`
- `inflationFactor`
- `normalizeFilingStatus`
- Tables: `TaxBrackets2024`, `LongTermCapitalGainsBrackets2024`,
  `StandardDeduction2024`

### 2. Specialized federal tax surcharges (`tax.go`)

- `CalculateTaxableSocialSecurity` (free function + receiver method)
- `CalculateNIIT` (free function + receiver method)
- `CalculateMonthlyIRMAA` (free function + receiver method)
- Tables: `socialSecurityTaxThresholds`, `niitThresholds`,
  `monthlyIRMAASurcharge2026`

### 3. Social Security (`internal/services/retirement/social_security.go`)

- `validSSClaimAge`, `normalizedSSFRA`, `normalizedSSCOLARate`
- `AdjustedSSBenefit` (PIA → benefit at claim age, FRA aware)
- `DerivedPIA` (inverse of the above)
- `AdjustedSpousalBenefit`
- `SpousalTopUp`
- `claimStartMonth`
- `projectedSSBenefitForMonth` (COLA compounding from claim)
- `projectedSocialSecurityIncome`
- `ProjectedSSEntries`
- `SSComparisonTable`, `ssComparisonTable`
- `SSBreakevenAges`, `ssBreakevenAges`
- `cumulativeBenefit`
- `RunSSAnalysis`
- `RunSSPortfolioAnalysis` (optimizer driver — verify decision rule, not
  Monte Carlo internals here)
- `bestSSPortfolioOption`, `isBetterSSPortfolioOption`

### 4. RMD (`internal/services/retirement/rmd.go`)

- `GetLifeExpectancyFactor`
- `CalculateRMD`
- `CalculateRMDAnalysis`
- The Uniform Lifetime Table embedded in this file

### 5. Present value & monthly compounding (`calculator.go`)

- `PresentValue`
- `PresentValueAnnuity`
- `monthlyCompoundFactorFromDecimal`
- `monthlyCompoundFactorFromPercent`
- `compoundedFactorFromPercent`
- `fractionalMonthlyReturn`
- `plannerInflationFactorForYear`
- `plannerIRMAAInflationFactorForYear`
- `calculateHealthcarePV`

### 6. Living-expense projection mechanics (`calculator.go`)

- `calculateLivingExpensesAtMonth`
- `rebaseLivingExpensesAtTransition`
- Spending-phase application (multipliers, age-triggered transitions)
- Inflation compounding inside the projection loop

### 7. Taxable account, allocation, and tax-aware withdrawals (`calculator.go`)

- `newTaxableAccountState`
- `taxableAccountState.addCash`, `withdraw`, `applyGrowth`, `syncAssumptions`
- `buildTaxableReturnComponents`
- `expectedTaxableMonthlyCashFlow`
- `executeTaxAwarePortfolioMonth` (withdrawal ordering)
- `executePortfolioCashFlowWithTaxableState`
- `reinvestRequiredRMDToTaxableState`
- `applyBigTicketExpenseWithTaxableState`
- `projectionTimingGrowthFractions`
- `taxDeferredDelayActive`, `shortfallIsTemporaryDueToDelay`
- Per-account allocation and glide-path math: every function in
  `calculator.go` whose name or call graph touches `StockAllocation`,
  `CashAllocation`, glide-path interpolation, or the per-account
  (tax-deferred / Roth / taxable) split. Enumeration is the audit's
  first sub-task in this area — the audit body lists each function
  found and applies the standard verification steps.

### 8. Roth conversion math (`calculator.go` + `tax.go`)

- `rothConversionAmountForYear`
- `EstimateRothConversionTax` (covered above; cross-listed here)
- Conversion application inside the projection loop

### 9. Backtest, Monte Carlo, guardrails (`backtest.go`, `guardrails.go`,
   `historical_data.go`)

- Monte Carlo return draws and statistical aggregation (mean, percentile,
  success rate)
- Historical sequence reconstruction
- Worst-start-year detection
- Guyton-Klinger-style guardrail rules (withdrawal raises / cuts)

### 10. Scenario chain, healthcare cost, budget-fit / steady-state
   (`chain.go`, `calculator.go`, `internal/models/whatif.go`)

- `ResolvedScenarioChainLink` math: handing off balances, ages,
  cumulative inflation across links
- Healthcare per-person cost: ACA cost after employer share, Medicare
  with IRMAA, age-based eligibility transitions
- `calculateMonthlyIncomeBreakdown`
- `CalculateTotalExpenses`
- `CalculateTotalIncome`
- `CalculateBudgetFit`, `findSteadyStateMonth`, steady-state inflation
  forward
- `CalculatePresentValueAnalysis`
- `buildProjectionExplainability`

If the audit discovers additional math functions that materially affect
What-If page outputs but are not listed above, they are added in-place
under the appropriate area and called out in the executive summary.

## Authoritative sources

Each finding cites a specific source by name and section. Default source
mapping:

| Area | Source |
|------|--------|
| Federal income tax brackets, std. deduction (TY2024) | IRS Rev. Proc. 2023-34 |
| LTCG brackets (TY2024) | IRS Rev. Proc. 2023-34 |
| Taxable Social Security thresholds | 26 USC § 86; IRS Pub 915 worksheet |
| NIIT | 26 USC § 1411; IRS Pub 550 |
| IRMAA tiers (2026) | CMS 2026 Medicare Part B & D IRMAA announcement |
| RMD life expectancy | IRS Pub 590-B Uniform Lifetime Table (post-2022) |
| RMD start age | SECURE 2.0 Act (age 73 starting 2023, age 75 starting 2033) |
| SS PIA → benefit at claim age | SSA POMS RS 00615.105 (early retirement reduction); RS 00615.690 (delayed retirement credits) |
| SS spousal benefit | SSA POMS RS 00615.020 |
| SS taxable threshold filing-status thresholds | 26 USC § 86 |
| Present value formulas | Standard finance — any corporate finance text; the audit cites the formula explicitly so a reader can re-derive |
| Monte Carlo / backtest | Internal model — no external authority required; verify statistical correctness only |
| Guyton-Klinger guardrails | Guyton & Klinger (2006), *Decision Rules and Maximum Initial Withdrawal Rates* |

For every internal-model function (e.g., glide path, scenario chain
hand-off, projection timing fractions), the audit verifies internal
consistency rather than external authority: invariants like "balance is
preserved across hand-off," "fractions sum to 1," "no negative growth
factor."

## Audit method

For each in-scope function:

1. **Read implementation** — current `master`-merged code on the `dev`
   branch (HEAD: `ad8fb19` at audit start).
2. **Identify source** — pick the authoritative reference from the table
   above, or document why no external authority applies.
3. **Verify formula** — confirm the implementation matches the source's
   formula. Note any divergence with line citations.
4. **Verify constants** — for tables (brackets, thresholds, IRMAA tiers,
   life-expectancy factors), check every cell against the source. Tables
   are small (≤8 brackets × 4 filing statuses; ≤30 IRMAA tiers; ~30
   life-expectancy factors), so full coverage is cheap and avoids
   sampling bias. Document mismatches per cell.
5. **Worked example** — for tax / SS / RMD / PV functions, run one or
   two numerical examples taken from the authoritative source (or
   computed by hand from it) and confirm the code produces the same
   number. Tolerance: ±$0.01 absolute for currency outputs, ±1e-6 for
   pure factors and ratios. Anything outside that is a finding.
6. **Test-coverage gap analysis** — read existing tests for the function
   and list boundary conditions *not* exercised. The boundary checklist
   used per function:

   - Zero input
   - Negative input (where mathematically meaningful)
   - Very large input (overflow / saturation)
   - Bracket / threshold boundaries (just-below, exact, just-above)
   - Each filing status (single, MFJ, MFS, HoH)
   - Time-zero (year 0, month 0, claim age = current age)
   - Time-end (final projection year)
   - Inflation factor = 1 (no inflation)
   - Empty / nil settings, empty income or expense lists
   - Off-by-one age transitions: RMD 73, RMD 75, FRA, Medicare 65,
     spending-phase boundaries
   - Year wrapping inside scenario chain hand-offs

7. **Severity assignment** — single rubric, applied uniformly:

   - **HIGH** — math is wrong in a way that produces a >5% error in a
     realistic scenario, or qualitatively wrong (entire tax category
     missed, sign flipped, wrong bracket selected).
   - **MEDIUM** — edge-case wrong, small-magnitude error in realistic
     ranges, or meaningful test-coverage gap that could let a future
     change ship a bug undetected.
   - **LOW** — cosmetic / precision, unrealistic-input handling, dead
     branches, defensive code that doesn't match documented behavior.
   - **INFO** — observation, currency note, naming concern, or
     documentation drift.

8. **Record finding** — even if status is PASS, the function name appears
   in the body section (with status `PASS`) so the audit is auditable.

## Constants-currency appendix

Single table at the end of the doc. Columns:

| Constant | Location | As-coded value | As-coded year | Most-current published value | Most-current year | Status |
|----------|----------|----------------|---------------|------------------------------|-------------------|--------|

Every numeric constant referenced by the math surface — every bracket
edge, every threshold, every IRMAA tier, every life-expectancy factor —
gets a row. The "Status" column is one of:

- `current` — code matches the most-current published value
- `stale` — code is older than current; bump candidate
- `n/a` — internal model, no external reference

This appendix is the only place where "should we update to TY2025?" is
addressed. The body of the audit treats year-as-coded as the baseline.

## What this audit does *not* answer

- Whether the planner's user-facing copy correctly describes the math.
- Whether the planner's defaults are sensible.
- Whether tests are well-organized or fast.
- Whether observed projection numbers are "reasonable" for any given
  household — only whether the math producing them matches the cited
  authoritative source.

## Process

1. Approve this design. (Awaiting user.)
2. Move to `writing-plans` skill to produce a step-by-step implementation
   plan for conducting the audit and writing the deliverable.
3. Execute the plan. The plan will likely break the audit into one
   sub-task per area (10 sub-tasks) plus the constants appendix and
   executive summary, with checkpoints between areas.
4. Review the produced audit document.
5. Triage findings into follow-up tickets. (Out of scope for this
   engagement; the user owns triage.)

## Open questions

None at design time. All scoping decisions are recorded in the
brainstorm transcript and locked in by sections above:

- Deliverable shape: audit-only, fixes deferred (chosen during
  brainstorming).
- Math surface: full (foundational + projection engine + everything
  user-visible) including test-coverage gap analysis as a first-class
  column.
- Correctness baseline: year-as-coded; staleness in a single appendix.
- Document format: hybrid — exec summary + severity-bucketed table on
  top, area-organized detail below, constants-currency appendix at end.

## Approval

After this spec is approved, the next step is the `writing-plans` skill
to produce a concrete step-by-step plan for conducting the audit. The
plan will own task breakdown, ordering, evidence-gathering format, and
the worked-example matrix used per area.
