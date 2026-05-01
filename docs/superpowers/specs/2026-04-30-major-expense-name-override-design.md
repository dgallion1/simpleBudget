# Major Expense Names Override Bank Descriptions — Design

## Problem

Bank-given transaction descriptions are noisy and inconsistent (`BOFA HOME LOANS 0123`, `BAC HOME LOAN`, `BANKOFAMER HOMELOANS`). Users already curate stable, human-readable names through the **Major Expenses** feature (e.g., `Mortgage`). Today, those names appear only as section headers on the Major Expenses page and as small chips in a few spots — every other view falls back to the bank text.

Users want their curated Major Expense names used as the primary label for transactions everywhere the app renders, aggregates, or exports them.

## Goals

- A transaction that matches a Major Expense displays under that expense's `Name` in every UI that lists transactions.
- Aggregations that key on transaction text (Top Merchants, etc.) roll up by the resolved label, not the raw bank text.
- Search matches the resolved label as well as the raw text.
- Transaction-level exports use the resolved label.
- Original bank text remains visible (as small subtext) wherever a user might need it to verify what the bank actually called the row.

## Non-Goals

- No new UI for editing Major Expense names from inline transaction rows. Editing stays on the Major Expenses page.
- No change to recurring-payment *detection* algorithms (similarity matching on raw description text remains correct).
- No change to backup zips (they preserve raw source-of-truth CSVs).
- No change to the KPI export (monthly aggregates with no description column).
- No migration of existing per-transaction `aliases.json`. Aliases keep working; they take precedence over Major Expense names.

## Label Resolution Rule

For every transaction display, aggregation key, or export field that previously used `Description`:

```
Label(t) =
    DisplayName     if t.DisplayName != ""           // per-transaction alias wins (most specific)
    MajorExpenseName if t.MajorExpenseName != ""     // group name overrides bank text
    Description     otherwise                        // fall back to raw bank text
```

Rationale: `DisplayName` is set per-row deliberately by the user via the Explorer rename UI; it represents an explicit override of *this one row*. `MajorExpenseName` comes from group matching (keywords/amount/pin); it overrides the bank text for everything else.

## Architecture

### 1. New derived field on `Transaction`

`internal/models/transaction.go`:

```go
type Transaction struct {
    // ...existing fields
    DisplayName       string `json:"display_name,omitempty"`        // per-txn alias (existing)
    MajorExpenseName  string `json:"major_expense_name,omitempty"`  // NEW: derived; not persisted to disk
    // ...
}

// Label returns the user-facing label for the transaction:
// DisplayName -> MajorExpenseName -> Description.
func (t Transaction) Label() string {
    switch {
    case t.DisplayName != "":
        return t.DisplayName
    case t.MajorExpenseName != "":
        return t.MajorExpenseName
    default:
        return t.Description
    }
}
```

`MajorExpenseName` is a derived field (not persisted to source CSVs or aliases.json). It is stamped at load time, like the existing `Month`/`Week`/`Year` derived fields.

### 2. Load-time stamping

`internal/services/dataloader/loader.go`:

```
Load() flow (existing):
   readCSVs -> dedupe -> applyAliases ...

New step appended after applyAliases:
   applyMajorExpenseNames(transactions)
```

`applyMajorExpenseNames` does:

1. Load `major_expenses.json` and `transaction_pins.json` (both already used by the Major Expenses handler).
2. Build a name lookup `id -> name`.
3. For each transaction:
   - If a pin exists for `t.Hash` and points to a valid expense: stamp that expense's name.
   - Else, run the same `matchTransaction` logic from `services/majorexpenses/engine.go` (keyword + amount rules); stamp the matched expense's name.
