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

**Test coverage note:** The test covers year=0, year=2, and year=5. The guard fires correctly for year=2. No test passes a fractional value within epsilon of 2.0.
