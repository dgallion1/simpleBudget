# What-If Math Audit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce `docs/whatif-math-audit-2026-05-05.md` — a written audit of every numerical computation that feeds the What-If retirement planner page, verifying each formula and constant against authoritative sources, with edge-case test-coverage gap analysis as a first-class column.

**Architecture:** Each area of math (tax, SS, RMD, PV, etc.) is one task. Each task produces one section of the final audit document, plus zero or more findings (with stable IDs `F-001`…`F-NNN`) appended to the running findings list. The doc is built incrementally on a feature branch; each task ends in a commit so the audit's progress is auditable. The executive summary and constants appendix come last because they roll up data from prior tasks.

**Tech Stack:** Go (read-only — no source changes), Markdown for output, `gitnexus_context` MCP tool for call-graph navigation, `go test -run` for spot-checking specific tests, IRS / SSA / CMS authoritative sources (cited per finding).

**Spec:** `docs/superpowers/specs/2026-05-05-whatif-math-audit-design.md` (commit `697c0de`)

---

## Conventions used throughout this plan

These apply to every task. Re-read this section if you switch agents mid-plan.

### Finding format

Every finding the audit emits looks like this:

```markdown
### F-NNN — [HIGH|MEDIUM|LOW|INFO] [one-line summary]

**Location:** `path/file.go:LINE` — `FunctionName`

**Source consulted:** [IRS Rev. Proc. 2023-34 §3.01(c) | SSA POMS RS 00615.105 | …]

**What it does:** [1-3 sentences in plain English.]

**Finding:** [What's wrong, missing, or noteworthy. If PASS, this row does
not appear — only findings get IDs.]

**Evidence / repro:** [Code excerpt with line numbers. If a worked example
disagrees, show the source's expected value, the code's actual value, and the
delta.]

**Recommended fix sketch:** [1-3 sentences. Not a patch — this audit ends
with documentation, not code.]

**Test coverage note:** [Which boundary conditions are not exercised, if
relevant. Reference test file by path.]
```

### Severity rubric (locked in spec)

- **HIGH** — math is wrong in a way that produces a >5% error in a realistic scenario, or qualitatively wrong (entire tax category missed, sign flipped, wrong bracket selected).
- **MEDIUM** — edge-case wrong, small-magnitude error in realistic ranges, or meaningful test-coverage gap that could let a future change ship a bug undetected.
- **LOW** — cosmetic / precision, unrealistic-input handling, dead branches, defensive code that doesn't match documented behavior.
- **INFO** — observation, currency note, naming concern, or documentation drift.

### Boundary checklist

For every audited function, list which of these the existing tests do NOT exercise. If a boundary doesn't apply mathematically (e.g., negative-income for taxable SS), say so explicitly.

- Zero input
- Negative input
- Very large input
- Bracket / threshold boundaries (just-below, exact, just-above)
- Each filing status (Single, MFJ, MFS, HoH)
- Time-zero (year 0, month 0, claim age = current age)
- Time-end (final projection year)
- Inflation factor = 1
- Empty / nil settings, empty income / expense lists
- Off-by-one age transitions (RMD 73, RMD 75, FRA, Medicare 65, spending-phase boundaries)
- Year wrapping inside scenario chain hand-offs

### Worked-example tolerance

- ±$0.01 absolute for currency outputs
- ±1e-6 for pure factors and ratios

If the code's output disagrees beyond tolerance, that's a HIGH or MEDIUM finding (case-by-case based on magnitude).

### Authoritative source map

| Area | Source |
|------|--------|
| Federal income tax brackets, std. deduction (TY2024) | IRS Rev. Proc. 2023-34 |
| LTCG brackets (TY2024) | IRS Rev. Proc. 2023-34 |
| Taxable Social Security thresholds | 26 USC § 86; IRS Pub 915 worksheet |
| NIIT | 26 USC § 1411; IRS Pub 550 |
| IRMAA tiers (2026) | CMS 2026 Medicare Part B & D IRMAA announcement |
| RMD life expectancy | IRS Pub 590-B Uniform Lifetime Table (post-2022) |
| RMD start age | SECURE 2.0 Act |
| SS PIA → benefit at claim age | SSA POMS RS 00615.105, RS 00615.690 |
| SS spousal benefit | SSA POMS RS 00615.020 |
| Present value formulas | Standard finance — derive in audit body |
| Monte Carlo / backtest | Internal model (verify statistical correctness only) |
| Guyton-Klinger guardrails | Guyton & Klinger (2006) |

### Citation format

Cite sources by name and section, never as URLs only. Example: `IRS Rev. Proc. 2023-34 §3.01(c)(2)(C) (Table 7)`. The audit must be readable without internet access.

---

## File Structure

The audit produces and modifies these files:

- **Create:** `docs/whatif-math-audit-2026-05-05.md` — the final deliverable
- **Modify:** none — no source code changes in this engagement
- **Read (extensively):**
  - `internal/services/retirement/tax.go`
  - `internal/services/retirement/social_security.go`
  - `internal/services/retirement/rmd.go`
  - `internal/services/retirement/calculator.go`
  - `internal/services/retirement/backtest.go`
  - `internal/services/retirement/guardrails.go`
  - `internal/services/retirement/historical_data.go`
  - `internal/services/retirement/chain.go`
  - `internal/services/retirement/settings.go`
  - `internal/models/whatif.go`
  - All `*_test.go` files in `internal/services/retirement/`
  - All `*_test.go` files in `internal/handlers/whatif/` (only for cross-reference, not in scope itself)

---

## Branch hygiene

- [ ] **Step 0a: Verify clean working tree for audit work**

Run: `git status --short`
Expected: working tree may have unrelated unstaged changes (e.g., calculator.go, rmd.go modifications from prior work). The audit will not touch those files for code changes — only read them. Note their presence; do not stash.

- [ ] **Step 0b: Create feature branch**

Run:
```bash
git checkout -b feat/whatif-math-audit
git status
```
Expected: on `feat/whatif-math-audit`, branched from `dev`.

- [ ] **Step 0c: Confirm audit start commit**

Run: `git log --oneline -1`
Expected: `697c0de docs(spec): whatif math audit — design`. Record this SHA at the top of the audit document so future readers know the codebase state being audited.

---

## Task 0: Scaffold the audit document

**Files:**
- Create: `docs/whatif-math-audit-2026-05-05.md`

- [ ] **Step 1: Create skeleton document**

Create the file with this exact content:

```markdown
# What-If Math Audit

**Date:** 2026-05-05
**Codebase audited at commit:** `<SHA from Step 0c>`
**Spec:** `docs/superpowers/specs/2026-05-05-whatif-math-audit-design.md`
**Scope:** Every numerical computation feeding the What-If page.
**Method:** Verify formulas and constants against authoritative sources; report
edge-case test coverage gaps. Audit-only — no source changes in this pass.

---

## Executive summary

_To be filled in last (Task 12). Will state functions audited, finding counts
by severity, and the top three concerns._

---

## Findings table

_To be filled in last (Task 12). Sorted by severity then area._

| ID | Sev | Area | Location | Summary |
|----|-----|------|----------|---------|

---

## 1. Federal & state income tax

_Filled in by Task 1._

## 2. Specialized federal tax surcharges (Taxable SS, NIIT, IRMAA)

_Filled in by Task 2._

## 3. Social Security

_Filled in by Task 3._

## 4. RMD

_Filled in by Task 4._

## 5. Present value & monthly compounding

_Filled in by Task 5._

## 6. Living-expense projection mechanics

_Filled in by Task 6._

## 7. Taxable account, allocation, and tax-aware withdrawals

_Filled in by Task 7._

## 8. Roth conversion math

_Filled in by Task 8._

## 9. Backtest, Monte Carlo, guardrails

_Filled in by Task 9._

## 10. Scenario chain, healthcare, budget-fit / steady-state

_Filled in by Task 10._

---

## Appendix A — Constants currency

_Filled in by Task 11._

## Appendix B — Audit method

_Filled in by Task 13._

## Appendix C — Findings ledger (running list)

_Maintained throughout. Each task that emits findings appends them here in
ID order. Task 12 reads this list to produce the findings table above._

```

