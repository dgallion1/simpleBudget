# ND run — near-duplicate detector gaps (undetected settlement pairs)

Date: 2026-08-31. Lead: agents2 worktree duplicate-records-issue-fc48ba.
User approval: "Run it as a swarm task" after the lead's diagnosis of the
May-2026 drill-down showing double-counted rows.

## Problem

Seven real duplicate pairs are live and double-counted (May 2026 alone is
inflated $1,242.89) because `detectNearDuplicatePairs` misses two shapes:

**Gap A — pending→posted affinity tested on the wrong field.**
`isPendingPostedPair` compares `Description` only. USAA's posted exports
prettify the merchant name (`BJS WHOLESALE #0075` → `BJ's Wholesale`,
`GRUBHUB*FIVEGUYS` → `Five Guys via Grubhub`, `BJS MEMBERSHIP` →
`Membership`, `GRAMMARLY CO*UK0GWQL` → `Grammarly.com`), so the 12-byte
prefix rule fails. In every observed miss the pending side's
**OriginalDescription** is a whitespace-normalized prefix of the posted
side's (`BJS WHOLESALE #0075` vs `BJS WHOLESALE #0075 WEBSTER NY`).

**Gap B — a scheduled bill pay that settles as ACH/autopay is invisible.**
The bill-pay shape requires the settled side to match `^Check #\d+`. The
insurance bill pay (`USAA INSURANCE BILL PAYMENT`, status `Recurring
Scheduled Bill Pay`, 5/4, orig desc empty) settled as `USAA Property and
Casualty Insurance` / `USAA P&C AUTOPAY` (Posted, 5/5); Monroe water
settled same-day as `Monroe Water`. No shape fires: not a check, no
"pending" status, empty OriginalDescription.

Confirmed live pairs (both sides counting today): BJ's $634.40 (5/2),
BJ's Membership $64.50 (5/1), Grubhub/Five Guys $59 (5/1→5/2) and $58
(7/12→7/13), Grammarly $144 (4/29), USAA Insurance $464.99 (5/4→5/5),
Monroe Water $20 (5/22). All 16 kept_winner + 1 kept_both decisions in
`duplicate_decisions.json` still bind and must keep binding.

## Task ND1 — extend detection: orig-desc affinity + scheduled→posted shape

Tier: **3** (`internal/services/dataloader/**` is a critical glob).
Checks: tests,second. Worker: worker-coder.

Files (ONLY these, plus manifests):
- internal/services/dataloader/near_duplicates.go
- internal/services/dataloader/near_duplicates_test.go (worker's own tests)
- internal/services/mcpsvc/admin/duplicates.go (list_duplicates tool
  description prose ONLY — it currently claims the only shapes are
  bill-pay-vs-check and pending-vs-posted; mention the scheduled→autopay
  settlement shape)

### Design (fixed — do not redesign)

1. **Gap A.** In `isPendingPostedPair`, when the existing Description
   affinity check fails, apply the SAME affinity rule (HasPrefix either
   way, else `commonPrefixLen ≥ pendingPostedPrefixMinLen`) to
   `normalizeOriginalDescription(a.OriginalDescription)` vs the same for
   b. A side whose normalized OriginalDescription is empty makes the
   orig-desc field pair ineligible (no empty-vs-X matches). Every other
   constraint is unchanged: 3-day window, same AccountID, exact
   pending/posted status split, both statuses non-empty.
2. **Gap B.** New shape `isScheduledSettledPair(a, b)`, checked last in
   `isCandidatePair`:
   - `dayDiff ≤ duplicateWindowDays` (7) and `a.AccountID == b.AccountID`;
   - exactly one side is *scheduled*: status non-empty and (contains
     "scheduled" or contains "bill pay", lowercased); the other side
     satisfies the existing `isPostedStatus`;
   - NEITHER side's Description matches `checkPrefixRE` (check
     settlements stay the exclusive business of the classify() shape —
     keeps existing pairs and their pair_keys byte-identical);
   - token affinity ≥ 2: tokenize each side's `Description` by lowercasing
     and splitting on runs of non-alphanumeric bytes; drop tokens shorter
     than 3 bytes and the stopwords {the, and, for, inc, llc, pay, pmt,
     pmts, bill, payment, autopay, online, recurring, scheduled, check,
     com, www}; require at least 2 shared tokens.
     Calibration: insurance pair shares {usaa, insurance}; Monroe shares
     {monroe, water}; `Wire Fee` vs Monroe scheduled shares 0; scheduled
     `Lucid` vs posted `Lucid Bill …` shares 1 (never pairs — its real
     settlement is the check, shape 1).
3. No other behavior changes: constants, pairKey, greedy pairing,
   classify(), status keyword lists all untouched.

### Acceptance criteria

- A1: the seven verbatim pairs above are each detected in isolation
  (oracle fixtures, exact CSV field values).
- A2: guards — cross-account, 8-day Gap-B window, single-shared-token,
  both-posted, Google One vs Google.com $26.99, Amazon $30.81 pending→
  posted at 4 days (documents the 3-day window staying as-is) — all
  detect 0 pairs; Hyundi scheduled + `Check #996593` still detects 1.
- A3: real data (`data/` copy through the loader): unresolved queue is
  EXACTLY the seven pairs; `ResolvedDuplicates()` == 16;
  `KeptBothDuplicates()` == 1 (decisions keep binding; no re-pairing).
- A4: package suites `./internal/services/dataloader/` and
  `./internal/services/mcpsvc/...` pass.
- A5: `internal/services/mcpsvc/admin/duplicates.go` description mentions
  the autopay/ACH settlement shape (grep-able: "autopay").

### Out of scope (backlog → .swarm/NEXT.md)

- Amazon $30.81 (2025-11-16→11-20): pending→posted at 4 days sits outside
  the 3-day window by design; only one side may still be live. Revisit
  window width only with false-positive data.
- Chateau $35.63 4/29: raw pending+posted rows exist but only one row
  survives load (case-only description difference appears to collapse at
  hash level); not double-counted, nothing to fix.
- The seven new pairs still need the USER to resolve them in the
  Duplicates queue after this lands; detection alone changes no totals.

## Concurrent-run territory

Foreign territory (live or recently active KD session; DO NOT touch, and
checkers attribute rather than FAIL): web/static/js/dashboard.js,
web/templates/components/kpi-month-detail.html,
internal/templates/render_kpi_month_detail_test.go, budget2.old-1345.
ND territory: exactly the three files in Task ND1 plus `.swarm/tier3/ND1/`,
`.swarm/verdicts/ND1.*`, `.swarm/manifests/ND1.*`, this file, the ND1
ledger row. Acceptance stands on package-scope evidence (A4), not a full
repo suite.

## Rulings

(none yet)
