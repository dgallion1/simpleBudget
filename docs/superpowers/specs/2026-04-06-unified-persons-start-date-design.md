# Unified Persons And Projection Start Date

## Problem

The current What-If model stores ages in multiple places:

- `WhatIfSettings.CurrentAge`
- `WhatIfSettings.SpouseAge`
- `HealthcarePerson.CurrentAge`
- implicit "current month" assumptions in the UI

That creates drift. A scenario can say the primary person is 65, a healthcare person is 64, and a chained scenario can rebase one age path without rebasing the other. The calculator works because everything is integer-age based, but the saved scenario state is not canonical.

This feature makes `start_date` plus a canonical `persons` list the source of truth for age-sensitive behavior while keeping the retirement calculator itself on integer ages.

## Goals

- Replace persisted scenario-level ages with a persisted projection start month and canonical people.
- Keep calculator internals using integer ages with minimal churn.
- Support month-accurate age derivation from `birth_month` plus projection `start_date`.
- Let healthcare entries optionally link to canonical people instead of carrying a second independent age.
- Preserve existing scenario-chaining semantics: the primary scenario owns the age baseline.

## Non-Goals

- Rewriting the projection engine to use dates directly.
- Making more than two people affect couple-aware projection logic.
- Changing healthcare monthly-cost math in [`internal/models/healthcare.go`](/home/darrell/bin/ai/budget2/internal/models/healthcare.go).
- Redesigning the full What-If page beyond the inputs needed for this feature.

Only the canonical `primary` and optional `spouse` persons feed couple-aware retirement logic. Additional people are allowed for healthcare linkage and future expansion only.

## Current Constraints

The design needs to fit the current seams in:

- [`internal/models/whatif.go`](/home/darrell/bin/ai/budget2/internal/models/whatif.go)
- [`internal/models/healthcare.go`](/home/darrell/bin/ai/budget2/internal/models/healthcare.go)
- [`internal/services/retirement/settings.go`](/home/darrell/bin/ai/budget2/internal/services/retirement/settings.go)
- [`internal/services/retirement/chain.go`](/home/darrell/bin/ai/budget2/internal/services/retirement/chain.go)
- [`internal/handlers/whatif/handlers.go`](/home/darrell/bin/ai/budget2/internal/handlers/whatif/handlers.go)
- [`web/templates/components/whatif/rate-assumptions.html`](/home/darrell/bin/ai/budget2/web/templates/components/whatif/rate-assumptions.html)
- [`web/templates/components/whatif/healthcare-card.html`](/home/darrell/bin/ai/budget2/web/templates/components/whatif/healthcare-card.html)
- [`web/templates/components/whatif/healthcare-person.html`](/home/darrell/bin/ai/budget2/web/templates/components/whatif/healthcare-person.html)

Two current implementation details matter:

1. `SettingsManager.Load()` and `LoadScenarioSettings()` both perform JSON initialization and migration work today.
2. `/whatif/settings` currently parses scalar form fields into a `map[string]interface{}` and hands them to `UpdateSettings()`. Person rows do not fit that shape cleanly, so this feature needs one typed path for person-aware updates instead of forcing everything through the current scalar map.

## Proposed Data Model

### New canonical person type

```go
type PersonRole string

const (
    PersonRolePrimary PersonRole = "primary"
    PersonRoleSpouse  PersonRole = "spouse"
    PersonRoleOther   PersonRole = "other"
)

type Person struct {
    ID         string     `json:"id"`
    Name       string     `json:"name"`
    BirthMonth string     `json:"birth_month"` // "YYYY-MM"
    Role       PersonRole `json:"role"`
}
```

### `WhatIfSettings` changes

Add persisted fields:

- `StartDate string   json:"start_date"` using `"YYYY-MM"`
- `Persons   []Person json:"persons"`

Keep compatibility fields, but stop persisting them:

```go
CurrentAge int `json:"-"`
SpouseAge  int `json:"-"`
```

`CurrentAge` and `SpouseAge` remain working fields for the calculator, templates, phase logic, scenario-chain validation, and chart event generation. They are derived from `StartDate` plus `Persons`.

### `HealthcarePerson` changes

Add:

```go
PersonID string `json:"person_id,omitempty"`
```

Behavior:

- If `PersonID == ""`, `Name` and `CurrentAge` remain persisted manual fields.
- If `PersonID != ""`, the linked canonical person owns display name and age.
- For linked entries, `CurrentAge` is treated as derived working state only.
- For linked entries, `Name` should be synchronized from the canonical person before templates and chart code consume the struct, so existing UI and event labels do not show stale names.

This keeps backward compatibility with the current templates while still making the canonical person authoritative.

## Validation Rules