- [ ] **Step 2: Commit skeleton**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): scaffold whatif math audit document"
```

---

## Task 1: Federal & state income tax

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md` — fill in section 1, append findings to ledger
- Read: `internal/services/retirement/tax.go`, `internal/services/retirement/tax_test.go`

**Functions to audit (from spec section "Math surface in scope" → area 1):**

- `CalculateFederalTax` (`tax.go:349`)
- `calculateFederalTaxOnTaxableIncome` (`tax.go:372`)
- `CalculateStateTax` (`tax.go:396`)
- `CalculateTotalTax` (`tax.go:404`)
- `CalculateTaxWithInvestmentIncome` (`tax.go:421`)
- `calculateTaxWithInvestmentIncomeInternal` (`tax.go:433`)
- `CalculateTaxWithInvestmentIncomeBreakdown` (`tax.go:429`)
- `EstimateRothConversionTax` (`tax.go:484`) [cross-listed with Task 8]
- `GetMarginalRate` (`tax.go:499`)
- `GetAdjustedBrackets` (`tax.go:181`)
- `GetAdjustedLongTermCapitalGainsBrackets` (`tax.go:210`)
- `GetAdjustedStandardDeduction` (`tax.go:238`)
- `inflationFactor` (`tax.go:251`)
- `normalizeFilingStatus` (`tax.go:258`)

**Constants to verify cell-by-cell:**

- `TaxBrackets2024` (`tax.go:38-75`)
- `LongTermCapitalGainsBrackets2024` (`tax.go:78-99`)
- `StandardDeduction2024` (`tax.go:102-107`)

- [ ] **Step 1: Read every function listed above**

For each function, take notes on: signature, what it returns, what it depends on (called functions, package vars). Use `Read` tool with line ranges. No external lookup yet — get the lay of the land first.

- [ ] **Step 2: Verify `TaxBrackets2024` against IRS Rev. Proc. 2023-34**

Source: IRS Rev. Proc. 2023-34 (published Nov 2023), §3.01 Tables 1, 3, 5, 7. The Rev. Proc. tables are denominated in dollars at the *bottom* of each bracket; the code stores `MinIncome` and `MaxIncome` at the bracket boundaries. Verify each of the 7 brackets × 4 filing statuses = 28 boundary values. Document any cell mismatch as a separate finding (`F-NNN HIGH`).

- [ ] **Step 3: Verify `LongTermCapitalGainsBrackets2024` against Rev. Proc. 2023-34 §3.03**

Source: §3.03 (Adjusted Net Capital Gain). Three brackets × four filing statuses = 12 cells. Note that these brackets apply to *taxable income including the gain*, not the gain alone — verify the implementation respects that.

- [ ] **Step 4: Verify `StandardDeduction2024` against Rev. Proc. 2023-34 §3.16**

Single, MFJ, MFS, HoH. Four cells. Rev. Proc. 2023-34 also publishes additional standard deduction for taxpayers 65+ — note whether the code accounts for that (`Pub 501`); if not, that's a finding (severity depends on whether the planner is targeting retirees, which it is — likely MEDIUM).

- [ ] **Step 5: Verify `inflationFactor` and `GetAdjustedBrackets`**

The code projects 2024 brackets forward by `(1 + inflationRate)^yearsFromBase`. Verify:

(a) The math matches the standard inflation-adjustment formula. The IRS itself uses chained CPI rounded to nearest $50/$100, not pure compounded inflation — note this divergence as INFO if applicable, not as a bug, since the planner is doing forward projection not historical reconstruction.

(b) `yearsFromBase` is computed correctly from `BaseYear` to the projection year.

(c) The function preserves bracket *rates* (rates do not inflate; only edges do).

- [ ] **Step 6: Worked example — single filer with $80,000 ordinary income, no investment income, year-0**

Compute by hand using TY2024 single brackets and standard deduction $14,600:

- Taxable income: $80,000 − $14,600 = $65,400
- Tax: 10% × $11,600 + 12% × ($47,150 − $11,600) + 22% × ($65,400 − $47,150)
- = $1,160 + $4,266 + $4,015 = $9,441

Reproduce by writing a small Go test (in a temporary `_audit_test.go` that you do NOT commit) or by constructing the call directly. Confirm `CalculateFederalTax(80000, 0)` for a Single filer returns ($9,441, effective ≈ 11.80%, marginal 22%) within ±$0.01.

If it disagrees, that's HIGH and recorded with both numbers.

- [ ] **Step 7: Worked example — MFJ with $200,000 ordinary, $20,000 LTCG, year-0**

Stacked computation: ordinary tax stacks first, then LTCG sits on top.

- Std deduction: $29,200
- Taxable income: $220,000 − $29,200 = $190,800
- Ordinary portion: $170,800 (MFJ brackets: 10% × $23,200 + 12% × $71,100 + 22% × $76,500) = $2,320 + $8,532 + $16,830 = $27,682
- LTCG sits on top from $170,800 to $190,800. The MFJ 0% LTCG bracket runs to $94,050; 15% bracket runs to $583,750. So all $20,000 of LTCG is taxed at 15% = $3,000.
- Federal tax total: $30,682.

Confirm `CalculateTaxWithInvestmentIncome(170800, 0, 20000, 0)` for MFJ produces $30,682 ± $0.01. Document if otherwise.

- [ ] **Step 8: Verify state tax**

Source: none external; verify it's a flat percentage of taxable income. Note in finding (INFO) that state tax is a single flat rate — does not handle progressive state brackets. This is a known simplification, not a bug, but worth documenting so a reader doesn't expect more.

- [ ] **Step 9: Test-coverage gap analysis for area 1**

Read `tax_test.go`. For each function in this area, run through the boundary checklist. Specifically check:

- Does any test exercise the exact bracket boundary (e.g., taxable income = $11,600 to test the 10%/12% transition)?
- Is each filing status tested for `CalculateFederalTax`?
- Is `inflationFactor` tested with `yearsFromBase=0` (factor must equal 1)?
- Is negative income tested? What does the code do with it?
- Is income above the top bracket tested?

List gaps as a single MEDIUM or LOW finding (one finding per function with gaps; don't bundle).

Also: spot-check `TestCalculateFederalTax_*` for divergence from the worked examples in Steps 6-7 — if existing tests assert different numbers, that's a flag.

- [ ] **Step 10: Write section 1 of the audit document**

Append after the `## 1. Federal & state income tax` header. Structure:

```markdown
### Functions audited

| Function | Status |
|----------|--------|
| `CalculateFederalTax` | PASS |
| `calculateFederalTaxOnTaxableIncome` | PASS |
| ... | ... |

### Constants verified

| Table | Cells checked | Mismatches |
|-------|---------------|------------|
| `TaxBrackets2024` | 28 | 0 |
| `LongTermCapitalGainsBrackets2024` | 12 | 0 |
| `StandardDeduction2024` | 4 | 0 |

### Worked examples

#### WE-1.1: Single, $80,000 ordinary, year-0
[show computation, source, expected, actual, delta]

#### WE-1.2: MFJ, $170,800 ordinary + $20,000 LTCG, year-0
[show computation, source, expected, actual, delta]

### Findings

[Findings emitted by this section, by ID. If none, state "No findings."]
```

Append all findings to Appendix C — Findings ledger in ID order.

- [ ] **Step 11: Commit**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): area 1 — federal & state income tax"
```

---

## Task 2: Specialized federal tax surcharges

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md` — fill in section 2, append findings to ledger
- Read: `internal/services/retirement/tax.go` (lines 267-345 + table definitions)

**Functions to audit:**

