# Review Fixes — Build Spec (run S)

Date: 2026-08-20. Source of truth for the defects: the external review of the
21 commits made 2026-08-17 → 2026-08-20 (runs A and R follow-ups). Every defect
below was reproduced in the source at `c363bb4` (branch `fix/review-aug16`,
clean tree) before this spec was written. Semantic authority: `GLOSSARY.md`.
Accessibility authority: `ACCESSIBILITY.md`. Verification rubric: `TIERS.md`.

## Scope

Four correctness defects: two in reported money figures (P1) and two in what
the UI tells the user about state it holds (P2). No new features, no schema
changes, no UI redesign.

Out of scope: the recurring-detection engine itself (`internal/services/insights`),
the transfer *suggestion* pass and its review-queue UI, the accounts sidecar
write path, anything not named in a task below.

## Verified premises

Each defect was read in the tree before dispatch. All four are live at
`c363bb4`.

1. **S1 — one recurring series drains every account it ever touched.**
   `internal/services/accounts/projection.go:155` filters recurring items with
   `recurringBelongsToAccount`, which returns true when **any** leg carries the
   account ID (`projection.go:235`). The recurring engine groups by merchant
   across the whole ledger, so a series whose payment account changed has legs
   on two accounts — and the loop then subtracts the **full** `rp.Amount` on
   **both**. Confirmed: the filter is membership-only; nothing downstream
   apportions. Effect matches the review's diagnostic ($400 projected against a
   series with $200 of occurrences), producing false low-balance crossings and
   inflated `SuggestedTopUp`.

2. **S2 — pairing depends on input row order.**
   `internal/services/transfers/transfers.go:178-195` is a greedy first-index
   pass: row `i` claims `nearest(out, i, cands)` and marks both paired, so a
   later row `j` that was strictly nearer to that same counterparty never gets
   to compete. The documented rule is closest-date. Confirmed: `paired[]` is
   consulted inside `candidates()`, so the claim is final; the only
   order-independent behaviour is the tie path (`ambiguous`), which fires on an
   exact tie for `i` alone, not on a global conflict.

3. **S3 — the resolve handler reports the verdict you asked for, not the one on
   disk.** `internal/handlers/transfers/handlers.go:183-189` branches on `v`
   (the requested verdict) inside the already-resolved error path, so a stale
   tab posting `reject` for a pair another tab already confirmed is told "Pair X
   was already rejected", while `data/transfer_decisions.json` still holds
   `confirm`. Confirmed: `ResolveTransfer` (`dataloader/transfers.go:95`)
   returns the same `no suspected transfer pair with key` error for an
   already-resolved key **and** for a key that never existed, and
   `isAlreadyResolvedError` (`handlers.go:227`) collapses both into the
   "already" message. The persisted map is readable from the handler package
   today via the exported `loader.LoadTransferDecisions()`; no service-layer
   change is required.

4. **S4 — the pattern-overlap warning UI is dead.**
   `pageData.Warnings` (`internal/handlers/accounts/handlers.go:107`) is
   declared and rendered — `web/templates/pages/accounts.html:60` (page) and
   `:136` (the `accounts-list-partial` swap target) both have an amber warning
   block — but nothing ever assigns it. `buildPageData` (`handlers.go:527-578`)
   returns a `pageData` literal with no `Warnings` field, and the mutation
   handlers call the plain `accounts.Save`, whose warnings go to the stdlib log
   (`internal/services/accounts/accounts.go:124`). `SaveWithWarnings`
   (`accounts.go:185`) already exists and returns them. Confirmed: the user
   never sees that two accounts claim the same CSV and that first-match-by-ID
   silently won.

## Semantic decision required before dispatch (S1)

"Which account does a forward occurrence belong to" is a product question, not
an implementation detail, so it is the user's call — the same way the UTC
calendar decision was in run R. Three candidates:

- **(a) Most-recent-leg wins (recommended).** The series is attributed to the
  account of its latest historical leg — the account that is in fact paying it
  now — and projected in full there, and not at all elsewhere. Matches the
  reported symptom (a payment method that moved), keeps the total across all
  accounts equal to one occurrence, and is a small, testable change. Cost: a
  genuinely alternating pair of cards is projected entirely against one of them.
- **(b) Pro-rate by leg share.** Each account gets `Amount × legs_here/legs_total`
  per occurrence. Total is conserved; no account is projected a charge it never
  pays. Cost: every account sees a fractional amount that matches no real
  transaction, and a moved payment method leaves a permanent ghost on the old
  account.
- **(c) Per-account sub-series.** Re-derive cadence and `NextExpected` from that
  account's own legs only. Most faithful; largest change (it re-implements part
  of the detection engine inside the projection) and the least stable when an
  account holds two or three legs.

**DECIDED (user, 2026-08-20): (a) most-recent-leg wins.** The series is
attributed to the account of its newest historical leg and projected in full
there only. The alternating-card limitation is accepted and must be recorded in
the doc comment of whatever function implements the rule, so the next reader
knows it is a chosen trade-off and not an oversight. S1's acceptance criteria
below are written against (a).

## Task breakdown

Statuses live in `.swarm/ledger.tsv` (tasks `S1`–`S4`), never here. All four are
independent; there is no ordering constraint.

| Task | Review # | Scope | Tier | Checks | Acceptance criteria (summary — worker brief carries full text) |
|------|----------|-------|------|--------|----------------------------------------------------------------|
| S1 | P1-1 | Attribute a recurring series to one account in `internal/services/accounts/projection.go`. Replace the any-leg membership filter with the decision above; `recurringBelongsToAccount` is either replaced or reduced to a helper of it. No change to the insights engine, to `BalanceAt`, or to the walk/calendar logic settled by R12. | **2** | tests, second | Fixture: one monthly series, legs on account A (older) then account B (newest), `Amount` 200. Over the horizon, the summed projected reduction across A and B equals one occurrence per period — **not** two. B is charged; A is not. Existing test "other accounts' recurring items are excluded" still passes unchanged. A single-account series is byte-identical to today's projection on a fixture. No `SuggestedTopUp` or crossing appears for an account that no longer carries the series. |
| S2 | P1-2 | Make auto-pairing order-independent and globally closest-date in `internal/services/transfers/transfers.go` (pass 2 only). Candidate pairs are considered in a deterministic global order — date distance first, then a stable tiebreak on the pair's identities — rather than by row index. Existing semantics kept: pattern gating (`pattern[i] || pattern[j]`), user decisions applied first, exact ties go to the review queue as `ReasonAmbiguous`, pass 3 (suggestions) untouched. | **3** | tests, second | Oracle `.swarm/tier3/S2/accept.sh`. **Order independence:** for a fixture with ≥3 competing rows, classifying every permutation of the input slice yields identical pairing (same pair keys, same suspected queue) — asserted over all permutations of a small fixture, not one shuffle. **Closest-date:** the review's counterexample (an earlier, farther candidate that today consumes a pattern-backed leg) pairs the nearer legs regardless of input order. Every input row still appears in the output (`len(out) == len(txns)`, none dropped). `Paired`/`External` counts stay row counts. Existing transfers tests pass unchanged, including the tie→review and decisions-first tests. |
| S3 | P2-1 | In `internal/handlers/transfers/handlers.go`, the already-resolved path reports the **persisted** verdict, read via `loader.LoadTransferDecisions()`, and distinguishes an unknown pair key from an already-decided one. Handler package only — no change to `dataloader/**` or `transfers/**`. | 2 | tests, second | Handler test: confirm pair X, then POST `reject` for X → the message names the persisted verdict (confirm) and states the reject was **not** applied; `transfer_decisions.json` still holds confirm. Re-posting the **same** verdict stays idempotent and keeps its current "nothing to do" wording. An unknown key gets a distinct message that does not claim any verdict was recorded. Neither path returns 4xx (idempotency contract unchanged). If the fix appears to need a change under `internal/services/dataloader/**` or `internal/services/transfers/**`, stop and report — those are critical globs and the task escalates rather than being widened. |
| S4 | P2-2 | **Tier 1 at Phase 0; ESCALATED to Tier 2 on 2026-08-20** by `gate.sh escalate-scan` (`two-consecutive-fails`, attempts 1 and 2), which added the `checker-second` lane. Accepted at attempt 3 after a judge panel. Populate `pageData.Warnings` in `internal/handlers/accounts/handlers.go`: `buildPageData` computes `accounts.OverlapWarnings(accts, csvBasenames())`, and the mutation handlers surface the warnings from `accounts.SaveWithWarnings` on the partial they render. Template markup already exists at `accounts.html:60` and `:136`; change it only if a11y requires it. | 1 → **2** | tests, second, a11y | Handler test: two accounts whose patterns both match one CSV → GET the page and POST a mutation both return `Warnings` non-empty and naming the contested basename; a non-overlapping fixture returns none. The rendered warning is announced per `ACCESSIBILITY.md` — not colour-only, and reachable in the HTMX partial swap, not just the full page. `checker-a11y` runs against the accounts page with warnings present. **Criteria as met at acceptance also include the a11y rework the escalation forced: a stable live region outside the HTMX swap target, a keyboard-dismissible banner, and light-theme contrast at 4.5:1.** |

