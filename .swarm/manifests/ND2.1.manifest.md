# ND2 attempt 1 — manifest (STATUS: DONE)

## Task
ND2 — promote two ND1 checker probes into committed regression tests in
`internal/services/dataloader/near_duplicates_test.go`:
1. `TestDetect_ScheduledSettled_ExactlyOneSharedTokenNoMatch` — kills the
   `sharedTokenCount(...) >= 2` → `>= 1` mutation in `isScheduledSettledPair`
   (Lucid / Lucid Bill Payment pair, exactly one shared token: "lucid").
2. `TestDetect_ScheduledSettled_CheckPrefixExclusionNoMatch` — kills removal
   of the `checkPrefixRE` exclusion block in `isScheduledSettledPair`
   ("Check #12345 Monroe Water" / "Monroe Water" pair, 2 shared tokens,
   every other condition holds, only the exclusion keeps it unpaired).

## Files changed
- internal/services/dataloader/near_duplicates_test.go (only file modified)

`near_duplicates.go` was temporarily mutated twice to prove each new test
kills its mutation, then reverted both times. Confirmed byte-identical to
its pre-task state (see Verification).

## Verification

### Full suite, clean tree
```
$ go build ./...
Go build: Success

$ go test -count=1 -v -run TestDetect ./internal/services/dataloader/
Go test: 35 passed in 1 packages
```
(35/35 PASS, includes all pre-existing tests plus the 2 new ones; no
existing test modified or weakened.)

### Mutation-kill proof 1 — sharedTokenCount(...) >= 2 → >= 1
Hand-edited `isScheduledSettledPair`'s final `return` line from `>= 2` to
`>= 1`, then ran only the new test:

```
$ go test -count=1 -v -run TestDetect_ScheduledSettled_ExactlyOneSharedTokenNoMatch ./internal/services/dataloader/
=== RUN   TestDetect_ScheduledSettled_ExactlyOneSharedTokenNoMatch
    near_duplicates_test.go:446: expected 0 pairs (exactly 1 shared token, below the >=2 threshold), got 1
--- FAIL: TestDetect_ScheduledSettled_ExactlyOneSharedTokenNoMatch (0.00s)
FAIL
FAIL	budget2/internal/services/dataloader	0.003s
FAIL
```
Test fails under the mutation, as required (the old fixture,
TestDetect_ScheduledSettled_SingleSharedTokenNoMatch, shares zero tokens
and would NOT have caught this — confirmed by inspection, not re-run,
since that pre-existing test is out of scope to touch).
Reverted `>= 1` back to `>= 2` immediately after.

### Mutation-kill proof 2 — checkPrefixRE exclusion removed
Hand-removed the `if checkPrefixRE.MatchString(...) { return false }` block
from `isScheduledSettledPair`, then ran only the new test:

```
$ go test -count=1 -v -run TestDetect_ScheduledSettled_CheckPrefixExclusionNoMatch ./internal/services/dataloader/
=== RUN   TestDetect_ScheduledSettled_CheckPrefixExclusionNoMatch
    near_duplicates_test.go:464: expected 0 pairs (checkPrefixRE exclusion), got 1
--- FAIL: TestDetect_ScheduledSettled_CheckPrefixExclusionNoMatch (0.00s)
FAIL
FAIL	budget2/internal/services/dataloader	0.003s
FAIL
```
Test fails under the mutation, as required. Reverted the exclusion block
back immediately after.

### near_duplicates.go left untouched
```
$ git diff --stat internal/services/dataloader/near_duplicates.go
internal/services/dataloader/near_duplicates.go | 131 ++++++++++++++++++++++--
 1 file changed, 125 insertions(+), 6 deletions(-)
```
This diff is entirely pre-existing (uncommitted ND1 work already in the
working tree before this task started — this task never touched HEAD).
Confirmed the two edited functions (isScheduledSettledPair's `>= 2` line
and the checkPrefixRE exclusion block, lines ~283-300) read byte-identical
after both mutate/revert cycles to the initial `Read` taken at the start of
this task — no net change was introduced by ND2.

### Full-tree scope check
```
$ git status --short
 M .swarm/NEXT.md
 M .swarm/ledger.tsv
 M internal/services/dataloader/near_duplicates.go       <- pre-existing (ND1), not touched net
 M internal/services/dataloader/near_duplicates_test.go  <- ND2's only change
 M internal/services/mcpsvc/admin/duplicates.go
 M web/static/js/dashboard.js
 web/templates/components/kpi-month-detail.html
?? .swarm/ND-RUN-SPEC.md
?? .swarm/manifests/ND1.1.files
?? .swarm/manifests/ND1.1.manifest.md
?? .swarm/tier3/ND1/
?? .swarm/verdicts/ND1.1.checker-second.verdict
?? .swarm/verdicts/ND1.1.checker-tests.verdict
?? budget2.old-1345
?? internal/templates/render_kpi_month_detail_test.go
```
All other listed files are ND1/foreign-territory/pre-existing state, not
touched by this task.

## Notes
- No git commands other than read-only `git diff --stat` / `git status
  --short` were run.
- Compiled `budget2` binary was never run.
