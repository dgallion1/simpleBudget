# Accounts & Transfers — Design

Date: 2026-08-16
Status: approved in conversation; build contract in `ACCOUNTS_TRANSFERS_SPEC.md`

## Problem

SimpleBudget has no Account concept and no Transfer type. Internal transfers
are handled by a load-time **drop filter** (`classifier.InternalTransferPatterns`
+ `MajorExpense.IsInternalTransfer`): matching rows vanish, non-matching
transfer rows silently double-count — the debit leg inflates Total Expenses and
the credit leg is usually classified Income (`IncomeKeywords` contains
"transfer in", "deposit"), corrupting savings rate in both directions. Neither
dedup layer can catch this: exact-hash needs identical date/description, and
near-duplicate pairing indexes outflows only.

The user moves money Schwab → USAA to keep checking funded and wants:

1. **Correct metrics** — transfers never pollute income/expenses, without
   depending on description substrings.
2. **Visible flows** — transfers as first-class events, not silently dropped.
3. **Checking-funding help** — "will checking run low, how much should I move?"
4. **Account awareness** — every transaction belongs to an account.

Decided constraints: one CSV file = one account; balances via user-entered
anchors rolled forward from transactions; stay file-based (no DB — see
"Rejected alternatives").

## 1. Data model

### Account (new)

`internal/models/account.go`, persisted in sidecar `data/accounts.json` through
`internal/services/storage` (encryption-transparent, like every other sidecar).

```go
type AccountKind string // "checking" | "savings" | "brokerage" | "credit" | "other"

type BalanceAnchor struct {
    Date   time.Time // balance as of END of this day
    Amount float64   // bank convention (positive = money you have)
    Note   string
}

type Account struct {
    ID                   string   // slug, e.g. "usaa-checking"; referenced by transactions & decisions
    Name                 string   // "USAA Checking"
    Institution          string   // "USAA"
    Kind                 AccountKind
    FilePatterns         []string // matched (case-insensitive glob) against CSV basename; first match wins
    Anchors              []BalanceAnchor // kept sorted by Date
    LowBalanceThreshold  float64  // 0 = use default (500); only meaningful for Kind checking/savings
    CreatedAt, UpdatedAt time.Time
}
```

Validation: IDs unique, non-empty Name, no two accounts whose FilePatterns
match the same existing file (checked at save time; warning surfaced, first
match still wins deterministically by account-ID sort order).

### Transaction (extended)

`internal/models/transaction.go` gains:

```go
AccountID       string // "" = unassigned (file matched no account)
TransferPairKey string // shared by exactly two legs of one paired transfer
TransferClass   string // "paired" | "external" | "" (non-transfer)
```

`TransactionType` gains a third value `Transfer` alongside `Income`/`Outflow`.
`GLOSSARY.md` is updated **before** any code lands (it currently declares
"There is no Transfer type").

### StableID (the identity hardening)

Problem being fixed: the durable key for all sidecar decisions is
`sha256(date|lower(desc)|amount)[:8]`. A bank reformatting descriptions (or
Amazon enrichment changing display text upstream of a future refactor) orphans
every decision keyed on the old hash.

```
StableID = accountID | YYYY-MM-DD | amount-in-cents | n
```

where `n` is the occurrence index (0-based) among rows with identical
(accountID, date, amount-cents), counted in file row order. Description is out
of the identity entirely.

- Amount is the **post-sign-normalization** value (after credit-card
  convention flip), so the ID is stable regardless of the heuristic's input.
- Collisions (several same-amount rows, one account, one day) rely on export
  row order being stable across re-exports. True for append-style bank
  exports; documented limitation in GLOSSARY.
- Unassigned transactions (no account) use `file:<basename>` in the accountID
  slot — usable but not durable across renames; decisions on unassigned rows
  are allowed but the UI nudges toward assigning the file to an account first.