- `StartDate` is required and must be valid `"YYYY-MM"`.
- `Persons` must contain exactly one `primary`.
- `Persons` may contain at most one `spouse`.
- Additional persons must use `other`.
- Every person must have a non-empty `ID`, `Name`, and valid `BirthMonth`.
- `BirthMonth` must not be after `StartDate`.
- `HealthcarePerson.PersonID`, when set, must reference an existing person.
- If there is no spouse person, `PhaseAgeReference == "spouse"` is invalid and must be normalized.

`older` and `younger` are still allowed when there is no spouse because the existing `GetOlderAge()` / `GetYoungerAge()` behavior naturally collapses to the primary age. Only `spouse` requires coercion.

## Age Derivation

Age should be derived with month precision and floored to whole years:

```text
months = (startYear - birthYear) * 12 + (startMonth - birthMonth)
ageYears = floor(months / 12)
```

Implications:

- Someone born later in the same calendar year does not age up until that month is reached.
- The calculator still receives integer ages.
- Medicare timing and chained-scenario transitions behave more consistently around birthdays.

## Core Helpers

Add the following helpers on `WhatIfSettings` or nearby model helpers:

| Helper | Purpose |
| --- | --- |
| `ComputeAges()` | Derive `CurrentAge`, `SpouseAge`, linked healthcare `CurrentAge`, and linked healthcare `Name` |
| `ValidatePersons()` | Validate `StartDate`, `Persons`, and healthcare links |
| `NormalizePhaseAgeReference()` | Default empty values and coerce invalid spouse-only cases |
| `GetPrimaryPerson() *Person` | Return canonical primary person |
| `GetSpousePerson() *Person` | Return canonical spouse person, if any |
| `FindPerson(id string) *Person` | Lookup by `Person.ID` |
| `PersonAge(personID string) int` | Compute a canonical person's age at `StartDate` |

`ComputeAges()` should only overwrite healthcare `CurrentAge` and `Name` when `PersonID` is set. Manual healthcare entries stay manual.

## Defaults

For brand-new settings from `DefaultWhatIfSettings()`:

- `StartDate` defaults to the current local month as `"YYYY-MM"`.
- `Persons` contains one `primary` person.
- That person uses the default age baseline from today’s `CurrentAge` default, back-calculated into `BirthMonth`.
- `PhaseAgeReference` defaults to `"older"`.

Back-calculating the default birth month should use the same month as `StartDate`, so the derived age exactly matches the existing default age on first load.

## Load And Save Lifecycle

### Shared load normalization

Both `Load()` and `LoadScenarioSettings()` should normalize through the same in-memory helper:

1. Unmarshal JSON.
2. Initialize nil slices and phase config the same way the current code does.
3. Migrate legacy `current_age` / `spouse_age` into `start_date` and `persons` if canonical fields are missing.
4. Migrate or normalize healthcare linkage if possible.
5. Normalize `PhaseAgeReference`.
6. Call `ComputeAges()`.
7. Validate canonical person data and healthcare links.

### Persistence difference between the two load paths

- `Load()` may persist the migrated canonical shape back to the active file after successful normalization.
- `LoadScenarioSettings()` must remain read-only. It should return the normalized in-memory result without writing anything to disk.

That distinction matters because chained-scenario loading happens during analysis and should not rewrite unrelated scenario files as a side effect.

### Save path

Before `Save()` or `saveInternal()` marshals JSON:

1. Normalize `PhaseAgeReference`.
2. Validate `StartDate`, `Persons`, and healthcare links.
3. Call `ComputeAges()`.
4. Marshal without `current_age` / `spouse_age`.

This guarantees that any code consuming the in-memory settings after save still sees current derived ages, while the persisted file only contains canonical person data.

## Legacy Migration

### Ages to persons

If a file is missing `start_date` or `persons`:

1. If `start_date` is missing, set it to the current local month first.
2. Create a primary person:
   - `Role = "primary"`
   - `Name = "You"`
   - `BirthMonth = startDate - CurrentAge years`
3. If `SpouseAge > 0`, create a spouse person:
   - `Role = "spouse"`
   - `Name = "Spouse"`
   - `BirthMonth = startDate - SpouseAge years`
4. If `PhaseAgeReference` is empty, store `"older"`.

The start month must be set before estimating birth months. Otherwise the inferred ages are ambiguous.

### Existing healthcare entries

For legacy healthcare people that already exist:

- If `PersonID` is already set, keep it and validate it.
- Otherwise try to infer a link:
  - primary aliases: `you`, `user`, `primary`
  - spouse aliases: `spouse`
  - normalized exact-name match against canonical persons
- If a reliable match is found, set `PersonID` and let linked age/name become derived.
- If not, leave the healthcare entry unlinked and preserve its manual `Name` and `CurrentAge`.

