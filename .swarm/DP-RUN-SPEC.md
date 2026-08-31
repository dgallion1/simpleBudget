# DP run — pending→posted duplicate detection (third candidate shape)

Date: 2026-08-30. Lead: agents2 worktree duplicate-transaction-detection-89ef29.
User approval: "yes, build it" (2026-08-30), after lead presented the design.

## Problem

Two live rows for the same card swipe, both counted in every spending total:

```
usaa-credit_2026-04-18_to_2026-05-04.csv:12:
  2026-05-01,"Harbor Freight Tools USA","Harbor Freight Tools USA",Category Pending,188.98,Pending
usaa-credit_2026-05-01_to_2026-07-12.csv:183:
  2026-05-01,"Harbor Freight Tools","HARBOR FREIGHT TOOLS3185 PENFIELD     NY",Home Improvement,188.98,Posted
```

The bank rewrote BOTH Description and Original Description when the pending
charge settled, so neither existing detection shape fires:
shape 1 (bill-pay → `Check #NNN`) — neither side is a check;
shape 2 (same-day re-import) — Original Descriptions differ.

## Task DP1 — third candidate shape in `detectNearDuplicatePairs`

File: `internal/services/dataloader/near_duplicates.go` (+ its test file,
+ the MCP tool description in `internal/services/mcpsvc/admin/duplicates.go`).
Tier: **3** (critical glob `internal/services/dataloader/**`). Checks: tests,second.

### Shape 3 — pending→posted settlement pair

`isCandidatePair(a, b)` gains a third disjunct, `isPendingPostedPair(a, b)`,
true iff ALL of:

1. **Window**: `dayDiff(a.Date, b.Date) <= 3` (`pendingPostedWindowDays = 3`,
   self-enforced inside the predicate exactly as shape 2 self-enforces its
   1-day window; the greedy loop's 7-day window stays untouched).
2. **Status split**: exactly one side is *pending* — its Status contains
   `"pending"` case-insensitively — and the OTHER side is *posted* — its
   Status contains one of the existing `postedStatusKeywords`
   (`posted`, `cleared`, `processed`) and does NOT contain `"pending"`.
   An empty status satisfies neither role (stricter than shapes 1/2 on
   purpose: this shape has no other signal to lean on).
3. **Same account**: `a.AccountID == b.AccountID` (plain string equality;
   two empty AccountIDs are equal, which keeps unit fixtures working).
4. **Description affinity**: normalize both Descriptions (lowercase,
   TrimSpace, collapse whitespace runs to one space — reuse
   `normalizeOriginalDescription`); match iff one normalized string is a
   prefix of the other, OR their longest common prefix is >=
   `pendingPostedPrefixMinLen = 12` bytes.
   ("harbor freight tools usa" vs "harbor freight tools3185 penfield ny"
   share a 20-byte prefix → match; "home depot" vs "harbor freight…" → no.)

Amount/sign/type equality is already guaranteed by the caller's cent bucket.

### Explicitly out of scope

- No change to `pairKey`, `identityKey`, the greedy pairing loop, shapes 1–2,
  `classify`, resolve/undo flows, or any handler.
- No settings UI; constants hardcoded like the existing ones (spec §9 stance).
- Files touched: exactly `near_duplicates.go`, `near_duplicates_test.go`,
  `internal/services/mcpsvc/admin/duplicates.go`.

### Acceptance criteria

A1. The verbatim Harbor Freight fixture (rows above, same AccountID) yields
    exactly one pair; each guard alone (different account, 4-day gap, alien
    description, pending/pending, posted/posted-with-differing-orig-desc)
    yields zero pairs. Unit tests cover every guard in the existing
    table/naming style of `near_duplicates_test.go`.
A2. Real-data regression: loading a copy of the repo `data/` directory
    yields exactly 11 unresolved pairs, each with exactly one Pending side
    and one Posted side, the HF pair (-188.98, 2026-05-01) among them;
    5 resolved, 1 kept_both — i.e. every recorded decision still binds.
    (Amended during oracle calibration 2026-08-30: the pass-end prototype
    revealed 10 further true pending→posted pairs on live data — Amazon
    Prime -4.99, Walgreens -8.13 and -10.00, Tops -15.96, Google Cloud
    -16.17, Spotify -18.99, OpenAI -21.60, Home Depot -32.24, Amazon
    Mktplace -152.40, Wegmans -252.14 — all stale-pending twins with a
    "Category Pending" pending side. The original "exactly 1" expectation
    was the spec's own error, corrected before dispatch.)
A3. `go test ./internal/services/dataloader/ ./internal/services/mcpsvc/...`
    green; no other package's behavior changes.
A4. The `list_duplicates` MCP tool description
    (`internal/services/mcpsvc/admin/duplicates.go`) names the new
    pending→posted shape (must contain the word "pending"); tool COUNT is
    unchanged (no README/skill/server_test drift).
A5. Package doc comments in `near_duplicates.go` enumerate all three shapes.

Oracle: `.swarm/tier3/DP1/accept.sh` asserts A1, A2, A3, A4 mechanically.

## Task DP2 — promote the both-posted oracle guard (Tier 1, checks: tests)

V3 promote-the-probe pattern (checker-tests F2 on DP1): the oracle's
`both-posted-differing-original` guard lives only in the transient staged
test, so promote it into the permanent suite. One new test in
`internal/services/dataloader/near_duplicates_test.go`:
`TestDetect_PendingPosted_NegativeBothPosted` — two Posted outflows, same
account/amount/date, differing OriginalDescription (so shape 2 cannot fire
and the status split is the sole guard), expect 0 pairs. No other file.
Acceptance: test present, styled like neighbors, passes, and fails under a
status-split mutation. DP2 manifest/verdict/ledger paths (`.swarm/*/DP2.*`)
are added to the DP territory list.
Validated at both ends before dispatch (must FAIL on master via A1/A2's
missing pair; guard sub-checks pass on master by design — the overall oracle
still fails on a featureless tree).

## Territories (concurrent-run fence, observed 2026-08-30)

Another session holds uncommitted edits in this checkout. DP workers,
checkers and the lead treat these as FOREIGN — never touch, never blame:

- FOREIGN: `internal/handlers/dashboard/handlers.go`,
  `internal/handlers/dashboard/handlers_test.go`,
  `internal/handlers/dashboard/handlers_http_test.go`,
  `web/static/css/tailwind.css`,
  `web/templates/components/dashboard-verdict-bar.html`
- DP territory (the ONLY writable paths):
  `internal/services/dataloader/near_duplicates.go`,
  `internal/services/dataloader/near_duplicates_test.go`,
  `internal/services/mcpsvc/admin/duplicates.go`,
  `.swarm/DP-RUN-SPEC.md`, `.swarm/tier3/DP1/**`, `.swarm/manifests/DP1.*`,
  `.swarm/verdicts/DP1.*`, ledger row DP1.

Verification is package-scoped (`./internal/services/dataloader/`,
`./internal/services/mcpsvc/...`) — no full-suite demands while the tree is
shared. No git-state changes (checkout/stash/branch/HEAD) by any DP agent.

## Rulings

- 2026-08-30 (lead note, no dispute): worker's flagged deviation accepted —
  `isPendingPostedPair` returns false when either normalized Description is
  empty, mirroring shape 2's empty-OriginalDescription guard. Within intent;
  without it two empty descriptions trivially satisfy the prefix branch.
