# Scenario Completeness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `completeness` package + UI banner that surfaces silent zero-defaults in what-if scenarios (state tax, Social Security, MFJ-without-spouse-Person), and wire state-income-tax-rate end-to-end so the warning has a fix path.

**Architecture:** Pure-function `completeness.Check(*models.WhatIfSettings) []Finding` lives in a new package. Banner partial renders findings above the projection chart on the what-if page. State-tax wiring is bundled: form field → handler write → persistence default → UI input. Federal-vs-state breakdown display is explicitly out of scope (Phase-2). Each task is one commit; every commit must keep `go build`, `go test`, `go vet`, and the pre-commit hook green.

**Tech Stack:** Go 1.x, chi router, html/template, HTMX. No new third-party dependencies.

**Spec:** [`docs/architecture-deepening/scenario-completeness.md`](../../architecture-deepening/scenario-completeness.md)

---

## Preconditions

1. Branch `feat/projection-engine` is merged to `dev`. Confirm with `git log --oneline dev | head -3` showing `8606f1f Merge feat/projection-engine into dev`.
2. `go test ./...` is green on `dev`.
3. No active edits to `internal/services/retirement/completeness/` (the package does not yet exist).
4. No active edits to `internal/handlers/whatif/form_spec.go` or `internal/handlers/whatif/handlers_rates.go`.

## File structure (final state)

```
internal/services/retirement/
├── completeness/                     NEW
│   ├── check.go                      Severity, Finding, Check
│   ├── checks_state_tax.go           checkStateTaxUnset
│   ├── checks_ss.go                  checkSSUnconfigured, checkSSPartial
│   ├── checks_household.go           checkMFJNoSpousePerson
│   └── check_test.go                 table-driven tests per check
├── settings.go                       modified: TaxConfig defaulted in initializeLoadedSettings, applySettingsUpdates handles state_income_tax_rate

internal/handlers/whatif/
├── form_spec.go                      modified: state_income_tax_rate entry
└── handlers.go                       modified: pageData and partial data include "Findings"

web/templates/components/whatif/
├── completeness.html                 NEW partial
└── rate-assumptions.html             modified: state tax input

web/templates/pages/
└── whatif.html                       modified: render completeness partial above whatif-results, OOB updates include it
```

## Per-commit invariants

After every task:
- `go build ./...` succeeds.
- `go test ./...` passes.
- `go vet ./...` is clean.
- The pre-commit hook passes.

If any fail, fix before committing.

---

## Task 0: Branch setup

**Files:** none (git only).

- [ ] **Step 1: Confirm base state.**

Run: `git checkout dev && git pull && git status && git log --oneline -1`
Expected: clean working tree on `dev` at or near `8606f1f`.

- [ ] **Step 2: Create the feature branch.**

Run: `git checkout -b feat/scenario-completeness`
Expected: switched to new branch.

- [ ] **Step 3: Verify green baseline.**

Run: `go test ./...`
Expected: all packages pass.

No commit on this task.

---

## Task 1: completeness package skeleton + state-tax check (TDD)

**Files:**
- Create: `internal/services/retirement/completeness/check.go`
- Create: `internal/services/retirement/completeness/checks_state_tax.go`
- Create: `internal/services/retirement/completeness/check_test.go`

- [ ] **Step 1: Write the failing test.**

Create `internal/services/retirement/completeness/check_test.go`:

```go
package completeness

import (
	"testing"

	"budget2/internal/models"
)

func TestCheck_StateTaxUnset(t *testing.T) {
	cases := []struct {
		name      string
		settings  *models.WhatIfSettings
		wantCode  string
		wantFound bool
	}{
		{
			name:      "nil TaxConfig emits state_tax_unset",
			settings:  &models.WhatIfSettings{TaxConfig: nil},
			wantCode:  "state_tax_unset",
			wantFound: true,
		},
		{
			name: "zero StateIncomeTaxRate emits state_tax_unset",
			settings: &models.WhatIfSettings{
				TaxConfig: &models.TaxConfig{StateIncomeTaxRate: 0.0},
			},
			wantCode:  "state_tax_unset",
			wantFound: true,
		},
		{
			name: "non-zero StateIncomeTaxRate emits no state_tax finding",
			settings: &models.WhatIfSettings{
				TaxConfig: &models.TaxConfig{StateIncomeTaxRate: 5.0},
			},
			wantCode:  "state_tax_unset",
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Check(tc.settings)
			if got := hasCode(findings, tc.wantCode); got != tc.wantFound {
				t.Fatalf("Check() finding %q present = %v, want %v (got %d findings)",
					tc.wantCode, got, tc.wantFound, len(findings))
			}
		})
	}
}

func TestCheck_StateTaxFindingShape(t *testing.T) {
	settings := &models.WhatIfSettings{TaxConfig: nil}
	findings := Check(settings)

	f := findByCode(findings, "state_tax_unset")
	if f == nil {
		t.Fatal("expected state_tax_unset finding, got none")
	}
	if f.Severity != SeverityWarn {
		t.Errorf("Severity = %v, want SeverityWarn", f.Severity)
	}
	if f.Title == "" || f.Detail == "" || f.Action == "" {
		t.Errorf("Finding has empty user-facing fields: %+v", f)
	}
}

func hasCode(findings []Finding, code string) bool {
	return findByCode(findings, code) != nil
}

func findByCode(findings []Finding, code string) *Finding {
	for i := range findings {
		if findings[i].Code == code {
			return &findings[i]
		}
	}
	return nil
}
```

- [ ] **Step 2: Run test to verify it fails.**

Run: `go test ./internal/services/retirement/completeness/...`
Expected: FAIL — package does not exist or `Check` undefined.

- [ ] **Step 3: Write the package skeleton.**

Create `internal/services/retirement/completeness/check.go`:

```go
// Package completeness inspects a WhatIfSettings for silent zero-defaults
// and other invariant violations that would produce mathematically valid
// but materially incomplete projections. Findings surface to the user via
// a banner above the projection chart so the omission is visible before
// they trust the numbers.
package completeness

import "budget2/internal/models"

// Severity ranks findings from informational to outright inconsistent.
// SeverityError means the projection is internally inconsistent (e.g.
// MFJ filing status with no spouse Person — taxes are computed for two,
// IRMAA / RMD for one). Warn means a silent zero is likely material.
// Info is discoverability only.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarn
	SeverityError
)

// Finding describes one detected issue with a scenario.
//
// Code is a stable identifier safe to use as a test fixture, telemetry
// key, or i18n lookup. FormAnchor is the fragment id (without "#") on
// the what-if page that the user can be deep-linked to in order to fix
// the issue.
type Finding struct {
	Severity   Severity
	Code       string
	Title      string
	Detail     string
	FormAnchor string
	Action     string
}

// Check returns findings ordered errors-first, then warnings, then info.
// Within a severity tier, order matches the order the checks are listed
// below (which roughly corresponds to "tax-related → income → household").
//
// Check is pure: it never mutates settings, never reads disk, never calls
// the engine. nil settings yields a single SeverityError finding so the
// banner still renders something meaningful.
func Check(s *models.WhatIfSettings) []Finding {
	if s == nil {
		return []Finding{{
			Severity: SeverityError,
			Code:     "settings_nil",
			Title:    "Scenario could not be loaded",
			Detail:   "Settings are missing. The projection cannot run.",
			Action:   "Reload the page",
		}}
	}

	var findings []Finding
	findings = appendIfPresent(findings, checkStateTaxUnset(s))
	return sortBySeverity(findings)
}

// appendIfPresent skips nil findings so individual check functions can
// return (*Finding) and let the orchestrator drop "no finding" cases.
func appendIfPresent(findings []Finding, f *Finding) []Finding {
	if f == nil {
		return findings
	}
	return append(findings, *f)
}

// sortBySeverity stable-sorts findings by descending severity. Errors
// first, then warnings, then info. Within a tier, append order is
// preserved so individual checks can document their intended order.
func sortBySeverity(findings []Finding) []Finding {
	if len(findings) < 2 {
		return findings
	}
	out := make([]Finding, 0, len(findings))
	for _, sev := range []Severity{SeverityError, SeverityWarn, SeverityInfo} {
		for _, f := range findings {
			if f.Severity == sev {
				out = append(out, f)
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Implement checkStateTaxUnset.**

Create `internal/services/retirement/completeness/checks_state_tax.go`:

```go
package completeness

import "budget2/internal/models"

// checkStateTaxUnset flags scenarios where state income tax is silently
// zero. The engine's tax calculator reads TaxConfig.StateIncomeTaxRate
// and applies state tax correctly when it is non-zero — the gap is that
// nothing prompts the user to set it. A user in a tax state will see
// federal tax modeled but state tax silently absent.
//
// nil TaxConfig and zero rate are both treated as "unset". They produce
// the same outcome (no state tax computed), so the user sees one
// uniform warning.
func checkStateTaxUnset(s *models.WhatIfSettings) *Finding {
	if s.TaxConfig != nil && s.TaxConfig.StateIncomeTaxRate > 0 {
		return nil
	}
	return &Finding{
		Severity:   SeverityWarn,
		Code:       "state_tax_unset",
		Title:      "No state income tax configured",
		Detail:     "Projections currently model federal tax only. If you live in a state with income tax, your after-tax balances are overstated.",
		FormAnchor: "rate-assumptions-card",
		Action:     "Set state tax rate",
	}
}
```

- [ ] **Step 5: Run tests to verify they pass.**

Run: `go test ./internal/services/retirement/completeness/...`
Expected: PASS — `TestCheck_StateTaxUnset` and `TestCheck_StateTaxFindingShape` both green.

- [ ] **Step 6: Verify build and full test suite still green.**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all green.

- [ ] **Step 7: Commit.**

```bash
git add internal/services/retirement/completeness/
git commit -m "feat(completeness): add scenario-completeness package + state-tax check

Pure Check(*WhatIfSettings) []Finding that detects silent zero-defaults.
First check: state_tax_unset (Warn) when TaxConfig is nil or
StateIncomeTaxRate is 0. Findings sorted errors-first.

Closes the silent-zero discovery part of the state-tax bug; the wiring
to make state tax editable lands in a later commit on this branch."
```

---

## Task 2: SS unconfigured + SS partial checks

**Files:**
- Create: `internal/services/retirement/completeness/checks_ss.go`
- Modify: `internal/services/retirement/completeness/check.go` (wire new checks into Check)
- Modify: `internal/services/retirement/completeness/check_test.go` (add SS test cases)

- [ ] **Step 1: Add failing tests for SS checks.**

Append to `internal/services/retirement/completeness/check_test.go`:

```go
func TestCheck_SSUnconfigured(t *testing.T) {
	cases := []struct {
		name      string
		settings  *models.WhatIfSettings
		wantFound bool
	}{
		{
			name: "nil SocialSecurity with primary age >= 50 emits ss_unconfigured",
			settings: &models.WhatIfSettings{
				StartDate: "2026-01",
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"}, // age ~56 in 2026
				},
				SocialSecurity: nil,
			},
			wantFound: true,
		},
		{
			name: "nil SocialSecurity with primary age 30 does not emit ss_unconfigured",
			settings: &models.WhatIfSettings{
				StartDate: "2026-01",
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1996-01"}, // age ~30 in 2026
				},
				SocialSecurity: nil,
			},
			wantFound: false,
		},
		{
			name: "configured SocialSecurity does not emit ss_unconfigured",
			settings: &models.WhatIfSettings{
				StartDate: "2026-01",
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
				},
				SocialSecurity: &models.SocialSecurityConfig{
					FRABenefit: 2500,
					ClaimAge:   67,
				},
			},
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Check(tc.settings)
			if got := hasCode(findings, "ss_unconfigured"); got != tc.wantFound {
				t.Fatalf("ss_unconfigured present = %v, want %v", got, tc.wantFound)
			}
		})
	}
}

