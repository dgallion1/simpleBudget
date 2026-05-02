# Whatif "Recently Removed" — Permanent Delete (Purge)

**Status:** Draft
**Date:** 2026-05-02
**Scope:** Whatif scenario — income sources, expense sources, big-ticket items

## Problem

The Whatif page soft-deletes income sources, expense sources, and big-ticket items into per-list `Removed*` arrays. Each removed entry shows up in a "Recently Removed" section with only a green restore arrow. There is no way to truly delete an entry, so the lists grow unboundedly. Users with months of history accumulate a long, mostly-irrelevant list of strikethrough rows that clutter the UI.

## Goal

Add a per-item permanent-delete (purge) action to the Recently Removed UI for income sources, expense sources, and big-ticket items. The action removes the entry from the corresponding `Removed*` slice in the settings JSON without restoring it to the active list. Action is irreversible and confirmed via a native browser dialog.

## Non-Goals

- No bulk "Clear all" button. Per-item only.
- No change to the active-list X button (it remains a soft-delete to the Removed list).
- No change to the Restore action.
- No purge for healthcare scenarios, spending phases, or other Whatif sub-features (none of those have a Recently Removed list today).
- No undo of a purge. The confirm dialog is the only safety net.

## UX

In each "Recently Removed" row, add a red trash-X icon next to the existing green restore arrow.

- Icon: same `M6 18L18 6M6 6l12 12` X used by the active-list delete button, in red (`text-red-500 hover:text-red-700` with dark-mode variants).
- Click flow: HTMX `hx-confirm="Permanently delete {Name}?"` triggers the browser's native confirm dialog. On OK, an `hx-delete` request fires; on Cancel, nothing happens.
- After success, `#whatif-results` is replaced (same target the existing restore button uses), which re-renders the Recently Removed section with the entry gone. If the section becomes empty, the existing `{{if not .Settings.RemovedX}}hidden{{end}}` class hides it.
- 404 on missing ID surfaces through the existing `renderError` path (same as restore).

## Backend Architecture

### New service methods (`internal/services/retirement/settings.go`)

Mirror the structure of `RestoreIncomeSource` / `RestoreExpenseSource` / `RestoreBigTicketItem` but only filter the Removed slice. Do not touch the active list.

```go
// PurgeRemovedIncomeSource permanently removes an income source from the
// removed list. Returns ScenarioNotFoundError if the ID is not in
// RemovedIncomeSources.
func (sm *SettingsManager) PurgeRemovedIncomeSource(id string) (*models.WhatIfSettings, error) {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    settings, err := sm.loadInternal()
    if err != nil {
        return nil, err
    }

    filtered := make([]models.IncomeSource, 0, len(settings.RemovedIncomeSources))
    purged := false
    for _, source := range settings.RemovedIncomeSources {
        if source.ID == id {
            purged = true
            continue
        }
        filtered = append(filtered, source)
    }
    if !purged {
        return nil, &ScenarioNotFoundError{Err: fmt.Errorf("removed income source %s not found", id)}
    }
    settings.RemovedIncomeSources = filtered

    if err := sm.saveInternal(settings); err != nil {
        return nil, err
    }
    return settings, nil
}
```

`PurgeRemovedExpenseSource` and `PurgeRemovedBigTicketItem` follow the same shape against `RemovedExpenseSources` / `RemovedBigTicketItems`.

**Idempotency:** Purge is **not** idempotent — a second purge of the same ID returns `ScenarioNotFoundError` (404). This matches the existing `Restore*` semantics and avoids masking double-fire bugs from the UI.

**Lock semantics:** Same `sm.mu.Lock()` as Restore. Atomic load → modify → save.

**No dependency on the active list:** Unlike Restore, purge does not check or write `IncomeSources`. An entry that somehow appears in both lists (a hand-edited file) is purged from Removed only; the active copy is left alone. This is the conservative behavior — purge cannot remove anything currently active.

### New HTTP handlers (`internal/handlers/whatif/handlers_income_expense.go`)

One handler per resource. Pattern matches `handleWhatIfRestoreIncome` exactly:

```go
func handleWhatIfPurgeIncome(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")

    settings, err := retirementMgr.PurgeRemovedIncomeSource(id)
    if err != nil {
        renderError(w, "Failed to purge income source: "+err.Error(), statusForScenarioOperationError(err))
        return
    }

    analysis, err := runAnalysisWithCache(settings)
    if err != nil {
        renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
        return
    }

    partialData := map[string]interface{}{
        "Settings": settings,
        "Analysis": analysis,
    }
    if renderer != nil {
        renderer.RenderPartial(w, "whatif-results", partialData)
    } else {
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(partialData)
    }
}
```

