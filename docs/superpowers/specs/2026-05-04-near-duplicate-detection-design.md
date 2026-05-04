# Near-Duplicate Transaction Detection — Design

**Date:** 2026-05-04
**Status:** Approved (brainstorming complete; awaiting implementation plan)
**Triggering case:** A single Lucid car payment appeared as two transactions in the explorer — `2026-03-19 "Lucid" -1580.43` (Status: Scheduled Bill Pay) and `2026-03-20 "Check #996583" -1580.43` (Status: Posted) — because two CSV exports overlapped and captured the same payment in different lifecycle states. Existing hash-based deduplication (`date|description|amount`) cannot detect this because both date and description differ.

## 1. Goals & non-goals

### Goals

- Detect bill-pay → posted-check duplicate pairs introduced by overlapping bank CSV exports.
- Surface candidate pairs visibly in the UI (badge in the explorer + dashboard alert + nav badge).
- Allow the user to soft-suppress one side per pair without losing the underlying transaction record.
- Persist user decisions across reloads so newly-detected pairs require fresh approval but resolved pairs stay resolved.

### Non-goals

- Auto-suppression on detection. Both transactions stay live in totals until the user explicitly resolves a pair.
- Generic same-amount near-duplicate detection. The heuristic is intentionally tight to nail the bill-pay-vs-check pattern only.
- Fuzzy payee matching (`Lucid` vs `LUCID INC.`).
- Batch "resolve all" actions.
- Settings UI for heuristic constants. Constants live in code; revisited only if the heuristic produces noise or misses real cases.

## 2. Detection heuristic (tight, bill-pay ↔ check only)

A pair `(A, B)` is a candidate iff ALL of the following hold:

1. Both are outflows (`Amount < 0`) AND `|A.Amount| == |B.Amount|`.
2. `|A.Date − B.Date| ≤ 7 days`.
3. Exactly one side is a **scheduled bill pay** AND the other is a **posted check**:
   - **Scheduled bill pay:** `Status` contains "Scheduled" or "Pending" (case-insensitive) OR description does NOT match the check-prefix regex below.
   - **Posted check:** description matches `(?i)^check\s*#\s*\d+\b` (case-insensitive prefix match, after trim) AND (`Status` contains "Posted" OR `Status` is empty).

The regex is anchored at the start but not the end so it tolerates banks that append text after the check number (e.g. `Check #996583 cleared`). Whitespace tolerance covers `Check#996583`, `CHECK # 996583`, etc.

### One-pair-per-transaction rule

A transaction can appear in at most one candidate pair. If multiple matches are possible:

1. Prefer the pair with the smallest date difference.
2. Tie-break by lexicographically smaller partner hash for determinism.

### Why tight

The only false-positive class possible under this rule is "you wrote a check the same week you scheduled a bill-pay for the same exact amount to a different payee" — vanishingly rare. The heuristic can be loosened later if needed; tightening after users have grown accustomed to noisy badges is harder.

## 3. Data model changes

### `Transaction` struct additions (`internal/models/transaction.go`)

| Field | Type | Source | Purpose |
|-------|------|--------|---------|
| `Status` | `string` | Parsed from CSV `Status` column at load time | Used by the heuristic; rendered in the review panel |
| `Suppressed` | `bool` | Derived during load | True iff this hash is the suppressed side of a `kept_winner` decision |
| `DuplicatePairKey` | `string` | Derived during load | Non-empty iff this transaction is part of an unresolved candidate pair |

The CSV column-mapping system already includes `Status` aliases; the loader currently reads but discards the value. No mapping changes needed.

### Pair key

Order-independent, deterministic:

```
pairKey = sha256(min(hashA, hashB) + "|" + max(hashA, hashB))[:16]
```

The same pair always produces the same key regardless of detection order.

## 4. Persistence: `data/duplicate_decisions.json`

Schema:

