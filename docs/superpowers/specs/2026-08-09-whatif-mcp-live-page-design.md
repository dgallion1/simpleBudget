# What-If MCP — Live Page Design

Extends `docs/superpowers/specs/2026-08-09-whatif-mcp-server-design.md`. That
design deliberately made every tool read-only. This one adds a write path and a
way for an open browser tab to notice the write, so a plan can be discussed in
Claude Code and watched in the browser at the same time.

## Problem

The MCP server answers questions about a retirement plan but cannot change one,
and the what-if page cannot learn that anything changed. Discussing an idea and
seeing it are two disconnected activities: you read figures in chat, then re-enter
them by hand in the browser to look at the charts.

Two independent gaps cause this.

**Nothing writes.** `run_scenario` applies overrides to an in-memory copy and
throws it away. That is correct for previewing a claim and useless for pursuing one.

**Nothing pushes.** The codebase has no SSE, no WebSocket, and no polling —
verified by searching for `EventSource`, `text/event-stream`, `websocket`,
`hx-sse`, `sse-connect`, and `hx-trigger="every` across `.go`, `.html`, and `.js`.
The what-if page re-renders only in response to an HTMX request the browser itself
initiates. A write from any other process leaves an open tab showing stale figures
until it is manually reloaded.

## Decisions taken before this design

Settled with the user before drafting:

1. **Direction.** Ideas are pushed from chat into the live page. Chat drives, the
   browser displays.
2. **Write target.** The active scenario, not a scratch copy — but a snapshot is
   taken before the first write, so any change can be rolled back.
3. **Refresh.** A settings revision counter plus HTMX polling. Not SSE (more
   moving parts), not Playwright driving the real UI (slow, brittle, and only
   works in a browser the MCP launched).
4. **Write surface.** Exactly the eleven fields `Overrides` already supports.
   Asset allocation, spending phases, healthcare persons, income and expense
   sources, big-ticket items, guardrails, glide path, and chains are out of scope
   for v1.
5. **Server lifecycle.** An `open_page` tool health-checks the server, launches
   it if nothing is listening, and returns the URL. It does not open a browser.
6. **Package layering.** `Overrides` and `Apply` move out of `whatifmcp` into
   their own package so the web handler can use them without importing a package
   named for a transport it does not speak.

## Non-goals

- SSE or WebSocket transport.
- Launching or driving a browser from the MCP.
- Writing any settings field outside the eleven in `Overrides`.
- Scratch or sandbox scenarios.
- Undo tooling beyond the `.bak` files the snapshot leaves behind.
- Exposing the tax optimizer, per-year RMD schedules, or the other items in
  `docs/whatif-mcp-followups-2026-08-09.md` §2.

## The central constraint — why not post the existing forms

The obvious implementation is for the MCP to post the same forms the browser
posts. Two of the three relevant routes make that unsafe.

`POST /whatif/settings` **is** safe. `handleWhatIfSettings` builds a sparse
`updates` map and calls `UpdateSettings(updates)`; an absent form key leaves the
field alone, and a key present with `"0"` persists an explicit zero. That matches
`Overrides`' nil-means-unchanged semantics exactly.

`POST /whatif/roth-conversion` is **not** safe. `handleWhatIfRothConversion` reads
the whole form:

```go
settings.RothConversion.Enabled = r.FormValue("enabled") == "on"
```

A post carrying only `annual_amount` silently sets `Enabled` to `false`, disabling
the conversions it was meant to size.

`POST /whatif/social-security` is **not** safe. `handleWhatIfSocialSecurity`
assigns `ClaimAge` unconditionally from the form, so a post carrying only
`spouse_claim_age` resets the primary claim age. Worse, it ends with:

```go
if settings.SocialSecurity.FRABenefit <= 0 {
    settings.SocialSecurity = nil
}
```

A partial post therefore **deletes the entire Social Security configuration**.

Both handlers are correct for their actual caller — a browser submitting a
complete form. Five of the eleven `Overrides` fields route to them. Reconstructing
full forms inside the MCP would duplicate handler knowledge, and would silently rot
the next time a field is added to either form.

**Resolution:** one typed sparse endpoint that reuses `Apply` server-side. Preview
and commit become the same code path:

```
run_scenario   → Apply(settings, overrides)             → analysis   (MCP process, no write)
apply_changes  → POST /whatif/apply → Apply(…) → Save   → analysis   (server process, writes)
```

`Apply` already deep-copies, validates with field-named errors, and has tests. The
write path adds `Save` and nothing else.

## Architecture

### Web app (`cmd/server`)

