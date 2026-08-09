# What-If MCP Server — Design

**Date:** 2026-08-09
**Status:** Approved (design); implementation plan pending

## Problem

The what-if planner produces a large, interconnected analysis — projection,
budget fit, RMD schedule, tax summary, IRMAA, Monte Carlo, sensitivity, failure
points, tax-optimizer recommendation — and renders it as cards. The numbers are
there; the *reasoning* behind them is not. Why RMDs jump in a particular year,
which assumption a result is most sensitive to, what the optimizer's
recommendation actually rests on, and where the model is weakest are all
questions the UI cannot answer.

The goal is to be able to hold a conversation about a plan — asking what a
number means, why it moved, and what happens under a different assumption —
with something that can read the live analysis and re-run the engine to check
its own claims.

## Decisions taken before this design

Recorded so the reasoning survives:

1. **Advisory framing.** The assistant speaks as an advisor making
   recommendations for this household, not as a neutral explainer. The user
   chose this with the objection on the table. What keeps it honest is that the
   engine's documented blind spots are published to the client (see
   *Assumptions resource*), so recommendations don't rest on things the model
   doesn't capture.
2. **Derived analysis only.** Computed figures — balances, projections,
   tax/RMD/IRMAA, optimizer output — are in scope. Transaction history, major
   expenses, and the rest of the dashboard are not.
3. **Read plus run, never write.** The server may read scenarios and run the
   engine with modified settings. It may not write to `data/`.
4. **MCP over an in-app chat panel.** An in-app panel would call the Messages
   API and bill API credits; a Claude Pro/Max subscription does not cover
   programmatic API access. An MCP server puts the model call in Claude
   Code/Desktop, on the subscription. The trade is a second window instead of a
   pane on the what-if page.

## Non-goals

- No writes to `data/`. Nothing in this server mutates a saved scenario.
- No network listener. stdio transport only.
- No engine or analysis changes. This is a read-and-run consumer of existing
  packages; if a change to `internal/services/retirement` seems necessary, that
  is a signal the design is wrong.
- No credentials. The server holds no API key and performs no outbound network
  calls.

## Architecture

A second binary, `cmd/whatif-mcp/`, alongside the existing `cmd/server`,
`cmd/validate`, and `cmd/enrich-amazon`. Claude Code launches it as a
subprocess and speaks MCP over stdio — no port, no listener, no auth surface,
nothing reachable from the network.

```
Claude Code  ──stdio──▶  cmd/whatif-mcp
                              │
                              ├── internal/services/storage   (read scenarios)
                              ├── internal/services/retirement/prepare
                              ├── internal/services/retirement/engine
                              └── internal/services/retirement/analysis
```

Scenario files are read through the existing settings/storage layer rather than
parsed directly, so the server sees exactly what the web app sees. Engine and
analysis calls are ordinary in-process function calls.

**Dependency cost:** `go.mod` gains the official MCP Go SDK
(`github.com/modelcontextprotocol/go-sdk`, v1.7.0 at time of writing, tracking
MCP spec 2026-07-28) — 6 direct dependencies to 7.

Server shape: `mcp.NewServer` with an `mcp.Implementation`, `mcp.AddTool` per
tool, and `server.Run(ctx, &mcp.StdioTransport{})`. Tool handlers are generic
over typed Go structs —
`func(ctx, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error)` — so
**input schemas are inferred from the argument struct** rather than
hand-written JSON Schema. The tool argument shapes below should therefore be
defined as Go structs with JSON tags, and the schema follows from them.

## Response shaping — the central constraint

`models.WhatIfAnalysis` is not safe to return raw. `Projection.Months` alone is
360 `ProjectionMonth` records for a 30-year plan, and the struct also carries
`YearlySummaries`, Monte Carlo distributions, sensitivity results, failure
points, and up to five scored tax-optimizer candidates each holding their own
`WhatIfSettings`. Returned verbatim, a single call would consume tens of
thousands of tokens of mostly-unread detail and crowd out the conversation it
is meant to support.

Every tool therefore returns a **shaped view**, and per-month detail is a
separate, range-bounded call. Shaping rules:

- Include headline scalars and per-*year* series.
- Exclude per-*month* series unless explicitly requested.
- Exclude embedded `*WhatIfSettings` copies (the optimizer candidates carry
  them); reference candidates by index and summary instead.
- Round currency to whole dollars. The engine's sub-cent precision is
  meaningful internally and noise in a conversation.

## Tool surface

Four tools, each shaped around a question a person actually asks rather than
one per internal function.

### `list_scenarios`

No arguments. Returns each saved scenario with filename, display name, whether
it is active, and a one-line summary (portfolio value, projection years,
survives/depletes). Orientation call — cheap, safe to call first.

### `get_analysis`

`{ scenario?: string }` — filename from `list_scenarios`; omitted means the
active scenario. The same optional-with-active-default rule applies to
`get_months` and `run_scenario`.

