# DP1 attempt 1 — manifest

## Files changed

### internal/services/dataloader/near_duplicates.go
- Added two consts: `pendingPostedWindowDays = 3`, `pendingPostedPrefixMinLen = 12`
  (with doc comments explaining the 3-day settlement-lag rationale and the
  12-byte merchant-fragment rationale).
- Updated the file-top doc comment on `detectNearDuplicatePairs` to enumerate
  all three candidate shapes (bill-pay/check, same-day reimport,
  pending->posted settlement).
- Updated the doc comment on `isCandidatePair` from "either of two shapes"
  to "any of three shapes", and added `isPendingPostedPair(a, b)` as the
  third disjunct (checked after the existing two, unconditionally — matches
  the "self-enforced window" pattern used by `isSameDayReimportPair`).
- Added `isPendingPostedPair(a, b models.Transaction) bool`: enforces the
  3-day window, same-AccountID (plain string equality), an exactly-one-side
  pending/posted status split via two new helpers, and description
  affinity on `Description` (not `OriginalDescription`) via the existing
  `normalizeOriginalDescription` — match iff one normalized string is a
  prefix of the other, or their common byte prefix is >= 12.
- Added `isPendingStatus(status string) bool` and
  `isPostedStatus(status string) bool` helper predicates: each requires a
  non-empty Status; `isPendingStatus` requires "pending" (case-insensitive);
  `isPostedStatus` excludes "pending" and requires one of
  `postedStatusKeywords`. Because the two are mutually exclusive per side by
  construction, `(aPending && bPosted) || (bPending && aPosted)` correctly
  implements the spec's "exactly one side is pending, the other side is
  posted" rule without a separate exactly-one check.
- Added `commonPrefixLen(a, b string) int`, a small byte-wise longest-
  common-prefix helper used only by `isPendingPostedPair`.
- Left `pairKey`, `identityKey`, the greedy pairing loop (including its
  untouched `duplicateWindowDays = 7` loop-window and `sort.Slice`),
  `classify`, and shapes 1-2 exactly as before.

### internal/services/dataloader/near_duplicates_test.go
- Added `makeTxAccount`, a sibling of `makeTx`/`makeTxOriginal` that also
  sets `AccountID`, in the existing helper style.
- Added 7 new tests under a "Third shape: pending->posted settlement pair"
  section, matching the file's existing flat-function naming/table style:
  - `TestDetect_PendingPosted_HarborFreightVerbatim` — the verbatim
    Harbor Freight rows from the defect report, same account, expects 1 pair.
  - `TestDetect_PendingPosted_CrossAccountNoMatch` — different AccountID,
    expects 0.
  - `TestDetect_PendingPosted_WindowExceeded` — 4-day gap, expects 0.
  - `TestDetect_PendingPosted_AlienDescriptionNoMatch` — "Home Depot" vs
    "Harbor Freight Tools", expects 0.
  - `TestDetect_PendingPosted_BothPendingNoMatch` — both sides Pending with
    DIFFERENT OriginalDescription (so shape 2 can't legitimately pair them),
    expects 0.
  - `TestDetect_PendingPosted_PostedEmptyStatusNoMatch` — posted side has
    empty Status, expects 0 (this shape's stricter-than-classify() rule).
  - `TestDetect_PendingPosted_ShortPrefixNoMatch` — "Ab Cdefghij" vs
    "Ab Cdxyzabc", neither a prefix of the other, shared prefix 5 bytes
    (< 12), expects 0.

### internal/services/mcpsvc/admin/duplicates.go
- Extended the `list_duplicates` tool's `Description` string only (no
  schema/name/behavior change): inserted a clause naming the pending->posted
  shape — "...or a Pending charge that re-appears Posted with a rewritten
  description within 3 days..." — between the existing bill-pay/check clause
  and "and are waiting for the user to decide between them." The words
  "pending" and "posted" now co-occur on one description line, matching the
  oracle's A4 grep.

## Commands run

```
go build ./...
go test -count=1 ./internal/services/dataloader/ ./internal/services/mcpsvc/...
bash .swarm/tier3/DP1/accept.sh
```

All green. `go build ./...` produced no output (success). The two-package
`go test` run: all `ok`. Foreign-territory files
(internal/handlers/dashboard/{handlers.go,handlers_test.go,handlers_http_test.go},
web/static/css/tailwind.css,
web/templates/components/dashboard-verdict-bar.html) confirmed untouched via
`git status --porcelain` on those exact paths (empty output). No git-state
commands (checkout/stash/commit/branch/HEAD) were run. The built binary was
never invoked (`go build`/`go test` only).

## Oracle output (tail)

```
== A1/A2: oracle fixture + real-data tests ==
ok  	budget2/internal/services/dataloader	0.017s
== A3: package suites ==
ok  	budget2/internal/services/dataloader	1.461s
ok  	budget2/internal/services/mcpsvc	0.037s
ok  	budget2/internal/services/mcpsvc/admin	4.011s
ok  	budget2/internal/services/mcpsvc/confirm	0.045s
ok  	budget2/internal/services/mcpsvc/curate	0.423s
ok  	budget2/internal/services/mcpsvc/ledger	0.366s
ok  	budget2/internal/services/mcpsvc/plan	5.558s
ok  	budget2/internal/services/mcpsvc/snapshot	0.004s
ok  	budget2/internal/services/mcpsvc/spend	0.720s
== A4: list_duplicates description names the pending→posted shape ==
A4 ok
ORACLE PASS
```

## Deviations from spec

None. One design note not spelled out verbatim in the spec but consistent
with it: `isPendingPostedPair` guards against both normalized Descriptions
being empty (`na == "" || nb == ""` short-circuits to no-match) before doing
the prefix comparison, mirroring `isSameDayReimportPair`'s existing empty-
OriginalDescription guard. Without it two empty descriptions would trivially
satisfy the "one is a prefix of the other" branch (empty string is a prefix
of everything). No test in the spec's guard list or the oracle exercises
this path either way, so it does not affect any acceptance criterion, but
flagging it as a small defensive addition beyond the literal four numbered
rules.