## Task S5 — post-dispute fixes (added 2026-08-20)

Opened after the S4 dispute. This is a NEW task, not a fourth S4 attempt: the
constitution's three-attempt hard stop is respected, and the work is a
root-cause fix plus test-quality debt that three checkers and one judge
recorded across the run.

| Task | Source | Scope | Tier | Checks | Acceptance criteria (summary) |
|------|--------|-------|------|--------|-------------------------------|
| S5 | S4 dispute + recorded gaps | (a) `accounts.html` `syncWarnings()` returns at `if (!banner) return;` before the dismissal key is ever cleared, so a resolved-then-recreated overlap re-announces with no banner and no dismiss control — contradicting the template's own comment that the empty data carrier exists to clear "the live region and the dismissal key too". Fix so suppression is parity-complete per the new `ACCESSIBILITY.md` point 16. (b) `TestWarningsKey_OrderInvariant`'s non-mutation guard inspects an already-sorted slice and cannot catch an in-place sort. (c) `TestHandlePage_RendererMode_NoOverlapRendersNoWarningBlock` string-scans the whole body including the always-served `<script>`, holding unrelated script copy hostage. (d) S3's `describeAlreadyResolved` load-failure branch is correct but untested (coverage: execution count 0), and its doc comment omits that fourth case. (e) S1's `TestProject_MixedRecurringFiltersByAccount` comment still names the deleted `recurringBelongsToAccount`. | 2 | tests, second, a11y | Dismiss → resolve overlap → recreate the same overlap: banner returns with its dismiss control, and the announcement matches what is on screen. Dismiss → reload: the accessibility tree does not carry warning text with no visible counterpart. Both verified in a real browser, not a harness. (b) guard fails when the sort is made in-place. (c) replacement assertion is scoped to the rendered warning block, and cannot be tripped by unrelated script copy. (d) a test covers the load-failure branch and the doc comment names all four cases. (e) comment matches the code. All existing tests pass unchanged. |

### S5 attempt 1 — verified fixed, failed on durable coverage (2026-08-20)

`checker-a11y` and `checker-second` both PASSed: the point-16 defects are fixed,
confirmed independently in real Chromium with `Accessibility.getFullAXTree`
showing zero nodes asserting the warning after dismiss + reload.
`checker-tests` FAILed on one criterion: **nothing in the repo turns red if it
regresses.** It authenticated the pre-S5 `accounts.html` against the md5 recorded
in `.swarm/verdicts/S4.3.checker-tests.verdict`, restored it, and the suite still
exited 0 — as it does with the whole script block replaced by a comment. budget2
has no JS runner, no `package.json`, and no browser-driving dependency, so every
browser verification this run produced dies with the session.

