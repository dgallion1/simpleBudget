# P17 — Tier-3 report (one arm, no N-version)

**This is not a divergence report**, for the same reason as P16's: `7fe9b98`
was written in a lead session and merged before the row was verified, so no
second blind arm exists or can now be created. User ruling 2026-08-22a applies
— record the gap, do not fabricate an N-version story and do not revert
shipped code to re-run from the top.

P17 reached tier 3 by escalation, not by judgment of its difficulty: its one
touched file, `internal/services/storage/ssh_provider.go`, matches
`internal/services/storage/**` in `.swarm/critical.globs`, and
`escalate-scan` bumps on the glob. The change itself is the removal of a dead
branch that a linter identified.

## What stands in for the oracle

`LEDGER_P16_P18_SPEC.md` § "P17", five criteria derived from the commit's
claims. The load-bearing one is criterion 2: behavior unchanged **byte for
byte**, checked by running the same harness against the pre-fix and post-fix
trees and diffing the output, not by reading the diff and agreeing.

## Arm

| Arm | Source | Result |
|-----|--------|--------|
| primary | `7fe9b98`, as merged | five criteria met, evidenced per criterion |
| second | — | does not exist |

## Divergences

None to review — one arm. `checker-second` (lane `adversarial`) returned FAIL,
but explicitly could not refute the behavioral claim after attacking it; its
FAIL rests entirely on the tier-3 process gap this report exists to record.
Whether that is a valid ground for FAIL once the gap is recorded rather than
concealed is a standards question, and goes to the judge panel.

Both checkers independently noted the same latent fragility, neither counting
it against a criterion: the surviving branch calls
`fmt.Errorf("failed to decrypt SSH key: %w", err)` unconditionally, so if
`decryptSSHKey` were ever implemented to succeed, a nil error would format as
`%!w(<nil>)` instead of the code proceeding. Not a behavior change today — the
stub has a single return and staticcheck confirms it never returns nil — but
worth a comment on the stub before anyone implements it.

RESOLUTION: single arm accepted as the merge base, with N-version deliberately
forgone under user ruling 2026-08-22a; the surviving question is whether the
recorded process gap is itself a valid FAIL, referred to the judge panel.
