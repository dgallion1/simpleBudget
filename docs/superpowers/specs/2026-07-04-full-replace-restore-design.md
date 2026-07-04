# Full-Replace Backup Restore — Design

**Date:** 2026-07-04
**Status:** Approved

## Problem

Restoring a backup does not return the system to the backed-up state. The
current `restoreFromZip` (`internal/handlers/backup/handlers.go`) extracts
archive entries into the data directory, overwriting matching files, but never
deletes files that exist on disk yet are absent from the archive. A CSV or
JSON file created after the snapshot survives the "restore", so the system
ends up in a merged state that never existed.

Backup *creation* is not the problem: both the manual download
(`HandleBackup`) and the automatic snapshot (`services/backup/snapshot.go`)
already capture everything under the data directory — CSVs, all JSON state,
`settings/`, `uploads/` — excluding only `cache/`, `*.tmp`, and the
encryption markers `.encrypted` / `.encryption-verify`.

## Decision

Restore becomes **full replace**: after a restore, the data directory
contains exactly what the archive contains (modulo the deliberate skip list).
This applies to both restore endpoints:

- `HandleRestore` (user-uploaded zip)
- `HandleRestoreTestData` (embedded test dataset)

`HandleDeleteAllData` is **out of scope** and keeps its CSV-only behavior.

A **safety snapshot is mandatory**: restore takes an automatic snapshot via
the backup service before modifying anything, and a snapshot failure blocks
the restore entirely (user decision, 2026-07-04).

## Approach: extract-then-prune

Chosen over wipe-then-extract (empty-directory window, same end state) and
stage-and-swap (breaks on encrypted stores where writes must go through
`store.WriteFile`, nested backup dirs, cross-device renames).

All changes live in `restoreFromZip` so both endpoints inherit the behavior.

### New flow

1. **Validate** the entire archive in memory (existing logic, unchanged):
   path sanitization (no absolute paths, no `..`, must stay under the data
   dir), reject encrypted blobs when the destination store is not
   encrypted+unlocked, build the full write queue. Any bad entry rejects the
   whole restore before anything on disk is touched.
2. **Safety snapshot** — call `backupSvc.Snapshot(ctx)` with the request
   context. This runs unconditionally, NOT through the `Enabled()` gate:
   it is a restore-safety net, not a scheduled backup, so it must fire even
   when the user has disabled auto-backup. On failure the restore aborts
   with no data modified:
   - `ErrSnapshotInProgress` → HTTP 409, "a backup is currently running;
     retry shortly"
   - any other error → HTTP 500 with the snapshot error
   - `backupSvc == nil` (not initialized) → HTTP 500; restore never
     proceeds without a recoverable fallback.
3. **Write** every queued entry via `store.WriteFile` (existing logic,
   fail-fast; the safety snapshot is the recovery path).
4. **Prune extras** — walk the data directory and delete, via
   `store.Remove`, every file that is not an archive entry. Skip list:
   - the backup directory, if nested under the data dir (same
     absolute-path-prefix guard as `HandleDeleteAllData` /
     `skipPredicate`)
   - the `cache/` directory (regenerable, never in archives)
   - `.encrypted` and `.encryption-verify` (define the store's encryption
     state; never in archives)
   - `*.tmp` files (possible in-flight atomic writes)

   Archive-entry membership is compared on cleaned absolute destination
   paths (the same `dest` computed during validation).

   After file pruning, remove directories left empty — never the data
   root, `cache/`, or the backup dir. Individual prune failures are logged
   and counted but do not abort: the restored data is already in place.
5. **Respond** `200` with `Restored N files, removed M stale files`
   (test-data endpoint keeps its "test files" wording, adding the removed
   count).

### Signature changes

`restoreFromZip(content []byte)` gains a `ctx context.Context` parameter
(for `Snapshot`); both handlers pass `r.Context()`. Return shape grows a
removed-count (e.g. `(restored, removed int, status int, msg string)`).

## Error handling summary

| Step | Failure behavior |
|------|------------------|
| Archive validation | All-or-nothing; nothing touched (existing) |
| Safety snapshot | **Blocks restore**; nothing touched; 409/500 |
| Writing entries | Fail-fast mid-way (existing); recovery = safety snapshot |
| Pruning | Best-effort per file; log + count failures; still 200 |

## Testing

Table-driven tests alongside the existing handler tests
(`internal/handlers/backup`):

- Restore removes a data-dir file absent from the archive.
- Restore preserves: `cache/` contents, `.encrypted` / `.encryption-verify`,
  `*.tmp`, and a backup dir nested under the data dir.
- A safety snapshot zip exists in the backup dir after a successful restore,
  and is taken even when auto-backup is disabled.
- Snapshot failure (e.g. unwritable backup dir) → restore aborts, data dir
  unchanged, non-200 status.
- Concurrent snapshot in flight → 409.
- Empty directories left after pruning are removed; data root, cache, and
  backup dir are never removed.
- `HandleRestoreTestData` also prunes.
- Existing merge-era restore tests updated to the new semantics and return
  shape.