The lead concurs with the FAIL rather than contesting it: the criterion was
written deliberately, and this defect class survived three S4 attempts precisely
because the Go suite cannot execute client-side code.

**User decision 2026-08-20e:** close the gap with a node-based harness invoked
from `go test` — no `go.mod` dependency, skips cleanly where node is absent, and
a Makefile target that requires it. S5 attempt 2 carries it.

### S5 HALTED at three failed attempts, then redirected (2026-08-20)

Attempts 1, 2 and 4 failed (attempt 3 was the Tier-3 arms). The constitution's
hard stop fired and the task was reported to the user rather than looped again.

What failed each time was never the harness — `checker-tests` confirmed at
attempt 4 that reintroducing the point-16 early return into `accounts.html`
turns `TestSyncWarnings_ClientRegressionHarness` red, so the regression this
task exists to catch is caught. What failed was every attempt to make the
harness impossible to SKIP. Three rounds of Makefile scanning produced three
rounds of evasions: unwired sibling targets (attempt 2), then `$(GOCMD)` aliases,
`define`d canned recipes, line continuations, `include`d fragments and recipe
misattribution (attempt 4) — found independently by both lanes.

**The lead's own oracle shared the blind spot.** `.swarm/tier3/S5/accept.sh`
scanned make's database with the same literal patterns, so a Makefile carrying
either evasion would still have printed `ORACLE: PASS`. A contract that cannot
see the failure class it polices is not a contract. Recorded rather than
quietly repaired.

**User decision 2026-08-20f:** stop policing Make. Move the guard into the Go
test itself — absent node is a FAILURE unless an explicit opt-out environment
variable is set — and delete the Makefile scanning machinery. The guard then
lives where the test lives and no Make idiom can route around it.

**Process deviation, declared:** attempt 5 is implemented by a single worker,
not a blind N-version pair, despite S5 sitting at Tier 3. N-version exists to
explore a design space; the user's directive removes the ambiguity that made the
earlier attempts a search. The oracle remains the contract and is rewritten to
the new design, and the Tier-2 dual-lane verification still applies.

### S5 attempt 6 — the warm-cache claim, and a real hole under it (2026-08-20)

Attempt 6 corrected two stale documentation claims and introduced a third, in
the middle of the sentence it was repairing: that the test cache "never replays"
the harness because it execs node and reads a file outside its package
directory. Both lanes falsified it independently.

`checker-tests` went to `cmd/go/internal/test/test.go`: `computeTestInputsID()`
bounds file tracking at the MODULE root, and a read outside it hits `break`
("Do not recheck files outside the module, GOPATH, or GOROOT root") — so reading
outside the package directory makes caching *less* conservative, not more, and
`exec` is not a testlog opcode at all. The operative mechanism is `getenv PATH`,
proved by holding node fixed and appending an empty directory to `PATH`.

Under the wrong explanation sat a real hole. With node reached through a
symlinked directory on a FIXED `PATH`, then the symlink deleted so no node
exists anywhere:

```
$ make test  ->  exit 0,  ok budget2/internal/handlers/accounts (cached)
```

A fully green `make test` on a machine with no node. The guard fires on a `PATH`
change; it does not fire when node vanishes at an unchanged `PATH` with a warm
cache.

**User decision 2026-08-20g:** document the limitation accurately rather than
disabling result caching for every `make test*` invocation. The scenario needs
node uninstalled without any `PATH` change AND a warm cache AND no other input
edited; the cost of `-count=1` on every target is paid by every developer on
every run. The comment must tell the reader when a cached green is not to be
trusted.

**Lead's own error, recorded.** When `checker-second` PASSed attempt 5, it gave
the exec/outside-the-package-dir explanation, and the lead repeated it to the
user as established fact — the same failure mode being audited in the workers:
relaying a plausible mechanism that nobody had executed. The behaviour claimed
(node disappearing busts the cache) is real for the PATH case; the mechanism
given was wrong and so was its scope.

