# Symlinks in the data directory

**Status:** observation, not a defect report. No code changed. Written
2026-08-26 while fixing an unrelated batch of audit findings, because the
behaviour was rediscovered from scratch twice and is worth writing down before
someone "fixes" one half of it in isolation.

## Summary

Nothing in the data directory is designed around symlinks, and the three
components that walk or write it each resolve them differently. Every
behaviour below was verified against the current code with temporary probe
tests, not inferred from reading it.

| Situation | What happens today |
|---|---|
| `data/x.csv` is a symlink to a file, snapshot runs | Archived as an ordinary entry containing **the target's bytes**. That it was a link is not recorded. |
| `data/linkdir` is a symlink to a directory, snapshot runs | **The whole snapshot fails** — `read …/linkdir: is a directory` — and no zip is written. |
| `data/x.csv` is a symlink, restore's archive contains `x.csv` | The link is **replaced by a regular file**. The old target keeps its previous contents and is orphaned. |
| `data/x.csv` is a symlink, restore's archive does not contain it | Prune **unlinks the link**. The target is untouched. |

## Why it comes out this way

`backup.buildZip` (`internal/services/backup/snapshot.go`) walks with
`filepath.Walk`, which stats with `Lstat`. A symlink therefore reports
`IsDir() == false` and reaches the file branch, where `os.ReadFile` *does*
follow it. For a link to a file that silently dereferences; for a link to a
directory `os.ReadFile` returns `EISDIR`, the walk returns that error, and
`snapshotLocked` aborts before publishing a zip.

`restore.FromZip` writes through `storage.ExclusiveWriter.WriteFile` →
`atomicWrite`, which stages beside the destination and renames over it.
`rename(2)` replaces the *link*, not what it points at. `pruneExtras` calls
`os.Remove`, which likewise unlinks the link itself.

So the two halves are not really inconsistent about data safety — a target's
bytes are never destroyed by either path — but they are inconsistent about the
link: backup preserves the *content* and drops the *indirection*, restore drops
the indirection too. A symlink never survives a backup/restore round trip.

## The part that actually bites

The directory case, not the file case. One stray symlinked directory anywhere
under `data/` turns every scheduled snapshot into a no-op that only shows up in
the log. A user who symlinks `data/imports` at an external drive gets no
backups from that moment on, and the failure is silent from the UI's point of
view. If any of this is ever addressed, address that first.

## Adjacent: the MCP snapshot write

`mcpsvc/snapshot.Ensure` (`internal/services/mcpsvc/snapshot/snapshot.go`)
publishes its `.bak` with a bare `os.WriteFile` — no staging file, no rename,
no fsync — while every other write to user data in this codebase goes through
`storage.atomicWrite` or `saveConfig`. It is the sole recovery path for an
unwanted tool-driven write, and its caller proceeds to overwrite the source as
soon as `Ensure` returns nil, so a crash mid-write can leave a truncated
recovery copy of a file that is about to be replaced. It also reads its source
with `os.ReadFile`, which dereferences a symlinked scenario file the same way
the backup walk does.

This one is cheap to fix and does not need the symlink question resolved
first: give it the same staged-write-then-rename-then-fsync treatment
`saveConfig` now has.

## Options, if this is ever picked up

1. **Refuse them.** `Lstat` in the backup walk and the prune walk; skip
   symlinks and report them once. Snapshots stop failing on a symlinked
   directory; a linked file stops being silently absorbed into the archive as
   a copy. Least code, and it makes the current de-facto rule ("the data
   directory is a plain tree") explicit.
2. **Follow them deliberately.** Walk with `EvalSymlinks` and descend into
   linked directories, keeping the dereference on the file side. Backups get
   larger and can escape the data directory, which is a containment change,
   not just a behaviour change.
3. **Preserve them.** Record link targets as zip symlink entries and recreate
   them on restore. Most faithful, most work, and it puts restore in the
   business of writing links from archive content — which needs the same
   traversal validation the path checks already do.

Option 1 matches how the rest of the codebase already treats symlinks:
`internal/handlers/explorer` (`handlers.go`, around the import scan) `Lstat`s
each candidate and drops symlinks rather than following them.
