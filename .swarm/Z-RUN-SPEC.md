# Z run — post-close review findings (2026-08-30)

Five findings from the user's review pass, all verified by the lead against
master @ 9d51cd0. Prefix `Z` (V/W/X/Y taken). Single lead session, no
concurrent runs, clean tree.

## Lead decisions

- **D-Z-a (Z1 scope):** Z1 covers `OneTimeExpenses` only. `BigTicketItems`
  are ALSO absent from PV, but they are balance events with tax treatment
  (and an income type), not expense-stream entries — folding them into
  PVExpenses is a modeling change the user hasn't asked for. Parked in
  NEXT.md.
- **D-Z-b (Z1 discounting):** each one-time expense discounts at the month
  the engine charges it. Engine semantics (expense.go:50): amount inflated
  by `compoundedFactorFromPercent(s.InflationRate, e.Year*12)`, charged in
  year `e.Year`. PV leg must reuse `presentValueOfMonthlyStream` (or match
  its `(m+1)` ordinary-annuity convention exactly) so timing matches the
  other stream legs.
- **D-Z-c (Z2 contract):** mirror X7's guard. The preview partial carries
  `expected_scenario` (active scenario at preview time) and `plan_hash`
  (SHA-256 of the canonical JSON of the computed syncPlan). Apply recomputes,
  then verifies BOTH before saving: scenario mismatch or hash mismatch →
  409, render an error partial telling the user the data changed and to
  re-open the preview; nothing is written. Missing/blank params → 400.
- **D-Z-d (Z5 mechanism):** the stepper records the active phase name at the
  same site that applies the multiplier (stepper.go:237 uses ACTIVE settings
  `s`), via the existing `GetSpendingPhaseNameAt` (whatif.go:466), onto
  `ProjectionYearSummary.PhaseName string json:"phase_name,omitempty"`.
  `buildSpendingTrajectoryRows` reads the summary's PhaseName and falls back
  to `trajectoryPhaseName(s, yi)` only when the summary lacks one. This
  guarantees label == applied config by construction.
- **D-Z-e (Z4 semantics):** when `RothConversion.Enabled` is false, the
  effective current amount is 0: `conversionSweepCurrentAmount` returns 0,
  so the $0 row is marked Current and the preserved AnnualAmount is neither
  highlighted nor force-enabled. The ladder still includes the preserved
  amount as a candidate? NO — plain ladder only; a disabled plan's retained
  amount is a UI convenience, not a plan value.

## Tasks

| ID | Tier | Checks | Summary |
|----|------|--------|---------|
| Z1 | 2 | checker-tests + checker-second | PV includes one-time expenses |
| Z2 | 2 | checker-tests + checker-second | Sync apply scenario+hash guard |
| Z3 | 1 | checker-tests | Case-insensitive Health Insurance exclusion in sync |
| Z4 | 1 | checker-tests | Sweep treats disabled conversion as current=0 |
| Z5 | 1 | checker-tests | Trajectory phase labels follow chain transitions |

Territories: Z1 `internal/services/retirement/analysis/`; Z2+Z3
`internal/handlers/whatif/sync*.go` + `web/templates/**/sync-preview*`
(SERIALIZED: Z3 accepted before Z2 dispatch); Z4
`internal/handlers/whatif/handlers_sweep*.go`; Z5 engine stepper +
`internal/models/whatif.go` (ProjectionYearSummary) +
`internal/handlers/whatif/spending_trajectory*.go`.

### Z1 — PV omits one-time expenses (P1, Tier 2)
`internal/services/retirement/analysis/present_value.go` totals living,
property tax, healthcare, and expense sources; `OneTimeExpenses` never enter
`pvExpenses`, while the projection charges them
(`engine.OneTimeExpensesForYear`, stepper.go:226). A planned roof reduces
projected balances but leaves PVExpenses/coverage/surplus unchanged.

Acceptance:
1. Each one-time expense contributes its inflated amount (engine's exact
   inflation rule) discounted at its charge month, per D-Z-b. Expenses
   beyond the projection horizon (`e.Year >= s.ProjectionYears`) contribute 0.
2. Regression tests in `analysis`: (a) a fixture with one one-time expense
   asserts PVExpenses rises by the closed-form expected discounted amount
   (recomputed independently in the test, tolerance ~1e-6 relative);
   (b) zero-discount-rate case where PV contribution equals the inflated
   amount exactly; (c) an expense past the horizon contributes nothing.
3. `go build ./... && go test ./internal/services/retirement/...` green.
4. Do NOT touch BigTicketItems (D-Z-a).

### Z2 — sync apply can hit a different scenario than previewed (P1, Tier 2)
`handleWhatIfSyncApply` (internal/handlers/whatif/sync.go:291) reloads
whichever scenario is active and recomputes; nothing binds the confirmation
to the preview. Implement D-Z-c.

Acceptance:
1. Preview handler embeds `expected_scenario` + `plan_hash` in the rendered
   partial (and includes them in the JSON fallback).
2. Apply: blank/missing param → 400 before any write; scenario mismatch or
   recomputed-plan hash mismatch → 409 + guidance, no write; match → save
   exactly as today.
3. Hash is computed by one shared function used by both handlers
   (split-classification rule: ONE source).
4. httptest regression tests: 400 path, 409 scenario-mismatch path, 409
   hash-mismatch path (mutate a transaction between preview and apply, or
   inject differing plans), and the happy path proving settings were saved.
