# Review Fixes — Build Spec (run R)

Date: 2026-08-19. Source of truth for the defects: the external review of the
accounts & transfers run (A0–A9), reproduced and verified against the tree at
branch `fix/review-aug16` before this spec was written. Semantic authority:
`GLOSSARY.md`. Accessibility authority: `ACCESSIBILITY.md`.

## Scope

Seven correctness defects in code shipped by runs P and A. Four are data- or
consent-integrity bugs (P1 in the review), three are correctness bugs in
reported figures (P2). No new features, no schema changes, no UI redesign.

Out of scope: the near-duplicate engine itself, the transfer pairing
algorithm, storage encryption, anything not named in a task below.

## Verified premises

Each defect was reproduced in the source before dispatch. Two differ from the
review as filed and the briefs carry the corrected statement:

- **R6 is nondeterministic, not merely stale.** `latestAnchorAtOrBefore`
  (`internal/services/accounts/balance.go:166`) keeps the *first* same-day
  anchor it encounters — `ad.After(dayOf(best.Date))` is false on a tie — while
  its own doc comment claims ties resolve to the last seen. The UI's
  `sort.Slice` on append is not a stable sort. So with two anchors on one day,
  which one is authoritative is arbitrary and may change across saves. The doc
  comment is part of the defect.
- **R3 and R5 share a call site.** `internal/services/mcpsvc/ledger/accounts.go`
  passes raw `ts.Transactions` to `BalanceAt` *and* to `Project` *and* to
  `recurringForProjection`. R3 fixes the active-set filtering; R5 fixes the
  as-of truncation at the same lines. R5 depends on R3.

Two fixes have an existing in-tree precedent the worker must follow rather
than invent:

- **R1**: `storage.SharedTx` / `Storage.BeginShared`
  (`internal/services/storage/storage.go:627`) exists and is documented for
  exactly this failure — a sidecar read-modify-write whose write lands after a
  restore and resurrects pre-restore state. `dataloader` pairs it with its own
  `writeMu`. The accounts sidecar never adopted the pattern.
- **R2**: `Storage.CreateExclusive` exists, is tested, and the upload path at
  `internal/handlers/explorer/handlers.go:911-913` carries a comment stating
  why Stat-then-write is wrong. The import path does the wrong thing anyway.

## Lock-order constraint (binds R1)

From `storage.go`: settings rewrite gate → `BeginExclusive` → backup snapshot
hold; and, for shared holders, *the caller's own serialization →
`BeginShared`*, never the reverse. Nothing holding the data lock in either
mode may then wait on that serialization, and nothing inside a shared or
exclusive section may call the plain `Storage` write methods. R1's accounts
mutex therefore sits **outside** `BeginShared`, mirroring dataloader's
`writeMu`. A worker that inverts this deadlocks the app.

## Task breakdown

Statuses live in `.swarm/ledger.tsv` (tasks `R1`–`R7`), never here.

```
R1 → R6
R3 → R5
R2, R4, R7 independent
```

