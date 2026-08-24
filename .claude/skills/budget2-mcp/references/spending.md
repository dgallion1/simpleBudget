# Spending — "where does my money go?"

All six tools are read-only over the transaction history. All of them exclude
transactions the user has resolved as duplicates, and none of them count
Transfer-typed rows as income or spending.

## summarize_spending

Totals for income, expenses, savings, plus category/merchant/month
breakdowns over a date window (default: the full history). Reports
**positive dollar amounts throughout**, and includes a budget-vs-target
comparison when a plan is configured.

Params: `start_date`, `end_date` (YYYY-MM-DD, inclusive), `top_n` (how many
categories/merchants, default 10).

## search_transactions

Search the ledger. Every filter is optional and they combine with AND.
Amounts come back **signed** — expenses negative — so summing a result set
mixes signs on purpose.

Params: `start_date`, `end_date`, `category` (exact name), `search`
(case-insensitive substring against description, display name, major-expense
name, or enriched description — the returned description may differ from
whichever matched), `type` (`income`, `outflow`, or `transfer`; `expense` is
an alias for `outflow`), `min_amount`/`max_amount` (absolute dollars; 0 means
unset), `page` (1-based), `per_page` (default 50, max 200).

This is also where you get transaction hashes for `pin_transactions`.

## get_trends

Category and major-expense spending, income patterns, and spending velocity
(burn rate) over a window, compared against the **immediately preceding
window of equal length**. Default window: the last full calendar month.

Params: `start_date`, `end_date`.

## get_recurring

Detects recurring payments — subscriptions, bills, repeating charges — by
clustering outflows into merchant groups with consistent amounts at
consistent intervals.

Params: `start_date`, `end_date` (display window), `subscriptions_only`,
`reference_date` (freshness cutoff: a payment is reported only if its most
recent occurrence is recent enough for its own interval as of this date;
default is the ledger's latest transaction date — pass today's date to hide
payments that appear to have stopped).

## get_price_creep

Recurring merchant charges whose amount has drifted **upward** over their
full history: for each merchant with at least 6 occurrences, the median of
the first 3 charges is compared to the median of the last 3, reporting when
the increase exceeds 5%. Decreases and single outliers never report, by
design — do not use this to look for price drops.

Params: `start_date`, `end_date`.

## get_anomalies

Flags unusual **expense** transactions (outflows only) three ways: amounts
far outside a merchant's typical range (`mad_merchant`), outside a
category's typical range (`mad_category`), or an outsized first-ever charge
from a brand-new merchant (`new_merchant`).

Detection always runs over the **complete history** — the baselines and each
merchant's first occurrence never change with the window. `start_date` /
`end_date` only filter which already-detected flags are returned, so a
narrow window does not change what counts as anomalous.
