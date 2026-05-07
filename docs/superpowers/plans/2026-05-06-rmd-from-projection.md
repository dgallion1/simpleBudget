# F-072 RMD from Projection — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the standalone `CalculateRMDAnalysis` with `BuildRMDAnalysis(projection)` so the RMD card reflects the actual tax-deferred bucket trajectory from `RunProjection`, eliminating the contradiction where the panel shows balances compounding for years while the projection has already depleted the portfolio.

**Architecture:** `RunFullAnalysis` first runs `RunProjection`, then passes the result into `BuildRMDAnalysis(projection)`. The new function reads each January's `TaxDeferredBalance` and the year's summed `RMDWithdrawal` from `projection.Months[]`. Depletion before RMD age suppresses the table with a banner; depletion during RMD years truncates the table with a footer note. The old isolated math is removed.

**Tech Stack:** Go 1.x, html/template, Tailwind. No new dependencies.

**Spec:** [`docs/superpowers/specs/2026-05-06-rmd-from-projection-design.md`](../specs/2026-05-06-rmd-from-projection-design.md)

---

## Task 1: Extend `RMDAnalysis` model with depletion fields

**Files:**
- Modify: `internal/models/whatif.go` (RMDAnalysis struct, ~line 1325)

- [ ] **Step 1: Locate the existing `RMDAnalysis` struct definition.**

Run: `grep -n "type RMDAnalysis struct" internal/models/whatif.go`
Expected: a single line number. The struct is around line 1325.

- [ ] **Step 2: Add three new fields after `TotalRMDsOver10Yr`.**

Modify the struct so the final layout is:

```go
type RMDAnalysis struct {
	StartsInYears     int             `json:"starts_in_years"`
	StartAge          int             `json:"start_age"`
	CurrentAge        int             `json:"current_age"`
	TaxDeferredValue  float64         `json:"tax_deferred_value"`
	Projections       []RMDProjection `json:"projections"`
	TotalRMDsOver10Yr float64         `json:"total_rmds_over_10yr"`

	// F-072: depletion context driven by the actual projection.
	DepletionYear     *int `json:"depletion_year,omitempty"`     // year index of portfolio depletion; nil if survives
	DepletionAge      *int `json:"depletion_age,omitempty"`      // older-person age at depletion year
	DepletedBeforeRMD bool `json:"depleted_before_rmd"`          // true when depletion precedes the first RMD year
}
```

If the existing struct has additional fields not shown above, preserve them exactly — only the three new fields below `TotalRMDsOver10Yr` are added.

- [ ] **Step 3: Verify the package builds.**

Run: `go vet ./internal/models/...`
Expected: no output (success).

- [ ] **Step 4: Run the full test suite.**

Run: `go test ./...`
Expected: all packages pass. New fields are additive and zero-valued by default; nothing else changes yet.

- [ ] **Step 5: Commit.**

```bash
git add internal/models/whatif.go
git commit -m "$(cat <<'EOF'
feat(whatif): F-072 add depletion fields to RMDAnalysis

Adds DepletionYear, DepletionAge, DepletedBeforeRMD to RMDAnalysis
so the RMD panel can render depletion-aware banners and footers.
Population of these fields lands in a follow-up commit when
BuildRMDAnalysis replaces CalculateRMDAnalysis.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Implement `BuildRMDAnalysis` with TDD coverage

**Files:**
- Modify: `internal/services/retirement/rmd.go`
- Create: `internal/services/retirement/rmd_test.go`

This task introduces the new function alongside the old one (which still has callers — they get switched in Task 3). Tests drive the implementation.

- [ ] **Step 1: Add a minimal stub for `BuildRMDAnalysis` at the bottom of `rmd.go`.**

Append below the existing `CalculateRMDAnalysis`:

```go
// BuildRMDAnalysis (F-072) builds the RMD analysis from the actual projection
// instead of an isolated standalone math model. Stub for TDD; real
// implementation lands in Step 5.
func (c *Calculator) BuildRMDAnalysis(projection *models.ProjectionResult) *models.RMDAnalysis {
	return &models.RMDAnalysis{
		Projections: []models.RMDProjection{},
	}
}
```

This deliberately returns an empty result so the new tests can compile and be observed to fail.

- [ ] **Step 2: Verify the package compiles.**

Run: `go vet ./internal/services/retirement/...`
Expected: no output.

- [ ] **Step 3: Create the unit-test file with all seven cases.**

Create `internal/services/retirement/rmd_test.go`:

```go
package retirement

import (
	"testing"

	"budget2/internal/models"
)

// fixtureProjection builds a *models.ProjectionResult inline for unit tests
// of BuildRMDAnalysis. Months[i].TaxDeferredBalance is set to taxDeferredFn(i),
// Months[i].RMDWithdrawal to rmdFn(i). depletionMonth is honored when non-nil.
func fixtureProjection(months int, taxDeferredFn func(m int) float64, rmdFn func(m int) float64, depletionMonth *int) *models.ProjectionResult {
	out := &models.ProjectionResult{
		Months:         make([]models.ProjectionMonth, months),
		DepletionMonth: depletionMonth,
		Survives:       depletionMonth == nil,
	}
	for m := 0; m < months; m++ {
		td := 0.0
		rmd := 0.0
		if taxDeferredFn != nil {
			td = taxDeferredFn(m)
		}
		if rmdFn != nil {
			rmd = rmdFn(m)
		}
		out.Months[m] = models.ProjectionMonth{
			Month:              m,
			Year:               float64(m) / 12,
			TaxDeferredBalance: td,
		}
		out.Months[m].RMDWithdrawal = rmd
	}
	return out
}

