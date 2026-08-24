# Housekeeping — the app itself

Nine tools about the app rather than the money in it: status, data files,
the duplicate-review queue, and backups. The two guarded ones at the bottom
are the most dangerous tools on the whole server.

## get_status (read)

Where the data lives, whether it is encrypted and unlocked, the plan's
settings revision, and the last backup. **Call it first when another tool
fails for no visible reason** — a locked encrypted store makes every
ledger-reading tool fail, and this is the only tool that still answers.
Unlocking happens at `/unlock` in the web UI, never through a tool.

## list_data_files (read)

The CSV files in the data directory with size and date coverage. Row counts
are **raw and unfiltered** — they will not match `search_transactions`
totals, because dedup, transfer classification, and duplicate resolutions
happen after load. Do not present the difference as data loss.

## list_duplicates (read)

Near-duplicate transaction pairs awaiting review (a scheduled payment
recorded twice, etc.). While a pair is unresolved, **both sides still count
in every spending total** — resolving duplicates is how totals get honest.

Params: `include_resolved` (also return settled pairs).

## resolve_duplicates ✏️ (write → duplicate_decisions.json)

Settles one pair from `list_duplicates`. Two outcomes:

- `kept_winner` — keep one side, exclude the other from every total.
  Requires `kept_hash` (the side to KEEP) and `suppressed_hash` (the side to
  EXCLUDE), both from `list_duplicates`.
- `kept_both` — declare them two genuinely separate payments; both keep
  counting, the pair is just not re-flagged.

Params: `pair_key`, `outcome`, `kept_hash`, `suppressed_hash`.
Copies `duplicate_decisions.json` to a `.bak` before the session's first
change.

## undo_resolve ✏️

Reverses one `resolve_duplicates` decision, putting the pair back in the
review queue: a `kept_winner` undo restores the suppressed transaction; a
`kept_both` undo simply re-flags the pair (nothing was suppressed).

Params: `pair_key` (of an already-resolved pair), and for `kept_winner`
undos `suppressed_hash` — note this is the identity as **persisted** in
`duplicate_decisions.json`, which may be a StableID rather than the legacy
hash `list_duplicates` reported.

## run_backup ✏️ (additive only — the safe one)

Takes one timestamped, verified backup zip of the data directory right now.
It adds a file and changes nothing else, so it is safe to call before any
change the user might want to walk back. Do that liberally.

## list_backups (read)

The backup archives on disk, **newest first** (`archives[0]` is the most
recent), each with name, timestamp, and size. This is the **only sanctioned
source of a name for `restore_backup`** — never construct an archive name.

## restore_backup 🔒 (guarded — the destructive one)

Overwrites the entire data directory from an archive **and deletes every
file the archive does not contain**: CSVs imported since, duplicate
decisions, major-expense definitions, the saved plan. A safety snapshot is
taken first and lands in the backup directory as a new archive — the
pre-restore state is recoverable only by restoring *that* in turn. There is
no undo tool.

Params: `name` (exactly as `list_backups` reported it), `confirm_token`
(bound to this tool AND that one archive name).

Follow the two-call protocol in `guarded-tools.md`. A failed restore says
whether data can have changed: every failure except a write failure leaves
the directory untouched.

## shutdown_server 🔒 (guarded — the terminal one)

Stops the budget2 server. **Not recoverable from inside the session**: after
it runs, every tool stops answering, and nothing here can start the server
again — the user must restart it and then start a new session. There is no
restart; never call this to "restart" anything.

Params: `confirm_token`.
Follow the two-call protocol in `guarded-tools.md`.
