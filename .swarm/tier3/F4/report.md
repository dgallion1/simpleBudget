# F4 — migration must not change a file's permission bits

Not a Tier-3 divergence report: under user ruling 2026-08-24b this ran as a
single worker with lead verification, no blind arms and no independence lanes,
because the process cost had stopped matching the stakes. The ledger row says
`merged-unverified` for that reason, and `gate.sh done` will keep reporting it.

## The defect

All four migration helpers published through `s.atomicWrite(path, data, 0644)`
— a hardcoded mode. `atomicWrite` chmods its staging file before renaming, so
the destination's permission bits were replaced. A user who ran `chmod 600` on
their ledger had that undone, silently, by the act of enabling encryption:

    BEFORE EnableEncryption: -rw-------
    AFTER  EnableEncryption: -rw-r--r--

Pre-existing, reachable through the ordinary success path rather than a failure
path, and it fires at the moment the user is actively increasing protection.
Found by `checker-second` while ruling on F2, where it was correctly reported
and correctly not counted against that task.

## The fix

`filePerm(path)` stats the file and returns `info.Mode().Perm()`, threaded into
all four helpers' `atomicWrite` calls. `atomicWrite` keeps its `perm` parameter
and its behaviour for every other caller: `Storage.WriteFile(path, data, perm)`
takes a mode explicitly and is entitled to apply it. This is about migration,
which changes encoding and nothing else.

On a stat failure `filePerm` returns the error rather than defaulting to 0644.
Every call site has just successfully read the same path, so a stat failure
there means concurrent deletion — there is no existing mode to preserve, and a
plausible default is how a permissions fix quietly becomes a permissions bug.

## Verification

Worker: oracle `passed=4 failed=0`; four mutations, one per helper, each caught
by its own named test; `go test -race` green on the unmutated tree.

The tests call each helper **directly** rather than through
`EnableEncryption`/`DisableEncryption`, so a mutation in one helper cannot hide
behind another helper's test. That is the specific failure that cost F3 two
attempts.

Lead: the original measurement rerun — `BEFORE -rw-------`, `AFTER
-rw-------`; one mutation reproduced independently (encrypt helper back to
hardcoded 0644 → `--- FAIL: TestEncryptFileWithRecipientPreservesMode`);
unmutated package suite `ok ... 66.182s`; build and vet clean; and the three
earlier fixes confirmed present — 10 `invalidateCache` sites, 14 `atomicWrite`
sites, zero legacy `.tmp` staging names.

The oracle's fourth check is the one worth keeping: an ordinary 0644 file must
still come back 0644. Without it, "preserve the mode" is satisfiable by
hardcoding 0600 — which would pass every other check and be wrong.

## Out of scope, still open

`atomicWrite` replaces a symlinked destination with a regular file. Reported
alongside the permissions finding and deliberately not bundled with it: whether
the data directory should honour symlinks is a different question from whether
a rewrite should preserve a mode.

RESOLUTION: single-arm run under user ruling 2026-08-24b — there were no blind
arms and hence no divergence to resolve; the sole worker-coder implementation
stands as merged, subject to the two-lane verification whose verdicts accompany
the ledger attempt.

## Attempt 1 two-lane verification (2026-08-24, retroactive) — both lanes FAIL

The retroactive verification this report anticipated was run: checker-tests
(anthropic lane) and checker-second (adversarial lane), verdicts at
`.swarm/verdicts/F4.1.*.verdict`. Both FAIL, concordantly:

- The substance held. checker-tests reproduced the 0600 end-to-end measurement
  with its own probe binary; all four per-helper mutations are caught by their
  own named test and no other; the hardcode-0600 evasion is caught; -race
  clean.
- `filePerm`'s stat-failure contract ("surface the error, never default") was
  claimed deliberate but had zero coverage: a `return 0644, nil` mutant passed
  the entire package suite AND this oracle, in both lanes independently.
- The oracle's race check carried a 900s timeout that a loaded 4-CPU container
  cannot meet even though the suite passes (~1040s, no races) — an environment
  assumption, not a property.

Lead actions on the oracle (recorded, since the oracle is the acceptance
authority): timeout raised to 1800s; planted check `TestOracleF4FilePermStatFailure`
added so the stat-failure contract is graded rather than narrated. Attempt 2
is dispatched as a test-only worker task adding the equivalent in-repo test.

Environment note for future verification: this container runs as root, where
chmod-based fixtures behave differently (the pre-existing F3 test
`TestRollbackDecryptionWithRecipientReportsUnrestorableFiles` fails under
root; proven pre-existing at 90bc39c^). Run suites as an unprivileged user.

## Follow-up resolved (T4, 2026-08-26)

The setgid/sticky observation recorded by checker-second at attempt 1 is
settled by ruling 2026-08-26c: special bits are OUT of `filePerm`'s
contract — migration rewrites only data files, where those bits are
meaningless. The contract is now stated at the function itself. See
.swarm/tier3/T4/report.md.
