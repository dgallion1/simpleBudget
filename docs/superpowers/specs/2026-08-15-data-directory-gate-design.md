# The data-directory gate — closing the phase-4a prune race

Closes the race carried as a known gap since phase 4a and widened by shipping
`restore_backup`: a restore's prune deletes a file an ordinary writer created
moments earlier, and the writer is told its write succeeded.

## The bug

A restore rewrites the data directory from an archive and then prunes
everything the archive did not contain. Between those two steps — and during
either of them — any other writer could land a file. The prune walk then
deletes it, because it is not in `archiveEntries`. The writer gets no error;
the data is simply gone.

The writers are the MCP curation and duplicate tools, the page handlers, and
anything else that touches a sidecar JSON. None of them took the snapshot hold
or the settings gate, which were the only two locks a restore held.

## Where the gate goes, and why not with the writers

At the storage layer, as a second RWMutex on `*storage.Storage`.

Every data-directory content write already funnels through `Storage`
(`WriteFile`, `Remove`, `MkdirAll`). Putting the gate there catches writers by
construction, including ones nobody remembers to update — and "a new writer
did not know there was a hold to take" is exactly how this bug got here. The
alternative, making each writer take `SnapshotAndHold`, is opt-in in the same
way that failed before, and it is the wrong tool besides: that call takes a
full archive per write.

It is deliberately not the existing `mu`, which guards encryption state. A
restore has no business blocking an unlock, and a mutex covering both would
have no stateable meaning.

**Reads are not gated.** A reader during a restore can see a partly rewritten
tree; that is a display concern a page reload settles. Gating reads would put
every page render in contention with a restore — a much larger change for a
much smaller problem — and would add lock edges from every read path.

## `ExclusiveWriter`

`Storage.BeginExclusive()` blocks until in-flight writes finish, then holds
the directory until `Release`. `sync.RWMutex` is not reentrant, so the holder
cannot call the plain write methods — it would deadlock on the lock it already
owns. The handle exposes `WriteFile`/`Remove`/`MkdirAll` that go through the
unexported `*Locked` bodies instead, the same shape
`backup.Service.snapshotLocked` already uses. `Release` is idempotent so an
explicit release followed by a deferred one cannot double-unlock, and using a
released handle is refused rather than silently writing outside the exclusion.

## Lock order

**Settings rewrite gate → data directory → snapshot hold.**

This is not a free choice. `SettingsManager.SaveWithRevision` holds the
manager's lock across its write through `Storage`
(`internal/services/retirement/settings.go:721,793`) — settings, then data. A
restore taking data-then-settings is the other half of an ABBA deadlock: the
restore holds the data directory and waits for the settings lock while an
in-flight save holds the settings lock and waits for the data directory. The
server hangs until it is killed.

The first version of this change had exactly that order. It was caught by the
regression test described below, which is why the test stages the save as
already in flight rather than starting it from inside the restore — a probe
fired from within the critical section is past the window where the cycle
forms, and passes against the broken order.

The snapshot hold is innermost and adds no edge back: nothing holding it
writes through `Storage` or calls the settings manager (snapshots write into
the backup dir with `os` directly).

Accepted consequence of taking the settings gate first: a restore that fails
at the snapshot step has still opened and closed the gate, so it bumps the
settings revision and drops the cache. That costs one page refresh on a failed
restore.

## The safety snapshot moved inside the hold

The hold is taken **before** `SnapshotAndHold`, not after. Acquired the other
way round, a write landing between the snapshot and the hold would be deleted
by the prune *and* absent from the safety archive — the only failure mode
where data has no copy anywhere. Under the hold, anything the prune deletes is
guaranteed to be in the archive the restore just took.

## Known bypasses, stated rather than implied

- **`settings/auto_backup.json`** is written by `backup.Service.saveEnabled`
  with `os.WriteFile`, inside the data directory and therefore subject to the
  prune. It stays that way deliberately: routing it through `Storage` would
  encrypt it on an encrypted store, and `loadEnabled` reads it with
  `os.ReadFile` at construction time, before anything is unlocked. A
  `SetEnabled` concurrent with a restore can still be lost.
- **The `cache/` write** in `handlers/backup` bypasses `Storage` too, but
  `SkipPredicate` skip-lists `cache/`, so the prune never touches it.

## Residual limitation

The gate makes each write atomic with respect to a restore. It does not make a
read-modify-write atomic. A tool that read a sidecar before the restore and
writes after it will not be pruned — its file survives — but it writes content
derived from pre-restore state, last-writer-wins. That is the same residual
`BeginExternalRewrite` already documents for settings saves: a gate serializes
in-flight operations, it cannot retract a stale caller's intent.

## Testing

- `storage`: the hold blocks writes, removes and mkdirs; a second holder waits;
  the holder can write through its own hold (the reentrancy trap); `Release` is
  idempotent; a released handle refuses to write.
- `restore`: an ordinary write arriving during a restore does not complete
  inside the critical section and survives afterwards; the safety snapshot runs
  under the hold; a restore does not deadlock against an in-flight settings
  save.
- Mutation-checked. Dropping the hold fails the interleaving test with "a write
  completed inside the restore's critical section" and the snapshot test with
  "the data directory was not held". Swapping the settings gate and the data
  directory fails the ordering test with the deadlock message. The first
  version of the ordering test did **not** fail against the swapped order —
  that is why it was rewritten.