| Task | Review # | Scope | Tier | Checks | Acceptance criteria (summary — worker brief carries full text) |
|------|----------|-------|------|--------|----------------------------------------------------------------|
| R1 | 1 | Serialized read-modify-write for the accounts sidecar: one mutex-guarded mutate API in `internal/services/accounts` used by **both** the HTTP handlers (`internal/handlers/accounts/handlers.go` — create, update, add-anchor, delete-anchor) and the MCP anchor path (`internal/services/mcpsvc/ledger/anchor.go:196`). Load and Save happen inside one held section, over `Storage.BeginShared`. | **3** | tests, second | Oracle `.swarm/tier3/R1/accept.sh`. Concurrent mutations under `-race` lose no writes (N goroutines each adding a distinct anchor → all N present). A restore interleaved between a mutation's load and save does not resurrect pre-restore accounts. Lock order per the constraint above; no plain `Storage` write inside a held section. Existing accounts tests unchanged and passing. |
| R2 | 2 | Folder import writes with `Storage.CreateExclusive` instead of Stat-then-`WriteFile`. `os.ErrExist` maps to `Status: "skipped"`, and a skipped import must **not** delete the source. Keep the readback verification and the non-`ErrNotExist` Stat rejection. `importDeps.write` is already the seam. | 2 | tests, second | Concurrent import + upload of the same basename: exactly one write wins, the loser reports skipped, the destination holds the winner's bytes, and the loser's source file still exists. Existing import outcome tests (rejected/imported/skipped reasons) unchanged. |
| R3 | 3 | Pass the active transaction set to balance and projection call sites: `internal/handlers/dashboard/handlers.go:114` (`data.Active()`, matching line 101) and `internal/services/mcpsvc/ledger/accounts.go` (`get_accounts` and `get_balance_projection`). No change to the explorer, which deliberately keeps raw rows. | 2 | tests, second | Fixture with a resolved duplicate pair (one row `Suppressed=true`): account balance, low-balance warning, and projection each count the debit once. Explorer still shows both rows. `BalanceAt` itself is unchanged — the filtering is the caller's job, per `Active()`'s doc comment. |
| R4 | 4 | Bind a pending browser approval to the exact operation shown, not to `(tool, subject)`. `confirm.Approvals.Create`/`Find` key on the operation identity — the confirm-token argument hash — so a second request for the same subject with different arguments cannot replace or be mistaken for the first. All four guarded call sites updated: `set_balance_anchor`, `resolve_transfer`, `restore_backup`, `shutdown_server`. | **3** | tests, second | Oracle `.swarm/tier3/R4/accept.sh`. Two concurrent anchors on one account with different amounts: each invocation awaits and consumes only its own approval; approving one does not authorize the other. Opposite verdicts on one transfer pair likewise. A single-operation approval still round-trips (existing tests pass). Answering an expired or superseded request is refused, not silently applied. Detail text shown to the human continues to name the load-bearing facts. |
| R5 | 5 | `get_balance_projection` honours `as_of`: truncate the active set at `as_of` and call `insights.DetectRecurringAt(ts, asOf)` instead of `DetectRecurring`. Applies to `recurringForProjection` and the `Project` input at `internal/services/mcpsvc/ledger/accounts.go:285`. | 2 | tests, second | A historical `as_of` schedules recurrence from evidence at or before that date only, and can schedule inside its window. A future `as_of` evaluates freshness against the requested date, not the ledger maximum. Default (no `as_of`) output is unchanged from today on a fixture. Depends on R3. |
| R6 | 6 | Accounts UI stops appending a second anchor for a date that already has one: replace same-day (matching the MCP path at `anchor.go`) or reject with a field error. Also correct `latestAnchorAtOrBefore`'s doc comment, which describes tie behaviour the code does not implement. | 2 | tests, second, a11y | Handler test: adding an anchor for an existing date leaves exactly one anchor for that date and the new amount is the one balances use. If the chosen behaviour is rejection, the error is announced per `ACCESSIBILITY.md` (field-level, programmatically associated, not colour-only). Doc comment matches code. |
| R7 | 7 | `resolve_transfer` refreshes the ledger before validating the pair key and re-validates before writing, using the existing `deps.load()`. Tests must stop preloading the queue to make the tool work. | 2 | tests, second | Fresh server, no prior load: a valid pair key resolves instead of being rejected. After the underlying CSVs change, a pair key that is no longer suspected is rejected rather than written. Existing resolve tests pass **without** their explicit queue preload. **Constraint:** implement inside `internal/services/mcpsvc/ledger/resolve.go`. If the fix appears to require modifying `internal/services/dataloader/**` or `internal/services/transfers/**`, stop and report — those are critical globs and the task escalates to Tier 3 rather than being widened by the worker. |

## Tier justification

Per `TIERS.md`, the answer that drove each:

- **R1 — Tier 3.** Not reversible: a lost or resurrected account write is user
  money data, and the failure is silent. Shared blast radius (HTTP + MCP both
  call it). Concurrency oracles are probabilistic even under `-race`.
- **R4 — Tier 3.** Not reversible: this *is* the consent boundary for every
  guarded mutating tool. A defect here authorizes a write the user did not
  agree to. Shared across four tools.
- **R2, R3, R5, R6, R7 — Tier 2.** Each has a strong executable oracle and a
  small blast radius, but every one of them touches money figures or money
  data, so the rubric's tie-break rounds up from 1.

No task is Tier 1. Tiers may only move up mid-run.

## critical.globs amendment

Proposed additions, for user sign-off:

```
internal/services/accounts/accounts.go
internal/services/mcpsvc/confirm/**
```

Applied 2026-08-19. Rationale: the accounts sidecar is the money-data store the
dashboard's every figure is rolled forward from, and `confirm/` is the consent
boundary. Both belong beside `storage/`, `dataloader/` and `transfers/`.
Neither changes this run's tiers — R1 and R4 are already Tier 3, and
`escalate-scan` writes no flag for a task already at the ceiling.

**Amended during Phase 0.** The glob was first drafted as
`internal/services/accounts/**`. `escalate-scan` walks the whole ledger, so that
form retroactively flagged the accepted tasks A4 and A5 — whose manifests are
only `balance.go` and `projection.go`, pure calculators that read transactions
and write nothing — and blocked `gate.sh done` for the repo. Narrowed to
`accounts.go`, the load/save/validate surface where a bad change actually loses
data (user ruling 2026-08-19a). The two stale flags were removed after
confirming `critical-glob` was their sole recorded reason; `escalate-scan` no
longer rewrites them and both tasks accept at tier 2 again.

Note for future amendments: a flag whose triggering condition disappears is not
self-clearing. `escalate-scan` only removes a flag once the ledger tier has been
raised to meet it, so a withdrawn glob leaves flags that must be cleared by
hand after checking no other reason is recorded in them.

## Rulings

**2026-08-19a — critical.globs narrowed from `internal/services/accounts/**` to
`internal/services/accounts/accounts.go`.** The package-wide form retroactively
escalated the accepted tasks A4 and A5, whose only manifest entries are the
read-only balance and projection calculators. The glob exists to guard the
money-data *store*; a calculator that writes nothing is not that. User ruling.