- `CalculateTaxableSocialSecurity` (free function, `tax.go:267`)
- `(tc *TaxCalculator) CalculateTaxableSocialSecurity` (method, `tax.go:335`)
- `CalculateNIIT` (free function, `tax.go:292`)
- `(tc *TaxCalculator) CalculateNIIT` (method, `tax.go:339`)
- `CalculateMonthlyIRMAA` (free function, `tax.go:308`)
- `(tc *TaxCalculator) CalculateMonthlyIRMAA` (method, `tax.go:343`)
- Helper: `resolveIRMALookbackMAGI` (`calculator.go:288`)
- Helper: `medicareEligibleAdultCountAtYear` (`calculator.go:317`)
- Helper: `plannerIRMAAInflationFactorForYear` (`calculator.go:339`)

**Constants:**

- `socialSecurityTaxThresholds` (`tax.go:109-113`)
- `niitThresholds` (`tax.go:115-120`)
- `monthlyIRMAASurcharge2026` (`tax.go:124-157`)

- [ ] **Step 1: Read every function and the IRS Pub 915 worksheet**

For taxable Social Security: the formula in IRC § 86 has two tiers (50% and 85%). Walk through the code's branch structure and confirm it implements both tiers correctly.

- [ ] **Step 2: Verify `socialSecurityTaxThresholds` against 26 USC § 86**

§ 86(c)(1): base ($25,000 single / $32,000 MFJ) — first tier; § 86(c)(2): adjusted base ($34,000 single / $44,000 MFJ) — second tier. Verify the four cells in the map. Note: code does not include MFS — verify whether MFS taxpayers who lived with spouse default to $0 threshold per § 86(c)(2)(B). If the code silently uses single-filer thresholds for MFS, that's at least MEDIUM.

- [ ] **Step 3: Verify `BaseTaxableAmount` field in `socialSecurityTaxThresholds`**

`BaseTaxableAmount` is $4,500 (single) and $6,000 (MFJ). This is the value of 50% of (UpperThreshold − BaseThreshold) plus the bridging amount. Derive the source-defined formula from § 86(b) and confirm `$4,500 = 50% × ($34,000 − $25,000) = $4,500` ✓ and `$6,000 = 50% × ($44,000 − $32,000) = $6,000` ✓. Document.

- [ ] **Step 4: Worked example — taxable SS for MFJ with $50,000 other income + $30,000 SS**

Source: IRS Pub 915 worksheet. Hand-compute:

1. Combined income (provisional): other income + ½ SS = $50,000 + $15,000 = $65,000
2. Both thresholds exceeded ($65,000 > $44,000), so second tier applies.
3. Amount over upper threshold: $65,000 − $44,000 = $21,000 × 85% = $17,850
4. Amount between thresholds: ($44,000 − $32,000) × 50% = $6,000
5. Plus 85% of SS that is over second tier amount? No — taxable SS is min of:
   - 85% of SS = $25,500
   - $17,850 + min($6,000, 50% of SS = $15,000) = $17,850 + $6,000 = $23,850

Final taxable SS = min($25,500, $23,850) = $23,850.

Confirm `CalculateTaxableSocialSecurity(30000, 50000, 0, 0)` for MFJ returns $23,850 ± $0.01.

- [ ] **Step 5: Verify NIIT**

Source: 26 USC § 1411 + IRS Pub 550. NIIT is 3.8% × min(net investment income, MAGI − threshold). Verify:

(a) Rate is 3.8%.
(b) Threshold is correct per filing status (single $200K, MFJ $250K, MFS $125K, HoH $200K).
(c) Output is zero when MAGI ≤ threshold.

NIIT thresholds are *not* inflation-adjusted by statute. Verify the code does not inflate them.

- [ ] **Step 6: Worked example — NIIT for MFJ with $300,000 MAGI, $40,000 net investment income**

- Excess over threshold: $300,000 − $250,000 = $50,000
- Lesser of $40,000 or $50,000 = $40,000
- NIIT: 3.8% × $40,000 = $1,520

Confirm `CalculateNIIT(300000, 40000, MFJ)` returns $1,520 ± $0.01.

Edge case: if NII = $60,000 and excess = $40,000, NIIT should be 3.8% × $40,000 = $1,520 (NOT 3.8% × $60,000). Test this branch explicitly.

- [ ] **Step 7: Verify `monthlyIRMAASurcharge2026`**

Source: CMS 2026 Medicare Part B and Part D IRMAA tables. Each tier in the code combines Part B + Part D add-on (e.g., `81.20 + 14.50` for tier 1). Verify each filing-status row cell-by-cell.

Key check: the published CMS values are *per-person, per-month*. The planner applies them per-eligible-adult via `medicareEligibleAdultCountAtYear`. Verify that scaling.

- [ ] **Step 8: Verify IRMAA inflation rescaling**

The code base year is `irmaaBaseYear = 2026`, but tax `taxBaseYear = 2024`. The comment in `tax.go:122-123` says "rescales them onto the tax model's 2024 base year." Verify:

(a) The rescaling math is `irmaa_threshold × (1 + inflation)^(2026 − 2024)` to project a 2024-base-dollar threshold up to 2026 actual? Or the inverse? Read carefully.

(b) `plannerIRMAAInflationFactorForYear` projects forward from base year. Confirm the year-zero value is exactly 1.0 (no spurious one-year offset).

(c) The two-year offset between `taxBaseYear` and `irmaaBaseYear` is handled correctly so the rescaling does not double-count or miss inflation. Construct a worked example: MAGI of $100,000 in projection year 0, MFJ filing. Trace the IRMAA tier selection step by step and confirm the right tier is picked.

- [ ] **Step 9: Verify lookback MAGI**

Source: 42 USC § 1395r-1 (IRMAA two-year lookback rule). Verify `resolveIRMALookbackMAGI` selects MAGI from 2 years prior. If the projection is shorter than 2 years from current, `assumedIRMALookbackMAGI` is used — confirm this fallback is applied correctly.

- [ ] **Step 10: Verify Medicare-eligible-adult counting**

`medicareEligibleAdultCountAtYear` should return 0, 1, or 2 based on whether each adult has reached 65 in that projection year. Boundary: an adult who turns 65 mid-year — does the code use age at start of year, end of year, or month-by-month? Document.

- [ ] **Step 11: Test-coverage gap analysis for area 2**

Read `tax_test.go`, `rmd_tax_test.go`, and any `*_test.go` that calls these functions. Boundary checks specific to this area:

- Taxable SS at exact lower threshold ($25,000 / $32,000)
- Taxable SS at exact upper threshold ($34,000 / $44,000)
- NIIT at MAGI = threshold (must return 0)
- NIIT with NII < excess (lesser-of branch)
- IRMAA at exact tier boundaries (just-below, just-above)
- IRMAA when neither adult is Medicare-eligible (count = 0)
- IRMAA in a year before lookback MAGI is available

- [ ] **Step 12: Write section 2 of the audit document**

Use the same structure as Task 1 Step 10. Append findings to Appendix C.

- [ ] **Step 13: Commit**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): area 2 — taxable SS, NIIT, IRMAA"
```

---

## Task 3: Social Security

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md` — fill in section 3, append findings to ledger
- Read: `internal/services/retirement/social_security.go`, `social_security_test.go`

**Functions to audit (from spec area 3):**

- `validSSClaimAge`, `normalizedSSFRA`, `normalizedSSCOLARate`
- `AdjustedSSBenefit`
- `DerivedPIA`
- `AdjustedSpousalBenefit`
- `SpousalTopUp`
- `claimStartMonth`
- `projectedSSBenefitForMonth`
- `projectedSocialSecurityIncome`
- `ProjectedSSEntries`
- `SSComparisonTable`, `ssComparisonTable`
- `SSBreakevenAges`, `ssBreakevenAges`
- `cumulativeBenefit`
- `RunSSAnalysis`
- `RunSSPortfolioAnalysis` (decision rule only)
- `bestSSPortfolioOption`, `isBetterSSPortfolioOption`

- [ ] **Step 1: Verify `AdjustedSSBenefit` against SSA POMS RS 00615.105 + RS 00615.690**

Early retirement reduction (POMS RS 00615.105):
- Months 1-36 before FRA: reduce by 5/9 of 1% per month (= 6.667% per year of first 3 years)
- Months 37+ before FRA: additional reduction of 5/12 of 1% per month (= 5% per year for years 4-5)

