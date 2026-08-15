# App-wide MCP — phase 4b (guarded operations) design

Extract the restore logic out of `internal/handlers/backup` into a service,
build the confirm-token guard the master design specified, and ship
`shutdown_server` as its first consumer. `restore_backup` is **deferred**, by
decision, to a later slice.

Refines the "Guarded operations" and phase-4b sections of
`2026-08-12-app-wide-mcp-design.md`. Everything that document says about tool
semantics, write safety and topology still holds.

## What changed since the master design was written

The master design lists three guarded tools. Two are gone from this slice:

- `set_encryption` was **descoped permanently** during phase 4a: enabling
  encryption needs a credential, and a credential passed as a tool argument
  lands in a model's transcript. `get_status` reports encryption state
  read-only instead.
- `restore_backup` is **deferred here, deliberately**, and the reasoning is
  recorded below rather than left implicit.

That leaves `shutdown_server` as the only guarded tool this slice ships — but
the guard is still built as general machinery, because retrofitting binding
and single-use semantics into a redeem path that already shipped without them
is how guards end up subtly wrong.

## Why restore_backup is deferred

Every write tool shipped so far settles into the same shape: it touches one
sidecar JSON, takes a `.bak` first, and has a reversal tool that does not
require the browser. `restore_backup` breaks all three properties at once.

It overwrites the entire live data directory, and `pruneRestoreExtras` deletes
files the archive does not contain — so its blast radius includes data that was
never in the backup. There is no undo tool, and there cannot be a cheap one:
the safety snapshot it takes is a full archive, not a sidecar `.bak`.

The confirm token does not close that gap. A token proves the caller called
twice; it does not prove a human agreed. A model can mint and redeem one inside
a single turn without the user ever seeing the preview. The browser flow has a
human clicking a button after reading a warning — the protocol equivalent of
that does not exist, and pretending the token is it would be the dangerous
mistake.

There is also an unbuilt prerequisite the master design never names: **the
model has no way to identify an archive.** `restore_backup` needs a filename,
and there is no public listing API — `listBackupTimes` is unexported in
`internal/services/backup/retention.go`, and the Backup Status page uses an
inline `filepath.Glob`. Restore therefore implies a `list_backups` tool (or a
new field on `get_status`) that phase 4a did not build.

Deferring costs nothing structural. The extraction below is the actual blocker,
and it lands in this slice, so a later decision to ship restore is a tool
registration against a tested service rather than a refactor.

## The extraction

`restoreFromZip` (`internal/handlers/backup/handlers.go:424-553`), its
`restoreResult` struct (`:558`) and `pruneRestoreExtras` (`:583-647`) move to a
new leaf package,
`internal/services/restore`. Two handlers call them today — `HandleRestore`
(uploaded zip) and `HandleRestoreTestData` (embedded zip) — and both stay in
`handlers`, becoming thin: read bytes, call the service, map the error, render
the same response text.

### Why a new package rather than internal/services/backup

`backup` is a "make and prune archives" service: `Snapshot`, retention, meta,
the enabled flag, `SkipPredicate`. Restore is the inverse operation over the
same file set, which is a real argument for putting it there — but restore also
depends on things `backup` currently knows nothing about: the
`SettingsRewriteGate`, the post-restore cache-invalidation path, and
prune-extras. Folding those in widens a deliberately small service's dependency
surface. `restore` imports `backup` for `SkipPredicate` and `SnapshotAndHold`;
the dependency runs one way and no cycle is possible.

### Interface

The service is constructed with everything the package-level handler globals
supply today, so nothing reads mutable package state:

```go
package restore

type Deps struct {
    DataDir   string
    BackupDir string
    Store     *storage.Storage
    Backups   SnapshotHolder      // SnapshotAndHold(ctx) (func(), error)
    Gate      SettingsRewriteGate // may be nil; see below
}

type Result struct {
    Restored         int
    Pruned           int
    SkippedProtected int
    PruneFailures    int
}

func New(d Deps) *Service
func (s *Service) FromZip(ctx context.Context, content []byte) (Result, error)
```

`Result`'s fields are exported where `restoreResult`'s were not; the handler
already renders all four.

