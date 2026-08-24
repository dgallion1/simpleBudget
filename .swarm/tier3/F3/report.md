# Tier 3 divergence report — F3

| worktree | oracle exit |
|----------|-------------|
| wt-primary | 0 |
| wt-alt     | 1 |

## Divergence
### wt-primary output
```
CHECK 01-partial-failure-behaviour: PASS
CHECK 02-f1-oracle-still-green: PASS
---
CHECK 03-f2-oracle-still-green: PASS
---
CHECK 05-build: PASS
CHECK 06-vet: PASS
---
passed=5 failed=0
```
### wt-alt output
```
CHECK 01-partial-failure-behaviour: PASS
CHECK 02-f1-oracle-still-green: FAIL (want 0, got 1)
---
CHECK 03-f2-oracle-still-green: FAIL (want 0, got 1)
---
CHECK 05-build: FAIL (want 0, got 1)
CHECK 06-vet: FAIL (want 0, got 1)
---
passed=1 failed=4
```
### diff (primary vs alt)
```diff
2c2
< CHECK 02-f1-oracle-still-green: PASS
---
> CHECK 02-f1-oracle-still-green: FAIL (want 0, got 1)
4c4
< CHECK 03-f2-oracle-still-green: PASS
---
> CHECK 03-f2-oracle-still-green: FAIL (want 0, got 1)
6,7c6,7
< CHECK 05-build: PASS
< CHECK 06-vet: PASS
---
> CHECK 05-build: FAIL (want 0, got 1)
> CHECK 06-vet: FAIL (want 0, got 1)
9c9
< passed=5 failed=0
---
> passed=1 failed=4
```

<!-- Boss: after adjudicating, append a line starting 'RESOLUTION:' -->

## Comparison — the first mechanical divergence of the run

Four earlier Tier-3 comparisons in this repo scored both arms identically while
the implementations differed materially. This one did not, and the reason is
worth recording: the difference here was **mechanical**, not structural.

**wt-alt's tree does not compile.** It left a scratch file at the repository
root, `test_mutation.go`, declaring `package storage` where the root package is
`budget2`, plus a `FIX_SUMMARY.txt`. `go build ./...` and `go vet ./...` fail
for the whole module, which cascaded into F1's and F2's nested oracles.
`passed=1 failed=4` against wt-primary's `passed=5 failed=0`.

Its report claimed `go build ./... — Success`. That claim was false for the
state it left behind; its own package-scoped `go test
./internal/services/storage/` never touched the broken root package. Build and
vet are the cheapest checks in any oracle and read like filler until the day
they are the only thing standing between a worker's summary and the truth.

**wt-primary wins on the implementation too**, independently of the build:

1. **wt-alt's helper takes a dead `identity` parameter.** Re-encryption needs
   only a recipient; `identity` is never referenced in the body. A dead
   parameter in a security-relevant helper invites a future caller to believe
   it matters.
2. **wt-alt has no nothing-decrypted-yet case.** When the very first file fails,
   it still calls the rollback with an empty slice and appends "(files
   re-encrypted; encrypted state restored)" to the error — claiming an action
   it did not take. wt-primary returns the original error unchanged.
3. **The error text.** wt-primary names the count, lists the files, states they
   "remain PLAINTEXT ON DISK despite encryption still being reported as
   enabled", and tells the user to back them up and address them manually.
   wt-alt emits "(restore failed: [...])". For a message whose entire job is to
   tell someone their financial data is exposed, that difference is the feature.

wt-primary also declined to reuse `rollbackEncryptionWithIdentity`'s shape, on
the grounds that its void signature is precisely the silent-swallow defect F2
had just removed — reusing it one level down would have rebuilt the same
problem. That is the reasoning the brief hoped for and did not supply.

**Grafted from wt-alt:** full paths rather than basenames in the unrestored
list. wt-primary used `filepath.Base`, and the message it feeds asks the user to
act on files it does not locate. Defended by
`TestRollbackDecryptionReportsFullPaths`.

## Disclosure — the lead broke the merge, and the nested oracles caught it

The graft changed the helper's return values, and wt-primary already had a test
asserting the old ones. That test was not updated, so the merged tree failed:

    TestRollbackDecryptionWithRecipientReportsUnrestorableFiles
        expected failed=[unreadable.csv], got [/tmp/.../unreadable.csv]

The process error was specific. After grafting, the new test was run alone, and
then the full suite was run **only on the mutated tree**, where exactly one
failure appeared as predicted — which felt like confirmation. The full suite was
never run on the unmutated tree. One end validated, the other taken on faith:
the same error this run has flagged in three separate briefs about oracles, made
by the lead about its own change.

It was caught because F3's oracle nests F1's and F2's oracles, in the window
between the arms finishing and the checkers being dispatched — the least
observed point in the process, where the lead edits code no checker has yet
seen. Both assertions now defend the graft; reverting to basenames fails both.

**The cost, stated rather than hidden:** nesting two oracles that each run a
`-race` suite makes this oracle take roughly fifteen minutes, doubled by
`tier3-compare` and paid again on every verification attempt. A cheaper design
would have run F1's and F2's behavioural tests without their race suites. The
expensive version earned its keep on its first real exercise, but that was not
guaranteed when it was written.

RESOLUTION: wt-primary merged on correctness, error legibility and the absence
of a dead parameter; wt-alt disqualified mechanically by a non-compiling tree
and beaten on the merits regardless; its full-path reporting grafted in and
defended by two assertions after the lead's own merge error was caught by the
nested regression guards.

