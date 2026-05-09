# Scenario-Completeness Banner Fixes — Design

**Date:** 2026-05-09
**Branch:** `feat/scenario-completeness`
**Status:** Approved (design phase)

## Problem

Two completeness checks introduced on `feat/scenario-completeness` produce findings that the existing UI and default settings cannot remedy:

1. **`mfj_no_spouse_person` (Error)** — `DefaultWhatIfSettings()` creates one Primary Person, but `DefaultTaxConfig()` returns `FilingMarriedJoint`. The new check fires on every fresh scenario. There is **no filing-status UI**, so the recommended remediation ("change filing status") is not actionable. The only escape is to add a spouse Person — which is wrong for solo users.
2. **`state_tax_unset` (Warn)** — Treats `StateIncomeTaxRate == 0` as "unset", but the rate input's help text explicitly says *"Leave at 0 for no-income-tax states (FL, TX, WA, etc.)"*. A user in FL who follows the documented behaviour sees a permanent unfixable warning banner.

## Goals

- A fresh scenario produces zero `SeverityError` findings.
- A user in a no-income-tax state can enter (or leave) `0` without a permanent warning.
- A user in a tax state can correctly switch filing status when adding a spouse.
- No regression to engine math, persistence, or the 30 callers of `completeness.Check`.

## Non-goals

