# GLOSSARY — the semantic contract

Canonical definitions for every term budget2 computes with. The dashboard, the
analytics/insights services, and the MCP tools must all agree on these; when
they drift, this file wins. Change a definition here first, then make the code
match. MCP tool descriptions should quote these definitions, not paraphrase
them — the LLM on the other end of those tools is the consumer most likely to
silently assume a different meaning.

Each entry names the owning code so the definition stays checkable.

---

## Transaction typing

**Transaction type** — exactly three values: `Income`, `Outflow`, and
`Transfer` (`internal/models/transaction.go`). `Transfer` is neither income
nor expense and is excluded from both, but unlike the prior drop-on-load
filter it remains visible in the ledger and has its own page
(`/transfers`). `metrics.Calculate` filters by type, so a `Transfer` row
falls out of Total Income and Total Expenses without a formula change.

**Classification** (`internal/services/classifier/classifier.go`) runs once
per row at load, in this precedence order:

1. Description contains a `NeverIncomeKeywords` entry (card/loan/bill
   payments, autopay, fees…) → always `Outflow`, regardless of sign.
2. Amount is positive **and** category or description matches
   `IncomeCategories` / `IncomeKeywords` → `Income`.
3. Everything else → `Outflow`.

**Sign convention** — after classification, amounts are normalized:

- `Income`: always positive.
- `Outflow`: negative if originally negative (a real purchase).
- **Positive amounts that are not income stay positive but are typed
  `Outflow`** — these are credits/refunds/chargebacks. Consequence: they
  *net against* expenses (see Total Expenses below). Do not treat
  "Outflow" as a synonym for "negative amount".

**Source sign convention (per-file auto-detection)** — source files disagree
about what a positive amount means: bank exports use positive = deposit /
negative = debit, credit-card exports use positive = charge / negative =
credit. The app stores everything in **bank convention**, so credit-card files
are detected and sign-flipped at parse time, per file
(`usesCreditCardSignConvention`, `internal/services/dataloader/loader.go`).
A file is flipped only when **both** hold:

1. **No row carries a bank-only signal.** Any single one aborts the flip for
   the *entire file* — classifiable income (`IsPotentialIncome`: a positive
   amount whose category or description matches the income dictionaries), a
   category of `check`, a description starting `check #` / `chk #`, or a
   description containing `wire transfer`, `funds transfer`, `direct deposit`,
   `direct dep`, or `payroll`.
2. **≥70% of its non-zero amounts are positive** (`ccConventionPositiveThreshold`).
   This is gated on sample size twice — the file needs **≥10 transactions** and
   **≥10 non-zero amounts** (`minSignConventionSample`); below either, it is
   left untouched. A small card export therefore keeps its raw signs.

The abort in (1) is deliberately conservative: it protects a genuine bank file
that happens to have a positive month from being flipped. The cost is that a
credit-card export containing even one paycheck-shaped row, paper check, or
"Interest Paid" line is left **unflipped** — every charge in it then stays
positive and is read as a refund/credit that *subtracts* from expenses (see
Sign convention). When a total looks implausibly low, check this first: the
loader logs `Detected credit-card sign convention in <file>` for each file it
flips, so a missing line for a card export is the tell.

Flipping **re-computes each transaction's `Hash`** on the post-flip amount, so
the same transaction arriving from a bank-convention and a card-convention
source hashes identically and deduplicates. Pins and enrichment key off that
same post-flip hash.

**Account** — a named source of transactions (`internal/models/account.go`,
persisted in the `data/accounts.json` sidecar through
`internal/services/storage`). An Account carries `ID`, `Name`, `Institution`,
`Kind`, `FilePatterns`, and `BalanceAnchors`. One CSV file maps to exactly one
account, matched by filename pattern against `FilePatterns`; **first match
wins**, with accounts sorted by ID for deterministic ordering. A file matching
no account leaves its rows' `AccountID` empty — **unassigned**, not dropped:
unassigned rows are counted and surfaced in the dashboard and explorer
banner, never silently passed through. IDs are unique; at save time a warning
is surfaced when two accounts' `FilePatterns` overlap an existing file (first
match still wins by ID sort).

**Account kind** — the `AccountKind` enum: `checking`, `savings`, `brokerage`,
`credit`, `other`. One behavioral consequence: `Kind: credit` **forces** the
credit-card sign convention for that file, overriding the ≥70%-positive
heuristic in `usesCreditCardSignConvention`. Other kinds leave the heuristic
alone.

**Transfer** — the third `TransactionType`, alongside `Income` and `Outflow`.
A transfer is neither income nor expense and is excluded from both, but unlike
today it **remains visible** in the ledger and has its own page
(`/transfers`). See Transfer classification below for how rows become
`Transfer`-typed.

