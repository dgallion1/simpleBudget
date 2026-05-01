# Automatic Round-Trip-Complete Backups

**Date:** 2026-05-01
**Branch:** data-storage-improvements
**Status:** Design approved

## Problem

Two real failure modes in the current backup story:

1. **Restore is incomplete (round-trip bug).** `HandleRestore` in
   `internal/handlers/backup/handlers.go:158` only extracts files whose
   name ends in `.csv`. Everything else in `data/` is silently skipped
   on restore — including `major_expenses.json`, `transaction_pins.json`,
   the entire `settings/` subdirectory, and any future JSON state. A
   user who relies on the existing "Backup → Restore" loop loses real
   application state without warning.

2. **Backups are manual-only.** `HandleBackup` produces a one-shot
   browser download. There is no automatic snapshot, no scheduled
   capture, no on-disk retained history. The presence of multiple
   `bk_download (1).csv` / `(2).csv` / `(3).csv` files in `data/`
   suggests the user has been versioning by hand. A user who forgets
   to click the button has no recoverable history at all.

Together: even users who *do* remember to back up cannot fully restore,
and most users will not remember.

## Goals (in scope)

- Automatic on-disk snapshots that capture **every** file in `data/`
  needed to restore application state (round-trip complete).
- Tiered retention so a single bad snapshot cannot destroy the only
  good one.
- Encryption posture that does not silently weaken the user's chosen
  storage encryption.
- A configurable backup directory whose default lives **outside**
  `data/` so a `data/` wipe (intentional or buggy) does not take the
  safety net with it.
- Fix `HandleRestore` to round-trip every file type.
- Minimal UI surface: status line + on/off toggle on the existing
  backup management page; manual download button unchanged.

## Non-goals (out of scope)

- **Off-machine / hardware-loss protection.** Snapshots stay on the
  same machine. The configurable path enables a user to point at a
  synced folder (Dropbox, NAS mount, USB), but the app does not
  manage off-machine replication. (Flavor "A" from brainstorming —
  hardware loss — was explicitly excluded.)
- **Per-record version history / undo / time-travel inside the app.**
  The backup is a whole-data-dir snapshot, not a fine-grained
  per-transaction history. (Flavor "B" — accidental destruction —
  was not selected.)
- **Continuous / change-triggered backups.** Daily cadence only.
- **A separate UI for browsing snapshot contents.** Restore stays
  zip-upload-only for v1.
- **Cloud anything.** Local files only, consistent with the project's
  privacy-first principle.

## Architecture

A new package `internal/services/backup/` (separate from the existing
`internal/handlers/backup/` HTTP layer) owns snapshot, retention,
scheduler, and status. Two integration points wire it in:

- **Lifecycle (`cmd/server/main.go`)**: after storage unlock, calls
  `SnapshotIfStale()` once, registers a graceful-shutdown snapshot,
  and starts the daily ticker goroutine.
- **HTTP surface (`internal/handlers/backup/handlers.go`)**: gains a
  `HandleBackupStatus` endpoint that reads `last_backup.json` and
  reports it to the UI, and a `HandleSetAutoBackupEnabled` endpoint
  for the on/off toggle.

### Components

| File | Responsibility |
|------|----------------|
| `internal/services/backup/service.go` | Public `Service`, `New(cfg, store)`, holds mutex so two snapshots can't race |
| `internal/services/backup/snapshot.go` | `Snapshot(ctx)`, `SnapshotIfStale(ctx, maxAge)` — builds zip, verifies, atomic-renames, updates metadata |
| `internal/services/backup/retention.go` | Tiered prune: keep last 7 daily + 4 weekly + 3 monthly |
| `internal/services/backup/scheduler.go` | `Run(ctx)` — hourly tick, exits on ctx cancel |
| `internal/services/backup/meta.go` | Reads/writes `last_backup.json` (timestamp, file count, bytes, encrypted bool) |
| `internal/config/*` | New `BackupDir` field, env override `BUDGET2_BACKUP_DIR`, default `${XDG_DATA_HOME:-~/.local/share}/budget2/backups` |
| `data/settings/auto_backup.json` | Persists the `AutoBackupEnabled` user toggle (defaults true on first run). Lives inside `data/` because it is user-scoped settings, not config |
| `internal/handlers/backup/handlers.go` | Fix `HandleRestore` to extract every entry; add `HandleBackupStatus`; add `HandleSetAutoBackupEnabled` |
| `internal/services/storage/storage.go` | Add `isAgeEncrypted(data)` short-circuit in `WriteFile` to prevent double-encryption when restoring an encrypted blob into an encrypted store |
| `web/templates/pages/filemanager/*` | Status line + on/off toggle |