```json
{
  "decisions": {
    "<pairKey>": {
      "kept_hash":       "<hash of kept transaction>",
      "suppressed_hash": "<hash of suppressed transaction, empty when outcome=kept_both>",
      "outcome":         "kept_winner | kept_both",
      "decided_at":      "2026-05-04T10:30:00Z"
    }
  }
}
```

**Outcomes:**

- `kept_winner`: the user picked one side; `suppressed_hash` is excluded from totals/charts/major-expense matching.
- `kept_both`: the user marked the pair "not actually a duplicate"; both transactions stay live, but the pair will not be re-flagged on future loads.

### Persistence layer

New file `internal/services/dataloader/duplicate_decisions.go`, mirroring the shape of `transaction_pins.go`:

- `LoadDuplicateDecisions() (map[string]Decision, error)`
- `SaveDuplicateDecision(pairKey string, decision Decision) error`
- `ClearDuplicateDecision(pairKey string) error`

Atomic write (write to `*.tmp`, then rename) like the existing pin store.

## 5. Load pipeline integration

In `LoadAll`, after the existing `deduplicateTransactions(...)` step:

```
1. detected := detectNearDuplicatePairs(transactions)
2. decisions, _ := dl.LoadDuplicateDecisions()
3. for each pair in detected:
       if decision exists for pair.key:
           if outcome == "kept_winner":
               transactions[suppressed].Suppressed = true
           // kept_both: no-op; pair stays unresolved-free, both stay live
       else:
           transactions[A].DuplicatePairKey = pair.key
           transactions[B].DuplicatePairKey = pair.key
4. dl.UnresolvedDuplicateCount = count of unresolved pairs
```

### Suppression boundary

Introduce a single helper:

```go
func (dl *DataLoader) ActiveTransactions() []models.Transaction
```

…that returns the slice with `Suppressed == true` filtered out.

Aggregation call sites that switch from raw slice to `ActiveTransactions()`:

- Dashboard handlers (`internal/handlers/dashboard/handlers.go`)
- Insights handlers (`internal/handlers/insights/handlers.go`)
- What-if income/expense classification
- Major-expense matching (`internal/services/dataloader/major_expense_names.go`)

The explorer keeps using the raw slice and renders badges based on `Suppressed` / `DuplicatePairKey`.

## 6. UI surface

### Top nav

Add a "Duplicates" link rendered with a count badge: `Duplicates (3)`. The link is hidden entirely when `UnresolvedDuplicateCount == 0`.

### Dashboard alert card

In `web/templates/components/alerts.html`, render an alert when `UnresolvedDuplicateCount > 0`:

> "3 possible duplicate pairs need review → Review"

The alert links to `/duplicates`. Disappears when count is zero.

### Review panel route

New handler at `/duplicates` (file: `internal/handlers/duplicates/handlers.go`).

**Layout — two tabs:**

**Unresolved (default):** one card per candidate pair, displaying both transactions side-by-side. Each card shows date, description, amount, source CSV file, and the `Status` value, plus three action buttons:

- `Keep left` — writes `outcome=kept_winner` with `suppressed_hash = right.Hash`
- `Keep right` — writes `outcome=kept_winner` with `suppressed_hash = left.Hash`
- `Both real (stop flagging)` — writes `outcome=kept_both` with empty `suppressed_hash`

After action, the card disappears from the unresolved tab; if `kept_winner`, it reappears in the suppressed tab.

**Suppressed:** list of resolved `kept_winner` decisions, showing kept and suppressed sides plus an `Undo` button that calls `ClearDuplicateDecision`. After undo, the pair returns to the unresolved tab on next load.

### Explorer integration

- Transactions with `DuplicatePairKey != ""` (unresolved partner): render `🔁 dup?` badge linking to `/duplicates#<pairKey>` (anchor scrolls to the relevant card).
- Transactions with `Suppressed == true`: render `🚫 suppressed dup` badge with faded/strikethrough styling and an inline `Undo` link calling `ClearDuplicateDecision`.