The snapshot-holder interface is deliberately **not** called `Snapshotter`:
`internal/services/mcpsvc/snapshot` already exports a `Snapshotter`, and it is a
different thing entirely (sidecar `.bak` copies, not archive locks). Two types
with one name in one codebase is how the wrong one gets wired. Phase 3's
self-review caught the same hazard with a duplicated `rowFor`.

### Errors

`restoreFromZip` returns `(restoreResult, int, string)` — an HTTP status and a
message. That is why it could never leave the handler package. The service
returns typed sentinels instead, and the handler maps them to exactly the
statuses it returns today:

| Sentinel | Today's status | Raised when |
|---|---|---|
| `ErrInvalidArchive` | 400 | `zip.NewReader` fails |
| `ErrUnsafePath` | 400 | absolute path, `..` segment, or destination escaping the data dir |
| `ErrUnreadableEntry` | 400 | an entry cannot be opened or read |
| `ErrEncryptedEntry` | 400 | an age-encrypted blob targeting a store that is not encrypted+unlocked |
| `ErrEmptyArchive` | 400 | no restorable entries survived the skip list |
| `ErrBadDataDir` | 500 | `filepath.Abs` on the data directory fails |
| `ErrNoBackupService` | 500 | no snapshotter configured |
| `ErrSnapshotFailed` | 500 | the safety snapshot fails for any reason other than the one below |
| `ErrWriteFailed` | 500 | `MkdirAll` or `store.WriteFile` fails |
| `backup.ErrSnapshotInProgress` | 409 | a snapshot is already running |

Each is wrapped with `fmt.Errorf("...: %w", ...)` so the entry name and
underlying cause survive into the message the handler renders. The handler
classifies with `errors.Is`. `backup.ErrSnapshotInProgress` is re-exported
rather than redefined, so one identity means one thing across both packages.

### Two invariants that must survive the move

These are load-bearing and easy to lose in a mechanical extraction:

1. **The snapshot hold and the rewrite gate bracket the whole write+prune.**
   `SnapshotAndHold`'s release and the gate's `endRewrite` are both deferred, so
   they unwind after `pruneRestoreExtras`. Nothing between acquiring the gate
   and returning may call a `SettingsManager` method — that deadlocks. The
   comment saying so moves with the code.
2. **A nil gate logs loudly and proceeds.** It means the service was wired
   without a settings manager, and the restore then runs unserialized against
   settings saves. Silently tolerating it would hide a wiring regression; the
   existing log line moves too.

Counting behavior also moves verbatim: `restored` counts `archiveEntries`, not
the write queue, and `skippedProtected` dedupes — so duplicate zip entries count
once on both paths. That symmetry was a fix, not an accident.

## The confirm-token guard

New leaf package `internal/services/mcpsvc/confirm`, following the precedent
phase 3 set with `snapshot`: a shared type used by tool subpackages lives
*below* them, never in `mcpsvc` itself, because `mcpsvc` imports the
subpackages and a type flowing the other way is an import cycle.

```go
package confirm

type Registry struct{ /* mu, map[string]entry, ttl, now func() time.Time */ }

func NewRegistry(ttl time.Duration) *Registry
func (r *Registry) Mint(tool string, args any) (token string, expiresAt time.Time, err error)
func (r *Registry) Redeem(token, tool string, args any) error
```

Properties, each of which gets its own test:

- **Single-use.** `Redeem` deletes the entry before returning success. A replay
  is refused.
- **Tool-bound.** A token minted for one tool is refused by another, so a guard
  cannot be laundered between operations.
- **Args-bound.** The token carries a hash of the canonical JSON of the
  arguments; redeeming with different arguments is refused.
- **TTL'd.** Expired tokens are refused and swept. `now` is injectable so the
  expiry test does not sleep.
- **In-memory, dropped on restart**, per the master design. A restart is a
  legitimate way to invalidate every outstanding token.

Tokens come from `crypto/rand`. `Redeem` compares with
`crypto/subtle.ConstantTimeCompare` — not because the threat model needs it,
but because a token comparison that is not constant-time is a finding waiting
to be filed against it.

**Honest limits, stated here so no one has to rediscover them.** The args-hash
binding does nothing for `shutdown_server`, whose input is empty and whose hash
is therefore constant — it exists for restore later. And the whole two-step
protocol buys *deliberateness*, not consent: a model can mint and redeem in one
turn. It raises the bar from "a stray tool call stops the server" to "a model
must decide twice with a preview in between". That is worth having and is not
the same as a human approving.

