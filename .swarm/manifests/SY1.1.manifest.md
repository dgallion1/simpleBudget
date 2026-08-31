# SY1 attempt 1 — manifest

Task: build the per-major-expense "modeled in plan" flag and make the
what-if dashboard sync exclude flagged spending from its living-expense
average.

## Files changed

### internal/models/major_expense.go
Added `ExcludeFromPlanSync bool` field to `models.MajorExpense`, JSON tag
`exclude_from_plan_sync,omitempty` (D-SY-a), with a doc comment stating it
marks the entry as modeled separately in the what-if plan (an
ExpenseSource) and that the sync must not fold its matches into living
expenses.

### internal/services/dataloader/major_expenses.go
`UpdateMajorExpense`'s explicit field-copy block now also copies
`ExcludeFromPlanSync` (both true→ and false→ directions), alongside the
existing `IsInternalTransfer` copy. Doc comment updated to list the field.

### internal/services/majorexpenses/engine.go
Added the exported classifier:
```go
func ComputePlanSyncExclusions(ts *models.TransactionSet, defs []models.MajorExpense, pins map[string]string) map[string]models.MajorExpense
```
Implementation per D-SY-b: builds a `flagged` set of def IDs with
`ExcludeFromPlanSync == true`, short-circuits to an empty map if none, then
runs exactly ONE `Match(ts, defs, MatchOptions{Pins: pins})` pass over the
FULL (unfiltered) defs list, and returns `Hash -> def` only for groups whose
def is flagged. This preserves first-def-wins semantics — an earlier
unflagged def keeps transactions a later flagged def's amount/keyword rule
would otherwise also match. Nil/empty-safe on every input (nil ts, nil/empty
defs, nil pins all return a non-nil empty map).

### internal/handlers/whatif/sync.go
- Added import `budget2/internal/services/majorexpenses`.
- New `syncExcludedGroup{Name string; MonthlyAmount, Total float64; Count int}`
  type and `syncPlan.ExcludedGroups []syncExcludedGroup` field (D-SY-d),
  doc-commented as staying map-free/sorted-by-Name for syncPlanHash
  determinism.
- `computeDashboardSync` now: loads `defs, err := loader.LoadMajorExpenses()`
  and `pins, err := loader.LoadTransactionPins()` (propagating errors the
  same way as the existing `LoadData` error path), computes
  `exclusions := majorexpenses.ComputePlanSyncExclusions(outflows, defs, pins)`,
  and rewrites the `totalExpenses` loop per D-SY-e: Health-Insurance-category
  check FIRST (unchanged, `continue`), THEN an exclusions-map lookup that
  accumulates that def's `Name`/`Total` (`math.Abs(t.Amount)`)/`Count` and
  `continue`s — so an HI-category-and-flag-matched row is skipped once, as
  HI, and never inflates a group's displayed total. Remaining rows still
  accumulate into the signed `totalExpenses` exactly as before;
  `NewMonthlyExpenses` stays `|signed sum| / months` with the months logic
  untouched. Excluded-group aggregates are collected in a local map, then
  converted to a slice and `sort.Slice`'d by `Name` (D-SY-f:
  `MonthlyAmount = Total / months`, same `months` divisor as
  `NewMonthlyExpenses`) before being assigned to `plan.ExcludedGroups`.

### web/templates/components/whatif/sync-preview.html
- Extended the living-expenses annotation line to also mention
  "expenses modeled separately in the plan".
- Added a new section, rendered only when `.Plan.ExcludedGroups` is
  non-empty, headed "Excluded from living expenses — modeled separately in
  the plan", listing each group as
  `{Name} — ${{formatNumber .MonthlyAmount}}/mo ({{.Count}} transactions)`,
  matching the existing "Detected but not synced" section's markup/classes
  (`space-y-1` / `h4` / `ul.list-disc.list-inside` / `li`).
- The hidden `expected_scenario`/`plan_hash`/`expected_revision` guard-field
  flow was not touched; round-trip verified by
  `TestHandleWhatIfSync_RendersExcludedGroupsSection`.

### NEW test files
- `internal/services/majorexpenses/plan_sync_test.go` — first-def-wins trap,
  pin-to-flagged-def excludes, pin-to-unflagged-def beats a flagged
  keyword/amount match, refund (positive amount, outflow-typed) in a
  flagged group excluded like its siblings, nil-safety across
  ts/defs/pins, and a no-flagged-defs-returns-empty case.
- `internal/services/dataloader/plan_sync_exclusion_test.go` — Add/Save/Load
  round-trip of `ExcludeFromPlanSync` (both true and default-false), and
  `UpdateMajorExpense` copying the flag in both the on→ and off→
  directions, verified against a disk reload (not just the returned slice).
