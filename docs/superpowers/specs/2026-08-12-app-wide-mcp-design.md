# App-wide MCP — design

Expand MCP coverage from the what-if planner to the whole SimpleBudget app:
spending analysis, major-expense curation, and housekeeping. The transport
changes from a second process reading the data files to an MCP endpoint served
by `cmd/server` itself.

Supersedes the transport half of `2026-08-09-whatif-mcp-server-design.md`. The
tool semantics in that document still hold for the what-if tools.

## Why the transport changes

Today `cmd/whatif-mcp` is a separate process. It reads the data files directly
via `storage.New(settingsDir)` and writes only through the running server's HTTP
API, which it must first authenticate: `GET /whatif/state` returns
`{app, settings_dir, active, revision}`, and `whatifmcp.Client.State` refuses to
write unless `app == "budget2"` and the absolute settings directory matches its
own. `apply_changes` then snapshots the scenario and POSTs
`/whatif/apply` with `expected_scenario`, compared inside the settings manager's
write lock so a browser scenario switch cannot absorb the write.

That machinery exists because two processes touch one dataset. It works for a
handful of read-only tools plus one narrow write. It does not scale to parity:

- Every write page would need a hand-built JSON twin of its form handler. The
  HTMX handlers cannot express sparse writes — `parseFormFloat` returns
  `(0, nil)` for an absent key, which is why `/whatif/apply` exists at all
  (a partial POST to `/whatif/roth-conversion` *disables* conversions).
- Two data paths drift. `docs/whatif-mcp-followups-2026-08-09.md` §3 records the
  MCP binary detecting encryption in the wrong directory, and §4 records
  `list_scenarios` reporting an `active` scenario a separate process cannot
  know.

Serving MCP from inside `cmd/server` removes the second reader. Those bug
classes stop existing rather than being defended against.

## Topology

**Package tree.** `internal/services/mcpsvc` — the name avoids colliding with
the SDK's `mcp` import — with one subpackage per domain:

| Package | Covers |
|---------|--------|
| `mcpsvc/plan` | what-if planner (today's tools) |
| `mcpsvc/spend` | transactions, spending summaries, recurring, trends, anomalies, price creep |
| `mcpsvc/curate` | major expenses and exception triage |
| `mcpsvc/admin` | duplicates, data files, status, backup, guarded destructive ops |

Each exports `Register(s *mcp.Server, deps Deps)`. `mcpsvc.NewServer(deps)`
assembles them and returns the `*mcp.Server`.

**Dependencies are passed explicitly** — `Deps{Loader, RetirementMgr, Store,
BackupService, Snapshotter, Cfg}` — mirroring how `dashboard.Initialize(...)`
and friends are wired in `SetupDependencies`. Domain packages never read handler
package globals.

**Mount.** `SetupRouter` mounts `mcp.NewStreamableHTTPHandler(getServer, opts)`
(go-sdk v1.7.0, a plain `http.Handler`) at `/mcp`, wrapped in
`http.NewCrossOriginProtection().Handler(...)`. The SDK's localhost DNS-rebinding
protection stays on (`DisableLocalhostProtection` remains false). `opts` sets an
idle `SessionTimeout`.

**Client config.** `.mcp.json` becomes:

```json
{"mcpServers": {"budget2": {"type": "http", "url": "http://localhost:8080/mcp"}}}
```

**`cmd/whatif-mcp` is retired.** With MCP served in-process there is no second
reader to keep alive. The cross-process defenses go with it: `/whatif/state`
identity verification, absolute settings-dir comparison, `expected_scenario`
racing the browser, and `EnsureServer`'s spawn logic. `Snapshotter` stays — a
pre-write `.bak` is undo, not verification.

The accepted cost: if `cmd/server` is not running when a Claude Code session
starts, the tools are absent. The go-sdk provides no stdio↔HTTP proxy helper, so
preserving today's auto-spawn would mean hand-writing a transport pump (session
IDs, SSE framing). Not worth it on speculation. Revisit only if launch friction
proves real in practice.

`GET /whatif/state`, `POST /whatif/apply`, and `GET /whatif/poll` remain — the
what-if page's own polling uses them.

**Fate of `internal/services/whatifmcp`.** Its tool registrations, shaped views
(`view.go`, `months.go`, `overrides.go`, `scenarios.go`, `insights.go`) and
`assumptions.md` move to `mcpsvc/plan`, with the insights tools continuing on to
`mcpsvc/spend`. `snapshot.go` moves to `mcpsvc` as shared write infrastructure.
`live.go` — the HTTP client and its identity verification — is deleted. Tests
move with their code.

## Tool surface

### `plan`

`list_scenarios`, `get_analysis`, `get_months`, `run_scenario`, `apply_changes`,
`open_page`, plus the `whatif://assumptions` resource. Shapes unchanged; only the
call path underneath changes.

### `spend` (read-only)

| Tool | Returns |
|------|---------|
| `search_transactions` | rows matching date range, category, text, type, amount bounds; paginated |
| `summarize_spending` | totals by category, month, and merchant for a window, plus budget-vs-target metrics |
| `get_recurring` | recurring payments with subscription flag and major-expense annotation |
| `get_trends` | category and major-expense trends, income patterns, spending velocity |
| `get_anomalies` | relocated from the current server, unchanged |
| `get_price_creep` | relocated from the current server, unchanged |