### Data flow — snapshot

```
1. Acquire service mutex (return ErrSnapshotInProgress if held)
2. ts := time.Now().UTC().Format("20060102_150405")
3. Walk cfg.DataDir, skipping:
     - cache/ subtree
     - any *.tmp file (atomicWrite leftovers)
     - .encrypted, .encryption-verify markers
4. For each surviving file: read raw bytes from disk
   (NO decryption — encrypted blobs are backed up as-is)
5. Stream into <BackupDir>/budget_backup_<ts>.zip.tmp via zip.Writer,
   preserving the relative path under data/
6. zw.Close(); f.Sync(); f.Close()
7. Reopen with zip.OpenReader to verify integrity
   - on failure: os.Remove(tmp); return error; do NOT update meta
8. os.Rename(tmp → <BackupDir>/budget_backup_<ts>.zip)   ← atomic
9. Write <BackupDir>/last_backup.json:
     {"ts":"...","file_count":N,"total_bytes":B,"encrypted":bool}
10. applyRetention()
    - failure here is logged but does not fail the snapshot
```

### Encryption posture

| Storage state | Auto-backup contents | Manual download (`HandleBackup`) |
|---------------|----------------------|----------------------------------|
| Encrypted | Zip of on-disk **encrypted** bytes verbatim. No additional encryption layer. Restoring requires the same age recipient / SSH key / YubiKey. | **Unchanged.** Decrypts before zipping for portability (existing behavior). |
| Not encrypted | Plaintext zip (same posture as `data/` itself — no regression). | Plaintext zip (unchanged). |

This deliberately separates the two intents:

- **Auto-backups** sit on disk indefinitely and must not silently
  weaken storage encryption.
- **Manual download** is a user-initiated export to a known
  destination (USB, another machine) where decrypted portability is
  the point.

### Triggers

- **Startup**: after storage unlock, `SnapshotIfStale(24h)` runs once.
  Cheap idempotent check against `last_backup.json`.
- **Daily ticker**: scheduler goroutine ticks every 1 hour and calls
  `SnapshotIfStale(24h)`. The hourly-tick / 24h-stale combo means a
  long-running session gets at most one auto-backup per day, and a
  short session that boots after a multi-day gap gets exactly one on
  startup.
- **Graceful shutdown**: signal handler in `main` calls `Snapshot()`
  unconditionally before exit, bounded by a 30-second deadline so a
  stuck snapshot cannot block shutdown forever.

### Retention

Tiered rules, applied after a successful snapshot:

- **Daily tier**: keep most recent backup per calendar day, last 7 days
- **Weekly tier**: keep most recent backup per ISO week, last 4 weeks
  (older than the daily window)
- **Monthly tier**: keep most recent backup per calendar month, last 3
  months (older than the weekly window)
- Anything else → delete

Deletes happen **only after** the new backup is on disk and verified,
so a prune failure can never produce a "no good backups" state. Steady
state is ~14 files / under 1 MB given current data sizes.

### Backup directory location

- Default: `${XDG_DATA_HOME:-$HOME/.local/share}/budget2/backups`
- Override: `BUDGET2_BACKUP_DIR` env var
- Created lazily on first snapshot via `MkdirAll(0700)`
- **`HandleDeleteAllData` explicitly does not touch `BackupDir`**,
  regardless of where it points. This is the defense-in-depth that
  makes the safety net survive a `data/` wipe.

## Restore changes

`HandleRestore` (`internal/handlers/backup/handlers.go:115`) currently
short-circuits at line 158 on `.csv` suffix. Change:

- Iterate every entry in the zip (skipping directories).
- Sanitize each entry's path: reject absolute paths and any path
  containing `..` (existing path-traversal guard, applied to every
  entry not just the basename).
- Preserve relative subdirectory structure under `cfg.DataDirectory`
  (e.g., a zip entry `settings/foo.json` restores to
  `data/settings/foo.json`).
- Write each file via `store.WriteFile`, which now short-circuits if
  the bytes are already age-encrypted (see storage change below).
- Restore counter reports total files, not just `.csv` count.
- Empty-zip / no-files-restored case still returns 400.

## Storage change

`internal/services/storage/storage.go:248` `WriteFile` currently
encrypts unconditionally when storage is encrypted and unlocked. Add a
guard:

