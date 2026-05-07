# F-077 Birth-Year-Based RMD Start Age (SECURE 2.0 Per-Person Correction)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix `EffectiveRMDStartAge` so the SECURE 2.0 age-75 threshold is applied per-person based on the older spouse's birth year (the IRS rule), not based on the projection's start year. Reviewer-found bug after F-073/4/5/6 merge: a 2026-start projection with the older spouse at age 65 currently triggers RMD at age 73 in projection year 8 (calendar year 2034), but per IRS Notice 2023-23 / SECURE 2.0 §107, anyone attaining age 73 in 2033 or later (born 1960+) must use start age 75.

**Architecture:** Single PR. One-function refactor of `EffectiveRMDStartAge` in `rmd.go`, exposing a new package-internal helper `effectiveRMDStartAgeForBirthYear(int) int`. Update existing F-032 unit tests (which were tautologically passing because they didn't set `CurrentAge`) to assert the new birth-year semantics explicitly. Add new F-077 unit + integration tests.

**Tech Stack:** Go 1.26, no external test deps.

---

## Pre-Flight

- [ ] **Step P-1: Confirm baseline green on `dev`.**

```bash
cd /home/darrell/bin/ai/budget2
git rev-parse --abbrev-ref HEAD   # expect: dev
git status                        # working tree state
git log --oneline -3              # confirm a81f589 (merge) is HEAD
go test ./internal/services/retirement/... -count=1
```

Expected: HEAD is `a81f589 Merge feat/rmd-audit-followup into dev`, retirement suite PASSES.

- [ ] **Step P-2: Create the working branch.**

```bash
git checkout -b feat/rmd-birth-year
```

---

## PR 1 — F-077: Birth-year-based applicable RMD age

**Audit reference:** Extends F-032 / F-075. F-032 introduced `EffectiveRMDStartAge` keyed off projection start year. F-075 wired the call into the projection engine. Reviewer found that the keying logic itself is wrong: SECURE 2.0 §107 (and IRS Notice 2023-23) ties the applicable age to the calendar year the **person** attains age 73, not to when the projection starts. A 2026-start projection where the older spouse is 65 should use applicable age 75 (born 1961, attains 73 in 2034); the current code returns 73.

### Files

- Modify: `internal/services/retirement/rmd.go` (refactor `EffectiveRMDStartAge`, add helper)
- Modify: `internal/services/retirement/rmd_tax_test.go:12-52` (5 existing F-032 tests need explicit `CurrentAge`)
- Create: `internal/services/retirement/rmd_birth_year_test.go` (new F-077 tests)
- Modify: `docs/whatif-math-audit-2026-05-05.md` (F-077 closing entry)

### Step 1: Write the failing integration test (the reviewer's example)

Create `internal/services/retirement/rmd_birth_year_test.go`:

```go
package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// F-077: a 2026-start projection with the older spouse aged 65 (born 1961)
// MUST NOT trigger RMD at age 73 in projection year 8 (calendar 2034) because
// the person attains age 73 after Dec 31, 2032 — applicable age is 75 per
// SECURE 2.0 §107 / IRS Notice 2023-23. RMD must start in projection year 10
// (calendar 2036) when the person turns 75.
func TestProjection_F077_BornAfter1959ReachesRMDAt75(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 65
	s.SpouseAge = 0
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 12
	s.StartDate = "2026-01"
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := NewCalculator(s).RunProjection()
	if proj == nil || len(proj.Months) < 12*12 {
		t.Fatalf("nil/short projection: months=%d", func() int {
			if proj == nil {
				return 0
			}
			return len(proj.Months)
		}())
	}

	// Year 8 (months 96..107, calendar 2034, person aged 73): NO RMD allowed.
	for m := 96; m < 108; m++ {
		if proj.Months[m].RMDWithdrawal != 0 {
			t.Errorf("month %d (year 8, age 73, calendar 2034) RMDWithdrawal = %.2f; want 0 — born 1961 must wait until 75",
				m, proj.Months[m].RMDWithdrawal)
		}
	}

	// Year 9 (months 108..119, calendar 2035, person aged 74): NO RMD allowed.
	for m := 108; m < 120; m++ {
		if proj.Months[m].RMDWithdrawal != 0 {
			t.Errorf("month %d (year 9, age 74) RMDWithdrawal = %.2f; want 0", m, proj.Months[m].RMDWithdrawal)
		}
	}

	// Year 10 (months 120..131, calendar 2036, person aged 75): RMD MUST trigger.
	var year10Total float64
	for m := 120; m < 132; m++ {
		year10Total += proj.Months[m].RMDWithdrawal
	}
	if year10Total <= 0 {
		t.Errorf("year-10 total RMDWithdrawal = %.2f; want > 0 (person turns 75 in 2036, applicable age 75)", year10Total)
	}
}

// F-077: a 2026-start projection with the older spouse aged 67 (born 1959)
// MUST trigger RMD at age 73 in projection year 6 (calendar 2032) because the
// person attains 73 in 2032 — applicable age stays at 73 per SECURE 2.0.
func TestProjection_F077_BornBefore1960ReachesRMDAt73(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 67
	s.SpouseAge = 0
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 10
	s.StartDate = "2026-01"
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := NewCalculator(s).RunProjection()
	if proj == nil || len(proj.Months) < 7*12 {
		t.Fatal("nil/short projection")
	}

	// Years 0..5 (months 0..71, ages 67..72): no RMD.
	for m := 0; m < 72; m++ {
		if proj.Months[m].RMDWithdrawal != 0 {
			t.Errorf("month %d (year %d, age %d) RMDWithdrawal = %.2f; want 0",
				m, m/12, 67+m/12, proj.Months[m].RMDWithdrawal)
		}
	}

	// Year 6 (months 72..83, calendar 2032, person aged 73): RMD MUST trigger.
	var year6Total float64
	for m := 72; m < 84; m++ {
		year6Total += proj.Months[m].RMDWithdrawal
	}
	if year6Total <= 0 {
		t.Errorf("year-6 total RMDWithdrawal = %.2f; want > 0 (person turns 73 in 2032, applicable age 73)", year6Total)
	}
}

// F-077: when the older person in a couple is the spouse, applicability uses
// the spouse's birth year (because GetOlderAge() returns the spouse's age).
func TestProjection_F077_OlderSpouseDrivesRMDAge(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 60
	s.SpouseAge = 70 // older; born 1956 → applicable age 73
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 5
	s.StartDate = "2026-01"
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := NewCalculator(s).RunProjection()
	if proj == nil || len(proj.Months) < 4*12 {
		t.Fatal("nil/short projection")
	}

	// Year 3 (months 36..47, calendar 2029, spouse aged 73): RMD MUST trigger.
	var year3Total float64
	for m := 36; m < 48; m++ {
		year3Total += proj.Months[m].RMDWithdrawal
	}
	if year3Total <= 0 {
		t.Errorf("year-3 RMDWithdrawal = %.2f; want > 0 (spouse turns 73 in 2029, applicable age 73)", year3Total)
	}
}

// F-077: birth-year boundary — exactly the SECURE 2.0 cusp.
func TestEffectiveRMDStartAge_F077_BirthYearBoundary(t *testing.T) {
	cases := []struct {
		name      string
		startYear string
		age       int
		want      int
	}{
		{"born 1959 (attains 73 in 2032) → 73", "2026-01", 67, 73},
		{"born 1960 (attains 73 in 2033) → 75", "2026-01", 66, 75},
		{"born 1958 (attains 73 in 2031) → 73", "2033-01", 75, 73},
		{"born 1961 (attains 73 in 2034) → 75", "2026-01", 65, 75},
		{"born 1953 (attains 73 in 2026) → 73", "2026-01", 73, 73},
	}
	for _, c := range cases {
		s := &models.WhatIfSettings{
			StartDate:  c.startYear,
			CurrentAge: c.age,
		}
		got := EffectiveRMDStartAge(s)
		if got != c.want {
			t.Errorf("%s: EffectiveRMDStartAge = %d; want %d", c.name, got, c.want)
		}
	}
	// Suppress unused import on `math` if the file has no other math use.
	_ = math.Abs
}
```

> **Note for the implementing agent:** the dummy `_ = math.Abs` line at the bottom keeps the `math` import alive in case the integration tests above are reorganized. If `math.Abs` is used naturally in the final test bodies, delete the line.

### Step 2: Run the new tests; expect FAIL.

```bash
go test ./internal/services/retirement/ -run "F077" -v -count=1
```

Expected:
- `TestProjection_F077_BornAfter1959ReachesRMDAt75` — FAIL (current code returns 73 for `StartDate=2026`, so RMD wrongly fires at age 73 in year 8).
- `TestProjection_F077_BornBefore1960ReachesRMDAt73` — PASS (already worked under old logic).
- `TestProjection_F077_OlderSpouseDrivesRMDAge` — PASS under old logic too (StartDate 2026 → 73, spouse age 73 in year 3, fires).
- `TestEffectiveRMDStartAge_F077_BirthYearBoundary` — FAIL on the "born 1960 → 75" case and "born 1961 → 75" case (current code returns 73 for both).

### Step 3: Refactor `EffectiveRMDStartAge` and add the helper.

In `internal/services/retirement/rmd.go`, replace lines 12-24 (the existing `EffectiveRMDStartAge` function and its docstring):

```go
// EffectiveRMDStartAge returns the SECURE 2.0 RMD applicable age for the
// older person in the household. Per SECURE 2.0 §107 and IRS Notice 2023-23,
// the applicable age is determined by the calendar year the person attains
// age 73:
//
//   - Attains age 73 in 2032 or earlier (born 1959 or earlier) → 73
//   - Attains age 73 in 2033 or later  (born 1960 or later)   → 75
//
// The older spouse drives the household's RMD timing, so this function uses
// GetOlderAge() against the projection's start year to derive the relevant
// birth year. F-077: prior implementation keyed off StartDate.Year() alone,
// which produced the wrong answer for any projection that begins before
// 2033 with a person born in 1960 or later.
func EffectiveRMDStartAge(s *models.WhatIfSettings) int {
	if s == nil {
		return 73
	}
	startYear := parseStartYear(s.StartDate)
	olderBirthYear := startYear - s.GetOlderAge()
	return effectiveRMDStartAgeForBirthYear(olderBirthYear)
}

// effectiveRMDStartAgeForBirthYear returns the SECURE 2.0 RMD applicable age
// for a person born in the given calendar year. Boundary: birth year ≥ 1960
// → 75 (attains 73 in 2033 or later); otherwise → 73.
func effectiveRMDStartAgeForBirthYear(birthYear int) int {
	if birthYear+73 >= 2033 {
		return 75
	}
	return 73
}
```

> **Important:** keep `parseStartYear` and `RMDStartAge` unchanged. Do not delete or rename them.

### Step 4: Update existing F-032 unit tests to assert the new birth-year semantics explicitly.

In `internal/services/retirement/rmd_tax_test.go`, replace lines 10-52 (the five F-032 tests). The existing tests didn't set `CurrentAge`, so under the new logic they'd evaluate `birthYear = startYear - 0 = startYear`, which is well past 1959 — every test would return 75. The tests were tautologically passing under F-032's start-year keying; they need explicit ages to be meaningful.

```go
// F-032 tests — EffectiveRMDStartAge (updated for F-077 birth-year semantics)

func TestEffectiveRMDStartAge_F032_Pre2033(t *testing.T) {
	// Older spouse turns 73 in 2029 (born 1956) — applicable age 73.
	s := &models.WhatIfSettings{
		StartDate:  "2026-01",
		CurrentAge: 70,
	}
	if got := EffectiveRMDStartAge(s); got != 73 {
		t.Errorf("born 1956, pre-2033 attainment, start age = %d; want 73", got)
	}
}

func TestEffectiveRMDStartAge_F032_PostJan2033(t *testing.T) {
	// Older spouse turns 73 in 2033 (born 1960) — applicable age 75.
	s := &models.WhatIfSettings{
		StartDate:  "2033-01",
		CurrentAge: 73,
	}
	if got := EffectiveRMDStartAge(s); got != 75 {
		t.Errorf("born 1960, attains 73 in 2033, start age = %d; want 75", got)
	}
}

func TestEffectiveRMDStartAge_F032_Post2033(t *testing.T) {
	// Older spouse turns 73 in 2034 (born 1961) — applicable age 75.
	s := &models.WhatIfSettings{
		StartDate:  "2040-06",
		CurrentAge: 79,
	}
	if got := EffectiveRMDStartAge(s); got != 75 {
		t.Errorf("born 1961, attains 73 in 2034, start age = %d; want 75", got)
	}
}

func TestEffectiveRMDStartAge_F032_NilSafe(t *testing.T) {
	if got := EffectiveRMDStartAge(nil); got != 73 {
		t.Errorf("nil settings start age = %d; want 73", got)
	}
}

func TestEffectiveRMDStartAge_F032_ExactBoundary2032(t *testing.T) {
	// Older spouse turns 73 in 2032 (born 1959) — applicable age 73 (last legacy year).
	s := &models.WhatIfSettings{
		StartDate:  "2032-12",
		CurrentAge: 73,
	}
	if got := EffectiveRMDStartAge(s); got != 73 {
		t.Errorf("born 1959, attains 73 in 2032, start age = %d; want 73", got)
	}
}
```

### Step 5: Run all retirement tests; expect PASS.

```bash
go test ./internal/services/retirement/ -count=1
```

Expected: PASS, including:
- 4 new F-077 tests (Step 1)
- 5 updated F-032 tests (Step 4)
- 3 existing F-075 projection tests (unchanged, all pass under new logic — verified by hand)
- 3 existing F-074 timing tests (unchanged, all use 2026 start with age 73 → birth 1953 → applicable age 73; behavior matches old)
- All other retirement tests

If any test outside the F-032/F-075/F-077 set fails, surface it (don't silently update) — that's a sign of an unintended downstream effect.

### Step 6: Run the full suite.

```bash
go test ./... -count=1
```

Expected: PASS. The handler tests (`TestBuildProjectionChartEvents_F075_*`) deserve special attention:

- `TestBuildProjectionChartEvents_F075_RMDStartsUsesEffectiveAge` uses `CurrentAge: 70, StartDate: "2033-01"` → birth = 1963 → applicable 75 → asserted year delta of 5 (75-70). Still correct under new logic.
- `TestBuildProjectionChartEvents_F075_RMDStartsPre2033Uses73` (the bonus pre-2033 test added in PR 3) — verify what it asserts. If it uses `CurrentAge=70, StartDate=2026`, then birth=1956 → 73 → year delta 3. Still correct.

If either handler test pinned the old wrong behavior, surface it.

### Step 7: Mark F-077 in audit doc and commit.

Append an F-077 closing entry to `docs/whatif-math-audit-2026-05-05.md` mirroring F-073/F-074/F-075/F-076 style. Cross-reference F-032 (origin) and F-075 (the PR that wired the buggy logic into projection).

```bash
git add internal/services/retirement/rmd.go \
        internal/services/retirement/rmd_tax_test.go \
        internal/services/retirement/rmd_birth_year_test.go \
        docs/whatif-math-audit-2026-05-05.md
git commit -m "$(cat <<'EOF'
fix(whatif): F-077 SECURE 2.0 RMD age uses birth year, not projection year

EffectiveRMDStartAge previously keyed off StartDate.Year() — returning 75
only when the projection itself started in 2033 or later. This is wrong:
SECURE 2.0 §107 (and IRS Notice 2023-23) ties the applicable age to the
calendar year the person attains age 73, not to when the projection begins.
A 2026-start projection with the older spouse at age 65 should use
applicable age 75 (born 1961, attains 73 in 2034); the prior code
incorrectly fired RMDs at age 73 in projection year 8.

EffectiveRMDStartAge now derives the older spouse's birth year from
StartDate and GetOlderAge(), then delegates to a new
effectiveRMDStartAgeForBirthYear helper that applies the SECURE 2.0
boundary at birth year 1960. Existing F-032 unit tests updated with
explicit CurrentAge values to assert birth-year semantics; F-074, F-075,
F-076 unaffected (verified by full suite).

Closes F-077. Extends F-032 / F-075.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

Pre-commit hook runs go vet, staticcheck, govulncheck, full test suite, GitNexus refresh. If it fails, fix and create a NEW commit (do not amend).

### Step 8: Run scope verification.

```bash
git diff --stat HEAD~1 HEAD
```

Expected: 4 files (`rmd.go`, `rmd_tax_test.go`, `rmd_birth_year_test.go`, audit doc).

---

## Process Notes

- **TDD strictly:** tests in Steps 1 + 4, fail-confirm in Step 2, implementation in Step 3, green in Steps 5-6.
- **Don't amend commits.** If pre-commit fails, fix and make a new commit.
- **Don't run `go test -race`** (project memory).
- **Don't touch projection-engine code.** F-075's call sites already invoke `EffectiveRMDStartAge(s)` correctly; this PR only changes what that function returns.
- **Don't touch templates or handlers.** F-076 territory is settled.

---

## Self-Review

| Spec point | Plan task | Status |
|---|---|---|
| `EffectiveRMDStartAge` keyed off birth year, not start year | Step 3 | ✓ refactor + helper |
| Bug-case integration test (born 1961, 2026 start) | Step 1 | ✓ `TestProjection_F077_BornAfter1959ReachesRMDAt75` |
| Boundary test at SECURE 2.0 cusp | Step 1 | ✓ `TestEffectiveRMDStartAge_F077_BirthYearBoundary` |
| Older-spouse drives applicability | Step 1 | ✓ `TestProjection_F077_OlderSpouseDrivesRMDAge` |
| Updated F-032 tests assert birth-year semantics | Step 4 | ✓ all 5 tests |
| Audit doc closing entry | Step 7 | ✓ |

**Placeholder scan:** none. Every step has literal code or commands.

**Type/symbol consistency:**
- `effectiveRMDStartAgeForBirthYear` (lowercase, package-internal) — defined in Step 3, no other callers required.
- `EffectiveRMDStartAge(s)` signature unchanged — all existing call sites in `calculator.go`, `backtest.go`, `handlers.go`, `rmd.go:142` continue to work without modification.

**Cross-task ordering:** single PR, no inter-task dependency.

**Spec coverage gap:** none. Reviewer's example (`StartDate=2026-01, CurrentAge=65, year 8`) is the literal subject of `TestProjection_F077_BornAfter1959ReachesRMDAt75`.
