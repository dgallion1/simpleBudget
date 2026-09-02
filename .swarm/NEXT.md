# NEXT — state of the swarm and the open work

Rewritten 2026-08-25. A relaunched session starts cold; this file is the
handoff. It is written for a lead session running cheaper models: every task
below carries its own context, acceptance criteria, and the traps already hit
once. The previous NEXT.md (File Manager era, 2026-08-16/21) is in git
history; nothing in it is still actionable.

## Where things stand

Every ledger row is `accepted` except R9, which sits at `no-change`
permanently and by design (an honest non-finding; `gate.sh done` reports it
forever — do NOT "fix" it). Recent history:

- **F1–F4** (storage/encryption-migration fixes, merged 2026-08-24 under
  ruling 2026-08-24b with reduced verification) were retroactively verified.
  F4 accepted at attempt 2, F3 at attempt 5 — each needed one more test-only
  attempt; the full attempt-by-attempt record, including what each lane found,
  is in `.swarm/tier3/F3/report.md` and `.swarm/tier3/F4/report.md`.
  Merged as PR #45.
- **Q1** (`get_suspected_transfers` MCP tool) accepted at Tier 2, attempt 1.
  Landed as PR #46. The MCP surface is now 32 tools in six groups; the
  checked-in skill at `.claude/skills/budget2-mcp/` routes sessions across
  them and MUST be updated whenever the tool surface changes (counts live in
  SKILL.md, README.md, and `internal/services/mcpsvc/server_test.go`'s
  registration want-list — all three drift independently; Q1 fixed all
  three, keep them in lockstep).
- Rulings 2026-08-24a/b/c are recorded in the two tier3 reports above, not in
  a SPEC.md (this repo has none).

## Environment facts — read before running anything

1. **RESOLVED by T3 (2026-08-26): the full module now passes under root.**
   The inventory below is historical; the recipe remains useful only if a
   future test reintroduces a DAC-based fixture (don't — use the T3
   patterns).
   Original note: **Verification containers may run as root (uid 0).** Three pre-existing
   tests rely on `chmod`-denial fixtures that root bypasses
   (`CAP_DAC_OVERRIDE`), so they FAIL (or a mutation goes undetected) under
   root while being green at any normal uid:
   - `internal/services/storage`: `TestRollbackDecryptionWithRecipientReportsUnrestorableFiles`
   - `internal/services/mcpsvc/curate`: `TestDeleteAbortsWhenAnExistingFileCannotBeBackedUp`,
     `TestUpsertSkipsThePinWhenItsSnapshotCannotBeTaken`
   - `internal/handlers/explorer`: `TestHandleFileDelete_RemoveError`,
     `TestHandleFileUpload_WriteError`
     (found during T1, confirmed pre-existing via stash; same class), plus a
     root-only SKIP in `TestHandleImport_FailedWriteKeepsSource`
   Run full suites as an unprivileged user. Working recipe: copy the tree,
   `chmod -R a+rwX <copy>`, give `nobody` its own `HOME`/`GOCACHE`/`GOPATH`/
   `GOMODCACHE` under /tmp (the root-owned caches are unreadable to it), then
   `su nobody -s /bin/bash -c 'cd <copy> && …'`.
2. **Oracle race timeouts are calibrated, not sacred.** The storage package's
   `-race` suite needs ~1030s on a loaded 4-CPU container. All four oracles
   (`.swarm/tier3/F{1,2,3,4}/accept.sh`) now carry 1800s. If a new oracle
   adds a race check, budget 1800s and say why in a comment.
3. **Toolchain**: go.mod pins `toolchain go1.26.6`; any go command
   auto-downloads it on first use (a build can transiently fail mid-download —
   retry once before diagnosing).
4. **Root-proof failure injection** (the F3 lesson): to force a write failure
   that root cannot bypass, use a kernel limit, not permissions — e.g. a
   255-byte basename (NAME_MAX) is valid to create and read, but
   `atomicWrite`'s staging name (`base + ".tmp-" + random`) exceeds the limit
   and `os.CreateTemp` fails ENAMETOOLONG at any uid. See
   `TestRollbackDecryptionReportsPathOnAtomicWriteFailure`.