func intPtr(v int) *int { return &v }

func newCalcF072(currentAge, spouseAge int, portfolio, tdPercent float64, projYears int, startDate string) *Calculator {
	s := &models.WhatIfSettings{
		CurrentAge:         currentAge,
		SpouseAge:          spouseAge,
		PortfolioValue:     portfolio,
		TaxDeferredPercent: tdPercent,
		ProjectionYears:    projYears,
		StartDate:          startDate,
	}
	return NewCalculator(s)
}

// 1. Depletion before first RMD year → empty Projections, DepletedBeforeRMD true.
func TestBuildRMDAnalysis_F072_DepletionBeforeRMD(t *testing.T) {
	calc := newCalcF072(60, 0, 100_000, 60, 30, "2026-01")
	depletion := 24 // month 24 = year 2
	proj := fixtureProjection(360, func(m int) float64 { return 1.0 }, nil, &depletion)

	analysis := calc.BuildRMDAnalysis(proj)

	if !analysis.DepletedBeforeRMD {
		t.Errorf("DepletedBeforeRMD = false; want true")
	}
	if len(analysis.Projections) != 0 {
		t.Errorf("len(Projections) = %d; want 0", len(analysis.Projections))
	}
	if analysis.TotalRMDsOver10Yr != 0 {
		t.Errorf("TotalRMDsOver10Yr = %.2f; want 0", analysis.TotalRMDsOver10Yr)
	}
	if analysis.DepletionYear == nil || *analysis.DepletionYear != 2 {
		t.Errorf("DepletionYear = %v; want 2", analysis.DepletionYear)
	}
	if analysis.DepletionAge == nil || *analysis.DepletionAge != 62 {
		t.Errorf("DepletionAge = %v; want 62", analysis.DepletionAge)
	}
	if analysis.StartsInYears != 13 {
		t.Errorf("StartsInYears = %d; want 13", analysis.StartsInYears)
	}
}

// 2. Depletion during RMD years → only pre-depletion rows emitted.
func TestBuildRMDAnalysis_F072_DepletionDuringRMD(t *testing.T) {
	// olderAge=70, startAge=73, depletion at month 12*15 = year 15
	// → RMD years 13, 14 (2 rows); year 15 hits depletion break.
	calc := newCalcF072(70, 0, 100_000, 60, 30, "2026-01")
	depletion := 12 * 15
	proj := fixtureProjection(360,
		func(m int) float64 { return 60_000 - float64(m)*100 }, // balance trends down
		func(m int) float64 {
			// 1000/mo only during RMD years before depletion
			y := m / 12
			if y >= 13 && y < 15 {
				return 1000
			}
			return 0
		},
		&depletion)

	analysis := calc.BuildRMDAnalysis(proj)

	if analysis.DepletedBeforeRMD {
		t.Errorf("DepletedBeforeRMD = true; want false")
	}
	if len(analysis.Projections) != 2 {
		t.Errorf("len(Projections) = %d; want 2", len(analysis.Projections))
	}
	if analysis.DepletionYear == nil || *analysis.DepletionYear != 15 {
		t.Errorf("DepletionYear = %v; want 15", analysis.DepletionYear)
	}
	wantTotal := 12 * 1000.0 * 2 // 2 years × 12 months × 1000
	if analysis.TotalRMDsOver10Yr != wantTotal {
		t.Errorf("TotalRMDsOver10Yr = %.2f; want %.2f", analysis.TotalRMDsOver10Yr, wantTotal)
	}
	if analysis.Projections[0].Age != 73 {
		t.Errorf("first row age = %d; want 73", analysis.Projections[0].Age)
	}
	if analysis.Projections[1].Age != 74 {
		t.Errorf("second row age = %d; want 74", analysis.Projections[1].Age)
	}
}

// 3. Surviving 30-year projection emits exactly 10 RMD rows in TotalRMDsOver10Yr.
func TestBuildRMDAnalysis_F072_FullTenYears_NoDepletion(t *testing.T) {
	// olderAge=72, startAge=73, projection survives full 30 years.
	// Year 1..29 are RMD years; expect 20 rows (rmdCount cap), 10 in 10-yr total.
	calc := newCalcF072(72, 0, 100_000, 60, 30, "2026-01")
	proj := fixtureProjection(360,
		func(m int) float64 { return 60_000 },
		func(m int) float64 {
			y := m / 12
			if y >= 1 { // RMD starts at year 1 (age 73)
				return 200 // 200/mo => 2400/yr
			}
			return 0
		},
		nil)

	analysis := calc.BuildRMDAnalysis(proj)

	if analysis.DepletedBeforeRMD {
		t.Errorf("DepletedBeforeRMD = true; want false")
	}
	// Years 1..20 = 20 rows (rmdCount cap).
	if len(analysis.Projections) != 20 {
		t.Errorf("len(Projections) = %d; want 20", len(analysis.Projections))
	}
	wantTotal := 2400.0 * 10
	if analysis.TotalRMDsOver10Yr != wantTotal {
		t.Errorf("TotalRMDsOver10Yr = %.2f; want %.2f", analysis.TotalRMDsOver10Yr, wantTotal)
	}
	if analysis.Projections[0].Age != 73 {
		t.Errorf("first row age = %d; want 73", analysis.Projections[0].Age)
	}
	if analysis.Projections[0].RMDAmount != 2400 {
		t.Errorf("first row RMDAmount = %.2f; want 2400", analysis.Projections[0].RMDAmount)
	}
}

