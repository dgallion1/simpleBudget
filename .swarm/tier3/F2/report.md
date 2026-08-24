# Tier 3 divergence report — F2

| worktree | oracle exit |
|----------|-------------|
| wt-primary | 0 |
| wt-alt     | 0 |

## No behavioral divergence
```
CHECK 01-atomic-publish-and-no-leftovers: PASS
CHECK 02-no-legacy-fixed-staging-name: PASS
CHECK 03-f1-properties-intact: PASS
CHECK 04-package-suite-race: PASS
CHECK 05-build: PASS
CHECK 06-vet: PASS
---
passed=6 failed=0
```

<!-- Boss: after adjudicating, append a line starting 'RESOLUTION:' -->

## Comparison beyond the oracle

Both arms scored 6/6 with no divergence. That is now the fourth Tier-3
comparison in this repo's history where an identical oracle score sat on top of
materially different implementations, so the review happens by reading.

**wt-primary wins, decisively and structurally.**

`rollbackEncryptionWithIdentity`'s publish became one line — `s.atomicWrite(path,
decrypted, 0644)` — reusing the routine `storage.go` already provides, which
stages under `StagingSuffix`, chmods, renames, and cleans up via `defer`.
Applying the same reuse to the other two helpers made the file *shorter*: 345
lines against the pre-change 348.

wt-alt hand-rolled the staging sequence — CreateTemp, Write, Close, Chmod,
Rename, with explicit cleanup and a log line per step — in each of the three
helpers, taking the file to 411 lines. It used the `StagingSuffix` constant
rather than a literal, so it is not the worst version of this, but it is three
copies of a routine that exists once and is already tested. `storage.go`'s own
comment on `StagingSuffix` makes the argument against exactly this: a consumer
should derive from the single definition, so the two "cannot drift apart because
there is only one definition". wt-alt's copies inherit nothing `atomicWrite`
gains later.

wt-alt also added `s.invalidateCache(path)` before each early `continue`.
Harmless but redundant: nothing was written on those paths, and the
pre-transform invalidation already covers them.

Nothing from wt-alt is grafted. That is a legitimate outcome of "pick a winner
or synthesize" and is recorded as such rather than manufacturing a graft to
make the comparison look productive.

**The two arms agreed on both judgement calls the brief deliberately left
open**, by different routes: neither widened
`rollbackEncryptionWithIdentity`'s signature to report failures, both reasoning
that its two call sites are already inside error paths returning to their own
caller, and that a signature change exceeds this task. Independent agreement on
an open question is worth more than either argument alone. The silent-failure
behaviour therefore stands, deliberately, and is noted below.

**One disclosure wt-primary volunteered**, which matches the standard this run
holds others to: its `TestMigrationRoundTripLeavesNoStagingFiles` is **not**
independently mutation-discriminating — no portable mutation of this diff makes
it fail alone, because every reachable path either fails before a staging file
exists or renames it away. It was kept as a regression guard against a future
refactor that forgets cleanup, with properties 1 and 3 carrying the adversarial
weight. Recorded rather than quietly counted as coverage.

Safety check the lead ran before accepting the reuse: `atomicWrite` acquires no
locks, so calling it from inside `EnableEncryptionWithProvider` /
`DisableEncryption`, which hold `s.mu` exclusively for their whole run, cannot
deadlock. That was the one way "reuse the existing routine" could have been the
wrong answer.

## Merged result

wt-primary's `migration.go` and `migration_atomic_test.go`, unmodified. Oracle
on the merged tree: 6/6, including the package suite under `-race` and F1's
properties intact.

### Still open, deliberately

- `rollbackEncryptionWithIdentity` still reports failures only to the log. Both
  arms judged the signature change out of scope; the lead agrees. If it is ever
  taken up, the argument for it is that `EnableEncryption` returns "failed to
  encrypt <file>" whether or not the rollback then succeeded, so a caller cannot
  distinguish "restored" from "partially encrypted store".
- `DisableEncryption`'s decrypt loop still has no rollback on partial failure,
  unlike the encrypt path. That is task F3.

RESOLUTION: wt-primary merged unmodified; it fixes the defect by reusing
`atomicWrite` rather than reimplementing it, and removes three lines net where
wt-alt added sixty-three. No graft from wt-alt.

## Attempt 2 — accepted, both lanes PASS

`gate.sh check F2` -> `OK: F2 accepted at tier 3 (attempt 2)`.

### The permission finding, and why it did not fail the task

`checker-second` found that routing rollback through `atomicWrite` changes the
published file's permissions: `atomicWrite` chmods its staging file to the
`perm` argument unconditionally, so a destination at `0600` comes back `0644`,
where the pristine bare `os.WriteFile` onto an *existing* file left its mode
alone. It also replaces a symlinked destination with a regular file. Verified
by the lead: `MODE AFTER ROLLBACK: -rw-r--r-- (was 0600)`.

It reported this and declined to fail the task on it, which was correct on both
counts. The lead then checked reachability, and the finding does not reach
production:

`rollbackEncryptionWithIdentity` only ever runs over `filesToEncrypt`, i.e.
files `encryptFileWithRecipient` has already processed. That helper has *always*
written its staging file at `0644` and renamed — in the pristine code as much as
now. Measured on a real `EnableEncryption`:

    BEFORE EnableEncryption: -rw-------
    AFTER  EnableEncryption: -rw-r--r--

So by the time rollback sees a file it will rewrite, that file is already
`0644`, and F2 cannot have regressed it. Files the encrypt loop had not reached
are skipped by rollback's `isAgeEncrypted` guard, untouched.

**The underlying behaviour is real and pre-existing**: migration silently makes
a user's hand-set `0600` financial data world-readable, and has always done so.
That is a confidentiality issue of the same family as F1 and F3, it is not F2's,
and it is recorded below rather than folded in.

### Recorded against the lead's own oracle

`checker-tests` established that **check 02's structural grep is unsound in both
directions**: `grep -qE '\+ "\.tmp"'` is evaded by spelling the name through a
variable (`suffix := legacyStagingSuffix`), and it fires on prose in comments.
It can pass on a tree that violates the property and fail on one that does not.
Property 3 passes on the worker's per-helper behavioural sentinel test, not on
this check. The check was left in place during verification rather than amended
mid-run — amending between the two lanes would have moved the bar for one and
not the other — and F3's oracle was written with no structural greps at all as
the direct consequence.

`checker-tests` also **falsified the winning arm's own disclosure**: it built a
mutation, shaped like what wt-alt actually wrote, under which
`TestMigrationRoundTripLeavesNoStagingFiles` was the only failure in the
package. The arm was right to disclose and wrong on the merits. The real gap is
narrower: a staging file created above an `isAgeEncrypted` early return would
leak on the skip path with the suite still green. `checker-second` traced every
skip path in the shipped code and found none reachable.

### Follow-ups, not absorbed

- **Migration forces `0644` on every file it rewrites**, discarding a
  hand-set `0600`, and replaces a symlinked destination with a regular file.
  Pre-existing, reachable through `EnableEncryption`, and a confidentiality
  issue on financial data. Its own row.
- `rollbackEncryptionWithIdentity` still reports failures only to the log.
  `checker-tests`' read: acceptable as shipped, and this fix defused most of it,
  since the one reachable cause of a silently swallowed failure was a
  non-writable destination and rename publishing removes it. What remains is
  directory-level failure, under which the encrypt that triggered the rollback
  would itself have failed the same way.
