# Accounts & Transfers — Build Spec

Date: 2026-08-16. Design authority:
`docs/superpowers/specs/2026-08-16-accounts-transfers-design.md` (read it
first; this file is the build contract and task ledger source).
Semantic authority: `GLOSSARY.md` (updated in A0 before any code).

## Scope

Introduce Accounts (sidecar `accounts.json`, CSV-file attribution), a
first-class `Transfer` transaction type with leg-pairing (replacing the
drop-on-load filter), stable transaction IDs with lazy sidecar migration,
anchor-based balances with a checking funding projection, the corresponding
UI (accounts settings, transfers page + review queue, dashboard card), and
MCP tools. No database; storage stays CSV + encrypted sidecars.

Out of scope: editing transactions, multi-currency, OFX/QFX import, automatic
transfer execution, changing the near-duplicate engine.

## Architecture summary

New packages: `internal/services/transfers`, `internal/services/accounts`,
`internal/handlers/accounts`, `internal/handlers/transfers`.
Modified: `internal/models` (Account, Transaction fields, Transfer type),
`internal/services/dataloader` (pipeline order — see design §2),
`internal/services/classifier` (patterns become classification signals, not
drop rules), sidecar stores (StableID-first lookup), `internal/services/mcpsvc`,
dashboard/explorer templates.

## Task breakdown

Statuses live in `.swarm/ledger.tsv` (tasks `A0`–`A9`), never here.
Dependency graph:

```
A0 → A2 → A1 → A3 → A7, A9
          A2 → A4 → A5 → A8
          A2, A4 → A6
```

| Task | Scope | Tier | Checks | Acceptance criteria (summary — worker brief carries full text) |
|------|-------|------|--------|----------------------------------------------------------------|
| A0 | GLOSSARY.md: define Account, Transfer type, TransferClass, StableID, BalanceAnchor; amend "no Transfer type" and internal-transfer sections ("filtered" → "classified"). CHANGELOG entry. | 1 | content | Every term used by A1–A9 defined; no remaining claim that transfers are dropped or that no Transfer type exists; wording matches design doc §1–§4. |
| A1 | StableID on Transaction (`accountID\|date\|cents\|n`, post-normalization amount, `file:<basename>` fallback); StableID-first + legacy-hash-fallback lookup and rewrite-on-save in pins, duplicate-decisions, amazon-enrichment stores. | 2 | tests, second | Unit tests: occurrence collisions, fallback hit rewrites to StableID on save, unassigned fallback. Existing sidecar files load unchanged (fixture with legacy keys). No behavior change to dedup. |
| A2 | Account model + `accounts.json` store (encrypted-capable); loader stamps AccountID by FilePatterns (first match, ID sort order); unassigned counting; `Kind: credit` forces credit-card sign convention. | 2 | tests, second | Fixture files map correctly; unmatched file → `AccountID ""` + count exposed; credit override beats ≥70% heuristic in a fixture the heuristic gets wrong; save-time overlap warning. |
| A3 | `internal/services/transfers`: auto-pair / suspected-queue / external-leg classification per design §3; `transfer_decisions.json` store; replace `filterInternalTransfers` with classification stage; income/outflow classifier skips Transfer rows; compatibility count for old FilteredTransfers UI. | 3 | tests, second | Oracle `.swarm/tier3/A3/accept.sh` (written before dispatch) runs fixture matrix: clean pair auto-pairs; pattern-less coincidence queues, never auto-pairs; ambiguous tie queues; external leg typed Transfer/external; confirm and reject decisions persist and survive reload; metrics income/expense totals on fixtures exclude all Transfer rows. |
| A4 | `internal/services/accounts`: BalanceAt (anchor + roll-forward), no-anchor unavailable state, freshness (latest txn date), consecutive-anchor drift report. | 2 | tests, second | Unit tests: single/multi anchor, txn on anchor day excluded (anchor = end of day), no-anchor returns unavailable not 0, drift math on fixture. |
| A5 | Funding projection: 35-day daily roll-forward using recurring engine (this account only); threshold crossing (default 500, per-account override); suggested top-up = shortfall rounded up to $100; median confirmed inbound transfer as reference. | 2 | tests, second | Unit tests with stubbed recurring items: crossing date, min balance, suggestion math, no-crossing case, threshold override. Advisory-only (no writes). |
| A6 | Accounts settings UI (`/accounts`): account CRUD, pattern editor showing live file matches, anchor entry, threshold; wire storage. | 1 | tests, a11y | CRUD round-trips through handler tests; pattern editor lists matched files; ACCESSIBILITY.md points 1–10 pass on the page. |
| A7 | Transfers page (`/transfers`): monthly institution-flow chart with data-table fallback, history (paired + external), suspected-pair review queue (confirm/reject, HTMX) wired to A3 decisions. | 2 | tests, second, a11y | Queue actions persist decisions and update via HTMX with aria-live announcement; chart has table fallback; handler tests for confirm/reject/idempotent re-post; ACCESSIBILITY.md full pass. |
| A8 | Dashboard Accounts card (balance, freshness, low-balance flag icon+text) + unassigned-files banner (dashboard + explorer) + projection summary line. | 1 | tests, a11y | Card renders all fixture states (healthy, stale, low, no-anchor); banner shows/links correctly; flag not color-only; ACCESSIBILITY.md pass. |
| A9 | MCP: `get_accounts`, `get_balance_projection`, `get_transfers`, `set_balance_anchor` (confirm-token), `resolve_transfer` (confirm-token); server description replaces the "cannot answer transfers" clause. | 2 | tests, second | Tool tests per existing mcpsvc conventions; mutating tools refuse without token; description no longer disclaims transfers and references GLOSSARY terms. |

Worker briefs: `.swarm/briefs/A<N>.md` — each contains the relevant design-doc
sections verbatim plus file paths and the full acceptance criteria. Workers
cannot see the lead conversation.

## Run mechanics

- Tasks `A0`–`A9` are appended to the existing `.swarm/ledger.tsv`
  (TAB-separated: task_id, tier, checks, status, attempt, worker, reason).
  Note: the ledger also carries the File Manager run's `P13`–`P15` (pending);
  `swarm/gate.sh done` covers **all** ledger rows, so run completion is
  entangled until those are finished or explicitly parked — sequencing is the
  user's call, recorded here when made.
- Acceptance only via `swarm/gate.sh check A<N>` exit 0 (cwd = this repo);
  escalation scan after every verdict; Tier-3 A3 gets worktrees via
  `swarm/tier3-setup.sh A3` and its oracle written before dispatch.
- `.swarm/critical.globs` additions for this run: `internal/models/*.go`,
  `internal/services/dataloader/*.go`, `internal/services/transfers/*.go`,
  `GLOSSARY.md`.

## Brand / voice

Matches existing SimpleBudget UI: server-rendered, terse labels, no marketing
voice. Money formatted like existing pages. Dark mode parity required
(existing pages ship dark variants; see ACCESSIBILITY.md §12).

## Rulings

(none yet — recorded here as they occur)
