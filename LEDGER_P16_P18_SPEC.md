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

---

## Rulings

- **2026-08-22a (user).** P16 and P17 are Tier-3 rows whose code was already
  written and merged before verification, so no blind second arm exists or can
  be created. Rather than fabricate an N-version story or revert shipped,
  CI-green code to re-run from the top, write a one-arm `report.md` that states
  the gap plainly and what it weakens. Recorded in
  `.swarm/tier3/P16/report.md` and `.swarm/tier3/P17/report.md`.

- **2026-08-22b (user).** `cmd_done` requires every row to be `accepted`, so
  R9's honest `no-change` pinned the gate at exit 1 permanently. Teach
  `gate.sh` that `no-change` is terminal — under conditions strict enough that
  it cannot become a way to dodge a FAIL.

- **2026-08-22c (P18, lead).** Accepted at tier 1, attempt 1.
  `checker-tests` PASS on all six criteria. `gate.sh check P18` → `OK: P18
  accepted at tier 1 (attempt 1)`. The checker recorded one observation that is
  outside the criterion as written and so not a failure: `-timeout` is
  per-package, so `12m` does not bound the whole `./...` invocation to under
  the 15-minute job budget. Measured margins on the real run were 4m11s of 15m
  and 14m11s of 45m.

- **2026-08-22d (P16 dispute, judge panel).** `checker-tests` PASS (lane
  `anthropic`), `checker-second` FAIL (lane `adversarial`) on a finding that
  `internal/services/storage/migration.go` rewrites data files in place at
  cached paths without ever calling `invalidateCache`. Panel: `judge-standards`
  OVERRULE, `judge-claude` OVERRULE, `judge-impact` UPHOLD. **Majority
  OVERRULE — accepted at tier 3, attempt 1.**

  The panel is worth reading rather than summarising, because the two overrules
  rest on different grounds and the dissent is not frivolous.
  `judge-standards` held that criterion 3 names its own scope — the atomic
  replace, `CreateExclusive`, `Remove` — and all three are correctly ordered;
  the "note for the second lane" widened where to look, not what the claim was,
  and `migration.go` was untouched by the `2a5c068` merge and predates
  `06176ef` on a separate history. `judge-claude` went further and attacked the
  question directly: it confirmed the stale entry survives (`gen 2->2,
  entryStillInMap=true`) and that the cache-hit fast path checks only
  `(modTime,size)`, but found the step from "stale entry survives" to "stale
  bytes served" unsupported — `checker-second`'s own reproduction substituted a
  bare `os.WriteFile` equal-length rewrite for `migration.go`'s real writer,
  which age framing (a fixed 182-byte floor, +16 per 64KiB chunk) provably
  cannot produce. It then found the case neither checker looked for — an
  Enable→Disable round trip restores the original size and the stranded entry
  IS served — and established that it serves *correct* bytes, because the cache
  holds the logical view while migration changes only the physical encoding.
  `judge-impact` dissented on the ground that a size collision, while unlikely,
  is possible under future implementation changes, and that this application's
  characteristic failure is a confidently wrong number with a green suite.

  **The dissent identified something real, which the majority then re-aimed.**
  The residual defect is confidentiality hygiene, not stale reads: decrypted
  plaintext for a now-encrypted file stays resident in `s.cache` until `Lock()`,
  a write, or exit — the mirror image of what `79ef006` added `Lock`'s
  generation bump to prevent. That is follow-up 1 below, and it is not P16's to
  fix in P16's diff.

- **2026-08-22e (P17 dispute, judge panel).** `checker-tests` PASS,
  `checker-second` FAIL — the FAIL resting entirely on the Tier-3 process gap,
  which `checker-second` stated outright after conceding it could not refute
  the behavioral claim. Panel: `judge-standards`, `judge-claude`,
  `judge-impact` all OVERRULE. **Unanimous — accepted at tier 3, attempt 1.**

  `judge-claude` supplied the argument that generalises: the `report.md` +
  `RESOLUTION:` requirement is enforced independently by `gate.sh:187-192`
  *before* it delegates to `check_tier2`, so overruling bypasses nothing.
  Encoding the same requirement inside a checker verdict double-counts one
  safeguard and routes it into the only channel whose remedy is worker rework —
  for a defect no worker can remedy. A missing oracle bounds how much
  confidence one is entitled to; it is not a finding that the work is wrong.
  It also noted that the constitution itself caps what P17's absent arm could
  have bought ("catches slips and misreadings rather than shared blind spots"),
  and that a pinned non-LLM linter plus a two-tree behavioral diff is stronger
  evidence for an eight-line dead-branch removal than a second model-tier arm.

## Follow-ups this verification deliberately did not absorb

1. **`migration.go` leaves plaintext in the cache.** `EnableEncryptionWithProvider`,
   `DisableEncryption`, `encryptFileWithRecipient`, `decryptFileWithIdentity`
   and `rollbackEncryptionWithIdentity` never call `invalidateCache`. Per
   ruling 2026-08-22d the harm is confidentiality, not stale reads: decrypted
   plaintext for a now-encrypted file stays resident until `Lock()`, a write,
   or exit. Mechanical fix: `invalidateCache` in the five writers. Manifest
   would hit `internal/services/storage/**`, so this is a Tier-3 row — and
   unlike P16 and P17 it can be run properly, oracle first.

2. **`rollbackEncryptionWithIdentity` is not atomic** (`migration.go:300`). It
   uses a bare `os.WriteFile` where both sibling helpers use write-tmp-plus-
   rename, so a concurrent reader can observe a torn file during rollback.
   Found by `judge-claude` while ruling on P16; unrelated to the cache.

3. **The `%!w(<nil>)` trap in `ssh_provider.go`.** The surviving branch calls
   `fmt.Errorf("failed to decrypt SSH key: %w", err)` unconditionally, so a
   future working `decryptSSHKey` returning nil would produce a non-nil error
   reading `failed to decrypt SSH key: %!w(<nil>)` with a nil `Unwrap`, rather
   than the code proceeding. Confirmed on go1.26.3 by `judge-claude`;
   unreachable today. Flagged independently by both checkers and both P17
   judges — four findings of the same latent trap, which is why it is written
   down here instead of being noticed a fifth time. Fix: a warning comment on
   the stub, or restore the `if err != nil` guard when the stub is implemented.

4. **`CreateExclusive`'s post-publish invalidation is untested**
   (`storage.go:452`). Deleting that one line leaves
   `go test -count=1 ./internal/services/storage/` green. The line is present
   and correct, which is all criterion 3 required, but nothing guards it. It
   was added by hand during the `2a5c068` merge, which is precisely the kind of
   line that gets dropped by a later refactor without a test to catch it.

## Amendments the panel recommended

Both standards judges recommended adding text rather than upholding a FAIL —
the same remedy shape as `ACCESSIBILITY.md` point 16 after S4. Neither is
adopted here; both are for the user.

- **A storage/caching standard point** requiring every function that rewrites a
  file at a cached path to invalidate on both sides, naming `migration.go`'s
  helpers explicitly — so that follow-up 1's defect class becomes citable by a
  future checker instead of un-citable now.
- **A Tier-3 clause for inherited rows** in `CLAUDE.md`: where a row is
  escalated onto code already written and merged before the escalation existed,
  skip dispatch, write `report.md` with a `RESOLUTION:` stating single-arm
  status and why, and treat the Tier-2 dual-checker pass on the merged result
  as full verification. This codifies ruling 2026-08-22a so the next inherited
  row does not need a judge panel at all.
