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
