# T3 — root-proof the five chmod-based test fixtures

Not a Tier-3 divergence report: RULING 2026-08-25a (user, in session) applied
the 2026-08-24c precedent — this test-only task runs as a single worker with
the full dual-lane verification, no blind arms, because two model-tier arms
add nothing to fixture rework. The task sits at Tier 3 because one fixture
lives under the `internal/services/storage/**` critical glob.

## The defect class

Five tests inject failures via chmod (0000 files, 0555 directories), which
`CAP_DAC_OVERRIDE` lets root bypass — so under root (this verification
container, any root CI) the tests fail outright or, worse, their mutations go
undetected. A sixth test skips itself at uid 0 for the same reason.
Inventory and history: NEXT.md "Environment facts #1"; the class was
discovered across F3/F4/Q1/T1 verification.

## The fix pattern

Replace DAC-based injection with kernel-enforced failures no uid bypasses:
EISDIR (operate on a path that is a directory), ENOTEMPTY (os.Remove on a
non-empty directory), ENOENT (dangling symlink), ENAMETOOLONG (NAME_MAX,
the F3 precedent). Each test keeps its original assertions and must still
kill its original mutation — at uid 0 AND at an ordinary uid.

RESOLUTION: single-arm run under ruling 2026-08-25a — no blind arms and hence
no divergence to resolve; the sole worker-coder implementation stands as
built, subject to the two-lane verification whose verdicts accompany the
ledger attempt.

## Attempt 1 two-lane verification — split verdict; scope enlarged

checker-tests PASS: the seven reworked tests are sound — all six mutants
killed with errno-probe injection precision, green at both uids, race-stable.
checker-second FAIL, on the criterion as written: "full module `go test
./...` green AS ROOT" is false — 49 more tests across 6 packages
(cmd/enrich-amazon 1, internal/config 1, handlers/backup 1,
handlers/majorexpenses 8, handlers/whatif 30, services/retirement 8) fail
under root with the same chmod-fixture defect class, none previously
inventoried. The finding is correct and the criterion stands as written.

RULING 2026-08-26a (user): attempt 2 extends the same kernel-enforced
injection rework to all 49, keeping the criterion. Same single-arm shape as
ruling 2026-08-25a. The 30 whatif and 8 retirement failures are expected to
share fixture helpers — fix helpers, not call sites, where possible.

## Attempt 2 — adversarial FAIL: wrong-reason defect in the save helpers

The full-module criterion was genuinely met (green at uid 0 and uid 65534,
both lanes' own runs), but checker-second mechanically confirmed a
wrong-reason defect: `makeSaveFail` (whatif) and `corruptSettingsDirToFile`
(retirement) inject ENOTDIR at the settings DIRECTORY, which fails
`loadInternal`'s MkdirAll before `saveInternal` ever runs — a
saveInternal-error-swallowing mutant survives undetected in ~26 of the
reworked tests. Green, but no longer defending the branch the test names
claim. Worker B's uid-conditional bind-mount mechanism (majorexpenses) was
attacked and survived: reads succeed, writes fail EROFS, root cannot bypass
a read-only mount, and non-root keeps plain chmod.

RULING 2026-08-26b (user standing authorization, "push it through" /
small-mechanical carve-out): attempt 3, test-only — replace the two helpers'
ENOTDIR injection with the proven bind-mount/chmod mechanism so loads
succeed and saves fail, then re-verify both lanes. The previously surviving
saveInternal mutant becomes a mandatory kill.
