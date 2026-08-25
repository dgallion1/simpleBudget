---
name: budget2-mcp
description: >-
  How to drive the budget2 server's built-in MCP tools (mounted at
  http://localhost:8080/mcp, auto-discovered via the repo's .mcp.json). Use
  this skill whenever the user asks anything about their money that the
  running app can answer — spending, transactions, subscriptions, account
  balances, transfers between accounts, the retirement/what-if plan, major
  expenses, duplicates, backups — or whenever you are about to call any
  budget2 MCP tool, especially a writing or guarded one. Also use it when a
  budget2 tool fails for no visible reason or the tools are missing from the
  session.
---

# budget2 MCP tools

The budget2 server exposes 31 tools over MCP. This skill is a router: find
the branch that matches what the user is asking, then read that branch's
reference file before calling its tools — every branch has gotchas that are
cheaper to read than to rediscover.

## Before anything else

- **The tools come from the running server.** If they are missing from the
  session, the server was not listening when the session started. Nothing you
  can do fixes that from here: the user must start `budget2` and start a new
  session. Say so plainly instead of hunting for the tools.
- **When a tool fails for no visible reason, call `get_status` first.** A
  locked encrypted store makes every ledger-reading tool fail, and
  `get_status` is the only tool that still answers. It reports where the data
  lives, whether it is encrypted and unlocked, and the last backup. Unlocking
  happens in the web UI at `/unlock`, not through a tool.
- **Before any change the user might want to walk back, consider
  `run_backup`.** It adds a timestamped archive and changes nothing else, so
  it is always safe.

## The five branches

| Branch | The user is asking… | Reference |
|---|---|---|
| **Spending** | "where does my money go?" — totals, trends, subscriptions, anomalies, finding transactions | `references/spending.md` |
| **Accounts & transfers** | "where is my money?" — balances, will checking run low, money moved between accounts | `references/accounts-transfers.md` |
| **Retirement plan** | "will I be OK?" — what-if scenarios, projections, changing plan assumptions | `references/retirement-plan.md` |
| **Major expenses** | "label my spending" — declared expense groups, pinning transactions, exceptions | `references/major-expenses.md` |
| **Housekeeping** | the app itself — status, data files, duplicates, backups, shutdown | `references/housekeeping.md` |

Tools by branch:

- **Spending** (all read-only): `summarize_spending`, `search_transactions`,
  `get_trends`, `get_recurring`, `get_price_creep`, `get_anomalies`
- **Accounts & transfers**: `get_accounts`, `get_balance_projection`,
  `get_transfers` (reads); `set_balance_anchor` 🔒, `resolve_transfer` 🔒
- **Retirement plan**: `list_scenarios`, `get_analysis`, `get_months`,
  `run_scenario`, `open_page` (reads); `apply_changes` ✏️
- **Major expenses**: `list_major_expenses`, `list_exceptions` (reads);
  `upsert_major_expense` ✏️, `pin_transactions` ✏️, `delete_major_expense` ✏️
- **Housekeeping**: `get_status`, `list_data_files`, `list_duplicates`,
  `list_backups` (reads); `resolve_duplicates` ✏️, `undo_resolve` ✏️,
  `run_backup` ✏️; `restore_backup` 🔒, `shutdown_server` 🔒

## The safety ladder

Every tool sits on one of three rungs. Know which rung you are on before you
call.

1. **Read-only (20 tools).** Call freely. Nothing can go wrong beyond a wrong
   answer, and the reference files exist to prevent those.
2. **Writes with a safety net (7 tools, ✏️).** Six of them (`resolve_duplicates`,
   `undo_resolve`, `upsert_major_expense`, `pin_transactions`,
   `delete_major_expense`, `apply_changes`) copy the file they are about to
   change to a `.bak` under `<backup-dir>/mcp-snapshots` before their first
   change of the session. `run_backup` is different: it is purely additive —
   it writes a full, timestamped archive of the data directory rather than a
   `.bak` of a file it is changing, and changes nothing existing. All seven
   are reversible by hand, but real: tell the user what you wrote, and
   remember there is no in-app undo for the plan.
3. **Guarded (4 tools, 🔒).** `restore_backup`, `shutdown_server`,
   `resolve_transfer`, `set_balance_anchor`. Two-call protocol with a
   single-use token and a human-approval step. These are the tools where a
   mistake erases data, kills the session, or silently corrupts every total.
   Read `references/guarded-tools.md` before the first call — the protocol
   has rules ("confirming twice yourself is not consent") that exist because
   models have gotten them wrong.

The pattern to internalize: every branch is mostly reads with one or two
sharp edges at the bottom. Start at the top of your branch; anything on rung
2 or 3 needs the user's word first.

## Cross-cutting facts

These hold everywhere and are the usual cause of "the numbers don't match":

- **Transfers are a third transaction type.** Money moved between the user's
  own accounts is neither income nor expense; every spending and income total
  excludes Transfer-typed rows by type. If a total looks too low, transfers
  are the first suspect — `get_transfers` shows them.
- **Resolved duplicates are excluded.** All six spending tools skip
  transactions the user resolved as duplicates. `list_data_files` reports
  raw, unfiltered row counts, so its numbers will not match
  `search_transactions` totals — that is not a bug.
- **Sign conventions differ by tool.** `search_transactions` and
  `get_transfers` return signed amounts (money out is negative);
  `summarize_spending` reports positive dollar figures throughout. Check the
  reference before doing arithmetic across tools.
- **"Unavailable" is not zero.** An account with no balance anchor reports
  `available=false` and `balance=0`. Never present that as "$0" — it means
  the balance is unknown.
