# File Manager: multi-upload, sortable columns, folder import

Date: 2026-08-12
Status: approved, not yet implemented

## Task table

Tiers per `TIERS.md`, approved by the user 2026-08-12. Tiers may only move up
mid-run, never down.

| Task | Scope | Tier | Justification |
|------|-------|------|---------------|
| P12 | Multi-file upload (§1) | 1 | Strong oracle (Go tests), reversible, single handler |
| P13 | Sortable columns (§2) | 2 | Weak oracle — client-side JS, no JS test infra in a pure-Go repo |
| P14 | `ImportDirectory` config + scan endpoint (§3) | 2 | Read-only, but `config.Config` is shared surface |
| P15 | Import execute + source delete (§3) | 3 | Irreversible — deletes files from the import folder |

The folder import is split so that only P15 — the code path that calls
`os.Remove` — carries Tier 3's blind N-version cost. P14 is read-only.

No task modifies a path in `.swarm/critical.globs`
(`internal/services/storage/**`, `internal/services/dataloader/**`,
`internal/services/retirement/engine/**`); P14 and P15 call storage without
changing it.

## Problem

The File Manager ([web/templates/pages/filemanager.html](../../../web/templates/pages/filemanager.html))
accepts one CSV at a time, renders its file table in fixed name order, and
leaves every imported bank export sitting in `~/Downloads` forever. Importing a
month of statements means repeating a one-file dance a dozen times, then
cleaning up by hand.

Three independent changes fix that. They share a file and a handler package but
have no dependency on each other and can land in any order.

## Constraint that shaped the design

A browser cannot delete a file chosen through `<input type="file">`. The element
hands the server the bytes and the bare filename, never a path, and no web API
can remove it from disk. "Delete the source after import" is therefore not
buildable on the upload path at all.

budget2 runs on localhost as the user, so the server *can* delete files it reads
directly off disk. That requires a server-side import path, which is what
section 3 specifies. The browser upload keeps working and simply never offers a
source delete.

## 1. Multi-file upload

`handleFileUpload` ([internal/handlers/explorer/handlers.go:480](../../../internal/handlers/explorer/handlers.go))
takes a single `r.FormFile("file")`.

- The input gains `multiple`.
- The handler iterates `r.MultipartForm.File["file"]` instead.
- Per-file validation is unchanged: `sanitizeUploadFilename` → `.csv` suffix
  check → `store.WriteFile`.
- One bad file does not abort the batch. Each file resolves to `saved`,
  `skipped: already exists`, or `rejected: <reason>`, and the response reports
  every outcome.
- Collisions skip, matching section 3. No overwrite, no auto-rename.

Incidental fix: the `ParseMultipartForm(10 << 20)` call carries a "max 10MB"
comment, but that argument is only the in-memory spill threshold — there is no
actual size limit today. Wrap the body in `http.MaxBytesReader` so the stated
limit is real.

## 2. Sortable columns

Sorting is client-side, scoped to the `filemanager-file-list` partial
([filemanager.html:1170](../../../web/templates/pages/filemanager.html)).

- Header cells for File, Rows, Date Range, and On become buttons that sort the
  `<tbody>` rows, toggling ascending/descending, with an arrow on the active
  column.
- Each `<tr>` carries `data-name`, `data-size`, `data-rows`, `data-mindate`,
  and `data-enabled`. Sorting compares those typed values, not the rendered
  strings — otherwise `25K` sorts above `4K` and dates sort lexically.
- The table is an htmx swap target: every toggle, delete, and upload replaces
  the list and would drop the sort. Sort state lives in a page-level script and
  re-applies on `htmx:afterSettle`.

Alternative considered: server-side sorting via `?sort=&order=`, which survives
swaps natively. Rejected because it means threading sort parameters through all
four handlers that render this partial, for a table of roughly a dozen rows.

## 3. Import from folder

A new panel below the upload row, listing CSVs found in one configured folder.

### Configuration

`ImportDirectory` on `config.Config`
([internal/config/config.go:21](../../../internal/config/config.go)), defaulting
to `~/Downloads`, overridable by `BUDGET2_IMPORT_DIR`. Same shape as
`BackupDir`. The UI displays the path but cannot change it.

### `GET /explorer/import/scan`