- A state-selector dropdown with pre-known per-state rates (would replace flat-rate model — separate epic).
- Auto-flipping `FilingStatus` when a spouse Person is added/removed (defer to explicit user choice).
- Per-finding dismiss/ack mechanism (would be useful for future warnings; not justified by today's two findings).

## Design

### Part 1 — Filing-status default + UI control

**Default change.** `DefaultTaxConfig()` returns `FilingStatus: FilingSingle` (was `FilingMarriedJoint`). This matches the default `Persons` slice (one Primary). `checkMFJNoSpousePerson` will not fire on first run.

**Existing helper update.** `defaultTaxConfigForPersons(persons)` at `internal/services/retirement/settings.go:211` currently relies on `DefaultTaxConfig()` returning MFJ and downgrades to Single when no spouse is present. After flipping the default, it must instead upgrade to MFJ when a spouse Person *is* present:

```go
func defaultTaxConfigForPersons(persons []models.Person) *models.TaxConfig {
    cfg := models.DefaultTaxConfig() // now Single
    for _, p := range persons {
        if p.Role == models.PersonRoleSpouse {
            cfg.FilingStatus = models.FilingMarriedJoint
            break
        }
    }
    return cfg
}
```

This preserves correct behaviour for legacy scenarios that load with `TaxConfig == nil` and have a spouse Person (line 170-171 path).

**UI control.** Add a filing-status `<select>` to `web/templates/components/whatif/rate-assumptions.html`, just above the existing State Income Tax Rate input (so both `TaxConfig` fields sit together visually). Options:

| Value | Label |
|---|---|
| `single` | Single |
| `married_joint` | Married Filing Jointly |
| `married_separate` | Married Filing Separately |
| `head_of_household` | Head of Household |

The select is named `filing_status` and submits via the existing whatif-settings `hx-post` form. When a user adds a spouse Person, they manually switch the select to `married_joint`; the banner clears on the next render.

**Form parsing.** Add an entry to `settingsFormSpec` in `internal/handlers/whatif/form_spec.go`:

```go
{Name: "filing_status", Kind: fieldEnum,
    EnumVals:       []string{"single", "married_joint", "married_separate", "head_of_household"},
    EnumInvalidMsg: "Invalid filing status"},
```

Add an apply step in `settings.go` (alongside the existing `state_income_tax_rate` block at line 1003):

```go
if v, ok := updates["filing_status"].(models.FilingStatus); ok {
    if settings.TaxConfig == nil {
        settings.TaxConfig = defaultTaxConfigForPersons(settings.Persons)
    }
    settings.TaxConfig.FilingStatus = v
}
```

(Verify the exact type the existing `fieldEnum` parser produces — likely `string`; coerce with `models.FilingStatus(v)` if so.)

**Open-question resolution.** Invalid `filing_status` form values produce a 400 with `EnumInvalidMsg`, matching the existing `phase_age_reference` pattern in `form_spec.go:60-62`.

**Tests.**
- `TestDefaultTaxConfig_FilingStatusIsSingle`
- `TestDefaultWhatIfSettings_NoErrorFindings` — `completeness.Check(DefaultWhatIfSettings())` yields no `SeverityError`
- `TestHandleWhatIfSettings_FilingStatusRoundTrip` — POST `filing_status=married_joint` → settings persist, GET reflects it
- `TestHandleWhatIfSettings_FilingStatusInvalid` — POST `filing_status=garbage` → 400 or fallback to existing value (pick one in implementation, document)

### Part 2 — Nullable `StateIncomeTaxRate`

**Type change.** `TaxConfig.StateIncomeTaxRate` becomes `*float64` (was `float64`).

- `nil` ⇒ user has not set a value → `state_tax_unset` finding fires
- `*0.0` ⇒ user explicitly chose 0 (no-tax state) → no finding, no tax computed
- `*x` (x > 0) ⇒ tax computed as before → no finding

**`DefaultTaxConfig()`** returns `StateIncomeTaxRate: nil`. A fresh scenario shows the warning until the user touches the input.

**Form parsing.** Today `state_income_tax_rate` is parsed by the `fieldFloat` entry at `form_spec.go:114`. The current inclusion rule (`form_spec.go:136-138`) skips the field entirely when the parsed value is 0 *and* the raw input is empty — meaning "absent" never gets propagated to the apply step, so the existing `TaxConfig.StateIncomeTaxRate` is left unchanged.

To support nil-as-explicit-state, introduce a new `fieldOptionalFloat` kind:

```go
const (
    fieldFloat fieldKind = iota
    fieldInt
    fieldEnum
    fieldOptionalFloat // empty raw → ptr nil; numeric raw → &v
)
```

Switch the `state_income_tax_rate` entry to `fieldOptionalFloat`. In `applyFieldSpec`, the new kind always includes the field in `updates` (with value `*float64`, possibly nil) when the form key is *present* (even if empty). It still respects bounds when non-nil.

Apply step at `settings.go:1003-1007` becomes:

```go
if v, ok := updates["state_income_tax_rate"].(*float64); ok {
    if settings.TaxConfig == nil {
        settings.TaxConfig = defaultTaxConfigForPersons(settings.Persons)
    }
    settings.TaxConfig.StateIncomeTaxRate = v
}
```

Note: the form key being absent (different scenario, e.g. partial PATCH) leaves the field untouched, matching today's semantics.

**Template.** In `rate-assumptions.html`, render an empty `value=""` when nil; show placeholder `e.g. 0 for FL/TX, 9.3 for CA`. Help text below is updated:

> Flat rate applied to ordinary income. Enter `0` for no-income-tax states (FL, TX, WA, etc.). Leaving the field blank flags this scenario as unconfigured.

**Engine and read sites.** Replace direct reads of `StateIncomeTaxRate` with a small helper:

```go
// internal/models/whatif.go
func (t *TaxConfig) StateIncomeTaxRateOrZero() float64 {
    if t == nil || t.StateIncomeTaxRate == nil {
        return 0
    }
    return *t.StateIncomeTaxRate
}
```

Production read sites (4 files):
- `internal/services/retirement/engine/tax.go`
- `internal/services/retirement/settings.go`
- `internal/services/retirement/completeness/checks_state_tax.go` (use `s.TaxConfig.StateIncomeTaxRate == nil` directly, not the helper)
- `internal/models/whatif.go` (any internal getters)

Tests (8 files) use `&val` literals or a `models.PtrFloat64` helper.

**Migration.** Existing JSON scenarios containing `"state_income_tax_rate": 0.0` decode to `*float64` pointing at `0` — i.e. **configured**. Banner stays clear for legacy scenarios. No load-time migration code needed. Verified in `TestStateIncomeTaxRate_PersistenceMigration`.

**`checkStateTaxUnset`** new body:

```go
func checkStateTaxUnset(s *models.WhatIfSettings) *Finding {
    if s.TaxConfig != nil && s.TaxConfig.StateIncomeTaxRate != nil {
        return nil
    }
    return &Finding{ /* unchanged */ }
}
```

Note: `s.TaxConfig == nil` still fires the warning, matching today's behaviour.

**Tests.**
- `TestCheckStateTaxUnset_NilFires`
- `TestCheckStateTaxUnset_ZeroPtrSilent`
- `TestCheckStateTaxUnset_NonzeroSilent`
- `TestCheckStateTaxUnset_NilTaxConfigFires`
- `TestStateIncomeTaxRate_PersistenceMigration` — JSON round-trip with `0.0` produces ptr-to-0 and no `state_tax_unset` finding
- Existing `calculator_state_tax_test.go`, `tax_test.go`, `settings_state_tax_test.go`, `coverage_gaps_test.go`, `coverage_gaps2_test.go`, `rmd_tax_test.go`, `models_extra_test.go`, `completeness_render_test.go`, `check_test.go`, `calculator_coverage_test.go` updated to construct rates as `&val`.

## Risks

- **Blast radius.** `completeness.Check` has 30 direct callers (CRITICAL per GitNexus). We change *which findings* it returns for some inputs; we do not change its signature, return type, or thread-safety. Expected to be safe.
- **Type change for `StateIncomeTaxRate`.** 14 files touched (4 production, 10 test). All call sites are direct field reads — mechanical update. The helper `StateIncomeTaxRateOrZero()` keeps engine math identical.
- **Default change to FilingSingle.** Any code path that assumed MFJ from the default config (none expected; deductions and brackets read from `TaxConfig.FilingStatus` which is explicit) needs verification. Run the e2e regression suite.
- **JSON migration semantics.** Legacy scenarios with `"state_income_tax_rate": 0.0` are treated as configured. A user previously living in CA who never set the rate will silently keep showing 0 — but this is no different from today's behaviour, and nothing about this change makes it worse.

## Out of scope (recap)

State dropdown, auto-filing-status, dismiss/ack — explicitly deferred.

## Resolved during design

- Invalid `filing_status` form value → 400 with `EnumInvalidMsg`, matching existing `phase_age_reference` enum handling in `form_spec.go`.
- `defaultTaxConfigForPersons` logic inverted (default Single, upgrade to MFJ on spouse) to keep loaded-legacy behaviour correct after the `DefaultTaxConfig` flip.
- New `fieldOptionalFloat` parser kind for the nullable rate, instead of one-off handling.
