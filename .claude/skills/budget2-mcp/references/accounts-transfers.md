# Accounts & transfers — "where is my money?"

An Account is a named source of transactions; one CSV file maps to exactly
one account by filename pattern. Balances are never read from the bank: they
are **rolled forward from a BalanceAnchor** — a user-entered {date, amount}
stating the balance as of the END of that day — plus the account's
transactions after it. A Transfer is the third transaction type (neither
income nor expense): money moving between the user's own accounts. See
GLOSSARY.md ("Account", "BalanceAnchor", "Transfer", "TransferClass").

## get_accounts (read)

Lists the configured accounts with current balance, freshness (latest
transaction date), and whether each is below its low-balance threshold.

The two rules that prevent wrong answers:

- An account with **no anchor reports `available=false` and `balance=0`.
  That is UNAVAILABLE, not a zero balance — never present it as $0.** The
  right response is "no balance anchor set for this account; set one to get
  balances."
- `low_balance` is true only when the balance is available and strictly
  below the threshold. An unavailable balance is not "low".

Freshness exists so a stale CSV masquerading as a healthy balance is
visible — mention it when the latest transaction is old.

## get_balance_projection (read)

The 35-day funding projection for one account: the first date the projected
balance crosses the low-balance threshold, the minimum projected balance, a
suggested top-up rounded up to the nearest $100, and the median of confirmed
inbound paired-transfer amounts as a reference ("you usually move $X").
Advisory only — nothing is written.

Params: `account_id` (from `get_accounts`), `as_of` (YYYY-MM-DD, default
today).

- When `available` is false, report **"cannot project"** — there is no
  anchor at or before the as-of date, and an unknown balance is not a zero
  balance to roll forward.
- When `has_reference` is false, `reference_amount` is zero and **must not
  be presented as a number to move**.

## get_transfers (read)

The transfer flows the ledger recorded. `class` is `paired` (both legs
loaded, linked by a shared `pair_key`) or `external` (the counterparty's CSV
is not loaded, e.g. a Vanguard contribution whose receiving CSV was never
imported — only one leg appears).

Params (AND-combined): `start_date`, `end_date`, `institution`
(case-insensitive), `account_id` (exact; validated — a typo is an error, not
an empty result).

- Amounts are **signed in bank convention**: positive = money received into
  that account, negative = money sent out. "How much did I move into
  checking this year" = filter to checking, read `total_in`.
- An `account_id` filter does **not** pull in a paired transfer's other leg
  — you see the one leg in that account. To see both legs of a pair, omit
  `account_id`.

## set_balance_anchor 🔒 (guarded write → accounts.json)

Records a BalanceAnchor. Anchors are load-bearing: every balance and
projection is rolled forward from one, so a wrong amount makes the dashboard
lie about the user's money. Hence the guard.

Params: `account_id`, `date` (balance as of the END of this day), `amount`
(bank convention: positive = money you have), `note` (e.g. the source
statement), `confirm_token`.

- A second anchor on the same day **overwrites** the first — the end-of-day
  balance is a single fact. An earlier-dated anchor is inserted in date
  order.
- Follow the two-call protocol in `guarded-tools.md`. The token is bound to
  this tool AND these exact arguments.

## resolve_transfer 🔒 (guarded write → transfer_decisions.json)

Confirms or rejects a **suspected** transfer pair: two cross-account,
opposite-sign, equal-amount rows inside the pairing window that no transfer
pattern backs. Coincidentally equal amounts are common, so the app only ever
suggests these — a human verdict is what pairs them.

Params: `pair_key` (from `get_transfers` or the review queue — **never
invent one**), `verdict` (`confirm` or `reject`), `confirm_token`.

- `confirm` is the load-bearing verdict: both legs become Transfer/paired on
  the next load, pattern hit or not — and if the rows were NOT actually a
  transfer, confirming silently erases real income or real spending from
  every total. `reject` marks the pair a coincidence, never suggested or
  auto-paired again.
- Follow the two-call protocol in `guarded-tools.md`. The token is bound to
  this tool AND to that `pair_key` and `verdict`.
