# ND3 attempt 2 — manifest (STATUS: DONE)

## Task

Attempt 1 widened `pendingPostedWindowDays` (near_duplicates.go) from 3 to 5
days. The two named checkers found two remaining surfaces still stating the
old 3-day fact:

1. **`internal/services/mcpsvc/admin/duplicates.go`** (adversarial checker
   catch) — the `list_duplicates` MCP tool's `Description` string still said
   the Pending→Posted rewritten-description shape re-appears "within 3
   days". Changed the literal `3 days` to `5 days` in that concatenated
   string; no other wording touched.
2. **`.swarm/NEXT.md`**, ND run backlog (2026-08-31) section, RESOLVED (ND3)
   bullet (primary checker catch) — the bullet said "all 24 recorded
   decisions still bind", conflating recorded (30 entries in
   `data/duplicate_decisions.json`) with bound (24, per the loader). Reworded
   per the lead's exact replacement text to "all 24 loader-bound decisions
   still bind identically ... ; 6 of the 30 recorded entries were already
   unbound at the old window (pre-existing, likely orphaned by the
   accounts-file StableID reassignment) and are unchanged." Rewrapped the
   paragraph to ~72 cols, preserving the two-space continuation indent.

No other files modified. Did not build or run the budget2 binary or any
cmd/ main.

## Evidence

- Oracle run: `.swarm/tier3/ND3/accept.sh 2>&1 | tee .swarm/tier3/ND3/oracle.2.log`
  — checks A-F all passed (constant is 5; dataloader window-pin tests pass;
  dataloader+mcpsvc package suites `ok`; live-data probe finds the single
  genuine 2025-11-16 $30.81 Amazon pair with kept_winner=23/kept_both=1 and
  zero junk; `list_duplicates` description states "within 5 days" and no
  longer states "within 3 days"; NEXT.md no longer contains "all 24 recorded
  decisions still bind" and now contains "already unbound at the old
  window"). Final log line: `ORACLE PASS`. Log path:
  `.swarm/tier3/ND3/oracle.2.log`.
- `go test -count=1 ./internal/services/mcpsvc/...` — all packages `ok`
  (mcpsvc, admin, confirm, curate, ledger, plan, snapshot, spend).
- `git status --short` — modifications confined to
  `internal/services/mcpsvc/admin/duplicates.go`, `.swarm/NEXT.md`, plus the
  pre-existing attempt-1 files (`near_duplicates.go`,
  `near_duplicates_test.go`, `.swarm/ledger.tsv`) and untracked `.swarm/`
  evidence files (flags, manifests, tier3, verdicts) — no other files
  touched.
- No `go build` or `cmd/` binary invocation performed at any point (per hard
  rule).