Lists `*.csv` directly inside `ImportDirectory`. No recursion, no symlink
following. Each entry carries name, size, parsed date range, and an `exists`
flag set when the data dir already holds that name. Entries with `exists` render
pre-unticked and disabled.

### `POST /explorer/import`

Takes the ticked names plus a `delete_source` flag. Per file:

1. Reject any name whose `filepath.Base` differs from the name as scanned.
2. Re-stat the file inside `ImportDirectory` and confirm the resolved path is a
   direct child of it.
3. Skip if the destination name already exists in the data dir.
4. Read, then write through `store.WriteFile` so encryption applies.
5. Read the destination back and confirm the expected byte length.
6. Only if `delete_source` is set and every step above succeeded, `os.Remove`
   the original.

The result panel lists every file and its outcome.

The readback in step 5 is what makes "fully saved" mean something. Without it a
truncated or encryption-failed write would still clear the way for the source
delete.

### Deletion guards

The source delete is gated three ways, all of which must hold: the write
succeeded, the readback matched, and the path resolved to a direct child of
`ImportDirectory`. A skipped file is never deleted.

## Duplicate handling: nothing new

`LoadData` already runs `deduplicateTransactions` across the merged set of all
enabled files
([internal/services/dataloader/loader.go:197](../../../internal/services/dataloader/loader.go)),
keyed on `sha256(date | lowercased description | amount)`
([internal/models/transaction.go:60](../../../internal/models/transaction.go)).
Importing CSVs with overlapping date ranges collapses the shared rows
automatically, first-wins. That is why the data dir can hold
`bk_download (1).csv` alongside `bk_download.csv` without double-counting.

So the importer adds no dedup of its own. It reports saved / skipped / rejected
and lets load-time dedup do its job.

Two known edges, both pre-existing and both deliberately left alone:

- Genuine same-day repeats collapse. Two identical-amount charges at the same
  merchant on the same date hash identically and become one. Importing more
  overlapping files makes this more likely to surface.
- The per-file `Rows` column is a raw count, so two overlapping files each
  display their full row count even though the merged view counts the shared
  rows once. Cosmetic.

The `/duplicates` page is unrelated to this: it pairs a scheduled bill-pay with
a posted check at the same amount within 7 days
([internal/services/dataloader/near_duplicates.go:48](../../../internal/services/dataloader/near_duplicates.go)).
`isCandidatePair` requires exactly one of each kind, so identical rows never
reach it — load-time dedup has already removed them.

## Out of scope

- No merging of near-duplicate files.
- No undo for the source delete beyond existing backups.
- No content-hash dedup at the file level; transaction dedup covers it.
- No change to the `Rows` column's raw-count behavior.

## Rulings

**2026-08-12a — P12 traversal wording.** The P12 task brief called for
traversal-shaped names to be "rejected". The implementation saves them instead:
`sanitizeUploadFilename` applies `filepath.Base` *before* its `..` check, so
`../evil.csv` normalizes to `evil.csv` and lands inside the data directory. This
is pre-existing behavior with a pre-existing test.

Ruled: the brief's wording was imprecise, not the code. The property that
matters is that no write escapes `DataDirectory`, and it holds —
`filepath.Base` output contains no separator (backslashes are normalized to
`/` first), so the sanitized name cannot act as a multi-segment path when
joined; a base name of literally `..` is caught by the explicit `Contains("..")`
check before any join. Verified independently by the checker rather than
inferred from the worker's test.

`sanitizeUploadFilename` is shared with `handleFileDelete` and was correctly
left unmodified. Tightening it to reject rather than normalize would change the
delete path too and belongs in its own task if wanted.

## Testing

- `sanitizeUploadFilename` and the import path guard: table tests for
  traversal (`../`), absolute paths, symlinks pointing outside the import dir,
  and names that differ from the scanned entry.
- Multi-upload: a batch mixing a valid CSV, a `.txt`, and a colliding name
  yields saved / rejected / skipped respectively, and the valid file lands.
- Import with `delete_source`: source is gone after success; source survives a
  skipped collision; source survives a simulated write failure.
- Readback guard: a `store` stub whose readback returns short bytes must leave
  the source in place.
- Scan: non-CSV files, subdirectories, and symlinks are excluded.

Sorting is client-side JS and is verified by hand in the browser.
