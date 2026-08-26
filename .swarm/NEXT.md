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

### T5 — auth.go's ad-hoc staging. Tier 3 (touches storage critical glob),
checks `tests,second`. `internal/services/storage/auth.go` `saveConfig`
stages with a fixed ad-hoc `.tmp` name outside the `atomicWrite` /
`StagingSuffix` regime the F2 work established (orphan cleanup does not know
about it; a crash can leave the temp file). Route it through `atomicWrite`
(mind T4's outcome for the mode argument).
**Accept**: no fixed-name staging remains (`grep -n '\.tmp"' internal/services/storage/`),
crash-window behavior covered by a test, package suite green at both uids.

### T6 — atomicWrite vs symlinked destinations. Decision first, then Tier 3
if changed. `atomicWrite` publishes by rename, so a symlinked destination is
replaced by a regular file (the data lands beside the link, not at its
target). Flagged during F4, deliberately not bundled. Either document "the
data directory does not honour symlinks" (README + a comment at atomicWrite)
or resolve symlinks before staging — the latter reopens the F2 atomicity
analysis, so it needs its own design note. ASK THE USER which.

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