Delayed retirement credits (POMS RS 00615.690):
- Born 1943 or later: 8% per year (2/3 of 1% per month) for each year past FRA up to age 70.

Read `AdjustedSSBenefit` and verify both directions match. Construct worked examples in Step 4 below.

- [ ] **Step 2: Verify `DerivedPIA` is the inverse of `AdjustedSSBenefit`**

Property: `DerivedPIA(AdjustedSSBenefit(pia, fra, claimAge), fra, claimAge) == pia` for any valid `(pia, fra, claimAge)`. If not, that's HIGH (a known SS feature is broken).

- [ ] **Step 3: Verify `AdjustedSpousalBenefit` and `SpousalTopUp` against POMS RS 00615.020**

Spousal benefit at FRA = 50% of higher earner's PIA. Reduction for early claim:
- Up to 36 months before spouse's FRA: 25/36 of 1% per month
- Beyond 36 months: additional 5/12 of 1% per month

Note: spousal benefits do NOT receive delayed retirement credits — verify the code returns 50% PIA at any age ≥ FRA, not more.

`SpousalTopUp` is the additional benefit a lower-earning spouse receives on top of their own benefit. Verify: top-up = max(0, AdjustedSpousalBenefit(higherPIA × 0.5, spouseFRA, claimAge) − spouseOwnBenefit). Whether the spousal portion uses the higher earner's claim age or the spouse's claim age varies by SSA rule — read carefully and document.

- [ ] **Step 4: Worked examples**

WE-3.1: PIA = $2,000, FRA = 67, claim age 62.
- Months early = 5 × 12 = 60
- First 36 months reduction: 36 × (5/9 × 1%) = 20%
- Next 24 months reduction: 24 × (5/12 × 1%) = 10%
- Total reduction: 30%
- Adjusted benefit: $2,000 × 0.70 = $1,400

(Cross-check: SSA publishes 70% as the age-62 benefit ratio for FRA-67 cohorts.)

Confirm `AdjustedSSBenefit(2000, 67, 62)` returns $1,400 ± $0.01.

WE-3.2: PIA = $2,000, FRA = 67, claim age 70.
- Months delayed = 3 × 12 = 36
- DRC = 36 × (2/3 × 1%) = 24%
- Adjusted benefit: $2,000 × 1.24 = $2,480

Confirm `AdjustedSSBenefit(2000, 67, 70)` returns $2,480 ± $0.01.

WE-3.3: Spousal at FRA, higher PIA = $3,000, spouse FRA = 67, spouse claim 62.
- Spousal at FRA: $1,500
- Months early = 60
- First 36 months: 36 × (25/36 × 1%) = 25%
- Next 24 months: 24 × (5/12 × 1%) = 10%
- Total reduction: 35%
- Adjusted spousal: $1,500 × 0.65 = $975

Confirm `AdjustedSpousalBenefit(1500, 67, 62)` returns $975 ± $0.01.

- [ ] **Step 5: Verify COLA compounding (`projectedSSBenefitForMonth`)**

The April 11 review noted a fix: COLA must compound from the *same calendar year* for both benefits being compared in breakeven, not from each benefit's own claim date. Verify the current implementation:

(a) Reads `monthsSinceClaim` and applies COLA per *calendar year* boundary, not per 12-month-from-claim anniversary.

(b) When comparing two claim ages in `ssBreakevenAges`, both benefits use the same time-zero for COLA application.

If either is wrong, that's HIGH.

- [ ] **Step 6: Verify SS projection logic**

`projectedSocialSecurityIncome` aggregates benefits for primary + spouse + spousal-top-up. Verify:

(a) Primary benefit only flows after `claimStartMonth(currentAge, claimAge)` has elapsed.
(b) Spouse benefit flows independently of primary's claim age.
(c) Spousal top-up flows only if spouse has claimed AND higher earner has claimed (top-up depends on higher earner's PIA being established, not their claim — but verify against POMS RS 00615.020 and document).

- [ ] **Step 7: Verify `SSBreakevenAges`**

Breakeven age is the age at which cumulative benefits from claiming earlier equal cumulative benefits from claiming later. Test with two simple scenarios:

WE-3.4: PIA = $2,000, FRA = 67, no COLA (cola = 0). Compare claim 62 vs claim 70.
- Claim 62: monthly $1,400 (30% reduction). Annual: $16,800.
- Claim 70: monthly $2,480 (24% DRC). Annual: $29,760.
- Breakeven equation: 16,800 × (T − 62) = 29,760 × (T − 70)
- → 16,800T − 1,041,600 = 29,760T − 2,083,200
- → 12,960T = 1,041,600
- → T ≈ 80.4 → ~age 80-81.

Verify `SSBreakevenAges(2000, 67, 0)` produces a value near 80-81 for the
62-vs-70 comparison. ±2 years of difference is acceptable depending on
whether the implementation uses month-by-month vs annual aggregation, or
end-of-year vs start-of-year alignment — but anything outside age 78-82
is a finding worth investigating.

- [ ] **Step 8: Verify SS optimizer decision rule**

`bestSSPortfolioOption` and `isBetterSSPortfolioOption` pick the best among Monte Carlo results. Verify:

(a) The decision rule is consistent (e.g., highest median portfolio, or highest success rate, or lex by both — read source and document).

(b) The Monte Carlo internals are NOT in scope for this task — that's covered in Task 9. Only verify the picker's tie-breaking and ordering.

- [ ] **Step 9: Test-coverage gap analysis for area 3**

Read `social_security_test.go`. Boundary checks:

- Claim age = 62 (earliest valid)
- Claim age = 70 (latest with DRC)
- Claim age = FRA (no adjustment)
- Claim age below 62 (invalid; what does code return?)
- Claim age above 70 (no further DRC)
- FRA = 65 (older birth cohort) — does the formula handle non-67 FRA?
- COLA = 0 (flat benefits)
- COLA = some negative number (policy: SS COLA can be 0 but never negative — confirm)
- Spouse same age as primary, both claim same year
- Spouse much younger than primary (spousal top-up timing)
- Roundtrip: `DerivedPIA ∘ AdjustedSSBenefit == identity`

- [ ] **Step 10: Write section 3 of the audit document**

Same structure. Append findings.

- [ ] **Step 11: Commit**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): area 3 — Social Security"
```

---

## Task 4: RMD

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md` — section 4
- Read: `internal/services/retirement/rmd.go`, `rmd_tax_test.go`

**Functions:**

- `GetLifeExpectancyFactor` (`rmd.go:64`)
- `CalculateRMD` (`rmd.go:76`)
- `CalculateRMDAnalysis` (`rmd.go:87`)

**Constants:**

- The Uniform Lifetime Table embedded in `rmd.go`

**Important context:** `internal/services/retirement/rmd.go` has uncommitted changes in the working tree. The audit reads the file as-it-is now and notes "audited at uncommitted state" if the changes are non-trivial. The audit does not stash or revert.

- [ ] **Step 1: Read `rmd.go` in full**

Note line counts and uncommitted modifications.

- [ ] **Step 2: Verify Uniform Lifetime Table cell-by-cell against IRS Pub 590-B (post-2022)**

Source: IRS Pub 590-B, Appendix B, Table III (Uniform Lifetime). The table runs ages 72 (or 73 under SECURE 2.0) through 120+. Verify every age-factor pair the code embeds. Even one off-by-one cell is HIGH because RMDs are legally required and a wrong factor under-withdraws or over-withdraws.

- [ ] **Step 3: Verify SECURE 2.0 RMD start age**

SECURE 2.0 (enacted Dec 2022) raised RMD start to age 73 starting 2023, with a future bump to 75 starting 2033. Verify:

(a) The code uses 73 (not 70½ or 72) as the start age for projections starting after 2023.

(b) If the planner allows projection start years past 2033, the start age is 75 from then on.

(c) If neither is conditional on year, document this as MEDIUM — projections starting in 2034+ should treat RMD start as 75.

- [ ] **Step 4: Verify `CalculateRMD` formula**

