# F-078 RMD Calendar-Year Gate (Wire EffectiveRMDStartAge into All Consumers)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every consumer of `EffectiveRMDStartAge` trigger RMDs in the correct calendar year and pass the correct age to the divisor table — so a person born late in 1959 (e.g. 1959-12) with `StartDate=2026-01` actually starts their RMDs in 2032 (the year they attain 73), not 2033, and the divisor uses the age-73 factor, not age-72.

**Architecture:** `EffectiveRMDStartAge` returns the right *applicable age* (73 vs 75) since F-077, but every consumer compares it against `s.GetOlderAge() + currentYear`. `GetOlderAge()` floors via `deriveAgeAtStartDate`, which under-counts by 1 whenever the older person's birthday falls after `StartDate`'s month — silently slipping the first RMD year by one. Two-bug fix: (a) gate on calendar year vs first-RMD-year, and (b) pass age-at-year-end to `CalculateRMD`. Single PR, four new helpers in `rmd.go`, eight call sites updated, one full-projection integration test as the canary.

**Tech Stack:** Go 1.26, no external test deps. Targets: `internal/services/retirement/{rmd,calculator,backtest}.go`, `internal/handlers/whatif/handlers.go`.

---

## Pre-Flight

- [ ] **Step P-1: Confirm baseline green on `dev`.**

```bash
cd /home/darrell/bin/ai/budget2
git rev-parse --abbrev-ref HEAD   # expect: dev
git status                        # confirm clean tree (AGENTS.md / CLAUDE.md drift is OK to stash if present)
git log --oneline -3              # expect d5eef13 fix(whatif): F-077 fixup at HEAD
go test ./internal/services/retirement/... -count=1
go test ./internal/handlers/whatif/... -count=1
```

Expected: HEAD is `d5eef13 fix(whatif): F-077 fixup — RMD birth year reads BirthMonth, not floor'd age`. Both retirement and whatif test suites PASS.

- [ ] **Step P-2: Cut the working branch.**

```bash
git checkout -b feat/rmd-calendar-year-gate
```

---

## PR 1 — F-078: Calendar-year gating + correct divisor age across all RMD consumers

**Audit reference:** Reviewer P2 finding on top of F-077:
> "Late-1959 births still start RMDs one year late in projections. The new EffectiveRMDStartAge correctly derives the applicable age from BirthMonth, but the projection/backtest/MC gates still use s.GetOlderAge() + currentYear. For someone born 1959-12 with StartDate=2026-01, ComputeAges gives age 66; the loop sees age 72 in calendar 2032 and does not trigger RMD until 2033, even though they attain 73 in December 2032 and should be under the age-73 rule. This affects the projection engine, Monte Carlo, backtest, chart event label, and RMD analysis 'starts in' math."

The same logic also feeds the wrong age into `CalculateRMD` once the gate finally fires — divisor reads the prior year's UL Table row, undershooting the trigger-year RMD by ~3% (age-72 factor 27.4 vs age-73 factor 26.5).

### Files

- Modify: `internal/services/retirement/rmd.go` (add helpers, fix `BuildRMDAnalysis`)
- Modify: `internal/services/retirement/calculator.go` (5 call sites)
- Modify: `internal/services/retirement/backtest.go` (1 call site)
- Modify: `internal/handlers/whatif/handlers.go` (chart event label)
- Create: `internal/services/retirement/rmd_calendar_year_test.go` (helper tests + integration canary)
- Modify: `internal/services/retirement/rmd_birth_year_test.go` (extend the existing `TestProjection_F077_*` integration tests with a 1959-12 case)
- Modify: `docs/whatif-math-audit-2026-05-05.md` (close F-078 entry)

---

### Task 1: Add `olderBirthYear`, `FirstRMDCalendarYear`, `RMDApplies`, `RMDAgeForCalendarYear` helpers (TDD)

**Files:**
- Create: `internal/services/retirement/rmd_calendar_year_test.go`
- Modify: `internal/services/retirement/rmd.go` (append helpers below `effectiveRMDStartAgeForBirthYear`)

- [ ] **Step 1.1: Write failing helper tests.**

Create `internal/services/retirement/rmd_calendar_year_test.go`:

```go
package retirement

import (
	"testing"

	"budget2/internal/models"
)

// F-078: helpers that drive calendar-year RMD gating across the projection,
// MC, backtest, chart event label, and RMD analysis. These replace the
// floor'd `olderAge >= EffectiveRMDStartAge(s)` comparisons that slipped
// late-year births by one calendar year.

func TestFirstRMDCalendarYear_F078_BirthMonth(t *testing.T) {
	cases := []struct {
		name         string
		primaryBirth string
		spouseBirth  string
		startDate    string
		want         int
	}{
		{"primary born 1959-12 → 2032 (1959+73)", "1959-12", "", "2026-01", 2032},
		{"primary born 1959-01 → 2032", "1959-01", "", "2026-01", 2032},
		{"primary born 1960-01 → 2035 (1960+75)", "1960-01", "", "2026-01", 2035},
		{"primary born 1960-12 → 2035", "1960-12", "", "2026-01", 2035},
		{"older spouse born 1959-11, primary 1965 → 2032", "1965-06", "1959-11", "2026-01", 2032},
		{"older primary 1953-06 → 2026 (already in RMD)", "1953-06", "", "2026-01", 2026},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.StartDate = c.startDate
			s.Persons = []models.Person{
				{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: c.primaryBirth},
			}
			if c.spouseBirth != "" {
				s.Persons = append(s.Persons, models.Person{
					ID: "s1", Name: "Spouse", Role: models.PersonRoleSpouse, BirthMonth: c.spouseBirth,
				})
			}
			s.ComputeAges()
			if got := FirstRMDCalendarYear(s); got != c.want {
				t.Errorf("FirstRMDCalendarYear = %d; want %d", got, c.want)
			}
		})
	}
}

// Legacy fallback: when no Person carries BirthMonth, the helper must derive
// the birth year from startYear - GetOlderAge() — exactly what the old
// projection gate did, so non-Person callers stay unchanged.
func TestFirstRMDCalendarYear_F078_LegacyFallback(t *testing.T) {
	s := &models.WhatIfSettings{
		StartDate:  "2026-01",
		CurrentAge: 66, // legacy: birth year derived as 2026-66=1960 → applicable 75 → first RMD 2035
	}
	if got := FirstRMDCalendarYear(s); got != 2035 {
		t.Errorf("legacy fallback (CurrentAge=66, 2026 start) = %d; want 2035", got)
	}
}

func TestRMDApplies_F078(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: "1959-12"},
	}
	s.ComputeAges()

	if RMDApplies(s, 2031) {
		t.Errorf("RMDApplies(1959-12, 2031) = true; want false")
	}
	if !RMDApplies(s, 2032) {
		t.Errorf("RMDApplies(1959-12, 2032) = false; want true (attains 73 in Dec 2032)")
	}
	if !RMDApplies(s, 2050) {
		t.Errorf("RMDApplies(1959-12, 2050) = false; want true")
	}
}

// RMDAgeForCalendarYear must return the age the older person attains by
// Dec 31 of the calendar year — the age the IRS Uniform Lifetime Table is
// keyed off. For a 1959-12 birth in calendar 2032 that's 73 (not the
// floor'd 72 the old `GetOlderAge() + currentYear` would have produced).
func TestRMDAgeForCalendarYear_F078(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: "1959-12"},
	}
	s.ComputeAges()

	if got := RMDAgeForCalendarYear(s, 2032); got != 73 {
		t.Errorf("RMDAgeForCalendarYear(1959-12, 2032) = %d; want 73", got)
	}
	if got := RMDAgeForCalendarYear(s, 2033); got != 74 {
		t.Errorf("RMDAgeForCalendarYear(1959-12, 2033) = %d; want 74", got)
	}
}

func TestRMDAgeForCalendarYear_F078_LegacyFallback(t *testing.T) {
	s := &models.WhatIfSettings{
		StartDate:  "2026-01",
		CurrentAge: 70, // legacy fallback: birth year 1956
	}
	if got := RMDAgeForCalendarYear(s, 2030); got != 74 {
		t.Errorf("legacy RMDAgeForCalendarYear(CurrentAge=70, 2030) = %d; want 74", got)
	}
}

// Nil-safe: the helpers must not panic on nil settings (matches existing
// EffectiveRMDStartAge behaviour). FirstRMDCalendarYear and friends fall
// back to the current calendar year + 73.
func TestRMDHelpers_F078_NilSafe(t *testing.T) {
	if !RMDApplies(nil, 9999) {
		t.Errorf("RMDApplies(nil, 9999) = false; want true (nil falls through to default 73 age)")
	}
}
```

