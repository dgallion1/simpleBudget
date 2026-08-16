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

**Ordering constraint: P14 must land before P15 is set up.** P15 builds on the
`ImportDirectory` config field and the scan endpoint, and `tier3-setup.sh` cuts
its two blind worktrees from whatever `HEAD` points at. Running the setup before
P14 merges would hand both workers a tree missing the foundation they need, and
the oracle's scan check would fail in both worktrees for a reason unrelated to
either implementation. P12 and P13 are independent and may land in any order.

The P15 oracle is written and lives at `.swarm/tier3/P15/accept.sh`. It asserts
on filesystem effects and HTTP status codes rather than response-body structure,
because two independent implementations will render their per-file outcome lists
differently and that difference is not a behavioral divergence — it would
otherwise show up as noise in `tier3-compare.sh`'s diff. Every check that
asserts a safety property ("the source survived") also requires a non-404
status, so that an unimplemented endpoint cannot satisfy it vacuously. Verified
against the current tree: 3 of 13 pass (build, unit tests, server boot — all
genuinely true today), 10 fail, and output is byte-identical across runs.

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

Wire format, pinned so that independent implementations agree: the request is
form-encoded (`application/x-www-form-urlencoded`), with the field `name`
repeated once per selected file and an optional `delete_source` field whose
value is `true` to enable deletion. Any other value, or the field's absence,
means do not delete. Names are bare filenames as returned by the scan; a name
containing a path separator is rejected without touching the filesystem.

The handler returns 200 for a processed batch even when individual files were
skipped or rejected — per-file outcomes travel in the body, matching P12's
batch-upload behavior. A malformed request (no `name` fields at all) returns
400.

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

**2026-08-16a — P13 attempt 1 rejected: the swap target is a different
template.** Both Tier-2 checkers returned PASS on attempt 1, including on the
high-risk "sort must survive htmx swaps" criterion. Both were wrong, and wrong
in the same way: each traced the *initial page* include
(`filemanager-content` → `filemanager-file-list`, filemanager.html:1334) and
assumed the htmx swap returns that same template. It does not.

`handleFileToggle` (handlers.go:583), the per-row delete (:640) and the upload
(:740) all call `RenderPartial(w, "file-list", ...)`, and `{{define
"file-list"}}` lives in **`web/templates/pages/explorer.html:739`** — a
different template in a different file, carrying none of P13's markup.
`grep -rln "data-sort-btn" web/templates/` matches only `filemanager.html`.

Established by running the app against a fixture data directory:

```
$ curl -s -X POST /explorer/files/toggle -d "filename=zeta-bank.csv&enabled=true" \
    | grep -c "data-sort-btn"
0
```

and confirmed in a browser: after a real click on a file's "On" checkbox, the
swapped-in `#file-list` had no sort buttons, no `data-*` row attributes, no
`aria-sort`, no `scope="col"`, and different column headers. The sort UI
disappears entirely until a full page reload — the `htmx:afterSettle` re-apply
is structurally unable to help, because there is nothing left to re-wire.

Ruled: attempt 1 fails the acceptance criterion; P13 returns to the worker as
attempt 2. Two process consequences, both binding on later tasks:

1. A checker may not conclude that an htmx swap preserves anything by reading
   the template include. It must establish which template the *handler*
   renders, and where feasible assert against the endpoint's actual response
   body.
2. Any task whose acceptance depends on swapped-in markup must ship a Go test
   that renders the **swap partial** and asserts on it. Attempt 1 had no such
   test, which is why a green suite coexisted with a broken feature.

**2026-08-16b — P13 attempt 2 may change Go handlers.** The P13 brief said "do
not touch `GetFileInfo` or any handler". Attempt 2 changed three
(`handleFileToggle`, `handleFileUpload`, `handleFileDelete`), each a one-token
change to the template name passed to `RenderPartial`, plus deletion of the
orphaned `{{define "file-list"}}` in `explorer.html`. Checker-tests flagged the
deviation rather than scoring it, and asked for a ruling.

Ruled: in scope. The brief's "no handler" clause was written on the assumption
that the swap rendered the same template the page did; ruling 2026-08-16a
established it did not, which makes the acceptance criterion unreachable
without touching those three call sites. The narrower reading — change the
minimum needed to make the swap render the sortable partial — is what attempt 2
did. No handler logic, signature, or route changed.

The `{{define "file-list"}}` deletion was independently verified safe by both
checkers: `git grep` shows its only consumers were those three `RenderPartial`
calls, there was never a `{{template "file-list"}}` invocation, and the Explorer
page still renders (HTTP 200, no template errors).

**Known, not caused by P13:** an axe run during P13 attempt-2 verification found
20 pre-existing WCAG violations on the File Manager page (unlabelled toggle
checkboxes, unnamed SVG delete buttons, low-contrast size text), all confirmed
byte-identical at HEAD. They belong to a follow-up task, not to P13, and are
recorded here so the final accessibility pass does not mistake them for a
regression introduced by this run.

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
