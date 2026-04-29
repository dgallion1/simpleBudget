# What-If Page Hardening

Date: 2026-04-29

Follow-up to `docs/what-if-page-review-2026-04-11.md`. This pass focused on
structural debt and a small set of correctness gaps that surfaced during a
fresh review of the what-if surface.

## Scope

- Cost-styling consistency in the what-if templates (project rule: cost numbers must be red, not neutral gray).
- Soft-delete restore correctness across income, expense, and big-ticket items.
- A11y on icon-only action buttons in the what-if list cards.
- Refactor of the 363-line `handleWhatIfSettings` super-handler into a declarative form spec.
- File-level split of the 2,797-line `internal/handlers/whatif/handlers.go` into domain-scoped files.

## Findings addressed

### Cost-styling violations

`web/templates/components/whatif/healthcare-person.html`, `healthcare-card.html`,
and `expense-sources-list.html` rendered cost amounts with neutral gray Tailwind
classes. Per the project rule, cost numbers (taxes, IRMAA, NIIT, healthcare,
expenses) use `text-red-600 dark:text-red-400`. Five spans updated:

- `healthcare-person.html:82` — current monthly cost
- `healthcare-person.html:122` — ACA cost after employer
- `healthcare-person.html:149` — Medicare cost
- `healthcare-card.html:95` — total monthly healthcare
- `expense-sources-list.html:20` — active expense amount

The "removed" entries (line-through, `opacity-60`) keep their muted gray
intentionally — they signal soft-deleted state, not a cost.

### Restore-path duplicate-active detection

`SettingsManager.RestoreIncomeSource`, `RestoreExpenseSource`, and
`RestoreBigTicketItem` previously walked the removed list and appended any
matching ID to the active list, regardless of whether that ID was already
present. A hand-edited or corrupted scenario file with the same ID in both
lists would silently produce a duplicate active entry.

Each restore method now:

1. Returns `*ScenarioConflictError` (HTTP 409 via
   `statusForScenarioOperationError`) when the ID is already in the active
   list — without mutating either list.
2. Returns `*ScenarioNotFoundError` (HTTP 404) when the ID is not in the
   removed list, instead of silently no-oping with a 200 response.

The corresponding handlers `handleWhatIfRestoreIncome`,
`handleWhatIfRestoreExpense`, `handleWhatIfRestoreBigTicket` now route through
`statusForScenarioOperationError` so 409 and 404 surface correctly to the user.

Three nonexistent-ID handler tests that previously asserted "200 or 500" were
updated to assert 404. Six new tests in
`internal/services/retirement/settings_crud_test.go` cover both the
duplicate-active and not-found paths for each of the three lists.

### A11y on icon-only action buttons