// 4. TaxDeferredPercent = 0 → empty projections, no panic.
func TestBuildRMDAnalysis_F072_TaxDeferredPercentZero(t *testing.T) {
	calc := newCalcF072(72, 0, 100_000, 0, 30, "2026-01")
	proj := fixtureProjection(360, nil, nil, nil)

	analysis := calc.BuildRMDAnalysis(proj)

	if analysis.TaxDeferredValue != 0 {
		t.Errorf("TaxDeferredValue = %.2f; want 0", analysis.TaxDeferredValue)
	}
	if len(analysis.Projections) == 0 {
		// Acceptable: no RMDs to report. But TotalRMDsOver10Yr must be 0.
		if analysis.TotalRMDsOver10Yr != 0 {
			t.Errorf("TotalRMDsOver10Yr = %.2f; want 0", analysis.TotalRMDsOver10Yr)
		}
	}
}

// 5. SECURE 2.0: start year >= 2033 → start age 75.
func TestBuildRMDAnalysis_F072_StartAge75_Secure20(t *testing.T) {
	// olderAge=72, projection start 2034 → effectiveStartAge=75.
	// Expect first emitted row at age 75 (year 3).
	calc := newCalcF072(72, 0, 100_000, 60, 30, "2034-01")
	proj := fixtureProjection(360,
		func(m int) float64 { return 60_000 },
		func(m int) float64 {
			if m/12 >= 3 {
				return 100
			}
			return 0
		},
		nil)

	analysis := calc.BuildRMDAnalysis(proj)

	if analysis.StartAge != 75 {
		t.Errorf("StartAge = %d; want 75", analysis.StartAge)
	}
	if len(analysis.Projections) == 0 {
		t.Fatal("expected projections under SECURE 2.0; got none")
	}
	if analysis.Projections[0].Age != 75 {
		t.Errorf("first row age = %d; want 75", analysis.Projections[0].Age)
	}
}

// 6. olderAge already at RMD age → first row at year 0 uses Months[0] balance.
func TestBuildRMDAnalysis_F072_AlreadyAtRMDAge(t *testing.T) {
	calc := newCalcF072(75, 0, 100_000, 80, 20, "2026-01")
	proj := fixtureProjection(240,
		func(m int) float64 {
			if m == 0 {
				return 80_000 // start-of-year balance
			}
			return 70_000
		},
		func(m int) float64 {
			if m < 12 {
				return 300
			}
			return 0
		},
		nil)

	analysis := calc.BuildRMDAnalysis(proj)

	if analysis.StartsInYears != 0 {
		t.Errorf("StartsInYears = %d; want 0", analysis.StartsInYears)
	}
	if len(analysis.Projections) == 0 {
		t.Fatal("expected at least one projection row")
	}
	if analysis.Projections[0].Age != 75 {
		t.Errorf("first row age = %d; want 75", analysis.Projections[0].Age)
	}
	// Year 0 should sample Months[0].TaxDeferredBalance == 80000.
	if analysis.Projections[0].TaxDeferredBal != 80_000 {
		t.Errorf("year-0 TaxDeferredBal = %.2f; want 80000", analysis.Projections[0].TaxDeferredBal)
	}
	wantRMD := 12 * 300.0
	if analysis.Projections[0].RMDAmount != wantRMD {
		t.Errorf("year-0 RMDAmount = %.2f; want %.2f", analysis.Projections[0].RMDAmount, wantRMD)
	}
}

