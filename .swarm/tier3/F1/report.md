# Tier 3 divergence report — F1

| worktree | oracle exit |
|----------|-------------|
| wt-primary | 0 |
| wt-alt     | 0 |

## No behavioral divergence
```
CHECK 01-migration-cache-residency: PASS
CHECK 02-package-suite-race: PASS
CHECK 03-build: PASS
CHECK 04-vet: PASS
---
passed=4 failed=0
```

<!-- Boss: after adjudicating, append a line starting 'RESOLUTION:' -->

## Comparison beyond the oracle

Both arms scored 4/4 and the oracle found no divergence. That is the third time
in this run's history that an identical oracle score sat on top of materially
different implementations, so the S2 lesson applies again: read both.

**wt-primary wins.** Three differences, and one of them is a defect.

1. **wt-alt skips invalidation when the rollback write fails.** It added a
   `continue` after `os.WriteFile`'s error branch in
   `rollbackEncryptionWithIdentity`, so a failed write returns with the stale
   entry still cached — at exactly the moment the file's on-disk state is
   unproven. That inverts the rule this package already documents on
   `writeFileLocked`: "Unconditional -- a failed write leaves the file's state
   unproven, and re-reading is cheaper than guessing." It is also an
   error-handling change the brief prohibited.
2. **wt-alt brackets rollback on one side only**, where wt-primary brackets all
   three data-file helpers consistently.
3. **The comments carry different reasoning.** wt-primary names this as "the
   confidentiality half of invalidateCache ... not the staleness half", which
   is the distinction ruling 2026-08-22d turned on. wt-alt writes "This is what
   orders the cache against the write ... barred from installing them" — the
   staleness framing, copied from `writeFileLocked`, and the reasoning the
   panel specifically ruled out for this path. A correct line with a wrong
   justification survives until someone refactors on the justification.

**wt-alt found the thing wt-primary missed**, and it is not cosmetic. wt-alt
invalidates *before* `encryptData`/`decryptData`; wt-primary invalidates after
the transform, immediately before the disk write. On every success path the two
are indistinguishable — which is why the oracle scored them identically and why
neither arm's own tests separated them. They differ only when the transform
fails, and there the later placement returns with the plaintext still resident,
which is the exact condition this task exists to remove, arriving at the moment
the migration is going wrong.

## Merged result

wt-primary's `migration.go` and its five tests, with wt-alt's earlier placement
grafted into all three helpers, plus one test the lead added to defend that
graft: `TestEncryptFailureDoesNotLeavePlaintextCached`, driving the otherwise
unreachable error path with a stub `age.Recipient` whose `Wrap` always fails.
Mutation-confirmed — moving the call back below `encryptData` fails that test
and nothing else in the suite.

Oracle on the merged tree: 4/4, including the package suite under `-race`.

### Disclosure

The oracle was corrected once, before either arm was graded and while both were
still implementing: check 02 ran the package suite with the oracle's own planted
fixtures still in the package, so it measured them rather than the arm's tests,
and it reported FAIL against an unmodified tree for a reason unrelated to the
tree. Fixed by removing the planted file before that check, then re-validated
at both ends. Unlike the attempt-3 amendment on `gate-no-change`, nothing had
been graded when this changed.

### Out of scope, still open

`rollbackEncryptionWithIdentity` still uses a bare non-atomic `os.WriteFile`
where its siblings use write-tmp-plus-rename. Both helpers still stage at the
fixed name `path + ".tmp"` — the legacy collision-prone form that
`StagingSuffix` and `IsStagingName` exist to replace. Both were named as out of
scope in the brief and both arms correctly left them alone.

RESOLUTION: wt-primary merged as the base on bracketing, error-path
unconditionality and comment accuracy; wt-alt's pre-transform invalidation
grafted into all three helpers and defended by a new error-path test; the
rollback `continue` rejected.

## Attempt 2 rejected — the graft was defended in one helper of three

`checker-tests` PASS, `checker-second` FAIL. The lead reproduced it rather than
convening a panel (ruling 2026-08-16g): reintroducing the losing arm's
placement in `decryptFileWithIdentity` left the entire package suite green.

The behaviour was correct on all three helpers; the coverage was not. The
lead grafted the pre-transform placement into all three and shipped a test for
one — having written, in the same breath, that "the graft needs a test, or I'm
adding an undefended line". `checker-second` also confirmed the harder angles
held: no aliasing or backup residency vector (deleting the map entry really
does make the plaintext unreachable), no bypass paths outside these helpers, no
concurrency hole, and the verify/marker exclusion correct.

**The oracle was extended before dispatch** with checks 05-07, which grade
*detection* rather than behaviour: each copies the tree, moves one helper's
`invalidateCache` below its fallible transform, and requires the package suite
to notice. A green suite under mutation is a failed check. Validated as
discriminating first — 05 passed, 06 and 07 failed on the attempt-2 tree.

This is the third time the same gap appeared in this run's history: P16's
untested `CreateExclusive` invalidation, the losing arm's ten tests in a file
`run_tests.sh` never enumerates, and now this. Three instances is a property of
the codebase, not luck, which is why it is now in the oracle rather than left
to a checker to notice a fourth time.

## Attempt 3 — accepted

Test-only; `migration.go` untouched. Added
`TestDecryptFailureDoesNotLeavePlaintextCached` and
`TestRollbackEncryptionWithDecryptFailureEvictsCachedPlaintext`.

Oracle `passed=7 failed=0`. Both lanes PASS. `gate.sh check F1` -> `OK: F1
accepted at tier 3 (attempt 3)`.

`checker-tests` re-derived the mutations itself on six scratch copies (move and
delete, all three helpers), got exactly one `--- FAIL` per mutant on the content
assertion, confirmed via probe instrumentation that the failure was the intended
one rather than an early return, and used a `publishCache` no-op mutant to prove
the preconditions fatal instead of passing vacuously.

`checker-second`, which broke attempts 2 and 3 of the previous task and attempt
2 of this one, could not break it: it reproduced the defect class on isolated
copies with no oracle involvement and built its own probe to rule out the
vacuous-pass hypothesis.

### Recorded against the lead's own process

- **No immutable reference for "unchanged".** `checker-tests` could not verify
  point 1 byte-for-byte, because the attempt-2 merge was never committed and the
  Tier-3 worktrees hold the *arms'* files. It fell back to three proxies. Commit
  the merged Tier-3 result before dispatching verification, so the next
  "unchanged since attempt N" is a hash comparison.
- **Preconditions assert presence, not content.** Both new tests check that an
  entry exists before the failing call, not that it holds the plaintext.
  `checker-tests` verified the content half independently. Tightening them would
  remove a way for these tests to rot into vacuity later.

### Out of scope, still open

- `rollbackEncryptionWithIdentity` still writes with a bare non-atomic
  `os.WriteFile`.
- Both helpers still stage at the fixed name `path + ".tmp"` -- the legacy
  collision-prone form `StagingSuffix` and `IsStagingName` exist to replace.
- **New, found by `checker-second` while ruling on attempt 3:**
  `DisableEncryption`'s decrypt loop has no rollback call on partial failure,
  unlike the encrypt path. Present in pristine `404fe1a`, so pre-existing.