Action SVGs across `bigticket-card.html`, `expense-sources-list.html`,
`income-sources-list.html`, and `scenario-chain-card.html` (8 in total) gained
`aria-hidden="true"`. Six icon-only buttons (delete and restore for income,
expense, and big-ticket) gained `aria-label` so screen readers announce a
human-readable action ("Delete expense Rent", "Restore income source
Pension"). The scenario-chain remove button kept its existing `title="Remove"`.

### A11y label/id pairings on heaviest forms

The two heaviest what-if forms — `rate-assumptions.html` (762 lines) and
`spending-phases.html` (649 lines) — had **0** explicit `<label for=...>` /
`<input id=...>` pairings. Sighted users saw the visual labels; screen-reader
users had no programmatic association between any label and the input it
described.

`rate-assumptions.html`:

- Six labels gained `for=` against existing input IDs (Projection Start Date,
  Spending Phase Based On, Delay Tax-Deferred Withdrawals, Inflation Rate,
  Spending Decline Rate, Investment Return Override).
- Nine singleton inputs gained both new `id=` and matching label `for=`
  (Tax-Deferred %, Roth %, Projection Timing, Dividend Yield, Qualified
  Share, Cap Gains Dist., Glide Start/End/Years).
- Six `<span>` "Stocks"/"Cash" labels in the per-account allocation block
  became proper `<label for=>` elements with new IDs on each input.
- Seven non-binding section-header `<label>`s (Persons, Portfolio Allocation,
  Taxable display, Taxable Account Assumptions, Asset Allocation by Account,
  Glide Path, Spending Model) became `<span>` — they don't label a single
  control, and an unbound `<label>` adjacent to multiple inputs is misleading
  to assistive tech.
- Person-row `{{range}}` block (Name, Birth Month, Role) now uses stable
  `id="person-{field}-{{.ID}}"` paired with label `for=`. The Role label is
  conditional: `<span>` when role is primary (read-only display),
  `<label for=>` when role is spouse/other (`<select>`). "Derived Age"
  became `<span>` (display only).
- JS template for newly-added person rows uses `aria-label` on each input
  since the row has no stable ID until the server assigns one on save.

`spending-phases.html`:

- Per-phase `Age` and `Spending` labels paired via `phase-{{$i}}-start-age`
  and `phase-{{$i}}-multiplier`.

Coverage:

| File | Labels with `for=` | Total labels | Notes |
|------|--------------------|--------------|-------|
| `rate-assumptions.html` | 24 | 25 | The unbound 1 is a valid wrap-style `<label>` around the glide-path checkbox |
| `spending-phases.html` | 2 | 3 | The unbound 1 is a valid wrap-style `<label>` around the enable toggle |

Every `for=` resolves to an `id=` on the same page (verified via grep).

### `handleWhatIfSettings` refactor

The handler grew over time into 363 lines of repetitive
`parseFormFloat → bounds-check → assign-to-updates-map` blocks. It is now
**41 lines**, orchestrating four helpers that live in
`internal/handlers/whatif/form_spec.go`:

- `settingsFormSpec` — a `[]fieldSpec` table with one entry per primitive form
  field (28 entries), declaring kind (float/int/enum), bounds, parse label,
  and bounds error message.
- `applyFieldSpec` — runs one entry against an `*http.Request`, returning the
  appropriate parse or bounds error.
- `applySettingsFormSpec` — iterates the table.
- `applyProjectionTiming` — special-cased because it uses
  `models.NormalizeProjectionTiming` rather than a static enum list.
- `validateSettingsCrossFieldInvariants` — enforces
  `tax_deferred + roth ≤ 100` and `stock + cash ≤ 100`. Reads partner field
  from the updates map first, falling back to the raw form value (matching
  legacy semantics).
- `clampPerAccountAllocations` — silently reduces per-account cash when
  stock + cash exceeds 100, preserving the legacy "favor stocks, trim cash"
  UX.

All error messages were preserved byte-for-byte to keep the existing 22
`TestHandleWhatIfSettings_*` tests green. A new `form_spec_test.go` adds 30
focused unit tests for the helpers; every spec helper is at 100% function
coverage.

### File split for `handlers.go`

The 2,797-line monolith was split by domain. After the refactor and the
split, the package layout is:

| File | Lines | Responsibility |
|------|-------|----------------|
| `handlers.go` | 745 | Shared helpers, `Initialize`, `RegisterRoutes`, `handleWhatIf` (page), `handleWhatIfCalculate`, projection-chart and dashboard-sync handlers, `syncSettingsFromDashboard` |
| `handlers_income_expense.go` | 567 | Income, expense, and big-ticket CRUD + restore (11 handlers) |
| `handlers_healthcare.go` | 343 | Healthcare add/update/delete (3 handlers) |
| `handlers_scenarios.go` | 199 | Scenario list/create/switch/delete/rename and chain (7 handlers) |
| `handlers_rates.go` | 665 | Settings, monte-carlo, spending phases, Roth conversion, social security, glide-path, guardrails (11 handlers + helpers) |
| `form_spec.go` | 250 | Declarative form-field spec |
| `form_spec_test.go` | 378 | Spec helper unit tests |

Pure file move — no exported APIs changed, no behavior changed.
`RegisterRoutes` still wires every route from one place.

## Findings investigated but not actionable

The Phase-1 code-mapping pass over-claimed in four places. Verified against
the source and dropped:

- **`SettingsManager.Load()` cache TOCTOU.** The function uses correct
  double-checked locking (RLock, release, Lock, re-check). `saveInternal`
  also updates `sm.cache = settings` on success
  (`internal/services/retirement/settings.go:527`), so the cache is never
  stale after a write. Not a bug.
- **`DeleteScenario` race between reference-check and delete.** The function
  holds the SettingsManager write lock across both steps
  (`internal/services/retirement/settings.go:1382-1413`). Single-process
  app — no cross-process locking needed. Not a bug.
- **Glide-path / guardrails handler validation.** Both
  `handleWhatIfGlidePath` and `handleWhatIfGuardrails` already clamp every
  field with `math.Max(low, math.Min(high, v))` or `max(low, min(high, v))`
  (`internal/handlers/whatif/handlers_rates.go`, in the methods of the same
  name). The clamping is a deliberate forgiving-input UX, not missing
  validation.
- **Healthcare-person update re-validation.** The proposed fix — calling
  `ValidatePersons()` after `UpdateHealthcarePerson` mutation — would
  validate the wrong thing. `ValidatePersons` checks the `Persons[]`
  invariants (one primary, ≤ one spouse, birth months within range), not
  healthcare-to-person linkage. The actual gap (no invariant that
  `HealthcarePerson.PersonID` must reference an existing `Persons[].ID`)
  is a separate model-level invariant; deferred.

## Verification

- `go build ./...` clean.
- `go test ./... -count=1` — all 17 packages pass.
- `go test -race ./internal/handlers/whatif/... ./internal/services/retirement/...` clean.
- Coverage:
  - `internal/handlers/whatif`: **84.6%** (was 85.7%; the 1.1% drop is a
    measurement artifact — `handleWhatIfSettings` shrunk 6× so the same
    untestable error paths now represent a larger percentage of its smaller
    statement count). Every helper in `form_spec.go` is at 100%.
  - `internal/models`: **98.4%** (held).
  - `internal/services/retirement`: **94.6%** (slight improvement from the
    new restore tests).

## Files touched

```
internal/handlers/whatif/handlers.go               (-2224 lines)
internal/handlers/whatif/handlers_income_expense.go (new, 567 lines)
internal/handlers/whatif/handlers_healthcare.go     (new, 343 lines)
internal/handlers/whatif/handlers_scenarios.go      (new, 199 lines)
internal/handlers/whatif/handlers_rates.go          (new, 665 lines)
internal/handlers/whatif/form_spec.go               (new, 250 lines)
internal/handlers/whatif/form_spec_test.go          (new, 378 lines)
internal/handlers/whatif/handlers_test.go           (3 nonexistent-ID tests
                                                     updated, 1 new test for
                                                     persons-without-startdate)
internal/services/retirement/settings.go            (3 Restore* methods now
                                                     reject duplicate active
                                                     IDs and not-found IDs)
internal/services/retirement/settings_crud_test.go  (6 new restore tests)
web/templates/components/whatif/healthcare-person.html
web/templates/components/whatif/healthcare-card.html
web/templates/components/whatif/expense-sources-list.html
web/templates/components/whatif/bigticket-card.html
web/templates/components/whatif/income-sources-list.html
web/templates/components/whatif/scenario-chain-card.html
web/templates/components/whatif/rate-assumptions.html  (a11y: label for/id
                                                       pairings on 28 controls;
                                                       section headers → span)
web/templates/components/whatif/spending-phases.html   (a11y: per-phase Age
                                                       and Spending labels
                                                       paired with inputs)
```