`handleWhatIfPurgeExpense` and `handleWhatIfPurgeBigTicket` are analogous.

`statusForScenarioOperationError` already maps `ScenarioNotFoundError` → 404.

### New routes (`internal/handlers/whatif/handlers.go`)

```go
r.Delete("/whatif/income/{id}/purge",     handleWhatIfPurgeIncome)
r.Delete("/whatif/expense/{id}/purge",    handleWhatIfPurgeExpense)
r.Delete("/whatif/bigticket/{id}/purge",  handleWhatIfPurgeBigTicket)
```

The `/purge` suffix avoids the existing `DELETE /whatif/income/{id}` (soft-delete from active) and mirrors the existing `/restore` convention. Place each new line directly after the corresponding `/restore` route for readability.

## Frontend Changes

### `web/templates/components/whatif/income-sources-list.html`

In the `whatif-removed-income-sources` block, replace the single restore button with a button group:

```html
<div class="flex items-center gap-1">
    <button hx-post="/whatif/income/{{.ID}}/restore" hx-target="#whatif-results"
        class="text-green-500 hover:text-green-700 dark:text-green-400 dark:hover:text-green-300"
        title="Restore" aria-label="Restore income source {{.Name}}">
        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6"></path>
        </svg>
    </button>
    <button hx-delete="/whatif/income/{{.ID}}/purge" hx-target="#whatif-results"
        hx-confirm="Permanently delete {{.Name}}?"
        class="text-red-500 hover:text-red-700 dark:text-red-400 dark:hover:text-red-300"
        title="Permanently delete" aria-label="Permanently delete income source {{.Name}}">
        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M6 18L18 6M6 6l12 12"></path>
        </svg>
    </button>
</div>
```

### `web/templates/components/whatif/expense-sources-list.html`

Same change to the `whatif-removed-expense-sources` block. URL is `/whatif/expense/{{.ID}}/purge`.

### `web/templates/components/whatif/bigticket-card.html`

Same change to the `whatif-removed-bigticket` block. URL is `/whatif/bigticket/{{.ID}}/purge`.

## Tests

### Service-level (`internal/services/retirement/settings_crud_test.go`)

Per resource (income, expense, big-ticket):

1. **Happy path:** seed a settings file with one entry in `Removed*`, call `Purge*`, assert returned settings has empty `Removed*`, assert active list unchanged, assert disk content matches.
2. **Not found:** call `Purge*` with an unknown ID, assert error is `*ScenarioNotFoundError`.
3. **Active-only ID:** seed an entry only in the active list, call `Purge*` with that ID, assert `*ScenarioNotFoundError` and active list untouched.
4. **Multiple in Removed:** seed three entries, purge the middle one, assert order preserved for the other two.

### Handler-level (`internal/handlers/whatif/handlers_test.go`)

Per resource:

1. **200 on success:** seed Removed entry, fire `DELETE /whatif/<resource>/{id}/purge`, assert 200 and rendered partial includes the empty Recently Removed state.
2. **404 on missing:** fire purge with a non-existent ID, assert 404.
3. **Settings persisted:** after a successful purge, reload settings from disk, assert the entry is gone.

Tests follow the existing `chiRequest` helper pattern used by the restore handler tests at `handlers_test.go:1209` and friends.

## File Inventory

| File | Change | Approx LOC |
|------|--------|-----------|
| `internal/services/retirement/settings.go` | Add 3 `Purge*` methods | +75 |
| `internal/services/retirement/settings_crud_test.go` | Add tests for 3 methods | +120 |
| `internal/handlers/whatif/handlers.go` | Add 3 routes | +3 |
| `internal/handlers/whatif/handlers_income_expense.go` | Add 3 handlers | +75 |
| `internal/handlers/whatif/handlers_test.go` | Add handler tests | +90 |
| `web/templates/components/whatif/income-sources-list.html` | Add purge button | +12 |
| `web/templates/components/whatif/expense-sources-list.html` | Add purge button | +12 |
| `web/templates/components/whatif/bigticket-card.html` | Add purge button | +12 |

## Risks & Mitigations

- **Risk:** User accidentally purges an entry they wanted to restore.
  **Mitigation:** Native browser `hx-confirm` dialog with the entry name. Spec explicitly accepts that no undo is provided.
- **Risk:** A stale UI fires purge on an ID already gone (race vs. another tab).
  **Mitigation:** Service returns `ScenarioNotFoundError` → handler returns 404. The error renders inline; user can re-sync.
- **Risk:** Test data files mutated by the purge persist between test runs.
  **Mitigation:** Existing test pattern uses `t.TempDir()` per `settings_crud_test.go` — purge tests must follow the same isolation.

## Open Questions

None.
