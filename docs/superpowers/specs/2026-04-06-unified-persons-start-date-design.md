# Unified Persons & Projection Start Date

## Summary

Replace scattered age inputs (`CurrentAge`, `SpouseAge`, healthcare person ages) with a unified `Persons` list and explicit `StartDate`. Ages are computed from birth months and the projection start date. All existing calculator internals continue using integer ages.

## Data Model

### New `Person` struct

```go
type Person struct {
    ID         string `json:"id"`          // UUID
    Name       string `json:"name"`        // Display name
    BirthMonth string `json:"birth_month"` // "YYYY-MM" format
    Role       string `json:"role"`        // "primary", "spouse"
}
```

### New fields on `WhatIfSettings`

| Field | Type | JSON | Description |
|-------|------|------|-------------|
| `StartDate` | `string` | `"start_date"` | Projection start date, "YYYY-MM" format |
| `Persons` | `[]Person` | `"persons"` | All people in the scenario |

### Existing fields kept but computed

`CurrentAge` and `SpouseAge` remain in the struct for calculator compatibility but are no longer stored in JSON. They are computed by `ComputeAges()` from `Persons` + `StartDate` at load time and before any calculation.

### Age computation

```
age = (StartDate.Year - BirthMonth.Year) * 12 + (StartDate.Month - BirthMonth.Month)
age_years = age / 12  (integer division, floor)
```

### `PhaseAgeReference` default change

Default changes from `"younger"` to `"older"`. Continues to support `"younger"`, `"older"`, `"primary"`, `"spouse"`.

## Healthcare Person Linkage

### New field on `HealthcarePerson`

| Field | Type | JSON | Description |
|-------|------|------|-------------|
| `PersonID` | `string` | `"person_id"` | Links to a `Person.ID`; when set, `CurrentAge` is computed from that person's birth month |

When `PersonID` is set, `HealthcarePerson.CurrentAge` is populated from the linked person during `ComputeAges()`. The age input is hidden in the UI for linked healthcare persons.

## Migration (backward compatibility)

When loading a scenario JSON that has `current_age`/`spouse_age` but no `persons` array:

1. Create a primary person with name "You", role "primary", birth month estimated as `StartDate - CurrentAge years`
2. If `SpouseAge > 0`, create a spouse person with name "Spouse", role "spouse", birth month estimated similarly
3. Set `StartDate` to current month (YYYY-MM)
4. Link healthcare persons to the matching `Person` by name heuristic (case-insensitive partial match)
5. Save the migrated settings

After migration, `current_age` and `spouse_age` are no longer written to JSON (use `json:"-"` tag). They remain as computed fields in the Go struct, populated by `ComputeAges()`.

## Scenario Creation

When creating a new scenario:
- Copy `StartDate` from the current scenario
- Copy `Persons` from the current scenario
- Healthcare persons are created with `PersonID` links to the copied persons

## UI Changes

### Rate Assumptions card

Replace the "Your Age" and "Spouse Age" number inputs with:

1. **Projection Start Date** — month picker (`<input type="month">`) at the top of the card
2. **Persons section** — for each person:
   - Name (text input)
   - Birth Month (month picker)
   - Role (display only: "Primary" / "Spouse")
   - Computed Age (read-only display: "Age XX")
   - Remove button (if more than one person)
3. **Add Person button** (creates with role "spouse" if only primary exists)

### Healthcare card

- When adding a healthcare person, show a dropdown to link to a defined `Person`
- Linked persons show computed age (read-only) instead of age input
- Existing "Age" field hidden when linked; shown only for unlinked healthcare persons

### Phase Age Reference

- Dropdown continues to show "Younger" / "Older" / person names
- Default selection is "Older" for new scenarios

## What stays the same

- All calculator/projection engine code uses `CurrentAge`/`SpouseAge` ints — no changes
- `PrimaryAgeAt(year)`, `SpouseAgeAt(year)`, `GetPhaseReferenceAge()` — unchanged
- `HealthcarePerson.GetMonthlyCost(month)` — unchanged (reads `CurrentAge`)
- Scenario chaining age rebasing logic — unchanged
- `GetYoungerAge()`, `GetOlderAge()`, `HasSpouse()` — unchanged

## Methods to add on `WhatIfSettings`

| Method | Description |
|--------|-------------|
| `ComputeAges()` | Populates `CurrentAge`, `SpouseAge`, and linked healthcare person ages from `Persons` + `StartDate` |
| `GetPrimaryPerson() *Person` | Returns the person with role "primary" |
| `GetSpousePerson() *Person` | Returns the person with role "spouse", or nil |
| `PersonAge(personID string) int` | Computes age for a specific person at the start date |

## Testing

- Unit tests for `ComputeAges()` with various birth month / start date combinations
- Unit tests for migration path (old JSON -> new persons)
- Unit tests for healthcare person linkage
- Integration tests for settings save/load round-trip
- Handler tests for new form inputs (start date, person birth months)
