# Scenario Completeness — Spec

**Created:** 2026-05-08
**Status:** Proposed
**Branch context:** Off `dev` (post `feat/projection-engine` merge). New branch:
`feat/scenario-completeness`.

This spec documents a class of bug — *silent zeros* in `WhatIfSettings` — and
proposes a `completeness/` package + UI panel that surfaces them, bundled with
the end-to-end wiring fix for state income tax (the trigger that surfaced the
class).

---

## Trigger

A user noticed their what-if projection produced **no state income tax**
despite running their portfolio in a tax state. Investigation showed
`TaxConfig.StateIncomeTaxRate` defaults to `0.0` and is **never** populated by
form, persistence load, or display — though the engine *does* compute it when
non-zero. State tax was silently absent from every projection in the app.

That single bug isn't unique. It is one instance of a broader class:

| Class | Mechanism | Examples |
|---|---|---|
| **Built-but-unwired** | Calc engine reads field; UI/persistence/display omit it | `StateIncomeTaxRate` |
| **Nil-pointer silence** | Engine guards `if x != nil` and skips with no signal | `SocialSecurity`, `GlidePath`, `Guardrails` |
| **Cross-field invariants unchecked** | User produces internally inconsistent scenario | `FilingMarriedJoint` w/ no spouse `Person`; partial per-account allocation |

Today nothing tells the user a scenario is incomplete. The projection runs,
charts render, and numbers look authoritative.

---

## Direction

Add a **scenario-completeness check** that runs before/after projection and
surfaces material omissions to the user. Plus close the specific
state-tax wiring gap so the warning has a fix path.

**New package:** `internal/services/retirement/completeness/`

```go
type Severity int

const (
    SeverityInfo Severity = iota
    SeverityWarn
    SeverityError
)

type Finding struct {
    Severity   Severity
    Code       string  // stable ID for tests + i18n
    Title      string  // short banner text
    Detail     string  // sentence explaining what's missing and why it matters
    FormAnchor string  // "#section-id" to deep-link the user to the fix
    Action     string  // CTA label, e.g. "Add state tax rate"
}

func Check(s *models.WhatIfSettings) []Finding
```

Pure function. No dependency on engine or handlers. Findings ordered: errors
first, then warnings, then info.

**Where it renders:** Above the projection chart in `web/templates/pages/whatif.html`,
via a new partial `web/templates/components/whatif/completeness.html`.

**When it runs:** On every what-if page render (after `prepare.PreparedSettings`
exists, but `Check` itself takes raw `WhatIfSettings` to keep it pure).

---

## MVP check set (PR-1)

Four checks. Stable codes are append-only.

| Code | Severity | Trigger | Title | Action |
|---|---|---|---|---|
| `state_tax_unset` | Warn | `s.TaxConfig == nil \|\| s.TaxConfig.StateIncomeTaxRate == 0` | "No state income tax configured" | "Set state tax rate" |
| `ss_unconfigured` | Warn | `s.SocialSecurity == nil` AND any `Person.Age >= 50` | "Social Security not configured" | "Add Social Security" |
| `ss_partial` | Warn | `s.SocialSecurity != nil && s.SocialSecurity.FRABenefit > 0 && s.SocialSecurity.ClaimAge == 0` (or spouse equivalent) | "Social Security claim age missing" | "Set claim age" |
| `mfj_no_spouse_person` | **Error** | `s.TaxConfig != nil && s.TaxConfig.FilingStatus == FilingMarriedJoint && s.GetSpousePerson() == nil` | "Filing MFJ but no spouse on record" | "Add spouse" |

Rationale for severities:
- `mfj_no_spouse_person` is **error** because IRMAA, RMD, and Medicare premium
  math will be computed for one person while taxes are computed for two —
  internally inconsistent, not just incomplete.
- The three `Warn` cases produce mathematically valid output but with
  outcome-shifting magnitudes silently set to zero.

Phase-2 checks (deferred):
- `allocation_partial` — any per-account % set on funded account, but
  `tax_deferred_stock + tax_deferred_cash != 100` etc.
- `healthcare_persons_empty` — multi-person household without per-person
  healthcare configured.
- `optional_features_off` — single info row listing disabled discretionary
  features (glide path, guardrails, Roth conversions). Discoverability only.

---

## State tax wiring (bundled with PR-1)

The completeness panel needs a fix path. Five changes close the loop:

### 1. Form field — `internal/handlers/whatif/form_spec.go:49`

Add to `settingsFormSpec`:
```go
{Name: "state_income_tax_rate", Kind: fieldFloat, ParseLabel: "state income tax rate",
    HasBounds: true, Min: 0, Max: 20,
    BoundsMsg: "State income tax rate must be between 0 and 20%"},
```

### 2. Handler write path — `internal/handlers/whatif/handlers_rates.go`