Per Pub 590-B: `RMD = balance / life_expectancy_factor`. Verify the code returns:

- `amount = balance / factor`
- `percent = 1 / factor` (the implied withdrawal rate)

If the code returns `amount = balance × factor` (multiplication instead of division), that's HIGH and the bug pre-dates the audit.

- [ ] **Step 5: Worked example — RMD at age 73 with $1,000,000 balance**

Pub 590-B Table III factor at age 73 = 26.5.
- RMD: $1,000,000 / 26.5 = $37,735.85
- Percent: 1 / 26.5 = 3.7736%

Confirm `CalculateRMD(1_000_000, 73)` returns ($37,735.85, 0.037736) within tolerance.

- [ ] **Step 6: Verify `CalculateRMDAnalysis`**

Read the function. It's a Calculator method that produces an `RMDAnalysis` struct for the projection. Verify:

(a) RMD only applies to tax-deferred balance, not Roth or taxable.
(b) RMD is calculated annually and divided into 12 monthly draws (or whatever cadence the planner uses).
(c) The first RMD year aligns with RMD start age (73 / 75).

- [ ] **Step 7: Test-coverage gap analysis for area 4**

Read `rmd_tax_test.go`. Boundary checks:

- Age = 72 (no RMD)
- Age = 73 (first RMD year under SECURE 2.0)
- Age = 75 (boundary for future bump)
- Age = 100+ (table edge)
- Age above table (does the code use the last factor or panic?)
- Balance = 0 (RMD = 0)
- Balance = negative (handled gracefully?)
- Projection that crosses the RMD start year mid-projection

- [ ] **Step 8: Write section 4, commit**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): area 4 — RMD"
```

---

## Task 5: Present value & monthly compounding

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md` — section 5
- Read: `calculator.go` (PV/compound functions), `calculator_pv_test.go`

**Functions:**

- `PresentValue` (`calculator.go:38`)
- `PresentValueAnnuity` (`calculator.go:51`)
- `monthlyCompoundFactorFromDecimal` (`calculator.go:135`)
- `monthlyCompoundFactorFromPercent` (`calculator.go:142`)
- `compoundedFactorFromPercent` (`calculator.go:146`)
- `fractionalMonthlyReturn` (`calculator.go:607`)
- `plannerInflationFactorForYear` (`calculator.go:332`)
- `plannerIRMAAInflationFactorForYear` (`calculator.go:339`) [cross-listed with Task 2]
- `calculateHealthcarePV` (`calculator.go:95`)

**Important context:** `internal/services/retirement/calculator.go` has uncommitted changes in the working tree. Audit reads current state.

- [ ] **Step 1: Verify `PresentValue` formula**

Standard formula: `PV = FV / (1 + r)^n`. Read the code; confirm it matches. Edge cases:

- `r = 0`: PV = FV
- `n = 0`: PV = FV
- `r < 0`: PV > FV (unusual but mathematically valid)

- [ ] **Step 2: Verify `PresentValueAnnuity`**

Formula for a growing annuity discounted at rate `r` with growth `g`, paying `payment` per period for `n` periods starting at month `startMonth`:

`PV = payment × ((1 + g) / (1 + r))^startMonth × (1 − ((1 + g) / (1 + r))^n) / (r − g)`

This is the closed-form for a deferred growing annuity. The code likely operates monthly, so `r` and `g` should be monthly. Read the code carefully; verify:

(a) Monthly conversion: `monthlyR = (1 + annualR)^(1/12) − 1` (or similar — confirm).
(b) The `r = g` degenerate case (denominator zero) is handled.
(c) The `startMonth = 0` case reduces to the standard immediate annuity formula.

- [ ] **Step 3: Worked example — PV of $1,000/month growing at 3%/year, discounted at 5%/year, for 30 years (360 months), starting immediately**

Annual PV closed form (approximation):
- Real rate ≈ 5% − 3% ≈ 2%
- PV ≈ ($12,000 × ((1 − (1.03/1.05)^30) / (0.05 − 0.03))) ≈ $12,000 × 22.4 ≈ $268,800

Compute via the code's monthly path and check it lands within ~1-2% of the annual approximation. (Tighter tolerance is achievable but requires hand-computing the monthly rates exactly, which Step 4 below does.)

- [ ] **Step 4: Worked example — exact, both conventions**

Read the code first to determine which monthly-rate convention is used.

**Convention A (geometric):** `r_m = (1 + r_annual)^(1/12) − 1`. For r=5%, g=3%:
- r_m ≈ 0.00407412, g_m ≈ 0.00246627
- ratio = (1+g_m)/(1+r_m) ≈ 0.99839929
- ratio^360 ≈ 0.56172
- PV ≈ 1000 × (1 − 0.56172) / (r_m − g_m) ≈ 1000 × 0.43828 / 0.00160785 ≈ **$272,591**

**Convention B (arithmetic shortcut):** `r_m = r_annual / 12`. For r=5%, g=3%:
- r_m = 0.0041667, g_m = 0.0025
- ratio = 1.0025 / 1.0041667 ≈ 0.99834
- ratio^360 ≈ 0.55014
- PV ≈ 1000 × (1 − 0.55014) / (0.0041667 − 0.0025) ≈ 1000 × 0.44986 / 0.0016667 ≈ **$269,917**

Confirm `PresentValueAnnuity(1000, 0.05, 0.03, 0, 360)` matches the
convention used by the code within ±$1.00. The convention itself becomes
a finding (INFO if convention is consistent across the codebase, MEDIUM
if `PresentValueAnnuity` uses one convention and `monthlyCompoundFactor*`
uses another — that's a real risk because portfolio growth and PV would
discount at slightly different rates).

- [ ] **Step 5: Verify `compoundedFactorFromPercent` and `fractionalMonthlyReturn`**

`compoundedFactorFromPercent(annualPercent, months)`: returns `(1 + annualPercent/100)^(months/12)`. Confirm.

`fractionalMonthlyReturn(monthlyReturn, fraction)`: this likely returns `(1 + monthlyReturn)^fraction − 1`. The fraction is for partial-month projection timing (start vs end of month). Verify the formula and document.

- [ ] **Step 6: Verify `plannerInflationFactorForYear`**

`(1 + inflationRate)^years`. Verify year-zero returns 1.0 exactly. Verify that fractional years (e.g., 0.5 for mid-year) compound correctly.

- [ ] **Step 7: Verify `calculateHealthcarePV`**

Healthcare cost is monthly; PV must aggregate monthly costs over `totalMonths` discounted at `discountRate`. Plus the monthly cost itself may inflate (medical inflation might differ from general — read the code). Verify:

(a) Monthly aggregation matches `PresentValueAnnuity`-style formula.
(b) If the cost inflates at general inflation rate (not separate medical inflation), document — the audit doesn't propose adding medical inflation, just notes the simplification.
(c) Eligibility transitions (Medicare at 65, ACA before) — does PV account for differing costs in different age bands? If a person transitions from ACA to Medicare mid-projection, both PV pieces should sum correctly.

- [ ] **Step 8: Test-coverage gap analysis**

Read `calculator_pv_test.go`. Boundary checks:

- `PresentValueAnnuity` with `r = g` (degenerate case)
- `PresentValueAnnuity` with `n = 0`
- `PresentValueAnnuity` with `startMonth > 0` (deferred annuity)
- `PresentValue` with `r = 0`
- Inflation factor for `years = 0` (must equal 1.0)
- Healthcare PV transitioning ACA → Medicare mid-projection

- [ ] **Step 9: Write section 5, commit**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): area 5 — present value & monthly compounding"
```

---

## Task 6: Living-expense projection mechanics

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md` — section 6
- Read: `calculator.go:153-211`, `calculator_expense_test.go`

**Functions:**

- `calculateLivingExpensesAtMonth` (`calculator.go:153`)
- `rebaseLivingExpensesAtTransition` (`calculator.go:165`)
- Spending-phase application logic (inline in projection loop — find via gitnexus or grep for `SpendingPhases`)
- `CalculateTotalExpenses` (`calculator.go:548`)

