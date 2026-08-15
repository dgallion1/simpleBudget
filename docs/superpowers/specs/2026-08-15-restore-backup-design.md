# restore_backup and list_backups — design

Ships the two tools phase 4b deferred. Supersedes the "Why restore_backup is
deferred" section of `2026-08-15-app-wide-mcp-phase-4b-design.md` and the first
line of that document's "Out of scope"; everything else there still holds.

## The decision, and what it does not change

4b deferred restore on three grounds. Two are now answered and one is not:

- **"No service to call."** Answered in 4b: `internal/services/restore` exists
  and is tested. Restore over MCP is a tool against that service.
- **"The model has no way to identify an archive."** Answered here:
  `backup.Service.List` is the public listing 4b said did not exist, and
  `list_backups` exposes it. The Backup Status page's inline glob now calls
  the same method, so the page and the tool cannot disagree about what counts
  as an archive — the glob counted `budget_backup_NOT_A_DATE.zip`, the service
  does not.
- **"The confirm token proves deliberateness, not consent."** Still true.
  Shipping was decided with this understood, not resolved. A model can mint and
  redeem a token inside one turn without a human ever seeing the preview. The
  guard raises the bar from "a stray tool call rewrites the data directory" to
  "a model must decide twice with a preview in between"; that is what it buys
  and the only thing it buys. The honest mitigation is not in the protocol — it
  is in the tool description and the preview text, which state the prune and
  the absence of an undo in terms a user can answer yes or no to. The browser's
  Backup page remains the path with a human actually in the loop, and the
  README says so.

## list_backups

`backup.Archive{Name, TS, Bytes}` and `(*Service).List() ([]Archive, error)`,
built on the existing `listBackupTimes`. Two properties are contract, not
incident:

- **Newest first.** "Restore the most recent backup" is the common request;
  making the caller sort is how it gets sorted wrong.
- **`Name` is a bare filename, never a path.** It is what restore takes, so a
  name that could carry a directory is a name that could point outside the
  backup directory.

An archive whose file disappears between the directory scan and the stat
(retention prunes concurrently) is omitted rather than reported at zero bytes:
it is not restorable, so listing it only invites a call that fails.

The tool reports each timestamp twice — `ts` in the `YYYYMMDD_HHMMSS` form
`get_status` already uses for `backup.last_backup_ts`, so the two answers can
be lined up, and `ts_iso` in RFC3339 so ordering and age need no parsing. An
empty directory returns `archives: []`, never null: null reads as "unknown"
where the truth is "none".

## Name resolution lives in the service

`(*Service).FromArchive(ctx, name)` resolves the name against `BackupDir` and
delegates to `FromZip`. It is in the service and not in the tool because it is
the only place an externally supplied string becomes a filesystem path, and
that check belongs where it can be tested without an MCP client.

`backup.ValidArchiveName` is the gate: prefix, `.zip` suffix, and a timestamp
that parses. The shape is exact enough that traversal cannot survive it, but
the explicit `filepath.Base` check stays anyway — a validator that only rejects
traversal as a side effect of an unrelated rule is one refactor away from not
rejecting it at all. New sentinels: `ErrBadArchiveName`, `ErrNoSuchArchive`,
`ErrArchiveUnreadable`, `ErrNoBackupDir` (an empty `BackupDir` would otherwise
resolve the name against the process working directory).

## The tool

Guarded through the same `confirm.Registry` as `shutdown_server`, with the
args-binding that shutdown could not use: the token carries the archive name,
so "preview the small archive, restore the big one" is refused. That binding
was built in 4b for this tool and is now doing the job it was built for.

Three decisions worth keeping:

- **The preview is minted only for an archive that is really on disk.** A token
  for a name that cannot be restored is a promise the server cannot keep — the
  same reasoning that makes a nil `Shutdown` fail before minting.
- **The preview names the prune.** Replacing files is the obvious half of a
  restore; deleting files the archive never contained is the half that
  surprises people, and it is what the user is actually being asked to agree
  to. A test asserts the preview says so.
- **A failed restore reports whether data can have changed.** The service
  validates the whole archive and takes its safety snapshot before it writes
  anything, so every failure except `ErrWriteFailed` leaves the data directory
  untouched — and saying "nothing changed" for `ErrWriteFailed` would be a lie
  in the one case that matters. The redeem happens before the restore, so the
  token is spent either way; the error says that too.

## Wiring

`handlers/backup.Initialize` now returns the `*restore.Service` it builds, and
`cmd/server` hands that instance to `mcpsvc.Deps.Restores`. One service, not
two: a second instance over the same directories works only for as long as
nobody gives the two different gates or directories, and the failure mode if
they diverge is a restore that does not hold the settings gate.

## Known gap, unchanged

The phase-4a prune race is still open and this widens its reach: an HTTP or
tool restore's prune can delete a file an MCP data-dir write tool created
seconds earlier, because those writers take neither the snapshot hold nor the
settings gate. Restore over MCP does not introduce the race — it makes it
reachable without the browser. It wants a design pass, not a patch, and is not
in this slice.

## Testing

- `backup`: ordering, non-archive files ignored (including `.zip.tmp`
  in-flight snapshots), empty directory, and `ValidArchiveName` against every
  traversal shape.
- `restore`: a real archive round-trip, every new sentinel, and the assertion
  that a rejected name leaves the data directory untouched.
- `admin`: driven through a real `mcp.Client` over the in-memory transport,
  against a real archive produced by a real `Snapshot` — not a fixture zip.
  The preview call is asserted not to touch the data directory; replay and
  name-mismatch are asserted not to restore, with a sentinel file that a prune
  would have deleted.
- Mutation-checked: dropping the args binding fails the name-binding test,
  making the preview restore fails the guard test, and dropping the newest-first
  reversal fails both ordering tests.
