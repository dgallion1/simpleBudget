# Full-Replace Backup Restore — Design

**Date:** 2026-07-04
**Status:** Approved (amended 2026-07-04 post-review, see "Post-review amendments")

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
   `store.Remove`, every file that is not an archive entry. Skip list
   (the shared predicate `backupsvc.SkipPredicate`, single source of
   truth for backup creation, restore writes, and pruning):
   - the backup directory, if nested under the data dir (same
     absolute-path-prefix guard as `HandleDeleteAllData`)
   - the `cache/` directory (regenerable, never in archives)
   - `.encrypted`, `.encryption-verify`, and `.encryption-config.json`
     (`storage.IsEncryptionStateFile`; these define the store's
     encryption state and are never archived)
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

## Post-review amendments (2026-07-04)

A code review of the initial implementation surfaced one critical defect in
this spec and several hardening gaps. The following are now part of the
design:

1. **`.encryption-config.json` joins the skip list.** The original skip
   list protected only the two encryption markers; the storage layer also
   keeps `.encryption-config.json` (auth method + key paths for
   age/SSH/YubiKey) in the data dir. Without it, restoring any archive
   lacking the file (e.g. the embedded test dataset) deleted the store's
   auth configuration and locked the user out after restart.
2. **One shared skip predicate.** The exclusion rules (nested backup dir,
   `cache/`, `*.tmp`, encryption-state files per
   `storage.IsEncryptionStateFile`) live in `backupsvc.SkipPredicate` and
   are consumed by snapshot creation (`buildZip`), the manual backup
   download (`HandleBackup`), the plaintext export
   (`HandleBackupPlaintext`), restore-entry validation, and pruning. The
   review found three hand-rolled copies had already drifted. Consequences:
   backup archives no longer contain `.encryption-config.json` (it is local
   store state, useless — and potentially harmful — on another store), and
   the manual download now skips a backup dir nested under the data dir
   (previously it archived old backups into new ones).
3. **Restore ignores skip-listed archive entries.** Entries named
   `.encrypted` / `.encryption-verify` / `.encryption-config.json`, `*.tmp`
   entries, or entries under the backup dir are not written. A foreign zip
   can no longer flip the store's encryption state; older backups that
   contain the config file restore cleanly without overwriting the live
   config.
4. **The snapshot lock is held for the whole restore.**
   `backupsvc.SnapshotAndHold` takes the safety snapshot and keeps the
   snapshot mutex until the restore finishes, so a scheduled snapshot or a
   concurrent restore cannot observe a half-restored data dir (they get
   `ErrSnapshotInProgress` → 409, as before).
5. **Restored count is deduplicated.** `Restored N files` counts distinct
   destination paths (`len(archiveEntries)`), not raw zip entries, so a zip
   with duplicate names reports the number of files actually on disk.
