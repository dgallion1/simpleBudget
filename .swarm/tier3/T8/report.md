# T8 — durability: fsync before rename in the stage-and-publish paths

Tier 3 (storage critical glob), single-arm with dual-lane verification under
the established shape (rulings 2026-08-25a/26d precedent).

## The decision

RULING 2026-08-26g (user, "push it through, I mostly take your advice"):
standing authorization to work the recorded follow-up backlog (T7–T11) on
the lead's judgment with merge-on-green treatment. T8 is the fsync gap both
T5 lanes recorded as pre-existing: none of the three publish paths —
`atomicWrite`, `createExclusive` (storage.go), `saveConfig` (auth.go) —
syncs the staged file before close, and none syncs the directory after the
rename/link, so a poorly timed crash or power loss can lose a write the
caller was told succeeded, or the rename itself. For the encryption config
that is the difference between a recoverable ledger and an unreadable one.

Lead design decisions for the build:
- All three paths gain `File.Sync()` after write, before close; a Sync
  error aborts the publish exactly like a Write error (staging cleaned by
  the existing defers, error surfaced).
- After a successful rename/link, the containing directory is opened and
  synced so the publish itself is durable; a directory-sync error is
  surfaced, not swallowed — the file is in place but the caller must not
  be told the save is durable when it is not.
- Testability comes from small package-level seams (function variables
  wrapping file and directory sync) so tests can record calls and inject
  failures at any uid — the T3 lesson: no chmod fixtures, injections must
  be kernel- or seam-enforced, root-proof.

## Verification outcome (attempt 1)

Primary lane: publish sequence verified line-by-line at all three sites;
grep proves those are the package's only os.Rename/os.Link publishes; 4/4
assigned mutants killed plus a fifth (cleanup-defer removal) proving the
no-leftover assertion bites; package and module suites green as root.
Adversarial lane: no swallowed error found on any branch; wrong-directory
and swallowed-syncDir-error mutants caught; the one surviving mutant
(default fileSync replaced by a no-op) is inherent to seam-based testing —
no unit test can distinguish a real fsync syscall from a no-op — and is
grounded by reading the default seams, which do call Sync; -race suite
clean in 822s; migration.go's bare marker writes confirmed outside scope
and not overclaimed by the new comments. Both lanes PASS.

RESOLUTION: single-arm run under ruling 2026-08-26g (T5/T6 shape) — no
divergence to resolve; both lanes PASS at attempt 1 and the durability
sequence (write → fileSync → close → chmod → publish → syncDir) is proven
mutant-detectable on all three paths.
