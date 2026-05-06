# What-If Math Audit

**Date:** 2026-05-05
**Codebase audited at commit:** `3ec6440`
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
| `GetAdjustedStandardDeduction` | `tax.go:238` | F-001 (MEDIUM), F-006 (LOW), F-016 (LOW) |
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
| Delta | $0.00 ✓ |

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
| Delta | $0.00 ✓ |

### Findings

#### F-001 — MEDIUM Missing age-65+ additional standard deduction

**Location:** `internal/services/retirement/tax.go:238` — `GetAdjustedStandardDeduction`

**Source consulted:** IRS Rev. Proc. 2023-34 §3.16 (additional standard deduction for age 65 or older and/or blind).

**What it does:** Returns the base 2024 standard deduction for the filing status, inflated forward. No age-based adjustment is applied.

**Finding:** Rev. Proc. 2023-34 §3.16 provides an additional standard deduction for taxpayers who are 65 or older: $1,550 for Single or Head of Household filers, and $1,300 per qualifying spouse for MFJ filers (i.e., $2,600 if both spouses are 65+). Since this planner targets retirees who are typically 65 or older, the base deduction is likely understated by $1,300–$2,600 for most users. This causes over-taxation of ordinary income.

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
A 65+ Single filer at $80,000 gross income would have actual taxable income of $65,400 − $1,550 = $63,850 (not $65,400), yielding true federal tax of $9,100 vs the code's $9,441, a $341 over-estimate (3.6% error on tax owed).

**Recommended fix sketch:** Add an `Age65Count int` field (0, 1, or 2) to `TaxCalculator` and a `StandardDeduction2024Additional` constant map keyed on filing status; sum the base deduction with `Age65Count * additional` before inflating.

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

**Location:** `internal/services/retirement/tax.go:239` — `GetAdjustedStandardDeduction`

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

**Finding:** `TestCalculateFederalTax` constructs a MFJ calculator and only checks that tax falls within loose bounds. No test uses Single, MFJ, MFS, or HoH constructors independently; no test passes `yearsFromBase > 0` to verify bracket inflation; no test passes a negative income value (the function returns zero for `grossIncome <= 0` per the guard, but this is not asserted).

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

### F-001 — MEDIUM Missing age-65+ additional standard deduction

**Location:** `internal/services/retirement/tax.go:238` — `GetAdjustedStandardDeduction`

**Source consulted:** IRS Rev. Proc. 2023-34 §3.16 (additional standard deduction for age 65 or older and/or blind).

**What it does:** Returns the base 2024 standard deduction for the filing status, inflated forward. No age-based adjustment is applied.

**Finding:** Rev. Proc. 2023-34 §3.16 provides an additional standard deduction for taxpayers who are 65 or older: $1,550 for Single or Head of Household filers, and $1,300 per qualifying spouse for MFJ filers (i.e., $2,600 if both spouses are 65+). Since this planner targets retirees who are typically 65 or older, the base deduction is likely understated by $1,300–$2,600 for most users. This causes over-taxation of ordinary income.

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
A 65+ Single filer at $80,000 gross income would have actual taxable income of $65,400 − $1,550 = $63,850 (not $65,400), yielding true federal tax of $9,100 vs the code's $9,441, a $341 over-estimate (3.6% error on tax owed).

**Recommended fix sketch:** Add an `Age65Count int` field (0, 1, or 2) to `TaxCalculator` and a `StandardDeduction2024Additional` constant map keyed on filing status; sum the base deduction with `Age65Count * additional` before inflating.

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

**Location:** `internal/services/retirement/tax.go:239` — `GetAdjustedStandardDeduction`

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

**Finding:** `TestCalculateFederalTax` constructs a MFJ calculator and only checks that tax falls within loose bounds. No test uses Single, MFJ, MFS, or HoH constructors independently; no test passes `yearsFromBase > 0` to verify bracket inflation; no test passes a negative income value (the function returns zero for `grossIncome <= 0` per the guard, but this is not asserted).

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
