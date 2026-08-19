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

**2026-08-19b — R5 attempt 2, judge panel: 2 OVERRULE / 1 UPHOLD. FAIL set aside,
work accepted.** The first genuine dispute of the run: checker-tests PASS,
checker-second FAIL. Both were factually honest and each independently
reproduced the other's key claim; the disagreement was about standards, not
facts.

Agreed facts: attempt 1's real defect (the default no-as_of path silently
changed, reference_amount 777.00 -> 0.00) is fixed and re-confirmed. The
explicit-as_of normalisation at accounts.go:307 uses asOf.Location(), which is
provably the identity for every reachable input because as_of parses via
time.Parse("2006-01-02", ...). No committed test pins it: swapping in
time.Now().Location() leaves the whole suite green under every timezone tried.

- judge-standards UPHELD. It read the ambiguous TZ-test bullet in the WORKER's
  favour, then found a different obligation unmet on plain text: "mutation check
  per fix" carries no exception, and the normalisation is one of the three
  enumerated fixes. It also noted checker-tests' own mutation only tested
  REMOVING the line (proving it inert), not the failure mode it guards.
- judge-claude OVERRULED, decisively: deleting line 307 outright leaves
  behaviour byte-identical and the suite green, so the mutation survives only
  BECAUSE the worker added the defence the brief asked for. "A standard that
  rejects the worker who added the requested defence, and accepts the one who
  omitted it, is measuring mutation-score aesthetics on inert code, not
  correctness." It further found the brief's own text exempts the explicit path
  ("Explicit as_of is unaffected because it parses to UTC"), that "by
  construction" attaches to criterion 3 / the default branch, and that the
  worker followed the mandated dayOf convention exactly -- dayOf itself uses
  t.Location(), so any hazard is a property of the convention the lead
  specified.
- judge-impact OVERRULED on consequence: no user-reachable input produces a
  wrong number today, while a third failed attempt would halt the task at the
  hard stop and ship the ORIGINAL reviewed defect unfixed.

Carried forward, not discarded: a unit test calling
activeSetAndRecurringForProjection directly with a non-UTC asOf would kill the
surviving mutant cheaply. Logged as desirable hardening alongside R12, not as an
unmet acceptance criterion.

Lead's note: the lead did not adjudicate this personally because the dispute
turned partly on whether the lead's own brief was clear. Two judges found it was
not, in the worker's favour. That is a defect in the brief, not in the work.

**2026-08-19a — critical.globs narrowed from `internal/services/accounts/**` to
`internal/services/accounts/accounts.go`.** The package-wide form retroactively
escalated the accepted tasks A4 and A5, whose only manifest entries are the
read-only balance and projection calculators. The glob exists to guard the
money-data *store*; a calculator that writes nothing is not that. User ruling.


## Coverage gaps found during verification (recorded, not separately tasked)

These were surfaced by checkers, judged not to warrant their own ledger task,
and are written down so they are not lost with the transcript.

- **Anchor day-comparison is untested for non-midnight times** (found by R6's
  adversarial lane). Mutating `sameCalendarDay` to an exact `.Equal(date)`
  comparison is caught by no test; the suite stays green. It is latent only
  because every current write path parses anchors with
  `time.Parse("2006-01-02", ...)`, producing midnight UTC. The same blind spot
  pre-exists in the accepted MCP-side `sameDay`. Any future path that writes a
  timestamped anchor breaks the day comparison silently.

- **`latestAnchorAtOrBefore` still resolves same-day ties nondeterministically**
  for data not created through the UI or MCP paths (hand-edited accounts.json,
  future import). R6 closed the entry point, not the underlying tie-break. The
  corrected doc comment now says so explicitly.

- **Tier-3 oracles asserted return values only.** `accept.sh` for R1 and R4
  checked signatures, concurrency and return values but never HTTP status codes
  or log side effects, which is how two regressions of exactly that kind
  (500 -> 200, dropped OverlapWarnings) survived R1's oracle and had to be
  caught by a checker. A future Tier-3 `accept.sh` should assert observable
  side effects.