- [ ] **Step 1: Read every function**

Map out: how does the planner go from "$10,000/month base, 3% inflation, Slow-Go phase 85% from age 75" to a specific dollar amount in projection month 240?

- [ ] **Step 2: Verify inflation compounding inside the projection loop**

Two valid conventions:

(a) Compound inflation continuously: living-expense at month M = base × (1 + inflation)^(M/12).

(b) Compound inflation per phase: at each phase transition, rebase the new phase's multiplier off the inflated value at that month, then continue compounding from there.

Convention (b) is what `rebaseLivingExpensesAtTransition` suggests. Verify the rebasing math is correct: `new_base = old_base × cumulative_inflation × phase_multiplier`. If a phase change incorrectly resets `cumulative_inflation` to 1, that's HIGH.

- [ ] **Step 3: Verify spending-phase boundary handling**

If a phase starts at age 75, on the user's 75th birthday month or the start of the calendar year of age 75? Read the code's age-to-month conversion. Document.

Off-by-one risk: phase[i] active for ages [phase[i].startAge, phase[i+1].startAge). Verify the half-open interval is consistent.

- [ ] **Step 4: Worked example**

Inputs: base expenses $10,000/month, inflation 3%/year, 3 phases (Go-Go 100% from age 65, Slow-Go 85% from age 75, No-Go 75% from age 85), current age 65.

- Month 0 (age 65): $10,000 × 1.0 = $10,000.
- Month 60 (age 70): $10,000 × 1.03^5 ≈ $11,593.
- Month 120 (age 75): phase change to Slow-Go. Hand off: pre-transition value = $10,000 × 1.03^10 ≈ $13,439. New phase value = $13,439 × 0.85 = $11,423. From here forward, compound at 3% from this base.
- Month 121 (age 75 + 1mo): $11,423 × 1.03^(1/12) ≈ $11,451.

Verify the code produces these values (or comparable, depending on convention).

- [ ] **Step 5: Test-coverage gap analysis**

Read `calculator_expense_test.go`. Boundary checks:

- Phase transitions happening exactly on month 0 of a year vs mid-year
- Single-phase scenarios (no transitions)
- 0-phase scenarios (does the code error gracefully?)
- Phase multiplier = 0 (no spending — does the projection survive?)
- Phase multiplier > 1 (more spending later — unusual but valid)

- [ ] **Step 6: Write section 6, commit**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): area 6 — living-expense projection mechanics"
```

---

## Task 7: Taxable account, allocation, and tax-aware withdrawals

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md` — section 7
- Read: `calculator.go:447-823`, `taxable_simulation_test.go`, `calculator_test.go` (allocation tests)

**Functions:**

- `newTaxableAccountState`
- `taxableAccountState.addCash`, `withdraw`, `applyGrowth`, `syncAssumptions`
- `buildTaxableReturnComponents`
- `expectedTaxableMonthlyCashFlow`
- `executeTaxAwarePortfolioMonth`
- `executePortfolioCashFlowWithTaxableState`
- `reinvestRequiredRMDToTaxableState`
- `applyBigTicketExpenseWithTaxableState`
- `projectionTimingGrowthFractions`
- `taxDeferredDelayActive`, `shortfallIsTemporaryDueToDelay`
- Per-account allocation and glide-path math (enumerate via `gitnexus_context` or `grep` for `StockAllocation`, `CashAllocation`, glide-path identifiers)

- [ ] **Step 1: Enumerate per-account allocation / glide-path functions**

Run: `grep -nE "StockAllocation|CashAllocation|GlidePath|glide" internal/services/retirement/calculator.go internal/services/retirement/settings.go`

Or use `gitnexus_query` with query "glide path stock allocation". List the functions found and add them to the area's audit list.

- [ ] **Step 2: Verify cost-basis tracking on partial withdrawals**

For a taxable account with cost basis $X and market value $Y > X:
- A withdrawal of $W draws cash and triggers a realized gain proportional to the unrealized fraction.
- Pro rata: realized gain = $W × ($Y − $X) / $Y.
- New basis after withdrawal = $X − ($W × $X / $Y).

Verify `taxableAccountState.withdraw` implements this. If the code uses LIFO or FIFO accounting, document — pro-rata is the simplest and most common for a planner; the audit doesn't insist on a specific method but does require the chosen method to be self-consistent.

Edge case: $W > $Y (withdraw more than market value). What does the code do — clip to $Y, return error, allow negative? Document.

- [ ] **Step 3: Verify dividend / qualified / cap-gains distribution split**

`buildTaxableReturnComponents` decomposes total annual return into:
- Total dividend yield × qualified share = qualified dividends
- Total dividend yield × (1 − qualified share) = non-qualified dividends
- Cap-gains distribution rate × balance = realized gains (taxed at LTCG)
- Remainder = unrealized appreciation

Verify the four components sum to total return (within floating-point tolerance). If they don't, that's HIGH (lost money in the math).

- [ ] **Step 4: Verify tax-aware withdrawal ordering**

Read `executeTaxAwarePortfolioMonth`. The standard tax-aware order is: taxable → tax-deferred → Roth (or some Roth-protective variant). Verify:

(a) The order is documented in code or settings.

(b) When `taxDeferredDelayActive`, tax-deferred is skipped and the planner draws from taxable / Roth only.

(c) Big-ticket expenses with the early-withdrawal penalty: read `applyBigTicketExpenseWithTaxableState`. The 10% penalty applies to tax-deferred withdrawals before age 59.5 — verify the penalty rate and age threshold against IRC § 72(t).

- [ ] **Step 5: Verify projection-timing fractions**

`projectionTimingGrowthFractions(timing)` returns `(before, after)` fractions for a single month, summing to 1.0. Verify:

(a) `start_of_month`: before=0, after=1 (full month of growth after activity).
(b) `end_of_month`: before=1, after=0 (full month of growth before activity).
(c) `mid_month`: before=0.5, after=0.5.

If the code defines additional timing modes, verify each.

- [ ] **Step 6: Verify RMD reinvestment**

`reinvestRequiredRMDToTaxableState`: when an RMD is forced but cash is not needed, the after-tax amount should be reinvested in the taxable account at current market price (basis = post-tax amount). Verify the basis tracking is correct.

- [ ] **Step 7: Verify per-account allocation + glide-path interpolation**

Glide path linearly (or by some curve — read the code) shifts stock allocation from `glideStartStock%` at glide start year to `glideEndStock%` at glide end year. Verify:

(a) Linear interpolation: `stock(y) = startStock + (endStock − startStock) × (y − startYear) / (endYear − startYear)` clipped to `[startYear, endYear]`.

(b) Outside the glide window, stock allocation is held at start or end value.

(c) Cash allocation = 1 − stock allocation? Or is there a separate bond bucket? Document the model.

- [ ] **Step 8: Test-coverage gap analysis**

Read `taxable_simulation_test.go` and the allocation-related tests. Boundary checks:

- Withdraw exactly = market value (basis must go to 0)
- Withdraw with zero unrealized gain (cost basis = market value)
- Glide-path year before start (must equal start allocation)
- Glide-path year after end (must equal end allocation)
- Glide-path year exactly at start
- Glide-path year exactly at end
- Big-ticket withdrawal under age 59.5 (penalty applies)
- Big-ticket withdrawal at exactly age 59.5 (boundary)
- Tax-deferred delay year that ends mid-projection
- Distribution components not summing to 1 (insurance test — should not happen, but if input rates are pathological, what does the code do?)

- [ ] **Step 9: Write section 7, commit**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): area 7 — taxable account, allocation, tax-aware withdrawals"
```

---

## Task 8: Roth conversion math

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md` — section 8
- Read: `calculator.go:411-446` (rothConversionAmountForYear), `tax.go:484-498` (EstimateRothConversionTax), conversion application in projection loop, `coverage_gaps_test.go` (likely covers Roth)

**Functions:**

- `rothConversionAmountForYear` (`calculator.go:411`)
- `EstimateRothConversionTax` (`tax.go:484`)
- Conversion application in projection loop (find via `grep` for `RothConversion`)