5. **Oracles plant test files in the package** (`zz_oracle_*_test.go`), so two
   oracle runs in one tree collide. Every checker copies the repo
   (`cp -a`, keep `.git`) and runs only in its copy; the sole write to the
   main tree is its verdict file.

## Process crib — the mechanics that get botched

- Ledger `.swarm/ledger.tsv`, TAB-separated:
  `task_id  tier  checks  status  attempt  worker  reason`.
- Verdict files: `.swarm/verdicts/<task>.<attempt>.<checker>.verdict`.
  Headers are one `KEY: value` PER LINE — `VERDICT:`, `CHECKER:`, `FAMILY:`,
  `TASK:`, `ATTEMPT:` — then `---`, then evidence. The gate refused a verdict
  whose fourth line read `TASK: Q1 ATTEMPT: 1`; the checker had to fix its
  own file (the lead NEVER edits or writes a checker's verdict).
- Lanes: primary verifier writes `FAMILY: anthropic`; `checker-second`
  writes `FAMILY: adversarial`. Two distinct families must PASS at the
  ledger's attempt number. Tier 3 additionally requires a `RESOLUTION:` line
  in `.swarm/tier3/<task>/report.md`.
- Gate (from the repo root; script lives in the agents2 repo):
  `bash <agents2>/swarm/gate.sh check <task>` — status may become `accepted`
  only after it exits 0, output pasted into the accepting message. Then
  `escalate-scan`. `done` must end reporting only R9.
- Workers write `.swarm/manifests/<task>.<attempt>.files` (repo-relative
  paths, one per line). Workers never commit; the lead commits after the gate.
- `.swarm/critical.globs` — a manifest touching these escalates the task one
  tier via `escalate-scan`. It covers `internal/services/storage/**`,
  `dataloader/**`, `retirement/engine/**`, `transfers/**`,
  `accounts/accounts.go`, `mcpsvc/confirm/**`. Assign Tier 3 up front for
  tasks that must touch them (T5/T6 below) instead of being surprised.
- Hard stop: two failed attempts at Tier 3 (or three anywhere) halts the task
  and reports to the user. F3 hit this; only an explicit user ruling reopens
  the attempt budget. Never loop silently.

## Open tasks

Priority order: T1 (best effort-to-value), T2 (largest user-visible debt),
T3 (matters because verification runs as root), then T4–T6 (want a design
decision each; small code).

### T1 — DONE (accepted 2026-08-25, attempt 1; landed as PR #48).
`TestHandleImport_WithRenderer_RendersImportResultBlock` covers the render
path; mutant-kill proven with asymmetry (verdict `.swarm/verdicts/T1.1.*`).

### T1 (original brief, retained for context) — handleImport's render path is untested. Tier 1, checks `tests`.
Every `handleImport` test runs with `renderer == nil` (JSON fallback), so no
Go test executes the template-render call in
`internal/handlers/filemanager/…` (find it: `grep -rn "Import finished"
internal/`). A typo in the template NAME would ship an empty body with a
green suite (the exact failure shape of ruling 2026-08-16a).
**Change**: one handler test using the existing `setupTestEnvWithRenderer`
helper, asserting the response body contains "Import finished".
**Accept**: the new test fails when the handler's template name is mutated on
a scratch copy; package tests green; gofmt/vet clean.

### T2 — DONE (accepted 2026-08-25, attempt 1). The recount found 11
patterns / 31 instances (audit: `.swarm/briefs/T2-audit.md`, supersedes both
historical counts); all eleven fixed and verified by live-trace re-audit plus
adversarial sweep (verdicts `.swarm/verdicts/T2.1.*`). Residue for a future
pass: `#age-encryption-error` (filemanager.html) lacks `role="alert"` — a
sixth error div outside the audit's five F6 locations; and two audit
observations worth their own decisions: the site's styling depends entirely
on `cdn.tailwindcss.com` at runtime (no vendored build — an offline user
gets an unstyled page, at odds with the README's local-only promise), and
several icon buttons rely on `title`-only accessible names.

### T2 (original brief, retained for context) — File Manager page accessibility. Tier 2, checks `a11y,second`.
Pre-existing WCAG violations, byte-identical since before the P-run: at least
unlabelled toggle checkboxes, an unnamed SVG delete button, low-contrast size
text. The two prior audits DISAGREE (20 vs 6 findings) and the discrepancy
was never reconciled — **step 1 is a fresh `checker-a11y` recount against
ACCESSIBILITY.md; trust neither old number**. Then one worker task per
finding cluster.
**Accept**: axe (or the repo's audit method) reports zero violations of the
recounted list on the File Manager page; no visual regression on the other
pages' shared components.

### T3 — DONE (accepted 2026-08-26, attempt 3, tier 3 under rulings
2026-08-25a/26a/26b). The real inventory was 56 tests (attempt 2's
adversarial lane found 49 beyond the first seven); all now use
kernel-enforced injections (ENOENT/EISDIR/ENOTEMPTY/ENOTDIR/ENAMETOOLONG,
plus a uid-conditional write-block: chmod for non-root, bind-mount+RDONLY
for root, t.Fatalf if mount setup fails). Attempt 2 taught the class's
sharpest lesson: an injection that breaks the LOAD makes save tests green
but defenseless — attempt 3 restored exact mutant-kill parity with the
chmod baseline (27/30 whatif, 5/8 retirement, survivors source-verified as
never reaching saveInternal). The FULL module is now green under root and
under ordinary uids; the Environment facts #1 recipe is no longer needed
for suite runs (kept for reference). Evidence: .swarm/verdicts/T3.{1,2,3}.*,
.swarm/tier3/T3/report.md.

### T3 (original brief, retained for context) — make the root-broken fixtures root-proof (four known). Tier 2, checks
`tests,second`. The tests in "Environment facts #1" fail under root.
Options per test: a root-proof injection (the ENAMETOOLONG pattern, or
rename-onto-directory), or an explicit documented `t.Skip` under uid 0 —
prefer injection; a skip recreates the F3 attempt-4 problem (an undefended
branch in root environments).
**Accept**: full module `go test ./...` green AS ROOT and as uid≠0; each
reworked test still kills its original mutation at both uids (state the
mutation per test in the brief; the storage one is in F3's report).
NOTE: two of the three live in `mcpsvc/curate` (not critical); the storage
one is under `internal/services/storage/**` → assign Tier 3 or expect an
escalation flag.

### T4 — DONE (accepted 2026-08-26, attempt 1, ruling 2026-08-26c): special
bits declared OUT of filePerm's contract, stated in the function's own
comment; both lanes verified the comment's factual claims against the code
(walks skip directories; only data files rewritten). Evidence:
.swarm/verdicts/T4.1.*, .swarm/tier3/T4/report.md.

### T4 (original brief, retained for context) — decide: does mode preservation include setgid/sticky bits? Decision
first, then Tier 1 code. `filePerm` (internal/services/storage/migration.go)
uses `Mode().Perm()`, which strips setuid/setgid/sticky — a data file with
02644 silently loses the bit on migration. Recorded by checker-second in
`.swarm/verdicts/F4.1.checker-second.verdict`. Either declare bits out of
contract (document at `filePerm` + F4 report) or preserve them
(`Mode() & (fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky)`) with a
test per bit. ASK THE USER which; do not choose silently.

### T5 — DONE (accepted 2026-08-26, attempt 1, ruling 2026-08-26d):
saveConfig stages via CreateTemp + StagingSuffix with error-path cleanup;
stays package-level and non-encrypting (it writes the encryption config
itself). Build finding worth knowing: there is NO orphan-deletion sweep in
this codebase — IsStagingName exists for backup-exclusion and
restore-prune protection only. Recorded observation (pre-existing, both
lanes): saveConfig does not fsync before rename. Evidence:
.swarm/verdicts/T5.1.*, .swarm/tier3/T5/report.md.

### T5 (original brief, retained for context) — auth.go's ad-hoc staging. Tier 3 (touches storage critical glob),
checks `tests,second`. `internal/services/storage/auth.go` `saveConfig`
stages with a fixed ad-hoc `.tmp` name outside the `atomicWrite` /
`StagingSuffix` regime the F2 work established (orphan cleanup does not know
about it; a crash can leave the temp file). Route it through `atomicWrite`
(mind T4's outcome for the mode argument).
**Accept**: no fixed-name staging remains (`grep -n '\.tmp"' internal/services/storage/`),
crash-window behavior covered by a test, package suite green at both uids.

### T6 — DONE (accepted 2026-08-26, attempt 4, rulings 2026-08-26e/f).
The user ruled: document "symlinks are not honoured", don't resolve them.
Docs-only, but it took four attempts because attempts 1-3 each made a
completeness claim ("every write replaces", "saves are stage+rename", "the
one exception is data/cache/") that the adversarial lane falsified with
another bare-write path — in order: SaveUserSettings (dead code),
HandlePlotly's live cache write, migration.go's live `.encryption-verify`/
`.encrypted` marker writes. The lesson, now in the tier3 report: in a
codebase with unswept write paths, docs must not claim completeness.
Attempt 4's README asserts non-support plus both proven failure modes as
possibilities; all four bypass/contract sites carry comments (atomicWrite,
SaveUserSettings, HandlePlotly, migration.go). Ruling 2026-08-26f carried
the task past the two-failed-attempts hard stop. Evidence:
.swarm/verdicts/T6.{1,2,3,4}.*, .swarm/tier3/T6/report.md.

### T6 (original brief, retained for context) — atomicWrite vs symlinked destinations. Decision first, then Tier 3
if changed. `atomicWrite` publishes by rename, so a symlinked destination is
replaced by a regular file (the data lands beside the link, not at its
target). Flagged during F4, deliberately not bundled. Either document "the
data directory does not honour symlinks" (README + a comment at atomicWrite)
or resolve symlinks before staging — the latter reopens the F2 atomicity
analysis, so it needs its own design note. ASK THE USER which.

## Follow-up run — ruling 2026-08-26g

After T1–T6 closed, the user authorized ("push it through, I mostly take
your advice") working the recorded observation backlog on the lead's
judgment, merge-on-green. Five rows added to the ledger:

- **T8** — fsync durability (tier 3, single-arm + dual lanes): none of the
  three stage-and-publish paths (`atomicWrite`, `createExclusive`,
  `saveConfig`) syncs the file before close or the directory after
  rename/link. Both T5 lanes recorded the gap as pre-existing. Design in
  `.swarm/tier3/T8/report.md`.
- **T9** — T2 accessibility residue (tier 1, a11y): `#age-encryption-error`
  lacks `role="alert"`; icon buttons with `title`-only accessible names.
- **T10** — remove dead `SaveUserSettings`/`LoadUserSettings` (tier 1,
  tests): zero live call sites; T6 documented them as outside the symlink
  contract — deletion removes the trap entirely.
- **T11** — backup/restore symlink asymmetry note (tier 1, tests, doc-only):
  backup's snapshot walk dereferences symlinked files while restore's prune
  unlinks them; document at the source per the T6 pattern (no completeness
  claims — that lesson cost three attempts).
Observation from T7 verification (recorded, not commissioned): the
coverage oracle `swarm/t7-coverage.sh` scans templates and web/static/js
but NOT Go source, so the literal Tailwind classes embedded in
`renderError()` fmt.Sprintf fragments (internal/handlers/{whatif,
majorexpenses,accounts}/handlers.go) and in internal/templates/render.go
are guarded only by the hand-maintained safelist. All resolve in the
built CSS today (adversarially verified with correct CSS escaping), but
a future edit to those fragments would not be caught by the script —
extend its extraction to internal/ if that becomes a recurring edit site.

New observation from the T11 build (recorded, not commissioned): a
symlinked DIRECTORY inside the data directory aborts the entire backup
snapshot — buildZip's walk sees it as a non-dir entry (Lstat), calls
os.ReadFile on it, gets EISDIR, and the walk errors out. A stray symlink
dir silently breaks backups until removed. Future bug candidate; the T11
comment documents only the true narrower claim (contents not walked).

- **T7** — vendor the Tailwind styling (tier 2, a11y+second): the site
  currently styles itself from `cdn.tailwindcss.com` at runtime; offline
  users get an unstyled page, at odds with the local-only promise. Executed
  last: largest and needs a toolchain decision (static build preferred;
  vendored JIT script the fallback).

Sequencing: T8 first (own branch/PR), then T9+T10+T11 batched on one
branch/PR, then T7 (own branch/PR). One worker in the main tree at a time;
each branch cut from the then-current master.

## Not tasks — do not "fix" these

- R9 `no-change` in the ledger: permanent, honest, correct.
- CHANGELOG's "Twenty-six tools" line: historical record of 2026-08-15.
- The `PLAINTEXT ON DISK` branch in migration.go has a comment explaining why
  it has no end-to-end test (not constructible in-process); the reasoning is
  also in F3's report. A vacuous test there would be worse than the gap.
- go-sdk v1.7.0 cannot send `notifications/elicitation/complete`; the browser
  approval flow survives without it. SDK limitation, tracked in CHANGELOG
  (2026-08-16 entry).
- F4's report cites regression grep counts ("10 invalidateCache / 14
  atomicWrite sites") that don't match a fresh grep; the underlying
  invariant was re-verified directly (see F4.1 verdicts). Stale prose, not a
  defect.

## V run — DONE 2026-08-29 (branch fix/undo-alias-backup-symlink)

All three tasks accepted, `gate.sh done` exits 0. Spec: `.swarm/V-RUN-SPEC.md`.

- **V1** (tier 3, attempt 1) — `undo_resolve` now pre-checks via
  `dataloader.LookupDuplicateDecision`, which applies the same legacy-alias
  set as `ClearDuplicateDecision`. Oracle `.swarm/tier3/V1/accept.sh`
  validated at both ends before dispatch (fails on master at the undo
  refusal; passed 4/4 on a discarded prototype). Dual-lane PASS.
- **V2** (tier 2, attempt 1) — symlinked-dir / dangling-symlink entries no
  longer abort any of the THREE backup walks (buildZip + both manual
  download handlers); decision centralized in exported
  `backupsvc.ArchiveEntry` next to `SkipPredicate`. Symlink-to-regular-file
  archiving behavior preserved. Dual-lane PASS; adversarial lane also
  proved ELOOP, double-indirection, unreadable-target, and race
  cleanliness.
- **V3** (tier 1, attempt 1) — test-only follow-up pinning the three
  behaviors the V1/V2 checkers verified with throwaway probes: lookup
  exact-key-over-alias precedence, legacy-alias kept_both undo, and
  plaintext-handler symlink skips. Checker re-applied all three mutations
  itself; every new test bites.

Escalate-scan note for future runs: V3's test-only edit under
`dataloader/**` did NOT trigger escalation — the exemption is PATH-based
(test-glob paths under a critical glob don't escalate; the gate never
inspects diff content — see agents2 TIERS.md, which is the accurate
statement; "escalates on the diff" is just 31e9954's commit title).

## W run — DONE 2026-08-29 (branch fix/phase-visibility)

All four tasks accepted; the ledger's W rows are complete (the X rows
belong to the concurrent agents2-26 session's run). Spec + rulings:
`.swarm/W-RUN-SPEC.md` (rulings 2026-08-29a through d).

- **W1** (tier 3, attempt 3) — Living Expenses phase sub-rows in the
  Monthly Budget Analysis; displayed-sum identity holds by construction
  (integer cents parsed from the same %.2f the template renders).
- **W2** (tier 3, attempt 4, user-reopened) — phase note under the slider;
  snap trap dead (hidden exact-value input is the only submission path;
  aria-valuetext + thumb parity); ONE whole-dollar rule (half-away,
  'en-US'-pinned) across Go and JS.
- **W3** (tier 1, attempt 2) — dashboard Target provenance title/aria.
- **W4** (tier 3, attempt 4, judges 3-0 overrule) — headroom decomposition
  + plain-English sentence in the budget banner; banner, card rows, tint,
  headline all consume one dead-banded BudgetVerdict classification.

Process lessons already encoded as rulings: 29a single-source thresholds,
29b rendered-strings sums, 29c scope-of-reopened-attempt governs, 29d a
FAIL can be factually wrong — verify the checker's premise before a fifth
attempt.

Recorded backlog (not tasks in this run): the Budget sparkline basis
mismatch (ruling 29d — chip raised); pre-existing a11y debt swept up by
the W checkers: text-gray-400/500 caption pair sitewide, master's banner
text on tinted bands, Net Savings coloring 3.4-4.3:1, unlabeled date-range
inputs, #steady-state-slider label, healthcare-person Source chip
contrast; W2 oracle helper zzCanonWhole mis-groups negatives (test-only).

## X run — DONE 2026-08-29 (backlog close-out; concurrent with the W run)

All eight tasks accepted; `gate.sh done` exits 0 across the full ledger.
Spec + rulings: `.swarm/X-RUN-SPEC.md` (incl. X-2026-08-29a shared-tree
acceptance and X-2026-08-29b dispute economics).

- **X1** (t2, att2, judges 3-0 overrule) — mcpsvc/snapshot: unreadable-source
  test is now real (EISDIR directory fixture, root-proof). The overruled FAIL
  was a wrong-citation comment inherited from issue #26's own text; comment
  rewritten by the lead per the judges' order. Issue #28 closed as obsolete
  (whatifmcp/live.go deleted in 0e6fb48).
- **X2** (t2, att2) — #27's revision test genuinely reaches the decode-time
  migration re-save (seed-guard asserts seeded-only values); #31: all 24
  mutators return prepare.Clone — "the cached object never escapes the
  manager" now unqualified, pinned by a 3-mutator table.
- **X3** (t2, att1) — #29: overrides.Apply uses prepare.Clone; the second
  re-attach in mcpsvc/plan stays (both guard tests bite-proofed).
- **X4** (t1, att2) — t7-coverage.sh scans Go string literals (identifier
  gate + content-shape fallback); traj-*/conversion-sweep-card whitelisted
  as hand-written hooks.
- **X5** (t3, att2) — aliases.json rekeyed to StableID (SaveAlias
  normalize-first; applyAliases resolves both identity forms). Attempt 1's
  removal-resurrect bug was found by the adversarial lane and is oracle-
  pinned (`.swarm/tier3/X5/accept.sh`, 8 behaviors).
- **X6** (t1, att1) — #25: both sides of the HX-Trigger contract pinned.
- **X7** (t2, att1) — #24: expected_scenario REQUIRED on POST /whatif/apply
  (400 before any write; 409 unchanged; sole caller unaffected).
- **X8** (t1, att3) — a11y close-out: h1s + full heading-hierarchy promotion
  on /whatif + /insights; text-green-600 sweep (67/68); gray caption sweep
  BOTH themes (attempts 2+3 fixed 118 dark-mode sites the attempt-1 sweep
  missed — the lesson: sweep completeness must be grep-the-token, not
  spot-check); labels for date inputs + steady-state slider; verdict-bar
  label/value contrast via the shared render.go map.

### Follow-ups recorded by X checkers (not tasks in this run)
- explorer.html suppressed-duplicate rows: opacity-50 on the <tr> caps small
  text at 3.63:1 (improved from 1.73) — full AA needs opacity moved off text
  nodes, or a documented de-emphasis exception. (X8.3 residual.)
- saveAndRenderConversionSweep emits HX-Trigger with no test assertion
  anywhere (X6 F2).
- successRateTextClass's one tinted-band use (whatif/verdict-bar.html) was
  outside X8's named scope (X8.1 note).
- ApplyOverrides still treats expectedScenario=="" as "no expectation" for
  IN-PROCESS callers; the requirement lives at the HTTP handler (X7 F3).
- mcpsvc/plan has no test of apply_changes' empty-Scenario fallback branch
  (dead-by-invariant, X7-second note).
- rate-assumptions.html:752 text-green-400 (no dark: pair) — wrong-shade
  defect outside the green-600 sweep (X8.1 note).
- Budget sparkline basis mismatch: chip task_2139e108 (W-run ruling 29d).
- low_balance flags credit-kind accounts permanently: chip task_3f52c4ef
  (agents2-fb session).
- pre-existing gofmt drift in retirement/analysis, engine, and two
  retirement test files (predates X; left untouched).

## Z run — DONE 2026-08-30 (post-close review findings)
Spec `.swarm/Z-RUN-SPEC.md`; all five tasks accepted, gate exit 0 each.
- **Z1** (t2, att1) — PV analysis now includes one-time expenses (engine's
  inflation rule, discounted at the charge month via the shared stream
  helper; BigTicketItems deliberately out of scope, see below).
- **Z2** (t2, att2) — sync apply bound to the preview: expected_scenario +
  plan_hash (single SHA-256 source), 400/409 contract, and — after a
  conceded attempt-1 TOCTOU (ruling Z-2026-08-30b) —
  SaveWithRevisionIfScenario checks scenario identity INSIDE the held
  lock (ApplyOverrides pattern). computeDashboardSync made deterministic
  (pattern sort + local-midnight window pin) en route.
- **Z3** (t1, att1) — Health Insurance sync exclusion now EqualFold,
  matching FilterByCategory's case-insensitivity.
- **Z4** (t1, att1) — sweep treats a disabled retained Roth amount as
  current=0; $0 row is Current; retained amount not force-enabled.
- **Z5** (t2 escalated critical-glob, att2) — trajectory phase labels
  follow chain transitions: ProjectionYearSummary.PhaseName recorded from
  ACTIVE settings at the multiplier site; after conceded attempt-1 ""
  overload (ruling Z-2026-08-30a), engine records "-" for
  phases-disabled years; "" reserved for pre-field projections.

### Follow-ups recorded by Z checkers (not tasks in this run)
- BigTicketItems are ALSO absent from PV (lead decision D-Z-a): balance
  events with tax treatment + an income type — a modeling question for
  the user, not a mechanical fix.
- SaveWithRevisionIfScenario guards scenario identity but not revision —
  same-scenario lost-update window is the pre-existing saveAndRecalc
  contract shared by 7 handlers (Z2.2-second).
- Atomicity of the locked check rests on source reading: no deterministic
  test can preempt inside a held mutex without another seam (Z2.2-tests
  disclosed limit).
- syncSettingsFromDashboard (sync.go:295) now has no non-test caller.
- Z2 happy-path test asserts income source, not MonthlyLivingExpenses;
  the IncomePatterns sort.Slice is load-bearing but no committed test
  kills its removal (V3 candidates). Preview list order changed to
  alphabetical (was largest-total-first).
- PV positive-side horizon boundary test (Year==ProjectionYears-1).
- EqualFold vs ToLower can disagree on exotic runes (Kelvin sign) —
  agree on all ASCII spellings (Z3 note).
- Sentinel "-" collisions: enabled-but-unnamed phase unreachable via
  handlers; dual "-" literals (engine noPhaseSentinel vs
  trajectoryPhaseName) agree today, could drift; phase_name:"-" now
  serializes where the key was absent (no golden breakage found);
  F-Z5-4: promote the composed engine+handler probe to a committed test.
- **Z6** (t1, att1, final-pass catch) — the guard's 400/409s were never
  rendered (renderError sets no HX-Retarget; htmx 2.0.4 drops 4xx swaps).
  All five paths (four handler-local + the locked-save conflict, intercepted
  in sync-only saveAndRecalcIfScenario via errors.As) now use
  renderRetargetedError → #whatif-sync-preview, header-asserted in tests.
  Z6 follow-up observations: the whatif renderError partial has no
  role="alert"/aria-live package-wide (accounts/filemanager do it right);
  handlers.go pre-existing trailing-blank-line gofmt nit (line ~1199).

## DB trigger conditions (decision note, 2026-08-30 — no action planned)
Stay on JSON/CSV files + SaveWithRevision optimistic locking. Revisit
(SQLite, single file, WAL — never a server DB) only if one of these fires:
1. A second WRITER PROCESS appears (MCP moved back out-of-process, cron,
   any automation writing data) — the in-process mutex stops covering.
2. Multi-object atomicity pain recurs (settings + aliases + decisions
   cannot commit as one unit; the StableID/orphaned-decisions drift class).
3. A corruption or partial-write incident (WAL makes crash-mid-write a
   non-event instead of a restore).
Rationale: the 2026-08-30 lost-update bug (Z7) was a contract gap, not a
storage failure — `UPDATE ... WHERE revision=?` requires the same
discipline; migration would ripple through backup/restore, MCP snapshots,
aliases, dataloader (storage/** and dataloader/** are critical.globs).
Swap seam if it ever happens: internal/services/storage.
Related parked item: revision-guard sweep across the ~7 other
saveAndRecalc callers (same lost-update window as Z7, sync-only fixed
per user decision 2026-08-30).
- **Z7** (t2, att1, user-review P1 on PR #71) — same-scenario lost update
  closed: LoadContextWithRevision (atomic settings+revision under one
  lock), expected_revision round-trip, SaveWithRevisionIfScenario compares
  scenario AND revision inside the held write lock. Revision is
  GLOBAL-per-manager (different-scenario save → conservative 409,
  documented). Z7 checker findings for the backlog: ApplyOverrides has a
  scenario guard but NO revision guard (handlers_live.go + mcpsvc/plan
  callers — same lost-update class, part of the parked saveAndRecalc
  sweep); loadInternalContext's migration-on-decode rewrite doesn't bump
  (pre-existing, harmless under its held lock); no test pins the
  global-revision false-positive 409; expectedScenario=="" skips both
  guards (unreachable today).

## HC run (2026-08-30) — pre-existing a11y findings (master-native, out of HC1 scope)
- base.html nav: select[name="comparison"] has no accessible name (axe select-name, critical).
- base.html nav: Filemanager/Accounts/Transfers links ~1.1:1 in light mode (white on bg-white/20 over gradient); remaining nav links axe-incomplete on gradient — manual check.
- kpis.html Monthly Living card: text-rose-600 "Target ... over" line 4.49:1 light — hairline near-fail.
- kpis.html Monthly Healthcare card: text-emerald-600 "Target ... under" line 3.43:1 light.
- kpis.html Budget card breakdown: text-rose-500 / text-emerald-500 lines 3.34:1 / 2.31:1 light.
- budget-vs-actual chart: plotly dashed target-line color contrast never assessed in either theme (JS-rendered).

## SY run (2026-08-30) — backlog from checker findings (see SY-RUN-SPEC.md rulings)
- CombinedCumulativeBalance walk assumes per-month |sum| partitions the
  range-level |sum| — FALSE for any refund-dominant month (one large
  outflow-typed credit suffices, given inferred typing). MASTER-NATIVE
  (proven identical with planExclusions=nil, both lanes' probes, SY4
  attempts 3-4); SY4 only makes the chart walk AGREE with metrics.go, it
  does not fix the shared assumption. Unrecorded in code — needs an issue
  or documented skip so it is not rediscovered a fourth time.
- The chart-vs-metrics walk equality test guards the SPEND term only
  (tiny-target calibration suppresses the pre-existing flat-monthTarget
  vs day-prorated accrual difference, which remains untested).
- Negative-net excluded groups render "$-163/mo" not "-$163/mo"
  (formatNumber as-is per D-SY-e); pick a convention before more
  surfaces render PlanExcludedTotal (currently annotation-only, not yet
  rendered anywhere).
- SY2 flagged-side checked-state assertion is unanchored (bare
  substring); windowing it like its unflagged sibling is a one-liner
  (checker-tests SY2.1 F1).
- SY3 doc pass leftovers: list_major_expenses now returns
  exclude_from_plan_sync on every row (undocumented); skill doc should
  note keeping Lucid on is_internal_transfer until the SY4 code is
  DEPLOYED and the data migration below is done.
- gitignore the stray repo-root binaries (untracked 16MB budget2.old-1345
  nearly rode into a blind `git add -A` twice this run).
- DATA MIGRATION after SY4 deploys (SY-RUN-SPEC.md SY4 criterion 6):
  flip Lucid b388e1c8 is_internal_transfer→false,
  exclude_from_plan_sync→true, re-sync, verify living ≈7,128.66 and
  Lucid visible in spending totals again. Do NOT flip before the new
  binary is running — old code ignores the new flag entirely.

## ND run backlog (2026-08-31)
- Greedy per-index pairing is order-dependent: an adversarially-ordered
  3-way same-cents collision lets a Scheduled decoy steal a Posted row from
  its true Pending partner (checker-second probe, ND1). Pre-existing,
  inert on real data; revisit only if a real dataset hits it.
- Gap-B token affinity can pair two DISTINCT same-amount payments sharing
  two generic merchant tokens ("Discover Card Payment" scheduled vs
  "Discover Card Minimum Payment" posted). v1 constants are hardcoded by
  spec §9 pending real false-positive data; kept_both exists for these.
- RESOLVED (ND3, 2026-09-01): Amazon pending→posted at 4 days (2025-11
  $30.81) — pendingPostedWindowDays widened 3→5, resurrecting the one
  piece of the abandoned DP3 commit (9c5b120) not superseded by ad63180.
  Live-data probe (.swarm/work/ND3): the widened window surfaces exactly
  this pair, zero junk pairs, all 24 loader-bound decisions still bind
  identically (23 kept_winner + 1 kept_both — the ND-era 16+1 plus the
  seven ND pairs since resolved); 6 of the 30 recorded entries were
  already unbound at the old window (pre-existing, likely orphaned by
  the accounts-file StableID reassignment) and are unchanged.
