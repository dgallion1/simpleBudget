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
| `CalculateFederalTax` | `tax.go:349` | PASS |
| `calculateFederalTaxOnTaxableIncome` | `tax.go:372` | PASS |
| `CalculateStateTax` | `tax.go:396` | PASS (INFO-01) |
| `CalculateTotalTax` | `tax.go:404` | PASS (INFO-02) |
| `CalculateTaxWithInvestmentIncome` | `tax.go:421` | PASS |
| `calculateTaxWithInvestmentIncomeInternal` | `tax.go:433` | PASS |
| `CalculateTaxWithInvestmentIncomeBreakdown` | `tax.go:429` | PASS |
| `EstimateRothConversionTax` | `tax.go:484` | PASS |
| `GetMarginalRate` | `tax.go:499` | PASS |
| `GetAdjustedBrackets` | `tax.go:181` | PASS (INFO-03) |
| `GetAdjustedLongTermCapitalGainsBrackets` | `tax.go:210` | PASS |
| `GetAdjustedStandardDeduction` | `tax.go:238` | F-001 (MEDIUM), LOW-01 |
| `inflationFactor` | `tax.go:251` | PASS (INFO-04) |
| `normalizeFilingStatus` | `tax.go:258` | PASS |

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

#### WE-1.2: MFJ, $200,000 ordinary + $20,000 LTCG, year-0

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

#### INFO-01 — State tax is a single flat rate

**Location:** `tax.go:396` — `CalculateStateTax`

**What it does:** Applies a single flat percentage to taxable income. No progressive state brackets, no state standard deduction or exemptions.

**Finding:** This is a known simplification. Most states use progressive brackets, personal exemptions, and/or differ in their treatment of retirement income (pension exclusions, SS exemptions). The simplification is acceptable for a high-level planner but may over- or under-estimate state tax by a meaningful margin for users in progressive-bracket states. No code bug — informational only.

---

#### INFO-02 — `CalculateTotalTax` uses federal standard deduction for state taxable income

**Location:** `tax.go:404` — `CalculateTotalTax`

**What it does:** Derives state taxable income as `grossIncome − federalStandardDeduction`.

**Finding:** Most states have their own standard deductions (or exemptions) that differ from the federal amount. Applying the federal standard deduction to the state base is a reasonable approximation but may diverge from actual state liability. The inline comment in the code acknowledges this: "Simplified: apply to same taxable income base." Not a bug — informational.

---

#### INFO-03 — Inflation projection uses pure compound growth; IRS uses chained CPI rounded to nearest $50

**Location:** `tax.go:181` — `GetAdjustedBrackets`; `tax.go:251` — `inflationFactor`

**What it does:** Multiplies all bracket edges by `(1 + inflationRate/100)^years`, producing continuous compounding without rounding.

**Finding:** The IRS adjusts brackets annually using chained CPI-U-RS and rounds to the nearest $50 (for ordinary income brackets) or $50 (for LTCG brackets) per Rev. Proc. 2023-34 §3.01. Pure compounding with user-supplied inflation rate will diverge from actual IRS-published values as years increase. This is appropriate for a projection tool (the actual IRS values are unknown for future years), but users should understand the projection is a smooth approximation, not a simulation of IRS rounding. Not a bug — informational.

---

#### LOW-01 — Dead fallback branch in `GetAdjustedStandardDeduction`

**Location:** `tax.go:239` — `GetAdjustedStandardDeduction`

**What it does:** If `baseDeduction == 0` (i.e., filing status not found in map), falls back to `StandardDeduction2024[models.FilingMarriedJoint]`.

**Finding:** `StandardDeduction2024` populates all four valid filing status keys. `normalizeFilingStatus` is not called before the map lookup in this function (unlike in several other functions), so an unknown filing status would yield 0 and trigger the fallback. However, `NewTaxCalculator` accepts any `models.FilingStatus` without normalizing it — meaning an invalid status passed at construction time would cause `GetAdjustedStandardDeduction` to silently fall back to MFJ values while other functions (which do call `normalizeFilingStatus`) would also use MFJ. The inconsistency is harmless in practice but worth noting. No user-visible math error in the normal usage path.

**Recommended fix sketch:** Call `normalizeFilingStatus(tc.FilingStatus)` consistently at the top of each method that accesses a map by filing status, or normalize once in `NewTaxCalculator`.

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