### S5 attempt 7 — split verdict, and a premise the lead got wrong (2026-08-20)

`checker-tests` PASSED: it re-derived the cache mechanism from the toolchain's
own `cmd/go/internal/test/test.go` and, decisively, dumped the raw testlog with
`-test.testlogfile`, showing the tracked inputs are `getenv PATH`,
`open .../accounts.html` and `stat .../warnings_dom_harness.js`. Every mechanism
sentence in attempt 7 is true.

`checker-second` FAILED it on reachability. The comment and README called the
PATH-unchanged exception "one narrow case", illustrated with an nvm prune or a
Docker base image. It falsified that from the distro package itself
(`apt-get download nodejs`, `dpkg-deb --contents`): Debian and Ubuntu install
`node` into `/usr/bin`, which is on `PATH` before and after, so the most common
Linux install method never changes the `PATH` string at all. It reproduced the
stale cached green in exactly that configuration. Understating a hole is the
same defect class as overstating a guard.

**The lead's premise was wrong, and the user decided on it.** Decision
2026-08-20g was taken after the lead described the hole as requiring an unusual
sequence — "node uninstalled without any PATH change" — and used that rarity to
argue against paying `-count=1`. On the dominant Linux install path that
sequence is just uninstalling node. The lead also failed to spot the cheap
mitigation until `checker-second` forced the issue: re-running only
`internal/handlers/accounts` with `-count=1` costs ~0.5s, not the ~32s of a
full uncached suite.

**User decision 2026-08-20h, taken on corrected facts:** Makefile test targets
re-run that one package with `-count=1`, closing the hole for every `make` path;
the comment drops "narrow" and names the distro-package case; bare
`go test ./...` remains exposed and must say so.

## Tier justification

Per `TIERS.md`:

- **S2 — Tier 3, forced.** `internal/services/transfers/**` is in
  `.swarm/critical.globs`, so any manifest touching it escalates regardless of
  the rubric. Independently justified: pairing decides which rows are excluded
  from every spending figure, a wrong pairing is silent, and the change is an
  algorithm replacement rather than a patch.
- **S1, S3 — Tier 2.** Strong executable oracles and small blast radius, but S1
  moves money figures the dashboard and MCP both read, and S3 reports on
  persisted consent state; the rubric's tie-break rounds both up from 1.
- **S4 — Tier 1 at Phase 0, escalated to Tier 2 during the run.** Assigned Tier
  1 as advisory UI plumbing: strong oracle (a handler test plus an axe run),
  reversible, no money data written, and the warning text already computed and
  logged. That assignment was wrong in practice — the task failed its a11y audit
  twice and `escalate-scan` raised it mechanically on `two-consecutive-fails`,
  adding the adversarial lane. The rubric weighed "no money data written" and
  under-weighed that this was the first task in the run to ship new interactive
  markup, where the oracle is an audit rather than a test.

## critical.globs

No amendment proposed. `projection.go` stays outside the accounts glob for the
reason recorded in run R (user ruling 2026-08-19a): it is a pure calculator that
writes nothing. S2's glob hit is intentional and already reflected in its tier.

## Known ledger condition (pre-existing)

`.swarm/ledger.tsv` carries `R9` with status `no-change` (no defect found; root
cause was lead-side tree contamination). `gate.sh done` requires **every** row
to be `accepted`, so a repo-wide `done` will report `pending: R9` and exit
non-zero through no fault of run S. This is not fixed by writing a verdict R9
never earned. To be decided with the user at completion, not silently.

## Rulings

- **2026-08-20a (Phase 0, user).** S1 attribution rule: most-recent-leg wins.
- **2026-08-20b (Phase 0, user).** Tiers approved as drafted: S1=2, S2=3, S3=2,
  S4=1.