- [ ] **Step 1.2: Run the new tests; expect FAIL (helpers don't exist yet).**

Run: `go test ./internal/services/retirement/ -run "F078" -count=1 -v`
Expected: build error — `undefined: FirstRMDCalendarYear`, `undefined: RMDApplies`, `undefined: RMDAgeForCalendarYear`.

- [ ] **Step 1.3: Add the helpers to `rmd.go`.**

Append after `effectiveRMDStartAgeForBirthYear` (which currently ends at line 72):

```go
// olderBirthYear returns the older household member's birth year. Prefers
// the BirthMonth on Person records; falls back to startYear - GetOlderAge()
// for legacy callers that build settings without populating Persons. The
// older person — the one with the earlier birth year — drives the
// household's RMD timing per SECURE 2.0.
func olderBirthYear(s *models.WhatIfSettings) int {
	if s == nil {
		return time.Now().Year() - 73
	}
	if y, ok := earliestPersonBirthYear(s); ok {
		return y
	}
	return parseStartYear(s.StartDate) - s.GetOlderAge()
}

// FirstRMDCalendarYear returns the first calendar year in which the older
// household member must take an RMD under SECURE 2.0. Equals the older
// person's birth year + their applicable age (73 or 75). Anchors all
// calendar-year RMD gating so floor'd integer ages can't slip the first
// RMD year by one for late-year births.
func FirstRMDCalendarYear(s *models.WhatIfSettings) int {
	return olderBirthYear(s) + EffectiveRMDStartAge(s)
}

// RMDApplies reports whether RMD applies to the older household member in
// the given calendar year.
func RMDApplies(s *models.WhatIfSettings, calendarYear int) bool {
	return calendarYear >= FirstRMDCalendarYear(s)
}

// RMDAgeForCalendarYear returns the age the older household member attains
// by December 31 of the given calendar year. This is the age the IRS
// Uniform Lifetime Table is keyed off, so it's the age that must be passed
// to CalculateRMD — not the start-of-year floor'd age that GetOlderAge()
// returns.
func RMDAgeForCalendarYear(s *models.WhatIfSettings, calendarYear int) int {
	return calendarYear - olderBirthYear(s)
}
```

- [ ] **Step 1.4: Run the helper tests; expect PASS.**

Run: `go test ./internal/services/retirement/ -run "F078" -count=1 -v`
Expected: all six F-078 helper tests PASS.

- [ ] **Step 1.5: Commit.**

```bash
git add internal/services/retirement/rmd.go internal/services/retirement/rmd_calendar_year_test.go
git commit -m "feat(retirement): F-078 add calendar-year RMD helpers

Adds FirstRMDCalendarYear, RMDApplies, RMDAgeForCalendarYear, and
the underlying olderBirthYear helper. Pure additions — no consumers
wired yet (subsequent commits replace the floor'd-age gates one site
at a time).
"
```

---

### Task 2: Wire the projection main loop (calculator.go:1112)

**Files:**
- Modify: `internal/services/retirement/calculator.go:1069-1117`
- Modify: `internal/services/retirement/rmd_birth_year_test.go` (add 1959-12 integration case)

- [ ] **Step 2.1: Add a failing integration test for the 1959-12 case.**

Open `internal/services/retirement/rmd_birth_year_test.go` and append at the bottom of the file (after the existing `TestEffectiveRMDStartAge_F077Fixup_LegacyFallbackUnchanged` test):

```go
// F-078: a primary born 1959-12 with StartDate=2026-01 must trigger RMD in
// calendar year 2032 (year offset 6) — they attain age 73 in Dec 2032,
// applicable age 73, first RMD year = 1959 + 73 = 2032. Pre-F-078 the
// projection gate read olderAge=72 in year 6 and slipped RMD to 2033.
// The trigger-year divisor must use age 73 (UL Table factor 26.5),
// not the floor'd age 72 (factor 27.4).
func TestProjection_F078_Born1959_12_TriggersRMDIn2032(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: "1959-12"},
	}
	s.ComputeAges()
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 9
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := NewCalculator(s).RunProjection()
	if proj == nil || len(proj.Months) < 7*12 {
		t.Fatalf("nil/short projection: months=%d", func() int {
			if proj == nil {
				return 0
			}
			return len(proj.Months)
		}())
	}

	// Year offset 5 (calendar 2031, age 72): NO RMD.
	for m := 5 * 12; m < 6*12; m++ {
		if proj.Months[m].RMDWithdrawal != 0 {
			t.Errorf("month %d (year 5, calendar 2031) RMDWithdrawal = %.2f; want 0",
				m, proj.Months[m].RMDWithdrawal)
		}
	}

	// Year offset 6 (calendar 2032, attains 73): RMD must fire.
	year6Total := 0.0
	for m := 6 * 12; m < 7*12; m++ {
		year6Total += proj.Months[m].RMDWithdrawal
	}
	if year6Total <= 0 {
		t.Fatalf("year-6 (calendar 2032) total RMDWithdrawal = %.2f; want > 0 (born 1959-12, attains 73 in 2032)", year6Total)
	}

	// Divisor must use age 73 (factor 26.5), not 72 (27.4). With 0% returns
	// and 0 living expenses, the year-6 starting tax-deferred balance equals
	// PortfolioValue ($1,000,000); RMD ≈ $37,735.85 (1M / 26.5). Allow a
	// $100 slack for monthly compounding effects in the trigger month.
	expected := 1_000_000.0 / 26.5
	if math.Abs(year6Total-expected) > 100 {
		t.Errorf("year-6 RMD = %.2f; want ~%.2f (age-73 divisor 26.5)", year6Total, expected)
	}
}
```

The `math` import is already present in this file (used by `TestProjection_F077_BornAfter1959ReachesRMDAt75`).

- [ ] **Step 2.2: Run the new integration test; expect FAIL.**

Run: `go test ./internal/services/retirement/ -run TestProjection_F078_Born1959_12_TriggersRMDIn2032 -count=1 -v`
Expected: FAIL — year-6 RMDWithdrawal is 0 (gate fires in year 7 instead).

- [ ] **Step 2.3: Wire the projection loop to use `RMDApplies` and `RMDAgeForCalendarYear`.**

In `internal/services/retirement/calculator.go`, locate the year-boundary block at line 1107-1117 inside `RunProjection`. The surrounding context starts at line 1069 (`for m := 0; m < months; m++ {`).

First, add a `startYear` variable just before the loop. Find line 1069 and the block immediately preceding it; locate the existing `currentYear := m / 12` at line 1070. Replace lines 1107-1116 (the F-074/F-075 RMD-gate block):

Old:
```go
			// F-074: compute annualRMD once per year on year-start tax-deferred
			// balance (matches IRS "December 31 prior year" rule). Per-month
			// monthlyRMD is set inside the month loop based on RMDTiming.
			// F-075: gate on EffectiveRMDStartAge (75 for 2033+ projections per
			// SECURE 2.0) so projection matches the BuildRMDAnalysis panel.
			if olderAge >= EffectiveRMDStartAge(s) && taxDeferredBalance > 0 {
				annualRMD, _ = CalculateRMD(taxDeferredBalance, olderAge)
			} else {
				annualRMD = 0
			}
			monthlyRMD = 0
```

New:
```go
			// F-074: compute annualRMD once per year on year-start tax-deferred
			// balance (matches IRS "December 31 prior year" rule). Per-month
			// monthlyRMD is set inside the month loop based on RMDTiming.
			// F-078: gate on calendar year vs FirstRMDCalendarYear and pass
			// age-at-year-end to CalculateRMD so late-year births attain
			// age 73 (or 75) in the right calendar year and the divisor
			// reads the correct UL Table row.
			calendarYear := parseStartYear(s.StartDate) + currentYear
			if RMDApplies(s, calendarYear) && taxDeferredBalance > 0 {
				annualRMD, _ = CalculateRMD(taxDeferredBalance, RMDAgeForCalendarYear(s, calendarYear))
			} else {
				annualRMD = 0
			}
			monthlyRMD = 0
```

Note: `olderAge` is still computed at line 1074 and used elsewhere in the loop (chart annotations, age-based phase logic) — leave it alone. We're only changing the RMD gate and divisor.

- [ ] **Step 2.4: Run the integration test; expect PASS.**

Run: `go test ./internal/services/retirement/ -run TestProjection_F078_Born1959_12_TriggersRMDIn2032 -count=1 -v`
Expected: PASS.

- [ ] **Step 2.5: Run all retirement tests to catch regressions.**

Run: `go test ./internal/services/retirement/... -count=1`
Expected: PASS. If any pre-existing F-077/F-075 test breaks here, *stop* and inspect — the most likely culprit is a test that hard-codes `olderAge >= 73` semantics with a mid-year birthday.

- [ ] **Step 2.6: Commit.**

```bash
git add internal/services/retirement/calculator.go internal/services/retirement/rmd_birth_year_test.go
git commit -m "fix(retirement): F-078 wire projection loop to RMDApplies + age-at-year-end

Replaces 'olderAge >= EffectiveRMDStartAge(s)' at calculator.go:1112 with
calendar-year gating via RMDApplies(s, startYear+currentYear). Divisor
now uses RMDAgeForCalendarYear so a person born 1959-12 takes their
first RMD in calendar 2032 with the age-73 factor (not age-72).
"
```

---

### Task 3: Wire the Monte Carlo loop (calculator.go:2426)

**Files:**
- Modify: `internal/services/retirement/calculator.go:2380-2431`

- [ ] **Step 3.1: Replace the MC RMD gate.**

In the MC loop (`runSingleSimulation` body), find the year-boundary block at line 2380-2431. Locate lines 2423-2430:

Old:
```go
			// F-074: see PR 2 — annualRMD computed once per year, applied
			// only in the trigger month inside the month loop.
			// F-075: gate on EffectiveRMDStartAge (75 for 2033+ per SECURE 2.0).
			if olderAge >= EffectiveRMDStartAge(s) && taxDeferredBalance > 0 {
				annualRMD, _ = CalculateRMD(taxDeferredBalance, olderAge)
			} else {
				annualRMD = 0
			}
			monthlyRMD = 0
```

New:
```go
			// F-074: see PR 2 — annualRMD computed once per year, applied
			// only in the trigger month inside the month loop.
			// F-078: calendar-year gate + age-at-year-end divisor so MC
			// matches the deterministic projection for late-year births.
			calendarYear := parseStartYear(s.StartDate) + currentYear
			if RMDApplies(s, calendarYear) && taxDeferredBalance > 0 {
				annualRMD, _ = CalculateRMD(taxDeferredBalance, RMDAgeForCalendarYear(s, calendarYear))
			} else {
				annualRMD = 0
			}
			monthlyRMD = 0
```

`currentYear` is already defined at line 2385 (`currentYear := m / 12`).

- [ ] **Step 3.2: Run retirement tests.**

Run: `go test ./internal/services/retirement/... -count=1`
Expected: PASS.

- [ ] **Step 3.3: Commit.**

```bash
git add internal/services/retirement/calculator.go
git commit -m "fix(retirement): F-078 wire Monte Carlo loop to calendar-year gate

Mirrors the projection-engine fix in runSingleSimulation. Without this
the MC tail risk metrics would diverge from the deterministic projection
for any household with a late-year birth crossing a SECURE 2.0 cusp.
"
```

---

### Task 4: Wire the historical backtest loop (backtest.go:250)

**Files:**
- Modify: `internal/services/retirement/backtest.go:240-255`

- [ ] **Step 4.1: Confirm `currentYear` is available in scope.**

Read `internal/services/retirement/backtest.go` lines 200-260. Confirm `currentYear := m / 12` (or equivalent) is in scope at line 250. (It is — same loop pattern as calculator.go.)

- [ ] **Step 4.2: Replace the backtest RMD gate.**

Old (lines 247-255):
```go
			// F-074: see PR 2 — annualRMD computed once per year, applied
			// only in the trigger month inside the month loop.
			// F-075: gate on EffectiveRMDStartAge (75 for 2033+ per SECURE 2.0).
			if olderAge >= EffectiveRMDStartAge(s) && taxDeferredBalance > 0 {
				annualRMD, _ = CalculateRMD(taxDeferredBalance, olderAge)
			} else {
				annualRMD = 0
			}
			monthlyRMD = 0
```

New:
```go
			// F-074: see PR 2 — annualRMD computed once per year, applied
			// only in the trigger month inside the month loop.
			// F-078: calendar-year gate + age-at-year-end divisor.
			calendarYear := parseStartYear(s.StartDate) + currentYear
			if RMDApplies(s, calendarYear) && taxDeferredBalance > 0 {
				annualRMD, _ = CalculateRMD(taxDeferredBalance, RMDAgeForCalendarYear(s, calendarYear))
			} else {
				annualRMD = 0
			}
			monthlyRMD = 0
```

- [ ] **Step 4.3: Run retirement tests.**

Run: `go test ./internal/services/retirement/... -count=1`
Expected: PASS.

- [ ] **Step 4.4: Commit.**

```bash
git add internal/services/retirement/backtest.go
git commit -m "fix(retirement): F-078 wire historical backtest to calendar-year gate"
```

---

### Task 5: Wire the first-year RMD snapshot (calculator.go:1469)

This is the *current-year* immediate snapshot used by the dashboard cards (BuildCurrentMonthlySnapshot). Calendar year = projection start year.

**Files:**
- Modify: `internal/services/retirement/calculator.go:1465-1473`

- [ ] **Step 5.1: Replace the first-year gate.**

Old (lines 1465-1473):
```go
	// Calculate RMD if older spouse has reached the effective RMD start age
	// (73 pre-2033, 75 from 2033 onward per SECURE 2.0 — F-075).
	monthlyRMD := 0.0
	olderAge := s.GetOlderAge()
	if olderAge >= EffectiveRMDStartAge(s) && s.TaxDeferredPercent > 0 {
		taxDeferredBalance := s.PortfolioValue * (s.TaxDeferredPercent / 100)
		annualRMD, _ := CalculateRMD(taxDeferredBalance, olderAge)
		monthlyRMD = annualRMD / 12
	}
```

New:
```go
	// F-078: first-year snapshot gates on calendar year vs FirstRMDCalendarYear
	// and uses RMDAgeForCalendarYear so a household where the older person
	// turns 73 later this calendar year still produces a non-zero current
	// snapshot (and uses the right UL Table row).
	monthlyRMD := 0.0
	currentCalendarYear := parseStartYear(s.StartDate)
	olderAge := s.GetOlderAge()
	if RMDApplies(s, currentCalendarYear) && s.TaxDeferredPercent > 0 {
		taxDeferredBalance := s.PortfolioValue * (s.TaxDeferredPercent / 100)
		annualRMD, _ := CalculateRMD(taxDeferredBalance, RMDAgeForCalendarYear(s, currentCalendarYear))
		monthlyRMD = annualRMD / 12
	}
```

`olderAge` is left in scope because the surrounding code uses it elsewhere in the snapshot block (it's referenced beyond the RMD gate).

- [ ] **Step 5.2: Run retirement tests.**

Run: `go test ./internal/services/retirement/... -count=1`
Expected: PASS.

- [ ] **Step 5.3: Commit.**

```bash
git add internal/services/retirement/calculator.go
git commit -m "fix(retirement): F-078 wire first-year RMD snapshot to calendar-year gate"
```

---

### Task 6: Wire steady-state and lookback RMDs (calculator.go:1587, 1607)

These are projected snapshots inside `BuildRetirementAnalysis` — at steady-state month and 24 months prior (IRMAA lookback).

**Files:**
- Modify: `internal/services/retirement/calculator.go:1582-1612`

- [ ] **Step 6.1: Replace the steady-state gate.**

Old (lines 1582-1593):
```go
		// Calculate RMD at steady state age (uses older person's age).
		// F-075: gate on EffectiveRMDStartAge so 2033+ projections honor the
		// SECURE 2.0 age-75 threshold here too.
		steadyStateOlderAge := s.GetOlderAge() + (steadyStateMonth / 12)
		estimatedTaxDeferred := 0.0
		if steadyStateOlderAge >= EffectiveRMDStartAge(s) && s.TaxDeferredPercent > 0 {
			// Estimate tax-deferred balance at steady state (simplified: assume growth only)
			estimatedTaxDeferred = s.PortfolioValue * (s.TaxDeferredPercent / 100) *
				math.Pow(1+effectiveReturn/100, yearsToSteadyState)
			annualRMD, _ := CalculateRMD(estimatedTaxDeferred, steadyStateOlderAge)
			result.SteadyStateRMD = annualRMD / 12
		}
```

New:
```go
		// F-078: gate on calendar year + use age-at-year-end for the divisor.
		steadyStateCalendarYear := parseStartYear(s.StartDate) + (steadyStateMonth / 12)
		steadyStateOlderAge := s.GetOlderAge() + (steadyStateMonth / 12)
		estimatedTaxDeferred := 0.0
		if RMDApplies(s, steadyStateCalendarYear) && s.TaxDeferredPercent > 0 {
			// Estimate tax-deferred balance at steady state (simplified: assume growth only)
			estimatedTaxDeferred = s.PortfolioValue * (s.TaxDeferredPercent / 100) *
				math.Pow(1+effectiveReturn/100, yearsToSteadyState)
			annualRMD, _ := CalculateRMD(estimatedTaxDeferred, RMDAgeForCalendarYear(s, steadyStateCalendarYear))
			result.SteadyStateRMD = annualRMD / 12
		}
```

`steadyStateOlderAge` is preserved because the surrounding code may reference it later in the function.

- [ ] **Step 6.2: Replace the lookback gate.**

Old (lines 1603-1612):
```go
			lookbackOlderAge := s.GetOlderAge() + (lookbackMonth / 12)
			lookbackTaxDeferred := 0.0
			lookbackRMD := 0.0
			// F-075: gate on EffectiveRMDStartAge for SECURE 2.0 2033+ rule.
			if lookbackOlderAge >= EffectiveRMDStartAge(s) && s.TaxDeferredPercent > 0 {
				lookbackTaxDeferred = s.PortfolioValue * (s.TaxDeferredPercent / 100) *
					math.Pow(1+effectiveReturn/100, yearsToLookback)
				annualRMD, _ := CalculateRMD(lookbackTaxDeferred, lookbackOlderAge)
				lookbackRMD = annualRMD / 12
			}
```

New:
```go
			lookbackCalendarYear := parseStartYear(s.StartDate) + (lookbackMonth / 12)
			lookbackOlderAge := s.GetOlderAge() + (lookbackMonth / 12)
			lookbackTaxDeferred := 0.0
			lookbackRMD := 0.0
			// F-078: calendar-year gate + age-at-year-end divisor.
			if RMDApplies(s, lookbackCalendarYear) && s.TaxDeferredPercent > 0 {
				lookbackTaxDeferred = s.PortfolioValue * (s.TaxDeferredPercent / 100) *
					math.Pow(1+effectiveReturn/100, yearsToLookback)
				annualRMD, _ := CalculateRMD(lookbackTaxDeferred, RMDAgeForCalendarYear(s, lookbackCalendarYear))
				lookbackRMD = annualRMD / 12
			}
```

- [ ] **Step 6.3: Run retirement tests.**

Run: `go test ./internal/services/retirement/... -count=1`
Expected: PASS.

- [ ] **Step 6.4: Commit.**

```bash
git add internal/services/retirement/calculator.go
git commit -m "fix(retirement): F-078 wire steady-state + lookback RMDs to calendar gate"
```

---

### Task 7: Wire the chart event timeline label (handlers.go:222)

**Files:**
- Modify: `internal/handlers/whatif/handlers.go:218-224`
- Modify: `internal/handlers/whatif/handlers_test.go` (if a "RMD starts" label test exists; verify and adapt)

- [ ] **Step 7.1: Check for an existing chart-label test.**

Run: `grep -n "RMD starts" /home/darrell/bin/ai/budget2/internal/handlers/whatif/handlers_test.go`
Expected: at least one hit (the F-075 test mentioned at handlers_test.go:2985).

Read whichever test asserts the "RMD starts" label and confirm what it currently asserts. If it hard-codes `effectiveStart - olderAge`, plan to update it in Step 7.4.

- [ ] **Step 7.2: Add a failing test for the 1959-12 chart label.**

In `internal/handlers/whatif/handlers_test.go`, append:

```go
// F-078: the "RMD starts" event-timeline label must use the calendar year
// of first RMD (FirstRMDCalendarYear), not floor'd-age arithmetic. For a
// primary born 1959-12 with StartDate=2026-01, RMDs start in calendar
// year 2032 → 6 years from start, not 7.
func TestProjectionChartEvents_F078_RMDStartsLabel_LateYearBirth(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: "1959-12"},
	}
	s.ComputeAges()
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.ProjectionYears = 10

	calc := retirement.NewCalculator(s)
	proj := calc.RunProjection()
	events := buildProjectionChartEvents(s, proj)

	for _, e := range events {
		if e.Label == "RMD starts" {
			if e.Year != 6 {
				t.Errorf("RMD starts event year = %.2f; want 6 (born 1959-12, first RMD calendar 2032)", e.Year)
			}
			return
		}
	}
	t.Fatalf("no 'RMD starts' event in %+v", events)
}
```

Imports needed: `"budget2/internal/services/retirement"` and `"budget2/internal/models"`. Both should already be imported in this test file. If not, add them.

- [ ] **Step 7.3: Run the test; expect FAIL.**

Run: `go test ./internal/handlers/whatif/ -run TestProjectionChartEvents_F078_RMDStartsLabel_LateYearBirth -count=1 -v`
Expected: FAIL — current code emits Year=7 (effectiveStart=73 minus olderAge=66 = 7).

- [ ] **Step 7.4: Replace the chart-label gate.**

In `internal/handlers/whatif/handlers.go`, locate lines 218-224. Replace:

Old:
```go
	// F-075: use EffectiveRMDStartAge (75 for 2033+ projections per SECURE 2.0)
	// so the timeline label matches BuildRMDAnalysis and the projection engine.
	olderAge := settings.GetOlderAge()
	effectiveStart := retirement.EffectiveRMDStartAge(settings)
	if olderAge < effectiveStart {
		appendEvent(float64(effectiveStart-olderAge), "RMD starts")
	}
```

New:
```go
	// F-078: use calendar-year arithmetic so late-year births land on the
	// right offset. FirstRMDCalendarYear knows about BirthMonth; floor'd
	// age subtraction does not.
	startYear := parseProjectionStartYear(settings.StartDate)
	firstRMDYear := retirement.FirstRMDCalendarYear(settings)
	if firstRMDYear > startYear {
		appendEvent(float64(firstRMDYear-startYear), "RMD starts")
	}
```

If `parseProjectionStartYear` doesn't exist in the handlers package, add a small helper near the top of `handlers.go` (just above `buildProjectionChartEvents`):

```go
// parseProjectionStartYear extracts the year from a "YYYY-MM" StartDate.
// Falls back to the current calendar year on parse failure (mirrors
// retirement.parseStartYear, which is unexported in that package).
func parseProjectionStartYear(startDate string) int {
	if startDate == "" {
		return time.Now().Year()
	}
	t, err := time.Parse("2006-01", startDate)
	if err != nil {
		return time.Now().Year()
	}
	return t.Year()
}
```

Confirm `"time"` is imported in `handlers.go`. (It already is — search the imports block at the top of the file.)

- [ ] **Step 7.5: Audit any pre-existing chart-label test.**

If Step 7.1 found a test asserting `Year=N` for a synthetic late-year-birth setup, run it and verify it still passes. If it was the only test and it asserted the buggy 7-year offset for a 1959-12 birth, update its expected `Year` to 6 (the right answer) and add a comment referencing F-078. If it tested an early-year birth (e.g., 1959-01 or 1965-06), it should keep passing unchanged.

- [ ] **Step 7.6: Run the new test + full whatif suite.**

Run: `go test ./internal/handlers/whatif/ -run TestProjectionChartEvents_F078 -count=1 -v`
Expected: PASS.

Run: `go test ./internal/handlers/whatif/... -count=1`
Expected: PASS (no regressions).

- [ ] **Step 7.7: Commit.**

```bash
git add internal/handlers/whatif/handlers.go internal/handlers/whatif/handlers_test.go
git commit -m "fix(whatif): F-078 wire chart 'RMD starts' label to calendar-year gate"
```

---

### Task 8: Wire `BuildRMDAnalysis` (rmd.go:192 + rmd.go:233-234)

The RMD analysis panel has two related defects:
1. `startsInYears := effectiveStartAge - olderAge` (line 192) — the same off-by-one.
2. The row-emit loop at line 232-235 uses `age := olderAge + y` and skips when `age < effectiveStartAge`.

**Files:**
- Modify: `internal/services/retirement/rmd.go:182-275`

- [ ] **Step 8.1: Add a failing test for late-1959 BuildRMDAnalysis.**

Append to `internal/services/retirement/rmd_calendar_year_test.go`:

```go
// F-078: BuildRMDAnalysis startsInYears must reflect the calendar-year
// first RMD year, not floor'd-age subtraction. For 1959-12 + 2026-01
// start, startsInYears = 6 (calendar 2032 - calendar 2026), not 7.
func TestBuildRMDAnalysis_F078_StartsInYearsLateYearBirth(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: "1959-12"},
	}
	s.ComputeAges()
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.ProjectionYears = 10

	calc := NewCalculator(s)
	proj := calc.RunProjection()
	analysis := calc.BuildRMDAnalysis(proj)
	if analysis == nil {
		t.Fatalf("BuildRMDAnalysis returned nil")
	}
	if analysis.StartsInYears != 6 {
		t.Errorf("StartsInYears = %d; want 6 (born 1959-12, first RMD calendar 2032 = 6 years from 2026)", analysis.StartsInYears)
	}
	if analysis.StartAge != 73 {
		t.Errorf("StartAge = %d; want 73", analysis.StartAge)
	}
	if len(analysis.Projections) == 0 {
		t.Fatalf("no Projections rows emitted")
	}
	first := analysis.Projections[0]
	if first.Age != 73 {
		t.Errorf("first row Age = %d; want 73", first.Age)
	}
	if first.Year != 6 {
		t.Errorf("first row Year = %d; want 6", first.Year)
	}
}
```

- [ ] **Step 8.2: Run the test; expect FAIL.**

Run: `go test ./internal/services/retirement/ -run TestBuildRMDAnalysis_F078_StartsInYearsLateYearBirth -count=1 -v`
Expected: FAIL — `StartsInYears = 7` and `first.Age = 72` (or first row missing entirely).

- [ ] **Step 8.3: Update `BuildRMDAnalysis`.**

In `internal/services/retirement/rmd.go`, replace the function header through the row-emit loop. Old (lines 186-241, abbreviated):

```go
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
	// ... (taxDeferredValue == 0 / projection nil guards unchanged) ...

	rmdCount := 0
	for y := 0; y < maxYears && rmdCount < 20; y++ {
		age := olderAge + y
		if age < effectiveStartAge {
			continue
		}
		// ... rest of loop ...
	}
```

New (only the gate and `age` derivation change — the rest of the function body stays identical):

```go
func (c *Calculator) BuildRMDAnalysis(projection *models.ProjectionResult) *models.RMDAnalysis {
	s := c.Settings

	taxDeferredValue := s.PortfolioValue * (s.TaxDeferredPercent / 100)
	effectiveStartAge := EffectiveRMDStartAge(s)
	olderAge := s.GetOlderAge()
	startYear := parseStartYear(s.StartDate)
	firstRMDYear := FirstRMDCalendarYear(s)
	// F-078: startsInYears = calendar gap to first RMD year, not floor'd-age
	// subtraction. Late-year births differ by one.
	startsInYears := firstRMDYear - startYear
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
```

Then update the row-emit loop. Locate lines 231-236:

Old:
```go
	rmdCount := 0
	for y := 0; y < maxYears && rmdCount < 20; y++ {
		age := olderAge + y
		if age < effectiveStartAge {
			continue
		}
```

New:
```go
	rmdCount := 0
	for y := 0; y < maxYears && rmdCount < 20; y++ {
		calendarYear := startYear + y
		if !RMDApplies(s, calendarYear) {
			continue
		}
		age := RMDAgeForCalendarYear(s, calendarYear)
```

This single change fixes both the per-row `Age` (now reflects age-at-year-end) and the row-emit gate.

Note: the `factor := GetLifeExpectancyFactor(age)` call later in the loop (around line 263) now correctly uses age-at-year-end, so the percent column on the RMD analysis panel will line up with the projection's actual RMDWithdrawal sums.

Also: there's a `da := olderAge + dy` at line 216 inside the depletion guard. This is the depletion *age* (informational), and it represents "what age will the older person be after `dy` whole years". It's fine as-is for that purpose — leave it alone.

- [ ] **Step 8.4: Run the F-078 BuildRMDAnalysis test.**

Run: `go test ./internal/services/retirement/ -run TestBuildRMDAnalysis_F078 -count=1 -v`
Expected: PASS.

- [ ] **Step 8.5: Run the full retirement suite.**

Run: `go test ./internal/services/retirement/... -count=1`
Expected: PASS. Pay attention to any F-072 / F-077 BuildRMDAnalysis tests — they should still pass because their setups use early-year births where the calendar-year and floor'd-age math agree.

- [ ] **Step 8.6: Commit.**

```bash
git add internal/services/retirement/rmd.go internal/services/retirement/rmd_calendar_year_test.go
git commit -m "fix(retirement): F-078 wire BuildRMDAnalysis to calendar-year gate

startsInYears, the row-emit loop gate, and per-row Age all now derive
from FirstRMDCalendarYear / RMDAgeForCalendarYear so the RMD panel
matches the projection engine for late-year births.
"
```

---

### Task 9: Final verification, audit doc, and merge readiness

- [ ] **Step 9.1: Run the entire test suite.**

Run: `go test ./... -count=1`
Expected: PASS across all packages. Investigate any failure before proceeding.

- [ ] **Step 9.2: Run gitnexus impact + change detection.**

Run: `npx gitnexus analyze` (refreshes the index)
Run impact analysis on `EffectiveRMDStartAge` and the new helpers via gitnexus MCP tools as specified in CLAUDE.md.
Run: `npx gitnexus detect-changes` (or the equivalent MCP tool) to confirm the affected scope matches expectations: rmd.go, calculator.go, backtest.go, handlers.go, plus their test files.

If gitnexus reports HIGH/CRITICAL risk on any symbol you didn't touch, stop and surface it.

- [ ] **Step 9.3: Manual smoke — UI dollar accuracy for the canary case.**

```bash
go run ./cmd/server &
sleep 2
```

Open `http://localhost:8080/whatif`, set:
- Primary BirthMonth: 1959-12
- Start Date: 2026-01
- Portfolio: $1,000,000 / 100% tax-deferred
- Returns/inflation: 0%

Confirm:
- Event timeline shows "RMD starts" at year 6 (not 7).
- RMD analysis panel: `StartsInYears = 6`, first row `Age = 73`, `Year = 6`.
- Year-6 (calendar 2032) projection row reports RMDWithdrawal ≈ $37,735.85.

Stop the server: `pkill -f "go run ./cmd/server"`

- [ ] **Step 9.4: Update the audit doc.**

Append an F-078 closing entry to `docs/whatif-math-audit-2026-05-05.md`. Find the F-077 entry (`grep -n "F-077" docs/whatif-math-audit-2026-05-05.md`) and add the F-078 block immediately after it. Use the same format as F-077:

```markdown
### F-078: RMD calendar-year gate (closed 2026-05-07)

**Status:** ✅ Fixed.

**Issue:** All consumers of `EffectiveRMDStartAge` compared it against
floor'd integer ages from `s.GetOlderAge() + currentYear`. For a primary
born 1959-12 with StartDate=2026-01, `GetOlderAge()` returned 66 (one
year low until the December birthday); in calendar 2032 the projection
loop saw `olderAge=72` and slipped first RMD to 2033, even though the
person attains 73 in Dec 2032 and falls under the age-73 SECURE 2.0
rule (born ≤ 1959). Same defect in MC, backtest, chart event label,
first-year snapshot, steady-state, IRMAA lookback, and BuildRMDAnalysis.
Plus a secondary divisor-age bug: even after the gate fired, the UL
Table lookup used age-at-year-start (off by 1) — ~3% under-withdrawal
in the trigger year.

**Fix:** New helpers `FirstRMDCalendarYear`, `RMDApplies`, and
`RMDAgeForCalendarYear` in `rmd.go`. All eight call sites now gate on
calendar year vs first-RMD-year and pass age-at-year-end to
`CalculateRMD`.

**Test:** `TestProjection_F078_Born1959_12_TriggersRMDIn2032` plus
helper unit tests, chart-label test, and BuildRMDAnalysis test.
```

- [ ] **Step 9.5: Commit the audit doc update.**

```bash
git add docs/whatif-math-audit-2026-05-05.md
git commit -m "docs(whatif-audit): F-078 close calendar-year RMD gate finding"
```

- [ ] **Step 9.6: Push and open PR.**

```bash
git push -u origin feat/rmd-calendar-year-gate
gh pr create --title "F-078: RMD calendar-year gate across all consumers" --body "$(cat <<'EOF'
## Summary
- Fixes the P2 finding from the F-077 review: late-year-1959 births still started RMDs one calendar year late in projection, MC, backtest, chart label, first-year snapshot, steady-state, IRMAA lookback, and BuildRMDAnalysis.
- Adds `FirstRMDCalendarYear`, `RMDApplies`, `RMDAgeForCalendarYear` helpers in `rmd.go`; all 8 call sites updated.
- Secondary fix: divisor age now reflects age-at-year-end (~3% RMD undershoot eliminated in the trigger year).

## Test plan
- [ ] `go test ./internal/services/retirement/... -count=1` PASS
- [ ] `go test ./internal/handlers/whatif/... -count=1` PASS
- [ ] `go test ./... -count=1` PASS
- [ ] Manual smoke (1959-12 birth, 2026-01 start, 100% tax-deferred): "RMD starts" event at year 6, year-6 RMDWithdrawal ≈ $37,735.85, RMD panel `StartsInYears = 6`.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Process Notes

**Per-task verification:** every task ends with `go test ./internal/services/retirement/... -count=1` (or the relevant package). If any test fails after a per-task edit, *do not* move on — debug it. The most common failure mode is a pre-existing test that happened to pass with the buggy semantics because it used a mid-year birthday where floor'd age and calendar year happen to agree.

**Helper visibility:** `parseStartYear` is package-private to `retirement`; call sites inside that package (Tasks 2-6, 8) can use it directly. The handlers package (Task 7) must use its own local helper (`parseProjectionStartYear`) — *do not* export `parseStartYear` from the retirement package just for one caller. The duplication is intentional and minimal.

**Why the divisor age matters:** `CalculateRMD(balance, age)` reads the IRS Uniform Lifetime Table by age. Age-72 factor is 27.4; age-73 is 26.5. Passing 72 in the year the person turns 73 underwithdraws by ~3.4%. In long projections this compounds — the tax-deferred balance stays ~3% higher than reality each RMD year, producing artificially small first-year RMD/IRMAA hits.

**Scope boundary:** This PR does *not* touch `EffectiveRMDStartAge` itself (that was F-077) or any RMD calculation downstream of `CalculateRMD` (the divisor table, monthly trigger month, multi-account withdrawal sequencing — all unchanged).

---

## Self-Review

**Spec coverage:** All eight call sites listed in the finding are addressed (Tasks 2-8 cover sites 1, 5, 6, 7, 8, 2, 3, 4 respectively, plus the divisor-age secondary fix). Helper-test, integration-test, chart-label-test, and BuildRMDAnalysis-test all present. Audit doc closure step included. Manual UI smoke included.

**Placeholder scan:** No "TBD", "implement later", "fill in details", or "similar to Task N" references. Each step shows actual code or actual command. Where existing test files are referenced, the test code to add is fully written out.

**Type consistency:**
- `FirstRMDCalendarYear(s *models.WhatIfSettings) int` — used as `int` in all sites (subtractions, comparisons).
- `RMDApplies(s *models.WhatIfSettings, calendarYear int) bool` — used in `if` conditions.
- `RMDAgeForCalendarYear(s *models.WhatIfSettings, calendarYear int) int` — passed to `CalculateRMD(_ float64, age int)` whose existing signature accepts `int`.
- `parseStartYear(startDate string) int` — already exists in `rmd.go:93`.
- `parseProjectionStartYear(startDate string) int` — new local helper in `handlers.go`, used only there.
- All replacements preserve the surrounding variable scope (`olderAge`, `steadyStateOlderAge`, `lookbackOlderAge` are kept where the surrounding code references them outside the RMD gate).
