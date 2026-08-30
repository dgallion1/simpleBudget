# X run — backlog close-out: issues #24–#29, #31 + recorded follow-ups

Commissioned 2026-08-29 by agents2-26. Ledger prefix `X` (`W` is taken by the
concurrent phase-visibility run; per-run-prefix lesson from
tier3-oracle-methodology). Working tree is SHARED with that run mid-flight,
so this run is split by file territory:

- **Batch A (now, disjoint from the W run's files):** X1 whatifmcp, X2
  retirement settings, X3 overrides, X4 t7-coverage.sh, X5 dataloader
  aliases (tier 3, oracle first).
- **Batch B (after the W run commits):** X6 = #25 HX-Trigger tests, X7 = #24
  require expected_scenario, X8 = a11y h1s + text-green-600 sweep — all
  inside `internal/handlers/whatif/**` / `web/templates/**`, which the W run
  is actively editing.

## Rulings

- **X-2026-08-29a (shared-tree acceptance):** while the W run's uncommitted
  edits sit in the same tree, `go build ./...` / `go test ./...` cannot be
  required of X workers (another run's transient state would fail them).
  Workers and checkers verify at PACKAGE scope; the lead runs the full suite
  once at integration/commit time and the gate's acceptance stands on the
  package evidence + that integration run. Checkers still copy the repo and
  work in the copy.
- Workers run NO git commands at all this run (shared tree, two runs
  mid-flight). Leads on both sides commit only their own paths.

## Lead decisions

- **D-2026-08-29a (#24):** require `expected_scenario` on `POST /whatif/apply`
  (issue option 1). One caller exists and always sends it.
- **D-2026-08-29b (#28 runtime):** keep `/tmp` + `os.CreateTemp`, but (1)
  remove the log file when `cmd.Start()` fails, and (2) best-effort prune
  `budget2-server-*.log` older than 7 days at spawn (errors ignored, never
  block the spawn).
- **D-2026-08-29c (#31):** mutators return `prepare.Clone(settings)` (issue
  option 1) — the natural completion of #23/#30.
- **D-2026-08-29d (green sweep, batch B):** `text-green-600` on light
  backgrounds → `text-green-700` (transfers-page precedent, 5.02:1).
  Decorative `aria-hidden` icons exempt; emerald tokens out of scope (A-run
  finding).
- **D-2026-08-29e (#26 fixtures):** verification may run as root; read-denial
  fixtures must be root-proof or skip at uid 0 with a comment (T3 patterns;
  F3 lesson: kernel limits over permissions; EISDIR-via-directory is an
  acceptable root-proof read failure).

---

## X1 — issues #26 + #28: whatifmcp fixture honesty + log retention
Tier 2, checks tests,second. Territory: `internal/services/whatifmcp/**` ONLY.

- #26: `TestSnapshotter_FailsWhenSourceUnreadable` (snapshot_test.go) writes
  no file, so it tests the MISSING case and asserts only that some error came
  back. The reason `Snapshotter.Ensure` reads bytes instead of `os.Link` is
  the UNREADABLE case (docs/whatif-mcp-followups-2026-08-09.md §3: blind copy
  of ciphertext would appear to succeed and the abort-before-write guarantee
  would silently not fire). Add the genuinely-unreadable subtest per
  D-2026-08-29e; keep/rename the missing-file case to what it actually checks.
- #28: `Client.EnsureServer` (live.go) creates `/tmp/budget2-server-*.log`
  per spawn, never cleaned; the file is created even when `cmd.Start()`
  fails; three tests in the package write into real `/tmp`. Required per
  D-2026-08-29b: delete the file on failed Start; best-effort prune of
  `budget2-server-*.log` older than 7 days at spawn; the three tests move to
  `t.TempDir()`. Tests: failed-start cleanup; prune removes an old file and
  leaves a young one (age via os.Chtimes).

Acceptance (package scope per X-2026-08-29a): `go vet ./internal/services/whatifmcp/...`,
`go test -count=1 ./internal/services/whatifmcp/...` green; no stray files in
/tmp after the package suite (`ls /tmp/budget2-server-*` before/after).

## X2 — issues #27 + #31: retirement settings revision test + mutator clones
Tier 2, checks tests,second. Territory: `internal/services/retirement/settings.go`
+ `settings_test.go` (+ sibling test files in that package if needed) ONLY.
Does NOT touch `internal/services/retirement/analysis/**` (W run territory).

- #27: `TestRevision_DoesNotBumpOnCacheMissLoad` runs against an empty
  TempDir, so `loadInternalContext` exits at `os.IsNotExist` and never
  reaches `decodeSettings`/the `changed`-branch `saveInternal`. Seed a
  scenario file whose decode genuinely reports `changed` (a shape needing
  decode-time migration), assert the revision is unmoved across the load,
  and prove the test now FAILS if that path bumps (temporary mutation,
  then restore).
- #31 per D-2026-08-29c: every mutator that returns the object
  `saveInternal` published into `sm.cache` (~20 methods — `AddIncomeSource`,
  `RemoveIncomeSource`, `UpdateSettings`, `ApplyOverrides`,
  `UpdateSpendingPhases`, …) returns `prepare.Clone(settings)` instead.
  Update the #30-era doc claims ("never escapes through Load") to the
  unqualified invariant. Add a test that mutates a mutator's return value
  and proves the cache is unaffected (must fail under the old aliasing).

Acceptance: `go vet ./internal/services/retirement/...`,
`go test -count=1 ./internal/services/retirement/...` green (the analysis
subpackage is another run's territory — if IT fails to compile/test, STOP
and report rather than touching it).

## X3 — issue #29: overrides.Apply uses prepare.Clone
Tier 2, checks tests,second. Territory: `internal/services/retirement/overrides/**` ONLY.

`overrides.Apply` predates `prepare.Clone` and hand-rolls DeepCopy + a
`PerYearOverrides` re-attach with a long explanatory comment. Replace with
`prepare.Clone`, deleting the hand-written carry. CAUTION (from the issue):
`RunWithOverrides` performs a SECOND re-attach after `prepare.From` (which
DeepCopies internally and drops the map past that boundary) — that one must
STAY. `TestApply_PreservesPerYearOverridesAcrossDeepCopy` and
`TestPreparedWithOverrides_PreservesPerYearOverridesAcrossBoundary` must
remain meaningful — prove each still fails if its respective carry is broken
(temporary mutation, then restore).

Acceptance: `go vet ./internal/services/retirement/overrides/...`,
`go test -count=1 ./internal/services/retirement/overrides/...` green.

## X4 — t7-coverage.sh must scan Go source
Tier 1, checks tests. Territory: `swarm/t7-coverage.sh` ONLY.

The script scans `web/templates/**/*.html` and `web/static/js/**` for
Tailwind-utility tokens; a class emitted from a Go string literal escapes
the proof. Extend the scan to string literals in `internal/**/*.go` and
`cmd/**/*.go`, reusing the existing token matcher and whitelist mechanism
(expect Go false-positive pressure — extend the whitelist, do not weaken the
matcher). Do NOT modify tailwind.config.js or tailwind.css. Validate against
a frozen `cp -a` copy of the repo (the live tree is mid-edit by another run):
show the copy passes (or list real uncovered tokens as REPORTED findings),
and show a planted fake utility class in a Go literal in the copy IS caught.

## X5 — aliases.json rekeyed to StableID
Tier 3, checks tests,second. Territory: `internal/services/dataloader/loader.go`
(alias functions) + dataloader tests. Critical glob — oracle at
`.swarm/tier3/X5/accept.sh`, lead-authored, validated both ends pre-dispatch.

Defect: `aliases.json` is `map[Transaction.Hash]displayName`; `SaveAlias`
stores the caller's key raw; `applyAliases` matches `Hash` only. A
description reformat changes the hash and orphans the alias — the exact
failure StableID (A1) exists to prevent, already fixed for pins, enrichment,
and duplicate decisions.
Required, mirroring the established sidecar pattern (`amazon_enrichment.go`,
`transaction_pins.go`):
- `SaveAlias` canonicalizes the incoming key (`canonicalKey`) and rekeys
  resolvable legacy entries on write (`rekeyToStable` semantics: unresolvable
  legacy keys preserved, never dropped; StableID entry wins collisions).
- `applyAliases` resolves BOTH identity forms (StableID first, legacy hash
  fallback) so pre-migration files keep working without a write.
- Alias removal (empty name) reaches an entry filed under either form.

## X6 — issue #25: pin the HX-Trigger omission [batch B]
Tier 1, checks tests. Territory: `internal/handlers/whatif/*_test.go`.
(a) item add/remove response carries NO `HX-Trigger`; (b) a settings/slider
edit carries one matching the revision that write produced; (c) fix the
now-false comment on `TestRenderRecalc_CarriesRevisionHeader`. Must fail if a
`revisionUnreported` route starts emitting the header.

## X7 — issue #24: require expected_scenario [batch B]
Tier 2, checks tests,second. Territory: `internal/handlers/whatif/handlers_live.go`
+ tests. Per D-2026-08-29a: missing/empty `expected_scenario` on
`POST /whatif/apply` → 400 naming the field, before any write; 409-on-mismatch
unchanged. Read and cite the one live caller (`apply_changes` in mcpsvc);
fix it too ONLY if it can actually send empty. Tests: missing → 400 + no
write; mismatch → 409; match → applies.

## X8 — a11y: h1s + text-green-600 sweep [batch B]
Tier 1, checks a11y. Territory: `web/templates/**`, vendored tailwind.css
rebuild, Go files emitting the class. Add `<h1>` to /whatif and /insights
consistent with sibling pages; sweep `text-green-600` → `text-green-700` per
D-2026-08-29d (exemptions listed and justified); pinned CSS rebuild;
css-verify green.

## Amendments (2026-08-29, batch A)

- **X1 rescoped.** Issues #26/#28 predate the MCP refactor: `internal/
  services/whatifmcp/` was deleted in 0e6fb48 (MCP now served in-process
  from cmd/server). The snapshotter and its misnamed test live at
  `internal/services/mcpsvc/snapshot/`. X1 attempt 1 ended with ZERO edits
  (worker correctly blocked on the stale path). X1 attempt 2 = Part 1 (#26)
  only, territory `internal/services/mcpsvc/snapshot/**`. **Issue #28 was
  CLOSED as obsolete** (EnsureServer/live.go/budget2-server logs no longer
  exist; evidence in the closing comment).
- **X5 oracle validated both ends** (before dispatch, per 2026-08-16d):
  on master the two compat guards pass and the three defect behaviors fail
  (StableID-keyed apply, save-rekey, removal-via-either-form); with a
  discarded prototype (canonicalKey + index rekey on save, StableID-first
  match in applyAliases) all five pass → ORACLE PASS.
- **t7-coverage finding to adjudicate on clean master:** 4 uncovered
  tokens — `conversion-sweep-card`, `traj-nominal`, `traj-real`,
  `traj-view-real` (arrived with PR #63 / tax-optimizer; likely
  hand-written CSS/JS hooks → whitelist candidates, not Tailwind).
- **Shared-tree coordination:** W run (phase-visibility) owner is
  agents2-86; X batch A ran in disjoint territories while their attempt-3
  worker was out, their full-suite run on the shared tree was green WITH
  X2/X3/X4 edits, and X checkers/batch B/X5 dispatch are frozen until
  their commit ping. agents2-fb curated live data/ today (account-scoped
  CSV names, new accounts.json, 6 duplicate pairs re-resolved); tarballs
  in ~/budget2-backups/.

- **X-2026-08-29b (dispute economics):** when the two lanes split and the
  LEAD sides with the FAIL after verifying its factual premise (ruling
  2026-08-29d discipline), the judge panel is not convened: sending the work
  back is UPHOLD-equivalent, and the panel exists to contest ACCEPTANCE, not
  rejection. The gate cannot accept with a FAIL on file at the current
  attempt anyway. Panels remain mandatory whenever acceptance over a
  standing FAIL is sought. Applied to X2 attempt 1 (vacuous seed-guard
  assertion; primary lane's premise verified from DefaultWhatIfSettings).
