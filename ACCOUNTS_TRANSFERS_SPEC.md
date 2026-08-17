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
          A2 → A6
```

| Task | Scope | Tier | Checks | Acceptance criteria (summary — worker brief carries full text) |
|------|-------|------|--------|----------------------------------------------------------------|
| A0 | GLOSSARY.md: define Account, Transfer type, TransferClass, StableID, BalanceAnchor; amend "no Transfer type" and internal-transfer sections ("filtered" → "classified"). CHANGELOG entry. | 1 | content | Every term used by A1–A9 defined; no remaining claim that transfers are dropped or that no Transfer type exists; wording matches design doc §1–§4. |
| A1 | StableID on Transaction (`accountID\|date\|cents\|n`, post-normalization amount, `file:<basename>` fallback); StableID-first + legacy-hash-fallback lookup and rewrite-on-save in pins, duplicate-decisions, amazon-enrichment stores. | **3** | tests, second | Unit tests: occurrence collisions, fallback hit rewrites to StableID on save, unassigned fallback. Existing sidecar files load unchanged (fixture with legacy keys). No behavior change to dedup. |
| A2 | Account model + `accounts.json` store (encrypted-capable); loader stamps AccountID by FilePatterns (first match, ID sort order); unassigned counting; `Kind: credit` forces credit-card sign convention. | **3** | tests, second | Fixture files map correctly; unmatched file → `AccountID ""` + count exposed; credit override beats ≥70% heuristic in a fixture the heuristic gets wrong; save-time overlap warning. |
| A3 | `internal/services/transfers`: auto-pair / suspected-queue / external-leg classification per design §3; `transfer_decisions.json` store; replace `filterInternalTransfers` with classification stage; income/outflow classifier skips Transfer rows; compatibility count for old FilteredTransfers UI. | 3 | tests, second | Oracle `.swarm/tier3/A3/accept.sh` (written before dispatch) runs fixture matrix: clean pair auto-pairs; pattern-less coincidence queues, never auto-pairs; ambiguous tie queues; external leg typed Transfer/external; confirm and reject decisions persist and survive reload; metrics income/expense totals on fixtures exclude all Transfer rows. |
| A4 | `internal/services/accounts`: BalanceAt (anchor + roll-forward), no-anchor unavailable state, freshness (latest txn date), consecutive-anchor drift report. | 2 | tests, second | Unit tests: single/multi anchor, txn on anchor day excluded (anchor = end of day), no-anchor returns unavailable not 0, drift math on fixture. |
| A5 | Funding projection: 35-day daily roll-forward using recurring engine (this account only); threshold crossing (default 500, per-account override); suggested top-up = shortfall rounded up to $100; median confirmed inbound transfer as reference. | 2 | tests, second | Unit tests with stubbed recurring items: crossing date, min balance, suggestion math, no-crossing case, threshold override. Advisory-only (no writes). |
| A6 | Accounts settings UI (`/accounts`): account CRUD, pattern editor showing live file matches, anchor entry, threshold; wire storage. | 1 | tests, a11y | CRUD round-trips through handler tests; pattern editor lists matched files; ACCESSIBILITY.md points 1–10 pass on the page. |
| A7 | Transfers page (`/transfers`): monthly institution-flow chart with data-table fallback, history (paired + external), suspected-pair review queue (confirm/reject, HTMX) wired to A3 decisions. | 2 | tests, second, a11y | Queue actions persist decisions and update via HTMX with aria-live announcement; chart has table fallback; handler tests for confirm/reject/idempotent re-post; ACCESSIBILITY.md full pass. |
| A8 | Dashboard Accounts card (balance, freshness, low-balance flag icon+text) + unassigned-files banner (dashboard + explorer) + projection summary line. | 1 | tests, a11y | Card renders all fixture states (healthy, stale, low, no-anchor); banner shows/links correctly; flag not color-only; ACCESSIBILITY.md pass. |
| A9 | MCP: `get_accounts`, `get_balance_projection`, `get_transfers`, `set_balance_anchor` (confirm-token), `resolve_transfer` (confirm-token); server description replaces the "cannot answer transfers" clause. | 2 | tests, second | Tool tests per existing mcpsvc conventions; mutating tools refuse without token; description no longer disclaims transfers and references GLOSSARY terms. |

**Graph amended 2026-08-16:** A6 originally listed A4 as a prerequisite. It is
not one — A6's scope is account CRUD, the file-pattern editor, anchor *entry*
and the threshold field, none of which read a balance. Displaying balances is
A8. A6 needs only A2's Account model and store, so it may run in parallel with
A1 and A4.

Worker briefs: `.swarm/briefs/A<N>.md` — each contains the relevant design-doc
sections verbatim plus file paths and the full acceptance criteria. Workers
cannot see the lead conversation.

## Run mechanics

- Tasks `A0`–`A9` are appended to the existing `.swarm/ledger.tsv`
  (TAB-separated: task_id, tier, checks, status, attempt, worker, reason).
  Note: the ledger also carries the File Manager run's `P1`–`P15`, all
  **accepted** as of 2026-08-16. `swarm/gate.sh done` covers **all** ledger
  rows, so it will fail on the `A*` rows until this run completes — which is
  correct, not a regression.
- Acceptance only via `swarm/gate.sh check A<N>` exit 0 (cwd = this repo);
  escalation scan after every verdict; Tier-3 A3 gets worktrees via
  `swarm/tier3-setup.sh A3` and its oracle written before dispatch.
- `.swarm/critical.globs`: **only `internal/services/transfers/**` was added**
  (user decision 2026-08-16, "targeted"). `internal/services/dataloader/**`
  was already present from the File Manager run and is deliberately kept.
  `internal/models/*.go` and `GLOSSARY.md` were NOT added — too broad, they
  would escalate nearly every task in the run for little gain.

  Consequence, recorded up front rather than discovered via a wasted
  verification cycle: **A1 and A2 are pre-assigned Tier 3**, because both must
  modify code under `internal/services/dataloader/` (A1 assigns StableID inside
  the load pipeline and touches the duplicate-decisions sidecar; A2 stamps
  AccountID during load), and the gate escalates any task whose manifest hits a
  critical glob. A3 was already Tier 3. So this run has three Tier-3 tasks:
  A1, A2, A3 — each needing the `worker-local` substitution decision (ruling
  2026-08-16c) until that infrastructure is fixed.

## Brand / voice

Matches existing SimpleBudget UI: server-rendered, terse labels, no marketing
voice. Money formatted like existing pages. Dark mode parity required
(existing pages ship dark variants; see ACCESSIBILITY.md §12).

## Rulings

**2026-08-16d — validating an oracle against a featureless tree is not
enough.** The A2 oracle was written and validated before dispatch, showing
3 pass / 7 fail on a tree without the feature — which looked like the P15
precedent. It nonetheless shipped three defects, found by the first blind
worker and verified independently: `probeDataDir` discarded the `error` from
`storage.New` (a compile error), the attribution fixture wrote identical
content to both files so exact-hash dedup collapsed them and the assertion was
unsatisfiable, and the probe called `dl.Transactions()`, an accessor absent
from the pinned API that one worker then invented purely to satisfy it.

The failure mode is specific: on a featureless tree the probe fails to compile
for a *legitimate* reason (the package under test does not exist yet), which
masks a probe that would not compile anyway. Both implementations would have
failed the same two checks and produced a zero diff that reads as agreement.

Ruled, binding on A3 and any later Tier-3 task: **an oracle must be validated
at both ends before dispatch** — it must fail on a featureless tree (proving
the checks discriminate) AND pass against at least one real implementation
(proving they are satisfiable). When no implementation exists yet, hand-write
the minimum stub needed to compile the probe and run it once, or accept that
the first worker's report is part of oracle validation and budget a repair
cycle. Repairing an oracle mid-flight is legitimate and must be recorded in
the divergence report, as it was here.

**2026-08-16e — probe assertions must be non-vacuous.** A2's
checker-tests found that `TestProbeA2_CreditKindForcesSignFlip` looped over the
loaded transactions asserting none stayed positive, which passes trivially on
an empty set — weaker than the worker's own test on precisely the axis the
probe exists to police. Both affected probe tests now assert the fixture row
count first. Any loop-based probe assertion must be preceded by a count
assertion.

**Carry-forward for A6/A7 (not a defect):**
`internal/handlers/explorer/handlers.go:477` constructs
`dataloader.New(importDir, store)` where the CSV directory is deliberately NOT
`store.BaseDir()`, so folder-import files are matched against the user's real
accounts. Intended, but easy to break; do not assume CSV dir == data dir.

**2026-08-16f — an oracle must exercise the legacy consumers, not just the new
helper.** A1's two blind implementations both passed the oracle 10/10 and were
not equivalent. Both rekey the pins sidecar to StableID, but three shared
lookups index that map by `Transaction.Hash`
(`insights/trends.go:38`, `majorexpenses/engine.go:125`, and
`AnnotateRecurringPayments`). One implementation fixed all three; the other
left them untouched, so after the first rekey every pin silently stops
resolving in the dashboard, explorer, insights and the MCP spend tools — no
error, no log line, the pin still on disk.

The oracle could not see it because it verifies `PinFor`, the NEW resolution
path, and never exercises the OLD call sites that consume the same map.

Ruled, binding on A3 and later: when a task changes how stored data is keyed,
the oracle must assert on at least one **existing consumer's observable
output** (e.g. "a pinned transaction still shows its major-expense name via the
insights path"), not merely on the new accessor. This is ruling 2026-08-16a's
shape again — green tests over a feature broken where the user looks.

**Known test gap, accepted at A2:** the early-exit `setUnassignedCount(0)`
reset paths in `LoadDataContext` are correct but not covered — deleting those
two lines leaves the suite green. Worth a test when A4 next touches that
function.