// 7. RMDPercent reports IRS table value, not realized percent.
func TestBuildRMDAnalysis_F072_RMDPercentIsTableValue(t *testing.T) {
	// olderAge=73, balance 100k, but actual RMD is only 1000 (well below table %).
	// Table for age 73 → factor 26.5 → percent = 100/26.5 ≈ 3.7736.
	calc := newCalcF072(73, 0, 100_000, 60, 5, "2026-01")
	proj := fixtureProjection(60,
		func(m int) float64 { return 60_000 },
		func(m int) float64 {
			if m < 12 {
				return 1000.0 / 12 // 1000 total for the year
			}
			return 0
		},
		nil)

	analysis := calc.BuildRMDAnalysis(proj)

	if len(analysis.Projections) == 0 {
		t.Fatal("expected projections")
	}
	wantPercent := 100.0 / 26.5
	if got := analysis.Projections[0].RMDPercent; (got-wantPercent) > 0.001 || (wantPercent-got) > 0.001 {
		t.Errorf("RMDPercent = %.4f; want %.4f (table value, not realized)", got, wantPercent)
	}
	// Sanity: realized RMD is much smaller than table% × balance.
	if analysis.Projections[0].RMDAmount > 1001 {
		t.Errorf("RMDAmount = %.2f; want ~1000 (the fixture value)", analysis.Projections[0].RMDAmount)
	}
}
```

- [ ] **Step 4: Run the tests against the stub; expect failures (TDD red).**

Run: `go test ./internal/services/retirement/ -run TestBuildRMDAnalysis_F072 -v`
Expected: tests #1, #2, #3, #5, #6, #7 FAIL because the stub returns an empty result. Test #4 (TaxDeferredPercentZero) may pass since the stub coincidentally returns zero values. This is the TDD "red" stage — the failures prove the tests are real.

If tests pass when they shouldn't, the test assertions are not strict enough — fix the test before continuing.

- [ ] **Step 5: Replace the stub with the real implementation.**

In `rmd.go`, replace the stub `BuildRMDAnalysis` body with the production implementation:

```go
// BuildRMDAnalysis (F-072) builds the RMD analysis from the actual projection
// instead of an isolated standalone math model. It samples each RMD year's
// starting tax-deferred balance and sums the actual RMDWithdrawal over the
// year, so the panel cannot diverge from the main projection.
//
// Replaces CalculateRMDAnalysis. Old function is removed in the follow-up
// commit (Task 3).
func (c *Calculator) BuildRMDAnalysis(projection *models.ProjectionResult) *models.RMDAnalysis {
	s := c.Settings

	taxDeferredValue := s.PortfolioValue * (s.TaxDeferredPercent / 100)
	effectiveStartAge := EffectiveRMDStartAge(s)
	olderAge := s.GetOlderAge()
	startsInYears := effectiveStartAge - olderAge
	if startsInYears < 0 {
		startsInYears = 0
	}

	result := &models.RMDAnalysis{
		StartsInYears:    startsInYears,
		StartAge:         effectiveStartAge,
		CurrentAge:       olderAge,
		TaxDeferredValue: taxDeferredValue,
		Projections:      []models.RMDProjection{},
	}

	if projection == nil || len(projection.Months) == 0 {
		return result
	}

	// Surface depletion year (whole years from start; floor of months/12).
	if projection.DepletionMonth != nil {
		dy := *projection.DepletionMonth / 12
		da := olderAge + dy
		result.DepletionYear = &dy
		result.DepletionAge = &da
		if dy < startsInYears {
			result.DepletedBeforeRMD = true
			return result
		}
	}

	// Iterate projection years, emit a row when age >= effectiveStartAge.
	maxYears := s.ProjectionYears
	if maxYears > len(projection.Months)/12 {
		maxYears = len(projection.Months) / 12
	}

	rmdCount := 0
	for y := 0; y <= maxYears && rmdCount < 20; y++ {
		age := olderAge + y
		if age < effectiveStartAge {
			continue
		}
		// Stop at depletion year — no further rows.
		if result.DepletionYear != nil && y >= *result.DepletionYear {
			break
		}

		// Start-of-year tax-deferred balance.
		startIdx := 12*y - 1
		if y == 0 {
			startIdx = 0
		}
		if startIdx >= len(projection.Months) {
			break
		}
		startBalance := projection.Months[startIdx].TaxDeferredBalance

		// Sum actual RMDWithdrawal across the 12 months of this year.
		startMonth := 12 * y
		endMonth := startMonth + 12
		if endMonth > len(projection.Months) {
			endMonth = len(projection.Months)
		}
		rmdAmount := 0.0
		for m := startMonth; m < endMonth; m++ {
			rmdAmount += projection.Months[m].RMDWithdrawal
		}

		factor := GetLifeExpectancyFactor(age)
		rmdPercent := 0.0
		if factor > 0 {
			rmdPercent = 100.0 / factor
		}

		result.Projections = append(result.Projections, models.RMDProjection{
			Age:            age,
			Year:           y,
			TaxDeferredBal: startBalance,
			LifeExpFactor:  factor,
			RMDAmount:      rmdAmount,
			RMDPercent:     rmdPercent,
		})

		if rmdCount < 10 {
			result.TotalRMDsOver10Yr += rmdAmount
		}
		rmdCount++
	}

	return result
}
```

- [ ] **Step 6: Run tests; expect all to pass (TDD green).**

Run: `go test ./internal/services/retirement/ -run TestBuildRMDAnalysis_F072 -v`
Expected: 7 PASS lines, no FAIL.

If any test still fails, debug the implementation against the failing assertions. Do not weaken the tests.

- [ ] **Step 7: Run the full retirement package suite.**

Run: `go test ./internal/services/retirement/ -v`
Expected: all existing tests still pass. The new function is additive; old `CalculateRMDAnalysis` is untouched until Task 3.

- [ ] **Step 8: Commit.**

```bash
git add internal/services/retirement/rmd.go internal/services/retirement/rmd_test.go
git commit -m "$(cat <<'EOF'
feat(whatif): F-072 BuildRMDAnalysis driven by actual projection

Adds BuildRMDAnalysis(projection) which reads each RMD year's actual
TaxDeferredBalance and RMDWithdrawal from the projection rather than
re-deriving balances from a standalone math model. Old
CalculateRMDAnalysis is retained until callers are switched in the
follow-up commit.

Includes 7 unit tests covering: depletion before/during RMD years,
no-depletion 10+ year case, zero tax-deferred percent, SECURE 2.0
age-75 transition, already-at-RMD-age, and IRS-table RMD percent.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Wire `BuildRMDAnalysis` into `RunFullAnalysis`; remove old code

