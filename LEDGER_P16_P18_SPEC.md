# P16–P18 — acceptance criteria for the three inherited ledger rows

Written 2026-08-22 by the lead, **after** the code shipped. That is backwards
and is stated plainly rather than disguised: P16, P17 and P18 were implemented
directly in a lead session with no brief, no manifest-bearing dispatch and no
verdict from either lane, and they were merged to `master` before this document
existed. Their ledger rows have carried `pending` ever since.

## Where these criteria come from

Each criterion below is derived from the **claim the commit message makes**,
not from a reading of the implementation. This matters. An oracle backfilled
from the code inherits exactly the assumptions it is supposed to test — the
prohibition recorded in `.swarm/NEXT.md` for P16. A criterion derived from the
author's written claim is different in kind: the claim is a statement about
observable behavior, and a checker can go and find out whether it holds.

Checkers verify against this document. Where a criterion names a command, cite
that command's output as the evidence. Where a criterion says a test must fail
against the pre-fix code, actually revert the hunk in a scratch tree and run it
— a test that passes with and without the fix proves nothing about the fix.

---

## P16 — order the storage read cache against writes

Commits `06176ef`, `79ef006`. Manifest: `internal/services/storage/storage.go`,
`internal/services/storage/cache_test.go`. Ledger tier 3.

The claim: `Storage` could serve stale bytes forever. A read that stat'd a file
just before a rename and published just after installed the OLD payload keyed
to the OLD mtime and size; where mtime is coarse or frozen and the rewrite is
the same length, that entry keeps matching the new file.

1. **The stale-read bug is real and is fixed.** A test asserts a read after
   `WriteFile` returns never observes the pre-write payload, and it fails
   against the pre-fix code — the commit claims "fails on the old code at
   round 1, under `-race`". Verify that claim by reverting the fix in a scratch
   tree and running the test; cite both outputs.
2. **The generation counter is what does the work.** Deleting the `cacheGen++`
   from `invalidateCache` must make a test fail. If every test still passes
   with the bump removed, the counter is decoration and this criterion fails.
3. **Both sides of every mutation invalidate.** Each write path — the atomic
   replace, `CreateExclusive`, and `Remove` — invalidates before the disk
   change and again after it. Name the line for each; a path that invalidates
   only once fails this criterion.
4. **`Lock` bumps the generation too**, so a read that decrypted before the
   lock cannot put plaintext back into the cleared map afterwards
   (`79ef006`'s claim). Drop the bump from `Lock` and
   `TestLockBarsPublishFromReadStartedBeforeLock` must fail on both counts the
   commit names: the entry lands, and the follow-up `ReadFile` then succeeds
   against a locked store.
5. **Uncontended reads still cache.** A test pins that the fix did not degrade
   the cache into a no-op.
6. **Green under the race detector.** `go test -race -count=1
   ./internal/services/storage/...` exits 0.

Note for the second lane: this file was merged by hand on 2026-08-22 (`2a5c068`)
against a conflicting refactor that split encryption into `encodeForWrite`. The
merge resolution is in scope. In particular, ask whether any write path reaches
disk through a code path that skips the generation bump.

## P17 — drop the dead decrypt path in loadIdentity (SA4023)

Commit `7fe9b98`. Manifest: `internal/services/storage/ssh_provider.go`.
Ledger tier 3.

The claim: `decryptSSHKey` is a stub that can only fail, so `loadIdentity`'s
success path was unreachable and its error check always true. Removing the dead
path changes no behavior.

1. **`staticcheck ./...` exits 0**, and this was the repo's last SA4023.
2. **Behavior is unchanged, byte for byte.** The passphrase branch still
   returns exactly: `failed to decrypt SSH key: encrypted SSH keys require
   passphrase decryption support (not yet implemented)`. Assert on the string,
   not on the shape of the error.
3. **The stub and its test are still present.** The commit deliberately did not
   delete them. A checker that finds them gone should FAIL this criterion —
   the scope was the dead path only.
4. **No other caller depended on the removed assignment.** Establish this by
   search, not by assumption.
5. **The suite is green**: `go test ./internal/services/storage/...` exits 0.

## P18 — CI workflow on pull requests

Commits `32c9d1f`, `133d1f5`, `36da2de`. Manifest:
`.github/workflows/test.yml`. Ledger tier 1, checks `tests`.

The claim: before this, `release.yml` fired only on `v*` tags, so a pull
request got an empty combined status that reads as "pending" rather than "not
configured".

1. **The workflow triggers on `pull_request` and on pushes to `master`.**
2. **Three jobs, separately reported**: build/vet/test, staticcheck, race
   detector. The split is the point — a lint failure and a test failure are
   different problems and must not mask each other.
3. **`go test` has an explicit `-timeout` in both test jobs**, and each is
   below its job's `timeout-minutes`. This is `36da2de`'s whole subject: the
   job budget was never the operative limit, because `go test` applies a
   10-minute cap per package. A job whose `-timeout` exceeds its
   `timeout-minutes` fails this criterion — it converts a hung test from a Go
   panic with a stack dump into an opaque job kill.
4. **staticcheck is pinned**, not `@latest`, for the reason `133d1f5` gives.
5. **Go version comes from `go.mod`** via `go-version-file`, not a hardcoded
   pin that only works by accident of the toolchain directive.
6. **It has actually run green on a real pull request.** This is the criterion
   that separates a plausible YAML file from a working one. PR #42 is the
   evidence: cite the run.

---

## What this document does NOT do

It does not make P16 or P17 acceptable at tier 3. `gate.sh check` requires
`.swarm/tier3/<task>/report.md` with a `RESOLUTION:` line — the divergence
report from two blind arms — and no such arms exist or can now be created for
code that is already on `master`. That is an open decision for the user, not
something these criteria resolve.
