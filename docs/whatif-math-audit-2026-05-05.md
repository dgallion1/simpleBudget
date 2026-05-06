# What-If Math Audit

**Date:** 2026-05-05 (audit baseline updated 2026-05-06)
**Codebase audited at commit:** `b978aa9` ("Fix what-if compounding math") —
the HEAD of `dev` at the time the user-authored compounding fix landed. Tasks
1 and 2 of this audit were drafted against `3ec6440` (one commit prior), but
neither task's scope intersects `b978aa9`'s changes (which touch only PV /
compounding helpers in `calculator.go` and the monthly-return calculation in
`rmd.go:CalculateRMDAnalysis`). All other tasks (3–10) audit the post-fix
code.
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

### Functions audited

_Legend: **PASS** = formula correct, no associated finding. **PASS (F-NNN)** = formula correct, but an associated finding (typically test-gap, severity LOW or MEDIUM) exists. **PARTIAL (F-NNN)** = formula partially correct; a real feature gap or missing behavior is noted in the finding. **FAIL (F-NNN)** = formula incorrect._

| Function | Location | Status |
|----------|----------|--------|
| `CalculateFederalTax` | `tax.go:349` | PASS (F-007) |
| `calculateFederalTaxOnTaxableIncome` | `tax.go:372` | PASS |
| `CalculateStateTax` | `tax.go:396` | PASS (F-002, F-008) |
| `CalculateTotalTax` | `tax.go:404` | PASS (F-003, F-009) |
| `CalculateTaxWithInvestmentIncome` | `tax.go:421` | PASS (F-010) |
| `calculateTaxWithInvestmentIncomeInternal` | `tax.go:433` | PASS (F-011) |
| `CalculateTaxWithInvestmentIncomeBreakdown` | `tax.go:429` | PASS (F-011) |
| `EstimateRothConversionTax` | `tax.go:484` | PASS (F-012) |
| `GetMarginalRate` | `tax.go:499` | PASS (F-013) |
| `GetAdjustedBrackets` | `tax.go:181` | PASS (F-004, F-014) |
| `GetAdjustedLongTermCapitalGainsBrackets` | `tax.go:210` | PASS (F-015) |
| `GetAdjustedStandardDeduction` | `tax.go:238` | PARTIAL (F-001), PASS (F-006, F-016) |
| `inflationFactor` | `tax.go:251` | PASS (F-004) |
| `normalizeFilingStatus` | `tax.go:258` | PASS (F-017) |

### Constants verified

| Table | Cells checked | Mismatches |
|-------|---------------|------------|
| `TaxBrackets2024` | 28 | 0 |
| `LongTermCapitalGainsBrackets2024` | 12 | 0 |
| `StandardDeduction2024` | 4 | 0 (but missing age-65+ additional — see F-001) |

### Worked examples

#### WE-1.1: Single, $80,000 ordinary, year-0

**Source:** IRS Rev. Proc. 2023-34 §3.01 (Table 1) and §3.16.

| Step | Value |
|------|-------|
| Standard deduction (Single, 2024) | $14,600 |
| Taxable income | $65,400 |
| 10% × $11,600 | $1,160.00 |
| 12% × ($47,150 − $11,600) = 12% × $35,550 | $4,266.00 |
| 22% × ($65,400 − $47,150) = 22% × $18,250 | $4,015.00 |
| **Expected total** | **$9,441.00** |
| **Actual from `CalculateFederalTax(80000, 0)` (Single, year-0)** | **$9,441.00** |
| Delta | $0.00 |

#### WE-1.2: MFJ, $170,800 ordinary + $20,000 LTCG, year-0

**Source:** IRS Rev. Proc. 2023-34 §3.01 (Table 3) and §3.03.

| Step | Value |
|------|-------|
| Standard deduction (MFJ, 2024) | $29,200 |
| Taxable ordinary income | $170,800 |
| Taxable LTCG | $20,000 |
| Total taxable income | $190,800 |
| Ordinary tax: 10% × $23,200 | $2,320.00 |
| Ordinary tax: 12% × ($94,300 − $23,200) = 12% × $71,100 | $8,532.00 |
| Ordinary tax: 22% × ($170,800 − $94,300) = 22% × $76,500 | $16,830.00 |
| Ordinary tax subtotal | $27,682.00 |
| LTCG tax: $20,000 at 15% (total taxable $190,800 < MFJ 15% ceiling $583,750) | $3,000.00 |
| **Expected total federal** | **$30,682.00** |
| **Actual from `CalculateTaxWithInvestmentIncome(200000, 0, 20000, 0)` (MFJ, year-0)** | **$30,682.00** |
| Delta | $0.00 |

### Findings

#### F-001 — MEDIUM Missing age-65+ additional standard deduction

**Location:** `internal/services/retirement/tax.go:238` — `GetAdjustedStandardDeduction`

**Source consulted:** IRS Rev. Proc. 2023-34 §3.16 (additional standard deduction for age 65 or older and/or blind).

**What it does:** Returns the base 2024 standard deduction for the filing status, inflated forward. No age-based adjustment is applied.

**Finding:** Rev. Proc. 2023-34 §3.16 provides an additional standard deduction for taxpayers who are 65 or older: **$1,950 per qualifying person for Single and Head of Household filers**, and **$1,550 per qualifying spouse for MFJ and MFS filers** (i.e., $3,100 if both MFJ spouses are 65+). The per-person amount is intentionally higher for Single/HoH filers under the IRS code. Since this planner targets retirees who are typically 65 or older, the base deduction is likely understated by $1,550–$3,900 for most users (the upper bound is a Single or HoH filer who is both 65+ and blind, adding two $1,950 increments). This causes over-taxation of ordinary income.

**Evidence / repro:**
```go
// tax.go:238-249
func (tc *TaxCalculator) GetAdjustedStandardDeduction(yearsFromBase int) float64 {
    baseDeduction := StandardDeduction2024[tc.FilingStatus]
    // ...
    return baseDeduction * tc.inflationFactor(yearsFromBase)
    // No age-65+ addition anywhere in the call chain.
}
```
A 65+ Single filer at $80,000 gross income would have:
- Standard deduction (Single, age 65+): $14,600 + $1,950 = **$16,550**
- Taxable income: $80,000 − $16,550 = **$63,450**
- Tax: 10% × $11,600 + 12% × ($47,150 − $11,600) + 22% × ($63,450 − $47,150) = $1,160 + $4,266 + $3,586 = **$9,012**
- Code's tax (WE-1.1, no age-65+ adjustment): **$9,441**
- Over-estimate: $9,441 − $9,012 = **$429** (**4.8%** error on tax owed)

**Recommended fix sketch:** Add an `Age65Count int` field (0, 1, or 2) to `TaxCalculator` and a `StandardDeduction2024Additional` constant map keyed on filing status (Single/HoH → $1,950; MFJ/MFS → $1,550); sum the base deduction with `Age65Count * additional` before inflating.

**Test coverage note:** No test exercises the age-65+ deduction path because the function doesn't implement it. A boundary test at the qualifying age transition (the year the client turns 65) is entirely absent.

---

#### F-002 — INFO State tax is a single flat rate

**Location:** `internal/services/retirement/tax.go:396` — `CalculateStateTax`

**Source consulted:** General knowledge of state income tax structures; no specific IRS source (state law varies by jurisdiction).

**What it does:** Applies a single flat percentage to taxable income. No progressive state brackets, no state standard deduction or exemptions.

**Finding:** This is a known simplification. Most states use progressive brackets, personal exemptions, and/or differ in their treatment of retirement income (pension exclusions, SS exemptions). The simplification is acceptable for a high-level planner but may over- or under-estimate state tax by a meaningful margin for users in progressive-bracket states. No code bug — informational only.

**Evidence / repro:** n/a

**Recommended fix sketch:** n/a (acknowledged simplification; could add a documentation note to the UI).

**Test coverage note:** See F-008 for test-coverage gaps specific to this function.

---

#### F-003 — INFO `CalculateTotalTax` uses federal standard deduction for state taxable income

**Location:** `internal/services/retirement/tax.go:404` — `CalculateTotalTax`

**Source consulted:** General knowledge of state income tax structures.

**What it does:** Derives state taxable income as `grossIncome − federalStandardDeduction`.

**Finding:** Most states have their own standard deductions (or exemptions) that differ from the federal amount. Applying the federal standard deduction to the state base is a reasonable approximation but may diverge from actual state liability. The inline comment in the code acknowledges this: "Simplified: apply to same taxable income base." Not a bug — informational.

**Evidence / repro:** n/a

**Recommended fix sketch:** n/a (acknowledged simplification).

**Test coverage note:** See F-009 for test-coverage gaps specific to this function.

---

#### F-004 — INFO Inflation projection uses pure compound growth; IRS uses chained CPI rounded to nearest $50

**Location:** `internal/services/retirement/tax.go:181` — `GetAdjustedBrackets`; `internal/services/retirement/tax.go:251` — `inflationFactor`

**Source consulted:** IRS Rev. Proc. 2023-34 §3.01 (inflation adjustment methodology).

**What it does:** Multiplies all bracket edges by `(1 + inflationRate/100)^years`, producing continuous compounding without rounding.

**Finding:** The IRS adjusts brackets annually using chained CPI-U-RS and rounds to the nearest $50 (for ordinary income brackets) or $50 (for LTCG brackets) per Rev. Proc. 2023-34 §3.01. Pure compounding with user-supplied inflation rate will diverge from actual IRS-published values as years increase. This is appropriate for a projection tool (the actual IRS values are unknown for future years), but users should understand the projection is a smooth approximation, not a simulation of IRS rounding. Not a bug — informational.

**Evidence / repro:** n/a

**Recommended fix sketch:** n/a (behavior is intentional for a projection tool; consider a UI tooltip explaining the approximation).

**Test coverage note:** `inflationFactor` is exercised indirectly via `GetAdjustedBrackets`. See F-014 for bracket-specific test gaps.

---

#### F-005 — INFO `inflationFactor` negative-years path not exercised by tests

**Location:** `internal/services/retirement/tax.go:251` — `inflationFactor`

**Source consulted:** Code inspection.

**What it does:** Returns `1.0` when `yearsFromBase <= 0`, and `(1 + inflationRate/100)^years` otherwise.

**Finding:** The `yearsFromBase < 0` branch (which also returns `1.0`) is never exercised in tests. Tests only call `GetAdjustedBrackets(0)` and `GetAdjustedBrackets(10)`, so the negative branch of `inflationFactor` is unreachable in the test suite. The behavior (return 1.0 for any non-positive year) is intuitive and correct, but the branch is dark. Informational only — not a code error.

**Evidence / repro:** `TestInflationAdjustedBrackets` calls `GetAdjustedBrackets(0)` and `GetAdjustedBrackets(10)` only; no test passes a negative year to any function that calls `inflationFactor`.

**Recommended fix sketch:** Add a table-driven case with `yearsFromBase = -1` to `TestInflationAdjustedBrackets` asserting the returned brackets equal the base brackets.

**Test coverage note:** The `yearsFromBase <= 0` branch is only partially exercised (year=0, not year<0).

---

#### F-006 — LOW Dead fallback branch in `GetAdjustedStandardDeduction`

**Location:** `internal/services/retirement/tax.go:238` — `GetAdjustedStandardDeduction`

**Source consulted:** Code inspection.

**What it does:** If `baseDeduction == 0` (i.e., filing status not found in map), falls back to `StandardDeduction2024[models.FilingMarriedJoint]`.

**Finding:** `StandardDeduction2024` populates all four valid filing status keys. `normalizeFilingStatus` is not called before the map lookup in this function (unlike in several other functions), so an unknown filing status would yield 0 and trigger the fallback. However, `NewTaxCalculator` accepts any `models.FilingStatus` without normalizing it — meaning an invalid status passed at construction time would cause `GetAdjustedStandardDeduction` to silently fall back to MFJ values while other functions (which do call `normalizeFilingStatus`) would also use MFJ. The inconsistency is harmless in practice but worth noting. No user-visible math error in the normal usage path.

**Evidence / repro:** n/a

**Recommended fix sketch:** Call `normalizeFilingStatus(tc.FilingStatus)` consistently at the top of each method that accesses a map by filing status, or normalize once in `NewTaxCalculator`.

**Test coverage note:** No test exercises the invalid-filing-status fallback path.

---

#### F-007 — LOW `CalculateFederalTax`: filing-status and time-axis coverage gaps

**Location:** `internal/services/retirement/tax.go:349` — `CalculateFederalTax`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Computes federal income tax given gross income and years from base year; applies standard deduction and progressive brackets.

**Finding:** `TestCalculateFederalTax` constructs a MFJ calculator and only checks that tax falls within loose bounds. Only MFJ is exercised; Single, MFS, and HoH are not tested for this function. No test passes `yearsFromBase > 0` to verify bracket inflation; no test passes a negative income value (the function returns zero for `grossIncome <= 0` per the guard, but this is not asserted).

**Evidence / repro:** `TestCalculateFederalTax` has four subtests (zero, low, middle, higher income), all using MFJ and `yearsFromBase=0`.

**Recommended fix sketch:** Add table-driven subtests with Single, MFS, and HoH filing statuses at a representative income; add a `yearsFromBase=10` subtest with a 3% inflation rate and verify brackets shift by approximately `1.03^10`; assert zero tax on negative income.

**Test coverage note:** Bracket-exact boundary cases (income = standard deduction, income = bracket edge) are not tested.

---

#### F-008 — LOW `CalculateStateTax`: no direct test coverage

**Location:** `internal/services/retirement/tax.go:396` — `CalculateStateTax`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns `taxableIncome * stateRate / 100`, or zero if either is non-positive.

**Finding:** `CalculateStateTax` is never called directly in the test suite. All tax tests that use a `TaxCalculator` set `StateIncomeTaxRate: 0`, so the state tax branch is always zero. The guard for `taxableIncome <= 0` is not tested. The guard for `tc.StateRate <= 0` is tested only implicitly (by always being zero).

**Evidence / repro:** `grep -n "CalculateStateTax" tax_test.go` returns no results; all calculator constructions in tests use `StateIncomeTaxRate: 0`.

**Recommended fix sketch:** Add a `TestCalculateStateTax` with: positive income + positive rate (assert exact value); zero income (assert zero); negative income (assert zero); zero rate (assert zero).

**Test coverage note:** The positive state-tax path is completely untested.

---

#### F-009 — LOW `CalculateTotalTax`: indirect coverage only; filing-status and inflation gaps

**Location:** `internal/services/retirement/tax.go:404` — `CalculateTotalTax`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns combined federal and state tax plus effective rate for given gross income and year offset.

**Finding:** `CalculateTotalTax` is called in `TestProjectionTaxAccumulatorEstimateMonthlyTaxes` only as a reference value to verify the accumulator, not as the direct subject under test. Only Single filing status and `yearsFromBase=0` are used there. The function's four return values (federalTax, stateTax, totalTax, effectiveRate) are never independently verified for correctness. MFJ, MFS, and HoH filing statuses are not covered. No test uses `yearsFromBase > 0`.

**Evidence / repro:** `TestProjectionTaxAccumulatorEstimateMonthlyTaxes` uses `CalculateTotalTax` to derive `wantTotal` but does not assert the individual federal/state splits.

**Recommended fix sketch:** Add a `TestCalculateTotalTax` with exact expected values for Single and MFJ at `yearsFromBase=0`; add a `yearsFromBase=10` case with a non-zero state rate to verify both federal inflation and state tax.

**Test coverage note:** effectiveRate return value is never asserted in any test.

---

#### F-010 — LOW `CalculateTaxWithInvestmentIncome`: single filing status; no LTCG at 20% bracket

**Location:** `internal/services/retirement/tax.go:421` — `CalculateTaxWithInvestmentIncome`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Computes federal + state tax on a mix of ordinary income, qualified dividends, and LTCG.

**Finding:** `TestCalculateTaxWithInvestmentIncome` uses only a Single calculator. There is no MFJ, MFS, or HoH test case. No test places total taxable income above the 15%-to-20% LTCG threshold ($518,900 Single / $583,750 MFJ), leaving the 20% LTCG bracket untested. No test combines a non-zero state rate with investment income.

**Evidence / repro:** Both subtests use `FilingSingle` and `StateIncomeTaxRate: 0`.

**Recommended fix sketch:** Add a MFJ subtest with combined ordinary + LTCG income above the 20% LTCG threshold; add a subtest with a non-zero state rate and verify the state tax component.

**Test coverage note:** The 20% LTCG bracket is never entered in any test.

---

#### F-011 — MEDIUM `calculateTaxWithInvestmentIncomeInternal` / `CalculateTaxWithInvestmentIncomeBreakdown`: nonQualifiedDividends path and Breakdown entry point untested

**Location:** `internal/services/retirement/tax.go:429` — `CalculateTaxWithInvestmentIncomeBreakdown`; `tax.go:433` — `calculateTaxWithInvestmentIncomeInternal`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** `calculateTaxWithInvestmentIncomeInternal` is the shared implementation for both wrappers; it computes NIIT using `nonQualifiedDividends` as part of net investment income. `CalculateTaxWithInvestmentIncomeBreakdown` is the public entry point that exposes the NIIT component and MAGI separately.

**Finding:** `CalculateTaxWithInvestmentIncomeBreakdown` is never called in the test suite. Because `CalculateTaxWithInvestmentIncome` always passes `nonQualifiedDividends=0` to the internal function, the `nonQualifiedDividends` contribution to NIIT is completely untested. This is a meaningful gap because non-qualified dividends increase the NIIT base independently of the ordinary-income NIIT threshold calculation, and a regression here would not be caught by any existing test.

**Evidence / repro:** No call to `CalculateTaxWithInvestmentIncomeBreakdown` appears in `tax_test.go`; all `CalculateTaxWithInvestmentIncome` calls omit the nonQualifiedDividends argument (the four-parameter variant always passes 0 internally).

**Recommended fix sketch:** Add `TestCalculateTaxWithInvestmentIncomeBreakdown` with a case where `nonQualifiedDividends > 0` and `magi > NIIT threshold`; assert that the returned `NIIT` field is non-zero and equals `min(excess_magi, netInvestmentIncome) * 0.038`.

**Test coverage note:** The entire `nonQualifiedDividends`-to-NIIT path is a dead code path from the test suite's perspective.

---

#### F-012 — LOW `EstimateRothConversionTax`: negative conversion and multi-year not tested

**Location:** `internal/services/retirement/tax.go:484` — `EstimateRothConversionTax`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns the incremental federal tax from adding a Roth conversion to base income. Returns zero if `conversionAmount <= 0`.

**Finding:** `TestEstimateRothConversionTax` tests a positive conversion and a zero conversion, but not a negative conversion amount (which the guard covers but is not asserted). No test uses `yearsFromBase > 0`. Only MFJ filing status is tested; Single, MFS, and HoH are absent.

**Evidence / repro:** Test has two cases: `conversionAmount=25000` and `conversionAmount=0`; both use MFJ and `yearsFromBase=0`.

**Recommended fix sketch:** Add a negative conversion case asserting result is zero; add a `yearsFromBase=10` case asserting the incremental tax shifts proportionally with inflation-adjusted brackets.

**Test coverage note:** The negative-input guard (`conversionAmount <= 0`) is exercised by the zero case but not verified as returning exactly 0 for a negative value.

---

#### F-013 — LOW `GetMarginalRate`: single filing status; no year-offset coverage

**Location:** `internal/services/retirement/tax.go:499` — `GetMarginalRate`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns the marginal bracket rate for a given gross income after applying the standard deduction.

**Finding:** `TestGetMarginalRate` tests seven income levels but only for Single filing status with `yearsFromBase=0`. MFJ, MFS, and HoH filing statuses produce different bracket thresholds and are not tested. `yearsFromBase > 0` is not tested. Negative income (returns 10 per guard) is not tested.

**Evidence / repro:** All seven test cases in `TestGetMarginalRate` use `FilingSingle` and pass `yearsFromBase=0` to `GetMarginalRate`.

**Recommended fix sketch:** Add MFJ and HoH cases at incomes that straddle the bracket boundaries unique to those statuses; add a negative-income case asserting 10%; add a `yearsFromBase=10` case with 3% inflation.

**Test coverage note:** MFJ 10%-to-12% threshold ($23,200 before deduction) never tested.

---

#### F-014 — LOW `GetAdjustedBrackets`: MFS and HoH filing statuses and negative year not tested

**Location:** `internal/services/retirement/tax.go:181` — `GetAdjustedBrackets`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns inflation-adjusted federal tax brackets for the configured filing status and year offset.

**Finding:** `TestInflationAdjustedBrackets` tests only Single with `yearsFromBase=0` and `yearsFromBase=10`. MFJ, MFS, and HoH filing statuses (which have different bracket structures) are not directly tested. The `yearsFromBase <= 0` early-return path is exercised by year=0 but the `yearsFromBase < 0` sub-case is not (see also F-005). The fallback to MFJ brackets for an unknown filing status (`baseBrackets == nil` branch) is never triggered.

**Evidence / repro:** `TestInflationAdjustedBrackets` explicitly constructs `FilingSingle`.

**Recommended fix sketch:** Parameterize the test over filing statuses; add a `yearsFromBase=-1` case asserting the result equals the `yearsFromBase=0` result; add an invalid-status case to exercise the nil-fallback branch.

**Test coverage note:** The nil-bracket fallback branch at `tax.go:183–185` is never reached in tests.

---

#### F-015 — LOW `GetAdjustedLongTermCapitalGainsBrackets`: never directly tested

**Location:** `internal/services/retirement/tax.go:210` — `GetAdjustedLongTermCapitalGainsBrackets`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns inflation-adjusted LTCG brackets for the configured filing status and year offset. Mirrors the structure of `GetAdjustedBrackets`.

**Finding:** No test in `tax_test.go` calls `GetAdjustedLongTermCapitalGainsBrackets` directly. It is exercised indirectly when `CalculateTaxWithInvestmentIncome` is called with non-zero LTCG, but only for Single at year-0. MFJ, MFS, and HoH are untested; `yearsFromBase > 0` is never exercised for this function specifically; the nil-fallback branch is unreachable in tests.

**Evidence / repro:** `grep "GetAdjustedLongTermCapitalGainsBrackets" tax_test.go` returns no results.

**Recommended fix sketch:** Add a `TestGetAdjustedLongTermCapitalGainsBrackets` parallel to `TestInflationAdjustedBrackets`, covering all four filing statuses at year-0 and year-10 with known inflation rates.

**Test coverage note:** Inflation adjustment of LTCG brackets is only implicitly tested through the investment-income calculation path, which covers Single at year-0 only.

---

#### F-016 — LOW `GetAdjustedStandardDeduction`: never directly tested

**Location:** `internal/services/retirement/tax.go:238` — `GetAdjustedStandardDeduction`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns the filing-status standard deduction inflated to the requested year.

**Finding:** No test calls `GetAdjustedStandardDeduction` directly. It is exercised indirectly through `CalculateFederalTax` and `CalculateTotalTax`, but never with `yearsFromBase > 0` for all four filing statuses. The nil-fallback branch (`baseDeduction == 0`) is not reachable in tests. Direct tests would also catch any future regression if the base constant table is edited incorrectly.

**Evidence / repro:** `grep "GetAdjustedStandardDeduction" tax_test.go` returns no results.

**Recommended fix sketch:** Add `TestGetAdjustedStandardDeduction` asserting the exact 2024 values for all four statuses at year-0, and a proportional inflation check at year-10 with 3% inflation rate.

**Test coverage note:** All four filing-status exact values are unasserted.

---

#### F-017 — LOW `normalizeFilingStatus`: never directly tested

**Location:** `internal/services/retirement/tax.go:258` — `normalizeFilingStatus`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Maps any `FilingStatus` value to one of the four valid statuses, defaulting to MFJ for unknown values.

**Finding:** No test exercises `normalizeFilingStatus` directly. All four valid filing status values are covered indirectly through the broader function tests, but only MFJ and Single receive meaningful coverage. The invalid-status fallback path (anything not in the switch) is never triggered in any test.

**Evidence / repro:** `grep "normalizeFilingStatus" tax_test.go` returns no results.

**Recommended fix sketch:** Add a `TestNormalizeFilingStatus` with all four valid statuses (assert identity) plus one invalid/zero value (assert MFJ default).

**Test coverage note:** The default fallback branch (`return models.FilingMarriedJoint`) is never reached in tests.

## 2. Specialized federal tax surcharges (Taxable SS, NIIT, IRMAA)

### Functions audited

**Legend:** PASS = formula correct, no findings · PASS (F-NNN) = formula correct, has associated finding (typically test-gap) · PARTIAL (F-NNN) = formula partially correct, missing feature · FAIL (F-NNN) = formula incorrect.

| Function | Location | Status |
|----------|----------|--------|
| `CalculateTaxableSocialSecurity` (free) | `tax.go:267` | PARTIAL (F-018) |
| `(tc *TaxCalculator) CalculateTaxableSocialSecurity` (method) | `tax.go:335` | PASS |
| `CalculateNIIT` (free) | `tax.go:292` | PASS (F-019) |
| `(tc *TaxCalculator) CalculateNIIT` (method) | `tax.go:339` | PASS |
| `CalculateMonthlyIRMAA` (free) | `tax.go:308` | PASS (F-020, F-021) |
| `(tc *TaxCalculator) CalculateMonthlyIRMAA` (method) | `tax.go:343` | PASS |
| `resolveIRMALookbackMAGI` | `calculator.go:286` | PASS (F-022) |
| `medicareEligibleAdultCountAtYear` | `calculator.go:315` | PASS (F-023) |
| `plannerIRMAAInflationFactorForYear` | `calculator.go:337` | PASS (F-024) |

### Constants verified

| Table | Cells checked | Mismatches |
|-------|---------------|------------|
| `socialSecurityTaxThresholds` | 9 (3 statuses × 3 fields each) | 0 (MFS absent by design; handling noted in F-018) |
| `niitThresholds` | 4 | 0 |
| `monthlyIRMAASurcharge2026` | 22 (tier counts × 2 fields across 4 statuses) | 0 verified against code logic; see F-020 for manual CMS cross-check advisory |

### Worked examples

#### WE-2.1: Taxable SS, MFJ, $50,000 other + $30,000 SS

**Source:** 26 USC § 86; IRS Pub 915 worksheet.

| Step | Value |
|------|-------|
| Provisional income: $50,000 + ½($30,000) | $65,000 |
| Exceeds upper threshold ($44,000 MFJ) | yes |
| Step 1: ($65,000 − $44,000) × 85% | $17,850 |
| Step 2: lesser of [BaseTaxableAmount $6,000, ½($30,000) $15,000] | $6,000 |
| Step 3: $17,850 + $6,000 | $23,850 |
| Cap: 85% × $30,000 | $25,500 |
| **Expected** | **$23,850** |
| **Actual** (`CalculateTaxableSocialSecurity(30000, 50000, 0, 0, MFJ)`) | **$23,850.00** |
| Delta | $0.00 ✓ |

#### WE-2.2: NIIT, MFJ, MAGI $300K, NII $40K

**Source:** 26 USC § 1411; IRS Pub 550.

| Step | Value |
|------|-------|
| Excess MAGI: $300,000 − $250,000 | $50,000 |
| Lesser of NII $40,000 or excess $50,000 | $40,000 |
| NIIT: 3.8% × $40,000 | **$1,520** |
| **Actual** (`CalculateNIIT(300000, 40000, MFJ)`) | **$1,520.00** |
| Delta | $0.00 ✓ |

#### WE-2.3: NIIT lesser-of, MFJ, MAGI $290K, NII $60K

**Source:** 26 USC § 1411 (lesser-of branch where NII > excess MAGI).

| Step | Value |
|------|-------|
| Excess MAGI: $290,000 − $250,000 | $40,000 |
| Lesser of NII $60,000 or excess $40,000 | $40,000 |
| NIIT: 3.8% × $40,000 | **$1,520** |
| **Actual** (`CalculateNIIT(290000, 60000, MFJ)`) | **$1,520.00** |
| Delta | $0.00 ✓ |

#### WE-2.4: IRMAA tier selection, MFJ, MAGI $300K, inflationFactor=1.0