- `internal/handlers/whatif/sync_plan_exclusions_test.go` — end-to-end
  `computeDashboardSync` integration test reproducing the oracle's fixture
  (10 groceries stay in living, 4 car-loan rows excluded, 1 gym row that
  shares the car-loan's amount but is claimed by the earlier unflagged gym
  keyword def stays in living, one HI-category row and one
  HI-category+flag-matched-amount row both excluded once via HI and absent
  from the group total); a determinism test asserting two
  `computeDashboardSync` calls against unchanged data hash identically; a
  no-flagged-defs case; and an HTTP-level test asserting the rendered
  preview contains the new section heading, group name, count, and that the
  guard hidden fields still round-trip.

### .swarm/tier3/SY1/oracle.1.log
Output of `bash .swarm/tier3/SY1/accept.sh`, ending `ORACLE PASS` (8/8
checks passed): planted contract test (signature pin +
first-def-wins/nil-safe), server build, `/whatif/sync` preview rendering
the excluded group's name/count/monthly figure, hidden guard fields present,
and `/whatif/sync/apply` saving the expected `NewMonthlyExpenses` figure
(9500/months in the oracle's fixture).

## Verification commands run (this attempt)

```
$ go build ./internal/models/ ./internal/services/majorexpenses/ ./internal/services/dataloader/ ./internal/handlers/whatif/
(no output — success)

$ go test ./internal/services/majorexpenses/ -count=1 -v
... all tests PASS, including TestComputePlanSyncExclusions_* (6 new)
PASS
ok  	budget2/internal/services/majorexpenses	0.003s

$ go test ./internal/services/dataloader/ -run "ExcludeFromPlanSync" -count=1 -v
--- PASS: TestExcludeFromPlanSync_AddSaveLoadRoundTrip
--- PASS: TestExcludeFromPlanSync_SaveLoadRoundTrip
--- PASS: TestUpdateMajorExpense_CopiesExcludeFromPlanSync
PASS
ok  	budget2/internal/services/dataloader	0.018s

$ go test ./internal/services/dataloader/ -count=1
ok  	budget2/internal/services/dataloader	1.392s
(full package, including the foreign-modified transfers_test.go — passes)

$ go test ./internal/handlers/whatif/ -count=1
ok  	budget2/internal/handlers/whatif	10.107s

$ go test ./internal/services/majorexpenses/ ./internal/handlers/whatif/ -count=1
ok  	budget2/internal/services/majorexpenses	0.003s
ok  	budget2/internal/handlers/whatif	10.107s

$ gofmt -l internal/models/major_expense.go internal/services/majorexpenses/engine.go \
    internal/services/majorexpenses/plan_sync_test.go internal/services/dataloader/major_expenses.go \
    internal/services/dataloader/plan_sync_exclusion_test.go internal/handlers/whatif/sync.go \
    internal/handlers/whatif/sync_plan_exclusions_test.go
(no output — clean)

$ bash .swarm/tier3/SY1/accept.sh | tee .swarm/tier3/SY1/oracle.1.log
CHECK contract-tests: PASS
CHECK server-up: PASS
CHECK preview-group-name: PASS
CHECK preview-group-count: PASS
CHECK preview-group-monthly: PASS
CHECK preview-guard-fields: PASS
CHECK apply-200: PASS
CHECK apply-saved-living: PASS
checks passed: 8, failed: 0
ORACLE PASS
```

## Foreign-territory attribution

No foreign file was touched. `git status --porcelain` after this attempt
shows my changes confined to the files listed above (plus the pre-existing
untracked HC-run artifacts, which I did not create or modify):
`internal/handlers/dashboard/handlers.go`,
`internal/handlers/dashboard/handlers_test.go`,
`internal/handlers/dashboard/verdict.go`, `internal/models/dashboard.go`,
`internal/services/dataloader/transfers_test.go`,
`internal/services/mcpsvc/spend/summary.go`,
`internal/services/mcpsvc/spend/summary_test.go`,
`internal/services/metrics/metrics.go`,
`internal/services/metrics/metrics_test.go`,
`web/templates/components/kpis.html`,
`internal/handlers/dashboard/verdict_fractional_cent_test.go`, and the
`.swarm/HC*`/`.swarm/verdicts/HC*`/`.swarm/tier3/HC1` artifacts — all
remained exactly as found. `internal/services/dataloader/transfers_test.go`
in particular ran clean as part of the full `dataloader` package test run
above; it needed no attention.

## Ambiguities resolved

- **Section placement in the template**: put the new "Excluded from living
  expenses" block directly after the "Monthly living expenses" block (before
  "Income sources to add"), since it's conceptually part of the living-
  expenses explanation and the spec didn't pin an exact position.
- **Annotation wording**: extended the existing parenthetical to
  "...excluding Health Insurance — premiums are modeled under Healthcare —
  and expenses modeled separately in the plan" rather than a full rewrite,
  to keep the existing HI clause's wording (and its own tests, which
  `strings.Contains`-check parts of it) intact.
- **Group aggregation key**: aggregated by `def.ID` (not `def.Name`) while
  building `ExcludedGroups`, then sorted the resulting slice by `Name` per
  D-SY-d — two different flagged defs that happened to share a Name would
  still render as two separate list items, which matches "per def" grouping
  implied by D-SY-b/e's transaction-hash-to-def map shape.
