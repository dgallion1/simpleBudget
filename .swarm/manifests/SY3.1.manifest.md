TASK: SY3
ATTEMPT: 1
WORKER: worker-coder

## What changed

MCP surface for `models.MajorExpense.ExcludeFromPlanSync` (added upstream by
SY1). Followed the `IsInternalTransfer` pattern exactly, as instructed.

### internal/services/mcpsvc/curate/upsert.go
- `upsertInput.ExcludeFromPlanSync *bool` with json tag
  `exclude_from_plan_sync,omitempty` and a jsonschema description explaining
  nil-leaves-unchanged semantics and the ExpenseSource double-count reason.
- Merge block: `if in.ExcludeFromPlanSync != nil { target.ExcludeFromPlanSync
  = *in.ExcludeFromPlanSync }`, placed directly after the existing
  IsInternalTransfer merge — same shape, applies on both create (zero-value
  target) and edit (loaded target).
- Echoed in `upsertOutput.Expense` (majorExpenseRow) via
  `ExcludeFromPlanSync: target.ExcludeFromPlanSync`.
- Tool description gained one sentence documenting
  `exclude_from_plan_sync`, adjacent to the existing `is_internal_transfer`
  sentence.

### internal/services/mcpsvc/curate/expenses.go
- `majorExpenseRow.ExcludeFromPlanSync bool` with json tag
  `exclude_from_plan_sync` (no omitempty — matches IsInternalTransfer's tag
  in the same struct, so `false` is never suppressed from list output).
- Populated from `e.ExcludeFromPlanSync` in the row-building loop.

### internal/services/mcpsvc/curate/upsert_test.go
Two new tests, mirroring the existing
`TestUpsertMergesIsInternalTransferOnUpdate`:
- `TestUpsertSetsExcludeFromPlanSyncOnCreate` — create with
  `exclude_from_plan_sync: true` stores/echoes true; a second create with
  `false` stores/echoes false. Covers the create path for both bool values.
- `TestUpsertMergesExcludeFromPlanSyncOnUpdate` — load-bearing nil/true/false
  merge-on-edit test: (1) an update omitting the field leaves a
  previously-true stored value untouched (guards against a zero-value bool
  overwrite bug); (2) an update with explicit `false` overwrites stored
  `true`; (3) a further update with explicit `true` overwrites stored
  `false`. Covers nil / true / false on the edit path as required.

Existing tests (`TestUpsertLeavesUnmentionedFieldsAlone`,
`TestUpsertMergesIsInternalTransferOnUpdate`, etc.) continue to pass
unmodified and implicitly cover the "nil leaves everything else alone"
contract for the new field too, since they don't set it.

### .claude/skills/budget2-mcp/references/major-expenses.md
Added `exclude_from_plan_sync` to the `upsert_major_expense` params list
with one-line semantics, plus the verbatim caveat sentence specified in the
task spec: "Until the dashboard budget-actuals consume the same flag (task
SY4), a flagged expense is excluded from the plan sync but still counts
toward dashboard spending targets — expect an apparent overspend of the
flagged amount there."

## Untouched (per acceptance criteria)
- Tool count unchanged: no new tool registered, `registerUpsert` and
  `registerListExpenses` still register exactly one tool each.
- README.md and register_test.go (the want-list) were not touched —
  confirmed via `git status --porcelain | grep -i "readme\|register_test"`
  returning empty after all edits.
- No files outside the SY3 territory (upsert.go, expenses.go, curate test
  files, major-expenses.md) were modified. Other concurrently-dirty files in
  the shared tree (HC/SY1 runs: internal/handlers/dashboard/*,
  internal/models/dashboard.go, internal/services/mcpsvc/spend/*,
  internal/services/dataloader/transfers_test.go,
  web/templates/components/kpis.html, etc.) were left exactly as found.

## Verification

```
$ go build ./...
(exit 0, no output)

$ gofmt -l internal/services/mcpsvc/curate/upsert.go internal/services/mcpsvc/curate/expenses.go internal/services/mcpsvc/curate/upsert_test.go
(exit 0, no output — clean)

$ go test ./internal/services/mcpsvc/curate/... -count=1
ok  	budget2/internal/services/mcpsvc/curate	0.315s

$ go test ./internal/services/mcpsvc/curate/... -count=1 -v
=== RUN   TestDeleteArchivesTheExpenseAndDetachesItsPins
--- PASS: TestDeleteArchivesTheExpenseAndDetachesItsPins (0.02s)
... (46 tests total, all PASS, including:)
=== RUN   TestUpsertMergesIsInternalTransferOnUpdate
--- PASS: TestUpsertMergesIsInternalTransferOnUpdate (0.01s)
=== RUN   TestUpsertSetsExcludeFromPlanSyncOnCreate
--- PASS: TestUpsertSetsExcludeFromPlanSyncOnCreate (0.01s)
=== RUN   TestUpsertMergesExcludeFromPlanSyncOnUpdate
--- PASS: TestUpsertMergesExcludeFromPlanSyncOnUpdate (0.01s)
=== RUN   TestUpsertClearsAnAmountBoundWithExplicitZero
--- PASS: TestUpsertClearsAnAmountBoundWithExplicitZero (0.01s)
PASS
ok  	budget2/internal/services/mcpsvc/curate	0.323s
```

## Notes for checkers
- `ExcludeFromPlanSync` was already present on `models.MajorExpense` (task
  SY1, uncommitted in this same tree) with the exact json tag this task's
  spec requires; this task did not touch internal/models/major_expense.go.
- Followed the "copy IsInternalTransfer's shape" instruction literally:
  pointer-input for nil-vs-explicit disambiguation, unconditional merge
  guarded by nil-check, non-omitempty bool in the read-side row struct.
