# Import fixes — transfer typing + re-export duplicates

Run date: 2026-08-28. Branch: `fix/import-dedup-classifier`.

## Context (evidence from live data)

The aggregator's bank exports overlap: a transaction first appears with
`Category Pending` and its raw bank text, and a later export carries the same
transaction with a *rewritten* Description and an assigned category. The
`Original Description` column is identical across both copies, but the loader
never reads that column, and `ComputeHash` (date|desc|amount) sees different
descriptions — so both copies load.

Two live rows demonstrate the damage (both dated 2026-08-12):

```
Date,Description,Original Description,Category,Amount,Status
2026-08-12,"Lucid BILL PMT                622400A0AHSQ","Lucid BILL PMT                622400A0AHSQ",Category Pending,-1580.43,Posted
2026-08-12,"Lucid Bill a Ahsq","Lucid BILL PMT                622400A0AHSQ",Bills & Utilities,-1580.43,Posted

2026-08-12,"USAA FUNDS TRANSFER CR","USAA FUNDS TRANSFER CR",Category Pending,8571.35,Posted
2026-08-12,"USAA Transfer","USAA FUNDS TRANSFER CR",Transfer,8571.35,Posted
```

- The Lucid car payment is double-counted (+$1,580.43 phantom expense).
- The incoming transfer's renamed copy ("USAA Transfer") misses the
  `"usaa funds transfer"` classifier pattern, and `IsInternalTransfer` ignores
  its `Transfer` category, so it classifies as a positive Outflow — a fake
  $8,571.35 "refund" that erased that much real spending from every total.

Near-duplicate detection cannot catch the Lucid pair: its only candidate shape
is bill-pay ↔ posted-check (`checkPrefixRE`), and neither row is a check.

## Tasks

| Task | Tier | Checks | Scope |
|------|------|--------|-------|
| T12  | 2    | tests,second | `internal/services/classifier/` |
| T13  | 3    | tests,second + oracle | `internal/models/transaction.go`, `internal/services/dataloader/` |

Tasks are independent and touch disjoint files. Neither task may edit files
outside its scope column (tests for the same packages included in scope).

---

## T12 — classifier: honor the Transfer category and the renamed USAA pattern

File: `internal/services/classifier/classifier.go` (+ `classifier_test.go`).

### Changes

1. In `IsInternalTransfer`, the category-based check currently recognizes only
   `credit card payment`. Add `transfer`: when the row's category
   (lowercased, trimmed) equals `"transfer"`, return true. Exact match only —
   do not substring-match (a merchant category like "Balance Transfer Fee"
   must not match; note `Transfers` plural is also NOT matched, by design).
2. Add `"usaa transfer"` to `InternalTransferPatterns`, with the same
   income-keyword escape the other patterns get. (The existing
   `"usaa funds transfer"` entry does not cover it — "funds" sits between.)

Behavior to preserve, verbatim:
- Rows already typed `models.Transfer` are never re-typed (ClassifyTransactions
  skips them).
- The income-keyword escape in `IsInternalTransfer` (positive amount + income
  keyword ⇒ not a transfer) still applies to pattern matches. The category
  check remains unconditional, matching the existing `credit card payment`
  behavior.

### Tests (table-driven, matching the existing style in classifier_test.go)

Must include at least:
- `{Description: "USAA Transfer", Category: "Transfer", Amount: 8571.35}` →
  `IsInternalTransfer` true (category path).
- `{Description: "USAA Transfer", Category: "Category Pending", Amount: 8571.35}`
  → true (new pattern path).
- `{Description: "Balance Transfer Fee", Category: "Fees"}` → false.
- Category `"Transfers"` (plural) → false via category path.
- Regression: an income row like `{Description: "transfer in from savings",
  Amount: 500}` keeps its current classification (income-keyword escape).

### Acceptance criteria

- `go build ./...` passes.
- `go test ./internal/services/classifier/...` passes; new tests fail on the
  pre-change tree (worker demonstrates red → green).
- `go test ./...` passes (no regressions elsewhere).
- Diff confined to `internal/services/classifier/`.

---

## T13 — surface re-export duplicates via Original Description