```go
if s.encrypted && s.provider != nil && s.provider.IsUnlocked() {
    if isAgeEncrypted(data) {
        // Already encrypted (e.g., restoring an encrypted-backup blob).
        // Write through without re-encrypting.
    } else {
        encrypted, err := encryptData(data, recipient)
        if err != nil { ... }
        data = encrypted
    }
}
```

`isAgeEncrypted` already exists at `storage.go:322` — this is a
~5-line change. Without it, restoring an encrypted backup into an
encrypted store would double-encrypt every file and brick the data.

## UI surface

On the existing backup management page (`filemanager`), add to the
top of the Backup section:

```
┌─────────────────────────────────────────────────────┐
│ Automatic backups: [●—] on                          │
│ Last backup: 2 hours ago · 14 snapshots · 312 KB    │
│ Location: /home/darrell/.local/share/budget2/backups│
└─────────────────────────────────────────────────────┘
```

- Toggle: `POST /backup/auto-enabled` writes the setting; the
  scheduler reads it on each tick and no-ops when disabled. Defaults
  on for new users.
- Status line: `GET /backup/status` returns JSON consumed by HTMX
  swap.
- Existing "Backup" button (manual download) and "Restore" upload are
  unchanged in behavior or position.

## Error handling

| Failure | Behavior |
|---------|----------|
| Walk error mid-snapshot | Abort, delete `.tmp`, leave prior backups untouched, log |
| Zip integrity verification fails | Delete `.tmp`, do NOT update `last_backup.json`, log; next tick retries |
| Disk full on snapshot write | Return error; surface "Last backup failed: <reason>" via status endpoint |
| Disk full on retention prune | Log, succeed snapshot anyway (retention catches up next tick) |
| Concurrent snapshot attempt | Skipped via mutex; returns `ErrSnapshotInProgress`; scheduler treats as no-op (not an error) |
| `BackupDir` does not exist | `MkdirAll(0700)` lazily on first snapshot |
| Storage locked when scheduler ticks | Tick is skipped (no-op) with debug log. Reading the raw on-disk bytes does not require an unlocked provider — but the scheduler still skips while locked, since a locked store is a strong signal that the user is not in an active session and we should not be doing background I/O on their data |
| `HandleDeleteAllData` invoked | Wipes `data/` only; `BackupDir` is explicitly excluded regardless of path |
| Process killed (SIGKILL) mid-snapshot | `.tmp` file is orphaned; cleaned up on next snapshot's pre-walk (any `*.tmp` in `BackupDir` older than 1 hour is deleted) |

## Testing

| File | Coverage |
|------|----------|
| `internal/services/backup/snapshot_test.go` | Round-trips csv + json + nested settings/ + encrypted blobs; verifies zip integrity; asserts atomic-rename behavior on simulated mid-write failure (close zip writer mid-stream); orphan-`.tmp` cleanup |
| `internal/services/backup/retention_test.go` | Table-driven across day/week/month boundaries (DST, ISO-week edge cases, year rollovers); asserts surviving file set for each scenario |
| `internal/services/backup/scheduler_test.go` | Fake clock; no snapshot when fresh, exactly one when stale, ctx-cancel exits cleanly, disabled flag short-circuits |
| `internal/services/backup/service_test.go` | Mutex prevents concurrent snapshots; ErrSnapshotInProgress returned, not deadlock |
| `internal/handlers/backup/handlers_test.go` (extends) | Restore round-trips csv + json + settings/ subdir into both encrypted and unencrypted stores; status endpoint shape |
| `internal/services/storage/storage_test.go` (extends) | `WriteFile` of already-age-encrypted bytes does not double-encrypt; round-trip preserves byte-identity |
| Integration | `make check` happy path on tmp data dir with encryption on and off |

Pre-commit hook (vet + staticcheck + tests) must stay green.

## Migration / compatibility

- **Existing users**: on first server start after upgrade,
  `SnapshotIfStale` produces the first auto-backup. No prompts, no
  blocking dialogs.
- **Existing `data/` layout**: unchanged. The new `BackupDir` is a
  new directory outside `data/`.
- **Existing manual backup zips** (those `bk_download_*.csv` style
  files) are unaffected. Users can continue to restore from any
  previously downloaded zip. Manual download behavior is unchanged.
- **Old restore behavior is fixed, not preserved**: previously,
  uploading a zip that contained JSON files silently dropped them on
  restore. After this change, those files are restored. This is a
  bugfix, not a breaking change.

## Open questions

None — all design choices were resolved during brainstorming
(D+E flavor, C tiered timestamps, D triggers, C configurable path
with default, C encryption posture, B tiered retention).