### Legacy single-person healthcare fallback

The current settings loader can synthesize one healthcare person from `MonthlyHealthcare` when `HealthcarePersons` is empty. That migration should move after canonical person normalization so it can use the canonical primary person as its baseline.

Preferred behavior:

- create one healthcare entry for the primary person
- link it with `PersonID = primary.ID`
- choose coverage from the derived primary age

If that linkage path proves too disruptive, the fallback can remain manual for the first implementation, but the canonical-link path is the cleaner long-term model.

## Scenario Creation And Copying

`CreateScenario()` should continue copying the whole settings object, with two clarifications:

- The copied settings must be normalized and have derived ages computed before the new file is written.
- Person IDs do not need regeneration when cloning a scenario. They only need to be unique within a scenario file, and a full copy preserves that consistency.

## Scenario Chaining

### Rule

Chained scenarios do not own their own age baseline. The primary scenario owns:

- `StartDate`
- `Persons`
- derived `CurrentAge`
- derived `SpouseAge`
- `PhaseAgeReference`

This is the canonical-person version of the current rule in [`internal/services/retirement/chain.go`](/home/darrell/bin/ai/budget2/internal/services/retirement/chain.go), where the primary scenario overwrites age fields on the prepared chained settings.

### `prepareChainedSettings()` behavior

Change chain preparation to:

1. Clone the linked scenario settings.
2. Overwrite from the primary scenario:
   - `StartDate`
   - deep-copied `Persons`
   - `PhaseAgeReference`
   - `ProjectionYears`
   - `TaxDeferredDelayYears`
3. Rebase time-relative income, expense, big-ticket, and Roth-conversion fields exactly as today.
4. Handle healthcare entries per person:
   - linked entries: keep `PersonID`, do not manually subtract years from `CurrentAge`
   - unlinked entries: keep the current rebasing behavior and subtract `transitionYear` from manual `CurrentAge`
5. Call `ComputeAges()` on the prepared copy before calculation continues.

Why this split matters:

- Linked healthcare entries should follow the primary scenario's canonical people through the full chain.
- Unlinked entries still mean "this manual person is age X when this chained scenario starts," which matches current behavior.

## Handler Contract

### `/whatif/settings`

Add support for:

- `start_date`
- `person_id[]`
- `person_name[]`
- `person_birth_month[]`
- `person_role[]`

Server behavior:

- Parse aligned indexed arrays.
- Preserve existing IDs when present.
- Generate UUIDs for blank `person_id[]` rows.
- Rebuild the `Persons` slice atomically.
- Normalize `PhaseAgeReference`.
- Save through a typed settings-update path.

Recommended implementation shape:

- keep the existing scalar `updates` map for existing non-person fields
- add a new `UpdateSettingsWithPersons(...)` manager method, or replace the current map-based update path with a typed request struct

Trying to push indexed person rows through the existing scalar map API will make validation and normalization brittle.

### `/whatif/healthcare`

Add support for optional `person_id` on create and update.

Rules:

- If `person_id` is present, ignore submitted `current_age`.
- If `person_id` is present, use the linked canonical person's name for display/state normalization.
- If `person_id` is empty, require manual `name` and manual `current_age` with the current validation rules.
- Switching a healthcare entry from linked to unlinked requires a manual age in the request.

## UI Changes

### Rate assumptions card

Replace the top age section in [`web/templates/components/whatif/rate-assumptions.html`](/home/darrell/bin/ai/budget2/web/templates/components/whatif/rate-assumptions.html) with:

1. `Projection Start Date`
   - `<input type="month" name="start_date">`
2. `Persons`
   - one row per person
   - hidden `person_id`
   - `name`
   - `birth_month`
   - role control or badge
   - derived age preview at `start_date`
   - remove button for non-primary rows
3. `Add Person`
   - if there is no spouse, add a spouse row
   - otherwise add an `other` row

The primary row cannot be removed.

### Phase age reference

- Keep the existing dropdown.
- Show it only when a spouse person exists.
- Preserve support for `younger`, `older`, `primary`, and `spouse`.
- Normalize `"spouse"` to `"older"` if the spouse row is removed.
- Default new or empty values to `"older"`.

### Healthcare UI

Update [`web/templates/components/whatif/healthcare-card.html`](/home/darrell/bin/ai/budget2/web/templates/components/whatif/healthcare-card.html) and [`web/templates/components/whatif/healthcare-person.html`](/home/darrell/bin/ai/budget2/web/templates/components/whatif/healthcare-person.html) so add/edit forms support:

- a person selector populated from canonical `Settings.Persons`
- a manual mode option

When linked:

- hide manual age input
- show linked person name
- show derived age preview