Returns the shaped analysis:

- **Headline** — final balance, survives, depletion year/age if any,
  sustainability score and label.
- **Budget fit** — monthly expenses, income, taxes, IRMAA, RMD, gap.
- **Per-year series** — from `ProjectionYearSummary`: starting/ending balance,
  growth, MAGI, taxes, NIIT, IRMAA, expenses, withdrawals.
- **RMD** — start age, years until start, tax-deferred value, 10-year total,
  and the per-year RMD schedule.
- **Tax** — total federal/state/total paid, average effective rate, conversion
  tax paid, per-year tax summary.
- **Monte Carlo** — stats only, not the full distribution.
- **Tax optimizer** — eligibility, baseline vs best summary, candidate count.
  Not the embedded settings.

### `get_months`

`{ scenario?: string, from_month: int, to_month: int }` — the drill-in, for
when a per-year number needs explaining. The range is required and bounded
(reject spans over 120 months) so 360 rows cannot be returned by accident.

### `run_scenario`

`{ scenario?: string, overrides: { … } }` — applies a sparse override set to a
deep copy of the named scenario, runs the engine, and returns the same shaped
view as `get_analysis`, plus an echo of the overrides applied.

Supported overrides, chosen because they are the levers the cards actually
expose: monthly living expenses, healthcare inflation, general inflation,
investment return, Roth conversion (annual amount, start year, end year),
Social Security claim ages, projection years, filing status.

Overrides apply to a **deep copy**. `prepare.DeepCopy` round-trips through
JSON, so any field tagged `json:"-"` will not survive — `RothConversion.PerYearOverrides`
is the known instance and must be re-attached explicitly after copying, the same
workaround `analysis/tax_optimizer.go:90-98` already applies.

## Assumptions resource

The engine's documented limitations are exposed as an MCP resource rather than
compiled into a prompt, so they live next to the code they describe and are read
by the client on demand:

- No mortality modeling; no survivor's penalty; filing status frozen for the
  horizon.
- Tax-deferred savings are a single household pool driven by the older member's
  age; account ownership is not modeled.
- IRMAA is computed at annual granularity, and eligibility turns on the plan
  anniversary rather than the birthday.
- IRMAA surcharge dollars are indexed to an assumed 5.5% Medicare per-capita
  cost growth, thresholds to plan CPI.
- No QCDs, no tax-exempt muni interest, no itemized deductions, no enhanced
  senior deduction.
- The reported marginal rate is the statutory bracket; it excludes the §86
  Social Security phase-in and the IRMAA cliff.
- `LifetimeTaxReal` excludes IRMAA.

This list is the direct output of the RMD/IRMAA audit and its fixes; it should
be updated whenever an engine limitation is added or removed.

## Wiring

A checked-in `.mcp.json` at the repo root registers the server so it is
available from Claude Code without per-machine setup. The binary resolves the
data directory the same way `cmd/server` does, with an override flag for
pointing at a copy.

## Error handling

- **Unknown scenario** — error naming the requested file and listing valid
  names, so the client can retry without another round trip.
- **Malformed scenario file** — surfaced as a tool error with the parse
  failure; never a panic.
- **Engine panic during a run** — recovered at the tool boundary and returned
  as a tool error. A panic must not kill the stdio session.
- **Out-of-range `get_months`** — rejected with the valid range stated.
- **Override that would produce an invalid scenario** (negative expenses,
  claim age outside 62–70) — rejected before the engine runs, naming the field.

## Testing

Everything below the MCP transport is a pure function over settings and
analysis structs, so the suite needs no network, no key, and no subprocess:

- Shaped-view construction: per-month series excluded, embedded settings
  excluded, currency rounded, headline fields populated. Table-driven.
- Override application: each supported override changes the intended field and
  nothing else; `PerYearOverrides` survives the deep copy; invalid values are
  rejected with a field-naming error.
- `get_months` range bounding, including the rejection path.
- Scenario listing against a fixture data directory.

Tests build inputs with `prepare.MustFrom`, following the existing convention
in `analysis/helpers_test.go`. `make check` stays hermetic.

## Risks

- ~~**MCP Go SDK maturity.**~~ *Resolved 2026-08-09:* the official SDK is
  published, past 1.0 (v1.7.0), and exposes the server/tool/stdio API this
  design assumes. No hand-rolled JSON-RPC fallback needed.
- **Shaped views drift from the analysis structs.** A new field on
  `WhatIfAnalysis` will not appear in the shaped view automatically. Accepted:
  silent omission is the safer failure than unbounded context growth.
- **Advisory framing depends on the client.** With no system prompt, the tool
  descriptions and the assumptions resource are the only things grounding
  recommendations. Tool descriptions are therefore part of the contract, not
  incidental documentation, and should be reviewed as such.