- **R8's test carries a wrong explanatory comment** (found by its checker).
  `import_binding_race_test.go:20-23` predicts that under the reverted binding
  "every goroutine would report imported". The observed mechanism is different:
  the step-3 Stat fast-path absorbs late arrivals, so the mutant yields
  imported==1, skipped==12, 3 rejected, and the test dies on the skipped-count
  assertion instead. The test is correct; its stated reasoning is not. Fix in
  the final review pass -- a wrong explanation in a concurrency test misleads
  the next reader.

- **No test covers "ErrExist branch + delete enabled + unstubbed binding".**
  R8's checker showed this cannot be covered by a deterministic test of R8's
  shape: with deleteSource=true the winner's os.Remove races the losers'
  os.Lstat, failing 10/10 for an unrelated reason. Would need a different test
  design.

- **Tier 3 is wasted on over-constrained tasks.** R4's two blind arms produced
  byte-identical production code because the brief pinned the API, the call
  sites and the opID. Pin only what the shared oracle must call. See the
  RESOLUTION in `.swarm/tier3/R4/report.md`.


## RESOLVED — debug output in production code (2026-08-19)

Twelve `println("DEBUG ...")` lines briefly appeared in `Project` in
`internal/services/accounts/projection.go`. The lead attributed them to R9's
in-flight worker on timing; that attribution was WRONG. They belonged to R5's
adversarial checker, instrumenting its investigation, and it reverted them
exactly as its report claimed. `projection.go` is byte-clean. The process point
below still stands: nothing in the gate would have caught them.

## (superseded) BLOCKER — debug output in production code

`internal/services/accounts/projection.go` currently carries 12 added lines of
`println("DEBUG ...")` inside `Project`, the funding-projection engine. These
write to stderr on every projection and are in no task's manifest.

Almost certainly R9's live instrumentation while diagnosing the
TestDashboardAndMCPProjectionAgree flake -- a legitimate technique mid-task,
but it is in a money path and must not survive. Not removed while R9 is still
running, because deleting a diagnostic mid-diagnosis would sabotage the task.

MUST be verified gone before: R9 is accepted, `gate.sh done` is run, or
anything is committed. If R9 does not clean up after itself, the lead removes
these lines in the final review pass.

Process note: this was surfaced by R6's checker noticing the tree changing
under its test run, not by any check aimed at it. Nothing in the gate would
have caught debug output added to a file outside every manifest -- worth a
mechanical check (e.g. grep for println/fmt.Print in non-test Go files) in
`gate.sh done`.


## R12 — byDay map keyed by time.Time compares Location pointers (found 2026-08-19)

NOT DISPATCHED. Pre-existing, independent of every task in this run, and a real
money-correctness defect. Found incidentally by R5's adversarial checker while
building a counterfactual.

`internal/services/accounts/projection.go:116` declares
`byDay := make(map[time.Time]float64)`. Keys are WRITTEN as
`occ := dayOf(rp.NextExpected)` (line 129) and READ as
`day := asOfDay.AddDate(0, 0, d)` where `asOfDay = dayOf(asOf)` (line 117, 149).

`dayOf` rebuilds the value in **its own argument's Location**
(`time.Date(..., t.Location())`). Go compares `time.Time` map keys by struct
fields, INCLUDING the `*Location` pointer. So when `asOf` derives from
`time.Now()` (Local) and `rp.NextExpected` derives from parsed CSV dates (UTC),
the read key never matches a written key. Every recurring outflow is dropped:
`Crossing` is never set, `Minimum` stays at the starting balance, and
`SuggestedTopUp` is 0.

Impact: on any host where TZ != UTC, MCP `get_balance_projection` called
WITHOUT an explicit `as_of` has been silently reporting "no crossing" whatever
the data says. The dashboard is unaffected because it derives asOf from the
ledger's own MaxDate, which carries the transactions' location.

