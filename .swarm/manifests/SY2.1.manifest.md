# SY2.1 manifest

Task: SY2 — editor UI for `ExcludeFromPlanSync` (from SY1) on the
major-expenses page: create form + per-expense edit form checkbox, handler
parsing on both create and update paths.

## What changed

### web/templates/pages/major-expenses.html
Added a checkbox to BOTH editor forms, copying the `is_internal_transfer`
checkbox's markup/classes exactly (same wrapping `<label>` implicit
association — no `for`/`id` needed since the sibling pattern doesn't use
one either — same `rounded border-gray-300 dark:border-gray-600
text-indigo-600 focus:ring-indigo-500` input classes, same helper-text
structure):

- Per-expense edit form (~line 1111, inside `major-expense-item`, posts via
  `hx-put`): new `<label class="flex items-center gap-2 mt-1">` block right
  after the existing `is_internal_transfer` label, with
  `{{if .Expense.ExcludeFromPlanSync}}checked{{end}}` reflecting stored
  state.
- Create ("Add a major expense") form (~line 1213, `major-expenses-add-form`,
  posts via `hx-post`): new `<label class="flex items-start gap-2
  text-xs">` block right after the existing `is_internal_transfer` label.

Both use `name="exclude_from_plan_sync" value="on"` (matches
`parseFormBool`'s "on"/"true"/"1" contract) and the label text specified in
the task: "Modeled in retirement plan — excluded from the plan's living-
expense sync", with the helper line "The retirement plan models this
expense separately; keep it out of the synced living-expense figure."

### internal/handlers/majorexpenses/handlers.go
`parseExpenseForm` (the single function shared by both `handleAdd` and
`handleUpdate` — confirmed by reading call sites at lines 78 and 112 before
editing) now also sets:

```go
ExcludeFromPlanSync: parseFormBool(r, "exclude_from_plan_sync"),
```

Since both create and update route through this one function, no separate
edit was needed at each call site — the field is set on both paths by
construction. Re-ran `gofmt -w` to fix struct-literal field alignment after
adding the longer field name.

### internal/handlers/majorexpenses/handlers_test.go
Added round-trip test coverage (test-files territory, per task scope):

- `TestParseExpenseForm_ExcludeFromPlanSync` — unit-level: checkbox "on" →
  true; checkbox absent → false. Mirrors
  `TestParseExpenseForm_IsInternalTransfer`.
- `TestHandleAdd_ExcludeFromPlanSync_Persists` — POST round trip through the
  full handler + storage layer: flag "on" persists true, flag absent
  persists false (asserted against `dl.LoadMajorExpenses()`, not just the
  response).
- `TestHandleUpdate_ExcludeFromPlanSync_Persists` — PUT round trip: turning
  the flag on persists true; a second edit posting without the checkbox
  clears it back to false (proves the update path doesn't just OR-in a
  stale true).
- `TestMajorExpenseEditForm_RendersExcludeFromPlanSyncCheckedState` — full
  template render (real `templates.Renderer` off the embedded FS, via
  `setupTestEnvWithRenderer`, the pattern already used by
  `TestHandleExceptions_WithRenderer_ReturnsPartial`): seeds one flagged and
  one unflagged expense, GETs `/major-expenses`, and asserts the flagged
  expense's edit-form fragment contains the checkbox `checked` while the
  unflagged one's does not — isolated per expense by slicing on each row's
  unique `id="major-expense-detail-<id>"` anchor so the assertion can't
  cross-match the sibling expense's checkbox.

## Why
SY1 added the `ExcludeFromPlanSync` model field and DataLoader plumbing;
this task is the UI surface so a user can actually set it. Following the
`is_internal_transfer` checkbox pattern precisely (same classes, same label
structure, same parse-call shape) keeps the two flags visually and
behaviorally consistent, satisfies ACCESSIBILITY.md point 4 (input already
programmatically associated via label wrapping — same as the sibling
pattern) and point 9 (native `<input type="checkbox">` is keyboard-operable
with a visible focus ring via `focus:ring-indigo-500`, unchanged from the
sibling).

## Test output

```
$ go test ./internal/handlers/majorexpenses/... -run ExcludeFromPlanSync -v -count=1
=== RUN   TestParseExpenseForm_ExcludeFromPlanSync
=== RUN   TestParseExpenseForm_ExcludeFromPlanSync/checkbox_on_yields_true
=== RUN   TestParseExpenseForm_ExcludeFromPlanSync/missing_checkbox_yields_false
--- PASS: TestParseExpenseForm_ExcludeFromPlanSync (0.00s)
    --- PASS: TestParseExpenseForm_ExcludeFromPlanSync/checkbox_on_yields_true (0.00s)
    --- PASS: TestParseExpenseForm_ExcludeFromPlanSync/missing_checkbox_yields_false (0.00s)
=== RUN   TestHandleAdd_ExcludeFromPlanSync_Persists
=== RUN   TestHandleAdd_ExcludeFromPlanSync_Persists/flag_on_persists_true
=== RUN   TestHandleAdd_ExcludeFromPlanSync_Persists/flag_absent_persists_false
--- PASS: TestHandleAdd_ExcludeFromPlanSync_Persists (0.01s)
    --- PASS: TestHandleAdd_ExcludeFromPlanSync_Persists/flag_on_persists_true (0.01s)
    --- PASS: TestHandleAdd_ExcludeFromPlanSync_Persists/flag_absent_persists_false (0.00s)
=== RUN   TestHandleUpdate_ExcludeFromPlanSync_Persists
--- PASS: TestHandleUpdate_ExcludeFromPlanSync_Persists (0.01s)
=== RUN   TestMajorExpenseEditForm_RendersExcludeFromPlanSyncCheckedState
--- PASS: TestMajorExpenseEditForm_RendersExcludeFromPlanSyncCheckedState (0.03s)
PASS
ok  	budget2/internal/handlers/majorexpenses	0.050s

$ go test ./internal/handlers/majorexpenses/... -count=1
ok  	budget2/internal/handlers/majorexpenses	0.318s (full package suite, all pre-existing tests still pass)

$ gofmt -l internal/handlers/majorexpenses/handlers.go internal/handlers/majorexpenses/handlers_test.go
(no output — clean)

$ go build ./internal/handlers/majorexpenses/... ./internal/models/... \
    ./internal/services/dataloader/... ./internal/services/majorexpenses/... \
    ./internal/handlers/... ./internal/templates/...
(no output — builds clean, template set — including the two edited
major-expenses.html forms — parses: "Templates loaded successfully: 56
files" appeared in the renderer-based test's log output above)
```

## NOTE for the lead: pre-existing whole-repo build break, NOT caused by SY2

`go build ./...` fails at `internal/services/mcpsvc/curate/upsert.go:166`
(`unknown field ExcludeFromPlanSync in struct literal of type
majorExpenseRow`). This file is OUTSIDE the SY2 territory (not in the list
of files I was told to touch) and was already modified/uncommitted by a
concurrent run when I started (`git status --porcelain` showed it dirty
before any of my edits). It references `ExcludeFromPlanSync` on a
`majorExpenseRow` struct that apparently doesn't have that field yet in its
current mid-edit state — looks like a different in-flight SY-run task
(MCP curation tool surface for the same flag) that hasn't finished landing
its own struct field. I did not touch this file. Scoped builds of every
package SY2 actually touches or that depends on it (`internal/handlers/...`,
`internal/models/...`, `internal/services/dataloader/...`,
`internal/services/majorexpenses/...`, `internal/templates/...`) all build
clean. Flagging so the lead can confirm the other SY task lands its
`majorExpenseRow.ExcludeFromPlanSync` field before requiring a green
`go build ./...`.
