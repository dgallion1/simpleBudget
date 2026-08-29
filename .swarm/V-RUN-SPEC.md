# V run — two recorded defects from the A-run follow-up list

Commissioned 2026-08-29. Branch `fix/undo-alias-backup-symlink` off master
(post PR #63 merge, 010f1aa). Two tasks, independent files, one worker each.
Ledger prefix `V` (fresh — P/A/T/R/Q/F/S are taken by earlier runs).

Rulings that bind this run: 2026-08-16d (validate oracles at both ends),
2026-08-16e (count assertion before any loop-based probe), 2026-08-16f
(oracle asserts on an existing consumer's observable output).

---

## V1 — undo_resolve must reach a decision filed under a legacy pair key

Tier 3 (touches `internal/services/dataloader/**`, a critical glob).
Checks: `tests,second`. Oracle: `.swarm/tier3/V1/accept.sh`.

### Defect

`internal/services/mcpsvc/admin/undo.go:57` pre-checks
`decisions[key]` against the raw map returned by `LoadDuplicateDecisions()`.
But a decision recorded before StableID existed is filed on disk under the
pair's OLD key — `pairKey(hashA, hashB)` over content hashes — while every
UI surface and `list_duplicates` hand back the CURRENT key,
`pairKey(stableA, stableB)`. `ClearDuplicateDecision`
(`internal/services/dataloader/duplicate_decisions.go:134`) handles this by
also trying `legacyPairKeysFor(pairKey)`; the undo tool's pre-check does not,
so `undo_resolve` refuses with "nothing to undo" exactly where the app's own
Undo button would succeed.

### Required behavior

1. Given the pair's current key, `undo_resolve` finds and undoes a decision
   filed under that key OR any of its legacy aliases (same alias set and
   precedence as `ClearDuplicateDecision`: exact key first).
2. `previous_outcome` reports the outcome of the entry actually found,
   wherever it was filed.
3. The refusal ("has no decision recorded against it") happens only when
   neither the key nor any alias holds a decision. The tool must NOT regress
   to trusting `ClearDuplicateDecision`'s silent no-op.
4. The snapshot (`ensureDecisionsSnapshot`) and post-undo reload behavior are
   unchanged.

### Design constraint (recommended shape)

The aliasing knowledge lives in `dataloader` (`legacyPairKeysFor` is
unexported). Extend the store surface minimally:

- Add to `*dataloader.DataLoader` an exported lookup, e.g.
  `LookupDuplicateDecision(pairKey string) (DuplicateDecision, bool, error)`,
  which loads the decisions file and checks `pairKey` then its legacy
  aliases, in that order. It must take the same locking path as the other
  decision methods (`beginWrite` / the shared sequence — read the file
  through the transaction like `loadDuplicateDecisionsLocked`).
- Add the method to `admin.DecisionStore`
  (`internal/services/mcpsvc/admin/register.go:67`) and use it for the
  pre-check in `undo.go` in place of the `LoadDuplicateDecisions()` +
  map-index pair.
- Update every non-test implementer/fake of `DecisionStore` that fails to
  compile. Do not change `ClearDuplicateDecision` or `SaveDuplicateDecision`
  semantics.

A different shape is acceptable only if the observable behavior above is
identical and the interface change is no larger.

### Tests required (worker-authored, besides the oracle)

- dataloader: the new lookup finds (a) an exact-keyed entry, (b) a
  legacy-keyed entry via the alias index (reuse the fixture recipe of
  `TestApplyDuplicateDetection_LegacyPairKeyStillResolves`,
  `stable_id_test.go:362`), and (c) reports not-found without error when
  neither exists.
- admin: end-to-end MCP test — decisions file planted under the legacy key,
  `undo_resolve` called with the current key succeeds, reports the planted
  outcome, and the file afterwards holds neither key. Follow the existing
  `newLiveDeps`/`connect`/`decodeToolResult` harness.

### Acceptance

- `.swarm/tier3/V1/accept.sh` exits 0 (run from the tree root).
- `go build ./... && go vet ./... && go test ./...` green.
- Manifest: `.swarm/manifests/V1.<attempt>.files`.

---

## V2 — a symlink in the data directory must not abort a backup

Tier 2. Checks: `tests,second`.

### Defect

Three walks read every non-dir entry under `DataDir` and abort the whole
archive on the first read error. `filepath.Walk` uses Lstat, so a symlink is
always a non-dir entry; a symlink to a DIRECTORY therefore reaches the read
call and fails EISDIR, and a DANGLING symlink fails ENOENT — killing the
entire backup for an entry the storage contract says is not honoured anyway:

1. `internal/services/backup/snapshot.go:172` (`buildZip`, scheduled + MCP
   `run_backup` snapshots) — `os.ReadFile(path)` at line 197.
2. `internal/handlers/backup/handlers.go:269` (manual encrypted-bytes
   download) — `os.Open` mid-stream, so the failure also truncates a zip
   whose headers are already sent.
3. `internal/handlers/backup/handlers.go:380` (manual plaintext download) —
   same pattern.

### Required behavior

- A symlink whose target is a directory, and a symlink with no resolvable
  target, are SKIPPED: not archived, not descended into, no error, in all
  three walks identically ("manual and scheduled backups are byte-identical"
  is an existing documented invariant — keep it true).
- A symlink to a regular file keeps its CURRENT behavior in all three walks:
  content read through and archived under the link's relative name.
- A read failure on a real regular file still aborts, exactly as today.
- Skipped symlinks do not count toward file counts or byte totals.

### Design constraint

Put the decision in ONE exported place in `internal/services/backup` next to
`SkipPredicate` (its doc comment already calls that file the single source of
truth for walk exclusions) — e.g. a helper that classifies a walked non-dir
entry as archive/skip/fail — and use it from all three walk callbacks. Update
the now-wrong comment block at `snapshot.go:167`. Do not touch
`internal/services/restore` or `internal/services/storage` walks.

### Tests required

In `internal/services/backup` and `internal/handlers/backup` (follow each
package's existing test conventions):

- data dir containing a symlinked directory (with a file inside it) → backup
  succeeds; the zip contains the real files, nothing under the link name,
  and counts/totals exclude it.
- dangling symlink → backup succeeds.
- symlink to a regular file → archived under the link's relative name
  (regression guard for current behavior).
- at least the snapshot-path tests repeated for one handler download path,
  asserting the zip stream stays well-formed.

### Acceptance

- `go build ./... && go vet ./... && go test ./...` green, plus
  `go test -count=1 ./internal/services/backup/ ./internal/handlers/backup/`.
- Manifest: `.swarm/manifests/V2.<attempt>.files`.

---

## Rulings

(none yet)