4. If neither expense definitions nor pins exist, no-op (zero churn for users who haven't set up Major Expenses).

The matching logic stays single-sourced in `services/majorexpenses` (refactor `matchTransaction` to be exported, or extract a small `Resolver` type used by both the engine and the loader). The Major Expenses page continues to use the engine's full `Match()` for groups/exceptions; load-time stamping just needs the per-transaction "which expense does this map to?" answer.

**Idempotency / re-stamping.** When the user changes Major Expense definitions or pins, the dataloader is invalidated and reloads — same flow re-runs and produces fresh `MajorExpenseName` values.

### 3. Template changes

Replace every `{{if .DisplayName}}{{.DisplayName}}{{else}}{{.Description}}{{end}}` snippet with `{{.Label}}`. Files to touch:

- `web/templates/pages/explorer.html` — main transaction table; keep the existing `Alias (bank text)` pattern by showing the bank text in small parens whenever `Label != Description`, so users can still verify what the bank called the row.
- `web/templates/pages/major-expenses.html` — three sites: matched transactions table, exception rows, anomaly rows.
- `web/templates/pages/insights.html` — subscriptions, recurring payments, top merchants, top income lists.
- `web/templates/components/category-drilldown.html` — drilldown row.
- Dashboard templates that render transaction-shaped rows (Top Merchants list).

The bank text stays visible as small parenthetical subtext in views with room for it (Explorer table, Insights detail rows). Compact views (Dashboard donut tooltips, mini-tables) just show `Label`.

### 4. Aggregation follows Label

- `internal/handlers/dashboard/handlers.go:951` — `merchantTotals[t.Description]` becomes `merchantTotals[t.Label()]`.
- `internal/handlers/insights/handlers.go` — keys built from `t.Description` for *display grouping* (top merchants by spend) become `t.Label()`. The recurring-payment *detection* code (similarity-of-text) keeps using `Description` — it's an algorithm input, not a display.

### 5. Search follows Label

`internal/models/transaction.go` `FilterBySearch` already checks `Description` and `DisplayName`. Add a third check for `MajorExpenseName` (so typing "Mortgage" finds rows even when the bank text is `BOFA HOME LOANS 0123`).

### 6. Export follows Label

- KPI export: unchanged (no description column).
- Backup zip: unchanged (raw source-of-truth CSVs preserved by design).
- Any current or future transaction-level export uses `t.Label()` for the description column. This is a policy line in the spec rather than new code, since no transaction-level export exists today.

## Data Flow

```
CSVs on disk
    -> readCSVs (raw Description)
    -> dedupe
    -> applyAliases       (sets DisplayName from aliases.json)
    -> applyMajorExpenseNames (NEW: sets MajorExpenseName from major_expenses.json + transaction_pins.json)
    -> in-memory TransactionSet
    -> handlers / templates use t.Label() for display, aggregation, search, export
```

## Edge Cases

- **No Major Expenses defined.** `applyMajorExpenseNames` no-ops; `Label()` falls back to `DisplayName`/`Description` exactly as today.
- **Pin to a deleted expense.** Pin is ignored (existing behavior in the engine), so the transaction falls through to keyword/amount matching, just like on the Major Expenses page.
- **Transaction matches multiple expenses.** First-def-wins for determinism (existing `matchTransaction` rule).
- **`major_expenses.json` or `transaction_pins.json` missing or unreadable.** Log a warning, no-op (mirrors `applyAliases` failure handling).
- **User renames a Major Expense.** Next request reads the updated definitions and re-stamps; labels update everywhere immediately.
- **CSV export of raw data.** `Description` column in CSV exports of `Transaction`-shaped rows uses `Label()`. The backup zip is the only export that ships raw `Description` and is intentionally exempt.

## Testing

- Unit test `Transaction.Label()` precedence (5 cases: only-Description, only-DisplayName, only-MajorExpenseName, all three set, none set).
- Unit test `applyMajorExpenseNames` happy paths: keyword match, exact-amount match, range match, pin override, pin to deleted expense, no defs (no-op), no pins file.
- Unit test `FilterBySearch` matches a Major Expense name when neither Description nor DisplayName contains the search term.
- Integration test through the dataloader: load fixtures with major_expenses.json + aliases.json + pins, assert transactions emerge with correct `MajorExpenseName` stamped and aliases winning where applicable.
- Template-level test (existing render tests) updated for the new label placement.
- Manual UI verification: explorer, insights, dashboard, major-expenses, category drilldown all show the curated names.

## Risks / Trade-offs

- **Loader gains a soft dependency** on Major Expense data files. Mitigated by graceful no-op when files are missing.
- **Single-pass matching cost** at load is `O(transactions × defs)` substring scans. With ~thousands of transactions and ~tens of defs this is sub-millisecond per load and runs only when data is loaded (not per request).
- **Existing aliases keep working.** No data migration. If a user wants to "consolidate" an alias under a Major Expense they can delete the alias manually (out of scope here).

## Out-of-Scope Future Work

- Inline "rename to Major Expense X" affordance from a transaction row (today the user navigates to the Major Expenses page).
- An Explorer "Download CSV" button (the policy line in this spec already covers what its description column should contain).
- Bulk migration of `aliases.json` entries into Major Expense definitions.