Files: `internal/models/transaction.go`,
`internal/services/dataloader/loader.go`,
`internal/services/dataloader/near_duplicates.go` (+ tests in those packages).

### Changes

1. **Parse the column.** Add `OriginalDescription string` to
   `models.Transaction` (json `original_description,omitempty`). In the
   loader's column mappings add an optional `"Original Description"` mapping
   (accept at least `Original Description`, `original description`,
   `ORIGINAL DESCRIPTION`). Populate it trimmed; absent column ⇒ empty string.
   `ComputeHash` and `StableIDFor` are NOT changed — row identity must not
   move, or every stored pin/alias/duplicate-decision breaks (see
   stable_id_test.go).
2. **Second near-duplicate candidate shape** in
   `detectNearDuplicatePairs` / `isCandidatePair`: two rows are also a
   candidate pair when ALL of:
   - both are `Outflow` with negative amounts (the existing bucket filter —
     unchanged),
   - same amount in cents (the existing bucket — unchanged),
   - `dayDiff ≤ 1`,
   - both have non-empty `OriginalDescription`, and the two are equal after
     lowercasing and collapsing runs of whitespace to one space.
   This shape is OR-ed with the existing bill-pay↔check shape. Pairing stays
   greedy/deterministic exactly as now (smallest date diff, hash tiebreak);
   pair keys, decisions, undo, and suppression plumbing are reused unchanged.
3. Transfers stay excluded from candidacy
   (`TestLoadData_TransferIsNotANearDuplicateCandidate` must keep passing).

Known accepted consequence: two *genuinely distinct* same-day, same-amount
charges with identical bank text will now surface as a candidate pair once.
That is what the review queue and its persisted `kept_both` decision are for.

Out of scope (do not attempt): deduplicating Transfer-typed re-export copies
(after T12 both copies of the 8/12 USAA row type as Transfer; they are
excluded from spending totals, so the residue is cosmetic on the transfers
page); changing exact dedup or hashing.

### Tests

Required names (the Tier-3 oracle asserts on these exactly):
- `TestDetect_SameDayReimport_LucidPair` — the two Lucid rows above (statuses
  both "Posted", categories differing) are detected as one pair.
- `TestDetect_SameDayReimport_WindowExceeded` — same rows 2 days apart → no
  pair.
- `TestDetect_SameDayReimport_DifferentOriginalDescription` — same
  day/amount, different `OriginalDescription` → no pair.
- `TestDetect_SameDayReimport_EmptyOriginalNotPaired` — one side has empty
  `OriginalDescription` → no pair via the new shape.
- `TestLoadCSVFile_OriginalDescription` — loader populates the field from the
  CSV column; absent column ⇒ empty.
Plus: existing near_duplicates and stable_id tests must pass unmodified
(pair-key order-independence and idempotency hold for the new shape too —
extend those tests rather than duplicating them if natural).

### Acceptance criteria

- `.swarm/tier3/T13/accept.sh` exits 0 (build, named oracle tests, full
  `go test ./...`).
- New detection tests fail on the pre-change tree (red → green shown).
- Diff confined to `internal/models/` and `internal/services/dataloader/`.

---

## Post-merge verification (lead, not workers)

Restart the server against the live data dir and confirm via MCP:
1. `search_transactions` 2026-08-12: the "USAA Transfer" row types as
   Transfer; August expenses no longer net negative.
2. `list_duplicates`: the Lucid 8/12 pair appears unresolved, for the user to
   resolve in the app.

## Rulings

1. T14 attempt 2 (2026-08-28): checker-second FAIL (column-swap mutation
   survives the render tests) vs checker-tests PASS. Panel OVERRULE 3-0
   (judge-claude, judge-standards, judge-impact). Grounds: criterion 4's
   probe list was closed and fully met; the shipped panel is correct in
   rendered output; and the column-identity gap is pre-existing and
   table-wide (swapping the pre-T14 End Portfolio / Lifetime Tax cells also
   survives), so the FAIL held new columns to a bar no existing column meets.
   Follow-up recorded (not a T14 defect): a thead-index helper asserting each
   cell sits under its own header, applied to the whole tax-optimizer table.