### `curate` (read + write)

| Tool | Effect |
|------|--------|
| `list_major_expenses` | definitions with match counts and totals |
| `list_exceptions` | the three exception buckets, searchable by text/amount/date |
| `pin_transactions` | pin one transaction or every transaction in a filter to a bucket |
| `upsert_major_expense` | create or edit a definition, including internal-transfer mode |
| `delete_major_expense` | soft-delete, matching the page's restore semantics |

### `admin`

| Tool | Effect |
|------|--------|
| `list_duplicates` | pending duplicate groups |
| `resolve_duplicates` | resolve a group |
| `undo_resolve` | undo the last resolution |
| `list_data_files` | CSV inventory with date coverage |
| `get_status` | lock/encryption state, backup status, revision, active scenario, data dir |
| `run_backup` | trigger a backup |
| `restore_backup` | **guarded** — overwrites the live data directory |
| `set_encryption` | **guarded** — enable/disable, change auth method |
| `shutdown_server` | **guarded** — the existing `/killme` path |

## Write safety

Every write tool: `Snapshotter.Snapshot(target)` → the owning service's write
path, under that service's own lock → return `{changed, revision}`. Because the
tools call the same managers the UI calls, the settings manager's revision
counter and its restore gate apply unchanged.

**Guarded operations** (`restore_backup`, `set_encryption`, `shutdown_server`)
are two-step. The first call performs nothing and returns a preview — for
restore, which files would be overwritten; for encryption, the current state and
what would change — together with a single-use confirm token bound to the tool
name and a hash of the arguments, with a short TTL, held in memory and dropped on
restart. A second call must echo that token or nothing happens. Restore snapshots
before writing. The UI has a human clicking; a tool gets called by a model
reading prose, so the confirmation must be in the protocol.

**Known limitation.** Only the what-if page polls (`/whatif/poll`). An MCP write
to major expenses or duplicates will not refresh an already-open tab; that page
shows stale data until reloaded. Generalizing the poll mechanism across pages is
separate work, deliberately out of scope here.

## Where the logic lives

`majorexpenses`, `anomalies`, `pricecreep`, `merchants`, and `retirement` are
already services and are callable as-is. Insights and dashboard are not: the
analysis lives unexported inside the handler packages.

- `internal/handlers/insights/handlers.go` — `detectRecurringPaymentsAt`,
  `detectByAmount`, `mergeSimilarGroups`, `analyzeCategoryTrends`,
  `analyzeMajorExpenseTrends`, `AnalyzeIncomePatterns`,
  `calculateSpendingVelocity`, `isSubscription`,
  `annotateRecurringWithMajorExpense` — roughly 1,000 lines under 2,767 lines of
  tests.
- `internal/handlers/dashboard/handlers.go` — `calculateMetrics`,
  `calculateComparison`, `bucketMajorExpenses`, the chart builders.

This extraction, not the transport work, is the bulk of the effort.

**Strategy: incremental, per tool.** Each `spend` tool moves only the logic it
needs into `internal/services/insights` or `internal/services/dashboard`; the
handler then delegates to the service, and the logic's existing tests move with
it. The suite stays green at every step and no single diff spans thousands of
test lines. Extracted functions keep their behavior exactly — this is a move,
not a rewrite.

`search_transactions` needs no extraction: `models.TransactionSet` already has
composable `FilterByDateRange` / `FilterByCategory` / `FilterBySearch` /
`GroupByCategory` / `CategoryTotals` / `MonthlyTotals` / `Paginate`.

## Errors

- Locked storage returns "storage is locked; unlock in the UI" — not a JSON
  parse failure. Using the server's own `Store` makes this correct by
  construction, closing followups §3.
- A missing data directory or an empty CSV set returns an empty result with an
  explanatory note, not an error.
- Tool handlers keep the existing panic-wrapping so a bug in one tool cannot take
  down the server.

## Testing

- Per-domain tool tests drive a real `mcp.Client` over an in-memory transport,
  following the pattern in `internal/services/whatifmcp/server_test.go`.
- A router test asserts `/mcp` is mounted, answers `initialize`, and rejects a
  cross-origin request.
- Guarded-operation tests assert that a first call mutates nothing, that a wrong
  or expired token is refused, and that a replayed token is refused.
- Extraction moves existing tests alongside the code. No coverage loss is
  acceptable; verify per package before and after each move.

## Phases

Each phase gets its own implementation plan.

1. **Transport.** `mcpsvc` skeleton, `/mcp` mount, `plan` tools migrated,
   `.mcp.json` updated, `cmd/whatif-mcp` retired, README note.
2. **`spend`.** Read tools, with insights/dashboard extraction as each tool
   requires it.
3. **`curate`.** Major-expense reads and writes.
4. **`admin`.** Housekeeping, then the three guarded operations last.

## Out of scope

- The three wrong caveats on master recorded in
  `docs/whatif-mcp-followups-2026-08-09.md` §1 — the
  `MedicareEligibleAdultCountAtYear` comment and the two RMD joint-life-divisor
  claims, one of which renders on the RMD card. Real bugs, but engine and UI
  text, not MCP.
- Generalizing `/whatif/poll` to other pages.
- A stdio↔HTTP proxy shim.