**Lazy migration**: every sidecar keyed by transaction identity
(`transaction_pins.json`, `duplicate_decisions.json`, `amazon_enrichment.json`,
new `transfer_decisions.json`) looks up by StableID first, falls back to the
legacy content hash, and rewrites entries to StableID on next save. The legacy
`Hash` field remains on Transaction for the fallback path. No one-shot
migration; nothing breaks if a sidecar is never re-saved.

## 2. Account attribution (loader)

In `internal/services/dataloader/loader.go`:

- Load `accounts.json`; for each CSV file, match basename against every
  account's FilePatterns (case-insensitive `path.Match` plus plain substring
  fallback). First match (accounts sorted by ID) stamps `AccountID` on all
  rows from that file.
- Unmatched files load normally with `AccountID: ""` and are counted;
  dashboard and explorer show a dismissible-but-recurring banner "N
  transactions in M files not assigned to any account" linking to the
  accounts settings page. Never a silent pass-through.
- Sign convention: `Kind == "credit"` **forces** credit-card convention for
  the file, overriding the ≥70%-positive heuristic
  (`usesCreditCardSignConvention`). Other kinds leave the heuristic alone.

### New pipeline order (`LoadDataContext`)

```
parse → sign-flip (heuristic, credit-kind override) → account stamp
      → StableID assignment → exact dedup
      → TRANSFER CLASSIFICATION (new — replaces filterInternalTransfers)
      → income/outflow classification (skips Transfer rows)
      → near-dup detection (unchanged; transfers are no longer outflows,
        so they drop out of its index automatically)
      → aliases → major-expense stamp → amazon enrichment → derived fields
```

Exact dedup runs before transfer classification so duplicate rows can't create
phantom pair candidates.

## 3. Transfer classification

New `internal/services/transfers` package. Inputs: post-dedup transaction set,
accounts, `MajorExpense` list, persisted decisions. Three outcomes:

1. **Auto-pair.** Candidate pair = opposite-sign rows, equal amount-in-cents,
   **different** AccountIDs, dates within **±4 days**, neither leg already
   paired, and **at least one leg matches** `InternalTransferPatterns` or an
   `IsInternalTransfer` MajorExpense. Unique candidate → both legs become
   `Type: Transfer`, `TransferClass: "paired"`, `TransferPairKey =
   sha256(stableID_a + "|" + stableID_b)[:12]` with legs ordered
   lexicographically. Multiple candidates → closest date wins; exact tie →
   suspected queue.
   *Rationale for the pattern gate: pure amount+window matching false-positives
   on coincidental equal amounts (a $60 deposit and $60 debit in the same
   week). Pattern-less candidates are suggested, never auto-paired.*
2. **Suspected (review queue).** Cross-account opposite-sign amount matches
   within the window with no pattern hit, or ambiguous ties. Surfaced in a
   review UI modeled on the existing near-duplicate resolution flow. User
   confirms (→ paired, as above) or rejects (→ never re-suggested).
   Decisions persist in `data/transfer_decisions.json`:
   `{pairKey, stableIDs [2], verdict: "confirm"|"reject", decidedAt}`.
3. **External leg.** Unpaired rows matching patterns / `IsInternalTransfer`
   MajorExpenses (Vanguard, Coinbase — counterparty CSV not loaded) →
   `Type: Transfer`, `TransferClass: "external"`. These are the rows the old
   filter dropped; now they're visible.

Metrics need no formula change: `metrics.Calculate` filters by type, and
`Transfer` is neither Income nor Outflow. `FilteredTransfers()` count is
replaced by real Transfer rows; the loader keeps a compatibility count for the
existing UI until the Transfers page ships.

Confirmed decisions on rows that later disappear (CSV re-export shrinks) are
retained but inert; a `validate` pass (existing `cmd/validate`) reports
decisions whose StableIDs no longer resolve.

## 4. Balances & funding forecast

New `internal/services/accounts` service.

- `BalanceAt(accountID, date)` = latest anchor with `anchor.Date <= date`
  plus the sum of that account's transaction amounts in `(anchor.Date, date]`.
  No anchor → balance unavailable (UI shows "set an anchor", not $0).