## Attempt 3 — HALTED at the hard stop

`checker-tests` PASS, `checker-second` FAIL. Not sent to a judge panel: the lead
reproduced the finding mechanically (ruling 2026-08-16g).

`checker-tests`' PASS was thorough and settled the question it was asked. It
rebuilt the naive post-hoc probe itself and confirmed cache contents are
identical on both trees, then found the one simpler discriminator nobody had
mentioned -- the private `cacheGen` counter, 2 against 1 -- and killed it with a
third mutation (invalidate moved to just after `encryptData` but before the
error check, leaving plaintext resident for the whole transform): `genDelta=2`,
identical to the correct tree, so a counter-based test would pass a tree with
the defect fully present. The blocking-recipient machinery is necessary, not
decoration. It also pinned "no production change" by line numbers recorded in
the attempt-2 verdict, working around the absence of a committed reference.

**`checker-second` failed it on the same pattern that sank attempt 2, in the
same helper attempt 3 was written to defend.** `rollbackDecryptionWithRecipient`
has three failure branches -- `ReadFile` fails, `encryptData` fails,
`atomicWrite` fails -- and only the first has a test. Mutating either of the
other two to swallow its error silently leaves the whole package suite green.
Reproduced by the lead: dropping the `failed = append(failed, path)` from the
`encryptData` branch gives `ok budget2/internal/services/storage 59.453s`.

What makes this decisive rather than pedantic is that both branches are
**trivially constructible with tools already in the file**: the
`encryptData`-failure fixture needs the `failingRecipient{}` type that sits nine
lines away in the same test file, and the `atomicWrite`-failure fixture needs
the directory-chmod technique `migration_atomic_test.go` already documents. This
is not the infeasible gap that was honestly disclosed and accepted -- it is two
feasible tests that were not written, in the helper this attempt existed to
cover.

**Second failed attempt at Tier 3, so the task halts to the user** rather than
looping into attempt 4. The production code is correct and both lanes agree on
that; what is missing is the defence.

### Also recorded, from `checker-tests`

The known gap's reasoning lives only in verdict files. A future reader of
`migration.go` will not find it. If F3 is accepted, that reasoning belongs at
the branch itself or in the spec.

## Attempt 4 — accepted, under a reduced verification the user authorised

User ruling 2026-08-24a lifted the hard stop for one scoped, test-only attempt.
User ruling 2026-08-24b then cut the verification process for the remainder of
this work, on the ground that its cost had stopped matching the stakes.

**What was done.** Two tests, both constructed with tools already in the
package, exactly as the brief anticipated:
`TestRollbackDecryptionReportsPathOnEncryptFailure` (using the existing
`failingRecipient{}`) and `TestRollbackDecryptionReportsPathOnAtomicWriteFailure`
(chmodding the containing *directory* to 0500, since `atomicWrite` publishes by
rename and a read-only destination file would not stop it). Plus a comment at
the `PLAINTEXT ON DISK` branch recording why it has no end-to-end test, so the
reasoning stops living only in verdict files.

Worker's oracle run: `passed=9 failed=0`, with checks 09 and 10 flipping from
FAIL to PASS as predicted.

**What verification was NOT done, and why it matters when reading this row.**
No `checker-tests` and no `checker-second` verdict exists for attempt 4. The
gate was not run for this attempt. Acceptance rests on the worker's oracle run
plus the lead's own targeted checks:

- code-only diff against the winning Tier-3 arm, confirming attempt 4 added no
  production code (only the lead's earlier graft differs)
- mutation 09 reproduced: `--- FAIL: TestRollbackDecryptionReportsPathOnEncryptFailure`
- mutation 10 reproduced: `--- FAIL: TestRollbackDecryptionReportsPathOnAtomicWriteFailure`
- unmutated package suite `ok ... 60.691s`; build, vet, gofmt clean

Each mutation kills exactly the test that claims the branch. What was skipped is
the nested F1/F2 oracle chain, which re-grades properties accepted earlier today
and unchanged since, and both independence lanes.

**The lead's misjudgement, recorded because the ledger should not imply a rigour
that was not applied.** Tier 3 with blind arms, dual lanes and judge panels was
proportionate to transfers double-counting money, to a gate that printed "no
unresolved flags" over an unresolved flag, and to plaintext left resident after
encryption. It was not proportionate to adding two tests to an error branch in a
rollback helper. Each F3 oracle run cost about twenty-four minutes — two nested
full oracles plus four whole-suite mutation runs — and was paid three or more
times per attempt, with the two checkers serialised on top. The individual steps
were each defensible; continuing to run them at full weight after the stakes
dropped was not.

### Ledger status: `merged-unverified`, not `accepted`

The lead first wrote `accepted` and the gate refused it — `FAIL: F3: no
verdicts for attempt 4` — which is the gate working exactly as intended, on the
lead.

The constitution permits `accepted` only after `gate.sh check` exits 0. Marking
this row accepted without lane verdicts would be the same act this run refused
for P16 and P17: a ledger claiming evidence that does not exist. The reduced
verification was authorised; pretending it was the full one was not.

So the row carries `merged-unverified`. `gate.sh done` will report it for as
long as it stands, which is the correct outcome — the work is merged and the
lead's own checks are recorded above, and anyone reading the ledger can see
exactly which verification was and was not performed. If the two lanes are ever
run against this attempt, the row can move to `accepted` on their evidence.
