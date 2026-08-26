# T4 — filePerm's contract: special permission bits are out of scope

Not a Tier-3 divergence report: doc-only task, run single-arm with dual-lane
verification under the same shape as rulings 2026-08-25a/26a/26b. It sits at
Tier 3 only because the comment being edited lives in
internal/services/storage/** (critical glob).

## The decision

RULING 2026-08-26c (user): `filePerm` (internal/services/storage/migration.go)
uses `Mode().Perm()`, which strips setuid/setgid/sticky. F4's adversarial
checker recorded this as scope-arguable against the commit title "must not
change a file's permission bits". The user ruled the bits OUT of contract:
migration only ever rewrites data files (CSVs, JSON sidecars) — never
executables or directories, the only places special bits mean anything — so
preserving them would defend a scenario with no practical path to occur,
at the cost of touching critical storage code and new per-bit test surface.

## The change

Comment-only: `filePerm`'s doc comment states the contract explicitly (the
0777 permission bits are preserved; special bits deliberately are not, and
why). This report and the F4 report carry the decision record. No behavior
change, no test change.

RESOLUTION: single-arm doc-only run under ruling 2026-08-26c — no divergence
to resolve; verification confirms the diff is comment-only and the storage
package's behavior is byte-for-byte unchanged.