- **Freshness**: each account reports its latest transaction date ("data
  through Aug 12"). A stale account's balance renders with an explicit
  staleness warning — a stale CSV must not masquerade as a healthy balance.
- **Drift check**: when consecutive anchors exist, report predicted-vs-actual
  at the later anchor (signals missing rows between exports).
- **Projection** (checking/savings kinds): roll current balance forward 35
  days, applying expected recurring items (dates + median amounts from the
  existing recurring engine used by `get_recurring`) for this account only.
  If the projected balance crosses below the account's threshold
  (`LowBalanceThreshold`, default $500): report first crossing date, minimum
  projected balance, suggested top-up = shortfall rounded up to the nearest
  $100, and the median of confirmed inbound paired transfers as a reference
  ("you usually move $2,000"). Advisory only — never writes to the ledger.

## 5. UI

Server-rendered templates + HTMX, matching existing pages. All new/touched
pages conform to `ACCESSIBILITY.md` (new, numbered standard).

- **Accounts settings** (`/accounts`, `internal/handlers/accounts`): CRUD for
  accounts, file-pattern editor showing which existing CSVs each pattern
  currently matches, anchor entry ("balance was $4,210 on Aug 1"), threshold.
- **Transfers page** (`/transfers`, `internal/handlers/transfers`): monthly
  flow chart between institutions (Plotly, with data-table fallback),
  transfer history (paired + external), review queue for suspected pairs
  with confirm/reject.
- **Dashboard**: Accounts card — per-account balance, freshness, low-balance
  flag (icon + text, not color-only); checking card links to projection
  detail. Unassigned-files banner (also on explorer).

## 6. MCP

`internal/services/mcpsvc`, new group `accounts/` plus additions to existing
groups, following current tool conventions:

- `get_accounts` — accounts, balances, freshness, low-balance flags.
- `get_balance_projection` — projection detail for one account.
- `get_transfers` — search/summarize transfer flows (institution↔institution,
  date range, class).
- `set_balance_anchor` — mutating; uses the existing `confirm/` token flow.
- `resolve_transfer` — confirm/reject a suspected pair; confirm-token flow.
- Server description (`server.go`) updated: it currently advertises that
  transfer questions are unanswerable — that clause is removed and replaced
  with the new capability summary. GLOSSARY.md is the semantic authority.

## 7. Testing

Fixture CSVs under `testdata/` (synthetic `schwab-checking.csv`,
`usaa-checking.csv`, `usaa-credit.csv`) covering:

- clean Schwab→USAA pair (auto-pair)
- pattern-less equal-amount coincidence (must go to queue, never auto-pair)
- ambiguous tie (two candidates, equal date distance → queue)
- external leg (Vanguard, no counterparty file)
- StableID occurrence collisions (two same-amount rows, one day)
- legacy-hash fallback + rewrite-on-save migration
- anchor roll-forward, multi-anchor drift, no-anchor case
- credit-kind sign override vs heuristic
- unassigned-file counting

Unit tests per package in the existing `coverage_test.go` style; TDD per task.
`gopls`/LSP call-graph checks before editing shared symbols per repo CLAUDE.md.

## Rejected alternatives

- **Pairing-only overlay (no accounts)** — fixes double-counting but delivers
  none of the balance/flow/funding goals.
- **SQLite double-entry ledger** — abandons the stateless reparse-from-CSV
  design and forces a storage/encryption migration nothing here needs. The
  planned trigger to revisit: the day bank export-history limits make the CSV
  directory the only copy of old data. The B data model (accounts, Transfer
  type, decisions) carries over to that future cleanly; only the loading
  layer would change.

## Known limitations (accepted)

- StableID occurrence index depends on export row order for same-day
  same-amount collisions within one account.
- Same-amount transfers that split/merge (send $1,000, receive $500 twice)
  are not paired; they surface as suspected/unmatched.
- Projection quality is bounded by the recurring engine's detection; it is
  advisory and clearly labeled as such.