**TransferClass** — `paired` (both legs present and linked via a shared
`TransferPairKey`) or `external` (a leg whose counterparty account is not
loaded, e.g. a Vanguard contribution whose receiving CSV is not imported).
A non-transfer row carries an empty `TransferClass`.

**TransferPairKey** — the value shared by exactly two legs of one paired
transfer (`Transaction.TransferPairKey`). Computed as
`sha256(stableID_a + "|" + stableID_b)[:12]` with the two legs ordered
lexicographically, so both legs carry the same key and either one resolves the
pair.

**StableID** — `accountID|YYYY-MM-DD|amount-in-cents|n`, where `n` is the
0-based occurrence index among rows identical in those first three fields,
counted in file row order (`Transaction.StableID`). The amount is the
**post-sign-normalization** value (after the credit-card flip), so the ID is
stable regardless of the heuristic's input. Description is deliberately out of
the identity, so a bank reformatting its description text cannot orphan user
decisions. Unassigned rows use `file:<basename>` in the accountID slot —
usable but not durable across file renames. Collisions (several same-amount
rows, one account, one day) rely on export row order being stable across
re-exports. Sidecar stores (`transaction_pins.json`,
`duplicate_decisions.json`, `amazon_enrichment.json`,
`transfer_decisions.json`) look up by `StableID` first and fall back to the
legacy content `Hash`, rewriting entries to `StableID` on next save; no
one-shot migration, nothing breaks if a sidecar is never re-saved.

**BalanceAnchor** — a user-entered `{date, amount, note}` stating an account's
balance as of the **end** of that day (`Account.Anchors`, kept sorted by
date). A balance at a given date is the latest anchor at or before that date
plus the sum of that account's transaction amounts after it
(`internal/services/accounts`, `BalanceAt`); a transaction on the anchor day
itself is excluded, because the anchor already reflects end-of-day. With no
anchor at or before the date, the balance is **unavailable** — not zero; the
UI shows "set an anchor" rather than `$0`.

**Internal transfer** — a movement between the user's own accounts (credit
card payments, brokerage ACH, "usaa funds transfer", …). Matching rows become
`Transfer`-typed at the transfer-classification stage (see Load pipeline
order); they are **not** filtered out, and remain in the ledger. Pairing is
what makes the determination robust: two legs of opposite sign, equal amount
in cents, different `AccountID`s, dates within ±4 days, with at least one leg
matching `InternalTransferPatterns` (classifier.go) or an
`IsInternalTransfer: true` Major Expense — the unique such candidate
auto-pairs. Candidates with no pattern hit are **suggested for review, never
auto-paired**, because coincidentally equal amounts are common; a user
confirm/reject decision persists in `data/transfer_decisions.json`. An
unpaired row that does match a pattern (counterparty CSV not loaded, e.g.
Vanguard) becomes `Transfer`/`external`. `DataLoader.FilteredTransfers()`
keeps returning a count for now, but it counts classified `Transfer` rows
rather than dropped rows, for compatibility with the existing UI until the
Transfers page ships.

**Suppressed** — the user resolved a near-duplicate pair and dropped this side
from totals (`Transaction.Suppressed`). **All aggregation/reporting must go
through `TransactionSet.Active()`**, which excludes suppressed rows. The
transaction explorer intentionally shows the raw set so suppressions are
auditable and undoable.

---

## Load pipeline order

Order matters — several definitions above only hold at a particular stage.
`DataLoader.LoadData` (`internal/services/dataloader/loader.go`) runs:

1. **Parse** each CSV (per file).
2. **Sign-convention auto-detect and flip** (per file), re-hashing on the
   post-flip amount. A `Kind: credit` account forces the flip for its file,
   overriding the heuristic.
3. **Stamp `AccountID`** on every row by matching the CSV basename against
   accounts' `FilePatterns` (first match, accounts sorted by ID); unmatched
   files leave `AccountID` empty and are counted as unassigned.
4. **Assign `StableID`** to every row (`accountID|date|cents|n`, post-flip
   amount; `file:<basename>` fallback for unassigned rows).
5. **Deduplicate** exact matches, then detect near-duplicate pairs and apply
   the user's resolutions (this is what sets `Suppressed`). Exact dedup runs
   before transfer classification so duplicate rows cannot create phantom pair
   candidates.
6. **Classify transfers** — pair opposite-sign rows with equal amount in
   cents, different `AccountID`s, within ±4 days, at least one leg matching
   `InternalTransferPatterns` or an `IsInternalTransfer` Major Expense; unique
   candidates become `Transfer`/`paired`, pattern-less matches go to the
   suspected review queue, and unpaired pattern hits become
   `Transfer`/`external`. Replaces the old `filterInternalTransfers` drop.
7. **Classify** Income/Outflow on the remaining (non-`Transfer`) rows and
   normalize amounts; `Transfer` rows are skipped.
8. **Stamp** aliases, `MajorExpenseName`, and Amazon `EnrichedDescription`.
9. **Compute derived fields** (month, week, quarter, …).