- [ ] **Step 1: Verify `rothConversionAmountForYear`**

Reads `Settings.RothConversion` (annual amount, start year, end year) and returns the conversion amount for the current year, capped at available tax-deferred balance.

Verify:
- Year < startYear or year > endYear: returns 0.
- Year ≥ startYear and year ≤ endYear: returns min(annualAmount, availableTaxDeferred).
- Negative annual amount: April 11 review fix — handler now rejects negatives, but the calculator path: if a malformed value somehow arrives, what happens? Should still return ≥ 0.

- [ ] **Step 2: Verify `EstimateRothConversionTax`**

The conversion is treated as ordinary income on top of base income. Tax = `Tax(baseIncome + conversion) − Tax(baseIncome)`. Verify the formula is exactly that (marginal tax on the conversion).

If the code instead returns `Tax(baseIncome + conversion) × (conversion / (baseIncome + conversion))` (a proportional approach), that's wrong — Roth conversion is taxed marginally, not pro-rata.

- [ ] **Step 3: Worked example**

MFJ, $80,000 base ordinary income, $50,000 Roth conversion, year-0 brackets.

- Tax on $80,000 (taxable = $50,800 after $29,200 std deduction):
  - 10% × $23,200 = $2,320
  - 12% × ($50,800 − $23,200) = 12% × $27,600 = $3,312
  - Total: $5,632
- Tax on $130,000 (taxable = $100,800):
  - 10% × $23,200 + 12% × ($94,300 − $23,200) + 22% × ($100,800 − $94,300)
  - = $2,320 + $8,532 + $1,430 = $12,282
- Conversion tax: $12,282 − $5,632 = $6,650

Confirm `EstimateRothConversionTax(80000, 50000, 0)` for MFJ returns $6,650 ± $0.01.

- [ ] **Step 4: Verify conversion application in projection loop**

When conversion happens:
- Tax-deferred balance decreases by conversion amount.
- Roth balance increases by conversion amount.
- Cash for tax payment comes from somewhere — taxable account? Or grossed-up from tax-deferred? Read the code and document.

Common bug: conversion increases Roth by full amount AND tax is deducted from Roth, double-counting. Verify this is not happening.

- [ ] **Step 5: Test-coverage gap analysis**

Boundary checks:

- Conversion amount > available tax-deferred (cap behavior)
- Conversion year = startYear (first year)
- Conversion year = endYear (last year)
- Conversion year between start and end with zero balance
- Conversion that pushes filer into next bracket
- Conversion when MAGI crosses NIIT or IRMAA threshold (does the planner reflect the surcharge in projection cost?)

- [ ] **Step 6: Write section 8, commit**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): area 8 — Roth conversion math"
```

---

## Task 9: Backtest, Monte Carlo, guardrails

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md` — section 9
- Read: `backtest.go`, `backtest_test.go`, `guardrails.go`, `guardrails_test.go`, `historical_data.go`

**Areas:**

(a) Monte Carlo simulation: random return draws + statistical aggregation
(b) Historical backtest: rolling-window historical returns
(c) Guyton-Klinger guardrails

- [ ] **Step 1: Verify Monte Carlo return distribution assumption**

Read the Monte Carlo loop. Common assumptions:

- Lognormal returns (geometric Brownian motion): `r = exp(μ + σZ) − 1` where Z ~ N(0,1).
- Normal returns: `r = μ + σZ`.

For long-horizon retirement projections, lognormal is more defensible (returns can't go below −100%). Verify which is used and document. If normal is used and σ is large, projections can produce nonsensical −150% return draws — that's MEDIUM.

- [ ] **Step 2: Verify Monte Carlo aggregation**

Read how the code aggregates N trials. Verify:
- Mean / median / percentile (5th, 25th, 75th, 95th) computation matches standard definitions.
- Success rate: fraction of trials where final balance > 0 (or some other threshold).

- [ ] **Step 3: Verify historical backtest sequence reconstruction**

`historical_data.go` should embed real historical return series. Verify:

(a) The data source and date range (e.g., "S&P 500 + 10Y Treasury, 1928-2024").

(b) Rolling-window construction: a 30-year backtest starting in year 1928 uses returns from 1928-1957. Verify the windowing is correct (no gaps, no overlap with future data).

(c) WorstStartYears: the years that produced the worst final outcomes. Verify the ranking math.

- [ ] **Step 4: Verify guardrails**

Source: Guyton & Klinger (2006). Two main rules:

- **Capital Preservation Rule (CPR):** if portfolio falls below threshold, cut withdrawal by 10%.
- **Prosperity Rule (PR):** if portfolio rises above threshold, raise withdrawal by 10%.

Read `guardrails.go`. Verify both rules are implemented and the trigger thresholds match the original paper (typically 20% above/below the initial withdrawal rate).

If the planner implements only one rule or different thresholds, document.

- [ ] **Step 5: Test-coverage gap analysis**

Read `backtest_test.go`, `guardrails_test.go`. Boundary checks:

- Monte Carlo with 0 trials (degenerate)
- Monte Carlo with 1 trial (no statistics)
- Backtest with horizon longer than available history
- Guardrail trigger: portfolio at exact upper threshold
- Guardrail trigger: portfolio at exact lower threshold
- Multiple guardrail triggers in a row (cuts compound or are capped?)

- [ ] **Step 6: Write section 9, commit**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): area 9 — backtest, Monte Carlo, guardrails"
```

---

## Task 10: Scenario chain, healthcare, budget-fit / steady-state

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md` — section 10
- Read: `chain.go`, `chain_test.go`, `calculator.go` (healthcare + budget-fit + steady-state functions), `internal/models/whatif.go` (healthcare cost methods)

**Functions:**

- `chain.go` — all functions in this file (chain link resolution, hand-off math)
- Healthcare per-person cost (find via `gitnexus` or `grep "GetTotalHealthcareCost"`)
- `calculateMonthlyIncomeBreakdown` (`calculator.go:364`)
- `CalculateTotalExpenses` (`calculator.go:548`) [cross-listed with Task 6]
- `CalculateTotalIncome` (`calculator.go:127`)
- `CalculateBudgetFit` (find via `grep "func.*CalculateBudgetFit"`)
- `findSteadyStateMonth` (referenced in April 11 review)
- `CalculatePresentValueAnalysis` (find via grep)
- `buildProjectionExplainability` (find via grep)

- [ ] **Step 1: Enumerate all functions in `chain.go`**

Run: `grep -n "^func " internal/services/retirement/chain.go`

List them and add to the audit's function table.

- [ ] **Step 2: Verify scenario chain hand-off math**

Across a chain link, the next scenario inherits:
- Portfolio balances (tax-deferred, Roth, taxable + cost basis)
- Cumulative inflation factor (so future expenses compound from the right base)
- Person ages
- Year offset

Verify each is preserved across the hand-off without loss. Worked example: a chain of [Phase 1: 5 years, Phase 2: 25 years] should produce identical month-by-month numbers as a single 30-year scenario with equivalent settings (provided the settings are equivalent).

If they don't match, document the divergence as MEDIUM or HIGH depending on magnitude.

- [ ] **Step 3: Verify healthcare cost calculation**

Read `WhatIfSettings.GetTotalHealthcareCost` (in `internal/models/whatif.go:471`). For each person:

- Pre-Medicare: ACA cost − employer contribution = effective monthly cost.
- Medicare-eligible: Medicare premium + IRMAA surcharge.

Verify:
- Age transitions (especially turning 65) compute correctly month-by-month.
- Employer contribution doesn't apply once person is on Medicare.
- Death / removal of a person mid-projection (if supported) reduces cost correctly.

- [ ] **Step 4: Verify budget-fit / steady-state**

Source: this is internal model; verify internal consistency.

- `findSteadyStateMonth` — picks a "representative" projection month for steady-state display. Read the code. Document the heuristic.
- April 11 review finding 11 fixed: `MinSteadyStateYear` now includes optimizer-driven SS timing. Verify the fix is intact.
- Steady-state values: nominal vs real. April 11 review finding 12 fixed: now consistently nominal. Verify.

