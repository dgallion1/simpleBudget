# SY run — plan-modeled spending excluded from the what-if sync (2026-08-30)

Status: APPROVED by user 2026-08-30 ("spec and dispatch option B").
Prefix `SY` (T/V/W/X/Y/Z/HC taken). CONCURRENT with the live HC run —
territory fence below is load-bearing.

## Problem

`computeDashboardSync` (internal/handlers/whatif/sync.go) folds every
trailing-12-month outflow except category `Health Insurance` into
`MonthlyLivingExpenses`. Spending the plan models separately as an
ExpenseSource — the user's Lucid loan, $1,600/mo ending year 3 — was
double-counted: ~$920/mo of Lucid payments inside the synced $8,042.54 PLUS
the modeled stream. Interim data-level fix applied 2026-08-30: the "Lucid
payment" major expense (b388e1c8) was marked `is_internal_transfer`, which
mislabels real spending as a transfer and hides it from every spending
surface. This run builds the first-class mechanism: a per-major-expense
flag that keeps the spending visible everywhere but OUT of the plan sync's
living-expense average.

## Lead decisions

- **D-SY-a (naming):** `models.MajorExpense.ExcludeFromPlanSync bool`,
  JSON `exclude_from_plan_sync,omitempty`, form field
  `exclude_from_plan_sync`, MCP field `exclude_from_plan_sync`. Semantics:
  "this spending is modeled separately in the what-if plan (an
  ExpenseSource); the plan sync must not fold it into living expenses."