**Revision counter.** A `revision int` on `SettingsManager`, guarded by the
existing `sm.mu`. Bumped in `saveInternal` rather than `Save`, because
`saveInternal` is the common tail of all eight write paths (`Save`,
`UpdateSettings`, `CreateScenario`, and the rest); bumping in `Save` alone would
miss most of them. Also bumped in `SwitchScenario`, which changes what the page
should display without writing a file. Read via a `Revision()` accessor under
RLock.

The counter is in-memory and not persisted. It only has to be monotonic within one
server process, because a page load always reads the current value as its baseline.

**`GET /whatif/state`** returns identity and state together:

```json
{"app":"budget2","settings_dir":"/home/darrell/bin/ai/budget2/data/settings",
 "active":"whatif.json","revision":42}
```

`settings_dir` is the absolute resolved path. `app` is the literal string
`"budget2"`. Both exist so a client can prove it is talking to the right server
about the right plan — see *Instance resolution*.

**`POST /whatif/apply`** accepts the `Overrides` JSON shape, loads the active
scenario, calls `Apply`, calls `Save`, and returns:

```json
{"scenario":"whatif.json","applied":{…},"revision":42}
```

**`GET /whatif/poll?since=N`** returns `204 No Content` when `revision == N`, and
otherwise the `whatif-results` partial re-rendered with `since` set to the current
revision. HTMX performs no swap on a 204, so the unchanged case costs one integer
comparison and runs no analysis — which is what makes a 2s poll acceptable.

An absent or malformed `since` is treated as `0`, producing a full render. That is
the safe direction: a bad parameter shows fresh figures rather than suppressing them.

**Template.** The `whatif-results` container gains:

```html
hx-get="/whatif/poll?since=42" hx-trigger="every 2s" hx-swap="outerHTML"
```

`outerHTML` replaces the container along with its own attributes, so the new
`since` rides along in the response and needs no JavaScript bookkeeping.

Two existing behaviors fall out of this for free:

- `charts.js:531` already reloads charts on `htmx:afterSettle` for a
  `#whatif-results` swap, so a polled update redraws charts on the same path a
  user-driven update does.
- The mutating handlers render the same partial, so a change you make with a
  slider returns HTML already carrying the fresh `since`. The page does not then
  redundantly re-fetch.

### Overrides package move

`Overrides`, `Apply`, `validate`, and their tests move from
`internal/services/whatifmcp` to `internal/services/retirement/overrides`.
`whatifmcp` and `handlers/whatif` both import the new package. No logic changes.

This is a mechanical move, but it is not cosmetic: without it a web handler
imports `whatifmcp`, which reads as the HTTP layer depending on the MCP layer when
the true dependency is on a settings-mutation vocabulary that neither owns.

`RunWithOverrides` stays in `whatifmcp` — it is MCP response shaping, not
settings mutation.

### MCP (`internal/services/whatifmcp`)

**`live.go`** — an HTTP client to the running server, with short timeouts:

- `State(ctx) (State, error)`
- `Apply(ctx, Overrides) (ApplyResult, error)`
- `EnsureServer(ctx, settingsDir) (State, error)` — probe, spawn if refused, wait
  for `/api/health`, then verify identity.

**`snapshot.go`** — a `Snapshotter` holding the set of scenarios already
snapshotted by this process. `Ensure(scenario)` copies the scenario file to
`<name>.<RFC3339>.bak` in the settings directory and records it.

Once per **(process, scenario)** — not once per process. Switching scenarios in
the UI mid-conversation must produce a second snapshot, because the first one
backs up a different plan.

**Reads stay file-based.** They are faster and still work with the server down.
The single change: resolving the *active* scenario asks `/whatif/state` when a
verified server is reachable, falling back to `whatif.json`. This closes
`docs/whatif-mcp-followups-2026-08-09.md` §4 — the active filename was in-process
state in the web server, so a separate MCP process always reported `whatif.json`
regardless of what the UI had selected.

### Instance resolution

The MCP resolves settings from its own flag; the server resolves from
`BUDGET_DATA_DIR`. Nothing makes them agree, and
`docs/whatif-mcp-followups-2026-08-09.md` §3 already records that the MCP ignores
`BUDGET_DATA_DIR` entirely. If the two diverge, reads describe one plan while
writes land on another, with no symptom. `scripts/whatif-verify.sh` makes this
concrete: it runs instances on `:8099` against a throwaway copy in `/tmp`.

`GET /api/health` returns only `{"status":"ok"}`, which cannot distinguish budget2
from anything else on the port. Hence the identity fields on `/whatif/state`.

Resolution order:

1. **Own settings dir `S`** — the `-data` flag, else `BUDGET_DATA_DIR/settings`,
   else `./data/settings`. Honoring `BUDGET_DATA_DIR` here closes §3.
2. **Candidate URL** — `BUDGET_SERVER_URL`, else `http://localhost:8080`
   (`config.DefaultConfig` listens on `:8080`).