**Files:**
- Modify: `internal/services/retirement/calculator.go` (line ~3068)
- Modify: `internal/services/retirement/rmd.go` (delete `CalculateRMDAnalysis`, delete `rmdGrowthFractions`)
- Modify: `internal/services/retirement/rmd_tax_test.go` (delete legacy tests; see Task 4)

This task changes the production call site and removes dead code. Tests in `rmd_tax_test.go` that exercise the deleted code must be removed in the same commit; that's why Task 4 is sequenced before final cleanup but bundled into this commit's diff via the file deletions noted below.

- [ ] **Step 1: Update the call site in `RunFullAnalysis`.**

Find: `internal/services/retirement/calculator.go:3068`

Change:

```go
	rmd := c.CalculateRMDAnalysis()
```

To:

```go
	rmd := c.BuildRMDAnalysis(projection)
```

`projection` is already in scope on line 3060.

- [ ] **Step 2: Remove `CalculateRMDAnalysis` and `rmdGrowthFractions` from `rmd.go`.**

Delete the entire `CalculateRMDAnalysis` function (currently `internal/services/retirement/rmd.go:131-214`). Also delete `rmdGrowthFractions` (currently lines 118-129) and its `RMDTimingStartOfYear` / `RMDTimingEndOfYear` switch — no other call sites in the package.

Verify with: `grep -rn "CalculateRMDAnalysis\|rmdGrowthFractions" internal/`
Expected: zero matches in non-test files.

- [ ] **Step 3: Delete legacy tests that referenced removed code.**

In `internal/services/retirement/rmd_tax_test.go`, delete these test functions entirely (they exercise the removed `CalculateRMDAnalysis` math):

- `TestCalculateRMDAnalysis` (currently lines 54-201)
- `TestCalculateRMDAnalysis_F035_TimingStartOfYear`
- `TestCalculateRMDAnalysis_F035_TimingMidYearIsDefault`
- `TestCalculateRMDAnalysis_F035_TimingEndOfYear`
- `TestCalculateRMDAnalysis_F035_StartOfYearLargestBalance`
- The `buildF035Settings` helper if defined in this file (verify with grep)

Do NOT delete:

- `TestEffectiveRMDStartAge_F032_*` (tests the still-present `EffectiveRMDStartAge`)
- `TestNormalizeRMDTiming_F035` (tests `models.NormalizeRMDTiming`, still in use by `RunProjection`)
- `TestCalculateStateTax`, `TestCalculateTotalTax`, `TestRunProjectionDeductsTaxesFromRMDCashFlow` (unrelated)

Run: `grep -n "buildF035Settings" internal/services/retirement/rmd_tax_test.go`
If matches remain after deletion, also delete the helper.

- [ ] **Step 4: Verify the package builds and tests pass.**

Run: `go test ./internal/services/retirement/ -v`
Expected: all tests pass. The 5 deleted tests are gone; the 7 new `TestBuildRMDAnalysis_F072_*` tests still pass; F-032 and NormalizeRMDTiming tests still pass.

If a test fails because it references `CalculateRMDAnalysis`, delete or migrate it to `BuildRMDAnalysis(c.RunProjection())` — depending on whether it's exercising removed math or genuinely testing end-to-end behavior.

- [ ] **Step 5: Run the full repository suite.**

Run: `go test ./...`
Expected: all packages pass.

- [ ] **Step 6: Commit.**

```bash
git add internal/services/retirement/calculator.go internal/services/retirement/rmd.go internal/services/retirement/rmd_tax_test.go
git commit -m "$(cat <<'EOF'
refactor(whatif): F-072 switch RunFullAnalysis to BuildRMDAnalysis

Wires BuildRMDAnalysis(projection) into RunFullAnalysis, deleting
the old isolated-math CalculateRMDAnalysis and the now-unused
rmdGrowthFractions helper. F-035 timing is still applied inside
RunProjection, so the RMD card now reflects whatever RMD the
projection actually withdrew.

Removes legacy tests that exercised the deleted standalone math.
F-032 EffectiveRMDStartAge and NormalizeRMDTiming tests are kept
since their targets remain in use.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Add integration regression tests

**Files:**
- Modify: `internal/services/retirement/calculator_test.go` (add two tests at the bottom)

These tests run the real `RunFullAnalysis` on settings, asserting the structural invariant that `Projections[i].RMDAmount` matches the sum of `RMDWithdrawal` from the corresponding months. This is the regression bar for the user-reported bug.

- [ ] **Step 1: Append the two integration tests.**

Append to `internal/services/retirement/calculator_test.go`:

```go
// TestRunFullAnalysis_F072_DepletedBeforeRMD_NoRMDRows is the regression test
// for the user-visible bug: when the projection depletes the portfolio before
// RMD age, the RMD panel must report zero rows, not idealized compounding.
func TestRunFullAnalysis_F072_DepletedBeforeRMD_NoRMDRows(t *testing.T) {
	// Use CurrentAge=65 (the default from DefaultWhatIfSettings) so we don't
	// fight the Persons[]/CurrentAge derivation. RMD start at 73 → 8yr cushion;
	// $5K portfolio against $5K/mo spending depletes month 1, far before RMD.
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 5_000   // tiny vs. expenses below
	s.TaxDeferredPercent = 100 // all in tax-deferred so RMD bucket = portfolio
	s.MonthlyLivingExpenses = 5_000
	s.MonthlyHealthcare = 0
	s.ProjectionYears = 30
	s.SocialSecurity = nil // no income to cushion

	calc := NewCalculator(s)
	analysis := calc.RunFullAnalysis()

	if analysis.RMD == nil {
		t.Fatal("analysis.RMD is nil")
	}
	if analysis.Projection == nil || analysis.Projection.DepletionMonth == nil {
		t.Fatal("expected the main projection to deplete; got survival")
	}
	if !analysis.RMD.DepletedBeforeRMD {
		t.Errorf("DepletedBeforeRMD = false; expected true (projection should deplete before age 73)")
	}
	if len(analysis.RMD.Projections) != 0 {
		t.Errorf("len(RMD.Projections) = %d; expected 0 when depleted before RMD",
			len(analysis.RMD.Projections))
	}
	if analysis.RMD.TotalRMDsOver10Yr != 0 {
		t.Errorf("TotalRMDsOver10Yr = %.2f; expected 0 when depleted before RMD",
			analysis.RMD.TotalRMDsOver10Yr)
	}
}