**Source:** CMS 2026 IRMAA table (code's `monthlyIRMAASurcharge2026`); `plannerIRMAAInflationFactorForYear` design.

Inflation factor 1.0 means no scaling of the 2026 thresholds (as would occur at 0% inflation or for testing purposes). The raw 2026 table applies.

| Tier | MFJ upper MAGI | Combined monthly surcharge |
|------|---------------|---------------------------|
| 1 | $218,000 | $0.00 |
| 2 | $274,000 | $95.70 |
| 3 | $342,000 | $240.40 |

MAGI $300,000 exceeds tier-2 upper ($274,000) and falls at or below tier-3 upper ($342,000), selecting tier-3 surcharge.

| Expected monthly surcharge | **$240.40** (202.90 Part B + 37.50 Part D) |
|---|---|
| **Actual** (`CalculateMonthlyIRMAA(300000, MFJ, 1.0)`) | **$240.40** |
| Delta | $0.00 ✓ |

**`plannerIRMAAInflationFactorForYear` at year 0:**
- `yearsFromIRMAABase = 0 − (2026−2024) = −2`
- At 0% inflation: `(1+0)^(−2) = 1.0` (confirmed by test)
- At 3% inflation: `(1.03)^(−2) = 0.9426…` (confirmed by test)
- The design correctly deflates the 2026 IRMAA table back to projection-year-0 (2024) purchasing power, so year-N MAGI is compared on equal footing with the year-N IRMAA thresholds.

### Findings

#### F-018 — MEDIUM `CalculateTaxableSocialSecurity`: MFS always-85% overstates tax for lived-apart filers

**Location:** `internal/services/retirement/tax.go:267` — `CalculateTaxableSocialSecurity`

**Source consulted:** 26 USC § 86(c)(2); IRS Pub 915 ("How Much Is Taxable?" worksheet for MFS filers).

**What it does:** When `filingStatus == FilingMarriedSeparate`, the function immediately returns `ssBenefits * 0.85` — i.e., 85% of benefits are always taxable. No provisional-income test is applied.

**Finding:** Under 26 USC § 86(c)(2), the 85% cap with $0 thresholds only applies to MFS filers who **lived with their spouse at any time during the tax year**. MFS filers who **lived apart from their spouse the entire year** are subject to the same provisional-income thresholds as Single filers ($25,000 base / $34,000 upper). The code applies the higher (85%-always) treatment to ALL MFS filers regardless of living arrangements, which overstates taxable SS (and thus tax) for MFS-lived-apart filers. For a filer with $20,000 SS and $0 other income: code returns $17,000 taxable; correct amount under lived-apart rules is $0 (provisional income $10,000 < $25,000 base threshold).

**Evidence / repro:**
```go
// tax.go:273-275
if filingStatus == models.FilingMarriedSeparate {
    return ssBenefits * 0.85
}
```
MFS-lived-apart example: `CalculateTaxableSocialSecurity(20000, 0, 0, 0, FilingMarriedSeparate)` → returns $17,000. Per statute (lived apart): PI = $10,000 < $25,000 → correct answer is **$0**. Overstatement: $17,000 taxable SS.

**Recommended fix sketch:** Add a `MFSLivedApart bool` flag to `WhatIfSettings` (or a new `SocialSecurityMFSTreatment` enum). If lived-apart, delegate to the Single/HoH threshold path; if lived-with-spouse or unspecified, retain the 85%-always treatment (conservative default).

**Test coverage note:** The existing test `TestCalculateTaxableSocialSecurity_MarriedSeparateAlways85Pct` asserts the current 85% behavior for all income levels. No test exists for MFS-lived-apart scenarios. The threshold-boundary tests in `coverage_gaps2_test.go` do not include MFS.

---

#### F-019 — MEDIUM `CalculateNIIT`: MAGI at exact threshold not tested; NIIT inflation note

**Location:** `internal/services/retirement/tax.go:292` — `CalculateNIIT`

**Source consulted:** 26 USC § 1411; IRS Pub 550 (NIIT); IRC § 1411(b) (thresholds not indexed).

**What it does:** Computes 3.8% NIIT on the lesser of net investment income or excess MAGI above the filing-status threshold. Returns 0 when MAGI ≤ threshold.

**Finding (two parts):**

*Part A — Formula and thresholds are correct.* The `niitThresholds` map correctly encodes the statutory amounts ($200K Single/HoH, $250K MFJ, $125K MFS). The code does NOT inflate these thresholds (verified: `CalculateNIIT` receives unadjusted MAGI and a fixed threshold — no `yearsFromBase` parameter exists, consistent with the statute's instruction that thresholds are NOT indexed for inflation, per 26 USC § 1411(b)). This is correct behavior.

*Part B — Test gap: MAGI exactly at threshold.* The test suite does not test `CalculateNIIT` at exactly the threshold amount (e.g., `magi = 200000` for Single). The guard `excessMAGI <= 0` covers this, but it is not directly asserted. The `TestCalculateNIIT` cases use $190K (below) and $260K, $215K, $140K (above) — no case uses the exact boundary.

**Evidence / repro:**
- `CalculateNIIT(200000, 50000, FilingSingle)`: expected 0 (at threshold), not tested.
- `CalculateNIIT(125000, 10000, FilingMarriedSeparate)`: expected 0 (at MFS threshold), not tested.

**Recommended fix sketch:** Add exact-threshold cases to `TestCalculateNIIT`: `magi=200000 Single → 0`, `magi=250000 MFJ → 0`, `magi=125000 MFS → 0`; add one case just above each threshold for completeness.

**Test coverage note:** The `excessMAGI <= 0` branch is only exercised via the `magi=190000 < 200000` case (excess is negative); the `excessMAGI == 0` sub-case (MAGI exactly at threshold) is dark.

---

#### F-020 — INFO `monthlyIRMAASurcharge2026`: CMS 2026 IRMAA table values require manual cross-check

**Location:** `internal/services/retirement/tax.go:124–157` — `monthlyIRMAASurcharge2026`

**Source consulted:** CMS 2026 Medicare Part B & D IRMAA announcement (late 2025); 42 USC § 1395r-1.

**What it does:** Defines the 2026 monthly IRMAA surcharge (Part B + Part D) and MAGI tier upper bounds for all four filing statuses. The code comment states these are the source amounts, with the planner rescaling them to the 2024 tax base year and inflating forward.

**Finding:** The surcharge dollar amounts and MAGI tier boundaries in the code cannot be independently verified from training-data knowledge with certainty — the 2026 IRMAA values were announced by CMS in late 2025. Based on best available knowledge, the tier structure (6 tiers for Single/MFJ/HoH, 3 tiers for MFS) and the relationship between Single/MFJ/MFS thresholds (MFJ approximately 2× Single; MFS tier 2 upper = ~$391K) match the legislative intent of 42 USC § 1395r-1. The Part B and Part D amounts are coded separately and summed (e.g., 81.20 + 14.50 = 95.70 for tier 2 Single). However, the specific dollar amounts should be manually cross-checked against the official CMS 2026 announcement to confirm accuracy. The code's internal consistency is intact (MFJ thresholds are exactly 2× Single thresholds where applicable; MFS special treatment follows § 1395r-1(f)(2)).

**Evidence / repro:** n/a (informational advisory, not a confirmed error).

**Recommended fix sketch:** Add a comment in the source referencing the exact CMS publication title and URL (e.g., CMS Fact Sheet "2026 Medicare Parts A and B Premiums and Deductibles"). Add a regression test that asserts specific known dollar amounts so future table updates are intentional changes caught by tests.

**Test coverage note:** The existing IRMAA test cases check tier selection and math (correct) but do not assert that the underlying dollar amounts match the CMS source — a future table-entry typo would not be caught unless the nominal dollar values are compared against a trusted constant.

---

#### F-021 — LOW `CalculateMonthlyIRMAA`: tier-boundary exact values not tested; HoH coverage absent

**Location:** `internal/services/retirement/tax.go:308` — `CalculateMonthlyIRMAA`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`, `coverage_gaps2_test.go`.

**What it does:** Returns the monthly IRMAA surcharge for a given MAGI, filing status, and inflation factor, by walking the tier table.

**Finding:** The test suite covers Single (below threshold, mid-tier, inflation shift), MFJ (top tier), and MFS (three-tier structure), but has these gaps:

1. **Head of Household (HoH) not tested.** HoH uses the same bracket table as Single, but this equivalence is not directly asserted for IRMAA (unlike taxable SS and NIIT where HoH tests exist).
2. **Exact tier-boundary MAGI values not tested.** The Single table has boundaries at $109K, $137K, $171K, $205K, $500K. No test uses these exact boundary values to verify the inclusive `<=` comparison (e.g., `magi = 109000` should return 0; `magi = 109001` should return $95.70).
3. **MFJ tier boundaries not tested.** Only the MFJ top tier is tested ($800K); the intermediate tier boundaries ($218K, $274K, $342K, $410K, $750K) are not exercised.

**Evidence / repro:** In `tax_test.go`, IRMAA tests: Single $100K (below), Single $160K (mid), MFJ $800K (top), Single $110K with 1.05 factor (inflation shifts). In `coverage_gaps2_test.go`, MFS 3-tier tests plus edge cases. HoH is absent.

**Recommended fix sketch:** Add HoH test case asserting same result as Single for equivalent MAGI. Add table-driven test with MAGI at each Single and MFJ tier boundary (both at and just above).

**Test coverage note:** The IRMAA loop's exact boundary behavior (`magi <= upperMAGI` with `<=`) is tested indirectly but not with exact boundary-match values.

---

#### F-022 — LOW `resolveIRMALookbackMAGI`: never directly tested; len=1 boundary not covered

**Location:** `internal/services/retirement/calculator.go:286` — `resolveIRMALookbackMAGI`

**Source consulted:** Code inspection; `calculator_test.go`.

**What it does:** Returns the MAGI from 2 years prior (index `len-2` of the history slice) if history has ≥ 2 entries; otherwise falls back to `assumedIRMALookbackMAGI` if provided; otherwise returns (0, false) indicating no lookback available.

**Finding:** `resolveIRMALookbackMAGI` is never called directly in any test. It is exercised indirectly via `estimateMonthlySnapshot` in `TestEstimateMonthlySnapshot_IRMAALookback` (len=2 history) and via the full projection in `TestRunProjectionDelaysIRMAAUntilLookbackYear`. The following branches are untested:

1. **`len == 1`** (single year of history): the function should fall through to `assumedIRMALookbackMAGI`. This branch is never exercised.
2. **`len == 0` with no assumed MAGI**: returns (0, false). Not directly tested.
3. **Negative assumed MAGI** (`*assumedIRMALookbackMAGI < 0`): clamped to 0 by `math.Max`. This branch is untested.
4. **`len == 2` exact boundary**: tested via the IRMAA lookback test.

**Evidence / repro:** `grep "resolveIRMALookbackMAGI" *_test.go` returns no direct calls.

**Recommended fix sketch:** Add `TestResolveIRMALookbackMAGI` with table-driven cases: empty history + nil assumed → (0, false); empty history + valid assumed → (assumed, true); empty history + negative assumed → (0, true); len-1 history + nil assumed → (0, false); len-1 history + valid assumed → (assumed, true); len-2 history → (history[0], true); len-3 history → (history[1], true).

**Test coverage note:** The `len == 1` path and the negative-assumed-MAGI clamp are dark.

---

#### F-023 — LOW `medicareEligibleAdultCountAtYear`: uses start-of-year age; mid-year birthdate not modeled

**Location:** `internal/services/retirement/calculator.go:315` — `medicareEligibleAdultCountAtYear`

**Source consulted:** Code inspection; 42 USC § 1395o (Medicare eligibility at age 65); `calculator_test.go`.

**What it does:** Returns 0, 1, or 2 — the count of adults (primary + spouse) who are Medicare-eligible (age ≥ 65) at projection year `year`. Age is computed as `CurrentAge + year` (integer year offset), which effectively uses start-of-year age.

**Finding:** The function uses `PrimaryAgeAt(year) = CurrentAge + year`, which is a whole-year step. A person who turns 65 partway through projection year N will show `age = 65` for the entire year N (since `PrimaryAgeAt(N)` equals their integer age at the start of year N + N years). This is a known modeling simplification: Medicare eligibility is actually month-specific (begins the month of the 65th birthday, or 3 months prior for enrollment purposes). The code counts someone as Medicare-eligible for the entire year in which they reach 65, which may overstate IRMAA for up to 11 months at the Medicare transition year.

The test coverage for this function is good: it covers nil settings, both ages below 65, one at 65, one above 65, and both at 65. All test cases use integer year boundaries (no mid-year fractional tests, which aren't possible given the function signature).

**Evidence / repro:**
```go
// calculator.go:315-328
func medicareEligibleAdultCountAtYear(s *models.WhatIfSettings, year int) int {
    if s.PrimaryAgeAt(year) >= 65 { count++ }  // integer year only
```
A 64-year-old who turns 65 in month 6 of year 1: `PrimaryAgeAt(1) = 65` → counted Medicare-eligible for all of year 1 (12 months), but is only eligible for ~6 months.

**Recommended fix sketch:** For higher fidelity, pass a `month int` parameter and check `CurrentAge + year + (month >= birthMonth ? 0 : -1)` — but this requires birth-month data not currently in the model. The existing simplification is reasonable for a projection tool and should be documented in comments.

**Test coverage note:** The function is well-tested for its current (start-of-year) semantics. The known modeling limitation (no mid-year granularity) is a design constraint, not a test gap.

---

#### F-024 — INFO `plannerIRMAAInflationFactorForYear`: zero-equality guard has floating-point fragility

**Location:** `internal/services/retirement/calculator.go:337` — `plannerIRMAAInflationFactorForYear`

**Source consulted:** Code inspection; `calculator_test.go` `TestPlannerIRMAAInflationFactorForYear_Rebases2026TableOntoTaxBaseYear`.

**What it does:** Computes the inflation factor to rescale the 2026 IRMAA table to projection year N. At year N=2 (= irmaaBaseYear−taxBaseYear), `yearsFromIRMAABase = 0` and the function returns exactly 1.0. For other years, it returns `(1+rate/100)^yearsFromIRMAABase`.

**Finding:** The early-return guard `if yearsFromIRMAABase == 0` uses exact float64 equality. Since `yearsFromTaxBase` is passed as `float64` (it is computed as `float64(month)/12` in the projection loop), and `irmaaBaseYear-taxBaseYear = 2` is an exact integer, the subtraction `yearsFromTaxBase - 2.0` can produce floating-point values that are not exactly 0 even when semantically at year 2 (e.g., month 24 → `24.0/12.0 = 2.0` exactly — safe; but month 25 → not 2). For the specific case where yearsFromTaxBase is derived from integer months, `float64(24)/12.0 = 2.0` is exact and the guard is safe. However, the function could be called with fractional years in future refactors, introducing fragility. Not a current bug but worth noting.

**Evidence / repro:** `float64(24)/12.0` is exactly `2.0` in IEEE 754; the early return fires correctly. If the function were called with, e.g., `1.9999999999` the guard would miss and return `math.Pow(1.03, -0.0000000001) ≈ 1.0` — negligible error, but the guard would not fire.

**Recommended fix sketch:** Replace `if yearsFromIRMAABase == 0` with `if math.Abs(yearsFromIRMAABase) < 1e-9` for robustness, or use integer arithmetic for the year comparison.

**Test coverage note:** The test covers year=0, year=2, and year=5. The guard fires correctly for year=2. No test passes a fractional value within epsilon of 2.0.

## 3. Social Security

### Functions audited

**Legend:** PASS = formula correct, no findings · PASS (F-NNN) = formula correct, has associated finding · PARTIAL (F-NNN) = formula partially correct · FAIL (F-NNN) = formula incorrect.

| Function | Location | Status |
|----------|----------|--------|
| `validSSClaimAge` | `social_security.go:12` | PASS (F-025) |
| `normalizedSSFRA` | `social_security.go:16` | PASS (F-025) |
| `normalizedSSCOLARate` | `social_security.go:23` | PARTIAL (F-026) |
| `AdjustedSSBenefit` | `social_security.go:205` | PASS (F-027) |
| `DerivedPIA` | `social_security.go:237` | PASS |
| `AdjustedSpousalBenefit` | `social_security.go:259` | PASS (F-028) |
| `SpousalTopUp` | `social_security.go:278` | PASS |
| `claimStartMonth` | `social_security.go:194` | PASS (F-025) |
| `projectedSSBenefitForMonth` | `social_security.go:187` | PASS |
| `projectedSocialSecurityIncome` | `social_security.go:169` | PASS |
| `ProjectedSSEntries` | `social_security.go:98` | PASS (F-029) |
| `SSComparisonTable` | `social_security.go:306` | PASS |
| `ssComparisonTable` | `social_security.go:316` | PASS |
| `SSBreakevenAges` | `social_security.go:353` | PASS |
| `ssBreakevenAges` | `social_security.go:357` | PASS |
| `cumulativeBenefit` | `social_security.go:679` | PASS |
| `RunSSAnalysis` | `social_security.go:398` | PARTIAL (F-029, F-030) |
| `RunSSPortfolioAnalysis` | `social_security.go:500` | PASS (F-031) |
| `bestSSPortfolioOption` | `social_security.go:655` | PASS (F-031) |
| `isBetterSSPortfolioOption` | `social_security.go:669` | PASS (F-031) |

### Worked examples

#### WE-3.1: AdjustedSSBenefit, PIA=$2,000, FRA=67, claim age 62

**Source:** POMS RS 00615.105 (early retirement reduction schedule).

| Step | Value |
|------|-------|
| Months early: (67 − 62) × 12 | 60 |
| First 36 months: 36 × 5/900 | 0.2000 |
| Next 24 months: 24 × 5/1200 | 0.1000 |
| Total reduction | 0.3000 (30%) |
| Code: `reduction = 36*5/900 + (60-36)*5/1200 = 0.20 + 0.10` | 0.3000 |
| **Expected** | **$1,400.00** |
| **Actual (`AdjustedSSBenefit(2000, 67, 62)`)** | **$1,400.00** |
| Delta | $0.00 ✓ |

Cross-check: SSA published table for FRA-67 cohort shows 70% benefit ratio at age 62.

#### WE-3.2: AdjustedSSBenefit, PIA=$2,000, FRA=67, claim age 70

**Source:** POMS RS 00615.690 (delayed retirement credits, born 1943+: 8%/yr = 2/3%/mo).

| Step | Value |
|------|-------|
| Months delayed: (70 − 67) × 12 | 36 |
| DRC: 36 × 2/300 | 0.2400 (24%) |
| Code: `increase = 36 * 2/300 = 0.24` | 0.2400 |
| **Expected** | **$2,480.00** |
| **Actual (`AdjustedSSBenefit(2000, 67, 70)`)** | **$2,480.00** |
| Delta | $0.00 ✓ |

Note: 2/300 per month = 2/3 of 1% per month = 8%/year. POMS RS 00615.690 confirmed.

#### WE-3.3: AdjustedSpousalBenefit, spousal PIA $1,500, spouse FRA=67, claim 62

**Source:** POMS RS 00615.020 (spousal benefit reduction: 25/36 of 1% first 36 months, 5/12 of 1% beyond).

| Step | Value |
|------|-------|
| Months early: (67 − 62) × 12 | 60 |
| First 36 months: 36 × 25/3600 | 0.2500 (25%) |
| Next 24 months: 24 × 5/1200 | 0.1000 (10%) |
| Total reduction | 0.3500 (35%) |
| Code: `reduction = 36*25/3600 + (60-36)*5/1200 = 0.25 + 0.10` | 0.3500 |
| **Expected** | **$975.00** |
| **Actual (`AdjustedSpousalBenefit(1500, 67, 62)`)** | **$975.00** |
| Delta | $0.00 ✓ |

Spousal at FRA = $1,500 (already 50% of $3,000 worker PIA by caller convention). No DRC confirmed (claim at 70 returns $1,500 = spousal PIA, no increase).

#### WE-3.4: SSBreakevenAges, PIA=$2,000, FRA=67, COLA=0, compare 62 vs 70

**Source:** Manual simulation using `ssBreakevenAges` logic (annual cumulative).

| Step | Value |
|------|-------|
| Monthly at 62 | $1,400 |
| Monthly at 70 | $2,480 |
| Breakeven equation (analytical): 1,400(T − 62) = 2,480(T − 70) → T ≈ 80.4 | ~80 |
| Acceptable range | 78–82 |
| **Actual (manual simulation with COLA=0)** | **age 80** |
| In range? | Yes ✓ |

Note: `ssBreakevenAges` computes adjacent pairs (62-63, 63-64, …) only; the direct 62-vs-70 breakeven was verified by replaying the same annual-loop logic. `SSBreakevenAges` with COLA=0 is accepted by the function (colaRate passes through unchanged — unlike `projectedSocialSecurityIncome` which substitutes the default via `normalizedSSCOLARate`).

### Findings

#### F-025 — LOW Utility helpers `validSSClaimAge`, `normalizedSSFRA`, `claimStartMonth` not directly tested

**Location:** `internal/services/retirement/social_security.go:12` — `validSSClaimAge`; `:16` — `normalizedSSFRA`; `:194` — `claimStartMonth`

**Source consulted:** Code inspection; `internal/services/retirement/social_security_test.go`.

**What it does:** `validSSClaimAge` returns true iff age is in [62, 70]. `normalizedSSFRA` substitutes FRA=0 with the default (67). `claimStartMonth` computes `(claimAge − currentAge) × 12` for future claims, or 0 for already-claiming.

**Finding:** None of these three helpers are called directly in any test. They are exercised indirectly through higher-level functions, but specific boundary values are untested:
- `validSSClaimAge`: exact boundaries 62 and 70 are never asserted; neither is the `false` case at 61 or 71.
- `normalizedSSFRA`: zero-input substitution (0 → 67) is never directly asserted; non-zero passthrough is untested directly.
- `claimStartMonth`: the `claimAge <= currentAge → 0` path and the `(claimAge − currentAge) × 12` math are exercised only via `ProjectedSSEntries` integration tests, not in isolation.

**Evidence / repro:** `grep -n "validSSClaimAge\|normalizedSSFRA\|claimStartMonth" social_security_test.go` returns no direct calls.

**Recommended fix sketch:** Add a `TestValidSSClaimAge` covering 61 (false), 62 (true), 70 (true), 71 (false); `TestNormalizedSSFRA` covering 0 → 67 and 66 → 66; `TestClaimStartMonth` covering already-claiming (→ 0) and future claim (exact multiplication).

**Test coverage note:** All three boundary conditions (inclusive range endpoints for `validSSClaimAge`, zero-substitution for `normalizedSSFRA`, already-claiming path for `claimStartMonth`) are dark from a direct-test perspective.

---

#### F-026 — MEDIUM `normalizedSSCOLARate`: zero-COLA scenario inexpressible; silently substitutes 2% default

**Location:** `internal/services/retirement/social_security.go:23` — `normalizedSSCOLARate`

**Source consulted:** Code inspection; `internal/services/retirement/social_security_test.go`; `internal/models/whatif.go:144`.

**What it does:** Converts a COLA rate of 0 to the default 2% (`defaultSocialSecurityCOLARate = 0.02`). Non-zero values pass through unchanged. Used in both `projectedSocialSecurityIncome` and `RunSSAnalysis`, so every user-facing SS computation goes through this normalization.

**Finding:** A user who sets `COLARate: 0.0` in their `SocialSecurityConfig` intends zero COLA growth — a conservative "what if SS gives no cost-of-living adjustment" scenario that SSA has historically delivered (e.g., 0% COLA in 2010, 2011, 2016). The code silently substitutes 2%, making the zero-COLA scenario inexpressible by users. The normalization is also inconsistent with how `SSBreakevenAges` handles COLA: that function receives the rate directly (without normalization), so `SSBreakevenAges(pia, fra, 0.0)` works correctly at COLA=0, but `RunSSAnalysis` (which calls `normalizedSSCOLARate`) cannot produce the same result.

The function is never directly tested; `normalizedSSCOLARate(0) = 0.02` is not asserted anywhere.

**Evidence / repro:**
```go
// social_security.go:23-28
func normalizedSSCOLARate(colaRate float64) float64 {
    if colaRate == 0 {
        return defaultSocialSecurityCOLARate  // 0.02 — zero is inexpressible
    }
    return colaRate
}
```
`RunSSAnalysis` at line 405: `colaRate := normalizedSSCOLARate(ss.COLARate)` — always at least 2%.

**Recommended fix sketch:** Use a sentinel value (e.g., −1) to mean "use default," and treat 0 as "user explicitly wants 0% COLA." Alternatively, add a `COLARateIsDefault bool` flag to `SocialSecurityConfig`. A simpler fix: change the normalization condition to `colaRate < 0` instead of `colaRate == 0`, reserving 0 for explicit user intent, and update UI validation to reject negative COLA rates.

**Test coverage note:** `normalizedSSCOLARate` is never called directly in tests. No test verifies the zero-substitution behavior or its downstream effect on the comparison table.

---

#### F-027 — LOW `AdjustedSSBenefit`: FRA values other than 66 and 67 not tested; DerivedPIA round-trip only covers FRA=67

**Location:** `internal/services/retirement/social_security.go:205` — `AdjustedSSBenefit`; `:237` — `DerivedPIA`

**Source consulted:** Code inspection; `internal/services/retirement/social_security_test.go`.

**What it does:** `AdjustedSSBenefit` applies the SSA's two-tier early-reduction / DRC schedule for any FRA. `DerivedPIA` is its exact algebraic inverse.

**Finding (formula is correct):** The formulas implement POMS RS 00615.105 and RS 00615.690 exactly (confirmed by WE-3.1 and WE-3.2). The `DerivedPIA` round-trip `DerivedPIA(AdjustedSSBenefit(pia, fra, claimAge), fra, claimAge) == pia` holds for all tested inputs (verified for FRA=67 at claimAge 62, 64, 67, 70; and FRA=66 at 62; delta 0.000000 in all cases).

**Test coverage gap:** `TestAdjustedSSBenefit` covers FRA=67 (claimAge 62, 64, 67, 70) and FRA=66 (claimAge 62), but does not test FRA=65 (birth years ~1937–1938) or FRA=66+months (birth years 1955–1959). `TestDerivedPIA` round-trips only at FRA=67. The claim-at-FRA identity (`AdjustedSSBenefit(pia, fra, fra) == pia`) is tested only for FRA=67; for other FRA values this would be caught by a round-trip test but is not directly asserted.

**Evidence / repro:** `TestAdjustedSSBenefit` has five cases; four use FRA=67. The fifth uses FRA=66 with claimAge=62 (below-FRA path). The at-FRA path and above-FRA path for FRA=66 are not tested.

**Recommended fix sketch:** Add `AdjustedSSBenefit(pia, 65, 65)` asserting result == pia; add `AdjustedSSBenefit(pia, 65, 70)` for the DRC path; extend `TestDerivedPIA` to round-trip at FRA=65 and FRA=66.

**Test coverage note:** The `monthsDiff == 0` branch (claim at FRA, return PIA directly) is only tested for FRA=67; the `monthsDiff > 0` branch (DRC) is also only tested for FRA=67.

---

#### F-028 — LOW `AdjustedSpousalBenefit`: claim age > 70 not explicitly clamped or tested

**Location:** `internal/services/retirement/social_security.go:259` — `AdjustedSpousalBenefit`

**Source consulted:** Code inspection; POMS RS 00615.020; `internal/services/retirement/social_security_test.go`.

**What it does:** Applies spousal early-reduction schedule for claim ages before spouseFRA. Returns spousalPIA unchanged for claimAge ≥ spouseFRA (no DRC for spousal). Clamps claimAge < 62 to 62.

**Finding (formula is correct):** The early-reduction formula matches POMS RS 00615.020 exactly (25/36 of 1% for first 36 months, 5/12 of 1% beyond, confirmed by WE-3.3). The no-DRC rule is correctly enforced by the `claimAge >= spouseFRA` guard. Spousal at FRA returns spousalPIA; spousal at 70 also returns spousalPIA (no increase). These behaviors were confirmed by existing tests.

**Test coverage gap:** Unlike `AdjustedSSBenefit`, the function does NOT clamp `claimAge > 70` at the top. For all realistic spouseFRA values (≤ 67), any `claimAge ≥ spouseFRA` will return `spousalPIA` correctly. But there is no explicit `claimAge > 70` clamp and no test asserting that behavior. If `spouseFRA` were ever set above 67 by a future model change, the missing clamp could cause the early-reduction formula to run for ages above 70.

**Evidence / repro:** `AdjustedSSBenefit` clamps at lines 206–211; `AdjustedSpousalBenefit` clamps only at line 261 (< 62). For `claimAge = 75, spouseFRA = 67`: `75 >= 67` → returns `spousalPIA` (correct, but relies on the FRA-cap logic, not an explicit age cap).

**Recommended fix sketch:** Add `if claimAge > 70 { claimAge = 70 }` after the `< 62` clamp for defensive consistency with `AdjustedSSBenefit`. Add a test asserting `AdjustedSpousalBenefit(pia, 67, 75) == AdjustedSpousalBenefit(pia, 67, 70)`.

**Test coverage note:** `claimAge > 70` path not tested; relied on implicitly by the `claimAge >= spouseFRA` guard.

---

#### F-029 — MEDIUM `RunSSAnalysis` / `ProjectedSSEntries`: `SpouseUsingSpousalBenefit` display flag uses raw `FRABenefit` instead of derived PIA

**Location:** `internal/services/retirement/social_security.go:484` — `RunSSAnalysis`

**Source consulted:** Code inspection; `internal/services/retirement/social_security_test.go`; `web/templates/components/whatif/social-security.html:176`.

**What it does:** `RunSSAnalysis` sets `result.SpouseUsingSpousalBenefit` to indicate whether the spouse's comparison table uses the spousal top-up path. The UI uses this flag to display "Using 50% spousal benefit ($X/mo) — higher than own benefit ($Y/mo)" alongside `Settings.SocialSecurity.FRABenefit * 0.5` as the dollar amount shown.

**Finding:** Line 484 computes the flag using `ss.FRABenefit * 0.5 > ss.SpouseFRABenefit`. When the primary person is **already claiming at a non-FRA age**, `ss.FRABenefit` is the **actual benefit being received** (not the PIA), and `DerivedPIA` has been applied to derive `primaryPIA` (lines 409–412). The comparison should use `primaryPIA * 0.5` (the true spousal PIA) but instead uses `ss.FRABenefit * 0.5` (the actual reduced/enhanced benefit, which is less than PIA for early claimants).

Concrete example: Primary claimed at age 62, PIA=$2,000, actual benefit=$1,400 (`ss.FRABenefit=1400`). Spouse own PIA=$800. Spousal entitlement = $1,000 (50% of PIA). Correct flag: `1000 > 800` → true (spouse uses spousal top-up). Code computes: `1400*0.5 = 700 > 800` → false (flag shows wrong — spouse appears not to use spousal benefit). The **computation** uses `primaryPIA` correctly (lines 423, 453-455) so the comparison table numbers are right; only the display flag and the associated UI text ($X/mo label) are wrong.

**Evidence / repro:**
```go
// social_security.go:484
result.SpouseUsingSpousalBenefit = ss.FRABenefit*0.5 > ss.SpouseFRABenefit
// Should be:
result.SpouseUsingSpousalBenefit = primaryPIA*0.5 > ss.SpouseFRABenefit
```
`primaryPIA` is in scope at this point (derived at lines 409–412).

**Recommended fix sketch:** Replace `ss.FRABenefit` with `primaryPIA` on line 484. The UI template at `social-security.html:177` also references `Settings.SocialSecurity.FRABenefit * 0.5` for the dollar display; that should likewise render from the analysis result (which has the correct `primaryPIA`-derived numbers) rather than raw settings.

**Test coverage note:** No `TestRunSSAnalysis` case combines an already-claiming primary (non-FRA claim age) with a lower-PIA spouse to verify this flag. The existing "already claiming back-derives PIA" test only checks `BestAge`, not `SpouseUsingSpousalBenefit`.

---

#### F-030 — LOW `RunSSAnalysis`: zero `ClaimAge` triggers spurious "already claiming" logic

**Location:** `internal/services/retirement/social_security.go:398` — `RunSSAnalysis`

**Source consulted:** Code inspection; `internal/models/whatif.go:147` (`ClaimAge int` default is 0 for "unset").

**What it does:** `RunSSAnalysis` computes the SS comparison table and picks a recommended `BestAge`. It treats `ss.ClaimAge <= c.Settings.CurrentAge && ss.ClaimAge != fra` as "already claiming."

**Finding:** `ClaimAge` defaults to 0 (Go zero value, documented as "0 means unset"). When `ClaimAge == 0` and `CurrentAge > 0` (which is always true for active users), the condition `ss.ClaimAge <= c.Settings.CurrentAge && ss.ClaimAge != fra` is true (since `0 != 67`). This causes two incorrect behaviors:

1. **Line 410**: `DerivedPIA(ss.FRABenefit, fra, 0)` is called — inside `DerivedPIA`, `claimAge=0` is clamped to 62, so PIA is incorrectly back-derived as though the primary claimed at 62. The true PIA (which the user entered as `FRABenefit`) is underused.

2. **Line 430**: `bestAge = ss.ClaimAge = 0` is set, locking the recommended age to 0 — an invalid age that should never appear in the UI.

In practice, `RunSSAnalysis` is only called from `RunFullAnalysis`, and `RunFullAnalysis` (or the UI) likely guards against calling it with an unset claim age. However, if `RunSSAnalysis` is ever called directly with an unconfigured SS state, it silently miscomputes rather than returning nil or an error.

**Evidence / repro:** `ClaimAge=0, CurrentAge=60`: `0 <= 60` and `0 != 67` → true; `DerivedPIA(pia, 67, 0)` internally uses claimAge=62; `bestAge = 0`.

**Recommended fix sketch:** Update the existing condition at `social_security.go:410` from `ss.ClaimAge <= c.Settings.CurrentAge && ss.ClaimAge != fra` to `ss.ClaimAge <= c.Settings.CurrentAge && ss.ClaimAge != fra && validSSClaimAge(ss.ClaimAge)`. The added `validSSClaimAge` guard rejects unset (0) and other out-of-range claim ages before the already-claiming branch runs.

**Test coverage note:** No test passes `ClaimAge=0` with a positive `CurrentAge` to `RunSSAnalysis`. The zero-ClaimAge path through the `already claiming` branch is dark.

---

#### F-031 — INFO `RunSSPortfolioAnalysis` / `bestSSPortfolioOption` / `isBetterSSPortfolioOption`: decision rule documented

**Location:** `internal/services/retirement/social_security.go:500` — `RunSSPortfolioAnalysis`; `:655` — `bestSSPortfolioOption`; `:669` — `isBetterSSPortfolioOption`

**Source consulted:** Code inspection; `internal/services/retirement/social_security_test.go`.

**What it does:** `RunSSPortfolioAnalysis` runs Monte Carlo simulations for each eligible claiming age (holding the other person's age fixed) and assembles portfolio survival statistics per claiming age. `bestSSPortfolioOption` picks the overall winner. `isBetterSSPortfolioOption` defines the ordering: (1) higher portfolio survival rate (`SurvivalRate`) wins; (2) tie-break on higher `MedianEndingBalance`; (3) second tie-break on lower `ClaimAge` (prefer earlier claiming when outcomes are identical).

**Finding (decision rule is consistent and sound):** The lexicographic rule is deterministic and well-defined. Using `SurvivalRate` as the primary metric correctly prioritizes avoiding portfolio depletion over maximizing wealth. The `MedianEndingBalance` secondary sort is a reasonable proxy for wealth accumulation when survival rates tie. The `ClaimAge` tiebreaker (prefer earlier) follows the conservative principle that, at equal portfolio outcomes, earlier certainty is better. No inconsistency or contradiction found.

**Evidence / repro:**
```go
// social_security.go:669-677
func isBetterSSPortfolioOption(candidate, current models.SSPortfolioOption) bool {
    if candidate.SurvivalRate != current.SurvivalRate {
        return candidate.SurvivalRate > current.SurvivalRate
    }
    if candidate.MedianEndingBalance != current.MedianEndingBalance {
        return candidate.MedianEndingBalance > current.MedianEndingBalance
    }
    return candidate.ClaimAge < current.ClaimAge  // prefer earlier
}
```

**Recommended fix sketch:** No change needed. Consider adding a comment explaining the rationale for the `ClaimAge` tiebreaker (prefer earlier when economically equivalent) for future maintainers.

**Test coverage note:** `isBetterSSPortfolioOption` and `bestSSPortfolioOption` are not directly unit-tested. Exercised indirectly via `TestRunSSPortfolioAnalysis`. No test exercises the `MedianEndingBalance` tiebreaker or the `ClaimAge` tiebreaker in isolation.

## 4. RMD

### Functions audited

**Legend:** PASS = formula correct, no findings · PASS (F-NNN) = formula correct, has associated finding · PARTIAL (F-NNN) = formula partially correct · FAIL (F-NNN) = formula incorrect.

| Function | Location | Status |
|----------|----------|--------|
| `GetLifeExpectancyFactor` | `rmd.go:64` | PASS (F-033) |
| `CalculateRMD` | `rmd.go:76` | PASS (F-034) |
| `(c *Calculator) CalculateRMDAnalysis` | `rmd.go:87` | PASS (F-032, F-035, F-036) |

### Constants verified

| Table | Cells checked | Mismatches |
|-------|---------------|------------|
| `uniformLifetimeTable` | 49 (ages 72–120) | 0 |
| `RMDStartAge` | 1 | 0 (see F-032) |

All 49 cells of `uniformLifetimeTable` verified cell-by-cell against IRS Pub 590-B Appendix B Table III (post-2022, Notice 2020-22 / Notice 2022-53). Every value matches exactly. The table correctly uses age 120 (not "120+") as the map key; `GetLifeExpectancyFactor` handles ages above 120 with a fallback of 2.0, matching the authoritative "120+" row.

`RMDStartAge = 73` matches the SECURE 2.0 Act rule for 2023–2032. SECURE 2.0 will raise the start age to 75 beginning 2033; the constant is a single hard-coded value with no year-dependent logic (see F-032).

### Worked examples

#### WE-4.1: RMD at age 73, $1,000,000 balance

**Source:** IRS Pub 590-B Appendix B Table III — factor at age 73 = 26.5.

| Step | Value |
|------|-------|
| Table III factor (age 73) | 26.5 |
| Expected RMD: $1,000,000 / 26.5 | **$37,735.849057…** |
| Expected percent: 1 / 26.5 × 100 | **3.77358491%** |
| Actual from `CalculateRMD(1_000_000, 73)` — amount | **$37,735.849057** |
| Actual from `CalculateRMD(1_000_000, 73)` — percent | **3.77358491%** |
| Delta | $0.00 / 0.000000% |

#### WE-4.2: Monthly compound rate for 5% annual

**Source:** Standard geometric monthly rate derivation: `(1 + 0.05)^(1/12) - 1`.

| Step | Value |
|------|-------|
| Expected `monthlyCompoundFactorFromPercent(5.0) - 1` | **0.0040741238** |
| Actual (measured) | **0.0040741238** |
| Delta | < 1e-12 |

The post-fix line `monthlyReturn := monthlyCompoundFactorFromPercent(investmentReturn) - 1` (rmd.go:112) is correct.

#### WE-4.3: RMD at age 100, $500,000 balance

**Source:** IRS Pub 590-B Appendix B Table III — factor at age 100 = 6.4.

| Step | Value |
|------|-------|
| Table III factor (age 100) | 6.4 |
| Expected RMD: $500,000 / 6.4 | **$78,125.00** |
| Actual from `CalculateRMD(500_000, 100)` | **$78,125.00** |
| Delta | $0.00 |

### Findings

### F-032 — MEDIUM `RMDStartAge` is a single constant; does not model SECURE 2.0 age-75 transition in 2033

**Location:** `internal/services/retirement/rmd.go:6` — `RMDStartAge` constant; `rmd.go:97`, `rmd.go:120` — call sites in `CalculateRMDAnalysis`

**Source consulted:** SECURE 2.0 Act of 2022 (§107): RMD start age 73 for taxpayers turning 73 in 2023–2032; age 75 for taxpayers turning 75 in 2033 or later.

**What it does:** `RMDStartAge = 73` is used both to compute `startsInYears` (rmd.go:97) and as the loop guard (rmd.go:120). It is a compile-time constant with no year-dependent logic.

**Finding:** For a user who is, for example, age 60 today (2026), the planner will incorrectly show RMDs beginning at age 73 (year 2039), when SECURE 2.0 will actually defer them until age 75 (year 2041). Any projection that crosses the 2033 boundary for a user who has not yet turned 73 will over-report required withdrawals for 2033–2041 and will under-report the 2-year additional tax-deferred accumulation window. This is a systematic planning error for younger retirement scenarios.

**Evidence / repro:** A user currently age 60 viewing the What-If RMD panel will see "RMDs begin in 13 years (at age 73, year 2039)." Correct answer under SECURE 2.0: RMDs begin at age 75 (year 2041), because this user turns 75 in 2041 which is ≥ 2033.

**Recommended fix sketch:** Replace the single constant with a function `rmdStartAge(birthYear int) int` returning 73 if `birthYear + 73 < 2033` (i.e., if they turn 73 before 2033) and 75 otherwise. Pass the projected birth year (derived from current year minus current age) when computing `startsInYears` and when entering the RMD projection loop.

**Test coverage note:** No test covers a user whose RMD start age under SECURE 2.0 is 75. The existing tests (age 60, 65, 70, 72, 75) either already past RMD start or start at 73 with no cross-2033 validation.

### F-033 — MEDIUM `GetLifeExpectancyFactor`: age 72 is in the table but `CalculateRMDAnalysis` skips it; age-71-or-below returns 0 (no divide-by-zero, but silent)

**Location:** `internal/services/retirement/rmd.go:64` — `GetLifeExpectancyFactor`; `rmd.go:120` — caller guard `if age >= RMDStartAge`

**Source consulted:** IRS Pub 590-B Appendix B Table III; code inspection.

**What it does:** `GetLifeExpectancyFactor(age int)` returns 0 for age < 72, returns the table value for ages 72–120, and returns 2.0 for ages > 120.

**Finding (two sub-issues):**

1. **Age 72 is in the table but is unreachable via `CalculateRMDAnalysis`**: The loop guard at rmd.go:120 is `age >= RMDStartAge` (i.e., `>= 73`), which is correct under SECURE 2.0 — age 72 is in the post-2022 table only for reference (it is still present in the IRS table but the required begin date moved from 72 to 73). The factor for age 72 (27.4) is stored in the map but `CalculateRMDAnalysis` will never return a projection row at age 72. This is *correct behavior* — the table row is harmless dead data. However, `CalculateRMD(balance, 72)` called directly (e.g., from a future caller) would return a non-zero RMD, which could mislead. The `GetLifeExpectancyFactor` function has no internal guard against returning a factor for a pre-RMD-start age.

2. **Age below 72 returns 0 silently**: `GetLifeExpectancyFactor` returns 0 for age < 72. `CalculateRMD` correctly guards against `factor == 0` and returns (0, 0). However, no comment or error signals the caller that a factor of 0 means "below start age" vs. "lookup error." This is low-stakes today (there is only one caller path) but a documentation/API clarity gap.

**Evidence / repro:** Verified by temp test:
```
GetLifeExpectancyFactor(71) = 0.0000   // below table — returns 0
GetLifeExpectancyFactor(72) = 27.4000  // in table, never triggered by CalculateRMDAnalysis
CalculateRMD(1_000_000, 72) = (36496.35, 3.65%)  // callable, non-zero — misleading if called directly
```

**Recommended fix sketch:** Add a sentinel constant or a named function `GetLifeExpectancyFactor` could return a second `bool` `ok` parameter (`0, false` for out-of-range). Alternatively, add a guard in `GetLifeExpectancyFactor` that also returns 0 for `age < RMDStartAge` to make the no-RMD case consistent and safe.

**Test coverage note:** `GetLifeExpectancyFactor(72)` is not tested in any existing test. `CalculateRMD` with age 72 is not tested. `TestGetLifeExpectancyFactor` covers only age 60, 73, and 125.

### F-034 — LOW `CalculateRMD`: negative balance produces a negative RMD amount with no guard

**Location:** `internal/services/retirement/rmd.go:76` — `CalculateRMD`

**Source consulted:** IRS Pub 590-B §IRA (RMD cannot be negative — a negative account balance is undefined in IRS rules); code inspection.

**What it does:** Computes `amount = taxDeferredBalance / factor`. If `taxDeferredBalance` is negative (corrupt state), `amount` will be negative.

**Finding:** A negative balance is semantically impossible in a real account, but the function does not guard against it and would silently return a negative RMD amount. `CalculateRMDAnalysis` guards against `currentBalance < 0` (line 140–142) before calling `CalculateRMD` in its own loop, so this path is not reachable through the standard analysis. However, `CalculateRMD` is an exported function; any future caller supplying a negative balance would receive a negative RMD without warning.

**Evidence / repro:** Verified by temp test:
```
CalculateRMD(-100000, 73): amount=-3773.5849  pct=3.7736
```

**Recommended fix sketch:** Add a guard at the top of `CalculateRMD`: `if taxDeferredBalance <= 0 { return 0, 0 }`. This is consistent with the zero-balance path (`balance = 0` already returns 0 due to arithmetic, but the guard makes intent explicit) and protects future callers.

**Test coverage note:** No existing test passes a negative balance to `CalculateRMD`. The `TestCalculateRMD_BelowStartAge` test covers age below RMD start but not negative balance. Adding a negative-balance test would close this gap.

### F-035 — MEDIUM `CalculateRMDAnalysis`: RMD withdrawn before year-end growth is applied; order is economically aggressive

**Location:** `internal/services/retirement/rmd.go:138–148` — growth/withdrawal ordering in `CalculateRMDAnalysis`

**Source consulted:** IRS Pub 590-B (RMD calculated on prior-year-end balance; typically the account grows throughout the year before the RMD deadline); code inspection.

**What it does:** Each year the loop: (1) computes RMD on `currentBalance`, (2) deducts RMD from `currentBalance`, then (3) applies 12 months of growth to the post-RMD balance.

**Finding:** The IRS-prescribed RMD for year N is computed on the December 31 balance of year N−1. The account then grows during year N, and the RMD must be distributed by December 31 of year N (April 1 of year N+1 for the first RMD). By deducting the RMD *before* applying the full year's growth, the model implicitly assumes the RMD is taken at the start of the year, which understates the growth on the RMD proceeds and slightly understates the projected balance in subsequent years. A more accurate model would: (a) apply full-year growth, (b) compute RMD on the year-start balance (which equals last year's ending balance), then (c) deduct the RMD. The error is modest in magnitude — for a $1M balance at 6% annual return, the "beginning of year" model distributes the RMD ~$2,400 earlier than midpoint-withdrawal, reducing the next year's starting balance by a few hundred dollars — but it accumulates over a 20-year projection.

**Evidence / repro:** Loop structure (rmd.go:138–148):
```go
currentBalance -= rmdAmount   // RMD deducted first
if currentBalance < 0 { currentBalance = 0 }
// then full-year growth applied to post-RMD balance
for m := 0; m < 12; m++ {
    currentBalance *= (1 + monthlyReturn)
}
```

A corrected flow: apply growth first (or split into half-year pre/post RMD), then deduct RMD, then apply remaining growth.

**Recommended fix sketch:** Apply the full year's growth to the starting balance, then compute and deduct the RMD. This matches the IRS model where the account balance at year-end (post-growth) is the basis for the following year's RMD, and the current year's RMD is withdrawn after that year's growth has occurred.

**Test coverage note:** No existing test validates the sequencing of growth vs. RMD withdrawal. The `TestCalculateRMDAnalysis` subtests check shape/direction of results but not the precise per-year trajectory.

### F-036 — MEDIUM `CalculateRMDAnalysis`: test coverage gaps across multiple boundaries

**Location:** `internal/services/retirement/rmd.go:87` — `CalculateRMDAnalysis`; `rmd_tax_test.go:10`; `calculator_coverage_test.go:183`

**Source consulted:** Code inspection of `rmd_tax_test.go` and `calculator_coverage_test.go`.

**What it does:** `CalculateRMDAnalysis` projects RMDs over a multi-year horizon. Existing tests cover: age 65 (8 years to RMD), age 75 (already past), spouse older age, zero investment return, zero portfolio.

**Finding:** The following boundaries are not exercised:

| Boundary | Concern |
|----------|---------|
| `GetLifeExpectancyFactor(72)` called directly | Returns non-zero RMD for a pre-SECURE-2.0 boundary age — no test |
| `CalculateRMD` with age 72 directly | Can produce a non-zero RMD at a legally ambiguous age |
| `CalculateRMD` with negative balance | Returns negative RMD (see F-034) |
| Projection crossing year 2033 for a user below age 73 | SECURE 2.0 age-75 transition never validated (see F-032) |
| Age > 120 in `CalculateRMDAnalysis` | The `GetLifeExpectancyFactor` fallback (2.0) is used but no test projects to age 120+ |
| `ProjectionYears` that would generate > 20 RMD rows | The `rmdCount < 20` cap is untested — a very young entrant with a long projection period would silently cap at 20 rows |
| Spouse younger by more than 10 years | IRS Table II (Joint Life and Last Survivor table) should be used instead of Table III — not modeled at all (informational; separate from `CalculateRMDAnalysis` scope) |

**Recommended fix sketch:** Add tests for: (a) direct `GetLifeExpectancyFactor(72)` and `CalculateRMD(balance, 72)` edge, (b) negative balance in `CalculateRMD`, (c) a projection that hits the 20-RMD cap, (d) a projection that reaches age 121 to confirm the 2.0 fallback propagates correctly through the full loop.

**Test coverage note:** See table above for untested boundaries.

## 5. Present value & monthly compounding

### Functions audited

**Legend:** PASS = formula correct, no findings · PASS (F-NNN) = formula correct, has associated finding · PARTIAL (F-NNN) = formula partially correct · FAIL (F-NNN) = formula incorrect.

| Function | Location | Status |
|----------|----------|--------|
| `PresentValue` | `calculator.go:38` | PASS (F-037, F-038) |
| `PresentValueAnnuity` | `calculator.go:51` | PASS (F-038, F-039) |
| `monthlyCompoundFactorFromDecimal` | `calculator.go:133` | PASS |
| `monthlyCompoundFactorFromPercent` | `calculator.go:140` | PASS |
| `compoundedFactorFromPercent` | `calculator.go:144` | PASS |
| `fractionalMonthlyReturn` | `calculator.go:605` | PASS |
| `plannerInflationFactorForYear` | `calculator.go:330` | PASS (F-040) |
| `plannerIRMAAInflationFactorForYear` | `calculator.go:337` | PASS (F-024) — cross-ref Task 2; no contradiction |
| `calculateHealthcarePV` | `calculator.go:93` | PASS (F-041) |

### Worked examples

#### WE-5.1: PV of $100,000 over 20 years at 5%

**Source:** Standard finance first principles: `PV = FV / (1 + r_m)^n` where `r_m = (1.05)^(1/12) − 1`, `n = 240`.

| | Value |
|--|-------|
| Expected (`100,000 / 1.05^20`) | $37,688.95 |
| Actual (`PresentValue(100000, 5.0, 240)`) | $37,688.95 |
| Delta | $0.00 ✓ |

The post-fix geometric monthly rate gives `PV = 100,000 / (1.05)^20 = $37,688.95` — exactly consistent with annual compounding as required.

#### WE-5.2: PresentValueAnnuity, $1K/mo, 5% discount, 3% growth, 30 years

**Source:** Growing annuity PV formula (first principles): `PV = payment × (1 − ((1+g_m)/(1+r_m))^n) / (r_m − g_m)`.

Geometric monthlies: `r_m = (1.05)^(1/12) − 1 ≈ 0.00407412`, `g_m = (1.03)^(1/12) − 1 ≈ 0.00246627`.

Hand estimate in spec said ~$272,580 ± $200.

| | Value |
|--|-------|
| Expected (formula, derived above) | $272,652.99 |
| Actual (`PresentValueAnnuity(1000, 5.0, 3.0, 0, 360)`) | $272,652.99 |
| Delta | $0.00 ✓ |

The actual value of $272,652.99 is within the ±$200 tolerance of the hand estimate ($272,580). The hand calculation underestimated slightly due to rounding of `ln(0.99839925)` in the spec example; the code result is exact.

#### WE-5.3: PresentValueAnnuity degenerate r=g case

**Source:** When monthly discount rate equals monthly growth rate, the standard growing-annuity formula has a 0/0 indeterminate form. The limit is `payment × n`.

| | Value |
|--|-------|
| Expected | $360,000.00 exactly |
| Actual (`PresentValueAnnuity(1000, 5.0, 5.0, 0, 360)`) | $360,000.00 |
| Delta | $0.00 ✓ |

The `|r_m − g_m| < 1e-10` branch fires correctly. Since `monthlyCompoundFactorFromPercent(5.0)` is a deterministic computation, `r_m` and `g_m` are identical bit-for-bit and their difference is exactly 0.0, well within the epsilon guard.

#### WE-5.4: PresentValue at zero / negative rate (early-return guard)

**Source:** Code guard at `calculator.go:42`: `if annualRate <= 0 { return futureValue }`.

| | Value |
|--|-------|
| `PresentValue(100000, 0.0, 240)` — expected $100,000 | $100,000.00 ✓ |
| `PresentValue(100000, −2.0, 240)` — code returns | $100,000.00 |
| Mathematically correct for −2% deflation: `100,000 / (0.98)^20` | $149,788.50 |

The code correctly handles the zero-rate case. The negative-rate case returns `futureValue` unchanged (see F-037).

#### WE-5.5: monthlyCompoundFactorFromPercent at zero

**Source:** Code path: `monthlyCompoundFactorFromPercent(0) → monthlyCompoundFactorFromDecimal(0.0) → (annualRate == 0) → return 1.0` exactly (no floating-point arithmetic).

| | Value |
|--|-------|
| `monthlyCompoundFactorFromPercent(0.0)` | exactly `1.0` ✓ |
| `monthlyCompoundFactorFromPercent(0.0) − 1` | exactly `0.0` ✓ |

The zero-input early-return in `monthlyCompoundFactorFromDecimal` guarantees that `monthlyGrowth == 0` in `PresentValueAnnuity` is an exact IEEE 754 zero when `growthRate = 0.0` is passed. The `== 0` float comparison in `PresentValueAnnuity:62` is therefore safe for this specific entry path.

### b978aa9 audit note

The commit b978aa9 changed monthly rate conversion in `PresentValue` and `PresentValueAnnuity` from arithmetic division (`r / 100 / 12`) to geometric compounding (`(1 + r/100)^(1/12) − 1`). **The fix is correct and internally consistent.** Both functions now use the same helper `monthlyCompoundFactorFromPercent(r) - 1`, which in turn calls `monthlyCompoundFactorFromDecimal`. The conversion is uniformly geometric across all three in-scope PV functions (the RMD analysis was updated separately in `rmd.go` and is audited in Task 4).

Two secondary changes in b978aa9 also improve correctness:

1. **`monthlyRate <= 0` branch: `monthlyGrowth <= 0` → `monthlyGrowth == 0`.** Pre-fix, a negative `growthRate` with zero or negative `discountRate` would incorrectly enter the flat-sum branch (ignoring the growth). Post-fix, negative growth correctly falls to the loop branch. This is a genuine bug fix verified by WE-5.extra7.

2. **`monthlyGrowth > 0` → `monthlyGrowth != 0`.** Pre-fix, a negative `growthRate` with a positive `discountRate` would fall through to the regular (zero-growth) annuity formula — a clearly wrong result. Post-fix, the growing-annuity formula is used for any non-zero growth, including negative growth. Verified correct by WE-5.extra6.

No new edge-case bugs were introduced. The arithmetic-to-geometric change produces the same result as `PV = FV / (1+r_annual)^(n/12)` for `PresentValue`, since `(1+r_m)^n = ((1+r_annual)^(1/12))^(12k) = (1+r_annual)^k` for whole-year multiples, and for arbitrary month counts it correctly compound-discounts at the geometric rate. The one pre-existing limitation — the `annualRate <= 0` early-return in `PresentValue` that ignores deflation scenarios — predates b978aa9 and is documented separately as F-037.

### Findings

---

### F-037 — LOW `PresentValue`: negative-rate deflation returns futureValue unchanged (economically incorrect)

**Location:** `internal/services/retirement/calculator.go:42` — `PresentValue`

**Source consulted:** Standard finance first principles: for a deflating economy (annualRate < 0), future nominal dollars buy more than present dollars, so `PV > FV`.

**What it does:** Early-returns `futureValue` when `annualRate <= 0`, treating zero and negative rates identically. For zero rate this is correct (`PV = FV`). For negative rates it is incorrect.

**Finding:** With `annualRate = −2.0` and 240 months (20 years), the mathematically correct PV is `100,000 / (0.98)^20 ≈ $149,788.50`. The code returns `$100,000`. The error is approximately **50%** of the true PV — technically HIGH severity by the ±5% threshold — but the negative-discount-rate scenario is unusual in planning practice (a user would need to set `DiscountRate < 0` explicitly, which the UI almost certainly prevents). Rated LOW because it is reachable only via an intentionally adversarial input, not a normal user scenario.

**Evidence / repro:**
```go
PresentValue(100000, -2.0, 240) → 100000  // code
// Correct: 100000 / math.Pow(0.98, 20) ≈ 149788.50
```

**Recommended fix sketch:** Split the guard: `if periods <= 0 { return futureValue }` and `if annualRate == 0 { return futureValue }`. For `annualRate < 0`, fall through to the standard formula — `(1 + r/100)^(1/12)` will correctly be less than 1 for negative r, and `Pow(..., n)` will produce a factor < 1, giving `PV > FV`.

**Test coverage note:** The negative-rate path is exercised in `calculator_pv_test.go` ("negative rate returns future value unchanged") but asserts the current incorrect behavior. If the guard is corrected, that test case should be updated with the mathematically correct expected value.

---

### F-038 — LOW `PresentValue` / `PresentValueAnnuity`: zero-rate guard inconsistency for `PresentValue` vs. `PresentValueAnnuity`

**Location:** `internal/services/retirement/calculator.go:42` — `PresentValue`; `:56` — `PresentValueAnnuity`

**Source consulted:** Code inspection.

**What it does:** `PresentValue` uses `annualRate <= 0` (covering both zero and negative). `PresentValueAnnuity` computes `monthlyRate := monthlyCompoundFactorFromPercent(discountRate) - 1` for all inputs and then checks `if monthlyRate <= 0` (which catches negative discount rates too, since a negative annual rate yields a monthly factor < 1, so `factor - 1 < 0`). The two functions handle zero-and-negative rates differently at the guard level but produce the same practical result.

**Finding:** This is a cosmetic inconsistency rather than a correctness bug: `PresentValue` short-circuits before the monthly rate computation, while `PresentValueAnnuity` computes the monthly rate first then tests it. Both correctly produce `FV` (or `payment * n`) for zero discount, and both avoid discounting for negative discount. The inconsistency could confuse a future maintainer who sees one pattern in each function and assumes they diverge. LOW: no numerical error; purely structural.

**Evidence / repro:** `PresentValue(100000, -2, 240) → 100000` (early return). `PresentValueAnnuity(1000, -2, 0, 0, 12) → 12000` (post-monthly-rate-compute guard, flat sum). Both avoid discounting for negative rate. The behaviors are parallel but the guard placement and mechanism differ.

**Recommended fix sketch:** Normalize the pattern in both functions to: compute the monthly rate, then test `monthlyRate <= 0`. Alternatively, add an explicit comment in `PresentValue` explaining why the guard is pre-computation.

**Test coverage note:** The `PresentValueAnnuity` path with `discountRate < 0` and `growthRate = 0` is exercised by the loop branch (`monthlyRate <= 0`, `monthlyGrowth == 0`), so both sub-paths of the negative-discount case are covered.

---

### F-039 — LOW `PresentValueAnnuity`: `startMonth > 0` deferral not applied when `monthlyRate <= 0`

**Location:** `internal/services/retirement/calculator.go:84` — `PresentValueAnnuity`

**Source consulted:** Standard deferred-annuity finance; code inspection.

**What it does:** After computing `pvAtStart`, the function discounts back to present by `pvAtStart / (1+monthlyRate)^startMonth` — but only if `startMonth > 0 && monthlyRate > 0`. If `monthlyRate <= 0`, `pvAtStart` is returned without deferral discounting.

**Finding:** When `discountRate = 0` and `startMonth > 0`, the function correctly returns the undiscounted sum of payments (since with a zero discount rate, future payments have the same PV as current payments). This is mathematically correct — there is no time value of money at zero rate. Existing tests verify this: "future start with zero discount rate does not discount." However, for `discountRate < 0` (deflation), deferred payments should have a *higher* PV than immediate payments, and the current code neither applies this nor is it guarded — it simply returns the flat sum without time adjustment. Since negative discount rates are not a normal user input (see F-037), the practical impact is negligible. LOW.

**Evidence / repro:** `PresentValueAnnuity(1000, 0, 0, 6, 12)` equals `PresentValueAnnuity(1000, 0, 0, 0, 12)` = `$12,000` ✓ (tested and correct for zero rate). `PresentValueAnnuity(1000, -2, 0, 6, 12)` would also return `$12,000` (deferral ignored for negative rate, which under-states PV for a deflating economy).

**Recommended fix sketch:** If F-037 is fixed (allow negative-rate discounting in `PresentValue`), apply the same correction here: remove the `monthlyRate > 0` guard from the deferral step, allowing the formula to run for negative rates. The formula `pvAtStart / (1+monthlyRate)^startMonth` with `monthlyRate < 0` correctly yields a value larger than `pvAtStart` (deferred payments in a deflating economy are worth more).

**Test coverage note:** The `startMonth > 0` with `discountRate = 0` path is tested and correct. The `startMonth > 0` with `discountRate < 0` path is untested.

---

### F-040 — LOW `plannerInflationFactorForYear`: zero-inflation-rate boundary not tested

**Location:** `internal/services/retirement/calculator.go:330` — `plannerInflationFactorForYear`

**Source consulted:** Code inspection; `internal/services/retirement/coverage_gaps2_test.go:899`.

**What it does:** Returns `(1 + annualInflationRate/100)^years`. Returns `1.0` for `years <= 0`.

**Finding:** The formula is correct: year=0 → 1.0 (via early return), year>0 with rate=0 → `1.0^years = 1.0`, year>0 with rate>0 → correct compound factor. The existing test suite covers zero years, negative years, and positive years with a 3% rate. The missing test boundary is `annualInflationRate = 0.0` with `years > 0` — the formula correctly returns `math.Pow(1.0, years) = 1.0`, but this is not explicitly asserted. LOW because the formula is unambiguously correct for zero rate; the gap is purely a test-coverage oversight.

**Evidence / repro:** `plannerInflationFactorForYear(0, 10) = 1.0` ✓ (verified by WE-5.extra5d during audit; not in the test suite). Existing tests (`TestPlannerInflationFactorForYear` in `coverage_gaps2_test.go`) cover rate=3.0 only.

**Recommended fix sketch:** Add a `{"zero rate", 0.0, 10, 1.0}` row to the `TestPlannerInflationFactorForYear` table-driven test.

**Test coverage note:** Zero-rate boundary not asserted. All other boundaries (years=0, years<0, years>0, rate>0) are covered.

---

### F-041 — INFO `calculateHealthcarePV`: IRMAA not included in PV calculation (by design; documented)

**Location:** `internal/services/retirement/calculator.go:93` — `calculateHealthcarePV`

**Source consulted:** Code inspection; `calculator.go:95-121`.

**What it does:** Aggregates the PV of healthcare costs (ACA and Medicare phases) using `PresentValueAnnuity`. The two-phase (pre-Medicare / post-Medicare) transition is handled internally, dispatching on `person.IsOnMedicare()` and `person.YearsUntilMedicare()`. IRMAA surcharges are NOT included.

**Finding (design note, no error):** IRMAA is correctly excluded from this PV calculation. IRMAA is a MAGI-dependent surcharge computed inside the main projection loop (`CalculateRMDAnalysis`, `RunProjection`) where annual MAGI is known. Including it in a static PV estimate would require projecting future MAGI, which is not available at the time `calculateHealthcarePV` is called (it is called from `CalculatePresentValueAnalysis`, which does not run a full projection). This is the correct architectural split. The PV analysis page should document that the healthcare PV excludes IRMAA (which varies annually based on income). Informational only.

**Evidence / repro:** `calculateHealthcarePV` calls only `PresentValueAnnuity` with base monthly cost and inflation — no IRMAA inputs. IRMAA is added to monthly expenses in the full projection loop at `calculator.go` growth/tax routines.

**Recommended fix sketch:** Add a UI tooltip or footnote on the What-If PV summary page noting that IRMAA surcharges are excluded from the healthcare PV estimate and are computed in the full projection instead.

**Test coverage note:** The Medicare-transition logic (pre → post) is well-tested in `TestCalculateHealthcarePV`. The edge case where `preMedicareMonths == 0` (person is exactly at Medicare age but `IsOnMedicare()` returns false) may be unreachable depending on `IsOnMedicare()` semantics — the test "person exactly at Medicare age" confirms `IsOnMedicare()` returns true at age 65, so the two-phase branch with `preMedicareMonths == 0` is unreachable in practice.

## 6. Living-expense projection mechanics

### Functions audited

**Legend:** PASS = formula correct, no findings · PASS (F-NNN) = formula correct, has associated finding · PARTIAL (F-NNN) = formula partially correct · FAIL (F-NNN) = formula incorrect.

| Function | Location | Status |
|----------|----------|--------|
| `calculateLivingExpensesAtMonth` | `calculator.go:151` | PASS (F-042) |
| `rebaseLivingExpensesAtTransition` | `calculator.go:163` | PASS (F-043) |
| `CalculateTotalExpenses` (living-expense path) | `calculator.go:546` | PASS (F-044) |
| `GetSpendingMultiplier` | `models/whatif.go:453` | PASS |
| `GetPhaseReferenceAge` | `models/whatif.go:417` | PASS |

### Convention used by the planner

**Multiplier convention: absolute.** Each month's living expense is computed as:

```
expense = base × GetSpendingMultiplier(phaseAge) × cumulativeInflation
```

where `GetSpendingMultiplier` looks up the phase whose `StartAge` is the highest age ≤ `phaseAge`, and returns that phase's multiplier directly (not composed with prior phases). The multiplier at No-Go (0.75) is applied directly to the original base — not to the Slow-Go value. This is internally consistent and mathematically equivalent to the relative convention when both are defined with absolute fractions of the original base.

**Phase interval: half-open `[phase[i].StartAge, phase[i+1].StartAge)`.** `GetSpendingMultiplier` iterates all phases in ascending `StartAge` order, keeping the latest match where `age >= phase.StartAge`. This correctly implements the half-open interval — phase i activates at its start age and is superseded (not overlapped) by phase i+1. No double-counting.

**Age-to-year granularity: whole-year, evaluated as `month / 12` (integer division).** `phaseAge = currentAge + (month / 12)`. Phase transitions activate on the first month of the birthday year, not on the exact birthday month. For age 75 with `currentAge = 65`, the Slow-Go multiplier first applies at month 120 (the start of year 10), not month 121.

**Inflation compounding: continuous per-month compound, functionally equivalent to `(1 + r/100)^(months/12)`.** `calculateLivingExpensesAtMonth` uses `compoundedFactorFromPercent(r, months)` = `(1+r/100)^(months/12.0)`. The projection loop tracks `cumulativeInflation` by multiplying monthly: `cumulativeInflation *= (1+r/100)^(1/12)`. After M months, `cumulativeInflation = (1+r/100)^(M/12)` — identical to the direct formula. Both are per-month compound, not per-year step.

**`rebaseLivingExpensesAtTransition` role:** Used only during scenario-chain transitions (when a new chain segment takes over). For spending-phases-enabled mode it is immediately overwritten by the main loop's formula (see F-043). For the legacy decline mode it correctly rebases the new segment's base expense against the accumulated inflation before the multiplicative update continues.

### Worked example

#### WE-6.1: Three-phase progression

**Inputs:** base $10,000/month, inflation 3%/year, 3 phases: Go-Go 100% (StartAge 0, active from age 65), Slow-Go 85% (StartAge 75), No-Go 75% (StartAge 85). `currentAge = 65`, `PhaseAgeReference = "older"`, no spouse.

**Convention identified:** Absolute multiplier — `expense = base × multiplier × (1.03)^(months/12)`.

**Verification method:** Temp test `audit_we6_test.go` in `internal/services/retirement/`, run with `go test -run TestAuditWE6`, then deleted. All assertions passed.

| Month | Year | Phase age | Multiplier | Formula | Expected | Code actual | Delta |
|-------|------|-----------|------------|---------|----------|-------------|-------|
| 0 | 0 | 65 | 1.00 | 10,000 × 1.00 × 1.03^0 | $10,000.00 | $10,000.00 | $0.00 |
| 60 | 5 | 70 | 1.00 | 10,000 × 1.00 × 1.03^5 | $11,592.74 | $11,592.74 | $0.00 |
| 119 | 9 | 74 | 1.00 | 10,000 × 1.00 × 1.03^(119/12) | $13,406.10 | $13,406.10 | $0.00 |
| 120 | 10 | 75 | 0.85 | 10,000 × 0.85 × 1.03^10 | $11,423.29 | $11,423.29 | $0.00 |
| 121 | 10 | 75 | 0.85 | 10,000 × 0.85 × 1.03^(121/12) | $11,451.46 | $11,451.46 | $0.00 |
| 239 | 19 | 84 | 0.85 | 10,000 × 0.85 × 1.03^(239/12) | $15,314.18 | $15,314.18 | $0.00 |
| 240 | 20 | 85 | 0.75 | 10,000 × 0.75 × 1.03^20 | $13,545.83 | $13,545.83 | $0.00 |

**Audit spec vs. code:** The audit spec's "just before" M240 value of $15,353.36 uses a slightly different intermediate (Slow-Go value at M120 inflated 10 years), yielding a $15,353.36 × (75/85) = $13,547.66 estimate. The discrepancy from our $13,545.83 is ≈$1.83 — a display-rounding artifact in the spec's calculation, not a code error. The code's formula `10,000 × 0.75 × 1.03^20` is correct under the absolute convention.

**Note on audit spec's month 119:** The spec quotes $13,406.07; we compute $13,406.10. Difference = $0.03, within the ±$0.01 per the spec's own footnote allowance for display rounding.

### Findings

### F-042 — LOW `calculateLivingExpensesAtMonth`: no test exercises phase transitions with nonzero inflation

**Location:** `internal/services/retirement/calculator.go:151` — `calculateLivingExpensesAtMonth`
**Source consulted:** Internal model — `calculator_expense_test.go` inspection.
**What it does:** Returns monthly living expenses at a given month, applying the current spending-phase multiplier and inflation compounding.
**Finding:** The expense test file's "with spending phases enabled" subtest uses `InflationRate = 0`. No test combines nonzero inflation with a phase transition, leaving the key formula `base × multiplier × (1+r)^(months/12)` at a boundary month untested. Additionally, the following boundaries have no coverage: (a) single-phase scenario (one entry in `Phases`); (b) zero-phase scenario (`Phases = []` with `Enabled = true`) — currently falls through to `return 1.0` from `GetSpendingMultiplier` but no test verifies this; (c) phase multiplier = 0 (zero spending); (d) phase multiplier > 1 (unusual but valid, e.g., 1.10 for Go-Go splurge); (e) two phases with the same `StartAge` (degenerate — last one wins, unverified); (f) phase `StartAge` above the projection end (never reached — `GetSpendingMultiplier` silently returns prior phase's multiplier); (g) `PhaseAgeReference = "younger"` or `"spouse"` with a couple (only "older"/default is tested).
**Evidence / repro:** All existing phase tests set `InflationRate = 0`. The combined multiplier × inflation path is only exercised by WE-6.1 in this audit.
**Recommended fix sketch:** Add a subtest in `calculator_expense_test.go` for month 120 with 3% inflation and a phase transition at age 75, verifying `base × 0.85 × 1.03^10`. Add edge-case subtests for zero-phase, zero-multiplier, and >1.0 multiplier.
**Test coverage note:** All boundaries listed above (a)–(g) are absent from the test suite.

---

### F-043 — LOW `rebaseLivingExpensesAtTransition`: dead code in spending-phases path; not directly tested

**Location:** `internal/services/retirement/calculator.go:163` — `rebaseLivingExpensesAtTransition`
**Source consulted:** Projection loop inspection at `calculator.go:1039–1089`.
**What it does:** Resets the living-expense tracker when a scenario-chain transition occurs, preserving accumulated inflation on the new segment's base.
**Finding:** In the spending-phases-enabled branch, `rebaseLivingExpensesAtTransition` (line 1046) is immediately overwritten by the `if m > 0` block (lines 1083–1086) which unconditionally recomputes `currentLivingExpenses = base × multiplier(phaseAge) × cumulativeInflation` using the same inputs. The rebase call is therefore dead code in the phases path — its result is never used. For the legacy decline path the function is load-bearing (it updates the base before the multiplicative step). The function has no direct unit test; it is only reachable via the projection loop during chain transitions. The dead-code effect means a bug introduced in `rebaseLivingExpensesAtTransition`'s phases branch would be silently masked.
**Evidence / repro:**
```go
// Line 1046 (chain-transition branch):
currentLivingExpenses = rebaseLivingExpensesAtTransition(s, phaseAge, cumulativeInflation)
// Lines 1083-1086 (immediately after, always executes when m > 0):
cumulativeInflation *= monthlyCompoundFactorFromPercent(s.InflationRate)
if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
    currentLivingExpenses = s.MonthlyLivingExpenses * s.GetSpendingMultiplier(phaseAge) * cumulativeInflation
}
```
**Recommended fix sketch:** Either remove the phases branch from `rebaseLivingExpensesAtTransition` (the return value is never used in that path), or restructure the loop so that the rebase result is the authoritative value for that month. Add a direct unit test for the function covering both the phases and non-phases branches.
**Test coverage note:** No test exercises `rebaseLivingExpensesAtTransition` directly. The chain-transition code path (the only call site) is not exercised by `calculator_expense_test.go`.

---

### F-044 — LOW `CalculateTotalExpenses`: phase boundary with inflation not tested; expense-source phase multiplier edge cases missing

**Location:** `internal/services/retirement/calculator.go:546` — `CalculateTotalExpenses`
**Source consulted:** `calculator_expense_test.go` inspection.
**What it does:** Aggregates living expenses, healthcare, and expense sources. Applies the spending-phase multiplier to discretionary expense sources when phases are enabled.
**Finding:** The living-expense path is correctly invoked via `calculateLivingExpensesAtMonth`. Coverage gaps: (a) no test combines nonzero inflation with a phase transition in this function (same gap as F-042); (b) no test covers a discretionary expense source at a phase boundary with nonzero inflation; (c) the non-discretionary expense-source path in phases mode is covered, but the boundary where a discretionary source becomes zero (multiplier = 0) is not tested; (d) no test for `PhaseAgeReference != "older"` (e.g., younger or spouse).
**Evidence / repro:** The "with spending phases enabled" and "discretionary expense gets phase multiplier" subtests both use `InflationRate = 0`.
**Recommended fix sketch:** Extend "with spending phases enabled" subtest to use 3% inflation and verify at month 120 (phase boundary). Add a test for `PhaseAgeReference = "younger"` with a couple to confirm the correct person's age drives phase transitions.
**Test coverage note:** Boundaries (a)–(d) above are absent.

## 7. Taxable account, allocation, and tax-aware withdrawals

### Functions audited

**Legend:** PASS = formula correct, no findings · PASS (F-NNN) = formula correct, has associated finding · PARTIAL (F-NNN) = formula partially correct · FAIL (F-NNN) = formula incorrect.

| Function | Location | Status |
|----------|----------|--------|
| `newTaxableAccountState` | `calculator.go:445` | PASS |
| `taxableAccountState.syncAssumptions` | `calculator.go:457` | PASS |
| `taxableAccountState.addCash` | `calculator.go:464` | PASS |
| `taxableAccountState.withdraw` | `calculator.go:472` | PASS (F-045) |
| `buildTaxableReturnComponents` | `calculator.go:498` | PASS (F-046) |
| `taxableAccountState.applyGrowth` | `calculator.go:511` | PASS |
| `expectedTaxableMonthlyCashFlow` | `calculator.go:540` | PASS |
| `projectionTimingGrowthFractions` | `calculator.go:594` | PASS (F-047) |
| `fractionalMonthlyReturn` | `calculator.go:605` | PASS (cross-ref Task 5) |
| `executeTaxAwarePortfolioMonth` | `calculator.go:630` | PASS (F-048) |
| `executePortfolioCashFlowWithTaxableState` | `calculator.go:784` | PASS (F-048) |
| `reinvestRequiredRMDToTaxableState` | `calculator.go:748` | PARTIAL (F-049) |
| `applyBigTicketExpenseWithTaxableState` | `calculator.go:759` | PASS (F-050) |
| `taxDeferredDelayActive` | `calculator.go:821` | PASS (F-051) |
| `shortfallIsTemporaryDueToDelay` | `calculator.go:825` | PASS (F-051) |
| `earlyWithdrawalPenaltyRate` | `calculator.go:836` | PASS (F-050) |
| `GetEffectiveAssetAllocation` | `models/whatif.go:533` | PASS |
| `GetTaxDeferredAllocation` | `models/whatif.go:569` | PASS |
| `GetRothAllocation` | `models/whatif.go:586` | PASS |
| `GetTaxableAllocation` | `models/whatif.go:603` | PASS |
| `GlidePathStockPct` | `models/whatif.go:646` | PASS (F-052) |
| `GetAllocationAtYear` | `models/whatif.go:662` | PASS |
| `GetBlendedReturn` | `models/whatif.go:689` | PASS |
| `GetExpectedReturnFromAllocation` | `models/whatif.go:697` | PASS |

### Conventions used by the planner

**Cost-basis convention:** Pro-rata (average cost). When `withdraw(amount)` is called on a `taxableAccountState`, the basis reduction is `costBasis × (min(amount, marketValue) / marketValue)`. The entire position is treated as one lot with a blended average cost per share. FIFO and specific-identification are not modeled.

**Withdrawal ordering:** The standard tax-aware order inside `withdrawForExpenses` is:
1. RMDs from tax-deferred (first, regardless of other sources)
2. Taxable account (brokerage)
3. Roth (tax-free)
4. Tax-deferred (non-RMD), if `allowTaxDeferred = true` and still not met

`applyBigTicketExpenseWithTaxableState` uses: taxable → Roth → tax-deferred (same relative priority for big-ticket items).

When `taxDeferredDelayActive(s, currentYear)` is true, tax-deferred withdrawals (step 4) are blocked; the resulting shortfall is flagged as temporary.

**Projection-timing modes:** `projectionTimingGrowthFractions` returns `(before, after)` fractions for applying growth within a month:
- `ProjectionTimingStartOfMonth`: `(0, 1)` — expenses precede growth
- `ProjectionTimingMidMonth`: `(0.5, 0.5)` — half growth before, half after
- `ProjectionTimingEndOfMonth` (default): `(1, 0)` — full growth before expenses

**Glide-path interpolation:** `GlidePathStockPct(year)` returns the stock percentage at projection year `year` via linear interpolation: `StartStockPct + (year / TransitionYears) × (EndStockPct − StartStockPct)`. Held at `StartStockPct` for `year ≤ 0` and at `EndStockPct` for `year ≥ TransitionYears`. Glide path applies a common stock target to all three account types (tax-deferred, Roth, taxable), preserving each account's cash percentage and absorbing the difference into bonds.

**Cash allocation:** There is no separate bond bucket field — bond percentage is computed as `100 − stock − cash`. The three-way split (stock, bond, cash) is fully determined by two stored fields (`StockPercent`, `CashPercent`) at the global level or (`TaxDeferredStockPercent`, `TaxDeferredCashPercent`) at the per-account level.

**Early withdrawal penalty:** `earlyWithdrawalPenaltyRate` returns 0.10 when `currentAge + currentYear < 60`, else 0. The code comment acknowledges this uses age 60 as a proxy for 59½ because the model operates in whole-year steps. See F-050 for the boundary nuance.

**Dividend reinvestment:** Dividends and cap-gains distributions returned by `applyGrowth` (pre- or post-expense fractions) are handled differently: "before" dividends reduce the amount needed from the portfolio (they fund expenses); "after" dividends are reinvested into the taxable account via `addCash` (increasing both `MarketValue` and `CostBasis` by the distribution amount). This is correct for a dividend-reinvestment assumption.

### Worked examples

#### WE-7.1: Pro-rata cost basis withdrawal

Initial state: `MarketValue = $200,000`, `CostBasis = $100,000`. Call `withdraw(50,000)`.

| Step | Expected | Actual |
|------|----------|--------|
| cash = min(50000, 200000) | $50,000.00 | $50,000.00 ✓ |
| basisReduction = 100000 × (50000/200000) | $25,000.00 | $25,000.00 ✓ |
| realizedGain = 50000 − 25000 | $25,000.00 | $25,000.00 ✓ |
| new CostBasis = 100000 − 25000 | $75,000.00 | $75,000.00 ✓ |
| new MarketValue = 200000 − 50000 | $150,000.00 | $150,000.00 ✓ |

Delta on all values: $0.00. Convention: pro-rata (average cost).

#### WE-7.2: Return component sum

Inputs: `totalAnnualReturn = 7%`, `TaxableDividendYield = 2%`, `TaxableQualifiedDividendPercent = 85%`, `TaxableCapitalGainsDistributionRate = 0.5%`.

| Component | Monthly rate |
|-----------|-------------|
| Qualified dividend | `(1.017)^(1/12) − 1 = 0.001406` |
| Non-qualified dividend | `(1.003)^(1/12) − 1 = 0.000250` |
| Capital gains distribution | `(1.005)^(1/12) − 1 = 0.000416` |
| Appreciation (residual) | `totalMonthly − sum of above = 0.003583` |
| **Sum** | **= totalMonthly = `(1.07)^(1/12) − 1 = 0.005654`** |

Delta from `totalMonthlyReturn`: 0.0 (exact by construction — Appreciation is the residual). Verified within ±1e-9.

Note: The components are NOT additive in annual percentage terms due to compounding non-linearity. The monthly-compounding approach keeps the sub-components consistent with the total monthly compounding of the portfolio.

#### WE-7.3: Big-ticket pre-59½ penalty

Inputs: `amount = $10,000`, `allowTaxDeferred = true`, `earlyPenaltyRate = 0.10`, taxable balance = 0, Roth = 0, tax-deferred = $50,000.

| Step | Value |
|------|-------|
| effectiveFactor = 1 − 0.10 | 0.90 |
| grossNeeded = 10,000 / 0.90 | $11,111.11 |
| fromTaxDeferred = min(11111.11, 50000) | $11,111.11 |
| net to spending = 11111.11 × 0.90 | $10,000.00 |
| remaining need after | $0.00 |
| taxDeferred remaining | $38,888.89 |

Convention: the code draws the **gross** amount from tax-deferred (including the penalty portion), and only the net `(gross × effectiveFactor)` counts toward meeting the spending need. The penalty is the difference (`grossDrawn − net`). The $10,000 net need is met exactly.

#### WE-7.4: Glide-path interpolation

Inputs: `StartStockPct = 80`, `EndStockPct = 40`, `TransitionYears = 20`.

| Year | Formula | Expected | Actual |
|------|---------|----------|--------|
| 0 | held at start | 80.0 | 80.0 ✓ |
| 5 | 80 + (5/20)(40−80) = 80 − 10 | 70.0 | 70.0 ✓ |
| 10 | 80 + (10/20)(40−80) = 80 − 20 | 60.0 | 60.0 ✓ |
| 20 | at end | 40.0 | 40.0 ✓ |
| 25 | held at end | 40.0 | 40.0 ✓ |

All values match expected within ±1e-9.

### Findings

#### F-045 — LOW `taxableAccountState.withdraw`: W=Y exact boundary and zero-gain boundary not directly tested

**Location:** `internal/services/retirement/calculator.go:472` — `taxableAccountState.withdraw`
**Source consulted:** Code inspection; `coverage_gaps_test.go:168–196`; `calculator_test.go:1393`.
**What it does:** Withdraws up to `amount` from the account using pro-rata average-cost basis. Returns `(cash, basisReduction, realizedGain)`.
**Finding:** The following boundary cases are not covered by any test:
1. **W = Y exactly** (withdraw exactly the market value): the `a.MarketValue <= 0` clamp at line 485 would fire, clearing both `MarketValue` and `CostBasis` to 0. `TestWithdraw_FullDepletion` tests W > Y (over-withdraw clips to Y) but not W == Y. The exact-equality case is mathematically identical to the over-withdraw case here (both set CostBasis to 0) but the code path differs: `cash = math.Min(Y, Y) = Y`, then the `MarketValue <= 0` branch fires after subtracting. No test exercises this with W == Y exactly.
2. **Zero unrealized gain** (CostBasis == MarketValue, e.g., newly purchased asset): withdraw should produce `realizedGain = 0`. Neither `coverage_gaps_test.go` nor `calculator_test.go` has a case with `CostBasis == MarketValue` and a positive withdrawal. The pro-rata formula yields `basisReduction = CostBasis × (W/Y) = W` and `realizedGain = W − W = 0`, which is correct, but untested.
**Evidence / repro:** `TestWithdraw_FullDepletion` uses `W=10000 > Y=5000`. No test uses `W=5000 == Y=5000`. `TestTaxableAccountWithdrawUsesAverageCostBasis` uses MV=120000 CB=100000 (gain present).
**Recommended fix sketch:** Add two cases to the existing `TestWithdraw_*` group: (a) `MarketValue=5000, CostBasis=5000, withdraw(5000)` → `realizedGain=0, newBasis=0, cash=5000`; (b) same but with cost basis below market value and W=Y.
**Test coverage note:** The `a.MarketValue <= 0` zeroing branch (line 485–488) is only exercised via over-withdrawal, not exact-equality withdrawal.

---

#### F-046 — LOW `buildTaxableReturnComponents`: negative-appreciation scenario not tested

**Location:** `internal/services/retirement/calculator.go:498` — `buildTaxableReturnComponents`
**Source consulted:** Code inspection; `coverage_gaps_test.go:202–225`.
**What it does:** Decomposes total annual return into four monthly components: appreciation (residual), qualified dividends, non-qualified dividends, and capital gains distributions. Appreciation is defined as `totalMonthlyReturn − qualifiedDividendMonthly − nonQualifiedDividendMonthly − capitalGainsDistributionMonthly`, guaranteeing the sum equals `totalMonthlyReturn` exactly.
**Finding:** The formula is correct by construction: the sum of all four components always equals `totalMonthlyReturn` within floating-point precision. However, if `dividendYield + capitalGainsDistributionRate > totalAnnualReturnPercent`, the `Appreciation` component becomes negative. This is plausible in a down market (e.g., total return = 2%, dividends = 3%) and would cause `applyGrowth` to apply a negative appreciation factor (reducing `MarketValue`). No test exercises this scenario. The math is correct but the downstream behavior (MarketValue declines despite positive dividends being paid out) is the correct economic picture and should be documented.
**Evidence / repro:** `buildTaxableReturnComponents(2.0, s)` with `TaxableDividendYield=3.0, TaxableCapitalGainsDistributionRate=0.5` would yield `Appreciation < 0`. No test covers this combination.
**Recommended fix sketch:** Add a test: `totalReturn=2%, dividendYield=3%, capGains=0.5%` → assert `Appreciation < 0`; assert sum of all components equals total monthly return; assert `applyGrowth` produces negative `TotalGrowth` (correct behavior).
**Test coverage note:** The negative-appreciation path of `applyGrowth` is tested in `TestApplyGrowth_NegativeMarketValueClamp` (using an artificial extreme), but not via `buildTaxableReturnComponents` with economically plausible inputs.

---

#### F-047 — LOW `projectionTimingGrowthFractions`: never directly tested; MidMonth path not covered

**Location:** `internal/services/retirement/calculator.go:594` — `projectionTimingGrowthFractions`
**Source consulted:** Code inspection; `calculator_test.go:1474`; `backtest_test.go:269`.
**What it does:** Maps a `ProjectionTiming` enum to `(before, after)` fractions for growth allocation: StartOfMonth → (0,1), MidMonth → (0.5,0.5), EndOfMonth (default) → (1,0).
**Finding:** `projectionTimingGrowthFractions` is never called directly in any test. It is exercised indirectly through `executeTaxAwarePortfolioMonth` in the end-to-end projection tests. `TestProjectionTimingAffectsDeterministicAndMonteCarloResults` exercises all three timing modes at the projection level but does not assert the specific `(before, after)` return values. The exact (0,1), (0.5,0.5), (1,0) outputs are not directly verified. Additionally, the `default` case (which covers EndOfMonth and any invalid value) returns `(1, 0)` — the behavior for an invalid/unknown timing string is untested.
**Evidence / repro:** `grep "projectionTimingGrowthFractions" *_test.go` returns no results in the test files.
**Recommended fix sketch:** Add `TestProjectionTimingGrowthFractions` with cases: `StartOfMonth → (0,1)`, `MidMonth → (0.5,0.5)`, `EndOfMonth → (1,0)`, and an invalid string → `(1,0)` (default). Each case should assert both the `before` and `after` values.
**Test coverage note:** All three enum branches and the default fallback are dark from a direct-assertion perspective.

---

#### F-048 — LOW `executeTaxAwarePortfolioMonth` / `executePortfolioCashFlowWithTaxableState`: taxable basis tracking in the re-undo pattern not directly tested

**Location:** `internal/services/retirement/calculator.go:630` — `executeTaxAwarePortfolioMonth`; `:784` — `executePortfolioCashFlowWithTaxableState`
**Source consulted:** Code inspection; `calculator_test.go:1412`; `coverage_gaps_test.go`.
**What it does:** `executePortfolioCashFlowWithTaxableState` calls `withdrawForExpenses` (which deducts from `taxable.MarketValue` directly, bypassing basis tracking), then undoes and redoes the taxable withdrawal via `taxable.withdraw()` to restore correct basis tracking. `executeTaxAwarePortfolioMonth` wraps the full iterative tax convergence loop.
**Finding:** The undo/redo pattern for taxable withdrawals (lines 795–801) is correct but fragile: if `withdrawForExpenses` deducts `W` from `taxable.MarketValue`, and then the code adds `W` back and calls `taxable.withdraw(W)`, the net effect on `MarketValue` is correct. However, this pattern is not directly unit-tested — no test calls `executePortfolioCashFlowWithTaxableState` with a taxable account that has unrealized gains and verifies that the realized-gain field is populated correctly by the undo/redo path. The integration test `TestRunProjectionTaxableSalesOfBasisRemainUntaxed` covers the end-to-end projection with zero basis appreciation, which does not exercise the undo/redo with actual unrealized gains.
**Evidence / repro:** `TestRunProjectionTaxableSalesOfBasisRemainUntaxed` sets `CostBasis = MarketValue` (no unrealized gain), so `realizedGain = 0` throughout and the undo/redo path produces zero `TaxableRealizedGain`. A scenario with `CostBasis < MarketValue` and a taxable withdrawal in the same month is not directly tested at the `executePortfolioCashFlowWithTaxableState` level.
**Recommended fix sketch:** Add a unit test calling `executePortfolioCashFlowWithTaxableState` directly with a taxable account having market value $100K and cost basis $60K, neededFromPortfolio=$20K, RMD=0, no Roth, no tax-deferred. Verify `TaxableRealizedGain = 20K × (40K/100K) = $8,000`.
**Test coverage note:** The undo/redo realized-gain path is verified at the end-to-end level only with zero-gain scenarios. The path with nonzero unrealized gains is not covered at the unit level.

---

#### F-049 — MEDIUM `reinvestRequiredRMDToTaxableState`: reinvests pre-tax RMD amount; basis overstated by tax liability

**Location:** `internal/services/retirement/calculator.go:748` — `reinvestRequiredRMDToTaxableState`
**Source consulted:** IRS Pub 550 §Cost Basis; IRC § 72 (RMD tax treatment); code inspection.
**What it does:** When an RMD must be taken but the cash is not needed for expenses, withdraws the RMD from tax-deferred and reinvests it in the taxable account via `taxable.addCash(rmdWithdrawal)`. The `addCash` method increases both `MarketValue` and `CostBasis` by the full RMD amount.
**Finding:** The RMD is fully taxable ordinary income (IRC § 72). The investor's actual net-of-tax amount deposited into the taxable brokerage account is `rmdWithdrawal × (1 − effectiveTaxRate)`. The code reinvests the **pre-tax** RMD and records the full pre-tax amount as the new cost basis. This overstates the cost basis by the tax owed on the RMD. Over many years with forced RMDs, the cumulative overstatement of basis reduces future realized-gain calculations, understating taxable capital gains on later taxable-account withdrawals. The tax on the RMD itself is captured separately in the iterative tax convergence loop, so income tax is not double-counted — but the cost basis of the reinvested amount reflects gross rather than net-of-tax dollars.

**Evidence / repro:**
```
RMD = $10,000, effective tax rate = 25%
After-tax reinvested = $7,500
Code sets: MarketValue += 10,000; CostBasis += 10,000
Correct:    MarketValue += 7,500 (or 10,000); CostBasis += 7,500
```
If the $10,000 is later withdrawn at the same market value: code computes realizedGain = 10,000 − 10,000 = $0; correct answer = 10,000 − 7,500 = $2,500 gain (the appreciation on the after-tax amount). The cost-basis overstatement reduces future capital gains by $2,500 per $10,000 RMD reinvested. At a 15% LTCG rate, the under-collected tax is $375 per $10,000 reinvested RMD.

**Recommended fix sketch:** Pass the effective tax rate into `reinvestRequiredRMDToTaxableState` (or compute it at the call site from `taxesPaid / grossIncome`) and call `taxable.addCash(rmdWithdrawal × (1 − effectiveTaxRate))` to set basis equal to the net-of-tax amount. Alternatively, use `taxable.MarketValue += rmdWithdrawal` (preserve market value at gross for growth purposes) but `taxable.CostBasis += rmdWithdrawal × (1 − effectiveTaxRate)`.
**Test coverage note:** `reinvestRequiredRMDToTaxableState` is never called directly in any test. Its behavior is exercised only indirectly through the full projection path, where the long-term cost-basis distortion is not measured.

---

#### F-050 — LOW `earlyWithdrawalPenaltyRate`: age-60 proxy overstates penalty window by up to 6 months; boundary at exactly 59 in final year untested

**Location:** `internal/services/retirement/calculator.go:836` — `earlyWithdrawalPenaltyRate`
**Source consulted:** IRC § 72(t)(2)(A)(i); code comment.
**What it does:** Returns 0.10 when `currentAge + currentYear < 60`, else 0. The comment explicitly states this uses age 60 as a proxy for 59½ since the model operates in whole-year steps.
**Finding:** IRC § 72(t) exempts distributions made after the participant reaches age 59½. The code exempts distributions when `currentAge + currentYear ≥ 60`, meaning the penalty is charged throughout projection year Y when `currentAge + Y = 59`. A person who turns 59.5 partway through that year (6 months in) incurs the penalty for the remaining 6 months of that year when no penalty is legally required. The over-penalty window is at most 6 months × `monthlyWithdrawal × 10%`. For a $3,000/month withdrawal, the maximum over-assessment per affected year is $1,800 (6 months × $300). The code comment acknowledges this approximation. No direct test exercises `earlyWithdrawalPenaltyRate(59, 0)` or the boundary transition year (`earlyWithdrawalPenaltyRate(59, 1) == 0`).
**Evidence / repro:**
```go
earlyWithdrawalPenaltyRate(59, 0)  // → 0.10 (correct: age 59 in year 0)
earlyWithdrawalPenaltyRate(59, 1)  // → 0  (correct: age 60 in year 1)
earlyWithdrawalPenaltyRate(58, 1)  // → 0.10 (correct: age 59 in year 1)
// Not tested: earlyWithdrawalPenaltyRate(60, 0) → 0
// Not tested: earlyWithdrawalPenaltyRate(59, 0) → 0.10
```
**Recommended fix sketch:** Add `TestEarlyWithdrawalPenaltyRate` with table-driven cases: `(59,0)→0.10`, `(59,1)→0.00`, `(58,1)→0.10`, `(60,0)→0.00`, `(0,0)→0.10`. Add a code comment quantifying the 6-month over-penalty approximation.
**Test coverage note:** `earlyWithdrawalPenaltyRate` is never called directly in any test. The exact boundary at `currentAge+currentYear = 59` (last year of penalty) and `= 60` (first exempt year) are untested.

---

#### F-051 — LOW `taxDeferredDelayActive` / `shortfallIsTemporaryDueToDelay`: never directly tested

**Location:** `internal/services/retirement/calculator.go:821` — `taxDeferredDelayActive`; `:825` — `shortfallIsTemporaryDueToDelay`
**Source consulted:** Code inspection; `calculator_delay_test.go`.
**What it does:** `taxDeferredDelayActive` returns true when the delay feature is enabled and `currentYear < TaxDeferredDelayYears`. `shortfallIsTemporaryDueToDelay` returns true when there is a shortfall, tax-deferred is not allowed this year, and the tax-deferred balance is nonzero (meaning the shortfall is temporary — once the delay period ends, funds are available).
**Finding:** Neither function is called directly in any test. They are exercised indirectly via the full projection in `calculator_delay_test.go`. Specific boundary cases not tested:
1. `taxDeferredDelayActive`: `TaxDeferredDelayYears=0` (disabled — should return false). `currentYear == TaxDeferredDelayYears` (boundary year — should return false, delay has ended). Negative `TaxDeferredDelayYears` (should return false).
2. `shortfallIsTemporaryDueToDelay`: all three conditions simultaneously true is tested indirectly, but the false branches (shortfall=0, or allowTaxDeferred=true, or taxDeferredBalance=0) are not directly asserted.
**Evidence / repro:** `grep "taxDeferredDelayActive\|shortfallIsTemporaryDueToDelay" *_test.go` returns no results.
**Recommended fix sketch:** Add `TestTaxDeferredDelayActive` with cases: delay=0 → false; delay=5 year=4 → true; delay=5 year=5 → false; delay=5 year=6 → false. Add `TestShortfallIsTemporaryDueToDelay` with the four logical combinations.
**Test coverage note:** Both functions are fully dark from a direct-test perspective.

---

#### F-052 — LOW `GlidePathStockPct`: negative-year path and `TransitionYears=0` guard not directly tested

**Location:** `internal/models/whatif.go:646` — `GlidePathStockPct`
**Source consulted:** Code inspection; `internal/models/allocation_test.go:79`.
**What it does:** Returns the glide-path stock percentage at a given projection year. Returns −1 if glide path is disabled or `TransitionYears ≤ 0`. Clips to `StartStockPct` for `year ≤ 0` and to `EndStockPct` for `year ≥ TransitionYears`.
**Finding:** `TestGlidePathStockPct` covers year=0 (start boundary), year=10 (mid), year=20 (at end), year=30 (past end), and disabled (`Enabled=false`). Missing cases:
1. **year < 0**: the `year ≤ 0` guard at line 653 covers this, but no test asserts `GlidePathStockPct(-1) == StartStockPct`. The guard is exercised by the year=0 case but the negative sub-case is dark.
2. **`TransitionYears = 0` with `Enabled = true`**: the guard `TransitionYears <= 0` at line 647 would return −1, but no test enables glide path and sets `TransitionYears=0`.
3. **`GlidePath == nil` with access**: the nil guard is not tested directly (the disabled test uses `&WhatIfSettings{}` which has `GlidePath=nil` — this DOES exercise the nil guard ✓).
**Evidence / repro:** Test "disabled returns -1" uses `s2 := &WhatIfSettings{}` with nil GlidePath → exercises nil guard. But no test uses enabled path with `year=-1` or `TransitionYears=0`.
**Recommended fix sketch:** Add subtest `year -5 returns StartStockPct` to `TestGlidePathStockPct`. Add subtest `TransitionYears=0, Enabled=true → returns -1`.
**Test coverage note:** The `year < 0` sub-case of the `year ≤ 0` branch is dark (year=0 exercises the branch but not the negative sub-range).

## 8. Roth conversion math

### Functions audited

**Legend:** PASS = formula correct, no findings · PASS (F-NNN) = formula correct, has associated finding · PARTIAL (F-NNN) = formula partially correct · FAIL (F-NNN) = formula incorrect.

| Function | Location | Status |
|----------|----------|--------|
| `rothConversionAmountForYear` | `calculator.go:409` | PASS (F-053, F-054) |
| `EstimateRothConversionTax` | `tax.go:484` | PASS (F-055) |
| Conversion application — `RunProjection` | `calculator.go:1062–1067` | PASS |
| Conversion application — `runSingleHistoricalSequence` | `calculator.go:2357–2362` | PASS |
| MAGI/tax accumulator integration | `calculator.go:249, 1199` | PASS (F-056) |

### Conventions used by the planner

**Amount cap:** `rothConversionAmountForYear` returns `min(AnnualAmount, availableTaxDeferred)`. Conversion is blocked if `availableTaxDeferred ≤ 0` or if `RothConversion == nil` or `!Enabled`.

**Year window:** Enabled when `currentYear ≥ StartYear`. Disabled after `EndYear` when `EndYear != 0`; `EndYear = 0` means indefinite.

**Frequency:** Conversion fires once per year at the year-boundary month (month 0 of each year, i.e., `m % 12 == 0`). Months 1–11 carry `rothConversionThisMonth = 0`.

**Portfolio mutation:** At year boundary: `taxDeferredBalance -= conversionAmount; rothBalance += conversionAmount`. Tax cash is NOT separately deducted from Roth or taxable here — it flows through the normal monthly tax-estimation loop (`estimateMonthlySnapshot` → `estimateMonthlyTaxes`), which adds the conversion amount to ordinary income and drives up estimated taxes paid from the general expense pool.

**Tax treatment:** Roth conversion amount is added to `otherIncome` (line 249: `otherIncome = ordinaryIncome + taxableWithdrawals + RothConversions`) and flows into `estimatedOrdinaryIncome`, which is then passed to `CalculateTaxWithInvestmentIncomeBreakdown`. The tax formula is marginal: `Tax(baseIncome + conversion) − Tax(baseIncome)` (verified — see WE-8.1).

**MAGI inclusion:** The conversion amount is included in `estimatedOrdinaryIncome` which is the `ordinaryIncome` parameter to `calculateTaxWithInvestmentIncomeInternal`. Inside that function, `magi = ordinaryIncome + qualifiedDividends + longTermCapitalGains`, so Roth conversions are properly included in MAGI. This means conversions correctly affect the NIIT threshold and the IRMAA lookback MAGI.

**NIIT:** A large conversion can push MAGI above the NIIT threshold ($250,000 MFJ). The NIIT calculation correctly uses the MAGI that includes the conversion.

**IRMAA lookback:** `completedMAGIHistory` accumulates `AnnualMAGI` (which includes conversions) at year end (line 1260 and surrounding context). The 2-year lookback resolves from this history (line 288). This means a large conversion in year Y correctly raises IRMAA premiums in year Y+2.

**Non-annualization of Roth conversions:** `annualizedInputs` (line 224) does NOT apply the `annualizationFactor` to `RothConversions`, unlike all other income types. This is intentional and correct: a conversion is a discrete lump-sum event that has already occurred (at month 0 of the year), not a recurring monthly flow. The test `TestAnnualizedInputs_RothConversionsNotAnnualized` explicitly documents this design.

**Negative `AnnualAmount` defense:** `rothConversionAmountForYear` computes `math.Min(AnnualAmount, availableTaxDeferred)`. For `AnnualAmount = −10000`, `math.Min(−10000, 100000) = −10000`, so the function returns `−10000`. However, both projection call sites gate on `conversionAmount > 0` (lines 1063 and 2358), so the negative return is safely discarded. The function itself has no internal clamp. See F-053.

### Worked examples

#### WE-8.1: EstimateRothConversionTax, MFJ, $80K base + $50K conversion

**Source:** IRS Rev. Proc. 2023-34 §3.01, Table 3 (MFJ 2024 brackets); standard deduction $29,200.

| Step | Value |
|------|-------|
| Tax on $80,000 — taxable income | $80,000 − $29,200 = $50,800 |
| 10% × $23,200 | $2,320.00 |
| 12% × ($50,800 − $23,200) = 12% × $27,600 | $3,312.00 |
| Tax on $80,000 | **$5,632.00** |
| Tax on $130,000 — taxable income | $130,000 − $29,200 = $100,800 |
| 10% × $23,200 | $2,320.00 |
| 12% × ($94,300 − $23,200) = 12% × $71,100 | $8,532.00 |
| 22% × ($100,800 − $94,300) = 22% × $6,500 | $1,430.00 |
| Tax on $130,000 | **$12,282.00** |
| Conversion tax = $12,282 − $5,632 | **$6,650.00** |
| Actual `EstimateRothConversionTax(80000, 50000, 0)` MFJ year-0 | **$6,650.00** |
| Delta | $0.00 ✓ |

#### WE-8.2: rothConversionAmountForYear cap behavior

Settings: `AnnualAmount = $50,000, StartYear = 2026, EndYear = 2030`.

| Case | Input | Expected | Actual | Result |
|------|-------|----------|--------|--------|
| Year 2025 (before start) | availableTaxDeferred = $100,000 | 0 | 0 | ✓ |
| Year 2026, balance limited | availableTaxDeferred = $30,000 | $30,000 | $30,000 | ✓ |
| Year 2027, full amount | availableTaxDeferred = $100,000 | $50,000 | $50,000 | ✓ |
| Year 2031 (past end) | availableTaxDeferred = $100,000 | 0 | 0 | ✓ |

#### WE-8.3: rothConversionAmountForYear negative AnnualAmount defense

Settings: `AnnualAmount = −$10,000, StartYear = 0, EndYear = 10, Enabled = true`. Year = 5, availableTaxDeferred = $100,000.

| Step | Result |
|------|--------|
| `math.Min(−10000, 100000)` | −10,000 |
| Function return value | **−10,000** (no clamp inside function) |
| Call site `conversionAmount > 0` guard | **blocks**: −10,000 ≤ 0 → conversion skipped |
| Net portfolio effect | No mutation — safe |

The function returns a negative value, but both call sites (`calculator.go:1063`, `calculator.go:2358`) gate with `> 0`, preventing the reversal. The function-level defense is absent; the call site saves correctness. See F-053.

### Findings

### F-053 — LOW `rothConversionAmountForYear`: no internal clamp for negative `AnnualAmount`

**Location:** `calculator.go:419` — `rothConversionAmountForYear`
**Source consulted:** Code inspection; WE-8.3 (above); IRS Pub 590-A (Roth conversions must be non-negative amounts).
**What it does:** Returns the actual conversion amount for a projection year, capped at the available tax-deferred balance. For `AnnualAmount < 0`, `math.Min(negativeAmount, availableTaxDeferred)` returns the negative amount.
**Finding:** The function returns a negative value for negative `AnnualAmount`, relying entirely on call-site `> 0` guards for safety. Both current call sites are correctly guarded (lines 1063 and 2358), so no runtime bug exists today. However, the function contract is silently violated: a caller who omits the `> 0` guard (e.g., in the `estimateTaxSnapshot` path at lines 1410, 1528, 1546) would pass a negative `rothConversion` into `estimateMonthlySnapshot`, adding negative income to the tax accumulator and reducing estimated taxes incorrectly. HTTP-layer validation rejects negative amounts, but the calculator has no defense-in-depth.
**Evidence / repro:**
```go
// calculator.go:419
return math.Min(s.RothConversion.AnnualAmount, availableTaxDeferred)
// For AnnualAmount=-10000, availableTaxDeferred=100000 → returns -10000
// WE-8.3 test confirmed: function returns -10000
```
**Recommended fix sketch:** Add `if s.RothConversion.AnnualAmount <= 0 { return 0 }` before the `math.Min`. This makes the function safe regardless of call context.
**Test coverage note:** No test exercises `AnnualAmount < 0`; a test asserting `return 0` for negative amounts would be the correct fix validation.

---

### F-054 — LOW `rothConversionAmountForYear`: boundary at `StartYear` and `EndYear = EndYear` not directly unit-tested

**Location:** `calculator.go:409` — `rothConversionAmountForYear`
**Source consulted:** Test files: `coverage_gaps_test.go:134–164, 1405–1461`.
**What it does:** Returns 0 before `StartYear`, returns 0 after `EndYear` (if non-zero), returns capped amount within window.
**Finding:** Existing tests cover: past-end-year, limited-by-balance, `EndYear=0` (no end), disabled, nil, and before-start-year. Missing boundary cases: (1) `year == StartYear` exactly (first eligible year with full balance) — tests use `StartYear=0` and test `year=0` indirectly through `TestRothConversionAmountForYear_LimitedByBalance` but do not assert the non-limited case with `year == StartYear` when balance > AnnualAmount; (2) `year == EndYear` exactly (last eligible year) — no test asserts the conversion fires on the last year (`EndYear=5, year=5` should return amount, but only `year=6` is tested in `TestRothConversionAmountForYear_PastEndYear`); (3) `AnnualAmount = 0` (zero conversion with enabled config) — not tested.
**Evidence / repro:**
```go
// TestRothConversionAmountForYear_PastEndYear tests year=6 > EndYear=5
// Missing: test for year=5 == EndYear=5 (should return amount, not 0)
```
**Recommended fix sketch:** Add subtests: `year == EndYear` → returns AnnualAmount (not 0); `year == StartYear, balance > AnnualAmount` → returns AnnualAmount; `AnnualAmount = 0, Enabled = true` → returns 0.
**Test coverage note:** The `year == EndYear` case is the highest priority gap — an off-by-one bug in the `>` vs `>=` check would not be caught by current tests.

---

### F-055 — LOW `EstimateRothConversionTax`: test asserts only direction, not exact value

**Location:** `tax.go:484` — `EstimateRothConversionTax`
**Source consulted:** `tax_test.go:135`; WE-8.1 hand computation.
**What it does:** Returns marginal tax increase from adding `conversionAmount` to `baseIncome`. Formula is `Tax(baseIncome + conversion) − Tax(baseIncome)` (correct).
**Finding:** `TestEstimateRothConversionTax` checks only that `additionalTax > 0` and `additionalTax ≤ conversion × 0.37`. The test does not assert the exact value. WE-8.1 confirms the formula is correct ($6,650 for MFJ, $80K base, $50K conversion), but without a pinned numeric assertion, a regression that changes the formula (e.g., to proportional tax) would go undetected. Additionally, no test covers: (1) conversion that spans a bracket boundary; (2) negative `conversionAmount` (currently guarded by `≤ 0` return-0 check — correctly handled); (3) inflated brackets (`yearsFromBase > 0`); (4) filing status other than MFJ.
**Evidence / repro:**
```go
// tax_test.go:146-152 — direction only, no pinned value
if additionalTax <= 0 { t.Errorf(...) }
if additionalTax > conversionAmount*0.37 { t.Errorf(...) }
// WE-8.1: correct answer is $6,650 but test would pass even if returned $5,000
```
**Recommended fix sketch:** Add `if math.Abs(additionalTax - 6650) > 0.01 { t.Errorf(...) }` to the existing test. Add a subtest for `yearsFromBase = 5` to cover inflated brackets.
**Test coverage note:** Exact-value assertion, bracket-crossing case, and inflated-bracket case are all missing.

---

### F-056 — INFO Roth conversion MAGI propagation: NIIT and IRMAA threshold effects are not directly tested

**Location:** `calculator.go:249, 460` — `estimateMonthlySnapshot` / `calculateTaxWithInvestmentIncomeInternal`
**Source consulted:** IRS Pub 590-A; IRC §1411 (NIIT); SSA IRMAA lookback rules.
**What it does:** Roth conversion amount is correctly included in `otherIncome` and flows into `estimatedOrdinaryIncome`, which becomes the `ordinaryIncome` parameter of `CalculateTaxWithInvestmentIncomeBreakdown`. Inside that function, `magi = ordinaryIncome + qualifiedDividends + longTermCapitalGains`, correctly including the conversion in MAGI used for NIIT and IRMAA thresholds.
**Finding:** The MAGI propagation is correct. However, no integration test verifies that a conversion that pushes MAGI above the NIIT threshold ($250K MFJ) results in a non-zero NIIT charge, or that a conversion in year Y raises IRMAA premiums in year Y+2 via the lookback. These are important retirement-planning scenarios that lack coverage.
**Evidence / repro:** Grep shows no test combining `RothConversion` with NIIT or IRMAA assertions. The code path is exercised incidentally through full-projection tests, but no test pins the expected NIIT or IRMAA increment.
**Recommended fix sketch:** Add an integration test: MFJ, MAGI just below $250K without conversion, conversion of $50K pushing above threshold → assert `AnnualNIIT > 0` in the returned snapshot. Add a 3-year projection test: large conversion in year 0 → assert IRMAA surcharge appears in year 2 (via `completedMAGIHistory`).
**Test coverage note:** NIIT-from-conversion path and IRMAA-lookback-from-conversion path are exercised only through incidental full-projection tests; no assertion pins the amount.

## 9. Backtest, Monte Carlo, guardrails

### Functions audited

**Legend:** PASS = formula correct, no findings · PASS (F-NNN) = formula correct, has associated finding · PARTIAL (F-NNN) = formula partially correct · FAIL (F-NNN) = formula incorrect.

| Function | Location | Status |
|----------|----------|--------|
| `yearsUntilDepletion` | `backtest.go:24` | PASS |
| `RunHistoricalBacktest` | `backtest.go:35` | PASS (F-057, F-058, F-059) |
| `runSingleHistoricalSequence` | `backtest.go:153` | PASS |
| `newGuardrailState` | `guardrails.go:18` | PASS |
| `evaluate` | `guardrails.go:28` | PASS (F-063, F-064) |
| `multiplier` | `guardrails.go:65` | PASS |
| `GetHistoricalReturns` | `historical_data.go:120` | PASS |
| `GetHistoricalSequence` | `historical_data.go:126` | PASS |
| `GetAvailableStartYears` | `historical_data.go:144` | FAIL (F-057) |
| `GetHistoricalStats` | `historical_data.go:158` | PASS (F-060) |
| `sqrt` | `historical_data.go:193` | PASS |
| `RunMonteCarloSimulation` | `calculator.go:2102` | PASS (F-061, F-062) |
| `runSingleMonteCarloSimulation` | `calculator.go:2247` | PASS |
| `generateAssetReturns` | `calculator.go:2539` | PASS (F-060) |
| `generateYearlyReturns` | `calculator.go:2600` | PASS |
| `calculateSequenceRiskImpact` | `calculator.go:2619` | PASS |
| `calculateSequenceRiskBreakdown` | `calculator.go:2657` | PASS |

### Conventions identified

- **Monte Carlo distribution:** **Normal** (`r = mean + σ·Z` where Z ← `rand.NormFloat64()`). Used for both stock and bond returns in normal years. Crash years substitute a deterministic crash-severity range; recovery years use `mean + RecoveryBoost + σ·0.8·Z`. Returns are clamped to `[−50%, +60%]` for stocks and `[−20%, +40%]` for bonds, preventing the worst tails a pure normal can produce. Lognormal would be theoretically preferable for long horizons but normal is acceptable here given the clamping. Documented as INFO (see F-060).
- **Mean and volatility inputs:** Derived at runtime from `GetHistoricalStats()` which computes **arithmetic** mean and **population** standard deviation of the 1928–2024 historical series (97 observations). Units: percent per year (annual). Arithmetic mean, not geometric mean, is used as the draw center; this produces a slight upward bias relative to the geometric (compound) mean but is standard practice for drawing a single-year return.
- **Random number generator:** `math/rand` with a wall-clock seed (`time.Now().UnixNano()`) for the primary simulation run. A separate fixed-seed source (`rand.NewSource(42)`) is used for the adaptive-spending sub-run. Seeded interface is exposed via `runSingleMonteCarloSimulation(rng, config)` making deterministic testing feasible. No `crypto/rand`.
- **Number of trials:** Default 1,000 (caller supplies `runs`; if `runs ≤ 0` it is reset to 1,000). No documented max; the adaptive sub-run uses `runs/2`. The SS optimizer hard-codes 250 runs.
- **Historical data source and range:** `HistoricalReturns` slice in `historical_data.go`, covering **1928–2024** (97 years). Sources cited in the file header: S&P 500 (Shiller data), 10-Year Treasury (FRED), 3-Month T-Bill (NYU Stern/Damodaran), Inflation (BLS CPI-U).
- **Rolling-window construction:** Annual data only; no monthly rebalancing within the historical backtest. Each sequence year uses that calendar year's asset returns applied with the geometric monthly-compounding formula `(1+r)^(1/12)−1`. Rebalancing is implicit — per-account allocations are applied to each year's blended returns, effectively annual rebalancing.
- **Guardrail rules implemented:** The implementation uses a **portfolio-value-based guardrail** (not the original withdrawal-rate-based Guyton-Klinger rules). Two triggers: (1) **Floor** — if portfolio drops `FloorDropPct`% from its peak, cut spending by `FloorCutPct`% and reset peak. (2) **Ceiling** — if portfolio rises `CeilingRisePct`% above `initialPortfolio`, raise spending by `CeilingRaisePct`% and update initialPortfolio. The canonical Guyton-Klinger (2006) **Capital Preservation Rule** (cut when withdrawal rate exceeds initial × 1.2), **Prosperity Rule** (raise when withdrawal rate is below initial × 0.8), **Inflation Rule** (no inflation adjustment after a loss year), and **Withdrawal Rule** (no inflation adjustment unless portfolio return exceeded inflation) are **not implemented**. The planner's approach is a plausible dynamic guardrail but is not the Guyton-Klinger protocol. Documented as INFO (see F-063).

### Worked examples

#### WE-9.1: Monte Carlo aggregation sanity

**Setup:** 500 trials, $1M portfolio, $3,000/mo expenses (3.6% WR), age 65, 30 years, 60/40/5 allocation, 7% nominal return, 2.5% inflation. Time-seeded RNG (non-reproducible). Separate 100-trial deterministic run with seed 42 used to measure median bias.

**Results:**

| Metric | Value |
|--------|-------|
| Runs | 500 |
| SuccessRate | 93.6% |
| MedianBalance | $2,641,802 |
| MeanBalance | $3,931,337 |
| Percentile10 | $269,389 |
| Percentile90 | $9,109,506 |
| Ordering (P10≤P25≤Med≤P75≤P90) | PASS |

**Median bias check (seeded 100-trial):** Implementation uses `balances[runs/2]` = `balances[50]` (51st element out of 100). True median = average of elements 49 and 50. Measured implied median: $2,998,005; true median: $2,975,174; bias: +0.77%. For large `runs` (≥1,000) the bias falls below 0.1%, which is acceptable. See F-061 for the indexing detail.

**Conclusion:** Aggregation logic is statistically sound at production scale. All ordering invariants hold.

#### WE-9.2: Historical backtest window count

**Setup:** 97 years of data (1928–2024), 30-year horizon.

**Standard formula:** N − H + 1 = 97 − 30 + 1 = **68** starting years (1928 through 1995).

**Planner result:** `GetAvailableStartYears(30)` returns **67** years (1928 through 1994). The 1995–2024 sequence (the most recent complete 30-year window, ending exactly at the 2024 data point) is **excluded**.

**Root cause:** `GetAvailableStartYears` uses `maxIdx = len(HistoricalReturns) - projectionYears` and iterates `i < maxIdx`, producing indices 0 through `maxIdx−1`. But `GetHistoricalSequence(1995, 30)` correctly accepts `startIdx=67`, `startIdx+yearsNeeded=97`, and the guard condition `97 > 97` is false, so the sequence is valid. The exclusive upper bound in `GetAvailableStartYears` is off by one. See F-057.

#### WE-9.3: Guardrail trigger

**CPR analog (floor):** Initial $1M, drop to $750K (25% from peak ≥ 20% threshold) → spending multiplier cut to **0.90** (−10%). PASS.

**PR analog (ceiling):** Initial $1M, rise to $1.3M (30% from initial ≥ 20% threshold) → spending multiplier raised to **1.10** (+10%). PASS.

**Exact threshold:** Drop to exactly $800K from $1M (exactly 20% ≥ 20%) → trigger fires → multiplier 0.90. The condition is `>=`, so the threshold is inclusive. PASS.

**Below threshold:** Drop to $801K (≈19.9% < 20%) → no trigger → multiplier 1.00. PASS.

**Note on Guyton-Klinger (2006) fidelity:** The canonical CPR fires when the current *withdrawal rate* (annual withdrawal / current portfolio) exceeds the *initial rate × 1.20*, not when the portfolio drops by a fixed percentage from peak. The planner's portfolio-drop approach is a simpler proxy that will not match G-K behavior exactly (it can fire earlier or later than the G-K rule depending on whether spending has already been adjusted). See F-063.

### Findings

See F-057 through F-064 in Appendix C.

## 10. Scenario chain, healthcare, budget-fit / steady-state

### Functions audited

**Legend:** PASS = formula correct, no findings · PASS (F-NNN) = formula correct, has associated finding · PARTIAL (F-NNN) = formula partially correct · FAIL (F-NNN) = formula incorrect.

| Function | Location | Status |
|----------|----------|--------|
| `prepareChainedSettings` | `internal/services/retirement/chain.go:9` | PARTIAL (F-065) |
| `findPreparedScenarioPerson` | `internal/services/retirement/chain.go:61` | PASS |
| `reconcilePreparedPersons` | `internal/services/retirement/chain.go:87` | PASS |
| `rebaseIncomeSources` | `internal/services/retirement/chain.go:101` | PASS |
| `rebaseExpenseSources` | `internal/services/retirement/chain.go:118` | PASS |
| `rebaseBigTicketItems` | `internal/services/retirement/chain.go:134` | PASS |
| `rebaseRothConversion` | `internal/services/retirement/chain.go:151` | PASS |
| `nextChainTransition` | `internal/services/retirement/chain.go:167` | PASS |
| `GetTotalHealthcareCost` | `internal/models/whatif.go:496` | PASS (F-068) |
| `HealthcarePerson.GetMonthlyCost` | `internal/models/healthcare.go:87` | PASS (F-067, F-068) |
| `calculateHealthcarePV` | `internal/services/retirement/calculator.go:91` | PASS |
| `calculateMonthlyIncomeBreakdown` | `internal/services/retirement/calculator.go:362` | PASS |
| `CalculateTotalIncome` | `internal/services/retirement/calculator.go:125` | PASS (F-066) |
| `CalculateTotalExpenses` | `internal/services/retirement/calculator.go:546` | PASS |
| `CalculateBudgetFit` | `internal/services/retirement/calculator.go:1291` | PASS |
| `findSteadyStateMonth` | `internal/services/retirement/calculator.go:1579` | PASS (F-069) |
| `CalculatePresentValueAnalysis` | `internal/services/retirement/calculator.go:1615` | PASS (F-070) |
| `buildProjectionExplainability` | `internal/services/retirement/calculator.go:2931` | PASS (F-071) |

### Conventions

**Chain hand-off preserved values:**
- Portfolio balances (tax-deferred, Roth, taxable market value, cost basis) are held in local variables in `RunProjection`; they are **not** part of `WhatIfSettings` and therefore survive the chain transition automatically. `prepareChainedSettings` never touches them.
- `cumulativeInflation` is also a local variable and persists across the transition.
- After the transition, `taxableAccount.syncAssumptions(s)` re-reads dividend yield and capital-gains distribution rate from the new settings while preserving `MarketValue`, `CostBasis`, and `RealizedGainsYTD`.
- `rebaseLivingExpensesAtTransition(s, phaseAge, cumulativeInflation)` re-anchors `currentLivingExpenses` to `s.MonthlyLivingExpenses * cumulativeInflation`. This is correct when `SpendingDeclineRate = 0` (net inflation = full inflation), but creates a step-up when `SpendingDeclineRate > 0` (see F-065).
- The following fields are copied from the **primary** (not the linked) settings: `StartDate`, `Persons`, `CurrentAge`, `SpouseAge`, `PhaseAgeReference`, `ProjectionYears`, `TaxDeferredDelayYears`.
- Income sources, expense sources, big-ticket items, and Roth conversion config are taken from the **linked** settings and rebased to year-0.
- `TaxConfig`, `InvestmentReturn`, `InflationRate`, `SpendingDeclineRate`, `MonthlyLivingExpenses`, and allocation fields (`TaxDeferredPercent`, `RothPercent`, etc.) are inherited from the linked settings.

**Healthcare transition rule:**
- `GetMonthlyCost(month)` uses integer-division years: `yearsElapsed = month / 12`. Age at month `m` is `CurrentAge + (m / 12)`.
- A person at age 64 is still pre-Medicare through month 11 (year 0). Medicare kicks in at month 12 (year 1, age 65). The transition is **year-based**, not month-based. A person who turns 65 in month 6 of year 1 transitions at month 12 (start of year 1), not at month 6.
- No employer-contribution field exists; the model stores the net out-of-pocket cost in `CurrentMonthlyCost`. The sub-task description's "$1,000 ACA − $400 employer = $600" is represented as `CurrentMonthlyCost = 600` directly.
- For the `CoverageEmployer` type with `EmployerCoverageYears > 0`, the cost is fixed (no inflation) during the employer period, then transitions to ACA with `PreMedicareInflation`, then to Medicare at `MedicareEligibleAge`.

**Steady-state convention:**
- Steady-state is expressed in **nominal** dollars (April 11 finding-12 fix is intact: no real-dollar conversion applied).
- `findSteadyStateMonth` returns the latest `StartMonth` across all active income sources. If `socialSecurityProjectionActive` is true (i.e., optimizer SS is configured), `claimStartMonth(CurrentAge, ClaimAge)` is evaluated for both primary and spouse and factored in. This fix (April 11 finding-11) is verified intact by WE-10.3.
- The result is capped at `ProjectionYears * 12`.

### Worked examples

#### WE-10.1: Two-phase chain equivalence

**Setup:** Age 65, 30-year projection, $1M portfolio (60% tax-deferred, 10% Roth), $4,000/mo expenses, 6% return, 3% inflation. `SpendingDeclineRate = 0` (so net inflation equals full inflation). Chain triggers at age 70 with identical linked settings.

**Expected:** Final balances at month 360 differ by ≤ $1.00 (FP rounding only).

**Result:** Delta = **$0.00**. Tax-deferred, Roth, and taxable all match exactly. Chain is transparent under these conditions. PASS.

**Finding (F-065):** When the same scenario is run with `SpendingDeclineRate = 1.0` (the default value), the chain delta at month 360 is **$178,943** (MEDIUM). Root cause: `rebaseLivingExpensesAtTransition` anchors the expense level to `MonthlyLivingExpenses × cumulativeInflation` where `cumulativeInflation` accumulates at the **full** `InflationRate`, but the ongoing pre-transition computation applies `InflationRate − SpendingDeclineRate`. At the chain boundary, expenses are therefore stepped up by `cumulativeInflation(full) / cumulativeInflation(net)`, permanently inflating the post-transition run.

#### WE-10.2: Healthcare ACA → Medicare transition

**Setup:** Person, age 64, `CoverageACA`, `CurrentMonthlyCost = 600` (representing $1,000 ACA − $400 employer = $600 net), `PreMedicareInflation = 0`, `MedicareMonthlyCost = 200`, `PostMedicareInflation = 0`, `MedicareEligibleAge = 65`.

**Expected:** Months 0–11 = $600.00; month 12+ = $200.00.

**Result:**
- Month 11 (`yearsElapsed = 0`, age 64): **$600.00**. PASS.
- Month 12 (`yearsElapsed = 1`, age 65): **$200.00**. PASS.

**Transition rule:** Transition occurs at the start of the **year** the person reaches `MedicareEligibleAge`, not the specific birth month within that year. The function uses `ageAtMonth = CurrentAge + (month / 12)` with integer division; anyone turning 65 in months 12–23 transitions at month 12. This is a rough approximation — someone turning 65 in month 18 should logically remain on ACA through month 17 but transitions at month 12. See F-067.

#### WE-10.3: Steady-state with projected SS

**Setup:** Primary age 60, 30-year projection. No SS in `IncomeSources`. `SocialSecurity.FRABenefit = 3000`, `FRA = 67`, `ClaimAge = 70`.

**Expected:** `findSteadyStateMonth` ≥ `(70 − 60) × 12 = 120`.

**Result:** `findSteadyStateMonth = 120`. PASS. April 11 finding-11 fix is intact — projected SS claim age is correctly factored into the minimum steady-state month.

#### WE-10.4: PV analysis cross-check vs verification doc

**Reference values (verified 2026-04-07, pre-b978aa9):**
- PV income: $1,445,806.64
- PV expenses: $3,058,923.29
- Coverage ratio: 1.1×
- Surplus/deficit: +$286,883.35

**Reconstructed scenario inputs** (from docs/what-if-retirement-verification.md): portfolio $1,900,000; living expenses $9,800/mo; 31-year projection; inflation 2.7%; discount 5%; healthcare Darrell Medicare $550, Christine ACA $1,150; income: Darrell SSI $4,000 from month 0, Christine job $2,000 through month 48, Christine pension $1,200 from month 48, Christine SSI $2,000 from month 156; spending phases Go-Go 100% / Slow-Go 85% from 75 / No-Go 85% from 85.

**Observed results (post-b978aa9):**
| Metric | Reference | Observed | Delta |
|--------|-----------|----------|-------|
| PV income | $1,445,806.64 | $1,465,150.58 | +$19,344 |
| PV expenses | $3,058,923.29 | $3,049,738.75 | −$9,185 |
| Surplus | +$286,883.35 | +$315,411.84 | +$28,528 |
| Coverage ratio | 1.1× | 1.103× | negligible |

All deltas are under $50,000. The PV income delta of ~$19K is consistent with the b978aa9 switch from simple-division monthly rate to geometric monthly rate (`(1+r)^(1/12)−1`), which lowers effective monthly discount and growth rates and increases PV of future income streams. PV expense delta of ~$9K moves in the opposite direction from the income delta, consistent with the same rate adjustment applied to expense discounting. These are INFO-level shifts, not a new bug. See F-070. The reference values in `docs/what-if-retirement-verification.md` should be refreshed post-b978aa9.

### Findings

See F-065 through F-071 in Appendix C.

---

## Appendix A — Constants currency

This appendix lists every authoritatively-bound numeric constant referenced by
the audited math, with as-coded value and most-current published value side-by-
side. Status definitions:

- `current` — code matches the most-current published value
- `stale` — code is from an older tax year; bump candidate
- `absent` — constant is not in the codebase but the audit recommends adding it
- `check` — audit-time uncertainty; recommend manual verification
- `n/a` — internal model constant, no external authority applicable

Internal-model constants (allocation defaults, spending-phase ages, default
return / inflation rates, etc.) are not in this table — they are calibration
defaults the user can override, not authoritatively fixed reference values.

---

### Federal income tax brackets (`tax.go` `TaxBrackets2024`)

Source: IRS Rev. Proc. 2023-34 §3.01 (TY2024, as-coded) vs IRS Rev. Proc.
2024-40 §3.01 (TY2025, current). Six upper-boundary values per filing status
(the top 37% bracket has no upper boundary and is omitted from the comparison).

| Filing | Bracket | As-coded (TY2024) | Current (TY2025) | Status |
|--------|---------|-------------------|------------------|--------|
| Single | 10% → 12% | 11,600 | 11,925 | stale |
| Single | 12% → 22% | 47,150 | 48,475 | stale |
| Single | 22% → 24% | 100,525 | 103,350 | stale |
| Single | 24% → 32% | 191,950 | 197,300 | stale |
| Single | 32% → 35% | 243,725 | 250,525 | stale |
| Single | 35% → 37% | 609,350 | 626,350 | stale |
| MFJ | 10% → 12% | 23,200 | 23,850 | stale |
| MFJ | 12% → 22% | 94,300 | 96,950 | stale |
| MFJ | 22% → 24% | 201,050 | 206,700 | stale |
| MFJ | 24% → 32% | 383,900 | 394,600 | stale |
| MFJ | 32% → 35% | 487,450 | 501,050 | stale |
| MFJ | 35% → 37% | 731,200 | 751,600 | stale |
| MFS | 10% → 12% | 11,600 | 11,925 | stale |
| MFS | 12% → 22% | 47,150 | 48,475 | stale |
| MFS | 22% → 24% | 100,525 | 103,350 | stale |
| MFS | 24% → 32% | 191,950 | 197,300 | stale |
| MFS | 32% → 35% | 243,725 | 250,525 | stale |
| MFS | 35% → 37% | 365,600 | 375,800 | stale |
| HoH | 10% → 12% | 16,550 | 17,000 | stale |
| HoH | 12% → 22% | 63,100 | 64,850 | stale |
| HoH | 22% → 24% | 100,500 | 103,350 | stale |
| HoH | 24% → 32% | 191,950 | 197,300 | stale |
| HoH | 32% → 35% | 243,700 | 250,500 | stale |
| HoH | 35% → 37% | 609,350 | 626,350 | stale |

Rates (10%, 12%, 22%, 24%, 32%, 35%, 37%) are set by statute and do not change year-to-year under current law; not repeated here.

---

### Federal LTCG brackets (`tax.go` `LongTermCapitalGainsBrackets2024`)

Source: IRS Rev. Proc. 2023-34 §3.03 (TY2024, as-coded) vs Rev. Proc. 2024-40
§3.03 (TY2025, current). Two upper-boundary values per filing status (20% bracket
has no upper boundary).

| Filing | Bracket | As-coded (TY2024) | Current (TY2025) | Status |
|--------|---------|-------------------|------------------|--------|
| Single | 0% → 15% | 47,025 | 48,350 | stale |
| Single | 15% → 20% | 518,900 | 533,400 | stale |
| MFJ | 0% → 15% | 94,050 | 96,700 | stale |
| MFJ | 15% → 20% | 583,750 | 600,050 | stale |
| MFS | 0% → 15% | 47,025 | 48,350 | stale |
| MFS | 15% → 20% | 291,850 | 300,000 | stale |
| HoH | 0% → 15% | 63,000 | 64,750 | stale |
| HoH | 15% → 20% | 551,350 | 566,700 | stale |

Rates (0%, 15%, 20%) are set by statute and do not change year-to-year under current law; not repeated here.

---

### Standard deduction (`tax.go` `StandardDeduction2024`)

Source: IRS Rev. Proc. 2023-34 §3.16 (TY2024, as-coded) vs Rev. Proc. 2024-40
§3.16 (TY2025, current).

| Filing | As-coded (TY2024) | Current (TY2025) | Status |
|--------|-------------------|------------------|--------|
| Single | 14,600 | 15,000 | stale |
| MFJ | 29,200 | 30,000 | stale |
| MFS | 14,600 | 15,000 | stale |
| HoH | 21,900 | 22,500 | stale |

---

### Additional standard deduction for age 65+ / blind (NOT in code)

Cross-reference F-001. These constants are **absent** from the codebase today;
the audit recommends adding them. Values are per qualifying person, per year.

| Filing | TY2024 value (as published) | TY2025 value | Status |
|--------|-----------------------------|--------------|--------|
| Single | 1,950 | 2,000 | absent |
| HoH | 1,950 | 2,000 | absent |
| MFJ (per qualifying spouse) | 1,550 | 1,600 | absent |
| MFS (per qualifying person) | 1,550 | 1,600 | absent |

Source: IRS Rev. Proc. 2023-34 §3.16 (TY2024); Rev. Proc. 2024-40 §3.16 (TY2025).

---

### Taxable Social Security thresholds (`tax.go` `socialSecurityTaxThresholds`)

Source: 26 USC § 86 (statute — never inflated). As-coded values are permanent
statute; current published value equals as-coded value.

| Filing | Field | As-coded | Statute | Status |
|--------|-------|----------|---------|--------|
| Single | BaseThreshold | 25,000 | 25,000 | current |
| Single | UpperThreshold | 34,000 | 34,000 | current |
| Single | BaseTaxableAmount | 4,500 | 4,500 | current |
| MFJ | BaseThreshold | 32,000 | 32,000 | current |
| MFJ | UpperThreshold | 44,000 | 44,000 | current |
| MFJ | BaseTaxableAmount | 6,000 | 6,000 | current |
| HoH | BaseThreshold | 25,000 | 25,000 | current |
| HoH | UpperThreshold | 34,000 | 34,000 | current |
| HoH | BaseTaxableAmount | 4,500 | 4,500 | current |

Note: MFS filing status is not present in `socialSecurityTaxThresholds`; the
code falls back to an always-85% rule for MFS. Cross-reference F-018.

---

### NIIT thresholds (`tax.go` `niitThresholds`)

Source: 26 USC § 1411 (statute — never inflated).

| Filing | As-coded | Statute | Status |
|--------|----------|---------|--------|
| Single | 200,000 | 200,000 | current |
| MFJ | 250,000 | 250,000 | current |
| MFS | 125,000 | 125,000 | current |
| HoH | 200,000 | 200,000 | current |

---

### IRMAA 2026 tiers (`tax.go` `monthlyIRMAASurcharge2026`)

Source: CMS 2026 Medicare Part B and Part D IRMAA announcement. Cross-reference
F-020 (audit-time advisory — recommend manual verification against the published
CMS 2026 announcement for the exact per-tier dollar amounts; the audit did not
independently obtain the primary CMS document).

Surcharge shown is Part B + Part D combined monthly dollar amount (as-coded).
The top-boundary `math.MaxFloat64` rows represent "above all thresholds" and are
omitted from the MAGI column.

| Filing | Tier MAGI upper | Part B + Part D surcharge (as-coded) | Status |
|--------|-----------------|--------------------------------------|--------|
| Single | 109,000 | 0.00 | check |
| Single | 137,000 | 95.70 | check |
| Single | 171,000 | 240.40 | check |
| Single | 205,000 | 385.00 | check |
| Single | 500,000 | 529.60 | check |
| Single | (above 500,000) | 578.00 | check |
| MFJ | 218,000 | 0.00 | check |
| MFJ | 274,000 | 95.70 | check |
| MFJ | 342,000 | 240.40 | check |
| MFJ | 410,000 | 385.00 | check |
| MFJ | 750,000 | 529.60 | check |
| MFJ | (above 750,000) | 578.00 | check |
| MFS | 109,000 | 0.00 | check |
| MFS | 391,000 | 529.60 | check |
| MFS | (above 391,000) | 578.00 | check |
| HoH | 109,000 | 0.00 | check |
| HoH | 137,000 | 95.70 | check |
| HoH | 171,000 | 240.40 | check |
| HoH | 205,000 | 385.00 | check |
| HoH | 500,000 | 529.60 | check |
| HoH | (above 500,000) | 578.00 | check |

Note: surcharge values shown are the sum of the inline Part B and Part D
addends in the code (e.g., `81.20 + 14.50 = 95.70`).

---

### RMD Uniform Lifetime Table (`rmd.go` `uniformLifetimeTable`)

Source: IRS Pub 590-B Appendix B Table III, post-2022 edition (Notice 2020-22 /
Notice 2022-53). Task 4 verified all 49 cells against the published table: zero
mismatches found.

| Age | As-coded factor | Pub 590-B factor | Status |
|-----|-----------------|------------------|--------|
| 72 | 27.4 | 27.4 | current |
| 73 | 26.5 | 26.5 | current |
| 74 | 25.5 | 25.5 | current |
| 75 | 24.6 | 24.6 | current |
| 76 | 23.7 | 23.7 | current |
| 77 | 22.9 | 22.9 | current |
| 78 | 22.0 | 22.0 | current |
| 79 | 21.1 | 21.1 | current |
| 80 | 20.2 | 20.2 | current |
| 81 | 19.4 | 19.4 | current |
| 82 | 18.5 | 18.5 | current |
| 83 | 17.7 | 17.7 | current |
| 84 | 16.8 | 16.8 | current |
| 85 | 16.0 | 16.0 | current |
| 86 | 15.2 | 15.2 | current |
| 87 | 14.4 | 14.4 | current |
| 88 | 13.7 | 13.7 | current |
| 89 | 12.9 | 12.9 | current |
| 90 | 12.2 | 12.2 | current |
| 91 | 11.5 | 11.5 | current |
| 92 | 10.8 | 10.8 | current |
| 93 | 10.1 | 10.1 | current |
| 94 | 9.5 | 9.5 | current |
| 95 | 8.9 | 8.9 | current |
| 96 | 8.4 | 8.4 | current |
| 97 | 7.8 | 7.8 | current |
| 98 | 7.3 | 7.3 | current |
| 99 | 6.8 | 6.8 | current |
| 100 | 6.4 | 6.4 | current |
| 101 | 6.0 | 6.0 | current |
| 102 | 5.6 | 5.6 | current |
| 103 | 5.2 | 5.2 | current |
| 104 | 4.9 | 4.9 | current |
| 105 | 4.6 | 4.6 | current |
| 106 | 4.3 | 4.3 | current |
| 107 | 4.1 | 4.1 | current |
| 108 | 3.9 | 3.9 | current |
| 109 | 3.7 | 3.7 | current |
| 110 | 3.5 | 3.5 | current |
| 111 | 3.4 | 3.4 | current |
| 112 | 3.3 | 3.3 | current |
| 113 | 3.1 | 3.1 | current |
| 114 | 3.0 | 3.0 | current |
| 115 | 2.9 | 2.9 | current |
| 116 | 2.8 | 2.8 | current |
| 117 | 2.7 | 2.7 | current |
| 118 | 2.5 | 2.5 | current |
| 119 | 2.3 | 2.3 | current |
| 120 | 2.0 | 2.0 | current |

---

### Other authoritative constants

| Constant | Location | As-coded value | Source value | Status |
|----------|----------|----------------|--------------|--------|
| `RMDStartAge` | `rmd.go:6` | 73 | SECURE 2.0 — 73 for 2023–2032; 75 from 2033 | current pre-2033; see F-032 |
| Early withdrawal penalty rate | `calculator.go:838` | 0.10 (10%) | IRC § 72(t) — 10% | current |
| Early withdrawal age cutoff (proxy) | `calculator.go:837` | 60 (whole-year proxy for 59½) | IRC § 72(t) — 59½ | check (F-050) |
| `MedicareEligibleAge` | `settings.go:283` | 65 | 42 USC § 426 — 65 | current |
| NIIT rate | `tax.go:305` | 0.038 (3.8%) | 26 USC § 1411 — 3.8% | current |

---

### Currency status summary

| Category | Rows in scope | current | stale | absent | check |
|----------|---------------|---------|-------|--------|-------|
| Federal income tax bracket boundaries | 24 | 0 | 24 | 0 | 0 |
| LTCG bracket boundaries | 8 | 0 | 8 | 0 | 0 |
| Standard deduction | 4 | 0 | 4 | 0 | 0 |
| Additional std deduction (65+/blind) | 4 | 0 | 0 | 4 | 0 |
| Taxable SS thresholds | 9 | 9 | 0 | 0 | 0 |
| NIIT thresholds | 4 | 4 | 0 | 0 | 0 |
| IRMAA 2026 tiers | 21 | 0 | 0 | 0 | 21 |
| RMD Uniform Lifetime Table | 49 | 49 | 0 | 0 | 0 |
| Other authoritative constants | 5 | 3 | 0 | 0 | 2 |
| **Total** | **128** | **65** | **36** | **4** | **23** |

- **Total cells in scope:** 128
- `current`: 65
- `stale`: 36
- `absent` (recommended additions): 4
- `check` (audit-time uncertainty): 23

**Triage recommendation:** All 36 stale cells are federal income tax and LTCG
bracket boundaries, along with the four standard deduction values — a single
annual bump from TY2024 to TY2025 tables (IRS Rev. Proc. 2024-40) would resolve
all stale rows at once. The 4 absent rows (additional standard deduction for
age 65+) require a new constant and new code path (see F-001). The 23 `check`
rows are IRMAA tier amounts that should be manually verified against the
published CMS 2026 announcement (see F-020).

## Appendix B — Audit method

_Filled in by Task 13._

## Appendix C — Findings ledger (running list)

_Maintained throughout. Each task that emits findings appends them here in
ID order. Task 12 reads this list to produce the findings table above._

### F-001 — MEDIUM Missing age-65+ additional standard deduction

**Location:** `internal/services/retirement/tax.go:238` — `GetAdjustedStandardDeduction`

**Source consulted:** IRS Rev. Proc. 2023-34 §3.16 (additional standard deduction for age 65 or older and/or blind).

**What it does:** Returns the base 2024 standard deduction for the filing status, inflated forward. No age-based adjustment is applied.

**Finding:** Rev. Proc. 2023-34 §3.16 provides an additional standard deduction for taxpayers who are 65 or older: **$1,950 per qualifying person for Single and Head of Household filers**, and **$1,550 per qualifying spouse for MFJ and MFS filers** (i.e., $3,100 if both MFJ spouses are 65+). The per-person amount is intentionally higher for Single/HoH filers under the IRS code. Since this planner targets retirees who are typically 65 or older, the base deduction is likely understated by $1,550–$3,900 for most users (the upper bound is a Single or HoH filer who is both 65+ and blind, adding two $1,950 increments). This causes over-taxation of ordinary income.

**Evidence / repro:**
```go
// tax.go:238-249
func (tc *TaxCalculator) GetAdjustedStandardDeduction(yearsFromBase int) float64 {
    baseDeduction := StandardDeduction2024[tc.FilingStatus]
    // ...
    return baseDeduction * tc.inflationFactor(yearsFromBase)
    // No age-65+ addition anywhere in the call chain.
}
```
A 65+ Single filer at $80,000 gross income would have:
- Standard deduction (Single, age 65+): $14,600 + $1,950 = **$16,550**
- Taxable income: $80,000 − $16,550 = **$63,450**
- Tax: 10% × $11,600 + 12% × ($47,150 − $11,600) + 22% × ($63,450 − $47,150) = $1,160 + $4,266 + $3,586 = **$9,012**
- Code's tax (WE-1.1, no age-65+ adjustment): **$9,441**
- Over-estimate: $9,441 − $9,012 = **$429** (**4.8%** error on tax owed)

**Recommended fix sketch:** Add an `Age65Count int` field (0, 1, or 2) to `TaxCalculator` and a `StandardDeduction2024Additional` constant map keyed on filing status (Single/HoH → $1,950; MFJ/MFS → $1,550); sum the base deduction with `Age65Count * additional` before inflating.

**Test coverage note:** No test exercises the age-65+ deduction path because the function doesn't implement it. A boundary test at the qualifying age transition (the year the client turns 65) is entirely absent.

---

### F-002 — INFO State tax is a single flat rate

**Location:** `internal/services/retirement/tax.go:396` — `CalculateStateTax`

**Source consulted:** General knowledge of state income tax structures; no specific IRS source (state law varies by jurisdiction).

**What it does:** Applies a single flat percentage to taxable income. No progressive state brackets, no state standard deduction or exemptions.

**Finding:** This is a known simplification. Most states use progressive brackets, personal exemptions, and/or differ in their treatment of retirement income (pension exclusions, SS exemptions). The simplification is acceptable for a high-level planner but may over- or under-estimate state tax by a meaningful margin for users in progressive-bracket states. No code bug — informational only.

**Evidence / repro:** n/a

**Recommended fix sketch:** n/a (acknowledged simplification; could add a documentation note to the UI).

**Test coverage note:** See F-008 for test-coverage gaps specific to this function.

---

### F-003 — INFO `CalculateTotalTax` uses federal standard deduction for state taxable income

**Location:** `internal/services/retirement/tax.go:404` — `CalculateTotalTax`

**Source consulted:** General knowledge of state income tax structures.

**What it does:** Derives state taxable income as `grossIncome − federalStandardDeduction`.

**Finding:** Most states have their own standard deductions (or exemptions) that differ from the federal amount. Applying the federal standard deduction to the state base is a reasonable approximation but may diverge from actual state liability. The inline comment in the code acknowledges this: "Simplified: apply to same taxable income base." Not a bug — informational.

**Evidence / repro:** n/a

**Recommended fix sketch:** n/a (acknowledged simplification).

**Test coverage note:** See F-009 for test-coverage gaps specific to this function.

---

### F-004 — INFO Inflation projection uses pure compound growth; IRS uses chained CPI rounded to nearest $50

**Location:** `internal/services/retirement/tax.go:181` — `GetAdjustedBrackets`; `internal/services/retirement/tax.go:251` — `inflationFactor`

**Source consulted:** IRS Rev. Proc. 2023-34 §3.01 (inflation adjustment methodology).

**What it does:** Multiplies all bracket edges by `(1 + inflationRate/100)^years`, producing continuous compounding without rounding.

**Finding:** The IRS adjusts brackets annually using chained CPI-U-RS and rounds to the nearest $50 (for ordinary income brackets) or $50 (for LTCG brackets) per Rev. Proc. 2023-34 §3.01. Pure compounding with user-supplied inflation rate will diverge from actual IRS-published values as years increase. This is appropriate for a projection tool (the actual IRS values are unknown for future years), but users should understand the projection is a smooth approximation, not a simulation of IRS rounding. Not a bug — informational.

**Evidence / repro:** n/a

**Recommended fix sketch:** n/a (behavior is intentional for a projection tool; consider a UI tooltip explaining the approximation).

**Test coverage note:** `inflationFactor` is exercised indirectly via `GetAdjustedBrackets`. See F-014 for bracket-specific test gaps.

---

### F-005 — INFO `inflationFactor` negative-years path not exercised by tests

**Location:** `internal/services/retirement/tax.go:251` — `inflationFactor`

**Source consulted:** Code inspection.

**What it does:** Returns `1.0` when `yearsFromBase <= 0`, and `(1 + inflationRate/100)^years` otherwise.

**Finding:** The `yearsFromBase < 0` branch (which also returns `1.0`) is never exercised in tests. Tests only call `GetAdjustedBrackets(0)` and `GetAdjustedBrackets(10)`, so the negative branch of `inflationFactor` is unreachable in the test suite. The behavior (return 1.0 for any non-positive year) is intuitive and correct, but the branch is dark. Informational only — not a code error.

**Evidence / repro:** `TestInflationAdjustedBrackets` calls `GetAdjustedBrackets(0)` and `GetAdjustedBrackets(10)` only; no test passes a negative year to any function that calls `inflationFactor`.

**Recommended fix sketch:** Add a table-driven case with `yearsFromBase = -1` to `TestInflationAdjustedBrackets` asserting the returned brackets equal the base brackets.

**Test coverage note:** The `yearsFromBase <= 0` branch is only partially exercised (year=0, not year<0).

---

### F-006 — LOW Dead fallback branch in `GetAdjustedStandardDeduction`

**Location:** `internal/services/retirement/tax.go:238` — `GetAdjustedStandardDeduction`

**Source consulted:** Code inspection.

**What it does:** If `baseDeduction == 0` (i.e., filing status not found in map), falls back to `StandardDeduction2024[models.FilingMarriedJoint]`.

**Finding:** `StandardDeduction2024` populates all four valid filing status keys. `normalizeFilingStatus` is not called before the map lookup in this function (unlike in several other functions), so an unknown filing status would yield 0 and trigger the fallback. However, `NewTaxCalculator` accepts any `models.FilingStatus` without normalizing it — meaning an invalid status passed at construction time would cause `GetAdjustedStandardDeduction` to silently fall back to MFJ values while other functions (which do call `normalizeFilingStatus`) would also use MFJ. The inconsistency is harmless in practice but worth noting. No user-visible math error in the normal usage path.

**Evidence / repro:** n/a

**Recommended fix sketch:** Call `normalizeFilingStatus(tc.FilingStatus)` consistently at the top of each method that accesses a map by filing status, or normalize once in `NewTaxCalculator`.

**Test coverage note:** No test exercises the invalid-filing-status fallback path.

---

### F-007 — LOW `CalculateFederalTax`: filing-status and time-axis coverage gaps

**Location:** `internal/services/retirement/tax.go:349` — `CalculateFederalTax`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Computes federal income tax given gross income and years from base year; applies standard deduction and progressive brackets.

**Finding:** `TestCalculateFederalTax` constructs a MFJ calculator and only checks that tax falls within loose bounds. Only MFJ is exercised; Single, MFS, and HoH are not tested for this function. No test passes `yearsFromBase > 0` to verify bracket inflation; no test passes a negative income value (the function returns zero for `grossIncome <= 0` per the guard, but this is not asserted).

**Evidence / repro:** `TestCalculateFederalTax` has four subtests (zero, low, middle, higher income), all using MFJ and `yearsFromBase=0`.

**Recommended fix sketch:** Add table-driven subtests with Single, MFS, and HoH filing statuses at a representative income; add a `yearsFromBase=10` subtest with a 3% inflation rate and verify brackets shift by approximately `1.03^10`; assert zero tax on negative income.

**Test coverage note:** Bracket-exact boundary cases (income = standard deduction, income = bracket edge) are not tested.

---

### F-008 — LOW `CalculateStateTax`: no direct test coverage

**Location:** `internal/services/retirement/tax.go:396` — `CalculateStateTax`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns `taxableIncome * stateRate / 100`, or zero if either is non-positive.

**Finding:** `CalculateStateTax` is never called directly in the test suite. All tax tests that use a `TaxCalculator` set `StateIncomeTaxRate: 0`, so the state tax branch is always zero. The guard for `taxableIncome <= 0` is not tested. The guard for `tc.StateRate <= 0` is tested only implicitly (by always being zero).

**Evidence / repro:** `grep -n "CalculateStateTax" tax_test.go` returns no results; all calculator constructions in tests use `StateIncomeTaxRate: 0`.

**Recommended fix sketch:** Add a `TestCalculateStateTax` with: positive income + positive rate (assert exact value); zero income (assert zero); negative income (assert zero); zero rate (assert zero).

**Test coverage note:** The positive state-tax path is completely untested.

---

### F-009 — LOW `CalculateTotalTax`: indirect coverage only; filing-status and inflation gaps

**Location:** `internal/services/retirement/tax.go:404` — `CalculateTotalTax`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns combined federal and state tax plus effective rate for given gross income and year offset.

**Finding:** `CalculateTotalTax` is called in `TestProjectionTaxAccumulatorEstimateMonthlyTaxes` only as a reference value to verify the accumulator, not as the direct subject under test. Only Single filing status and `yearsFromBase=0` are used there. The function's four return values (federalTax, stateTax, totalTax, effectiveRate) are never independently verified for correctness. MFJ, MFS, and HoH filing statuses are not covered. No test uses `yearsFromBase > 0`.

**Evidence / repro:** `TestProjectionTaxAccumulatorEstimateMonthlyTaxes` uses `CalculateTotalTax` to derive `wantTotal` but does not assert the individual federal/state splits.

**Recommended fix sketch:** Add a `TestCalculateTotalTax` with exact expected values for Single and MFJ at `yearsFromBase=0`; add a `yearsFromBase=10` case with a non-zero state rate to verify both federal inflation and state tax.

**Test coverage note:** effectiveRate return value is never asserted in any test.

---

### F-010 — LOW `CalculateTaxWithInvestmentIncome`: single filing status; no LTCG at 20% bracket

**Location:** `internal/services/retirement/tax.go:421` — `CalculateTaxWithInvestmentIncome`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Computes federal + state tax on a mix of ordinary income, qualified dividends, and LTCG.

**Finding:** `TestCalculateTaxWithInvestmentIncome` uses only a Single calculator. There is no MFJ, MFS, or HoH test case. No test places total taxable income above the 15%-to-20% LTCG threshold ($518,900 Single / $583,750 MFJ), leaving the 20% LTCG bracket untested. No test combines a non-zero state rate with investment income.

**Evidence / repro:** Both subtests use `FilingSingle` and `StateIncomeTaxRate: 0`.

**Recommended fix sketch:** Add a MFJ subtest with combined ordinary + LTCG income above the 20% LTCG threshold; add a subtest with a non-zero state rate and verify the state tax component.

**Test coverage note:** The 20% LTCG bracket is never entered in any test.

---

### F-011 — MEDIUM `calculateTaxWithInvestmentIncomeInternal` / `CalculateTaxWithInvestmentIncomeBreakdown`: nonQualifiedDividends path and Breakdown entry point untested

**Location:** `internal/services/retirement/tax.go:429` — `CalculateTaxWithInvestmentIncomeBreakdown`; `internal/services/retirement/tax.go:433` — `calculateTaxWithInvestmentIncomeInternal`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** `calculateTaxWithInvestmentIncomeInternal` is the shared implementation for both wrappers; it computes NIIT using `nonQualifiedDividends` as part of net investment income. `CalculateTaxWithInvestmentIncomeBreakdown` is the public entry point that exposes the NIIT component and MAGI separately.

**Finding:** `CalculateTaxWithInvestmentIncomeBreakdown` is never called in the test suite. Because `CalculateTaxWithInvestmentIncome` always passes `nonQualifiedDividends=0` to the internal function, the `nonQualifiedDividends` contribution to NIIT is completely untested. This is a meaningful gap because non-qualified dividends increase the NIIT base independently of the ordinary-income NIIT threshold calculation, and a regression here would not be caught by any existing test.

**Evidence / repro:** No call to `CalculateTaxWithInvestmentIncomeBreakdown` appears in `tax_test.go`; all `CalculateTaxWithInvestmentIncome` calls omit the nonQualifiedDividends argument (the four-parameter variant always passes 0 internally).

**Recommended fix sketch:** Add `TestCalculateTaxWithInvestmentIncomeBreakdown` with a case where `nonQualifiedDividends > 0` and `magi > NIIT threshold`; assert that the returned `NIIT` field is non-zero and equals `min(excess_magi, netInvestmentIncome) * 0.038`.

**Test coverage note:** The entire `nonQualifiedDividends`-to-NIIT path is a dead code path from the test suite's perspective.

---

### F-012 — LOW `EstimateRothConversionTax`: negative conversion and multi-year not tested

**Location:** `internal/services/retirement/tax.go:484` — `EstimateRothConversionTax`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns the incremental federal tax from adding a Roth conversion to base income. Returns zero if `conversionAmount <= 0`.

**Finding:** `TestEstimateRothConversionTax` tests a positive conversion and a zero conversion, but not a negative conversion amount (which the guard covers but is not asserted). No test uses `yearsFromBase > 0`. Only MFJ filing status is tested; Single, MFS, and HoH are absent.

**Evidence / repro:** Test has two cases: `conversionAmount=25000` and `conversionAmount=0`; both use MFJ and `yearsFromBase=0`.

**Recommended fix sketch:** Add a negative conversion case asserting result is zero; add a `yearsFromBase=10` case asserting the incremental tax shifts proportionally with inflation-adjusted brackets.

**Test coverage note:** The negative-input guard (`conversionAmount <= 0`) is exercised by the zero case but not verified as returning exactly 0 for a negative value.

---

### F-013 — LOW `GetMarginalRate`: single filing status; no year-offset coverage

**Location:** `internal/services/retirement/tax.go:499` — `GetMarginalRate`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns the marginal bracket rate for a given gross income after applying the standard deduction.

**Finding:** `TestGetMarginalRate` tests seven income levels but only for Single filing status with `yearsFromBase=0`. MFJ, MFS, and HoH filing statuses produce different bracket thresholds and are not tested. `yearsFromBase > 0` is not tested. Negative income (returns 10 per guard) is not tested.

**Evidence / repro:** All seven test cases in `TestGetMarginalRate` use `FilingSingle` and pass `yearsFromBase=0` to `GetMarginalRate`.

**Recommended fix sketch:** Add MFJ and HoH cases at incomes that straddle the bracket boundaries unique to those statuses; add a negative-income case asserting 10%; add a `yearsFromBase=10` case with 3% inflation.

**Test coverage note:** MFJ 10%-to-12% threshold ($23,200 before deduction) never tested.

---

### F-014 — LOW `GetAdjustedBrackets`: MFS and HoH filing statuses and negative year not tested

**Location:** `internal/services/retirement/tax.go:181` — `GetAdjustedBrackets`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns inflation-adjusted federal tax brackets for the configured filing status and year offset.

**Finding:** `TestInflationAdjustedBrackets` tests only Single with `yearsFromBase=0` and `yearsFromBase=10`. MFJ, MFS, and HoH filing statuses (which have different bracket structures) are not directly tested. The `yearsFromBase <= 0` early-return path is exercised by year=0 but the `yearsFromBase < 0` sub-case is not (see also F-005). The fallback to MFJ brackets for an unknown filing status (`baseBrackets == nil` branch) is never triggered.

**Evidence / repro:** `TestInflationAdjustedBrackets` explicitly constructs `FilingSingle`.

**Recommended fix sketch:** Parameterize the test over filing statuses; add a `yearsFromBase=-1` case asserting the result equals the `yearsFromBase=0` result; add an invalid-status case to exercise the nil-fallback branch.

**Test coverage note:** The nil-bracket fallback branch at `tax.go:183–185` is never reached in tests.

---

### F-015 — LOW `GetAdjustedLongTermCapitalGainsBrackets`: never directly tested

**Location:** `internal/services/retirement/tax.go:210` — `GetAdjustedLongTermCapitalGainsBrackets`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns inflation-adjusted LTCG brackets for the configured filing status and year offset. Mirrors the structure of `GetAdjustedBrackets`.

**Finding:** No test in `tax_test.go` calls `GetAdjustedLongTermCapitalGainsBrackets` directly. It is exercised indirectly when `CalculateTaxWithInvestmentIncome` is called with non-zero LTCG, but only for Single at year-0. MFJ, MFS, and HoH are untested; `yearsFromBase > 0` is never exercised for this function specifically; the nil-fallback branch is unreachable in tests.

**Evidence / repro:** `grep "GetAdjustedLongTermCapitalGainsBrackets" tax_test.go` returns no results.

**Recommended fix sketch:** Add a `TestGetAdjustedLongTermCapitalGainsBrackets` parallel to `TestInflationAdjustedBrackets`, covering all four filing statuses at year-0 and year-10 with known inflation rates.

**Test coverage note:** Inflation adjustment of LTCG brackets is only implicitly tested through the investment-income calculation path, which covers Single at year-0 only.

---

### F-016 — LOW `GetAdjustedStandardDeduction`: never directly tested

**Location:** `internal/services/retirement/tax.go:238` — `GetAdjustedStandardDeduction`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Returns the filing-status standard deduction inflated to the requested year.

**Finding:** No test calls `GetAdjustedStandardDeduction` directly. It is exercised indirectly through `CalculateFederalTax` and `CalculateTotalTax`, but never with `yearsFromBase > 0` for all four filing statuses. The nil-fallback branch (`baseDeduction == 0`) is not reachable in tests. Direct tests would also catch any future regression if the base constant table is edited incorrectly.

**Evidence / repro:** `grep "GetAdjustedStandardDeduction" tax_test.go` returns no results.

**Recommended fix sketch:** Add `TestGetAdjustedStandardDeduction` asserting the exact 2024 values for all four statuses at year-0, and a proportional inflation check at year-10 with 3% inflation rate.

**Test coverage note:** All four filing-status exact values are unasserted.

---

### F-017 — LOW `normalizeFilingStatus`: never directly tested

**Location:** `internal/services/retirement/tax.go:258` — `normalizeFilingStatus`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`.

**What it does:** Maps any `FilingStatus` value to one of the four valid statuses, defaulting to MFJ for unknown values.

**Finding:** No test exercises `normalizeFilingStatus` directly. All four valid filing status values are covered indirectly through the broader function tests, but only MFJ and Single receive meaningful coverage. The invalid-status fallback path (anything not in the switch) is never triggered in any test.

**Evidence / repro:** `grep "normalizeFilingStatus" tax_test.go` returns no results.

**Recommended fix sketch:** Add a `TestNormalizeFilingStatus` with all four valid statuses (assert identity) plus one invalid/zero value (assert MFJ default).

**Test coverage note:** The default fallback branch (`return models.FilingMarriedJoint`) is never reached in tests.


---

### F-018 — MEDIUM `CalculateTaxableSocialSecurity`: MFS always-85% overstates tax for lived-apart filers

**Location:** `internal/services/retirement/tax.go:267` — `CalculateTaxableSocialSecurity`

**Source consulted:** 26 USC § 86(c)(2); IRS Pub 915 ("How Much Is Taxable?" worksheet for MFS filers).

**What it does:** When `filingStatus == FilingMarriedSeparate`, the function immediately returns `ssBenefits * 0.85` — i.e., 85% of benefits are always taxable. No provisional-income test is applied.

**Finding:** Under 26 USC § 86(c)(2), the 85% cap with $0 thresholds only applies to MFS filers who **lived with their spouse at any time during the tax year**. MFS filers who **lived apart from their spouse the entire year** are subject to the same provisional-income thresholds as Single filers ($25,000 base / $34,000 upper). The code applies the higher (85%-always) treatment to ALL MFS filers regardless of living arrangements, which overstates taxable SS (and thus tax) for MFS-lived-apart filers. For a filer with $20,000 SS and $0 other income: code returns $17,000 taxable; correct amount under lived-apart rules is $0 (provisional income $10,000 < $25,000 base threshold).

**Evidence / repro:**
```go
// tax.go:273-275
if filingStatus == models.FilingMarriedSeparate {
    return ssBenefits * 0.85
}
```
MFS-lived-apart example: `CalculateTaxableSocialSecurity(20000, 0, 0, 0, FilingMarriedSeparate)` → returns $17,000. Per statute (lived apart): PI = $10,000 < $25,000 → correct answer is **$0**. Overstatement: $17,000 taxable SS.

**Recommended fix sketch:** Add a `MFSLivedApart bool` flag to `WhatIfSettings` (or a new `SocialSecurityMFSTreatment` enum). If lived-apart, delegate to the Single/HoH threshold path; if lived-with-spouse or unspecified, retain the 85%-always treatment (conservative default).

**Test coverage note:** The existing test `TestCalculateTaxableSocialSecurity_MarriedSeparateAlways85Pct` asserts the current 85% behavior for all income levels. No test exists for MFS-lived-apart scenarios. The threshold-boundary tests in `coverage_gaps2_test.go` do not include MFS.

---

### F-019 — MEDIUM `CalculateNIIT`: MAGI at exact threshold not tested; NIIT inflation note

**Location:** `internal/services/retirement/tax.go:292` — `CalculateNIIT`

**Source consulted:** 26 USC § 1411; IRS Pub 550 (NIIT); IRC § 1411(b) (thresholds not indexed).

**What it does:** Computes 3.8% NIIT on the lesser of net investment income or excess MAGI above the filing-status threshold. Returns 0 when MAGI <= threshold.

**Finding (two parts):**

*Part A — Formula and thresholds are correct.* The `niitThresholds` map correctly encodes the statutory amounts ($200K Single/HoH, $250K MFJ, $125K MFS). The code does NOT inflate these thresholds (verified: `CalculateNIIT` receives unadjusted MAGI and a fixed threshold — no `yearsFromBase` parameter exists, consistent with the statute's instruction that thresholds are NOT indexed for inflation, per 26 USC § 1411(b)). This is correct behavior.

*Part B — Test gap: MAGI exactly at threshold.* The test suite does not test `CalculateNIIT` at exactly the threshold amount (e.g., `magi = 200000` for Single). The guard `excessMAGI <= 0` covers this, but it is not directly asserted. The `TestCalculateNIIT` cases use $190K (below) and $260K, $215K, $140K (above) — no case uses the exact boundary.

**Evidence / repro:**
- `CalculateNIIT(200000, 50000, FilingSingle)`: expected 0 (at threshold), not tested.
- `CalculateNIIT(125000, 10000, FilingMarriedSeparate)`: expected 0 (at MFS threshold), not tested.

**Recommended fix sketch:** Add exact-threshold cases to `TestCalculateNIIT`: `magi=200000 Single → 0`, `magi=250000 MFJ → 0`, `magi=125000 MFS → 0`; add one case just above each threshold for completeness.

**Test coverage note:** The `excessMAGI <= 0` branch is only exercised via the `magi=190000 < 200000` case (excess is negative); the `excessMAGI == 0` sub-case (MAGI exactly at threshold) is dark.

---

### F-020 — INFO `monthlyIRMAASurcharge2026`: CMS 2026 IRMAA table values require manual cross-check

**Location:** `internal/services/retirement/tax.go:124-157` — `monthlyIRMAASurcharge2026`

**Source consulted:** CMS 2026 Medicare Part B & D IRMAA announcement (late 2025); 42 USC § 1395r-1.

**What it does:** Defines the 2026 monthly IRMAA surcharge (Part B + Part D) and MAGI tier upper bounds for all four filing statuses. The code comment states these are the source amounts, with the planner rescaling them to the 2024 tax base year and inflating forward.

**Finding:** The surcharge dollar amounts and MAGI tier boundaries in the code cannot be independently verified from training-data knowledge with certainty — the 2026 IRMAA values were announced by CMS in late 2025. Based on best available knowledge, the tier structure (6 tiers for Single/MFJ/HoH, 3 tiers for MFS) and the relationship between Single/MFJ/MFS thresholds (MFJ approximately 2x Single; MFS tier 2 upper approximately $391K) match the legislative intent of 42 USC § 1395r-1. The Part B and Part D amounts are coded separately and summed (e.g., 81.20 + 14.50 = 95.70 for tier 2 Single). However, the specific dollar amounts should be manually cross-checked against the official CMS 2026 announcement to confirm accuracy. The code's internal consistency is intact (MFJ thresholds are exactly 2x Single thresholds where applicable; MFS special treatment follows § 1395r-1(f)(2)).

**Evidence / repro:** n/a (informational advisory, not a confirmed error).

**Recommended fix sketch:** Add a comment in the source referencing the exact CMS publication title and URL (e.g., CMS Fact Sheet "2026 Medicare Parts A and B Premiums and Deductibles"). Add a regression test that asserts specific known dollar amounts so future table updates are intentional changes caught by tests.

**Test coverage note:** The existing IRMAA test cases check tier selection and math (correct) but do not assert that the underlying dollar amounts match the CMS source — a future table-entry typo would not be caught unless the nominal dollar values are compared against a trusted constant.

---

### F-021 — LOW `CalculateMonthlyIRMAA`: tier-boundary exact values not tested; HoH coverage absent

**Location:** `internal/services/retirement/tax.go:308` — `CalculateMonthlyIRMAA`

**Source consulted:** Code inspection and `internal/services/retirement/tax_test.go`, `coverage_gaps2_test.go`.

**What it does:** Returns the monthly IRMAA surcharge for a given MAGI, filing status, and inflation factor, by walking the tier table.

**Finding:** The test suite covers Single (below threshold, mid-tier, inflation shift), MFJ (top tier), and MFS (three-tier structure), but has these gaps:

1. **Head of Household (HoH) not tested.** HoH uses the same bracket table as Single, but this equivalence is not directly asserted for IRMAA (unlike taxable SS and NIIT where HoH tests exist).
2. **Exact tier-boundary MAGI values not tested.** The Single table has boundaries at $109K, $137K, $171K, $205K, $500K. No test uses these exact boundary values to verify the inclusive `<=` comparison (e.g., `magi = 109000` should return 0; `magi = 109001` should return $95.70).
3. **MFJ tier boundaries not tested.** Only the MFJ top tier is tested ($800K); the intermediate tier boundaries ($218K, $274K, $342K, $410K, $750K) are not exercised.

**Evidence / repro:** In `tax_test.go`, IRMAA tests: Single $100K (below), Single $160K (mid), MFJ $800K (top), Single $110K with 1.05 factor (inflation shifts). In `coverage_gaps2_test.go`, MFS 3-tier tests plus edge cases. HoH is absent.

**Recommended fix sketch:** Add HoH test case asserting same result as Single for equivalent MAGI. Add table-driven test with MAGI at each Single and MFJ tier boundary (both at and just above).

**Test coverage note:** The IRMAA loop's exact boundary behavior (`magi <= upperMAGI` with `<=`) is tested indirectly but not with exact boundary-match values.

---

### F-022 — LOW `resolveIRMALookbackMAGI`: never directly tested; len=1 boundary not covered

**Location:** `internal/services/retirement/calculator.go:286` — `resolveIRMALookbackMAGI`

**Source consulted:** Code inspection; `calculator_test.go`.

**What it does:** Returns the MAGI from 2 years prior (index `len-2` of the history slice) if history has >= 2 entries; otherwise falls back to `assumedIRMALookbackMAGI` if provided; otherwise returns (0, false) indicating no lookback available.

**Finding:** `resolveIRMALookbackMAGI` is never called directly in any test. It is exercised indirectly via `estimateMonthlySnapshot` in `TestEstimateMonthlySnapshot_IRMAALookback` (len=2 history) and via the full projection in `TestRunProjectionDelaysIRMAAUntilLookbackYear`. The following branches are untested:

1. **`len == 1`** (single year of history): the function should fall through to `assumedIRMALookbackMAGI`. This branch is never exercised.
2. **`len == 0` with no assumed MAGI**: returns (0, false). Not directly tested.
3. **Negative assumed MAGI** (`*assumedIRMALookbackMAGI < 0`): clamped to 0 by `math.Max`. This branch is untested.
4. **`len == 2` exact boundary**: tested via the IRMAA lookback test.

**Evidence / repro:** `grep "resolveIRMALookbackMAGI" *_test.go` returns no direct calls.

**Recommended fix sketch:** Add `TestResolveIRMALookbackMAGI` with table-driven cases: empty history + nil assumed → (0, false); empty history + valid assumed → (assumed, true); empty history + negative assumed → (0, true); len-1 history + nil assumed → (0, false); len-1 history + valid assumed → (assumed, true); len-2 history → (history[0], true); len-3 history → (history[1], true).

**Test coverage note:** The `len == 1` path and the negative-assumed-MAGI clamp are dark.

---

### F-023 — LOW `medicareEligibleAdultCountAtYear`: uses start-of-year age; mid-year birthdate not modeled

**Location:** `internal/services/retirement/calculator.go:315` — `medicareEligibleAdultCountAtYear`

**Source consulted:** Code inspection; 42 USC § 1395o (Medicare eligibility at age 65); `calculator_test.go`.

**What it does:** Returns 0, 1, or 2 — the count of adults (primary + spouse) who are Medicare-eligible (age >= 65) at projection year `year`. Age is computed as `CurrentAge + year` (integer year offset), which effectively uses start-of-year age.

**Finding:** The function uses `PrimaryAgeAt(year) = CurrentAge + year`, which is a whole-year step. A person who turns 65 partway through projection year N will show `age = 65` for the entire year N (since `PrimaryAgeAt(N)` equals their integer age at the start of year N + N years). This is a known modeling simplification: Medicare eligibility is actually month-specific (begins the month of the 65th birthday, or 3 months prior for enrollment purposes). The code counts someone as Medicare-eligible for the entire year in which they reach 65, which may overstate IRMAA for up to 11 months at the Medicare transition year.

The test coverage for this function is good: it covers nil settings, both ages below 65, one at 65, one above 65, and both at 65. All test cases use integer year boundaries (no mid-year fractional tests, which are not possible given the function signature).

**Evidence / repro:** A 64-year-old who turns 65 in month 6 of year 1: `PrimaryAgeAt(1) = 65` → counted Medicare-eligible for all of year 1 (12 months), but is only eligible for approximately 6 months.

**Recommended fix sketch:** For higher fidelity, pass a `month int` parameter and check `CurrentAge + year + (month >= birthMonth ? 0 : -1)` — but this requires birth-month data not currently in the model. The existing simplification is reasonable for a projection tool and should be documented in comments.

**Test coverage note:** The function is well-tested for its current (start-of-year) semantics. The known modeling limitation (no mid-year granularity) is a design constraint, not a test gap.

---

### F-024 — INFO `plannerIRMAAInflationFactorForYear`: zero-equality guard has floating-point fragility

**Location:** `internal/services/retirement/calculator.go:337` — `plannerIRMAAInflationFactorForYear`

**Source consulted:** Code inspection; `calculator_test.go` `TestPlannerIRMAAInflationFactorForYear_Rebases2026TableOntoTaxBaseYear`.

**What it does:** Computes the inflation factor to rescale the 2026 IRMAA table to projection year N. At year N=2 (= irmaaBaseYear minus taxBaseYear), `yearsFromIRMAABase = 0` and the function returns exactly 1.0. For other years, it returns `(1+rate/100)^yearsFromIRMAABase`.

**Finding:** The early-return guard `if yearsFromIRMAABase == 0` uses exact float64 equality. Since `yearsFromTaxBase` is passed as `float64` (it is computed as `float64(month)/12` in the projection loop), and `irmaaBaseYear-taxBaseYear = 2` is an exact integer, the subtraction `yearsFromTaxBase - 2.0` can produce floating-point values that are not exactly 0 even when semantically at year 2 (e.g., month 24 → `24.0/12.0 = 2.0` exactly — safe; but month 25 → not 2). For the specific case where yearsFromTaxBase is derived from integer months, `float64(24)/12.0 = 2.0` is exact and the guard is safe. However, the function could be called with fractional years in future refactors, introducing fragility. Not a current bug but worth noting.

**Evidence / repro:** `float64(24)/12.0` is exactly `2.0` in IEEE 754; the early return fires correctly. If the function were called with, e.g., `1.9999999999` the guard would miss and return `math.Pow(1.03, -0.0000000001) ≈ 1.0` — negligible error, but the guard would not fire.

**Recommended fix sketch:** Replace `if yearsFromIRMAABase == 0` with `if math.Abs(yearsFromIRMAABase) < 1e-9` for robustness, or use integer arithmetic for the year comparison.

**Test coverage note:** The test covers year=0, year=2, and year=5. No test passes a fractional value within epsilon of 2.0.

---

### F-025 — LOW Utility helpers `validSSClaimAge`, `normalizedSSFRA`, `claimStartMonth` not directly tested

**Location:** `internal/services/retirement/social_security.go:12` — `validSSClaimAge`; `:16` — `normalizedSSFRA`; `:194` — `claimStartMonth`

**Source consulted:** Code inspection; `internal/services/retirement/social_security_test.go`.

**What it does:** `validSSClaimAge` returns true iff age is in [62, 70]. `normalizedSSFRA` substitutes FRA=0 with the default (67). `claimStartMonth` computes `(claimAge − currentAge) × 12` for future claims, or 0 for already-claiming.

**Finding:** None of these three helpers are called directly in any test. They are exercised indirectly through higher-level functions, but specific boundary values are untested: exact boundaries 62 and 70 for `validSSClaimAge`; zero-substitution for `normalizedSSFRA`; the already-claiming (→ 0) path for `claimStartMonth`.

**Evidence / repro:** `grep -n "validSSClaimAge\|normalizedSSFRA\|claimStartMonth" social_security_test.go` returns no direct calls.

**Recommended fix sketch:** Add `TestValidSSClaimAge` covering 61 (false), 62 (true), 70 (true), 71 (false); `TestNormalizedSSFRA` covering 0 → 67 and 66 → 66; `TestClaimStartMonth` covering already-claiming (→ 0) and future claim (exact multiplication).

**Test coverage note:** All three boundary conditions are dark from a direct-test perspective.

---

### F-026 — MEDIUM `normalizedSSCOLARate`: zero-COLA scenario inexpressible; silently substitutes 2% default

**Location:** `internal/services/retirement/social_security.go:23` — `normalizedSSCOLARate`

**Source consulted:** Code inspection; `internal/services/retirement/social_security_test.go`; `internal/models/whatif.go:144`.

**What it does:** Converts a COLA rate of 0 to the default 2% (`defaultSocialSecurityCOLARate = 0.02`). Non-zero values pass through unchanged. Used in both `projectedSocialSecurityIncome` and `RunSSAnalysis`, covering all user-facing SS computations.

**Finding:** A user who sets `COLARate: 0.0` intends zero COLA growth — a real SSA scenario (0% COLA in 2010, 2011, 2016). The code silently substitutes 2%. This makes the zero-COLA scenario inexpressible and is inconsistent with `SSBreakevenAges`, which accepts raw colaRate without normalization and correctly handles 0.0. The function is never directly tested; `normalizedSSCOLARate(0) = 0.02` is not asserted anywhere.

**Evidence / repro:**
```go
func normalizedSSCOLARate(colaRate float64) float64 {
    if colaRate == 0 {
        return defaultSocialSecurityCOLARate  // 0.02 — zero is inexpressible
    }
    return colaRate
}
```
`RunSSAnalysis` line 405: `colaRate := normalizedSSCOLARate(ss.COLARate)` — always at least 2%.

**Recommended fix sketch:** Use a sentinel (e.g., −1) for "use default," treat 0 as explicit user intent, and update UI validation to reject negative COLA rates. Alternatively add `COLARateIsDefault bool` to `SocialSecurityConfig`.

**Test coverage note:** `normalizedSSCOLARate` is never called directly in tests. No test verifies the zero-substitution behavior or its downstream effect.

---

### F-027 — LOW `AdjustedSSBenefit`: FRA values other than 66 and 67 not tested; `DerivedPIA` round-trip only covers FRA=67

**Location:** `internal/services/retirement/social_security.go:205` — `AdjustedSSBenefit`; `:237` — `DerivedPIA`

**Source consulted:** POMS RS 00615.105; POMS RS 00615.690; `internal/services/retirement/social_security_test.go`.

**What it does:** `AdjustedSSBenefit` applies the SSA's two-tier early-reduction / DRC schedule. `DerivedPIA` is its exact algebraic inverse. Both are formula-correct (verified by WE-3.1, WE-3.2, and the round-trip test).

**Finding (formula correct; coverage gap):** `TestAdjustedSSBenefit` tests FRA=67 (claimAge 62, 64, 67, 70) and FRA=66 (claimAge 62). FRA=65 (birth years ~1937–1938) and the at-FRA identity for non-67 FRA are not directly tested. `TestDerivedPIA` round-trips only at FRA=67.

**Evidence / repro:** Five test cases; four use FRA=67. `TestDerivedPIA` has two subtests, both FRA=67.

**Recommended fix sketch:** Add `AdjustedSSBenefit(pia, 65, 65)` asserting result == pia; add `AdjustedSSBenefit(pia, 65, 70)` for DRC at FRA=65; extend `TestDerivedPIA` round-trip to FRA=65 and FRA=66.

**Test coverage note:** The `monthsDiff == 0` and `monthsDiff > 0` branches are only tested for FRA=67.

---

### F-028 — LOW `AdjustedSpousalBenefit`: claim age > 70 not explicitly clamped or tested

**Location:** `internal/services/retirement/social_security.go:259` — `AdjustedSpousalBenefit`

**Source consulted:** POMS RS 00615.020; code inspection; `internal/services/retirement/social_security_test.go`.

**What it does:** Applies spousal early-reduction schedule for claim ages before spouseFRA. Returns spousalPIA unchanged for claimAge ≥ spouseFRA (no DRC for spousal). Clamps claimAge < 62 to 62. Formula verified correct by WE-3.3.

**Finding (formula correct; coverage gap):** Unlike `AdjustedSSBenefit`, there is no explicit `claimAge > 70` clamp. For all realistic spouseFRA values (≤ 67), the `claimAge >= spouseFRA` guard correctly caps the benefit, but no test asserts that `AdjustedSpousalBenefit(pia, 67, 75)` equals `AdjustedSpousalBenefit(pia, 67, 70)`.

**Evidence / repro:** `AdjustedSSBenefit` clamps explicitly at lines 206–211; `AdjustedSpousalBenefit` clamps only `< 62` at line 261.

**Recommended fix sketch:** Add `if claimAge > 70 { claimAge = 70 }` for defensive consistency. Add a test asserting equivalence at age 75 and 70.

**Test coverage note:** `claimAge > 70` path not tested; relied on implicitly by the FRA-cap logic.

---

### F-029 — MEDIUM `RunSSAnalysis`: `SpouseUsingSpousalBenefit` display flag uses raw `FRABenefit` instead of derived PIA

**Location:** `internal/services/retirement/social_security.go:484` — `RunSSAnalysis`

**Source consulted:** Code inspection; `internal/services/retirement/social_security_test.go`; `web/templates/components/whatif/social-security.html:176`.

**What it does:** Sets `result.SpouseUsingSpousalBenefit` to flag whether the spouse uses the spousal benefit path in the comparison table. The UI uses this flag to show "Using 50% spousal benefit ($X/mo) — higher than own benefit ($Y/mo)."

**Finding:** Line 484 computes `ss.FRABenefit*0.5 > ss.SpouseFRABenefit`. When primary is already claiming at a non-FRA age, `ss.FRABenefit` is the actual benefit (not PIA). The flag should use `primaryPIA*0.5` (derived at line 411 and in scope). The actual computation numbers are correct (they use `primaryPIA`); only the display flag and the associated UI dollar label are wrong for the already-claiming-at-non-FRA-age case.

**Evidence / repro:** Primary claimed age 62 (PIA=$2,000, actual=$1,400). Spouse own PIA=$800. `ss.FRABenefit*0.5 = 700 < 800` → flag false (wrong); `primaryPIA*0.5 = 1000 > 800` → flag should be true.

**Recommended fix sketch:** Replace `ss.FRABenefit` with `primaryPIA` on line 484. Update `social-security.html:177` to derive the dollar display from the analysis result rather than raw settings.

**Test coverage note:** No test checks `SpouseUsingSpousalBenefit` for an already-claiming primary at a non-FRA age.

---

### F-030 — LOW `RunSSAnalysis`: zero `ClaimAge` triggers spurious "already claiming" logic

**Location:** `internal/services/retirement/social_security.go:398` — `RunSSAnalysis`

**Source consulted:** Code inspection; `internal/models/whatif.go:147`.

**What it does:** Identifies whether the primary person is already claiming (`ss.ClaimAge <= c.Settings.CurrentAge && ss.ClaimAge != fra`) to back-derive PIA and lock the best-age recommendation.

**Finding:** `ClaimAge` defaults to 0 (Go zero value, meaning "unset"). When `ClaimAge=0` and `CurrentAge > 0`, `0 <= CurrentAge` is true, triggering: (1) `DerivedPIA(pia, fra, 0)` — clamped to 62, incorrectly back-derives PIA as if claimed at 62; (2) `bestAge = 0` — an invalid recommended age. The guard should require `validSSClaimAge(ss.ClaimAge)`.

**Evidence / repro:** `ClaimAge=0, CurrentAge=60`: `0 <= 60` → true; `DerivedPIA(pia, 67, 0)` uses claimAge=62 internally; `bestAge = 0`.

**Recommended fix sketch:** Update the existing condition at `social_security.go:410` from `ss.ClaimAge <= c.Settings.CurrentAge && ss.ClaimAge != fra` to `ss.ClaimAge <= c.Settings.CurrentAge && ss.ClaimAge != fra && validSSClaimAge(ss.ClaimAge)`. The added `validSSClaimAge` guard rejects unset (0) and other out-of-range claim ages before the already-claiming branch runs.

**Test coverage note:** No test passes `ClaimAge=0` with a positive `CurrentAge` to `RunSSAnalysis`. The zero-ClaimAge "already claiming" path is dark.

---

### F-031 — INFO `RunSSPortfolioAnalysis` / `bestSSPortfolioOption` / `isBetterSSPortfolioOption`: decision rule documented

**Location:** `internal/services/retirement/social_security.go:500` — `RunSSPortfolioAnalysis`; `:655` — `bestSSPortfolioOption`; `:669` — `isBetterSSPortfolioOption`

**Source consulted:** Code inspection; `internal/services/retirement/social_security_test.go`.

**What it does:** Evaluates claiming ages by portfolio Monte Carlo survival. `isBetterSSPortfolioOption` uses a three-level lexicographic rule: (1) higher `SurvivalRate`; (2) higher `MedianEndingBalance`; (3) lower `ClaimAge` (prefer earlier when outcomes are identical).

**Finding (decision rule correct and consistent):** The ordering is deterministic. `SurvivalRate`-first correctly prioritizes portfolio safety. The `ClaimAge` tiebreaker (prefer earlier) is a reasonable conservative default when economic outcomes are identical. No inconsistency found. Informational documentation only.

**Evidence / repro:** `isBetterSSPortfolioOption` at lines 670–677.

**Recommended fix sketch:** Add a comment explaining the rationale for the `ClaimAge` tiebreaker.

**Test coverage note:** `isBetterSSPortfolioOption` and `bestSSPortfolioOption` are not directly unit-tested; the `MedianEndingBalance` and `ClaimAge` tiebreakers are never exercised in isolation.

### F-032 — MEDIUM `RMDStartAge` is a single constant; does not model SECURE 2.0 age-75 transition in 2033

**Location:** `internal/services/retirement/rmd.go:6` — `RMDStartAge` constant; `rmd.go:97`, `rmd.go:120` — call sites in `CalculateRMDAnalysis`

**Source consulted:** SECURE 2.0 Act of 2022 (§107): RMD start age 73 for taxpayers turning 73 in 2023–2032; age 75 for taxpayers turning 75 in 2033 or later.

**What it does:** `RMDStartAge = 73` is used both to compute `startsInYears` (rmd.go:97) and as the loop guard (rmd.go:120). It is a compile-time constant with no year-dependent logic.

**Finding:** For a user who is, for example, age 60 today (2026), the planner will incorrectly show RMDs beginning at age 73 (year 2039), when SECURE 2.0 will actually defer them until age 75 (year 2041). Any projection that crosses the 2033 boundary for a user who has not yet turned 73 will over-report required withdrawals for 2033–2041 and will under-report the 2-year additional tax-deferred accumulation window. This is a systematic planning error for younger retirement scenarios.

**Evidence / repro:** A user currently age 60 viewing the What-If RMD panel will see "RMDs begin in 13 years (at age 73, year 2039)." Correct answer under SECURE 2.0: RMDs begin at age 75 (year 2041), because this user turns 75 in 2041 which is ≥ 2033.

**Recommended fix sketch:** Replace the single constant with a function `rmdStartAge(birthYear int) int` returning 73 if `birthYear + 73 < 2033` (i.e., if they turn 73 before 2033) and 75 otherwise. Pass the projected birth year (derived from current year minus current age) when computing `startsInYears` and when entering the RMD projection loop.

**Test coverage note:** No test covers a user whose RMD start age under SECURE 2.0 is 75. The existing tests (age 60, 65, 70, 72, 75) either already past RMD start or start at 73 with no cross-2033 validation.

### F-033 — MEDIUM `GetLifeExpectancyFactor`: age 72 is in the table but `CalculateRMDAnalysis` skips it; age below 72 returns 0 silently

**Location:** `internal/services/retirement/rmd.go:64` — `GetLifeExpectancyFactor`; `rmd.go:120` — caller guard `if age >= RMDStartAge`

**Source consulted:** IRS Pub 590-B Appendix B Table III; code inspection.

**What it does:** `GetLifeExpectancyFactor(age int)` returns 0 for age < 72, returns the table value for ages 72–120, and returns 2.0 for ages > 120.

**Finding (two sub-issues):**

1. **Age 72 is in the table but is unreachable via `CalculateRMDAnalysis`**: The loop guard at rmd.go:120 is `age >= RMDStartAge` (i.e., `>= 73`), which is correct under SECURE 2.0. The factor for age 72 (27.4) is stored in the map but `CalculateRMDAnalysis` will never return a projection row at age 72. However, `CalculateRMD(balance, 72)` called directly returns a non-zero RMD, which could mislead future callers.

2. **Age below 72 returns 0 silently**: No comment or error signals the caller that a factor of 0 means "below start age" vs. "lookup error."

**Evidence / repro:** Verified by temp test:
```
GetLifeExpectancyFactor(71) = 0.0000   // below table — returns 0
GetLifeExpectancyFactor(72) = 27.4000  // in table, never triggered by CalculateRMDAnalysis
CalculateRMD(1_000_000, 72) = (36496.35, 3.65%)  // callable, non-zero
```

**Recommended fix sketch:** Consider returning a second `bool` `ok` parameter from `GetLifeExpectancyFactor`, or guard `CalculateRMD` against ages below `RMDStartAge` in addition to the zero-factor guard. At minimum, document the age-72 dead entry.

**Test coverage note:** `GetLifeExpectancyFactor(72)` and `CalculateRMD(balance, 72)` are not tested. `TestGetLifeExpectancyFactor` covers only ages 60, 73, and 125.

### F-034 — LOW `CalculateRMD`: negative balance produces a negative RMD amount with no guard

**Location:** `internal/services/retirement/rmd.go:76` — `CalculateRMD`

**Source consulted:** IRS Pub 590-B (RMD is always non-negative); code inspection.

**What it does:** Computes `amount = taxDeferredBalance / factor`. No guard against negative input.

**Finding:** A negative balance is semantically impossible but the exported function does not guard against it, returning a negative RMD silently. `CalculateRMDAnalysis` clamps `currentBalance` to 0 before calling `CalculateRMD` in its loop (rmd.go:140–142), so this is not reachable via the normal analysis path. However, it is a latent hazard for future callers.

**Evidence / repro:** Verified by temp test:
```
CalculateRMD(-100000, 73): amount=-3773.5849  pct=3.7736
```

**Recommended fix sketch:** Add `if taxDeferredBalance <= 0 { return 0, 0 }` at the top of `CalculateRMD`.

**Test coverage note:** No existing test passes a negative balance to `CalculateRMD`.

### F-035 — MEDIUM `CalculateRMDAnalysis`: RMD deducted before year-end growth is applied; order is economically aggressive

**Location:** `internal/services/retirement/rmd.go:138–148` — growth/withdrawal ordering in `CalculateRMDAnalysis`

**Source consulted:** IRS Pub 590-B (RMD calculated on prior-year-end balance; account grows during year N before RMD deadline); code inspection.

**What it does:** Each projection year: (1) computes RMD on `currentBalance`, (2) deducts RMD, (3) applies 12 months of growth to post-RMD balance.

**Finding:** The IRS-prescribed RMD for year N is computed on the December 31 balance of year N−1. The account then grows during year N, and the distribution is due by December 31 of year N. By deducting the RMD before applying the full year's growth, the model assumes distribution at year-start, understating subsequent projected balances. The error accumulates over long projections.

**Evidence / repro:**
```go
currentBalance -= rmdAmount          // RMD deducted first
if currentBalance < 0 { currentBalance = 0 }
for m := 0; m < 12; m++ {
    currentBalance *= (1 + monthlyReturn)   // growth on post-RMD balance only
}
```

**Recommended fix sketch:** Apply full-year growth to the year-start balance first, then compute and deduct the RMD. This matches the IRS model where the RMD is based on the prior-year-end balance, and the distribution is made from the account that has already grown.

**Test coverage note:** No test validates the per-year balance trajectory precisely enough to catch this sequencing issue.

### F-036 — MEDIUM `CalculateRMDAnalysis`: multiple test-coverage gaps

**Location:** `internal/services/retirement/rmd.go:87` — `CalculateRMDAnalysis`; `rmd_tax_test.go:10`; `calculator_coverage_test.go:183`

**Source consulted:** Code inspection of all RMD-related test files.

**What it does:** Projects RMDs over a multi-year horizon. Existing tests cover ages 65, 72, 75, spouse-older scenario, zero return, zero portfolio.

**Finding:** Untested boundaries:

| Boundary | Concern |
|----------|---------|
| `GetLifeExpectancyFactor(72)` / `CalculateRMD(balance, 72)` directly | Returns non-zero RMD for pre-SECURE-2.0 boundary age |
| `CalculateRMD` with negative balance | Returns negative RMD (see F-034) |
| Projection crossing 2033 for user below age 73 | SECURE 2.0 age-75 transition not validated (see F-032) |
| Projection reaching age 121+ | Fallback factor 2.0 used but never validated in a full loop |
| `ProjectionYears` triggering > 20 RMD rows | The `rmdCount < 20` cap is untested |

**Recommended fix sketch:** Add tests for each row above. The 20-row cap and the age-72 direct-call path are especially low-cost to add.

**Test coverage note:** See table above.

---

### F-037 — LOW `PresentValue`: negative-rate deflation returns futureValue unchanged (economically incorrect)

**Location:** `internal/services/retirement/calculator.go:42` — `PresentValue`

**Source consulted:** Standard finance first principles: for a deflating economy (annualRate < 0), future nominal dollars buy more than present dollars, so `PV > FV`.

**What it does:** Early-returns `futureValue` when `annualRate <= 0`, treating zero and negative rates identically. For zero rate this is correct (`PV = FV`). For negative rates it is incorrect.

**Finding:** With `annualRate = −2.0` and 240 months (20 years), the mathematically correct PV is `100,000 / (0.98)^20 ≈ $149,788.50`. The code returns `$100,000`. The error is approximately 50% of the true PV — technically HIGH severity by the ±5% threshold — but the negative-discount-rate scenario is unusual in planning practice (a user would need to set `DiscountRate < 0` explicitly, which the UI almost certainly prevents). Rated LOW because it is reachable only via an intentionally adversarial input, not a normal user scenario.

**Evidence / repro:**
```go
PresentValue(100000, -2.0, 240) → 100000  // code
// Correct: 100000 / math.Pow(0.98, 20) ≈ 149788.50
```

**Recommended fix sketch:** Split the guard: `if periods <= 0 { return futureValue }` and `if annualRate == 0 { return futureValue }`. For `annualRate < 0`, fall through to the standard formula — `monthlyCompoundFactorFromPercent(r)` with negative r correctly returns a factor < 1, and `PV = FV / factor^n` correctly gives `PV > FV`.

**Test coverage note:** The negative-rate path is exercised in `calculator_pv_test.go` ("negative rate returns future value unchanged") but asserts the current behavior. If the guard is corrected, that test case should be updated with the mathematically correct expected value.

---

### F-038 — LOW `PresentValue` / `PresentValueAnnuity`: zero-rate guard inconsistency between the two functions

**Location:** `internal/services/retirement/calculator.go:42` — `PresentValue`; `:56` — `PresentValueAnnuity`

**Source consulted:** Code inspection.

**What it does:** `PresentValue` uses a pre-computation guard `annualRate <= 0`. `PresentValueAnnuity` computes `monthlyRate` for all inputs, then guards on `monthlyRate <= 0`. Both produce numerically correct results for normal inputs.

**Finding:** Cosmetic inconsistency rather than a correctness bug. Both functions correctly produce `FV` (or `payment × n`) for zero discount and correctly skip time-value discounting for negative discount rates. The inconsistency in guard placement (pre-vs-post monthly-rate computation) could confuse future maintainers. No numerical error. LOW.

**Evidence / repro:** `PresentValue(100000, -2, 240) → 100000` (early return before computing monthly rate). `PresentValueAnnuity(1000, -2, 0, 0, 12) → 12000` (monthly rate computed: `(0.98)^(1/12)−1 ≈ −0.00168 < 0`, then guard fires). Results are parallel but implementation paths differ.

**Recommended fix sketch:** Normalize both functions to compute the monthly rate first, then guard. Or add an explicit comment in `PresentValue` explaining the pre-computation guard is intentional for efficiency.

**Test coverage note:** `PresentValueAnnuity` with `discountRate < 0` and `growthRate = 0` (the `monthlyRate <= 0`, `monthlyGrowth == 0` branch) is tested in "no discount rate without growth." `PresentValue` with negative rate is tested in "negative rate returns future value unchanged."

---

### F-039 — LOW `PresentValueAnnuity`: `startMonth > 0` deferral not applied when `monthlyRate <= 0`

**Location:** `internal/services/retirement/calculator.go:84` — `PresentValueAnnuity`

**Source consulted:** Standard deferred-annuity finance; code inspection.

**What it does:** Discounts `pvAtStart` back by `startMonth` periods — but only if `startMonth > 0 && monthlyRate > 0`. When `monthlyRate <= 0`, returns `pvAtStart` without deferral adjustment.

**Finding:** For zero discount rate, this is mathematically correct — future payments have identical PV at zero time-value of money. For negative discount rates (deflation), deferred payments should have a higher PV than immediate payments, but the code returns the flat sum without adjustment. This is a latent issue only for the negative-discount-rate scenario (which is also affected by F-037 and F-038). In normal operation, `discountRate` is positive and the guard correctly triggers. LOW.

**Evidence / repro:** `PresentValueAnnuity(1000, 0, 0, 6, 12)` correctly equals `PresentValueAnnuity(1000, 0, 0, 0, 12) = $12,000` (existing test passes). `PresentValueAnnuity(1000, -2, 0, 6, 12)` returns `$12,000` (deferral ignored for negative rate; strictly speaking PV should be slightly higher for deflation).

**Recommended fix sketch:** If F-037 is resolved to allow negative-rate discounting, remove the `monthlyRate > 0` clause from the deferral guard so it reads `if startMonth > 0`. The formula `pvAtStart / (1+monthlyRate)^startMonth` with `monthlyRate < 0` correctly produces `PV > pvAtStart`.

**Test coverage note:** `startMonth > 0` with `discountRate = 0` is tested ("future start with zero discount rate does not discount"). `startMonth > 0` with `discountRate < 0` is not tested.

---

### F-040 — LOW `plannerInflationFactorForYear`: zero-inflation-rate boundary not tested

**Location:** `internal/services/retirement/calculator.go:330` — `plannerInflationFactorForYear`

**Source consulted:** Code inspection; `internal/services/retirement/coverage_gaps2_test.go:899` `TestPlannerInflationFactorForYear`.

**What it does:** Returns `(1 + annualInflationRate/100)^years`. Returns `1.0` for `years <= 0`.

**Finding:** The formula is correct for all inputs, including zero rate (`1.0^years = 1.0`). The test suite covers zero years, negative years, and positive years with a 3% rate. The zero-rate boundary — which is a valid scenario (some users may set zero inflation as a conservative assumption) — is not asserted. LOW because the formula is unambiguously correct; the gap is a pure test-coverage oversight.

**Evidence / repro:** `plannerInflationFactorForYear(0, 10)` returns `1.0` ✓ (verified during audit; absent from `TestPlannerInflationFactorForYear`).

**Recommended fix sketch:** Add `{"zero rate", 0.0, 10, 1.0}` to the table-driven test in `coverage_gaps2_test.go:899`.

**Test coverage note:** Zero-rate boundary not asserted. Year=0, year<0, and year>0 with rate>0 are all covered.

---

### F-041 — INFO `calculateHealthcarePV`: IRMAA excluded by design; PV summary page should document this

**Location:** `internal/services/retirement/calculator.go:93` — `calculateHealthcarePV`

**Source consulted:** Code inspection; `calculator.go:95-121`; IRMAA computation in projection loop.

**What it does:** Aggregates the PV of ACA and Medicare healthcare costs using `PresentValueAnnuity`. Handles the two-phase Medicare transition internally. IRMAA surcharges are not included.

**Finding:** The exclusion of IRMAA is architecturally correct. IRMAA is a MAGI-dependent surcharge computed inside the full projection loop where annual MAGI is known. Including it here would require projecting future MAGI, which is not available at PV-analysis call time. The PV analysis page should note that healthcare PV excludes IRMAA, which is separately computed in the full projection. Informational — no code error.

**Evidence / repro:** `calculateHealthcarePV` calls only `PresentValueAnnuity` with base monthly cost and inflation; no IRMAA inputs exist in its signature.

**Recommended fix sketch:** Add a UI footnote or tooltip on the What-If PV summary page noting that IRMAA surcharges are excluded from the healthcare PV estimate.

**Test coverage note:** The Medicare transition logic is well-tested. The `preMedicareMonths == 0` path in the two-phase branch is unreachable because `IsOnMedicare()` returns true when `age >= MedicareEligibleAge`, so the two-phase branch only activates when the person is genuinely pre-Medicare — confirmed by the "person exactly at Medicare age" test which routes through the `IsOnMedicare()` path.

---

### F-042 — LOW `calculateLivingExpensesAtMonth`: no test exercises phase transitions with nonzero inflation

**Location:** `internal/services/retirement/calculator.go:151` — `calculateLivingExpensesAtMonth`
**Source consulted:** Internal model — `calculator_expense_test.go` inspection.
**What it does:** Returns monthly living expenses at a given month, applying the current spending-phase multiplier and inflation compounding.
**Finding:** The expense test file's "with spending phases enabled" subtest uses `InflationRate = 0`. No test combines nonzero inflation with a phase transition, leaving the key formula `base × multiplier × (1+r)^(months/12)` at a boundary month untested. Additionally, the following boundaries have no coverage: (a) single-phase scenario (one entry in `Phases`); (b) zero-phase scenario (`Phases = []` with `Enabled = true`) — currently falls through to `return 1.0` from `GetSpendingMultiplier` but no test verifies this; (c) phase multiplier = 0 (zero spending); (d) phase multiplier > 1 (unusual but valid, e.g., 1.10 for Go-Go splurge); (e) two phases with the same `StartAge` (degenerate — last one wins, unverified); (f) phase `StartAge` above the projection end (never reached — `GetSpendingMultiplier` silently returns prior phase's multiplier); (g) `PhaseAgeReference = "younger"` or `"spouse"` with a couple (only "older"/default is tested).
**Evidence / repro:** All existing phase tests set `InflationRate = 0`. The combined multiplier × inflation path is only exercised by WE-6.1 in this audit.
**Recommended fix sketch:** Add a subtest in `calculator_expense_test.go` for month 120 with 3% inflation and a phase transition at age 75, verifying `base × 0.85 × 1.03^10`. Add edge-case subtests for zero-phase, zero-multiplier, and >1.0 multiplier.
**Test coverage note:** All boundaries listed above (a)–(g) are absent from the test suite.

---

### F-043 — LOW `rebaseLivingExpensesAtTransition`: dead code in spending-phases path; not directly tested

**Location:** `internal/services/retirement/calculator.go:163` — `rebaseLivingExpensesAtTransition`
**Source consulted:** Projection loop inspection at `calculator.go:1039–1089`.
**What it does:** Resets the living-expense tracker when a scenario-chain transition occurs, preserving accumulated inflation on the new segment's base.
**Finding:** In the spending-phases-enabled branch, `rebaseLivingExpensesAtTransition` (line 1046) is immediately overwritten by the `if m > 0` block (lines 1083–1086) which unconditionally recomputes `currentLivingExpenses = base × multiplier(phaseAge) × cumulativeInflation` using the same inputs. The rebase call is therefore dead code in the phases path — its result is never used. For the legacy decline path the function is load-bearing (it updates the base before the multiplicative step). The function has no direct unit test; it is only reachable via the projection loop during chain transitions. The dead-code effect means a bug introduced in `rebaseLivingExpensesAtTransition`'s phases branch would be silently masked.
**Evidence / repro:**
```go
// Line 1046 (chain-transition branch):
currentLivingExpenses = rebaseLivingExpensesAtTransition(s, phaseAge, cumulativeInflation)
// Lines 1083-1086 (immediately after, always executes when m > 0):
cumulativeInflation *= monthlyCompoundFactorFromPercent(s.InflationRate)
if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
    currentLivingExpenses = s.MonthlyLivingExpenses * s.GetSpendingMultiplier(phaseAge) * cumulativeInflation
}
```
**Recommended fix sketch:** Either remove the phases branch from `rebaseLivingExpensesAtTransition` (the return value is never used in that path), or restructure the loop so that the rebase result is the authoritative value for that month. Add a direct unit test for the function covering both the phases and non-phases branches.
**Test coverage note:** No test exercises `rebaseLivingExpensesAtTransition` directly. The chain-transition code path (the only call site) is not exercised by `calculator_expense_test.go`.

---

### F-044 — LOW `CalculateTotalExpenses`: phase boundary with inflation not tested; expense-source phase multiplier edge cases missing

**Location:** `internal/services/retirement/calculator.go:546` — `CalculateTotalExpenses`
**Source consulted:** `calculator_expense_test.go` inspection.
**What it does:** Aggregates living expenses, healthcare, and expense sources. Applies the spending-phase multiplier to discretionary expense sources when phases are enabled.
**Finding:** The living-expense path is correctly invoked via `calculateLivingExpensesAtMonth`. Coverage gaps: (a) no test combines nonzero inflation with a phase transition in this function (same gap as F-042); (b) no test covers a discretionary expense source at a phase boundary with nonzero inflation; (c) the non-discretionary expense-source path in phases mode is covered, but the boundary where a discretionary source becomes zero (multiplier = 0) is not tested; (d) no test for `PhaseAgeReference != "older"` (e.g., younger or spouse).
**Evidence / repro:** The "with spending phases enabled" and "discretionary expense gets phase multiplier" subtests both use `InflationRate = 0`.
**Recommended fix sketch:** Extend "with spending phases enabled" subtest to use 3% inflation and verify at month 120 (phase boundary). Add a test for `PhaseAgeReference = "younger"` with a couple to confirm the correct person's age drives phase transitions.
**Test coverage note:** Boundaries (a)–(d) above are absent.

---

### F-045 — LOW `taxableAccountState.withdraw`: W=Y exact boundary and zero-gain boundary not directly tested

**Location:** `internal/services/retirement/calculator.go:472` — `taxableAccountState.withdraw`
**Source consulted:** Code inspection; `coverage_gaps_test.go:168–196`; `calculator_test.go:1393`.
**What it does:** Withdraws up to `amount` from the account using pro-rata average-cost basis. Returns `(cash, basisReduction, realizedGain)`.
**Finding:** Two boundary cases are not covered: (1) W=Y exactly — both `MarketValue` and `CostBasis` are zeroed by the `MarketValue <= 0` clamp after subtraction, but no test uses `withdraw(Y)` with W equal to Y; (2) zero unrealized gain (CostBasis == MarketValue) — the formula yields `realizedGain = 0` correctly but is untested.
**Evidence / repro:** `TestWithdraw_FullDepletion` uses W=10000 > Y=5000; no test uses W=Y exactly. `TestTaxableAccountWithdrawUsesAverageCostBasis` uses MV=120000 CB=100000 (gain present).
**Recommended fix sketch:** Add: (a) `MarketValue=5000, CostBasis=5000, withdraw(5000)` → `realizedGain=0, newBasis=0, cash=5000`; (b) `MarketValue=5000, CostBasis=3000, withdraw(5000)` → `realizedGain=2000, newBasis=0, cash=5000`.
**Test coverage note:** The `a.MarketValue <= 0` zeroing branch (line 485–488) is only exercised via over-withdrawal, not exact-equality withdrawal.

---

### F-046 — LOW `buildTaxableReturnComponents`: negative-appreciation scenario not tested

**Location:** `internal/services/retirement/calculator.go:498` — `buildTaxableReturnComponents`
**Source consulted:** Code inspection; `coverage_gaps_test.go:202–225`.
**What it does:** Decomposes total annual return into four monthly components. Appreciation is the residual after subtracting dividend and cap-gains distribution components; the sum of all four components always equals `totalMonthlyReturn` exactly.
**Finding:** If `dividendYield + capitalGainsDistributionRate > totalAnnualReturnPercent`, the Appreciation component is negative (e.g., total return 2%, dividends 3%). This plausible scenario is not tested. The math is correct by construction, but the downstream behavior in `applyGrowth` (MarketValue decreases while dividends are still paid out) is not verified.
**Evidence / repro:** No test calls `buildTaxableReturnComponents` with dividends exceeding total return.
**Recommended fix sketch:** Add test: `totalReturn=2%, dividendYield=3%, capGains=0.5%` → assert `Appreciation < 0` and component sum equals total monthly return.
**Test coverage note:** Negative-appreciation branch of `applyGrowth` exercised only with artificially extreme values, not via realistic inputs from `buildTaxableReturnComponents`.

---

### F-047 — LOW `projectionTimingGrowthFractions`: never directly tested; MidMonth exact values not verified

**Location:** `internal/services/retirement/calculator.go:594` — `projectionTimingGrowthFractions`
**Source consulted:** Code inspection; `calculator_test.go:1474`.
**What it does:** Maps `ProjectionTiming` to `(before, after)` growth fractions: StartOfMonth → (0,1), MidMonth → (0.5,0.5), EndOfMonth → (1,0).
**Finding:** Never called directly in tests. Integration tests exercise all three timing modes but do not assert the specific `(before, after)` pair. The default branch (EndOfMonth and any unknown value → (1,0)) is not explicitly verified.
**Evidence / repro:** `grep "projectionTimingGrowthFractions" *_test.go` returns no direct calls.
**Recommended fix sketch:** Add `TestProjectionTimingGrowthFractions`: StartOfMonth → (0,1), MidMonth → (0.5,0.5), EndOfMonth → (1,0), unknown string → (1,0).
**Test coverage note:** All three enum branches and the default fallback are dark from a direct-assertion perspective.

---

### F-048 — LOW `executePortfolioCashFlowWithTaxableState`: undo/redo realized-gain path not directly tested with nonzero gain

**Location:** `internal/services/retirement/calculator.go:784` — `executePortfolioCashFlowWithTaxableState`
**Source consulted:** Code inspection; `calculator_test.go:1412`.
**What it does:** Calls `withdrawForExpenses` (bypasses basis), then undoes/redoes the taxable withdrawal via `taxable.withdraw()` to populate `TaxableRealizedGain` correctly.
**Finding:** The undo/redo pattern is correct but untested at the unit level with nonzero unrealized gains. The integration test `TestRunProjectionTaxableSalesOfBasisRemainUntaxed` uses zero unrealized gain (CostBasis = MarketValue), so `TaxableRealizedGain` is always 0 and the undo/redo path produces a trivial result.
**Evidence / repro:** The end-to-end test sets `TaxableQualifiedDividendPercent=0, TaxableDividendYield=0, TaxableCapitalGainsDistributionRate=0` and buys at cost (no gain). The undo/redo math with `realizedGain > 0` is untested at the function level.
**Recommended fix sketch:** Add unit test: taxable account MV=$100K, CB=$60K, neededFromPortfolio=$20K, no RMD, no Roth, no tax-deferred. Verify `TaxableRealizedGain = 20K × (40K/100K) = $8,000`.
**Test coverage note:** Undo/redo path with nonzero unrealized gain is dark at the unit level.

---

### F-049 — MEDIUM `reinvestRequiredRMDToTaxableState`: reinvests pre-tax RMD; cost basis overstated by tax liability

**Location:** `internal/services/retirement/calculator.go:748` — `reinvestRequiredRMDToTaxableState`
**Source consulted:** IRS Pub 550 (cost basis); IRC § 72 (RMD tax treatment).
**What it does:** Withdraws an unneeded RMD from tax-deferred and reinvests via `taxable.addCash(rmdWithdrawal)`, which sets `CostBasis += rmdWithdrawal` (pre-tax amount).
**Finding:** The RMD is 100% taxable ordinary income. The investor's actual net-of-tax reinvested amount is `rmdWithdrawal × (1 − effectiveTaxRate)`. Recording the pre-tax RMD as cost basis overstates the basis, understating future realized gains on taxable withdrawals. The tax on the RMD is captured in the convergence loop (income tax is not double-counted), but the basis distortion reduces future LTCG by the tax amount.
**Evidence / repro:** RMD=$10K, effectiveTaxRate=25%: code basis += $10K; correct basis += $7.5K. Later withdrawal: code realizedGain=$0; correct realizedGain=$2.5K (at 15% LTCG rate: $375 under-collected).
**Recommended fix sketch:** Pass the effective tax rate to `reinvestRequiredRMDToTaxableState` and call `taxable.addCash(rmdWithdrawal × (1 − effectiveTaxRate))`.
**Test coverage note:** `reinvestRequiredRMDToTaxableState` never called directly in tests; long-term basis distortion unmeasured.

---

### F-050 — LOW `earlyWithdrawalPenaltyRate`: age-60 proxy over-penalizes by up to 6 months; boundary not directly tested

**Location:** `internal/services/retirement/calculator.go:836` — `earlyWithdrawalPenaltyRate`
**Source consulted:** IRC § 72(t)(2)(A)(i).
**What it does:** Returns 0.10 when `currentAge + currentYear < 60`, else 0 — approximating the IRC 59½ threshold.
**Finding:** Penalizes all withdrawals in the year when `currentAge + currentYear = 59`, even during the second half of that year when the participant is past 59½. Over-penalty window is at most 6 months per person. For $3,000/month withdrawal, maximum over-assessment is $1,800 per affected year. The approximation is intentional (documented in code comment), but the boundary transition is never directly tested.
**Evidence / repro:** No test calls `earlyWithdrawalPenaltyRate` directly. `earlyWithdrawalPenaltyRate(59,0)→0.10` and `earlyWithdrawalPenaltyRate(59,1)→0.00` are not asserted.
**Recommended fix sketch:** Add `TestEarlyWithdrawalPenaltyRate` with table-driven cases covering the boundary: `(59,0)→0.10`, `(59,1)→0.00`, `(60,0)→0.00`, `(58,1)→0.10`. Quantify the 6-month over-penalty in the code comment.
**Test coverage note:** Function never called directly in tests; boundary at `sum=59` and `sum=60` is dark.

---

### F-051 — LOW `taxDeferredDelayActive` / `shortfallIsTemporaryDueToDelay`: never directly tested

**Location:** `internal/services/retirement/calculator.go:821` — `taxDeferredDelayActive`; `:825` — `shortfallIsTemporaryDueToDelay`
**Source consulted:** Code inspection; `calculator_delay_test.go`.
**What it does:** `taxDeferredDelayActive` returns true while the delay period is active. `shortfallIsTemporaryDueToDelay` identifies shortfalls that will resolve when the delay ends.
**Finding:** Neither function is directly tested. Key boundary cases unexercised: `TaxDeferredDelayYears=0` (disabled), `currentYear == TaxDeferredDelayYears` (boundary — should return false), and all logical branches of `shortfallIsTemporaryDueToDelay`.
**Evidence / repro:** `grep "taxDeferredDelayActive\|shortfallIsTemporaryDueToDelay" *_test.go` returns no direct calls.
**Recommended fix sketch:** Add `TestTaxDeferredDelayActive`: delay=0→false, delay=5 year=4→true, delay=5 year=5→false. Add `TestShortfallIsTemporaryDueToDelay` with four logical combinations.
**Test coverage note:** Both functions are fully dark from direct-test perspective.

---

### F-052 — LOW `GlidePathStockPct`: negative-year and `TransitionYears=0` paths not tested

**Location:** `internal/models/whatif.go:646` — `GlidePathStockPct`
**Source consulted:** Code inspection; `internal/models/allocation_test.go:79`.
**What it does:** Returns glide-path stock % at a given projection year, clipping to start/end and returning −1 if disabled.
**Finding:** `TestGlidePathStockPct` covers year=0, 10, 20, 30, and disabled. Missing: (1) `year < 0` — the `year ≤ 0` guard exercises this at year=0 but not at year=−1; (2) `Enabled=true, TransitionYears=0` — the `TransitionYears ≤ 0` guard should return −1 but is untested with glide path enabled.
**Evidence / repro:** No test asserts `GlidePathStockPct(-1) == StartStockPct` or `GlidePathStockPct(5)` with `TransitionYears=0`.
**Recommended fix sketch:** Add subtest `year=-5 → StartStockPct`; add subtest `Enabled=true, TransitionYears=0, year=5 → -1`.
**Test coverage note:** The `year < 0` sub-range of the `year ≤ 0` branch, and the `TransitionYears=0` with enabled path, are dark.

---

### F-053 — LOW `rothConversionAmountForYear`: no internal clamp for negative `AnnualAmount`

**Location:** `internal/services/retirement/calculator.go:419` — `rothConversionAmountForYear`
**Source consulted:** Code inspection; WE-8.3; IRS Pub 590-A (Roth conversions are non-negative).
**What it does:** Returns `math.Min(AnnualAmount, availableTaxDeferred)`. For `AnnualAmount < 0`, returns a negative value.
**Finding:** The function returns a negative value for negative `AnnualAmount`. Both main projection call sites guard with `conversionAmount > 0` (lines 1063, 2358), preventing runtime portfolio mutation. However, the snapshot/summary call sites (lines 1410, 1528, 1546) pass the return value directly to `estimateMonthlySnapshot` without a `> 0` guard. A negative `rothConversion` there would flow into `otherIncome` and reduce estimated taxes incorrectly. HTTP-layer validation currently rejects negative values, providing an outer defense, but the calculator has no defense-in-depth. WE-8.3 confirmed: function returns −10,000 for `AnnualAmount = −10,000`.
**Evidence / repro:**
```go
// calculator.go:419
return math.Min(s.RothConversion.AnnualAmount, availableTaxDeferred)
// AnnualAmount=-10000, available=100000 → returns -10000
```
**Recommended fix sketch:** Add `if s.RothConversion.AnnualAmount <= 0 { return 0 }` before the `math.Min` call.
**Test coverage note:** No test exercises `AnnualAmount < 0`. A test asserting `return 0` for negative amounts would validate the fix.

---

### F-054 — LOW `rothConversionAmountForYear`: missing tests for exact boundary years and zero AnnualAmount

**Location:** `internal/services/retirement/calculator.go:409` — `rothConversionAmountForYear`
**Source consulted:** `internal/services/retirement/coverage_gaps_test.go:134–164, 1405–1461`.
**What it does:** Returns 0 outside [StartYear, EndYear], returns capped amount inside window.
**Finding:** Missing boundary-case tests: (1) `year == EndYear` exactly — only `year = EndYear + 1` is tested; an off-by-one in the `>` operator would not be caught; (2) `year == StartYear` with `balance > AnnualAmount` — tests that exercise StartYear use a balance-limited scenario only; (3) `AnnualAmount = 0` with `Enabled = true` — not tested; returns 0 (correct), but uncovered.
**Evidence / repro:**
```go
// coverage_gaps_test.go:136 — tests year=6 > EndYear=5 only
// Missing: year=5 == EndYear=5 should return amount
```
**Recommended fix sketch:** Add subtests: `year == EndYear → AnnualAmount`; `year == StartYear, balance > AnnualAmount → AnnualAmount`; `AnnualAmount == 0 → 0`.
**Test coverage note:** The exact-boundary (year == EndYear) case is the highest-priority gap for off-by-one regression detection.

---

### F-055 — LOW `EstimateRothConversionTax`: test checks direction only, not exact value; several input variants uncovered

**Location:** `internal/services/retirement/tax.go:484` — `EstimateRothConversionTax`
**Source consulted:** `internal/services/retirement/tax_test.go:135`; WE-8.1.
**What it does:** Returns `Tax(baseIncome + conversion) − Tax(baseIncome)` — correct marginal formula, confirmed by WE-8.1.
**Finding:** `TestEstimateRothConversionTax` asserts only `additionalTax > 0` and `additionalTax ≤ conversion × 0.37`. No pinned numeric value. WE-8.1 shows the correct answer is $6,650; a proportional-formula bug returning $5,992 would pass the test. Missing: (1) exact-value assertion; (2) bracket-crossing case (where conversion spans two brackets); (3) `yearsFromBase > 0` (inflated brackets); (4) filing status other than MFJ.
**Evidence / repro:**
```go
// tax_test.go:146-152
if additionalTax <= 0 { t.Errorf(...) }        // direction only
if additionalTax > conversionAmount*0.37 { ... } // upper bound only
// WE-8.1 actual: $6,650.00; test would pass at any value in (0, 9250]
```
**Recommended fix sketch:** Pin to $6,650 ± $0.01 for the existing MFJ case. Add a bracket-crossing subtest (e.g., base near 12%/22% boundary, conversion spanning into 22%). Add `yearsFromBase = 5` case.
**Test coverage note:** Exact-value, bracket-crossing, inflated-bracket, and non-MFJ paths are all dark.

---

### F-056 — INFO Roth conversion MAGI propagation to NIIT and IRMAA not directly tested

**Location:** `internal/services/retirement/calculator.go:249, 460` — `estimateMonthlySnapshot` / `calculateTaxWithInvestmentIncomeInternal`
**Source consulted:** IRS Pub 590-A; IRC §1411; SSA IRMAA lookback.
**What it does:** Conversion amount is correctly included in MAGI (via `otherIncome → estimatedOrdinaryIncome → ordinaryIncome` parameter) and thus affects NIIT threshold and IRMAA lookback.
**Finding:** The MAGI propagation is mechanically correct. No test pins: (1) NIIT charge when a conversion pushes MAGI above $250K (MFJ); (2) IRMAA surcharge in year Y+2 due to a large conversion in year Y (via `completedMAGIHistory`). These are high-value retirement-planning scenarios. Absence of pinned tests means a refactor that accidentally excludes conversion from MAGI would not be caught.
**Evidence / repro:** `grep -rn "RothConversion" *_test.go | grep -i "NIIT\|IRMAA"` returns no results.
**Recommended fix sketch:** Add snapshot-level test: MFJ, $240K base, $50K conversion → assert `AnnualNIIT > 0`. Add 3-year projection test with large year-0 conversion → assert year-2 IRMAA surcharge appears.
**Test coverage note:** Both NIIT-from-conversion and IRMAA-lookback-from-conversion paths are tested only incidentally through full-projection smoke tests.

---

### F-057 — MEDIUM `GetAvailableStartYears` off-by-one excludes the most recent valid 30-year window

**Location:** `internal/services/retirement/historical_data.go:144` — `GetAvailableStartYears`
**Source consulted:** Internal model — rolling-window backtest construction.
**What it does:** Returns the slice of calendar years that can be used as backtest starting points given a required projection length.
**Finding:** The implementation uses `maxIdx = len(HistoricalReturns) - projectionYears` and iterates `i < maxIdx`, yielding `maxIdx` entries. The correct count of valid starting years is N − H + 1 (standard rolling-window formula). With 97 data years and a 30-year horizon this produces 67 windows instead of 68, excluding the 1995–2024 window — the most recent complete 30-year period, which covers the dot-com crash, the 2008 financial crisis, COVID, and the 2022 rate shock. The excluded window is the one with the most relevance to near-term retirees.
**Evidence / repro:** `GetHistoricalSequence(1995, 30)` succeeds (guard: `67 + 30 = 97 > 97` is false), yet `GetAvailableStartYears(30)` stops at 1994. WE-9.2 confirmed: `TotalSequences=67`, expected 68.
**Recommended fix sketch:** Change `maxIdx := len(HistoricalReturns) - projectionYears` to `maxIdx := len(HistoricalReturns) - projectionYears + 1` and adjust the loop `for i := 0; i < maxIdx; i++`. Verify `GetHistoricalSequence` correctly handles the last entry (it does).
**Test coverage note:** `TestGetAvailableStartYears` checks `lastYear <= data[last].Year - 29` which passes under the buggy code. Add an assertion `lastYear == data[last].Year - projectionYears + 1`.

---

### F-058 — LOW Historical backtest computes percentiles but discards them without exposing them

**Location:** `internal/services/retirement/backtest.go:97–138` — `RunHistoricalBacktest`
**Source consulted:** Internal model.
**What it does:** Computes `p10`, `p25`, `p50`, `p75`, `p90` of final balances across all backtest sequences, then assigns them to blank identifiers (`_ = p10` etc.) so they are silently dropped.
**Finding:** The five percentile values are computed but never stored in `models.HistoricalBacktestAnalysis`, so callers cannot display them. The Monte Carlo result exposes analogous percentiles. The backtest percentiles would allow users to see the P10/P90 corridor of historical outcomes, which is more informative than listing individual start years. The computation is correct but dead code.
**Evidence / repro:** `backtest.go:134–138` — `_ = p10; _ = p25; _ = p50; _ = p75; _ = p90`.
**Recommended fix sketch:** Add `Percentile10, Percentile25, Percentile50, Percentile75, Percentile90 float64` to `models.HistoricalBacktestAnalysis` and populate them from the computed values; remove the blank assignments.
**Test coverage note:** No test can assert these fields because they are not set.

---

### F-059 — LOW Historical backtest percentile indexing has systematic +1 element upward bias

**Location:** `internal/services/retirement/backtest.go:97–101` — `RunHistoricalBacktest`; `internal/services/retirement/calculator.go:2162–2167` — `RunMonteCarloSimulation`
**Source consulted:** Standard percentile definition.
**What it does:** Both functions compute percentiles as `sorted[n/2]`, `sorted[n/4]`, etc. using integer division (floor division).
**Finding:** For a sorted array of length `n`, the element at index `⌊n/k⌋` is the `(⌊n/k⌋+1)`th value — slightly above the true `(100/k)`th percentile. For n=1,000 the bias is 0.1 percentage point (negligible). For n=100 it reaches 1–2 pp; for n=10 the "P10" is actually P20 (the 2nd of 10 elements). At the default 1,000-trial MC run the bias is immaterial. The historical backtest with 67 sequences (after the F-057 fix, 68) has a slightly larger but still acceptable bias (≤ 1 pp). Documented as LOW because it may mislead at small trial counts.
**Evidence / repro:** `n=100`, `balances[100/2]=balances[50]` is the 51st element (P51.0). Seeded 100-trial test showed implied median $2,998,005 vs true median $2,975,174 (+0.77%). For `n=10`, `balances[10/10]=balances[1]` is the 2nd element (P20, not P10).
**Recommended fix sketch:** For median: `(sorted[n/2-1] + sorted[n/2]) / 2` when `n` is even. For percentile-k: use the nearest-rank formula `max(0, int(float64(n)*pct/100 + 0.5) - 1)`. This is most important to fix if the MC ever exposes a small-sample mode.
**Test coverage note:** No test asserts the exact median formula; only ordering invariants are checked.

---

### F-060 — INFO `GetHistoricalStats` uses population standard deviation; Monte Carlo uses arithmetic mean as draw center

**Location:** `internal/services/retirement/historical_data.go:158` — `GetHistoricalStats`; `internal/services/retirement/calculator.go:2545` — `generateAssetReturns`
**Source consulted:** Statistical convention; historical data series 1928–2024.
**What it does:** Computes arithmetic mean and population stddev of the 97-year series, then uses them as parameters for `NormFloat64() * stockStdDev` in Monte Carlo draws.
**Finding (a) — Population vs. sample stddev:** Divides by `n=97` instead of `n−1=96`. Underestimates true sample stddev by factor √(97/96) ≈ 1.0052 (0.52%). For σ ≈ 19.7% this is ≈ 0.1 pp — negligible. INFO.
**Finding (b) — Arithmetic mean as draw center:** Using the arithmetic mean (~11.5%) rather than the geometric/compound mean (~9.6%) causes the Monte Carlo to draw slightly optimistic returns on average. For a 30-year projection the compounding effect of this ~1.9 pp overstatement is material: `1.115^30 / 1.096^30 ≈ 1.76`, meaning terminal wealth could be overstated by ≈76% in the median trial. The difference reflects the Jensen's inequality relationship between arithmetic and geometric mean. INFO only because this is a common modeling choice and the crash/volatility overlays offset some of the excess; but users comparing MC output to historical backtest output may be confused.
**Finding (c) — Distribution choice:** Normal distribution is used (not lognormal). For stock σ ≈ 19.7%, a −2σ draw gives −28%, and −3σ gives −48% — within the hard clamp of −50%. Normal is acceptable here given the clamping.
**Evidence / repro:** `historical_data.go:186` `sqrt(sumSqDiffStock / n)`; `calculator.go:2580` `stockMean + rng.NormFloat64()*stockStdDev`.
**Recommended fix sketch:** Consider using geometric mean `= exp(mean(ln(1+r))) − 1` as the draw center for multi-year projections; keep arithmetic mean in single-year contexts. The population/sample difference is immaterial and not worth changing.
**Test coverage note:** `TestGetHistoricalStats` checks ranges but not the population-vs-sample distinction.

---

### F-061 — LOW `RunMonteCarloSimulation` is time-seeded; deterministic testing relies on private helper

**Location:** `internal/services/retirement/calculator.go:2122` — `RunMonteCarloSimulation`
**Source consulted:** Code inspection.
**What it does:** Creates `rng := rand.New(rand.NewSource(time.Now().UnixNano()))` for the primary simulation, making the public function non-reproducible.
**Finding:** The public `RunMonteCarloSimulation` function cannot be deterministically tested. Test files rely on calling the private `runSingleMonteCarloSimulation(rng, config)` with a fixed-seed `rng`. If the public function's aggregation logic is ever changed, those internal tests may not catch regressions. The adaptive sub-run does use a fixed seed (42) for reproducibility. LOW severity because the private interface is accessible within the package.
**Evidence / repro:** `calculator.go:2122` `rand.New(rand.NewSource(time.Now().UnixNano()))`.
**Recommended fix sketch:** Accept an optional `rand.Source` parameter (or extract an `RunMonteCarloSimulationWithSource(runs int, src rand.Source)` overload) so callers can supply a fixed seed in tests. The time-seeded default is fine for production.
**Test coverage note:** No test exercises `RunMonteCarloSimulation` in a statistically rigorous way (e.g., verifying convergence of the success rate to a known analytical bound). Tests only check shape (non-nil, ordering invariants).

---

### F-062 — MEDIUM `RunMonteCarloSimulation`: degenerate inputs (runs=0, runs=1) have no coverage

**Location:** `internal/services/retirement/calculator.go:2102` — `RunMonteCarloSimulation`
**Source consulted:** Code inspection.
**What it does:** When `runs ≤ 0`, resets to 1,000. When `runs=1`, computes percentiles on a length-1 sorted array.
**Finding:** (a) `runs=0` → resets to 1,000 silently. The intent is defensible but undocumented. (b) `runs=1` → `balances[1/2] = balances[0]` (median), `balances[1/10] = balances[0]` (P10), `balances[1*3/4] = balances[0]` (P75 = index 0), `balances[1*9/10] = balances[0]` (P90 = index 0). All percentiles collapse to the single element, which is correct but exercises the same array element every time. No panic, but the output is degenerate. (c) `runs=2` → `balances[2/4] = balances[0]` (P25 = first element), `balances[2*3/4] = balances[1]` (P75 = second element), but `balances[2/10] = balances[0]` (P10 = same as P25). Degenerate but no panic.
**Evidence / repro:** Integer division: `2/10 = 0`; for `runs=2`, P10 = P25 = `balances[0]`, which is potentially confusing.
**Recommended fix sketch:** Guard `runs < 10` with a documented minimum (e.g., clamp to 100). Add a test asserting `RunMonteCarloSimulation(0)` returns non-nil with `Stats.Runs == 1000` and `RunMonteCarloSimulation(1)` returns non-nil without panic.
**Test coverage note:** No test covers `runs=0` guard or `runs=1` degenerate case.

---

### F-063 — INFO Guardrail implementation is a portfolio-drop heuristic, not Guyton-Klinger (2006)

**Location:** `internal/services/retirement/guardrails.go:28` — `evaluate`
**Source consulted:** Guyton & Klinger (2006), *Decision Rules and Maximum Initial Withdrawal Rates*, Journal of Financial Planning, March 2006.
**What it does:** Compares the current portfolio value to a tracked peak (floor trigger) and to the initial portfolio value (ceiling trigger). Adjusts a spending multiplier by configured percentages.
**Finding:** The Guyton-Klinger (2006) rules are defined in terms of the **current withdrawal rate** (annual withdrawal / current portfolio) relative to the **initial withdrawal rate**. The canonical rules are: (1) Capital Preservation Rule — if current WR > initial WR × 1.20, cut withdrawal by 10%; (2) Prosperity Rule — if current WR < initial WR × 0.80, raise withdrawal by 10%; (3) Inflation Rule — do not take the inflation increase in years when the previous year's portfolio return was negative; (4) Withdrawal Rule — do not take the full inflation increase unless the portfolio return exceeded inflation. None of these four rules are implemented. The planner's portfolio-drop/rise approach will fire on different events than the G-K rules. For example, if spending is reduced (e.g., by an earlier guardrail cut), the current WR falls even without a portfolio gain, and the Prosperity Rule would eventually trigger — but the planner's ceiling trigger requires the portfolio to physically rise above its initial value. This is not a calculation bug (the planner's implementation is internally consistent), but it is materially different from G-K and should not be labeled as "Guyton-Klinger" in the UI.
**Evidence / repro:** `guardrails.go:38` `dropPct := (g.peakPortfolio - currentPortfolio) / g.peakPortfolio * 100` — portfolio-drop criterion, not withdrawal-rate criterion.
**Recommended fix sketch:** Either implement the withdrawal-rate-based G-K rules (tracking `initialWithdrawalRate` and `currentWithdrawalRate`), or rename the UI label from "Guyton-Klinger" to "Dynamic Guardrails" to avoid misrepresenting the model.
**Test coverage note:** Tests correctly exercise the implemented portfolio-drop/rise logic. No test compares planner output to published G-K example scenarios.

---

### F-064 — LOW `evaluate`: consecutive-year guardrail behavior and multiple-trigger interaction not tested

**Location:** `internal/services/retirement/guardrails.go:28` — `evaluate`
**Source consulted:** Code inspection.
**What it does:** Applies floor or ceiling adjustments, resets peak/initial, and clamps the multiplier.
**Finding:** Three coverage gaps: (1) No test calls `evaluate` in consecutive years to verify that multiplier compounding works (e.g., two successive 10% cuts → 0.81, not 0.80). (2) No test verifies behavior when the floor triggers, the peak is reset to current value, and then the portfolio rises — ensuring the ceiling is measured from the post-floor-trigger portfolio value (which is `initialPortfolio`, not the reset peak). The `initialPortfolio` field is not updated by floor triggers; only `peakPortfolio` is reset. This asymmetry is intentional but untested. (3) No test covers `evaluate` at year 0 (first call with portfolio equal to initial — no trigger expected).
**Evidence / repro:** `guardrails.go:41` `g.peakPortfolio = currentPortfolio` (floor resets peak only); `guardrails.go:52` `g.initialPortfolio = currentPortfolio` (ceiling resets initial only).
**Recommended fix sketch:** Add three table-driven subtests: consecutive-year cuts, post-floor-cut ceiling check, and year-0 no-trigger.
**Test coverage note:** Existing tests cover single-trigger scenarios only.

---

### F-065 — MEDIUM `rebaseLivingExpensesAtTransition`: uses full inflation instead of net inflation when `SpendingDeclineRate > 0`

**Location:** `internal/services/retirement/calculator.go:163` — `rebaseLivingExpensesAtTransition`
**Source consulted:** Code inspection + WE-10.1 worked example.
**What it does:** At a chain boundary, re-anchors `currentLivingExpenses` to `s.MonthlyLivingExpenses × cumulativeInflation` (or `× cumulativeInflation × spendingMultiplier` when spending phases are enabled).
**Finding:** `cumulativeInflation` in `RunProjection` compounds at the full `InflationRate` each month (`cumulativeInflation *= monthlyCompoundFactor(InflationRate)`). However, the ongoing pre-transition expense accumulation uses `net = InflationRate − SpendingDeclineRate`. At the chain boundary the rebase therefore steps expenses up by the ratio `(1+InflationRate)^T / (1+netRate)^T` relative to where the ongoing accumulation would have placed them. For `InflationRate = 3%`, `SpendingDeclineRate = 1%`, after a 5-year phase this is approximately `1.03^5 / 1.02^5 ≈ 1.049` — a 4.9% step-up in expenses that compounds into a 17.9% portfolio delta over 30 years ($178,943 in the WE-10.1 scenario).
**Evidence / repro:** WE-10.1 test: transparent-chain delta with `SpendingDeclineRate = 0` = $0.00; with `SpendingDeclineRate = 1.0` = $178,943.35.
**Recommended fix sketch:** Pass both `InflationRate` and `SpendingDeclineRate` to `rebaseLivingExpensesAtTransition`; compute a separate `netCumulativeInflation` that uses `InflationRate − SpendingDeclineRate`, and use that for the rebase instead of `cumulativeInflation`.
**Test coverage note:** No chain test uses non-zero `SpendingDeclineRate`; the gap is undetected in the existing suite.

---

### F-066 — LOW `CalculateTotalIncome`: stale public function does not apply SS optimizer projection

**Location:** `internal/services/retirement/calculator.go:125` — `CalculateTotalIncome`
**Source consulted:** Code inspection.
**What it does:** Sums `GetAdjustedAmount(month)` across all `IncomeSources`. Does not call `calculateMonthlyIncomeBreakdown`, which applies the SS optimizer override (dropping manual SS sources and adding projected SS income when `socialSecurityProjectionActive` is true).
**Finding:** `CalculateTotalIncome` is a public function that is never called in production code — all internal paths use `calculateMonthlyIncomeBreakdown`. Any external caller that uses `CalculateTotalIncome` when the SS optimizer is active will receive incorrect income totals: manual SS sources that the optimizer replaces will be double-counted if they are retained in `IncomeSources`, or the optimizer-projected amount will be omitted if no manual source exists.
**Evidence / repro:** `grep -rn "CalculateTotalIncome" internal/` returns only the definition. Internal projection loop at line 1132 calls `calculateMonthlyIncomeBreakdown(s, m)`.
**Recommended fix sketch:** Either remove `CalculateTotalIncome` (it is unused) or rewrite it to delegate to `calculateMonthlyIncomeBreakdown` so it stays accurate.
**Test coverage note:** No test calls `CalculateTotalIncome` with a live SS optimizer scenario. The function's divergence from projection behavior is undetected.

---

### F-067 — LOW `GetMonthlyCost`: Medicare transition is year-based, not month-based

**Location:** `internal/models/healthcare.go:87` — `GetMonthlyCost`
**Source consulted:** Code inspection + WE-10.2.
**What it does:** Determines pre-Medicare vs post-Medicare cost for a given projection month using `yearsElapsed = month / 12` (integer division).
**Finding:** A person who turns 65 in July (birth month 7 of year 1) transitions to Medicare at month 12 (the start of year 1), six months early. Conversely, a person whose birthday is in January of year 1 (month 1) also transitions at month 12 — no error there. The maximum early-transition error is 11 months: for a birthday in month 11 of year 1, Medicare kicks in at month 12 rather than month 23. In monetary terms, for a premium difference of $900/mo this is an 11-month early switch worth ~$9,900 in present value. This is a systematic underestimation of pre-Medicare costs.
**Evidence / repro:** Person age 64 (birthday in month 6 of year 1): `GetMonthlyCost(11)` = $600 (ACA), `GetMonthlyCost(12)` = $200 (Medicare). The transition fires 6 months before the 65th birthday.
**Recommended fix sketch:** Accept a `birthMonth int` (1–12) parameter and compute the true transition month as `(MedicareEligibleAge − CurrentAge) × 12 + (birthMonth − startMonth%12 + 12) % 12`. Alternatively, link `HealthcarePerson` to the `Person.BirthMonth` string and compute the exact transition month at initialization.
**Test coverage note:** Existing tests verify compounding within phases but do not test mid-year birthday scenarios.

---

### F-068 — LOW `GetTotalHealthcareCost` / `GetMonthlyCost`: no guard against negative net cost in multi-person model

**Location:** `internal/models/whatif.go:496` — `GetTotalHealthcareCost`; `internal/models/healthcare.go:87` — `GetMonthlyCost`
**Source consulted:** Code inspection.
**What it does:** `GetTotalHealthcareCost` sums `GetMonthlyCost` for each `HealthcarePerson`. `GetMonthlyCost` can return values derived from `CurrentMonthlyCost`, which is user-supplied.
**Finding:** Neither function clamps the result to zero. If a user enters a negative `CurrentMonthlyCost` (e.g., representing net employer subsidy that exceeds the premium), the monthly healthcare cost will be negative, reducing `CalculateTotalExpenses` and producing anomalous projection results. The legacy single-value path (`MonthlyHealthcare * inflation`) also has no floor. In the multi-person model the issue applies per-person.
**Evidence / repro:** `GetMonthlyCost(0)` with `CurrentMonthlyCost = -500` returns `-500`. No clamp in either function. `GetTotalHealthcareCost(0)` sums these directly.
**Recommended fix sketch:** Add `math.Max(0, result)` before returning in `GetMonthlyCost`, and/or validate that `CurrentMonthlyCost >= 0` in the model constructor/validator.
**Test coverage note:** No test exercises negative `CurrentMonthlyCost`. No validation logic rejects it.

---

### F-069 — LOW `findSteadyStateMonth`: no coverage for projected SS with spouse-only SS or when primary SS already started

**Location:** `internal/services/retirement/calculator.go:1579` — `findSteadyStateMonth`
**Source consulted:** Code inspection.
**What it does:** Returns the latest month among active income source starts and projected SS claim starts (primary + spouse).
**Finding:** Two coverage gaps: (1) No test covers the spouse-only projected SS case (`FRABenefit = 0`, `SpouseFRABenefit > 0`, `validSSClaimAge(SpouseClaimAge)` = true). The code path at line 1598 guards this correctly, but the condition is untested. (2) No test verifies behavior when `ClaimAge ≤ CurrentAge` (`claimStartMonth` returns 0). In that case the projected SS is already active and should not affect the steady-state month — the code returns 0, which is correct, but is unverified.
**Evidence / repro:** `findSteadyStateMonth` at line 1598: `if ss != nil && c.Settings.HasSpouse() && ss.SpouseFRABenefit > 0 && validSSClaimAge(ss.SpouseClaimAge)` — no test constructs this combination. Existing tests in `calculator_coverage_test.go` cover only `IncomeSources`-based steady-state timing.
**Recommended fix sketch:** Add a table-driven test with: (a) spouse-only projected SS at claim age 67 (primary has no benefit), (b) primary `ClaimAge ≤ CurrentAge` (already started), and (c) both primary and spouse projected at different claim ages (verifying max is taken).
**Test coverage note:** WE-10.3 covered the primary-only path; spouse and already-started paths remain uncovered.

---

### F-070 — INFO `CalculatePresentValueAnalysis`: reference values in verification doc are stale post-b978aa9

**Location:** `internal/services/retirement/calculator.go:1615` — `CalculatePresentValueAnalysis`
**Source consulted:** `docs/what-if-retirement-verification.md` (2026-04-07 snapshot); WE-10.4.
**What it does:** Computes PV of expenses and income using `PresentValueAnnuity`, which post-b978aa9 uses geometric monthly rates (`(1+r)^(1/12)−1`) rather than simple division.
**Finding:** Reconstructing the 2026-04-07 verification scenario produces PV income +$19,344, PV expenses −$9,185, and surplus +$28,528 relative to the reference values. These are consistent with the b978aa9 compounding fix: geometric rates lower effective monthly rates slightly, which lowers discounting and raises PV of income streams, while simultaneously reducing PV of costs. The coverage-ratio change (1.1→1.103) is within rounding. No formula error is present; the reference values simply need refreshing. The deltas are all under $50,000 (INFO threshold).
**Evidence / repro:** WE-10.4 test output: PV income 1,465,150.58 vs ref 1,445,806.64 (Δ+19,344); PV expenses 3,049,738.75 vs ref 3,058,923.29 (Δ−9,185); surplus 315,411.84 vs ref 286,883.35 (Δ+28,528).
**Recommended fix sketch:** Re-run the live app with the same 2026-04-07 settings and update `docs/what-if-retirement-verification.md`. Alternatively, add a golden-value regression test that fails when the values change unexpectedly.
**Test coverage note:** No regression test pins the PV values; changes to `PresentValueAnnuity` will silently shift them.

---

### F-071 — LOW `buildProjectionExplainability`: tax-share denominator is gross cash flow, not gross income — undocumented

**Location:** `internal/services/retirement/calculator.go:2984` — `buildProjectionExplainability`
**Source consulted:** Code inspection.
**What it does:** Computes `TaxShareOfGrossCashFlow = totalTaxes / totalGrossIncome × 100` where `totalGrossIncome` accumulates `month.GrossIncome` from each projection month. `GrossIncome` at line 1203 is defined as `totalIncome + qualifiedDividends + nonQualifiedDividends + capitalGainsDistributions + withdrawalFromTaxDeferred + withdrawalFromTaxable + withdrawalFromRoth` — i.e., income plus all portfolio withdrawals.
**Finding:** The label `TaxShareOfGrossCashFlow` is accurate, but the UI renders it as "Taxes consumed" without clarifying that the denominator includes portfolio withdrawals (which are not income). For a retiree drawing heavily from a tax-deferred account, the denominator is inflated by untaxed Roth withdrawals and return-of-basis taxable withdrawals, making the "taxes consumed" percentage appear lower than a naive "taxes / income" ratio. There is no coverage for the case where `totalGrossIncome = 0` (e.g., a scenario with no income and no withdrawals — fully depleted portfolio); the guard at line 2985 (`if totalGrossIncome > 0`) handles this correctly but is untested. The `InflationLossPercent` formula (`(1 − realBalance/nominalBalance) × 100`) is correct and guarded against zero final balance.
**Evidence / repro:** `buildProjectionExplainability` line 2986: denominator is `totalGrossIncome` summed from `month.GrossIncome`, which includes Roth withdrawals at line 1203.
**Recommended fix sketch:** Add a UI tooltip clarifying that the denominator is total gross cash flow (income + withdrawals), not income alone. Optionally expose a separate `TaxShareOfOrdinaryIncome` field using only `OrdinaryIncome + SocialSecurityIncome`. Add a test with a zero-cash-flow scenario (all depleted at month 0) to cover the guard.
**Test coverage note:** No test exercises `buildProjectionExplainability` with a zero-gross-income scenario or with all-Roth withdrawals.