- [ ] **Step 5: Verify present-value analysis**

`CalculatePresentValueAnalysis` returns total resources (PV income), PV expenses, coverage ratio (income/expenses), surplus/deficit. Verify:

- PV income includes all income sources (employment, pension, SS, big-ticket inflows if any).
- PV expenses includes living expenses, healthcare, taxes (or are taxes computed separately?).
- Coverage ratio: PV income / PV expenses.
- Surplus/deficit: PV income − PV expenses.

These must agree with the displayed values in the live whatif page (the verification doc from 2026-04-07 has worked numbers — cross-check against those).

- [ ] **Step 6: Verify projection explainability**

`buildProjectionExplainability` produces percentages: taxes consumed, cumulative inflation, inflation distortion. Verify each percentage is computed against the right denominator (gross withdrawals? final balance? cumulative spending?). Document.

- [ ] **Step 7: Test-coverage gap analysis**

Read `chain_test.go` plus any tests for budget-fit and explainability. Boundary checks:

- Chain with 1 link (degenerate single-scenario)
- Chain with 0 links (does it fall through correctly?)
- Chain link with 0-month duration
- Chain link transitioning while a person is dying / aging out
- Healthcare person turning 65 in the same month as scenario chain hand-off
- Budget-fit when monthly income exactly equals monthly expenses
- Coverage ratio when expenses = 0 (division by zero protection)
- Steady-state year before any income source begins

- [ ] **Step 8: Write section 10, commit**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): area 10 — chain, healthcare, budget-fit"
```

---

## Task 11: Constants-currency appendix

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md` — Appendix A

- [ ] **Step 1: Enumerate every numeric constant audited**

Walk back through tasks 1-10. Every constant verified in those steps is a row in the appendix. Make a flat list:

- 28 federal tax bracket boundaries
- 12 LTCG bracket boundaries
- 4 standard deduction values
- 2 SS taxable thresholds × 4 filing statuses + 4 BaseTaxableAmount values
- 4 NIIT thresholds + 1 NIIT rate (3.8%)
- 6 IRMAA tier UpperMAGI × 4 filing statuses + 6 surcharges × 4 = ~48 IRMAA cells (note: MFS has fewer tiers — adjust)
- ~50+ Uniform Lifetime Table factors (depends on age range covered)
- 1 RMD start age (73 / 75)
- 1 Roth conversion early withdrawal age (59.5)
- 1 Early withdrawal penalty rate (10%)
- 2 Medicare-eligible age (65)

Roughly 150–200 rows.

- [ ] **Step 2: Build the table**

For each row, columns:

| Constant | Location | As-coded value | As-coded year | Most-current published value | Most-current year | Status |

Status:
- `current` — code matches the most-current published value
- `stale` — code is older; bump candidate
- `n/a` — internal model, no external authority

Most TY2024 federal brackets are stale relative to TY2025 (Rev. Proc. 2024-40) — so most rows in the federal tax section will be `stale` with the TY2025 value in the "most-current" column. That's expected per the spec — the staleness story lives here so it can be triaged separately.

- [ ] **Step 3: Append to Appendix A**

Replace the placeholder with the full table.

- [ ] **Step 4: Commit**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): constants-currency appendix"
```

---

## Task 12: Executive summary + findings table

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md` — Executive summary + Findings table

- [ ] **Step 1: Roll up findings ledger**

Read Appendix C (Findings ledger). Sort by severity then area then ID. Build the findings table at the top of the document with columns:

| ID | Sev | Area | Location | Summary |

- [ ] **Step 2: Write executive summary**

Three short paragraphs:

1. **What was audited:** function counts by area, total functions, total constants verified, total worked examples.
2. **What was found:** finding counts by severity (HIGH x, MEDIUM y, LOW z, INFO w). Pass rate for functions audited.
3. **Top three concerns:** the three highest-severity, highest-impact findings. Reference by ID.

If there are no HIGH findings, say so explicitly — "no HIGH-severity findings" is a meaningful audit result.

- [ ] **Step 3: Verify cross-references**

Every finding ID in the executive summary and findings table must exist in Appendix C. Every finding in Appendix C must appear in the findings table. Run a quick consistency check:

```bash
grep -oE "F-[0-9]+" docs/whatif-math-audit-2026-05-05.md | sort -u | wc -l
```

The number of unique IDs should equal the number of rows in the findings table. If it doesn't, find and fix the discrepancy.

- [ ] **Step 4: Commit**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): executive summary + findings table"
```

---

## Task 13: Method appendix + final review

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md` — Appendix B
- Verify: end-to-end document quality

- [ ] **Step 1: Write Appendix B (Audit method)**

Roughly 500-1000 words describing how the audit was conducted. Cover:

- Branch / commit audited
- Source map used
- Severity rubric
- Boundary checklist
- Worked-example tolerance
- What was NOT audited (UI, performance, settings persistence, etc.)
- How a future auditor can reproduce this pass

- [ ] **Step 2: Final pass — readability**

Read the document end to end. Check:

- All section placeholders replaced.
- All cross-references resolve (no `F-NNN` orphans, no "see Section X" with X missing).
- Findings ledger (Appendix C) and findings table both contain every emitted finding.
- Constants appendix is complete (no missing rows).
- Worked examples each show source, expected, actual, delta.
- No internal `TODO` or `TBD` markers.

If the document is internally inconsistent, fix inline.

- [ ] **Step 3: Final pass — math sanity**

Re-read each finding. For each HIGH finding, sanity-check the severity: is it really >5% error or qualitatively wrong? Demote to MEDIUM if not.

For each PASS function, sanity-check that you actually verified it (vs. assumed it from familiarity). If a function was assumed PASS without verification, that's a hole — either verify or downgrade to "not audited" with explanation.

- [ ] **Step 4: Commit final document**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(audit): final method appendix + cross-reference verification"
```

- [ ] **Step 5: Push branch**

```bash
git push -u origin feat/whatif-math-audit
```

The audit is complete. Triage of findings into follow-up tickets is owned by the user, out of scope for this engagement.

---

## Self-review checklist

After this plan is reviewed and before execution starts, verify:

1. **Spec coverage:**
   - [ ] All 10 math areas in spec are covered by tasks 1-10. ✓
   - [ ] Constants-currency appendix is covered by task 11. ✓
   - [ ] Executive summary + findings table is covered by task 12. ✓
   - [ ] Method appendix is covered by task 13. ✓
   - [ ] Test-coverage gap analysis is included in every audit task as a step. ✓
   - [ ] Authoritative source map is locked in conventions section. ✓
   - [ ] Severity rubric is locked in conventions section. ✓
   - [ ] Worked-example tolerance is locked in conventions section. ✓

2. **Placeholder scan:**
   - [ ] No "TBD" / "TODO" / "fill in details" markers. ✓
   - [ ] Every step has concrete content (code path, table cell, commit message). ✓
   - [ ] No "similar to Task N" references — every task is self-contained. ✓

3. **Type / name consistency:**
   - [ ] Function names match `tax.go`, `social_security.go`, etc. Every function name in the plan was verified by `grep -n "^func "` output before writing. ✓
   - [ ] File line numbers are stale-tolerant (numbers are approximate; the audit will verify and update).

4. **Open issues for execution-time discovery:**
   - Some functions referenced in the spec (e.g., per-account allocation / glide-path math) require enumeration during audit (Task 7 Step 1). The plan describes how to do that enumeration rather than naming functions that may not exist.
   - Some functions (`CalculateBudgetFit`, `CalculatePresentValueAnalysis`, `buildProjectionExplainability`) require `grep`-driven discovery in Task 10 Step 1. The plan describes the discovery process rather than asserting line numbers.

---

## Execution

This plan is ready. Choose:

**1. Subagent-Driven (recommended)** — Dispatch a fresh subagent per task. Each subagent gets the full conventions section + its own task as context. Review between tasks. Fits this work well because each area is independent.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch with checkpoints.
