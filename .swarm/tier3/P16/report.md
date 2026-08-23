# P16 — Tier-3 report (one arm, no N-version)

**This is not a divergence report.** There was no second blind arm, and none
can now be created: the implementation was written directly in a lead session
and merged to `master` (`06176ef`, `79ef006`, via PR #32) long before this row
was verified. Two blind arms require dispatch before implementation. That
window is closed.

User ruling 2026-08-22a: write this report honestly rather than either
fabricating an N-version story or reverting shipped, CI-green code to re-run
the task from the top. The cost is recorded below rather than hidden.

## What stands in for the oracle

`LEDGER_P16_P18_SPEC.md` § "P16", six criteria derived from the claims the
commit messages make — not from a reading of the implementation. Three of the
six are mutation experiments (revert the fix; delete `cacheGen++` from
`invalidateCache`; delete it from `Lock`), each requiring the named test to
fail without the change. A criterion that only passes is weak evidence; a
criterion that also fails when the mechanism is removed is not.

## What this does and does not buy

Bought: the mechanism is load-bearing, demonstrated three ways. Not bought:
N-version's actual product, which is two independent readings of the same brief
disagreeing about something neither author thought to question. A post-hoc
oracle, however carefully derived, is written by someone who has already seen
the answer. Weigh the acceptance accordingly.

## Arm

| Arm | Source | Result |
|-----|--------|--------|
| primary | `06176ef` + `79ef006`, as merged and hand-resolved at `2a5c068` | six criteria met, evidenced per criterion |
| second | — | does not exist |

## Divergences

None to review — there is one arm. The contested material is in the verdicts,
not between arms: `checker-second` (lane `adversarial`) returned FAIL on a
defect in `internal/services/storage/migration.go`, a file outside P16's
manifest. `checker-tests` (lane `anthropic`) found the same code and judged it
out of scope and not exploitable, because age encryption changes file size and
so the cached `mtime`+`size` key cannot match. That disagreement is a genuine
dispute and goes to the three judges, not to this report.

RESOLUTION: single arm accepted as the merge base, with N-version deliberately
forgone under user ruling 2026-08-22a; the surviving question is the
migration.go finding, referred to the judge panel.