- **D-SY-b (classification runs the FULL match pass):** the classifier
  calls `majorexpenses.Match(outflows, ALL defs, MatchOptions{Pins: pins})`
  and takes the groups whose def carries the flag. NEVER match against a
  filtered flagged-only def list: `MatchTransaction` is first-def-wins, so
  filtering the def list steals transactions that belong to an earlier
  unflagged def. (The oracle's GYM trap exists to catch exactly this.)
- **D-SY-c (interaction with IsInternalTransfer):** transfer-flagged
  matches are dropped at load and never reach the sync; setting both flags
  is legal, redundant, and NOT validated against. validate.go unchanged.
- **D-SY-d (preview transparency):** `syncPlan` gains
  `ExcludedGroups []syncExcludedGroup{Name string; MonthlyAmount, Total
  float64; Count int}`, sorted by Name (determinism: syncPlanHash hashes
  canonical JSON; the struct must stay map-free and slice order must be
  deterministic or preview/apply hash equality breaks).
  **AMENDED by ruling SY-2026-08-30b:** sort by Name with def ID as
  tiebreaker (Names are NOT unique — majorexpenses.Validate does not
  enforce uniqueness, and live data has three defs named "Subscription");
  the determinism test must use ≥2 flagged defs SHARING a Name and assert
  hash stability across ≥100 in-process computeDashboardSync iterations,
  such that deleting the sort makes the test fail. The preview partial
  renders a section listing each group "Name — $X/mo (N transactions)",
  shown only when non-empty. The JSON fallback inherits the field through
  struct embedding.
- **D-SY-e (no double-subtraction with the Health Insurance category):**
  iterate outflows: (1) HI-category → skip (existing rule, case-insensitive,
  Z3); (2) ELSE flagged-group member (by Hash) → skip AND count into that
  group's displayed Count/Total. A row that is both HI-category and
  amount-matched to a flagged def is excluded exactly once, as HI, and does
  NOT appear in the group's displayed total. Displayed totals are
  accumulated at the sync site from actually-skipped rows — never
  precomputed from raw group membership.
  **AMENDED by ruling SY-2026-08-30a:** the displayed `Total` NETS refunds —
  `Total += -t.Amount` over actually-skipped rows (a purchase is negative in
  the ledger, so payments add and refunds subtract), matching the
  major-expenses net-spend convention (handlers/majorexpenses ~366 and the
  MCP list tool's documented semantics). NEVER `math.Abs` per row. A group
  whose refunds exceed its spend renders a negative Total/MonthlyAmount
  as-is via formatNumber. `NewMonthlyExpenses` arithmetic is unchanged
  (signed sum of remaining rows, then one final Abs).
- **D-SY-f (formatting):** display formatting goes through the existing
  template funcs (`formatNumber`); no new Go-side rounding paths
  (ruling 2026-08-29b lineage). `MonthlyAmount = Total / months` using the
  SAME months divisor as `NewMonthlyExpenses`.
- **D-SY-g (deferred consumer):** the dashboard budget actuals
  (metrics.Calculate living split → verdict bar, budget-vs-actual chart,
  MCP summarize_spending budget block) must eventually consume the SAME
  classifier — that is task SY4, **BLOCKED until the HC run merges**
  (metrics/dashboard is HC territory right now). Until SY4 lands, living
  ACTUALS include flagged spend while the synced TARGET excludes it — a
  documented gap. Consequence for the user: keep the Lucid group on
  `is_internal_transfer` (not the new flag) until SY4 is accepted.

## Tasks

| ID  | Tier | Checks               | Summary |
|-----|------|----------------------|---------|
| SY1 | 3    | tests,second         | flag + shared classifier + sync exclusion + preview section |
| SY2 | 2    | tests,a11y,second    | major-expenses editor checkbox (both editor forms) |
| SY3 | 1    | tests                | MCP curate field + skill reference doc |
| SY4 | 2    | tests,second         | BLOCKED-ON-HC: metrics/dashboard actuals consume the classifier |

Sequencing: SY1 accepted first, then SY2 ∥ SY3. SY4 is NOT dispatched in
this run unless HC merges while SY2/SY3 are still open.

## Territories (exact paths — fence against the live HC run)

- SY1: `internal/models/major_expense.go`;
  `internal/services/majorexpenses/engine.go` + NEW test file(s) in that
  package; `internal/services/dataloader/major_expenses.go` + NEW test file
  (do NOT touch `transfers_test.go` — HC's);
  `internal/handlers/whatif/sync.go` + NEW sync test file;
  `web/templates/components/whatif/sync-preview.html`.
- SY2: `web/templates/pages/major-expenses.html`;
  `internal/handlers/majorexpenses/handlers.go` (+ its test files).
- SY3: `internal/services/mcpsvc/curate/upsert.go`,
  `internal/services/mcpsvc/curate/expenses.go` (+ curate tests);
  `.claude/skills/budget2-mcp/references/major-expenses.md`.
- FOREIGN — HC territory, never touched by any SY worker or checker fix:
  `internal/services/metrics/**`, `internal/handlers/dashboard/**`,
  `internal/models/dashboard.go`, `internal/services/mcpsvc/spend/**`,
  `internal/services/dataloader/transfers_test.go`,
  `web/templates/components/kpis.html`, `.swarm/HC*`, `.swarm/*/HC1*`.
- Checkers: package-scope evidence only (shared-tree rule). The foreign
  dirty files above belong to the HC run — attribute, never FAIL on them.
  Full-server builds (oracle stage 2) are run by the LEAD at a moment
  coordinated with the HC lead.

## SY1 — acceptance criteria (Tier 3)

1. `models.MajorExpense` gains `ExcludeFromPlanSync bool` with JSON tag
   `exclude_from_plan_sync,omitempty`; `DataLoader.UpdateMajorExpense`
   copies it (it copies fields explicitly — see IsInternalTransfer at
   major_expenses.go:124); Add/Save/Load round-trips it.
2. Exported classifier in `internal/services/majorexpenses` with EXACTLY
   this signature (the oracle's planted contract test pins it):
   `func ComputePlanSyncExclusions(ts *models.TransactionSet, defs []models.MajorExpense, pins map[string]string) map[string]models.MajorExpense`
   — returns transaction Hash → the flagged def that claimed it, built per
   D-SY-b from one full `Match` pass. Empty/nil-safe on every input.
3. `computeDashboardSync` loads defs (`loader.LoadMajorExpenses`) and pins
   (`loader.LoadTransactionPins`), computes the exclusion map over the
   filtered outflows, and applies D-SY-e ordering. `NewMonthlyExpenses` =
   |signed sum of remaining outflows| / months (months logic unchanged).
   `plan.ExcludedGroups` built per D-SY-d/e, sorted by Name.
4. Preview template renders the excluded section (name, $X/mo via
   formatNumber, count), only when non-empty; the living-expenses
   annotation mentions plan-modeled exclusions. Hidden-field guard flow
   (expected_scenario/plan_hash/expected_revision) unchanged and still
   round-trips — preview→apply with unchanged data must still succeed.
5. Unit tests in the SY1 territory covering at least: first-def-wins trap
   (keyword def ahead of flagged amount def); pin to a flagged def excludes
   a transaction keywords would miss; pin to an UNFLAGGED def beats a
   flagged keyword/amount match; HI+flagged overlap row excluded once and
   absent from group totals; refund (positive outflow) inside a flagged
   group skipped like its siblings; persistence round-trip.
   **AMENDED after attempt 1:** additionally (a) a DISPLAY-layer refund
   test — a flagged group with payments plus a refund asserts the
   ExcludedGroups entry's Total/MonthlyAmount are the NET figures
   (SY-2026-08-30a); (b) the determinism test per amended D-SY-d — ≥2
   flagged defs sharing a Name, ≥100 iterations, killed by deleting the
   sort (SY-2026-08-30b / checker-tests mutation M4).
6. `bash .swarm/tier3/SY1/accept.sh` from repo root ends `ORACLE PASS`
   (lead runs it; server build coordinated with HC lead).

## SY2 — acceptance criteria (Tier 2)

- Checkbox "Modeled in retirement plan — excluded from the plan's
  living-expense sync" in BOTH editor forms of major-expenses.html (the
  create form and the per-expense edit form — the existing
  `is_internal_transfer` checkboxes at ~1112 and ~1213 are the pattern),
  posting `exclude_from_plan_sync`; handler parses via `parseFormBool` into
  the model on BOTH create and update paths (handlers.go:528 vicinity).
- Round-trip tests: form post with the flag on persists true; absent
  persists false; edit form renders the stored state checked.
- ACCESSIBILITY.md applies: label programmatically associated, keyboard
  reachable, visible focus, dark-mode classes match the sibling checkbox.

## SY3 — acceptance criteria (Tier 1)

- `curate/upsert.go`: `ExcludeFromPlanSync *bool
  json:"exclude_from_plan_sync,omitempty"` with jsonschema description,
  nil = leave unchanged (IsInternalTransfer is the pattern), applied on
  create and edit, echoed in the response struct.
- `curate/expenses.go`: list output includes `exclude_from_plan_sync`.
- Tool COUNT unchanged → README / server_test want-list untouched. Update
  `.claude/skills/budget2-mcp/references/major-expenses.md` (upsert params
  + one-line semantics, including the D-SY-g caveat until SY4 lands).
- Tests in the curate package for nil/true/false on create and edit.

## SY4 — acceptance criteria (Tier 2; HC merged as 4c3b65e, unblocked)

Design (D-SY-g resolution, lead decision 2026-08-30): the flag excludes
flagged spend from the LIVING-BUDGET comparison only. Total expenses, net
savings, and savings rate are UNCHANGED — the money is really spent (this
is exactly where the flag differs from `is_internal_transfer`). Pattern is
HC's coverageStart precedent: callers derive from the UNFILTERED set, the
computation lives in ONE place.

1. `metrics.Calculate` gains one parameter: `planExclusions
   map[string]models.MajorExpense` (nil-safe; nil == no exclusions). Rows
   whose `Hash` is in the map are excluded from the living-expenses figure
   (the same figure healthcare premiums are subtracted from): Monthly
   Living Expenses card, per-month living rate, budget cumulative
   variance, and the combined cumulative walk's living side. NOT from
   totalIncome/totalExpenses/netSavings/savingsRate. `DashboardMetrics`
   gains `PlanExcludedTotal float64` (and count) so surfaces can annotate.
2. All three `Calculate` call sites (dashboard handlers.go:101, :188,
   mcpsvc/spend/summary.go:291) build the map via
   `majorexpenses.ComputePlanSyncExclusions` over the UNFILTERED
   transaction set (like HealthcareCoverageStart) with loader defs+pins.
   No call site may pass nil while defs exist — enumerate ALL sites by
   grep at review, not from this list (split-classification rule
   2026-08-29a).
3. Any OTHER surface computing a living actual from transactions directly
   (budget-vs-actual chart per-month actual accrual in dashboard
   handlers.go ~line 616 vicinity; verdict.go living breakdown) must
   consume the same map/fields — never re-classify locally. Worker
   enumerates these by grepping for living-actual arithmetic and lists
   every touched-or-cleared surface in the manifest.
4. Rendered-string arithmetic rule (2026-08-29b): where a surface renders
   living + healthcare = combined, a test asserts on RENDERED strings with
   a fractional-cent fixture including a flagged def.
5. Tests: fixture with a flagged def matching known outflows asserts (a)
   living actual drops by exactly the flagged net while totalExpenses does
   not change; (b) nil map == empty map == zero-value behavior identical
   to pre-SY4 for every field; (c) MCP summarize_spending budget block
   reflects the exclusion; (d) verdict/chart consumers get the excluded
   figures (per the surfaces enumerated in 3).
6. Post-merge data migration (LEAD action, not worker): flip Lucid
   b388e1c8 `is_internal_transfer` false → `exclude_from_plan_sync` true,
   re-sync, verify living target unchanged (~7,128.66) and Lucid visible
   again in spending totals.

## Oracle SY1 — calibration record

`.swarm/tier3/SY1/accept.sh`. Fixture CSV generated at run time (the sync
window is time.Now()-relative), expected values computed by the same
day/30 months rule as computeDashboardSync. Traps wired: GYM first-def-wins,
HI+flagged overlap single-exclusion, master-drops-unknown-JSON-field.
Both-ends validation, run 2026-08-30 by the lead:
- FAIL end (clean `git archive master` tree): 5 checks fail, each on the
  defect — contract test compile-fails on the missing symbol+field, preview
  lacks the excluded section, saved living 1276.99 (= 11500/months, flag
  silently dropped by json.Unmarshal on master). Harness checks
  (server-build, server-up, guard fields, apply-200) all PASS → failures
  attribute to the defect, not the harness.
- PASS end (throwaway prototype tree implementing D-SY-a/b/d/e minimally):
  8/8 checks PASS, ORACLE PASS. Prototype discarded.

Oracle v2 (extended for attempt 2 with the refund trap: fixture row
`CAR LOAN PAYMENT REVERSAL,+500.00`, group check now expects count
"5 transactions" and NET monthly 1500/months). Both-ends revalidation, run
2026-08-30 by the lead during the coordinated HC write-hold window:
- FAIL end (live tree, attempt-1 code): 7/8 PASS with exactly one failure,
  `preview-group-monthly` — "group monthly 278.0, want net ~166.67
  (gross-abs defect shows ~277.78)". The RIGHT failure: the gross-abs
  defect of ruling SY-2026-08-30a, not a harness error. ORACLE FAIL.
- PASS end (throwaway cp -a copy with the two contract fixes applied:
  `Total += -t.Amount` netting and the Name+def-ID sort tiebreaker):
  8/8 PASS, ORACLE PASS. Copy discarded.
Note: the count check does not discriminate (the reversal row matches the
amount-only def in both variants); the discriminating check is the net
monthly. The v2 determinism mutation (delete-the-sort) is enforced by the
worker's amended AC5 test, not by the oracle.

## Run conventions (added after SY4 attempt 2)

Checker probes and mutations run via `go test -overlay` ONLY — never as
files on the shared disk, even add-run-delete throwaway probes. Rationale:
during SY4.2 verification, one checker's on-disk probe made the other
checker's concurrent whole-tree `go test ./...` fail unattributably.
Overlay state never touches the tree; sha256 the touched sources at start
and finish anyway.

## Rulings

- **SY-2026-08-30a (lead CONCEDES checker-second FAIL, attempt 1):** the
  displayed excluded-group Total summed `math.Abs` per row, so a refund
  INFLATED the total ($2,500 shown for a 4×$500-payments-plus-one-$500-
  reversal group whose true net is $1,500). Defect traced to the LEAD'S OWN
  dispatch instruction — the worker implemented it verbatim. Contract
  amended in D-SY-e (net refunds: `Total += -t.Amount`), matching the
  major-expenses net-spend convention. Implicit UPHOLD; no panel.
- **SY-2026-08-30e (lead CONCEDES checker-second FAIL, SY4 attempt 3 —
  HARD STOP):** the set-exclusion rewrite is sound at every metrics.go
  site (both prior probes + a both-signs-diverging probe pass; primary
  lane's 12-mutant battery all kill), but `buildBudgetVsActualChartData`'s
  cumulative walk was left half-rewritten: it now sums `livingMonth +
  hcAmt` where livingMonth is an independent |LivingOutflows bucket| —
  master's identity `livingMonth = expAmt − hcAmt` had cancelled hcAmt
  exactly, so the walk recombination needed the same merge-then-one-Abs
  rewrite metrics.go's walk got. Proven live: ~$615 chart-vs-metrics walk
  divergence WITH planExclusions=nil on a sign-divergent month —
  a nil-map regression vs master, impossible pre-attempt-3. Third failed
  attempt at Tier 2 → hard stop per the constitution; reported to the
  user. Remediation if the user authorizes an attempt 4: rewrite the
  chart walk's accrual to merge the two buckets then Abs once (mirroring
  metrics.go), plus a chart-walk-vs-metrics-walk equality test on a
  sign-divergent fixture with nil AND non-nil maps. Implicit UPHOLD; no
  panel.
- **SY-2026-08-30d (lead CONCEDES checker-second FAIL, SY4 attempt 2 —
  CONTRACT DEFECT, T18 rule):** second consecutive failure to the same
  root class (sign × abs interaction), so the defect is the LEAD'S
  contract, which prescribed "subtract the flagged net" — arithmetic
  subtraction from an independently-Abs'd total. Checker-second's probe
  (verified by the lead by hand): living remainder S=+3000 (−1000 grocery
  + 4000 outflow-typed credit), ordinary flagged F=−500 → code computes
  |S+F|+F = 2000, correct is |S| = 3000. Attempt 2 fixed only the case
  where the FLAGGED group's sign diverges; the remainder's own sign was
  never exercised. Contract REWRITTEN for attempt 3 (last before hard
  stop): **set exclusion, not arithmetic subtraction** — flagged rows are
  REMOVED from the outflow set before the pre-existing living arithmetic
  runs (`|sum(outflows − flagged)| − healthcareTotal` at range, month,
  and walk granularity; the chart bar likewise). No planExcluded term may
  appear in any living formula; PlanExcludedTotal (signed net, unchanged
  convention) becomes display-only annotation data. Nil-map behavior is
  byte-identical to master by construction (empty set subtraction). The
  pre-existing HI-abs quirk (|S_HI| with a net-refund HI month) is
  master-native and out of scope. Required new fixtures: remainder-sign-
  divergent (remainder nets refund, flagged ordinary) in every consumer
  package, alongside the kept flagged-sign-divergent ones. Implicit
  UPHOLD; no panel. checker-tests' attempt-2 verification was stood down
  mid-run as moot.
- **SY-2026-08-30c (lead CONCEDES checker-second FAIL, SY4 attempt 1):**
  every SY4 consumer subtracted `math.Abs(planExcludedSet.SumAmount())`
  from an already-absolute living total — wrong whenever the flagged
  group's net and the remaining total differ in sign. Probe through the
  real Calculate: flagged $2,000 payment + $2,500 refund (net refund
  +$500) beside $3,000 rent → LivingExpensesTotal 2,000, want 3,000.
  Same defect class as SY-2026-08-30a, at the metrics layer. Contract
  amended: the flagged net is SIGNED — `planExcludedTotal :=
  -planExcludedSet.SumAmount()` (positive = net spend, negative = net
  refund; the SY1 `Total += -t.Amount` convention) — and
  `DashboardMetrics.PlanExcludedTotal` carries that signed net. Every
  consumer package must gain a sign-divergent fixture (flagged refunds
  exceed flagged payments) asserting the living figure. checker-tests'
  attempt-1 PASS stands as evidence for everything it covered; its F1
  (no test executes the handlers' map-BUILDING code — mutants B_dash,
  B_kpis, B_chart survive) is folded into attempt 2 as a required
  handler-level wiring test per site, calibrated against those three
  mutants. Implicit UPHOLD; no panel.
- **SY-2026-08-30b (lead CONCEDES checker-tests FAIL, attempt 1):**
  `ExcludedGroups` aggregated by def ID but sorted by non-unique Name via
  unstable sort.Slice — two flagged defs sharing a Name randomize the
  order under map iteration, syncPlanHash flips between preview and apply
  (probe: 2 distinct hashes / 40 runs), and the user gets a spurious 409.
  Contract amended in D-SY-d (tiebreaker). Also per checker-tests mutation
  M4: the attempt-1 determinism test used ONE excluded group and survived
  deletion of the sort it guards — attempt 2's test must kill that
  mutation. Implicit UPHOLD; no panel.