// TestRunFullAnalysis_F072_RMDMatchesProjection enforces the structural
// invariant: each emitted RMD row's amount equals the sum of RMDWithdrawal
// across that year's months in the main projection.
func TestRunFullAnalysis_F072_RMDMatchesProjection(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 1_500_000
	s.TaxDeferredPercent = 60
	s.RothPercent = 10
	s.InvestmentReturn = 5.0
	s.ProjectionYears = 30
	s.MonthlyLivingExpenses = 4_000

	calc := NewCalculator(s)
	analysis := calc.RunFullAnalysis()

	if analysis.RMD == nil || len(analysis.RMD.Projections) == 0 {
		t.Skip("no RMD rows in scenario; structural test does not apply")
	}
	if analysis.Projection == nil || len(analysis.Projection.Months) == 0 {
		t.Fatal("missing projection")
	}

	for _, row := range analysis.RMD.Projections {
		startMonth := 12 * row.Year
		endMonth := startMonth + 12
		if endMonth > len(analysis.Projection.Months) {
			endMonth = len(analysis.Projection.Months)
		}
		var got float64
		for m := startMonth; m < endMonth; m++ {
			got += analysis.Projection.Months[m].RMDWithdrawal
		}
		if (row.RMDAmount-got) > 0.01 || (got-row.RMDAmount) > 0.01 {
			t.Errorf("year %d (age %d): RMD.RMDAmount = %.4f; sum of Projection.RMDWithdrawal = %.4f",
				row.Year, row.Age, row.RMDAmount, got)
		}
	}
}
```

- [ ] **Step 2: Run the new tests.**

Run: `go test ./internal/services/retirement/ -run TestRunFullAnalysis_F072 -v`
Expected: both PASS.

If `TestRunFullAnalysis_F072_DepletedBeforeRMD_NoRMDRows` doesn't depleted (because settings happen to survive), tweak `s.MonthlyLivingExpenses` higher or `s.PortfolioValue` lower until depletion happens before age 73. The bar is: `analysis.Projection.DepletionMonth != nil && *analysis.Projection.DepletionMonth/12 < 13`. The values above were chosen to be unambiguously deplete-fast; adjust if a default behavior shift makes them survive.

- [ ] **Step 3: Run the full repository suite.**

Run: `go test ./...`
Expected: all packages pass.

- [ ] **Step 4: Commit.**

```bash
git add internal/services/retirement/calculator_test.go
git commit -m "$(cat <<'EOF'
test(whatif): F-072 RMD-from-projection integration regression

Adds two end-to-end tests:
- TestRunFullAnalysis_F072_DepletedBeforeRMD_NoRMDRows pins the
  regression bar for the reported user bug: depleted-before-RMD
  scenario must yield zero RMD rows.
- TestRunFullAnalysis_F072_RMDMatchesProjection enforces the
  structural invariant that each emitted RMD row's amount equals
  the sum of RMDWithdrawal across that year's projection months.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Update RMD card template

**Files:**
- Modify: `web/templates/components/whatif/rmd.html`

- [ ] **Step 1: Read the current template to confirm structure.**

Run: `cat web/templates/components/whatif/rmd.html`
Expected: the file matches what you saw during brainstorming (header KPIs, table, footer empty-state).

- [ ] **Step 2: Replace the template with the depletion-aware version.**

Overwrite `web/templates/components/whatif/rmd.html` with:

```html
{{/* RMD Projections Card */}}
{{/* Expects: .Analysis.RMD and .Settings */}}
{{define "whatif-rmd"}}
{{if and .Analysis.RMD (gt .Settings.TaxDeferredPercent 0.0)}}
<div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
    <h3 class="text-lg font-semibold text-gray-800 dark:text-gray-100 mb-4">Required Minimum Distributions (RMD)</h3>

    <div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-4">
        <div class="text-center p-3 bg-gray-50 dark:bg-gray-700 rounded-lg">
            <p class="text-xs text-gray-500 dark:text-gray-300 uppercase">Current Age</p>
            <p class="text-xl font-bold text-gray-800 dark:text-gray-200">{{.Analysis.RMD.CurrentAge}}</p>
        </div>
        <div class="text-center p-3 bg-gray-50 dark:bg-gray-700 rounded-lg">
            <p class="text-xs text-gray-500 dark:text-gray-300 uppercase">RMDs Begin</p>
            <p class="text-xl font-bold text-gray-800 dark:text-gray-200">
                {{if gt .Analysis.RMD.StartsInYears 0}}
                    Age {{.Analysis.RMD.StartAge}} ({{.Analysis.RMD.StartsInYears}}yr)
                {{else}}
                    Now
                {{end}}
            </p>
        </div>
        <div class="text-center p-3 bg-gray-50 dark:bg-gray-700 rounded-lg">
            <p class="text-xs text-gray-500 dark:text-gray-300 uppercase">Tax-Deferred</p>
            <p class="text-xl font-bold text-indigo-600 dark:text-indigo-400">{{formatMoney .Analysis.RMD.TaxDeferredValue}}</p>
        </div>
        <div class="text-center p-3 bg-gray-50 dark:bg-gray-700 rounded-lg">
            <p class="text-xs text-gray-500 dark:text-gray-300 uppercase">10-Yr RMD Total</p>
            <p class="text-xl font-bold text-amber-600 dark:text-amber-400">{{formatMoney .Analysis.RMD.TotalRMDsOver10Yr}}</p>
        </div>
    </div>

    {{if .Analysis.RMD.DepletedBeforeRMD}}
    <div class="bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-800 rounded-lg p-4">
        <p class="text-sm text-amber-800 dark:text-amber-200">
            Portfolio depletes in year {{.Analysis.RMD.DepletionYear}}{{if .Analysis.RMD.DepletionAge}} (age {{.Analysis.RMD.DepletionAge}}){{end}} — no RMDs would apply in this scenario.
        </p>
    </div>
    {{else if .Analysis.RMD.Projections}}
    <div class="overflow-x-auto">
        <table class="w-full text-sm">
            <thead>
                <tr class="text-left text-gray-500 dark:text-gray-300 border-b dark:border-gray-600">
                    <th class="pb-2 font-medium">Age</th>
                    <th class="pb-2 font-medium text-right">Year</th>
                    <th class="pb-2 font-medium text-right">Tax-Deferred Balance</th>
                    <th class="pb-2 font-medium text-right">Life Exp. Factor</th>
                    <th class="pb-2 font-medium text-right">RMD Amount</th>
                    <th class="pb-2 font-medium text-right">RMD %</th>
                </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-gray-700">
                {{range $i, $p := .Analysis.RMD.Projections}}
                {{if lt $i 10}}
                <tr class="text-gray-700 dark:text-gray-300">
                    <td class="py-2 font-medium">{{$p.Age}}</td>
                    <td class="py-2 text-right text-gray-500 dark:text-gray-400">{{$p.Year}}</td>
                    <td class="py-2 text-right">{{formatMoney $p.TaxDeferredBal}}</td>
                    <td class="py-2 text-right">{{printf "%.1f" $p.LifeExpFactor}}</td>
                    <td class="py-2 text-right font-semibold text-amber-600 dark:text-amber-400">{{formatMoney $p.RMDAmount}}</td>
                    <td class="py-2 text-right">{{printf "%.1f" $p.RMDPercent}}%</td>
                </tr>
                {{end}}
                {{end}}
            </tbody>
        </table>
    </div>
    {{if .Analysis.RMD.DepletionYear}}
    <p class="text-xs text-amber-600 dark:text-amber-400 mt-2">
        Portfolio depletes in year {{.Analysis.RMD.DepletionYear}}{{if .Analysis.RMD.DepletionAge}} (age {{.Analysis.RMD.DepletionAge}}){{end}}; subsequent RMD years not shown.
    </p>
    {{end}}
    <p class="text-xs text-gray-400 dark:text-gray-500 mt-2">
        RMD % is the IRS Uniform Lifetime factor; RMD amount is the projected withdrawal from your scenario.
    </p>
    {{else}}
    <p class="text-sm text-gray-500 dark:text-gray-300">
        {{if le .Settings.TaxDeferredPercent 0.0}}
            No tax-deferred accounts configured.
        {{else if gt .Analysis.RMD.StartsInYears .Settings.ProjectionYears}}
            RMDs begin after your projection period.
        {{else}}
            RMD projections will appear when applicable.
        {{end}}
    </p>
    {{end}}
</div>
{{end}}
{{end}}
```

Differences from the original:

1. Removed the *"Scenario chain is not yet applied"* italic line (line 9 of the original).
2. Added the `{{if .Analysis.RMD.DepletedBeforeRMD}}` banner branch immediately after the KPI grid. When true, it suppresses the table and the empty-state.
3. Added the `{{if .Analysis.RMD.DepletionYear}}` footer (rendered only when *not* `DepletedBeforeRMD`, since that branch returns first).
4. Added the IRS-vs-actual percent footer line under the table.

- [ ] **Step 3: Run a quick template smoke test.**

If the project has a template render test, run it. Otherwise:

```bash
go build ./...
```

Expected: clean build. If the html/template parser fails at runtime, errors only surface during request handling — Step 4 catches that.

- [ ] **Step 4: Run the full test suite.**

Run: `go test ./...`
Expected: all tests pass. Any handler-level test that renders the whatif page exercises this template.

- [ ] **Step 5: (Manual) Visually verify the three states.**

Optional but recommended: launch the dev server, load three What-If scenarios:

1. One that depletes early (e.g., the regression-test settings from Task 4) — confirm the banner renders and the table is hidden.
2. One that depletes late (e.g., during RMD years) — confirm a small table renders with the footer note.
3. One that survives — confirm the standard table renders with the new IRS-vs-actual footer.

If running headlessly or in CI, skip this step.

- [ ] **Step 6: Commit.**

```bash
git add web/templates/components/whatif/rmd.html
git commit -m "$(cat <<'EOF'
feat(whatif): F-072 depletion-aware RMD card

Renders one of three states from the new RMDAnalysis fields:
- DepletedBeforeRMD: amber banner; table hidden
- Depleted-during-RMD: table truncated to pre-depletion rows + footer
- Survives: standard table with IRS-vs-actual percent footer

Removes the obsolete "Scenario chain is not yet applied" disclaimer
since the panel now reads from the chained projection.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Update audit ledger

**Files:**
- Modify: `docs/whatif-math-audit-2026-05-05.md`

- [ ] **Step 1: Add the F-072 row to the Findings table.**

Open `docs/whatif-math-audit-2026-05-05.md`. Locate the Findings table (around line 66 — `## Findings table`). Add a new row in the MEDIUM severity block. Suggested placement: alphabetically by ID (after F-071 if present, otherwise at the end of the MEDIUM block):

```markdown
| [F-072](#f-072--medium-rmd-analysis-detached-from-actual-projection-misleading-balances-when-portfolio-depletes-early) | MEDIUM | 10. Scenario chain, healthcare, budget-fit / steady-state | `rmd.go:132` | `CalculateRMDAnalysis` is computed in isolation from `RunProjection`; RMD card shows compounding balances and quotes future RMDs even when the main projection has already depleted the portfolio |
```

- [ ] **Step 2: Add a full F-072 entry in Appendix C — Findings ledger.**

Locate `## Appendix C — Findings ledger` (around line 2513). Add at the end:

```markdown
### F-072 — MEDIUM — RMD analysis detached from actual projection; misleading balances when portfolio depletes early

**Status:** RESOLVED in PR 11 (commit range `94219bd..HEAD` on `feat/whatif-fixes`).

**Location:** `internal/services/retirement/rmd.go:132` (pre-fix); the new entry point is `BuildRMDAnalysis(projection)` invoked from `RunFullAnalysis` at `calculator.go:3068`.

**Symptom:** A user with a low portfolio relative to expenses sees the main projection deplete in under two years, while the RMD card cheerfully shows the tax-deferred bucket compounding for ten or more years and projects RMDs against balances that do not exist in the user's plan.

**Cause:** `CalculateRMDAnalysis` re-derived the RMD trajectory from `s.PortfolioValue * s.TaxDeferredPercent / 100` and applied only investment growth and the RMD itself. It never accounted for living expenses, healthcare, taxes, big-ticket spending, or the actual `RunProjection` cash-flow drawdown.

**Fix:** Replaced with `BuildRMDAnalysis(projection *ProjectionResult)` which samples each January's `TaxDeferredBalance` and sums the year's `RMDWithdrawal` from `projection.Months[]`. Three new fields on `RMDAnalysis` (`DepletionYear`, `DepletionAge`, `DepletedBeforeRMD`) drive depletion-aware rendering. The template renders one of three states: banner (depleted before RMD), truncated table with footer note (depleted during RMD years), or standard table with an IRS-vs-actual percent disclosure.

**Tests:**
- 7 unit tests in `rmd_test.go` covering depletion before/during/after RMD, SECURE 2.0 transition, already-at-RMD-age, zero TaxDeferredPercent, and IRS-table percent semantics.
- 2 integration tests in `calculator_test.go`: `TestRunFullAnalysis_F072_DepletedBeforeRMD_NoRMDRows` (regression bar for the user-reported bug) and `TestRunFullAnalysis_F072_RMDMatchesProjection` (structural invariant).
```

The anchor URL fragment is generated from the heading text by GitHub-flavored markdown — verify the link in Step 1 matches the heading text exactly (lowercase, hyphens for spaces, punctuation removed).

- [ ] **Step 3: Run the full test suite one more time.**

Run: `go test ./...`
Expected: all packages pass.

- [ ] **Step 4: Commit.**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "$(cat <<'EOF'
docs(audit): mark F-072 resolved in PR 11

Adds F-072 to the Findings table and full ledger entry under
Appendix C, documenting the RMD-from-projection refactor that
eliminates the divergence between the RMD card and the main
projection's actual portfolio drawdown.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-review checklist (for the executing engineer)

After all tasks complete:

- [ ] `grep -rn "CalculateRMDAnalysis\b" internal/` returns zero results — old function fully removed.
- [ ] `grep -rn "rmdGrowthFractions\b" internal/` returns zero results — old helper fully removed.
- [ ] `grep -rn "Scenario chain is not yet applied" web/` returns zero results — disclaimer fully removed.
- [ ] `grep -rn "BuildRMDAnalysis\b" internal/` returns the function definition, the call site in `calculator.go`, and tests.
- [ ] `git log --oneline | head -7` shows the six new commits in expected order: model, BuildRMDAnalysis, switch caller, integration tests, template, audit.
- [ ] `go test ./...` is fully green.
- [ ] `gitnexus_detect_changes()` shows changes scoped to the expected symbols (RMD-related functions and the WhatIfAnalysis assembly).

If any check fails, fix before proposing the PR.