Two consequences worth stating: the sign flip and credit-kind override happen
**before** account stamping, StableID assignment, and transfer classification,
so the amount used for pairing and identity is the normalized one; and
transfers are **classified, not dropped** — they remain in the ledger as
`Transfer`-typed rows, so income/outflow totals exclude them by type filter
rather than by absence.

---

## Core aggregates

Owner: `internal/services/metrics/metrics.go` (`Calculate`).

**Total Income** — sum of `Income` amounts in the selected range.

**Total Expenses** — `|sum of Outflow amounts|`. Because refund credits are
positive Outflows, they reduce this figure. "Expenses" always means
*net of refunds*.

**Net Savings** — Total Income − Total Expenses.

**Savings Rate** — Net Savings / Total Income × 100. Defined as **0 when
Total Income ≤ 0** (not negative, not NaN).

**Months in range** — inclusive day count of the *user-selected* date range
(not transaction min/max) divided by 30.4375 (365.25/12), floored at one day
(`MonthsBetween`). A sparse range still divides spend across the full window
the user selected. Single day ≈ 0.033 months.

---

## Living vs. healthcare (the double-billing rule)

**Health Insurance category** — the load-bearing literal
`HealthInsuranceCategory = "Health Insurance"` (metrics.go). It matches what
the bank/credit-card CSVs export; comparison is case-insensitive
(`FilterByCategory`), but write the canonical casing everywhere.

**Healthcare (actual)** — `|sum|` of Outflows in the Health Insurance
category over the range.

**Living Expenses** — **Total Expenses − Healthcare.** Healthcare premiums
have their own KPI, so they are excluded from the living figure; counting
them in both places would double-bill the user. Any new consumer that shows
"living", "monthly spend vs budget", or budget variance must use this
subtraction, not raw Total Expenses.

---

## Budget targets and variance

**Budget target (living)** — the what-if plan's `MonthlyLivingExpenses`,
**phase-adjusted**: when spending phases are enabled, each calendar month in
the range contributes its own phase multiplier and the result is the average
(`phaseAdjustedMonthlyTarget`, metrics.go). A range straddling a phase
boundary gets a blended target.

**Healthcare target** — the what-if plan's total healthcare cost at month 0,
i.e. "today's planned premium" (`currentHealthcareTarget`). Healthcare is
**intentionally not phase-adjusted** — premiums rise with age rather than
fall with spending phases.

**Target sentinels** — a target of `0` means "no target configured".
`HasBudgetTarget` / `hasHealthcareTarget` carry this. When false, the delta
fields degenerate (`PerMonthDelta` = actual monthly, `CumulativeDelta` =
total): **consumers must gate on the Has\* flag before presenting a delta as
variance.**

**Delta sign convention** — positive = over budget, negative = under. Applies
to `PerMonthDelta`, `CumulativeDelta`, and their healthcare/combined
variants.

**Combined variance** — Living + Healthcare actuals netted against the sum of
both targets, so one category being under can offset the other being over.
Drives the Budget KPI card.

---

## Naming and grouping

**Label** — the user-facing name of a transaction, by precedence
(`Transaction.Label()`): `DisplayName` (per-txn user alias) →
`EnrichedDescription` (external data, e.g. Amazon order) →
`MajorExpenseName` (rule-based group) → `Description` (raw bank text).
Per-transaction signals beat rule-based grouping. Grouping/aggregation reads
`MajorExpenseName` directly and is unaffected by this precedence.

**Category** — a free string from the source CSV. An empty category is
*displayed and grouped* as `"Uncategorized"`, but stays empty in storage —
`"Uncategorized"` is a presentation value, never persisted.

**Major Expense** — a *user-declared* expense baseline
(`internal/models/major_expense.go`): a name, matching keywords, and an
expected amount range, used for grouping and anomaly flagging. The
`IsInternalTransfer: true` variant is not an expense at all but a
user-supplied transfer filter (see Internal transfer); it is hidden from
spending rollups but visible in the Major Expenses list for auditability.

---

## Insights vocabulary

Owner: `internal/services/insights/` (shared by the insights pages and the
MCP `get_recurring` / `get_trends` tools).

**Recurring payment** — a merchant/description pattern that repeats at a
regular interval in the history.

**Subscription** — a recurring payment that is neither *retail* (matches
`retailKeywords` — stores, restaurants, gas: frequent but not a service) nor
a *bill* (matches `billKeywords` — utilities, rent/mortgage, insurance, tax).
"Recurring" is the superset; "subscription" is the interesting subset.

---

## Rules for new code

1. Reuse the exported constants (`HealthInsuranceCategory`, keyword lists) —
   never re-type the literals.
2. Aggregate through `Active()`; present raw sets only in the explorer.
3. Gate every variance display on its `Has*Target` flag.
4. If a new metric needs a term this file doesn't define, define it here in
   the same change.