func TestCheck_SSPartial(t *testing.T) {
	cases := []struct {
		name      string
		settings  *models.WhatIfSettings
		wantFound bool
	}{
		{
			name: "FRABenefit set, ClaimAge zero emits ss_partial",
			settings: &models.WhatIfSettings{
				StartDate: "2026-01",
				Persons:   []models.Person{{Role: models.PersonRolePrimary, BirthMonth: "1970-01"}},
				SocialSecurity: &models.SocialSecurityConfig{
					FRABenefit: 2500,
					ClaimAge:   0,
				},
			},
			wantFound: true,
		},
		{
			name: "SpouseFRABenefit set, SpouseClaimAge zero emits ss_partial",
			settings: &models.WhatIfSettings{
				StartDate: "2026-01",
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
					{Role: models.PersonRoleSpouse, BirthMonth: "1972-01"},
				},
				SocialSecurity: &models.SocialSecurityConfig{
					FRABenefit:       2500,
					ClaimAge:         67,
					SpouseFRABenefit: 1800,
					SpouseClaimAge:   0,
				},
			},
			wantFound: true,
		},
		{
			name: "fully configured SS does not emit ss_partial",
			settings: &models.WhatIfSettings{
				StartDate: "2026-01",
				Persons:   []models.Person{{Role: models.PersonRolePrimary, BirthMonth: "1970-01"}},
				SocialSecurity: &models.SocialSecurityConfig{
					FRABenefit: 2500,
					ClaimAge:   67,
				},
			},
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Check(tc.settings)
			if got := hasCode(findings, "ss_partial"); got != tc.wantFound {
				t.Fatalf("ss_partial present = %v, want %v", got, tc.wantFound)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail.**

Run: `go test ./internal/services/retirement/completeness/...`
Expected: FAIL — `ss_unconfigured` and `ss_partial` codes not produced.

- [ ] **Step 3: Implement SS checks.**

Create `internal/services/retirement/completeness/checks_ss.go`:

```go
package completeness

import (
	"time"

	"budget2/internal/models"
)

// ssAttentionAge is the age at which we expect a user to start thinking
// about Social Security. Below this, the absence of SS configuration is
// not flagged — many users build long-horizon scenarios decades before
// claiming and shouldn't be nagged.
const ssAttentionAge = 50

// checkSSUnconfigured flags scenarios where Social Security is entirely
// absent (nil pointer) and at least one Person is at or near claiming
// age. The engine silently produces zero SS income when SocialSecurity
// is nil — for retirees this can mean tens of thousands of dollars per
// year of missing income with no visual indication.
//
// We only flag when a Person is age >= 50 because younger users
// modelling far-out retirements legitimately may not yet know their
// FRA benefit.
func checkSSUnconfigured(s *models.WhatIfSettings) *Finding {
	if s.SocialSecurity != nil {
		return nil
	}
	if !anyPersonAtLeast(s, ssAttentionAge) {
		return nil
	}
	return &Finding{
		Severity:   SeverityWarn,
		Code:       "ss_unconfigured",
		Title:      "Social Security not configured",
		Detail:     "No FRA benefit or claim age set. The projection assumes zero Social Security income, which can underestimate retirement income by hundreds of thousands of dollars over a 30-year horizon.",
		FormAnchor: "whatif-social-security-card",
		Action:     "Add Social Security",
	}
}

// checkSSPartial flags scenarios where SS is partially configured —
// a benefit is entered but no claim age, or vice versa. The engine
// guards against this case by returning zero income when ClaimAge is
// zero, but the user has clearly intended to configure SS, so silent
// zero is a likely surprise.
//
// One finding covers both primary and spouse — emitted if either side
// is partial.
func checkSSPartial(s *models.WhatIfSettings) *Finding {
	if s.SocialSecurity == nil {
		return nil
	}
	ss := s.SocialSecurity

	primaryPartial := ss.FRABenefit > 0 && ss.ClaimAge == 0
	spousePartial := ss.SpouseFRABenefit > 0 && ss.SpouseClaimAge == 0

	if !primaryPartial && !spousePartial {
		return nil
	}
	return &Finding{
		Severity:   SeverityWarn,
		Code:       "ss_partial",
		Title:      "Social Security claim age missing",
		Detail:     "A Social Security benefit is configured but no claim age is set. The engine treats this as zero income — the entered benefit has no effect until you also pick a claim age.",
		FormAnchor: "whatif-social-security-card",
		Action:     "Set claim age",
	}
}

// anyPersonAtLeast returns true if any Person in settings is at or
// above the given age as of the scenario's start date.
//
// Falls back to settings.CurrentAge / SpouseAge when the Person record
// lacks BirthMonth (legacy scenarios). This is intentional: legacy
// scenarios should still trigger the warning rather than silently slip
// through because BirthMonth is the empty string.
func anyPersonAtLeast(s *models.WhatIfSettings, minAge int) bool {
	startYear := parseStartYear(s.StartDate)
	for _, p := range s.Persons {
		if personAge(p, startYear) >= minAge {
			return true
		}
	}
	if s.CurrentAge >= minAge {
		return true
	}
	if s.SpouseAge >= minAge {
		return true
	}
	return false
}

// personAge computes the age in (whole) years at the given reference
// year. BirthMonth is "YYYY-MM"; if it cannot be parsed we return 0
// so the Person doesn't trigger age-gated checks (the CurrentAge
// fallback in anyPersonAtLeast still applies).
func personAge(p models.Person, refYear int) int {
	if len(p.BirthMonth) < 4 {
		return 0
	}
	birthYear, err := atoiFour(p.BirthMonth[:4])
	if err != nil {
		return 0
	}
	return refYear - birthYear
}

func parseStartYear(startDate string) int {
	if len(startDate) < 4 {
		return time.Now().Year()
	}
	y, err := atoiFour(startDate[:4])
	if err != nil {
		return time.Now().Year()
	}
	return y
}

// atoiFour parses exactly four ASCII digits. We avoid strconv.Atoi to
// keep the function allocation-free and to refuse anything that isn't
// a clean YYYY (e.g. "20xx").
func atoiFour(s string) (int, error) {
	if len(s) != 4 {
		return 0, errBadYear
	}
	n := 0
	for i := 0; i < 4; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, errBadYear
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

type completenessError string

func (e completenessError) Error() string { return string(e) }

const errBadYear completenessError = "completeness: year is not four digits"
```

- [ ] **Step 4: Wire SS checks into Check.**

Edit `internal/services/retirement/completeness/check.go`. Replace the body of `Check` with:

```go
func Check(s *models.WhatIfSettings) []Finding {
	if s == nil {
		return []Finding{{
			Severity: SeverityError,
			Code:     "settings_nil",
			Title:    "Scenario could not be loaded",
			Detail:   "Settings are missing. The projection cannot run.",
			Action:   "Reload the page",
		}}
	}

	var findings []Finding
	findings = appendIfPresent(findings, checkStateTaxUnset(s))
	findings = appendIfPresent(findings, checkSSUnconfigured(s))
	findings = appendIfPresent(findings, checkSSPartial(s))
	return sortBySeverity(findings)
}
```

- [ ] **Step 5: Run tests to verify they pass.**

Run: `go test ./internal/services/retirement/completeness/...`
Expected: PASS — all SS test cases green plus existing state-tax tests.

- [ ] **Step 6: Verify full suite still green.**

Run: `go test ./...`
Expected: all packages pass.

- [ ] **Step 7: Commit.**

```bash
git add internal/services/retirement/completeness/
git commit -m "feat(completeness): add Social Security unconfigured / partial checks

ss_unconfigured (Warn) when SocialSecurity is nil and at least one
Person is age >= 50.
ss_partial (Warn) when an FRA benefit is set but the matching claim
age is zero (covers both primary and spouse)."
```

---

## Task 3: MFJ-without-spouse-Person check (Error severity)

**Files:**
- Create: `internal/services/retirement/completeness/checks_household.go`
- Modify: `internal/services/retirement/completeness/check.go` (wire into Check)
- Modify: `internal/services/retirement/completeness/check_test.go` (add household test cases)

- [ ] **Step 1: Add failing tests.**

Append to `internal/services/retirement/completeness/check_test.go`:

```go
func TestCheck_MFJNoSpousePerson(t *testing.T) {
	cases := []struct {
		name      string
		settings  *models.WhatIfSettings
		wantFound bool
	}{
		{
			name: "MFJ filing with no spouse Person emits mfj_no_spouse_person",
			settings: &models.WhatIfSettings{
				TaxConfig: &models.TaxConfig{
					FilingStatus:       models.FilingMarriedJoint,
					StateIncomeTaxRate: 5.0,
				},
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
				},
			},
			wantFound: true,
		},
		{
			name: "MFJ filing with spouse Person does not emit",
			settings: &models.WhatIfSettings{
				TaxConfig: &models.TaxConfig{
					FilingStatus:       models.FilingMarriedJoint,
					StateIncomeTaxRate: 5.0,
				},
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
					{Role: models.PersonRoleSpouse, BirthMonth: "1972-01"},
				},
			},
			wantFound: false,
		},
		{
			name: "Single filing with no spouse Person does not emit",
			settings: &models.WhatIfSettings{
				TaxConfig: &models.TaxConfig{
					FilingStatus:       models.FilingSingle,
					StateIncomeTaxRate: 5.0,
				},
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
				},
			},
			wantFound: false,
		},
		{
			name: "nil TaxConfig does not emit (no filing status to compare)",
			settings: &models.WhatIfSettings{
				TaxConfig: nil,
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
				},
			},
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Check(tc.settings)
			if got := hasCode(findings, "mfj_no_spouse_person"); got != tc.wantFound {
				t.Fatalf("mfj_no_spouse_person present = %v, want %v", got, tc.wantFound)
			}
		})
	}
}

func TestCheck_OrderingErrorsFirst(t *testing.T) {
	settings := &models.WhatIfSettings{
		TaxConfig: &models.TaxConfig{
			FilingStatus:       models.FilingMarriedJoint,
			StateIncomeTaxRate: 0, // triggers Warn
		},
		Persons: []models.Person{
			{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
		},
	}
	findings := Check(settings)

	// Both findings should be present.
	if !hasCode(findings, "mfj_no_spouse_person") || !hasCode(findings, "state_tax_unset") {
		t.Fatalf("expected both findings, got: %+v", findings)
	}
	// Errors must come before warns.
	if findings[0].Code != "mfj_no_spouse_person" {
		t.Errorf("expected mfj_no_spouse_person first, got %q", findings[0].Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail.**

Run: `go test ./internal/services/retirement/completeness/...`
Expected: FAIL — `mfj_no_spouse_person` code not produced.

- [ ] **Step 3: Implement household check.**

Create `internal/services/retirement/completeness/checks_household.go`:

```go
package completeness

import "budget2/internal/models"

// checkMFJNoSpousePerson flags scenarios where filing status is married
// joint but the household has no spouse Person on record. This is an
// Error rather than a Warn because the projection is internally
// inconsistent: federal tax brackets, standard deduction, and Roth
// thresholds use the MFJ filing status (effectively two filers), while
// IRMAA, Medicare premium counts, and RMD calendars walk the Persons
// slice (one filer). The two halves of the projection disagree about
// household size.
//
// nil TaxConfig is treated as "no opinion" — DefaultTaxConfig will be
// applied at engine boundary if not explicitly set, but until the user
// has chosen a filing status we don't pretend to know.
func checkMFJNoSpousePerson(s *models.WhatIfSettings) *Finding {
	if s.TaxConfig == nil {
		return nil
	}
	if s.TaxConfig.FilingStatus != models.FilingMarriedJoint {
		return nil
	}
	if s.GetSpousePerson() != nil {
		return nil
	}
	return &Finding{
		Severity:   SeverityError,
		Code:       "mfj_no_spouse_person",
		Title:      "Filing married-jointly but no spouse on record",
		Detail:     "Tax brackets and standard deduction assume two filers, but Medicare premiums, IRMAA, and RMD timing only see one person. Add a spouse or change the filing status.",
		FormAnchor: "whatif-portfolio-settings-card",
		Action:     "Add spouse",
	}
}
```

- [ ] **Step 4: Wire into Check.**

Edit `internal/services/retirement/completeness/check.go`. Update `Check` body:

```go
func Check(s *models.WhatIfSettings) []Finding {
	if s == nil {
		return []Finding{{
			Severity: SeverityError,
			Code:     "settings_nil",
			Title:    "Scenario could not be loaded",
			Detail:   "Settings are missing. The projection cannot run.",
			Action:   "Reload the page",
		}}
	}

	var findings []Finding
	findings = appendIfPresent(findings, checkStateTaxUnset(s))
	findings = appendIfPresent(findings, checkSSUnconfigured(s))
	findings = appendIfPresent(findings, checkSSPartial(s))
	findings = appendIfPresent(findings, checkMFJNoSpousePerson(s))
	return sortBySeverity(findings)
}
```

- [ ] **Step 5: Run tests to verify they pass.**

Run: `go test ./internal/services/retirement/completeness/...`
Expected: PASS — all four checks green plus the ordering test.

- [ ] **Step 6: Verify full suite still green.**

Run: `go test ./...`
Expected: all packages pass.

- [ ] **Step 7: Commit.**

```bash
git add internal/services/retirement/completeness/
git commit -m "feat(completeness): add MFJ-without-spouse-Person error check

Filing married-jointly while Persons[] has no spouse is an internally
inconsistent state — taxes are computed for two filers, IRMAA / RMDs
for one. Severity is Error to flag the inconsistency, not just a
silent zero."
```

---

## Task 4: State-tax form field + persistence default + handler write

**Files:**
- Modify: `internal/handlers/whatif/form_spec.go` (add field spec)
- Modify: `internal/services/retirement/settings.go` (default TaxConfig in load + accept state_income_tax_rate update)
- Test: `internal/services/retirement/settings_test.go` (existing — add load test) or `internal/handlers/whatif/handlers_test.go` (handler test)

- [ ] **Step 1: Write a settings test for the persistence default.**

Determine the right test file:

Run: `ls internal/services/retirement/settings_test.go 2>/dev/null && echo EXISTS || echo MISSING`

If `EXISTS`, append to it. If `MISSING`, create
`internal/services/retirement/settings_state_tax_test.go`:

```go
package retirement

import (
	"encoding/json"
	"testing"

	"budget2/internal/models"
)

func TestInitializeLoadedSettings_TaxConfigDefaults(t *testing.T) {
	t.Run("legacy file without tax_config produces non-nil TaxConfig", func(t *testing.T) {
		settings := &models.WhatIfSettings{TaxConfig: nil}
		raw := map[string]json.RawMessage{}

		initializeLoadedSettings(settings, raw)

		if settings.TaxConfig == nil {
			t.Fatal("expected non-nil TaxConfig after initializeLoadedSettings")
		}
		if settings.TaxConfig.FilingStatus == "" {
			t.Errorf("expected default filing status, got empty")
		}
	})

	t.Run("existing TaxConfig is preserved", func(t *testing.T) {
		original := &models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: 7.5,
		}
		settings := &models.WhatIfSettings{TaxConfig: original}
		raw := map[string]json.RawMessage{}

		initializeLoadedSettings(settings, raw)

		if settings.TaxConfig != original {
			t.Errorf("TaxConfig was replaced; expected pointer-equal preservation")
		}
		if settings.TaxConfig.StateIncomeTaxRate != 7.5 {
			t.Errorf("StateIncomeTaxRate = %v, want 7.5", settings.TaxConfig.StateIncomeTaxRate)
		}
	})
}

func TestApplySettingsUpdates_StateIncomeTaxRate(t *testing.T) {
	t.Run("state_income_tax_rate writes to TaxConfig", func(t *testing.T) {
		settings := &models.WhatIfSettings{TaxConfig: nil}
		updates := map[string]interface{}{"state_income_tax_rate": 5.0}

		sm := &SettingsManager{}
		sm.applySettingsUpdates(settings, updates)

		if settings.TaxConfig == nil {
			t.Fatal("expected TaxConfig allocated by applySettingsUpdates")
		}
		if settings.TaxConfig.StateIncomeTaxRate != 5.0 {
			t.Errorf("StateIncomeTaxRate = %v, want 5.0", settings.TaxConfig.StateIncomeTaxRate)
		}
	})

	t.Run("state_income_tax_rate preserves existing TaxConfig fields", func(t *testing.T) {
		settings := &models.WhatIfSettings{
			TaxConfig: &models.TaxConfig{
				FilingStatus: models.FilingSingle,
				Age65Count:   1,
			},
		}
		updates := map[string]interface{}{"state_income_tax_rate": 4.25}

		sm := &SettingsManager{}
		sm.applySettingsUpdates(settings, updates)

		if settings.TaxConfig.FilingStatus != models.FilingSingle {
			t.Errorf("FilingStatus mutated to %v", settings.TaxConfig.FilingStatus)
		}
		if settings.TaxConfig.Age65Count != 1 {
			t.Errorf("Age65Count mutated to %v", settings.TaxConfig.Age65Count)
		}
		if settings.TaxConfig.StateIncomeTaxRate != 4.25 {
			t.Errorf("StateIncomeTaxRate = %v, want 4.25", settings.TaxConfig.StateIncomeTaxRate)
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail.**

Run: `go test ./internal/services/retirement/ -run "InitializeLoadedSettings_TaxConfig|ApplySettingsUpdates_StateIncomeTaxRate"`
Expected: FAIL — `TaxConfig` stays nil and `state_income_tax_rate` key is ignored.

- [ ] **Step 3: Default TaxConfig in initializeLoadedSettings.**

Edit `internal/services/retirement/settings.go`. Inside `initializeLoadedSettings` (currently at line 150), append before the closing brace of the function — specifically right after the `Persons` block ending at line 168 — these lines:

```go
	if settings.TaxConfig == nil {
		settings.TaxConfig = models.DefaultTaxConfig()
	}
```

Locate the exact insertion point: the existing function has the Persons nil-check ending at the line that reads `}` after `settings.Persons = []models.Person{}`. Insert the TaxConfig block there, before the existing `if settings.SpendingPhaseConfig == nil {` block.

- [ ] **Step 4: Add state_income_tax_rate handling in applySettingsUpdates.**

Edit `internal/services/retirement/settings.go`. Inside `applySettingsUpdates` (currently at line 886), after the `steady_state_override_year` block (currently lines 975-977) and before the `current_age` re-read block at line 979, insert:

```go
	if v, ok := updates["state_income_tax_rate"].(float64); ok {
		if settings.TaxConfig == nil {
			settings.TaxConfig = models.DefaultTaxConfig()
		}
		settings.TaxConfig.StateIncomeTaxRate = v
	}
```

- [ ] **Step 5: Add the form field spec.**

Edit `internal/handlers/whatif/form_spec.go`. Append a new entry to `settingsFormSpec` (currently ending at line 113 with `steady_state_override_year`). Insert before the closing `}` of the slice literal:

```go
	{Name: "state_income_tax_rate", Kind: fieldFloat, ParseLabel: "state income tax rate",
		HasBounds: true, Min: 0, Max: 20,
		BoundsMsg: "State income tax rate must be between 0 and 20%"},
```

- [ ] **Step 6: Run tests to verify they pass.**

Run: `go test ./internal/services/retirement/ -run "InitializeLoadedSettings_TaxConfig|ApplySettingsUpdates_StateIncomeTaxRate"`
Expected: PASS.

- [ ] **Step 7: Run full suite.**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all packages pass.

- [ ] **Step 8: Commit.**

```bash
git add internal/handlers/whatif/form_spec.go internal/services/retirement/settings.go internal/services/retirement/settings_state_tax_test.go
git commit -m "feat(whatif): wire state_income_tax_rate through form and persistence

- form_spec.go: state_income_tax_rate field (0-20%) parsed from form
- applySettingsUpdates: writes to TaxConfig (allocating if nil)
- initializeLoadedSettings: defaults TaxConfig on legacy files so
  downstream code can read it without a nil check

The engine already computes state tax when StateIncomeTaxRate > 0,
so changing the rate now changes projection totals. Federal-vs-state
breakdown display is a follow-on (TaxAnalysis is dead-coded today)."
```

---

## Task 5: UI input control for state tax + completeness panel template

**Files:**
- Modify: `web/templates/components/whatif/rate-assumptions.html` (add state-tax input)
- Create: `web/templates/components/whatif/completeness.html` (banner partial)

- [ ] **Step 1: Add the state-tax input to rate-assumptions.html.**

Edit `web/templates/components/whatif/rate-assumptions.html`. Locate the `<select id="rmd-timing-select"` block (currently lines 145-152). Immediately after that `<div>` closes (currently line 152's closing `</div>`, before the `<div class="space-y-2">` Taxable Account Assumptions block at line 154), insert:

```html
        <div>
            <label for="state-income-tax-rate-input" class="block text-sm font-medium text-gray-700 dark:text-gray-200">State Income Tax Rate %</label>
            <input type="number" id="state-income-tax-rate-input" name="state_income_tax_rate"
                value="{{if .Settings.TaxConfig}}{{printf "%.2f" .Settings.TaxConfig.StateIncomeTaxRate}}{{else}}0.00{{end}}"
                min="0" max="20" step="0.25"
                class="mt-1 block w-full rounded-md border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm">
            <p class="text-xs text-gray-400 dark:text-gray-500 mt-1">Flat rate applied to ordinary income. Leave at 0 for no-income-tax states (FL, TX, WA, etc.).</p>
        </div>
```

- [ ] **Step 2: Create the completeness banner partial.**

Create `web/templates/components/whatif/completeness.html`:

```html
{{/* Scenario completeness banner. Renders nothing when Findings is empty. */}}
{{define "whatif-completeness"}}
{{if .Findings}}
<div id="whatif-completeness" class="space-y-2 mb-3">
    {{range .Findings}}
    <div class="rounded-md border p-3 text-sm
        {{if eq .Severity 2}}border-red-300 bg-red-50 text-red-800 dark:border-red-700 dark:bg-red-900/30 dark:text-red-200
        {{else if eq .Severity 1}}border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-700 dark:bg-amber-900/30 dark:text-amber-200
        {{else}}border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-700 dark:bg-blue-900/30 dark:text-blue-200{{end}}">
        <div class="flex items-start justify-between gap-3">
            <div class="flex-1">
                <p class="font-semibold">{{.Title}}</p>
                <p class="mt-1 text-xs opacity-90">{{.Detail}}</p>
            </div>
            {{if and .FormAnchor .Action}}
            <a href="#{{.FormAnchor}}" class="shrink-0 text-xs font-medium underline hover:no-underline">
                {{.Action}}
            </a>
            {{end}}
        </div>
    </div>
    {{end}}
</div>
{{end}}
{{end}}
```

The Severity numeric values match the Go enum (`SeverityInfo=0`, `SeverityWarn=1`, `SeverityError=2`).

- [ ] **Step 3: Verify templates compile.**

Run: `go build ./... && go test ./internal/templates/...`
Expected: PASS — template loader does not error.

- [ ] **Step 4: Commit.**

```bash
git add web/templates/components/whatif/completeness.html web/templates/components/whatif/rate-assumptions.html
git commit -m "feat(whatif/ui): add state-tax input + completeness banner template

State-tax input lives next to RMD timing in rate-assumptions. The
completeness banner is a new partial rendered above the projection
results; it expects \`.Findings\` from the page handler (wired in the
next commit) and stays silent when the slice is empty."
```

---

## Task 6: Wire completeness into handler + page render + OOB updates

**Files:**
- Modify: `internal/handlers/whatif/handlers.go` (call Check, attach Findings)
- Modify: `internal/handlers/whatif/handlers_rates.go` (attach Findings to settings partial)
- Modify: `web/templates/pages/whatif.html` (render banner)

- [ ] **Step 1: Write a handler test that asserts the panel renders.**

Find the existing whatif handler test file:

Run: `ls internal/handlers/whatif/ | grep -i handlers_.*test`

Append to `internal/handlers/whatif/handlers_test.go` (or, if too large, create `internal/handlers/whatif/completeness_render_test.go`):

```go
package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/completeness"
)

func TestBuildPageData_IncludesFindings(t *testing.T) {
	t.Run("settings with no state tax surfaces state_tax_unset finding", func(t *testing.T) {
		settings := &models.WhatIfSettings{
			TaxConfig: &models.TaxConfig{
				FilingStatus:       models.FilingSingle,
				StateIncomeTaxRate: 0,
			},
			Persons: []models.Person{
				{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
			},
		}
		findings := completeness.Check(settings)

		got := false
		for _, f := range findings {
			if f.Code == "state_tax_unset" {
				got = true
				break
			}
		}
		if !got {
			t.Fatalf("expected state_tax_unset finding in Check output, got %+v", findings)
		}
	})
}

func TestCompletenessBanner_RenderedTitle(t *testing.T) {
	// Smoke test that the partial uses the Title/Detail/Action fields we
	// produce. Renders the partial in isolation rather than the whole page
	// so the test does not depend on the full template tree.
	if renderer == nil {
		t.Skip("renderer not initialized in this test environment")
	}
	findings := []completeness.Finding{
		{
			Severity: completeness.SeverityWarn,
			Code:     "state_tax_unset",
			Title:    "TestTitle_StateTax",
			Detail:   "TestDetail",
			Action:   "TestAction",
		},
	}
	var b strings.Builder
	renderer.RenderPartial(&b, "whatif-completeness", map[string]interface{}{
		"Findings": findings,
	})
	out := b.String()
	if !strings.Contains(out, "TestTitle_StateTax") {
		t.Errorf("rendered partial missing finding title; got: %s", out)
	}
}
```

If `renderer.RenderPartial` does not accept `io.Writer` (it likely accepts `http.ResponseWriter`), use `httptest.NewRecorder()` instead:

```go
import "net/http/httptest"
...
	w := httptest.NewRecorder()
	renderer.RenderPartial(w, "whatif-completeness", map[string]interface{}{
		"Findings": findings,
	})
	out := w.Body.String()
```

Inspect `internal/templates/render.go` for the actual `RenderPartial` signature before writing the test.

- [ ] **Step 2: Run tests to verify they fail.**

Run: `go test ./internal/handlers/whatif/ -run "BuildPageData_IncludesFindings|CompletenessBanner_RenderedTitle"`
Expected: PASS for `BuildPageData_IncludesFindings` (it only tests the package), FAIL or skip for `CompletenessBanner_RenderedTitle` (template not yet referenced from a registered partial). Either is acceptable — the partial test is a smoke check.

- [ ] **Step 3: Wire Check into handleWhatIf.**

Edit `internal/handlers/whatif/handlers.go`. At the top of the imports block, add:

```go
	"budget2/internal/services/retirement/completeness"
```

Inside `handleWhatIf` (currently line 578), after `activeFilename := retirementMgr.ActiveFilename()` and before `pageData := map[string]interface{}{`, insert:

```go
	findings := completeness.Check(settings)
```

Then modify the `pageData` literal (currently lines 596-604) to include the new key:

```go
	pageData := map[string]interface{}{
		"Title":          "What-If Analysis",
		"ActiveTab":      "whatif",
		"Settings":       settings,
		"Analysis":       analysis,
		"Scenarios":      scenarios,
		"ActiveScenario": activeScenario,
		"ActiveFilename": activeFilename,
		"Findings":       findings,
	}
```

- [ ] **Step 4: Wire findings into the settings-update partial.**

Edit `internal/handlers/whatif/handlers_rates.go`. Inside `handleWhatIfSettings` (currently line 17), after `analysis, err := runAnalysisWithCache(settings)` (currently line 63) and before `partialData := map[string]interface{}{` (currently line 69), insert:

```go
	findings := completeness.Check(settings)
```

Add the import to `handlers_rates.go`:

```go
	"budget2/internal/services/retirement/completeness"
```

Modify the `partialData` literal (currently lines 69-72):

```go
	partialData := map[string]interface{}{
		"Settings": settings,
		"Analysis": analysis,
		"Findings": findings,
	}
```

- [ ] **Step 5: Render the banner on the page.**

Edit `web/templates/pages/whatif.html`. Inside the `whatif-results` div (currently line 92's `<div class="lg:col-span-2 space-y-4" id="whatif-results">`), prepend the banner above the existing `{{template "whatif-results" .}}` (currently line 93):

```html
    <div class="lg:col-span-2 space-y-4" id="whatif-results">
        {{template "whatif-completeness" .}}
        {{template "whatif-results" .}}
    </div>
```

Also add the banner to the OOB partial. Find the `{{define "whatif-results"}}` block (currently line 107). After the opening line and before `<template>` (currently line 109), add an OOB section that targets a wrapper around the banner:

```html
{{define "whatif-results"}}
<div id="whatif-completeness-wrapper" hx-swap-oob="true">
    {{template "whatif-completeness" .}}
</div>
{{/* OOB updates for HTMX - these update the left column when results change */}}
```

Then, in the page-level render at line 92, wrap the banner in the matching id:

```html
    <div class="lg:col-span-2 space-y-4" id="whatif-results">
        <div id="whatif-completeness-wrapper">
            {{template "whatif-completeness" .}}
        </div>
        {{template "whatif-results" .}}
    </div>
```

The wrapper `<div id="whatif-completeness-wrapper">` is required because `hx-swap-oob` needs an element id to target.

- [ ] **Step 6: Verify the banner partial is registered with the renderer.**

The Renderer auto-loads templates from `web/templates/components/whatif/`. Confirm by running:

Run: `grep -rn "components/whatif" internal/templates/`

Expected: pattern that globs `components/whatif/*.html`. If templates require explicit registration, add `completeness.html` to whatever list is in use.

- [ ] **Step 7: Run a full build + manual smoke render check.**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all green.

Then start the dev server and exercise the feature:

Run: `make run` (or whatever the project uses; check `Makefile` for the dev target)

Manual checks (record results inline below):
1. Open `/whatif` — confirm a banner appears at top showing "No state income tax configured" if state tax is unset on the active scenario.
2. Set state tax rate to `5.00` via the new input. Click "Recalculate" or whatever submits the rates form. Banner for state tax should disappear; total tax in the projection should rise.
3. Set state tax back to `0`. Banner reappears.
4. If active scenario has filing_status=married_joint and no spouse Person, an Error banner ("Filing married-jointly but no spouse on record") shows in red.

Document any discrepancy as a follow-up issue rather than blocking this PR — the code path is verified by the unit tests; the manual run is for visual regression only.

- [ ] **Step 8: Commit.**

```bash
git add internal/handlers/whatif/handlers.go internal/handlers/whatif/handlers_rates.go internal/handlers/whatif/handlers_test.go web/templates/pages/whatif.html
git commit -m "feat(whatif): render scenario-completeness banner above projection

handleWhatIf and handleWhatIfSettings now compute completeness.Check
on the loaded settings and pass the findings to the template. The
banner is OOB-swapped so it updates whenever settings change. Empty
findings render nothing."
```

---

## Task 7: End-to-end regression test for state-tax wiring

**Files:**
- Create or extend: `internal/services/retirement/calculator_state_tax_test.go`

- [ ] **Step 1: Write the regression test.**

Create `internal/services/retirement/calculator_state_tax_test.go`:

```go
package retirement

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// TestStateTaxRateChangesProjection asserts that setting a non-zero
// StateIncomeTaxRate produces strictly higher total taxes than rate=0
// across an otherwise-identical scenario. This is the user-visible
// guarantee of the state-tax wiring: changing the input changes the
// output.
func TestStateTaxRateChangesProjection(t *testing.T) {
	build := func(rate float64) *models.WhatIfAnalysis {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 6_000
		s.StartDate = "2026-01"
		s.Persons = []models.Person{
			{ID: "p1", Role: models.PersonRolePrimary, BirthMonth: "1960-01", Name: "You"},
		}
		s.TaxConfig = &models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: rate,
		}
		s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2500, ClaimAge: 67}

		prepared, err := prepare.From(s)
		if err != nil {
			t.Fatalf("prepare.From: %v", err)
		}
		eng := engine.New()
		in := engine.Input{Prepared: prepared}
		return RunFull(eng, in)
	}

	zero := build(0)
	five := build(5)

	if zero == nil || five == nil || zero.Projection == nil || five.Projection == nil {
		t.Fatal("RunFull returned nil projection")
	}

	zeroTax := totalTaxesPaid(zero.Projection)
	fiveTax := totalTaxesPaid(five.Projection)

	if !(fiveTax > zeroTax) {
		t.Errorf("expected 5%% state tax to produce more total tax than 0%%; got 0%%=%v 5%%=%v",
			zeroTax, fiveTax)
	}
}

// totalTaxesPaid sums TaxesPaid across all months in the projection.
// We intentionally do not depend on TaxAnalysis (which is not yet
// populated by production code) — this regression is about the
// engine-level effect, not the breakdown display.
func totalTaxesPaid(p *models.ProjectionResult) float64 {
	if p == nil {
		return 0
	}
	var sum float64
	for _, m := range p.Months {
		sum += m.TaxesPaid
	}
	return sum
}
```

The exact field names (`Input.Prepared`, `RunFull`, `engine.New`) need to match the current API. Before writing, confirm by:

Run: `grep -n "func RunFull\|type Input struct\|func New" internal/services/retirement/orchestrator.go internal/services/retirement/engine/engine.go internal/services/retirement/engine/input.go`

Adjust the test's setup to match the actual signatures. The assertion (`fiveTax > zeroTax`) is the load-bearing piece.

- [ ] **Step 2: Run the test to verify it passes.**

Run: `go test ./internal/services/retirement/ -run TestStateTaxRateChangesProjection -v`
Expected: PASS — total taxes with 5% state rate exceed total taxes with 0% rate.

If FAIL with `fiveTax == zeroTax`, the engine is not actually consuming the rate. Diagnose by logging `prepared.TaxConfig.StateIncomeTaxRate` inside the engine; the wiring task did not complete correctly.

- [ ] **Step 3: Run the full suite.**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all packages pass.

- [ ] **Step 4: Commit.**

```bash
git add internal/services/retirement/calculator_state_tax_test.go
git commit -m "test(retirement): regression for state-tax rate affecting projection

End-to-end: a scenario with StateIncomeTaxRate=5 produces strictly
higher total TaxesPaid than the same scenario with rate=0. Guards
against future regressions of the wiring path closed earlier on this
branch."
```

---

## Task 8: Open the pull request

**Files:** none.

- [ ] **Step 1: Push the branch.**

Run: `git push -u origin feat/scenario-completeness`
Expected: push succeeds.

- [ ] **Step 2: Open the PR.**

Run:

```bash
gh pr create --base dev --title "feat(whatif): scenario completeness panel + state-tax wiring" --body "$(cat <<'EOF'
## Summary
- New `completeness/` package with 4 checks: state_tax_unset, ss_unconfigured, ss_partial, mfj_no_spouse_person
- State income tax wired end-to-end (form field, persistence default, write path); engine already computed it correctly when set
- Banner partial above projection chart, severity-styled (red/amber/blue), with deep-link Action buttons
- HTMX OOB updates so the banner refreshes when settings change

## Out of scope (Phase-2)
- Federal-vs-state tax breakdown display. `TaxAnalysis.TotalStateTaxPaid` is dead-coded today; standing it up requires accumulator surgery and is a separate PR.
- Phase-2 checks (allocation_partial, healthcare_persons_empty, optional_features_off).

## Test plan
- [x] `go test ./...` green
- [ ] Manual: open /whatif on a scenario with no state tax → banner appears
- [ ] Manual: set rate to 5%, banner disappears, total taxes rise
- [ ] Manual: MFJ filing + no spouse Person → red error banner

## Spec / plan
- Spec: docs/architecture-deepening/scenario-completeness.md
- Plan: docs/superpowers/plans/2026-05-08-scenario-completeness.md
EOF
)"
```

Expected: PR URL printed.

- [ ] **Step 3: Note the PR URL in the conversation.**

No commit needed.

---

## Self-review checklist

Before marking the plan done:

1. **Spec coverage:**
   - [ ] state_tax_unset check → Task 1
   - [ ] ss_unconfigured + ss_partial → Task 2
   - [ ] mfj_no_spouse_person → Task 3
   - [ ] State tax form + persistence + handler write → Task 4
   - [ ] State tax UI input + completeness banner template → Task 5
   - [ ] Handler wires Findings into page + OOB → Task 6
   - [ ] End-to-end regression → Task 7
   - [ ] PR creation → Task 8

2. **Placeholder scan:** No "TBD", "implement appropriate", "add error handling" — all steps show concrete code.

3. **Type consistency:** `Severity`, `Finding`, `Check` used identically in tests and impls. `FormAnchor` is the bare anchor (no `#`); the template prepends `#`. Severity numeric ordering: Info=0, Warn=1, Error=2 — matches the template's `eq .Severity 2 / 1 / else` cascade.

4. **Out-of-scope deferred:** TaxAnalysis breakdown display is explicitly Phase-2 in the spec and not referenced as a task.
