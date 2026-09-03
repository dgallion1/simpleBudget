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

The three rules that prevent wrong answers:

- An account with **no anchor reports `available=false` and `balance=0`.
  That is UNAVAILABLE, not a zero balance — never present it as $0.** The
  right response is "no balance anchor set for this account; set one to get
  balances."
- `low_balance` is true only when the balance is available and strictly
  below the threshold. An unavailable balance is not "low".
- The threshold applies only to **checking and savings**. Credit,
  brokerage, and other kinds always report `low_balance=false` and
  `threshold=0` — a credit card's negative balance is money owed, not a
  cash shortfall; never describe such an account as "below its threshold".

Freshness exists so a stale CSV masquerading as a healthy balance is
visible — mention it when the latest transaction is old.

## get_balance_projection (read)

The 35-day funding projection for one account: the first date the projected
balance crosses the low-balance threshold, the minimum projected balance, a
suggested top-up rounded up to the nearest $100, and the median of confirmed
inbound paired-transfer amounts as a reference ("you usually move $X").
Advisory only — nothing is written. Cash kinds only: for credit,
brokerage, or other accounts the tool returns an error naming the kind
instead of a projection — relay that, don't retry.

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

## get_suspected_transfers (read)

The transfer **review queue**: candidate pairs the classifier found but
would not auto-pair. A suspected pair is two cross-account, opposite-sign,
equal-amount rows inside the pairing window that no transfer pattern backs —
coincidentally equal amounts are common, so these are only ever *suggested*.
`reason` is `amount_match` (no pattern hit on either leg) or `ambiguous`
(several pattern-backed candidates tied on date distance).

No params. An empty result (`count` 0) means nothing is currently awaiting
review — a normal answer, not an error.

- **This is the tool `resolve_transfer`'s `pair_key` must come from.** It
  replaces reading the `/transfers` page by hand — the same "Suspected
  pairs" queue, but reachable in-session. Never invent a `pair_key`.
- Legs are shaped like `get_transfers`' rows (`date`, `description`,
  `account_id`, signed `amount`, `category`), so the two tools read the same
  to a model.

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

Params: `pair_key`, `verdict` (`confirm` or `reject`), `confirm_token`.

- `pair_key` must come from **`get_suspected_transfers`** — the same queue
  the `/transfers` page shows under "Suspected pairs" (an HTMX swap target),
  now reachable in-session. `get_transfers` only returns pairs already
  classified as Transfer, so a key read from it is already resolved and
  `resolve_transfer` will refuse it with "no longer a suspected transfer
  awaiting review". **Never invent a `pair_key`.**

- `confirm` is the load-bearing verdict: both legs become Transfer/paired on
  the next load, pattern hit or not — and if the rows were NOT actually a
  transfer, confirming silently erases real income or real spending from
  every total. `reject` marks the pair a coincidence, never suggested or
  auto-paired again.
- Follow the two-call protocol in `guarded-tools.md`. The token is bound to
  this tool AND to that `pair_key` and `verdict`.