When unlinked:

- keep the current name and age behavior

Coverage, cost, employer, ACA, and Medicare controls remain unchanged.

## Compatibility Notes

- Calculator methods like `PrimaryAgeAt`, `SpouseAgeAt`, `GetOlderAge`, and `GetPhaseReferenceAge` stay integer-age based.
- `buildProjectionChartEvents()` continues to work as long as settings are normalized before analysis runs.
- Existing templates can continue reading `.Settings.CurrentAge`, `.Settings.SpouseAge`, and `.CurrentAge` on healthcare entries after `ComputeAges()` runs.

This is deliberate. The feature changes persistence and normalization, not the shape of downstream projection math.

## Testing

- [`internal/models/models_extra_test.go`](/home/darrell/bin/ai/budget2/internal/models/models_extra_test.go)
  - month-boundary age derivation
  - phase reference normalization
  - person validation and helper lookups
- [`internal/services/retirement/settings_test.go`](/home/darrell/bin/ai/budget2/internal/services/retirement/settings_test.go)
  - load/save round-trip without persisted `current_age` / `spouse_age`
  - active-scenario migration persistence
  - read-only `LoadScenarioSettings()` normalization
- [`internal/services/retirement/chain_test.go`](/home/darrell/bin/ai/budget2/internal/services/retirement/chain_test.go)
  - prepared chained settings inherit primary `StartDate` and `Persons`
  - linked healthcare ages and names follow canonical persons
  - unlinked healthcare entries still rebase manual ages
- [`internal/handlers/whatif/handlers_test.go`](/home/darrell/bin/ai/budget2/internal/handlers/whatif/handlers_test.go)
  - parsing and validation of indexed person arrays
  - healthcare create/update with and without `person_id`
- template/manual verification
  - removing spouse hides the spouse-only phase option
  - linked healthcare rows hide manual age
  - derived age previews update when start month changes

## Recommended Implementation Order

1. Model helpers and JSON tags.
2. Settings load/save normalization and legacy migration.
3. Chain preparation changes.
4. Typed handler path for `/whatif/settings`.
5. Healthcare link handling.
6. Template updates and manual verification.

That order keeps the data model and normalization solid before the UI starts relying on it.

## Post-Implementation Review Fixes

Code review against this spec identified and resolved:

1. **`reconcilePreparedPersons` birth month degradation** — The function back-calculated `BirthMonth` from integer ages, which could shift canonical birth months copied from the primary scenario. Fixed to only remove the spouse person when the primary scenario has no spouse; canonical birth months from the deep-copied `Persons` are preserved. Further refined to check `primary.GetSpousePerson() != nil` instead of `spouseAge > 0`, removing a coupling to pre-computed age state.

2. **Duplicate month helpers** — Two implementations existed in `models/whatif.go` and `services/retirement/settings.go` with different error handling. Consolidated `BirthMonthForAge()` to a single export in the models package. Similarly consolidated duplicate `currentLocalMonth()`/`currentMonthString()` into a single exported `models.CurrentLocalMonth()`.

3. **Double `ComputeAges()` call clarity** — Added comments in `normalizeLoadedWhatIfSettings` explaining why `NormalizePhaseAgeReference` and `ComputeAges` are called twice: first to derive ages for the healthcare migration block, then again after healthcare link inference may change `PersonID` assignments.

4. **Test coverage gaps** — Added unit tests for:
   - `ValidatePersons`: 16 cases covering all validation branches (roles, IDs, names, dates, healthcare links)
   - `deriveAgeAtStartDate`: 12 month-boundary cases (same-month, not-yet-aged-up, zero age, error paths)
   - Person helpers: `GetPrimaryPerson`, `GetSpousePerson`, `FindPerson`, `PersonAge`
   - `BirthMonthForAge`: age-to-birth-month round-trip
   - `NormalizePhaseAgeReference`: all reference values with and without spouse

5. **Orphaned healthcare links in chain preparation** — When `findPreparedScenarioPerson` could not match a linked healthcare person to the primary scenario, the `PersonID` was left pointing at a nonexistent person. Fixed to clear `PersonID` to `""` so the entry becomes manual rather than silently orphaned.

6. **Healthcare handler TOCTOU documentation** — `handleWhatIfAddHealthcare` and `handleWhatIfUpdateHealthcare` read settings outside the write lock for person validation. Added comments explaining that `saveInternal`'s `ComputeAges()` re-derives linked name/age and `ValidatePersons()` catches orphaned links, so correctness is preserved.

7. **`LoadScenarioSettings` read-only intent** — Added comment documenting that the `changed` flag from `decodeSettings` is intentionally discarded, since chained-scenario loading must not rewrite unrelated scenario files.