The current rate handlers (`handleWhatIfSettings`) merge the parsed map into
the settings struct via `applyUpdates` or similar. `state_income_tax_rate`
needs special handling because `TaxConfig` may be nil:

```go
if v, ok := updates["state_income_tax_rate"].(float64); ok {
    if settings.TaxConfig == nil {
        settings.TaxConfig = models.DefaultTaxConfig()
    }
    settings.TaxConfig.StateIncomeTaxRate = v
}
```

### 3. Persistence load — `internal/services/retirement/settings.go:150`

Add to `initializeLoadedSettings`:
```go
if settings.TaxConfig == nil {
    settings.TaxConfig = models.DefaultTaxConfig()
}
```

This guarantees `TaxConfig` is non-nil after load, so downstream code
(handlers, completeness checks) can read `settings.TaxConfig.StateIncomeTaxRate`
without nil-guarding. Non-nil `TaxConfig` with `StateIncomeTaxRate == 0` is the
"unset" signal the completeness check looks for.

### 4. Display — UI input control

Add an input for `state_income_tax_rate` in
`web/templates/components/whatif/rate-assumptions.html` near the existing
tax-related inputs (TaxDeferredDelayYears, RMDTiming). Once the user sets
a rate, the engine's `TaxBreakdown.StateTax` flows into `TotalTax` via
`CalculateTaxWithInvestmentIncomeBreakdown` — the user sees overall taxes
rise correctly even without a federal-vs-state breakdown.

### Out of scope (Phase-2): federal-vs-state tax breakdown display

`TaxAnalysis.TotalStateTaxPaid` and `TotalFederalTaxPaid` (`whatif.go:1224-1225`)
are defined but **never assigned in production code today** — `TaxAnalysis` itself
is dead-coded. The projection accumulator (`engine/projtax.go:25`) tracks only
combined `TaxesPaidYTD` (single float64), discarding the federal/state split
that exists transiently in `TaxBreakdown`. Standing up the assignment requires:

1. Splitting accumulator: `TaxesPaidYTD` → `FederalTaxesPaidYTD` + `StateTaxesPaidYTD`.
2. Plumbing both through `EstimateMonthlySnapshot`, `month.go`, and `ProjectionMonth`.
3. Aggregating into `TaxAnalysis` and assigning it on the result.
4. Updating `web/templates/components/whatif/projection-breakdown.html` to render
   the new rows.

That work is a separate PR. PR-1 stops at "user can configure state tax and
see total taxes change correctly".

---

## File structure (after this work)

```
internal/services/retirement/
├── completeness/                NEW
│   ├── check.go                 Check, Finding, Severity
│   ├── checks_state_tax.go      checkStateTaxUnset
│   ├── checks_ss.go             checkSSUnconfigured, checkSSPartial
│   ├── checks_household.go      checkMFJNoSpousePerson
│   └── check_test.go            table-driven tests per check
├── engine/
│   └── tax.go                   modified: write state tax to accumulator
├── settings.go                  modified: TaxConfig defaulted in load
└── (existing structure)

internal/handlers/whatif/
├── form_spec.go                 modified: state_income_tax_rate entry
├── handlers_rates.go            modified: TaxConfig nil-guard + write
└── handlers.go                  modified: pass findings to template data

web/templates/components/whatif/
├── completeness.html            NEW partial
├── rate-assumptions.html        modified: state tax input
└── projection-breakdown.html    modified: state tax row

web/templates/pages/
└── whatif.html                  modified: render completeness partial
```

---

## Tasks (PR-1)

Each task is a single commit. Per-commit invariants: `go build ./...`,
`go test ./...`, `go vet ./...`, pre-commit hook all green.

### Task 0 — Branch setup
- Branch off `dev` as `feat/scenario-completeness`.
- Confirm green baseline: `go test ./...`.

### Task 1 — `completeness/` package skeleton + first check
**Files:** `internal/services/retirement/completeness/check.go`,
`checks_state_tax.go`, `check_test.go`.

Implement `Severity`, `Finding`, `Check` (returns empty slice or one finding),
`checkStateTaxUnset`. Table-driven test asserts:
- `TaxConfig == nil` → finding emitted
- `TaxConfig.StateIncomeTaxRate == 0` → finding emitted
- `TaxConfig.StateIncomeTaxRate > 0` → no finding
- finding has correct Code, Severity, Action

No handler integration yet. Just the package + test.

### Task 2 — Add SS and household checks
**Files:** `completeness/checks_ss.go`, `checks_household.go`, extend
`check_test.go`.

Implement `checkSSUnconfigured`, `checkSSPartial`,
`checkMFJNoSpousePerson`. Wire into `Check()`. Order: errors first.

Tests cover each trigger condition + each negative case (configured ≠
finding).