Likely fix: key the map on a normalized value (UTC-truncated date, or a
`yyyy-mm-dd` string) rather than a raw `time.Time`. Cheap, but it changes
figures users see, so it needs its own verification.


## PROCESS DEFECT — verification ran against a concurrently-mutated tree

Found by R9 (2026-08-19), and it is a defect in the LEAD's orchestration, not in
any worker's or checker's output.

R9 was asked to fix a 1-in-27 flake in TestDashboardAndMCPProjectionAgree. It
could not reproduce the failure in **5,100+ executions** across isolated,
per-process, raced, unraced, and whole-package conditions. Instead of
guess-patching, it documented what it directly observed:

- `zz_adversarial_default_test.go` appeared mid-session referencing an undefined
  symbol, changed twice, vanished; `zz_adversarial_parity_test.go` then appeared.
  (These were R5's adversarial checker's scratch files.)
- `go vet` / `go build` on the package FAILED mid-run because of that file.
- A 3000-iteration batch failed every time on another agent's in-flight test.
- `accounts_card.go`, `anchor.go`, `resolve.go`, `approvals.go` all changed size
  and mtime during its run — files R9 was forbidden to touch.

Mechanism for the original flake: `dashboard_parity_test.go` is the only test in
that package reading live shared state — `dashboard.Initialize` ->
`templates.New(templateDir, false)` globs and `os.ReadFile`s every `.html` under
the shared repo's `web/templates/**` at construction. A concurrent write during
that non-atomic multi-file read yields a torn render. That matches the observed
signature exactly: lines 175 AND 181, both dashboard-render assertions, failing
together in one run while the MCP-JSON assertions in the same run passed.

R9 correctly declined to call this a confirmed diagnosis (it cannot rewind to the
failing run) and declined to "harden" the test against an unconfirmed cause.

### What this means for the run

The lead dispatched workers and checkers concurrently against ONE shared working
tree. Every verification result in this run was produced against a tree that
other agents could be, and demonstrably were, editing. This does not invalidate
the accepted work — the tree's FINAL state is what the gate checked, and each
checker re-ran its own commands — but it does mean:

- Any single observed failure may be a collision artifact rather than a defect.
- Any single observed pass may have been measured against a transient state.

### Required mitigation before `gate.sh done`

Run one full `go build ./... && go vet ./... && go test -race ./...` on a
QUIESCENT tree, with zero workers or checkers in flight, and treat THAT as the
run's toolchain evidence. Nothing else is trustworthy at face value.

### Fix for future runs

Give each verification pass a private worktree, as Tier 3 already does for its
N-version arms. Tier 3 was isolated; Tier 2 verification was not, which is
backwards — Tier 2 is where most of the run's verification actually happens.


## Shared-tree contamination, second instance (2026-08-19)

R5 attempt 2's two checkers ran concurrently against the same working tree. The
ADVERSARIAL lane applied its mutation to `internal/services/mcpsvc/ledger/accounts.go`
IN THE REPOSITORY (its report: "applied and reverted only in the working tree"),
while the PRIMARY lane was verifying that same file. The primary lane caught a
snapshot carrying `time.Now().Location()` at line 307 where the committed content
has `asOf.Location()`, and recorded the mtime discrepancy (file written 18:04:21,
manifest 17:58:51).

Both handled it correctly -- the adversarial reverted and re-verified diff-clean;
the primary discarded the contaminated snapshot, re-ran every experiment against a
fresh copy, and hash-pinned the verified content at both ends of its window. No
finding rests on contaminated state.

This is the same lead-side defect R9 identified, now demonstrated BETWEEN TWO
CHECKERS ON ONE TASK rather than between a worker and a checker. The dispatch
told the adversarial lane to write nothing into the repo; it mutated in place
anyway, which is the natural thing to do when no isolated copy is provided.

Reinforces the required fix: every verification pass needs its own worktree. An
instruction not to touch the shared tree is not a substitute for not sharing it.