## 7. Error handling

| Condition | Behavior |
|-----------|----------|
| `duplicate_decisions.json` missing | Treat as empty map; do not error |
| `duplicate_decisions.json` corrupt | Log warning; treat as empty; do not block load |
| Decision references hashes that no longer exist (CSV deleted) | Keep decision silently — CSV may return on next import |
| Pair detection runs on every load | Idempotent; same input → same pair keys → same outcomes apply |
| Two pair candidates share a transaction | Resolved by tie-break rules in §2 |

## 8. Testing

### Unit tests — `internal/services/dataloader/detect_near_duplicates_test.go`

Synthetic transaction fixtures covering:

- Positive: Lucid 3/19 (Scheduled Bill Pay) + Check #996583 3/20 (Posted) → one pair detected.
- Negative: same pattern but 8 days apart → no pair.
- Negative: same amount, both checks → no pair.
- Negative: same amount, both bill-pays (no check pattern) → no pair.
- Negative: opposite signs (income + outflow) → no pair.
- Negative: status mismatch (e.g., both "Posted") → no pair.
- Triplet: three same-amount transactions inside the window with mixed statuses → exactly one pair (closest date wins; tie-break by hash).
- Idempotency: re-running detection produces identical pair keys.

### Persistence tests — `internal/services/dataloader/duplicate_decisions_test.go`

Mirror the structure of `transaction_pins_test.go`:

- Load missing file → empty map, no error.
- Load corrupt file → empty map, warning logged.
- Save decision → loadable round-trip.
- Clear decision → key absent on reload.
- Atomic write: simulated failure leaves prior file intact.

### Integration test — `internal/services/dataloader/loader_test.go`

Fixture CSVs containing one bill-pay-vs-check pair → load → expect:

- One entry in `detected` pairs.
- Both transactions tagged with `DuplicatePairKey`.
- After saving a `kept_winner` decision and reloading: one transaction has `Suppressed=true`, neither has `DuplicatePairKey`.
- After saving `kept_both`: neither has `DuplicatePairKey`, neither has `Suppressed`.

### Handler tests — `internal/handlers/duplicates/handlers_test.go`

- `GET /duplicates` renders unresolved tab with expected pair cards.
- `POST /duplicates/resolve` with `kept_winner` writes decision and 303s back.
- `POST /duplicates/resolve` with `kept_both` writes decision and 303s back.
- `POST /duplicates/undo` clears decision and 303s back.
- Bad pair key → 404.

### Smoke / dogfood

- Browser smoke test (Playwright): nav badge appears, dashboard alert appears, review panel renders both tabs, resolve and undo update the page.

## 9. Out of scope (explicit)

- Different-amount duplicate detection (e.g., currency conversion fee differences).
- Fuzzy payee matching (`Lucid` vs `LUCID INC.`).
- Batch "resolve all unresolved" / "always pick the Posted side" automation.
- A settings UI for the heuristic — `windowDays = 7`, the `^Check #\d+$` regex, and the Status keyword sets are all package-level constants.
- Detection of duplicates *within a single CSV file* (the existing exact-hash dedup already covers that case).

## 10. Known limitations (acceptable trade-offs, not bugs)

- **No account isolation.** The heuristic runs against the unified transaction stream and does not filter by source account or source CSV file. A bill-pay in account A and a same-amount posted check in account B within 7 days would be flagged as a candidate pair. Given the conjunction of constraints (same exact amount, ≤ 7 day window, status-pattern match, and a check from a *different* account) this is rare enough to accept as a false-positive class the user can dismiss via "Both real (stop flagging)".
- **No batch dismissal.** Each flagged pair must be resolved individually. If a CSV cleanup produces a flood of new candidates, the user resolves them one at a time.
- **Decisions referencing missing hashes are kept indefinitely.** A garbage-collection pass for dead decisions is deferred.

## 11. Open questions

None at design time. Implementation may surface edge cases that warrant follow-ups.
