# Major expenses — "label my spending"

Major expenses are the user's own labels for spending they already expect —
"Mortgage", "Groceries" — matched to transactions by keywords and amount
ranges, with explicit pins for the stragglers. Two reads, three writes. Each
write copies the file it changes to a `.bak` under
`<backup-dir>/mcp-snapshots/data` before the session's first change to it.

Only the what-if page polls for changes: a curation write leaves an
already-open Major Expenses browser tab stale until reloaded — mention that
if the user says "I don't see it".

## list_major_expenses (read)

The declared expenses with each one's in-window match count and net total.

Params: `start_date`, `end_date` (default: full record), `include_transactions`
(return each expense's matched transactions **with their hashes** — the way
to get hashes for pinning), `include_deleted` (soft-deleted expenses that can
still be restored).

## list_exceptions (read)

The transactions the Major Expenses page flags for attention, in three
buckets: `unmatched` (outflows matching no expense), `anomalous` (outside
their expense's expected amount range), `new_merchants` (first-time
merchants).

Params: `bucket` (omit for all three), `start_date`, `end_date`, `search`,
`min_amount`/`max_amount`, `limit` (per bucket, default 50, max 200 — each
bucket still reports its full total).

## upsert_major_expense ✏️

Creates or edits a definition. **Omitted fields are left untouched**, so an
edit passes only what changes; an empty list explicitly clears a list field.

Params: `id` (omit to create), `name` (required when creating), `keywords`
(case-insensitive substrings against descriptions), `expected_min` /
`expected_max` (positive dollars; equal = exact match, a range flags
anything outside it as anomalous), `category`, `notes`, `pin_hash` (pin the
transaction that prompted the expense in the same call),
`is_internal_transfer` (treat matches as money moving between the user's own
accounts — this feeds the transfer classifier, dropping matches from
spending totals; use it for broker patterns the built-in list misses).

## pin_transactions ✏️

Attaches transactions to an expense, or detaches them. Pins beat keywords:
a pinned transaction is matched even if the keywords would never catch it.

Params: `expense_id` (from `list_major_expenses`; required unless
unpinning), `hashes` (from `list_exceptions`, `list_major_expenses`, or
`search_transactions`) **or** `filter` (act on every matching outflow) — one
or the other, not both. `unpin` removes pins so the transactions fall back
to keyword/amount matching.

A filter selecting **more than 200 transactions is refused** rather than
applied — narrow the filter instead of retrying.

## delete_major_expense ✏️

Soft-deletes a declared expense — or restores one.

Params: `id` (from `list_major_expenses`, or from the deleted list when
restoring), `restore` (bring a deleted expense back).

Soft means recoverable: a delete is undone by the same tool with
`restore: true`, and `list_major_expenses` with `include_deleted` shows what
can come back.