## shutdown_server

Registered in `admin`, guarded by the registry.

- **First call** (no token): performs nothing, returns what shutting down would
  mean — that every MCP tool in this session stops answering, that an open
  browser tab stops working, and that nothing in-process can start the server
  again — plus the token and its expiry.
- **Second call** (echoing the token): schedules the exit and returns *first*.
  The existing `/killme` shape is the model: hand back the response, then exit
  after a short delay, because a handler that exits inline never delivers its
  result.

`admin.Deps` gains `Shutdown func()` and `Confirm *confirm.Registry`, both
nil-able in the established style — a nil dependency fails that one call with a
named error rather than dropping the tool from the list. **`Shutdown` must be
injected, never a direct `os.Exit`**: a test that calls the real thing kills the
test binary. `cmd/server` supplies the real one; it follows the `exitFunc` seam
already in `handlers/backup`.

The tool description must say plainly that this is not recoverable from inside
the session. `.mcp.json` points at `http://localhost:8080/mcp`, and the client
only registers these tools if the server was already running when the session
started — so after a shutdown the user must restart the server *and* the
session. A model that reads "I can just start it again" from a vague
description would be wrong in a way it cannot detect.

## Testing

- `restore` gets real service-level tests over a `t.TempDir()` — the first time
  this logic is testable without an HTTP request. Every sentinel above gets a
  case, including the ones only reachable through a malformed archive
  (traversal, absolute path, encrypted-entry-into-plaintext-store).
- The extraction moves existing tests alongside the code. **Do not work from a
  list of test names in a plan document** — enumerate the real callers with
  `LSP findReferences` on each moved symbol and move what that finds. Test
  inventories written by hand have been undercounted every time they have been
  tried in this repo.
- Coverage is verified per package before and after the move. No loss is
  acceptable.
- `confirm` tests cover replay, wrong tool, wrong args, expiry, and the happy
  path, with an injected clock.
- `shutdown_server` tests drive a real `mcp.Client` over the in-memory
  transport with a recording `Shutdown` func, and assert the first call does
  **not** invoke it.
- Every new test must be checked by mutation: disable the branch it claims to
  guard and confirm it fails. Red-before/green-after has passed vacuous tests in
  this repo before.

## Decisions from the final whole-branch review

**(a) Five restore error strings changed wording, deliberately.** The
extraction moved error text from ad hoc strings in the handler to messages
rendered from the sentinels in the table above, and five of the resulting
400-class strings now read differently than they did before the move:
absolute path in archive, path traversal in archive, a destination escaping
the data directory, an unreadable entry (which also collapsed the old
separate "cannot open" and "cannot read" messages into one `ErrUnreadableEntry`
string), and an encrypted entry going into a store that is not
encrypted-and-unlocked. This is intentional, not a regression to fix: the new
text is strictly more informative — it names the offending archive entry —
and restoring the byte-identical old prose would mean re-deriving those
distinctions in the handler from sentinels that no longer carry them.

**(b) `shutdown_server` signals rather than exits.** `cmd/server/main.go`
wires `Shutdown` to send the process `SIGTERM` (falling back to `os.Exit(0)`
only if the signal send itself fails), not to call `os.Exit` directly. This
lets a guarded shutdown go through the same signal handler as an operator's
Ctrl-C: it drains in-flight HTTP requests and takes a final backup snapshot
before the process exits, rather than killing the process mid-request. The
concrete failure this avoids: a browser `POST /restore` mid write-and-prune,
holding the snapshot lock and the settings gate, when a model redeems a
shutdown token — a hard exit there would leave the data directory
half-restored with no shutdown snapshot taken.

## Out of scope

- `restore_backup` and its `list_backups` prerequisite.
- `set_encryption`, permanently.
- Generalizing the guard into an `mcp.AddTool` wrapper. With one consumer the
  abstraction would be designed against a sample size of one; revisit when
  restore lands.
- The pre-existing fact that `/killme` is a public, unauthenticated `GET`
  outside the lock-check group. It is relied on by `scripts/whatif-verify.sh`.
  Worth a look someday; not this slice's problem, and changing it would break
  the verify tooling this work depends on.