- **2026-08-20c (S4 dispute, judge panel).** S4 attempt 3 drew PASS from
  `checker-tests` and `checker-a11y` and FAIL from `checker-second`. Panel:
  `judge-standards` OVERRULE (no numbered point covered the finding — see the
  point 16 amendment it prompted), `judge-impact` OVERRULE, `judge-claude`
  UPHOLD. Majority OVERRULE, so S4 is accepted at tier 2, attempt 3.
  A SECOND premise error in the same verdict, missed by this document's first
  draft and found by the final-pass review of the lead's own work: `judge-impact`
  argued partly from "S4 is Tier 1 ... the explicit pass from the primary lane
  (a11y) carries weight", but S4 had already escalated to Tier 2 before that
  attempt. The lead noticed this at the time and said so in conversation, then
  failed to write it into this document — and left the task table and tier
  justification saying "Tier 1" as well, so the permanent record contradicted the
  ledger. Both are corrected above. The 2-1 outcome is unaffected:
  `judge-standards`' OVERRULE rests on the text of the standard alone.
  Recorded honestly: the dissent produced the strongest evidence — a real
  browser driving the running server, reading the accessibility tree over CDP
  — and it falsified the premise `judge-impact` leaned on (`checker-a11y`
  checked `data-warnings-text` after reload, never the region's own content).
  The panel's ruling stands; the defect it found is fixed as S5, not by the
  lead overriding the vote.
- **2026-08-20d (user).** Three decisions after the dispute: open S5 for the
  root cause plus the recorded test-quality gaps; amend `ACCESSIBILITY.md`
  with a new point 16 (AT parity on client-side suppression); leave the R9
  ledger row at `no-change` and document the `gate.sh done` consequence
  rather than fabricating a verdict R9 never earned.

## Run S — final state (2026-08-20)

All five tasks accepted through `gate.sh check`. 33 verdict files written across
the run, plus three judge rulings on the S4 dispute.

### `gate.sh done`

```
pending: R9 (status=no-change)
FAIL: run incomplete
exit=1
```

This is the documented Phase-0 exception, decided by the user (2026-08-20d):
`R9` is a row from run R, closed as `no-change` because no defect existed — its
root cause was lead-side tree contamination. `gate.sh done` requires every
ledger row to be `accepted`, and no verdict was fabricated to make a
previous run's honest non-finding look like an acceptance. Every row belonging
to run S is `accepted`; R9 is the only pending line, and it is not run S's to
close.

### Final pass

- `checker-a11y`, site-wide: **PASS**. No accessibility defect introduced by run
  S. The point-16 sweep found the unassigned-files banner compliant (its dismiss
  removes text and control atomically) and no other hide/dismiss pattern in the
  app matching the failure shape. Eight pre-existing defect classes in untouched
  pages are listed in `.swarm/verdicts/FINAL.1.checker-a11y.verdict` — missing
  `<h1>` on two pages, missing `<th scope>` on four, `<div onclick>` on the
  dashboard and explorer, no `prefers-reduced-motion` anywhere, and a
  point-16-shaped defect on the transfers confirm/reject path that predates this
  run. None are run S's, and none are fixed here.
- `checker-second`, reviewing the LEAD's own work: **FAIL**, two findings, both
  accepted rather than argued. The spec's S4 tier record contradicted the ledger
  (corrected above: Tier 1 → 2, escalation and judge panel now recorded), and
  `swarm/gate.sh` commit `d8c863b` weakens the two-consecutive-fails escalation
  more broadly than its message discloses. The latter is a pre-existing harness
  defect, demonstrated with a synthetic fixture, and is referred to the user as
  an escalation-policy decision rather than changed unilaterally at the end of a
  run.

### Operational incident

The site-wide a11y checker started a scratch server whose own
`killPreviousInstance()` logic terminated whatever process held `:8080` before
it moved to a non-default port. The identity of that process is unknown and it
was not restarted. Its own two scratch servers were left running and were
stopped by the lead. Mitigation for future runs: any checker permitted to start
the app must be given a non-default port in its brief.
