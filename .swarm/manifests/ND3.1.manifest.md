# ND3 attempt 1 — manifest (STATUS: DONE)

## Task

Widen `pendingPostedWindowDays` from 3 to 5 days so the genuine 4-day-lag
pending→posted settlement (Amazon Mktplace -30.81, Nov 2025 — ND run
backlog, .swarm/NEXT.md) is detected. This resurrects the single piece of
the abandoned DP3 commit (9c5b120) not superseded by ND's ad63180, with
DP3's calibration rationale adapted into the constant's doc comment.

Implemented by the lead under the 2026-08-31 lean exception (small,
well-specified, Tier 2). Worktree: .claude/worktrees/nd3-window, branch
fix/nd3-pending-posted-window off origin/master 42e7224.

## Changes

- `internal/services/dataloader/near_duplicates.go`
  - `pendingPostedWindowDays` 3 → 5; doc comment rewritten with the live
    calibration rationale (weekend/holiday settlement lag reaches 4 days
    on live data; 5 gives a day of margin; the status-split, same-account
    and description-affinity guards carry the false-positive load).
  - `isPendingPostedPair` doc comment: "window is 3 days" → "5 days".
- `internal/services/dataloader/near_duplicates_test.go`
  - `TestDetect_PendingPosted_WindowExceeded`: negative case moved from
    4 days (now inside the window) to 6 days.
  - NEW `TestDetect_PendingPosted_WindowBoundaryFiveDays`: 5-day pair must
    match — with the 6-day negative this mutation-pins the constant at
    exactly 5.
  - NEW `TestDetect_PendingPosted_FourDayLag_AmazonVerbatim`: the verbatim
    2025-11 Amazon rows (pending 2025-11-16 / posted 2025-11-20, -30.81)
    must produce exactly one pair.
- `.swarm/NEXT.md`: ND backlog bullet for this pair marked RESOLVED.

## Evidence

- `go test -count=1 ./internal/services/dataloader/` → ok (full package).
- Live-data probe (ND1-style, copies the live data dir into t.TempDir()):
  - `.swarm/work/ND3/zz_probe_nd3_test.go` — staged into the package for
    each run, removed after; `ND3_DATA_DIR` overrides the data location.
  - `.swarm/work/ND3/probe.baseline.log` — run at window=3 BEFORE the
    change: unresolved queue EMPTY, kept_winner=23, kept_both=1.
    (The ND-era 16+1 figure grew: the user resolved the seven ND pairs.)
  - `.swarm/work/ND3/probe.after.log` — run at window=5 AFTER the change:
    unresolved queue is EXACTLY [2025-11-16|30.81], kept_winner=23,
    kept_both=1. Zero junk pairs admitted; every recorded decision binds.