3. **Probe `GET /whatif/state`:**
   - *Connection refused* — no instance. `open_page` spawns `cmd/server` with
     `BUDGET_DATA_DIR` set to `S`, so the two agree by construction. A write
     attempted before that fails telling the caller to run `open_page`.
   - *200, `app == "budget2"`, `settings_dir == S`* — adopt. Reads take `active`
     from it; writes go to it.
   - *200, `settings_dir != S`* — **refuse to write**, reporting both paths.
   - *200, `app != "budget2"` or unparseable* — refuse, reporting what answered.

Never spawn when a healthy instance is already on the URL; a second `cmd/server`
would fail to bind anyway.

## Tool surface

### `open_page`

Ensures a server is running and returns where to look.

Input: `scenario` (optional) — switches to that scenario first via
`POST /whatif/scenarios/switch`.

Output: `{url, started, active, revision}`. `url` is the `/whatif` page. `started`
distinguishes "I launched this" from "it was already up".

### `apply_changes`

Writes overrides to the active scenario and returns the resulting analysis.

Input: `overrides` (the `Overrides` shape, identical to `run_scenario`) and
`scenario` (optional).

Output: `{scenario, applied, revision_before, revision_after, snapshot_path, analysis}`.

`revision_before` comes from the `State` call the tool already makes to resolve
the active scenario and verify identity; `revision_after` is the `revision` field
of the `POST /whatif/apply` response. Both are reported so the caller can state
plainly whether the page will update, rather than inferring it from a 200.

The tool description must state that this **modifies the saved plan** and that
`run_scenario` is the way to check a claim without writing.

## Error handling

Every failure names the thing and refuses. None degrade to a silent no-op.

| Condition | Behavior |
|---|---|
| Server unreachable and spawn fails | Error naming the URL and the manual start command |
| `settings_dir` mismatch | Refuse; report both paths |
| `app` mismatch or unparseable body | Refuse; report what answered |
| Override validation rejected | `Apply`'s error passes through verbatim; it already names the field |
| Snapshot fails | Abort **before** the POST — never write unbacked |
| `Save` fails | No revision bump; report. The snapshot already taken is harmless |
| 200 but revision unchanged | Report "accepted but reported no change" — not success |
| Panic in either handler | Existing `recoverToError` converts it to a tool error |

**Spawned server lifetime.** The spawned `cmd/server` is detached deliberately, so
it outlives the MCP process and the browser tab keeps working after the session
ends. The cost is that a stray server can linger; `/killme` stops it. This is a
choice, recorded here so it is not mistaken for an oversight.

## Testing

**Revision counter** — bumps on `Save`, `SwitchScenario`, and `CreateScenario`;
monotonic; race-clean under `-race`.

**Poll handler** — 204 when `since == revision`; 200 plus partial when stale;
absent and malformed `since` both treated as 0; the response embeds the new `since`.

**State handler** — reports `app`, `settings_dir`, `active`, and `revision`; the
directory is absolute.

**Apply handler, sparsity** — the regression tests that justify the whole design:

- posting only `roth_conversion_amount` leaves `RothConversion.Enabled` **true**
- posting only `spouse_claim_age` leaves `SocialSecurity` **non-nil** and the
  primary `ClaimAge` unchanged

**`live.Client`** against `httptest` — adopts on match; refuses on `settings_dir`
mismatch; refuses on foreign `app`; clean error on refused connection.

**Snapshotter** — once per scenario; twice across two scenarios; byte-equal copy;
a failing copy aborts the write.

**End-to-end** via the existing `scripts/whatif-verify.sh` against its throwaway
data copy: apply, assert the revision bumped, assert the next poll returns 200,
assert the poll after that returns 204. No new test infrastructure.

**Package move** — `go build ./... && go vet ./... && go test ./... &&
staticcheck ./...` green, with the moved tests passing unchanged in their new
location.

## Risks

**A 2s poll on every open tab.** The 204 path is an integer comparison, but it is
still a request per tab per 2s. Acceptable for a single-user local app; it would
not be for a multi-tenant one.

**The write path bypasses the form handlers' validation.** `Apply`'s `validate`
covers the eleven fields, but the form handlers carry additional cross-field
invariants (`validateSettingsCrossFieldInvariants`, `clampPerAccountAllocations`).
Fields outside `Overrides` cannot be reached by this endpoint, so the exposure is
bounded — but the boundary must be re-checked whenever `Overrides` grows.

**Snapshots accumulate.** Every scenario written to in every session leaves a
`.bak` in the settings directory. Nothing prunes them, and `ListScenarios` must
not start listing them as scenarios.

**Detached servers linger.** See *Error handling*.

**Revision resets on restart.** A page held open across a server restart has a
`since` above the new counter, so `since != revision` and it re-renders once. Safe,
but the comparison must be inequality rather than `revision > since`.