### Task 3 — State tax wiring (form, persistence, write)
**Files:**
- `internal/handlers/whatif/form_spec.go` — add `state_income_tax_rate`.
- `internal/handlers/whatif/handlers_rates.go` — TaxConfig nil-guard + write.
- `internal/services/retirement/settings.go` — default TaxConfig in load.

Add focused tests:
- Handler test: POST with `state_income_tax_rate=5` persists to settings.
- Engine end-to-end test: scenario with `StateIncomeTaxRate=5` produces
  higher `TaxesPaid` than identical scenario with rate=0 (regression-style;
  no federal-vs-state breakdown asserted).
- Settings load test: legacy file without `tax_config` key yields non-nil
  `TaxConfig` after load.

### Task 4 — UI rendering
**Files:**
- `web/templates/components/whatif/completeness.html` — render banner with
  finding cards, severity-styled (red error, orange warn, blue info).
- `web/templates/components/whatif/rate-assumptions.html` — add state tax
  input near other tax fields.
- `web/templates/pages/whatif.html` — include completeness partial above
  projection chart.
- Handler page-data builder — call `completeness.Check()` and add
  `[]Finding` to template data.

Cost styling: warnings/errors use red text per existing convention
(memory: "Cost items must use red styling, not neutral gray").

### Task 5 — Integration test + screenshot
**Files:** `internal/handlers/whatif/handlers_test.go` (or new file).

- Render whatif page for scenario with `StateIncomeTaxRate=0` → assert
  completeness panel renders `state_tax_unset` warning.
- Render with `StateIncomeTaxRate=5` → assert no warning.
- Render with MFJ + no spouse Person → assert `mfj_no_spouse_person` error.
- If a screenshot harness exists for whatif, add one.

---

## Test strategy

- **Unit:** Each check function gets a table test in `completeness/`. ~20
  rows total across the 4 checks.
- **Integration:** Handler test renders the page and asserts the partial
  appears with expected finding codes.
- **Engine:** State tax accumulator gets a calculator test asserting yearly
  + total sums.
- **Negative tests:** Each check has at least one "configured correctly →
  no finding" case to prevent false positives.

Coverage target: 100% of `completeness/` (small package, all branches
testable). State-tax wiring follows existing whatif handler test patterns.

---

## Non-goals (this PR)

- Phase-2 checks (allocation partial, healthcare, optional-features info row).
- Severity escalation rules (e.g. "warning becomes error after 30 days
  unfixed"). Not yet.
- Telemetry / counters of which warnings users see most.
- A11y polish on the banner beyond semantic HTML + correct ARIA roles.
- Internationalization. English-only strings, but `Code` field gives a stable
  i18n key for later.

---

## Risks

| Risk | Mitigation |
|---|---|
| State-tax accumulator change introduces drift in existing projection tests | Run full whatif test suite before/after; add explicit regression test for state-tax-zero scenarios producing identical numbers as today |
| `TaxConfig` defaulting in load breaks scenarios that intentionally had nil | The struct is read-only after load; nil → DefaultTaxConfig (`MFJ`, state=0) is identical behavior to "MFJ assumed by engine" today |
| Banner becomes noisy and users dismiss it without reading | MVP keeps to 4 high-signal checks; phase-2 only adds if users request more visibility |
| Completeness check on every render adds latency | `Check` is pure, ~µs cost; no allocation-heavy paths. Benchmark if it shows in profiles. |

---

## Deletion test

If we removed `completeness/`, the silent-zeros class returns: state tax
disappears again the next time someone forgets to wire a field; SS-nil
projections stay quiet; MFJ-without-spouse Person still produces inconsistent
math. The package is the *only* place that asserts cross-field invariants
without coupling to the engine. Concentration, not dispersion.

---

## Out-of-scope follow-ons

After PR-1 lands and the pattern is exercised:

- **Phase-2 checks** (separate PR): allocation_partial,
  healthcare_persons_empty, optional_features_off.
- **Settings-side audit pass**: walk every `*Config` pointer field and every
  `bool Enabled` and decide whether the engine's `if x != nil` guard should
  become a completeness check. The output of that audit becomes a list of
  Phase-3 findings.
- **Form-spec coverage tool**: a test that diffs `whatif.go` field tags
  against `form_spec.go` and fails CI if a new persisted field has no form
  binding (would have caught the original state-tax gap at PR time).

---

## References

- Class-of-bug investigation that surfaced this work: this conversation,
  2026-05-08.
- Related: `docs/superpowers/specs/2026-05-08-architecture-deepening.md`
  (Candidate #2, `WhatIfSettings` config-vs-derived split — landed).
- Engine state-tax compute site: `internal/services/retirement/engine/tax.go:198,
  453–478, 521–528`.
- Empty fields in `TaxAnalysis`: `internal/models/whatif.go:1225` (`TotalStateTaxPaid`),
  `:1214` (`YearlyTaxSummary.StateTax`).