5. `go build ./... && go test ./internal/handlers/whatif/...` green.

### Z3 — Health Insurance exclusion is case-sensitive (P2, Tier 1)
sync.go:130 compares `t.Category == metrics.HealthInsuranceCategory`, while
the dashboard's split uses `FilterByCategory` (models/transaction.go:246),
which lowercases both sides. Use `strings.EqualFold`.

Acceptance:
1. Exclusion matches category case-insensitively — same predicate semantics
   as FilterByCategory.
2. Regression test: transactions categorized `HEALTH INSURANCE` and
   `health insurance` are excluded from NewMonthlyExpenses.
3. `go test ./internal/handlers/whatif/...` green.

### Z4 — sweep treats disabled retained conversion as active (P2, Tier 1)
`conversionSweepCurrentAmount` (handlers_sweep.go:94) ignores
`RothConversion.Enabled`; handlers_rates.go preserves AnnualAmount on
disable; `candidateSettingsForConversionAmount` re-enables it. Implement
D-Z-e.

Acceptance:
1. `conversionSweepCurrentAmount` returns 0 when RothConversion is nil OR
   Enabled is false.
2. Regression tests: disabled config with AnnualAmount=25000 → current 0,
   the $0 row is Current, 25000 is not injected into the ladder; enabled
   config unchanged behavior.
3. `go test ./internal/handlers/whatif/...` green.

### Z5 — trajectory phase labels ignore chain transitions (P2, Tier 1)
`trajectoryPhaseName` reads the PRIMARY settings' phase config for every
year; the stepper's multiplier follows post-chain ACTIVE settings. Implement
D-Z-d.

Acceptance:
1. `ProjectionYearSummary` gains `PhaseName` (omitempty), recorded from
   active settings at multiplier-application time.
2. `buildSpendingTrajectoryRows` prefers the summary's PhaseName; falls back
   to `trajectoryPhaseName` when empty (phases disabled → "-" as today).
3. Regression tests: (a) engine test — chained scenario whose linked
   settings rename/rearrange phases → summaries after the transition year
   carry the linked scenario's phase names; (b) handler/rows test — rows
   reflect the summary names, and the no-chain case is byte-identical to
   today's labels.
4. `go build ./... && go test ./internal/services/retirement/... ./internal/handlers/whatif/...` green.

## Rulings

- **Z-2026-08-30a (Z5 attempt 1, conceded FAIL).** checker-second: the new
  `ProjectionYearSummary.PhaseName` overloads `""` — both "legacy
  projection, no data" and "chain-active settings have phases disabled" —
  so the handler's fallback relabels chain-disabled years from the PRIMARY
  settings (showed "Slow-Go" where "-" is correct). checker-tests PASSed
  the same attempt (the enabled→enabled path is sound). Lead sided with
  the FAIL — UPHOLD-equivalent, no panel (X-2026-08-29b economics).
  Remedy D-Z-f: the engine records the literal no-phase token `-` when
  the active settings yield no phase name (including the month-0 seed),
  restoring `""` to a single meaning (pre-field projection); handler
  preference logic unchanged. Attempt 2 also folds in checker-tests'
  observations F-Z5-1 (promote the no-chain preference-branch parity
  probe to a committed test) and F-Z5-2 (cover rearranged StartAges, not
  just renamed phases).
- **Z-2026-08-30b (Z2 attempt 1, conceded FAIL).** checker-second proved the
  new sync-apply guard checks expected_scenario in its own lock
  acquire/release and saves via SaveWithRevision, which never re-checks —
  an injected scenario switch between check and save returned 200 and
  wrote to the un-previewed scenario. Weaker than the X7 precedent
  (ApplyOverrides compares INSIDE the held lock, twice). checker-tests
  PASSed the same attempt (hash determinism airtight: 6 timezones,
  midnight-straddling, 4 guard mutants each killed). Lead sided with the
  FAIL — no panel. Remedy D-Z-g: a SettingsManager method performs the
  scenario comparison and the write in one held lock (ApplyOverrides
  pattern); handler maps the mismatch to 409; hash comparison stays
  outside the lock (the saved plan IS the hashed plan — only scenario
  identity can rot).

## Backlog findings (observations, not defects — for NEXT.md at close)
- PVExpenses doc comment (models/whatif.go ~1136) omits one-time expenses
  (Z1 second lane). Lead will fix in final pass.
- Positive-side horizon boundary test (Year==ProjectionYears-1) for the PV
  leg (Z1 primary).
- Unicode edge: EqualFold vs FilterByCategory's ToLower can disagree on
  exotic runes (Kelvin sign) — agree on all ASCII (Z3 checker).
- Sentinel collision: enabled-but-unnamed phase would render "-";
  unreachable via handlers today (Z5.2 second lane).
- Dual "-" literals: engine noPhaseSentinel vs trajectoryPhaseName —
  agree today, could drift (Z5.2 second lane).
- F-Z5-3 stale comment in attempt-1 fallback test; F-Z5-4 promote the
  composed engine+handler probe to a committed test (V3 pattern);
  F-Z5-5 phase_name:"-" now serializes where key was absent (no golden
  breakage found).
- Z2 happy-path test asserts income source but not MonthlyLivingExpenses
  (pre-existing narrowness); sort.Slice on IncomePatterns is load-bearing
  but no committed test kills its removal (V3 candidate); preview list
  order changed from largest-total-first to alphabetical.
