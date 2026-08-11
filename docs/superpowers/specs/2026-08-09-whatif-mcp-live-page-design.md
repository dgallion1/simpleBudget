# What-If MCP — Live Page Design

Extends `docs/superpowers/specs/2026-08-09-whatif-mcp-server-design.md`. That
design deliberately made every tool read-only. This one adds a write path and a
way for an open browser tab to notice the write, so a plan can be discussed in
Claude Code and watched in the browser at the same time.

Revised after an independent code review; see *Review* at the end for what
changed and why.

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
2. **Write target.** The active scenario, not a scratch copy — with a snapshot
   taken before the first write. Re-affirmed after review, once the lock race and
   the unbounded-value hazard were given real fixes rather than mitigations.
3. **Refresh.** A settings revision counter plus HTMX polling. Not SSE (more
   moving parts), not Playwright driving the real UI (slow, brittle, and only
   works in a browser the MCP launched).
4. **Write surface.** The fields `Overrides` already supports, less
   `healthcare_inflation` — ten fields. See *Writable field set*.
5. **Server lifecycle.** An `open_page` tool health-checks the server, launches
   it if nothing is listening, and returns the URL. It does not open a browser.
6. **Package layering.** `Overrides` and `Apply` move out of `whatifmcp` into
   their own package so the web handler can use them without importing a package
   named for a transport it does not speak.

## Non-goals

- SSE or WebSocket transport.
- Launching or driving a browser **from the MCP at runtime**. A browser used by a
  test to verify the swap mechanism is in scope — see *Testing*.
- Writing any settings field outside the ten listed below.
- Scratch or sandbox scenarios.
- In-app undo. Recovery from a bad write is restoring a `.bak` by hand.
- Exposing the tax optimizer, per-year RMD schedules, or the other items in
  `docs/whatif-mcp-followups-2026-08-09.md` §2.

## The central constraint — why not post the existing forms

The obvious implementation is for the MCP to post the same forms the browser
posts. It cannot work, and the reason is deeper than awkwardness.

`parseFormFloat` returns `(0, nil)` for an absent key
(`internal/handlers/whatif/handlers.go:590-596`). Inside any handler that reads
the form directly, **"field absent" and "field is zero" are indistinguishable**.
No header, flag, or partial-post convention fixes that from the client side.

`POST /whatif/settings` escapes this because it is spec-driven.
`handleWhatIfSettings` builds a sparse `updates` map via `settingsFormSpec`
(`form_spec.go:159-161, 171-188, 213-215`), so an absent key leaves the field
alone and a key present with `"0"` persists an explicit zero.

`POST /whatif/roth-conversion` does not. `handlers_rates.go:286`:

```go
settings.RothConversion.Enabled = r.FormValue("enabled") == "on"
```

A post carrying only `annual_amount` disables the conversions it was meant to
size — and because of `parseFormFloat`, `:289-311` also zeroes `AnnualAmount`,
`StartYear`, and `EndYear`.

`POST /whatif/social-security` does not. `handlers_rates.go:366, 382` assign
`ClaimAge` and `SpouseClaimAge` unconditionally, and `:357-359` sets
`FRABenefit = 0` on any post omitting `fra_benefit`. That reaches `:385-387`:

```go
if settings.SocialSecurity.FRABenefit <= 0 {
    settings.SocialSecurity = nil
}
```

So a partial post **deletes the entire Social Security configuration**,
unconditionally — not only when it was already empty.

Both handlers are correct for their real caller, a browser submitting a complete
form. Five of the ten writable fields route to them.

**Resolution:** one typed sparse endpoint that reuses `Apply` server-side.
Preview and commit become the same code path:

```
run_scenario   → Apply(settings, overrides)                    → analysis   (MCP process, no write)
apply_changes  → POST /whatif/apply → ApplyOverrides(…)        → analysis   (server process, writes)
```

## Writable field set

Ten fields. Five route to the safe spec-driven handler, five to the unsafe ones —
which is why all ten go through the new endpoint instead.

| Field | Would-be route |
|---|---|
| `monthly_living_expenses`, `projection_years`, `inflation_rate`, `investment_return`, `filing_status` | `/whatif/settings` (safe) |
| `roth_conversion_amount`, `roth_conversion_start_year`, `roth_conversion_end_year` | `/whatif/roth-conversion` (unsafe) |
| `social_security_claim_age`, `spouse_claim_age` | `/whatif/social-security` (unsafe) |

**`healthcare_inflation` is excluded**, though `Overrides` carries it.
`models/whatif.go:118` marks it legacy for the single-person model; once
`HealthcarePersons` is populated — which the migration at `settings.go:309-330`
does automatically for any plan with `MonthlyHealthcare > 0` — it is read only by
`analysis/present_value.go:59`. It has no form control anywhere in
`web/templates/`. Writing it would persist a value the user cannot see, cannot
revert through the UI, and which does not move the charts they were told to watch.
It remains available to `run_scenario` as a preview-only field.

**Two semantics that must be stated in the tool description, not inferred:**

- `roth_conversion_amount: 0` **disables** conversions. `overrides.go:74` sets
  `Enabled = *o.RothConversionAmount > 0`. This is the same footgun this design
  condemns in `handleWhatIfRothConversion`, so it is documented rather than
  hidden. The regression test must cover the zero case, not only a positive one.
- Setting `roth_conversion_start_year`/`end_year` alone, on a scenario with
  conversions disabled, is **rejected** with a message naming the reason.
  `docs/whatif-mcp-followups-2026-08-09.md` §5 records this as a silent no-op in
  `Apply`; harmless as a preview, a contract violation as a persisted write.

## Architecture

### Revision counter

A `revision int` on `SettingsManager`, guarded by the existing `sm.mu`, with a
private `bump()` called explicitly from **every site that changes what the page
should show**:

- `saveInternal` — the common tail of 23 call sites across 22 methods
- `SwitchScenario` (`settings.go:1590`)
- `DeleteScenario` (`:1716-1727`) — removes a file and may silently revert the
  active scenario
- `RenameScenario` (`:1772`) — writes directly via `sm.store.WriteFile`, bypassing
  `saveInternal` entirely
- `InvalidateCache` (`:419-423`)
- `BeginExternalRewrite`'s `end()` (`:487-496`) — the full-replace backup restore

An earlier draft claimed `saveInternal` was the sole tail of all write paths.
It is not, and the omission mattered most for backup restore: the settings
directory is rewritten wholesale with no `saveInternal` call, so every open tab
would have shown pre-restore figures indefinitely — while the user, now trusting
the page to update itself, had no reason to reload.

**One suppression.** `loadInternalContext:536-544` calls `saveInternal` on a
*read* when decode reports a migration. That path must not bump; a cache-miss
load is not a change. `bump()` is therefore called by `saveInternal`'s callers'
common wrapper, not by `saveInternal` itself, or gated by an explicit flag.

The counter is in-memory and not persisted. It only has to be monotonic within one
server process, because a page load reads the current value as its baseline. A
tab held across a restart sees `since != revision` and re-renders once — which is
why the comparison is inequality, never `revision > since`.

### Write path — one lock, no read-modify-write

`Load` returns `sm.cache`, the **shared pointer**, and releases the lock before
returning (`settings.go:386-414`). A handler doing `Load` → `Apply` → `Save`
therefore holds nothing across its own read-modify-write, unlike every existing
mutation (`UpdateSettings:989-1005`, `AddIncomeSource:686-702`, and 20 others),
all of which load, modify, and save inside a single write lock.

Without this, the following loses data:

- t0: `POST /whatif/apply` calls `Load()`, gets settings `S`
- t1: user drags the expenses slider; `UpdateSettings` writes `S2`
- t2: apply saves `Apply(S, o)` — the slider's change is gone, and the poll
  faithfully renders the reverted number two seconds later

A smaller window exists on `sm.filename`: `Save` writes to `sm.filepath()`, the
active file *at save time*, so a `SwitchScenario` between `Load` and `Save`
writes scenario A's data into scenario B's file.

**Therefore:** add

```go
func (sm *SettingsManager) ApplyOverrides(o overrides.Overrides) (*models.WhatIfSettings, int, error)
```

doing `loadInternal` → `Apply` → `saveInternal` → `bump` under a single
`sm.mu.Lock()`, returning the settings and the revision it produced. The handler
calls only this. `revision_after` is then exactly the revision this write created,
rather than whatever `Revision()` happens to report afterwards — which under
concurrency can be another writer's number.

### Endpoints

**`GET /whatif/state`** — identity and state together:

```json
{"app":"budget2","settings_dir":"/home/darrell/bin/ai/budget2/data/settings",
 "active":"whatif.json","revision":42}
```

`settings_dir` is the absolute resolved path; `app` is the literal `"budget2"`.
Both exist so a client can prove it is talking to the right server about the right
plan. `GET /api/health` returns only `{"status":"ok"}`
(`internal/handlers/backup/handlers.go:200-203`) and cannot distinguish budget2
from anything else on the port.

**`POST /whatif/apply`** — accepts the `Overrides` JSON shape, calls
`ApplyOverrides`, returns `{scenario, applied, revision}`.

**`GET /whatif/poll?since=N`** — `204 No Content` when `revision == N`; otherwise
the poll partial (below) plus an `HX-Trigger: {"whatif:revision": <new>}` response
header. HTMX performs no swap on 204, so the unchanged case costs one integer
comparison and runs no analysis. An absent or malformed `since` becomes `-1` — a
value the counter never returns — producing a full render. It cannot be `0`: the
counter also starts at `0` on every server start, so a bad parameter would
collide with a fresh counter and suppress the very render it was meant to force.
`-1` preserves the safe direction: a bad parameter shows fresh figures rather
than hiding them.

### The swap — sentinel, not `outerHTML`

`#whatif-results` is the wrapper `<div>` at `web/templates/pages/whatif.html:125`,
and the `whatif-results` template it invokes (`:140`) renders the *contents*, with
no wrapper of its own. So `hx-swap="outerHTML"` on that container replaces it with
a response that does not contain it. After one 200 poll the container is gone,
polling stops, and every one of the ~40 `hx-target="#whatif-results"` sites in
`web/templates/components/whatif/*.html` starts raising `htmx:targetError`.

`outerHTML` also breaks charts. htmx fires `htmx:afterSettle` with
`detail.target` set to the request's original target — for an `outerHTML` swap,
the **removed** node. `charts.js:536-547` passes `evt.detail.target` as `scope`
into `loadAllCharts(scope)` (`:440-448`) and `initWhatIfProjectionCards(root)`
(`:514-516`), both of which `querySelectorAll` on it. Charts would be fetched into
a detached subtree and never appear. `base.html:205-214` (scroll restore) and
`portfolio-settings.html:141-146` (range init) read the same field and break
identically.

**Design:** a sibling sentinel element that is never itself replaced.

```html
<div id="whatif-poll"
     class="hidden"
     hx-get="/whatif/poll"
     hx-vals='js:{since: window.__whatifRevision || 0}'
     hx-trigger="every 2s"
     hx-target="#whatif-results"
     hx-swap="innerHTML"></div>
```

The baseline lives in `window.__whatifRevision`, updated by a small listener on
the `whatif:revision` event that the `HX-Trigger` header raises. The sentinel is
never swapped, so its timer is never disturbed. `detail.target` stays
`#whatif-results` for all three existing listeners, and every existing swap site
is untouched.

The mutating handlers emit the same `HX-Trigger` header, so a change the user
makes with a slider updates the baseline too. An earlier draft claimed this
happened for free because the handlers render the same partial — false: their
responses swap `innerHTML` and never touch the container's own attributes, so
without the header every slider drag would be followed by a redundant full
re-render within two seconds.

### The poll must not rewrite the left column

`whatif.html:144-162` and `:164-215` are OOB swap blocks inside the
`whatif-results` partial. They replace `#whatif-portfolio-settings-card`,
`#whatif-rate-assumptions-card`, `#whatif-spending-phases-card`,
`#whatif-social-security-card`, `#income-sources-list`, `#expense-sources-list`,
and the four quick-adjust panels.

That is right for a user-initiated mutation and wrong for a background poll. The
feature's whole premise is that a human and the MCP touch the plan at the same
time, so the colliding case is the normal case: you are half-way through typing
`4200` into monthly expenses, or holding a slider, when an `apply_changes` lands.
Two seconds later the poll replaces the card, destroying the partial input, the
focus, and the drag. The 500ms `change/input` debounce on
`portfolio-settings.html:27` means your own edit is in flight at the same time.

**Two measures, both required:**

1. **Split the template.** The OOB blocks move into a `whatif-results-oob`
   template used only by the mutating handlers. The poll renders
   `whatif-results` content alone and emits no OOB blocks, so it can never
   rewrite a control the user is holding.
2. **Yield to the user.** The sentinel carries `hx-sync="#whatif-results:drop"`
   so a poll is dropped while a user-initiated request to that target is in
   flight, and a `htmx:confirm` guard drops the poll while `document.activeElement`
   is inside `#whatif-results` or a left-column form. A dropped poll costs
   nothing — the next one two seconds later still sees the stale `since`.

### Overrides package move

`Overrides`, `Apply`, `validate`, and their tests move from
`internal/services/whatifmcp` to `internal/services/retirement/overrides`.
`whatifmcp` and `handlers/whatif` both import the new package. No logic changes.
`prepare` imports only `models`, so the new package creates no cycle.

`RunWithOverrides` stays in `whatifmcp` — it is MCP response shaping, not settings
mutation.

`overrides_test.go` **must be split**, not moved: it mixes `Apply`/`validate`
tests (moving) with `TestPreparedWithOverrides_…:57` and
`TestRunWithOverrides_…:266, :281` (staying), sharing `ptr`/`ptrInt` helpers that
both halves need.

### MCP client

**`live.go`** — HTTP client to the running server, short timeouts: `State`,
`Apply`, `EnsureServer`.

**`snapshot.go`** — a `Snapshotter` holding the set of scenarios already
snapshotted by this process. `Ensure(scenario)` copies the scenario file and
records it, once per **(process, scenario)** — not once per process, because
switching scenarios mid-conversation must snapshot the second plan too.

**Snapshots are written outside the data directory**, to
`<BackupDir>/mcp-snapshots/<scenario>.<2006-01-02T15-04-05Z>.bak`.

Not into `data/settings`. `ListScenarios` would ignore them (its glob is
`whatif*.json`, `settings.go:1525, 1665`), but the backup system would not:
`internal/services/backup/snapshot.go:235-273` (`SkipPredicate`) skips only the
backup dir, `cache/`, `atomicWrite` leftovers, and encryption-state files. Both
the automatic snapshotter (`:167`) and the manual backup
(`handlers/backup/handlers.go:252`) walk the whole data dir. Thirty sessions
across three scenarios would put ninety `.bak` files into every subsequent backup
zip, and each new one would itself trigger a fresh snapshot.

The filename uses `2006-01-02T15-04-05Z`, not RFC3339 — RFC3339 contains colons,
which survive Linux but break extraction on Windows and exFAT.

`Ensure` must **read** the source file, not merely `os.Link`/copy blindly, and
fail if it cannot. `docs/whatif-mcp-followups-2026-08-09.md` §3 records that the
MCP detects encryption in the wrong directory; a byte copy of ciphertext would
otherwise "succeed" and the abort-before-POST guarantee would not fire.

**Reads stay file-based** — faster, and they still work with the server down. The
single change: resolving the *active* scenario asks `/whatif/state` when a
verified server is reachable, falling back to `whatif.json`. This closes
`docs/whatif-mcp-followups-2026-08-09.md` §4.

### Instance resolution

The MCP resolves settings from its own flag; the server resolves from
`BUDGET_DATA_DIR`. Nothing makes them agree, and followups §3 records that the MCP
ignores `BUDGET_DATA_DIR` entirely. If they diverge, reads describe one plan while
writes land on another, with no symptom. `scripts/whatif-verify.sh` makes this
concrete: it runs instances on `:8099` against a throwaway copy in `/tmp`.

1. **Own settings dir `S`** — the `-data` flag, else `BUDGET_DATA_DIR/settings`,
   else `./data/settings`. Honoring `BUDGET_DATA_DIR` here closes §3.
2. **Candidate URL** — `BUDGET_SERVER_URL`, else `http://localhost:8080`
   (`config.go:54`).
3. **Probe `GET /whatif/state`:**
   - *Connection refused* — no instance; spawn (below). A write attempted before
     that fails telling the caller to run `open_page`.
   - *200, `app == "budget2"`, `settings_dir == S`* — adopt.
   - *200, `settings_dir != S`* — **refuse to write**, reporting both paths.
   - *200, `app != "budget2"` or unparseable* — refuse, reporting what answered.

**Spawning.** `config.Load` derives `SettingsDirectory = filepath.Join(dataDir,
"settings")` (`config.go:77-81`). So the server must be spawned with
`BUDGET_DATA_DIR=filepath.Dir(S)`, **not** `BUDGET_DATA_DIR=S`. An earlier draft
had the latter, which would have produced a server whose settings dir was
`S/settings`, failed the identity check the same design had just added, and left
a bogus `S/settings` behind via `ensureDirectories()`.

When `filepath.Base(S) != "settings"` — reachable via an arbitrary `-data` flag —
spawning is **refused with an explanation**, because no `BUDGET_DATA_DIR` value
can produce that settings path. Never guess.

Never spawn when a healthy instance is already on the URL; a second `cmd/server`
would fail to bind anyway.

## Tool surface

### `open_page`

Input: `scenario` (optional) — switches first via `POST /whatif/scenarios/switch`.

Output: `{url, started, active, revision}`. `started` distinguishes "I launched
this" from "it was already up".

### `apply_changes`

Input: `overrides` (the writable ten) and `scenario` (optional).

Output: `{scenario, applied, revision_before, revision_after, snapshot_path, analysis}`.

`revision_before` comes from the `State` call the tool already makes;
`revision_after` is the revision `ApplyOverrides` produced. Both are reported so
the caller can state plainly whether the page will update.

The description must state that this **modifies the saved plan**, that
`run_scenario` checks a claim without writing, that recovery is a manual `.bak`
restore, and the two semantics under *Writable field set*.

## Error handling

| Condition | Behavior |
|---|---|
| Server unreachable and spawn fails | Error naming the URL and the manual start command |
| `S` is not a `settings` directory | Refuse to spawn; explain why `BUDGET_DATA_DIR` cannot express it |
| `settings_dir` mismatch | Refuse; report both paths |
| `app` mismatch or unparseable body | Refuse; report what answered |
| Override validation rejected | `Apply`'s error passes through verbatim; it names the field |
| Roth start/end set with conversions off | Refuse, naming the reason |
| Snapshot fails, including unreadable source | Abort **before** the POST — never write unbacked |
| `saveInternal` validation fails (`ValidatePersons:646`, `validateChainInternal:653-657`) | Report; no bump. `Apply` succeeding does not mean `Save` will |
| Panic in either handler | Existing `recoverToError` converts it to a tool error |

**Spawned server lifetime.** Detached deliberately, so it outlives the MCP process
and the tab keeps working after the session ends. A stray server can linger;
`/killme` stops it. Recorded so it is not mistaken for an oversight.

**Bounds.** `overrides.go:103-134` bounds `monthly_living_expenses`,
`roth_conversion_amount`, `projection_years`, both claim ages, and
`filing_status`. It bounds **nothing** on `inflation_rate` or
`investment_return`. That was parity with the equally lax form
(`form_spec.go:103, 106`) when the value lived in a discarded copy; it is not
acceptable once the value is persisted. An absurd rate that produces a NaN or an
engine panic turns every `GET /whatif` into a 500 via `middleware.Recoverer`
(`cmd/server/main.go:113`) — and the poll into a 500 every two seconds — with no
in-app undo. `validate()` therefore gains bounds on both (−20…50 percent covers
every legitimate plan).

## Documentation that becomes false

Not optional cleanup — this changes what a user consents to when they add the
server.

- `cmd/whatif-mcp/main.go:1-4` — "it never writes to the data directory and makes
  no network calls." It will write snapshots, make HTTP calls, and spawn a process.
- `whatifmcp/scenarios.go:22-24` — "Read-only by construction: it exposes no
  method that writes to the settings directory."
- The README's MCP section and the `.mcp.json` posture note.

## Testing

**Revision** — bumps on `Save`, `SwitchScenario`, `CreateScenario`,
`DeleteScenario`, `RenameScenario`, `InvalidateCache`, and `BeginExternalRewrite`'s
`end()`; does **not** bump on the `loadInternalContext` migration path; monotonic;
race-clean under `-race`.

**Poll handler** — 204 when `since == revision`; 200 plus partial when stale;
absent and malformed `since` both treated as 0; the `HX-Trigger` header carries
the new revision; the response contains **no OOB blocks**.

**State handler** — reports `app`, `settings_dir` (absolute), `active`, `revision`.

**`ApplyOverrides` sparsity** — the regression tests that justify the design:

- only `roth_conversion_amount` (positive) leaves `Enabled` **true**
- `roth_conversion_amount: 0` **disables** — asserting the documented semantics,
  not the comfortable ones
- only `spouse_claim_age` leaves `SocialSecurity` **non-nil** and primary
  `ClaimAge` unchanged

**Concurrency** — a concurrent `UpdateSettings` and `ApplyOverrides` under
`-race`, asserting no lost update. Expect this to also surface the pre-existing
shared-`sm.cache` mutation in `handleWhatIfRothConversion:281-311` and
`handleWhatIfSocialSecurity:353-387`, which a 2s-per-tab poll turns from rare into
continuous. Fixing that is in scope if the race detector implicates it.

**Spawn env derivation** — assert the `exec.Cmd` environment carries
`BUDGET_DATA_DIR=filepath.Dir(S)`, and that a non-`settings` `S` refuses. No
server needed.

**`live.Client`** against `httptest` — adopts on match; refuses on `settings_dir`
mismatch; refuses on foreign `app`; clean error on refused connection.

**Snapshotter** — once per scenario; twice across two scenarios; byte-equal copy;
unreadable source aborts; target path is outside the data directory.

**Backup exclusion** — a backup taken after a snapshot contains no `.bak`.

**Browser smoke test** — the swap mechanism cannot be verified in Go. The
sentinel, the `HX-Trigger` baseline update, chart reload after a polled update,
and the hx-sync/focus guard all need a real browser. A Playwright smoke script
plus a companion `-smoke.md` doc (matching
`docs/superpowers/specs/2026-05-03-major-expense-checkbox-pinning-smoke.md`) runs
against `scripts/whatif-verify.sh` on `:8099`. Without it the single mechanism the
whole feature rests on has no automated verification.

**End-to-end** via `scripts/whatif-verify.sh`: apply, assert the revision bumped,
assert the next poll returns 200, assert the poll after that returns 204.

**Package move** — `go build ./... && go vet ./... && go test ./... &&
staticcheck ./...` green, with the split test files passing in their new locations.

## Risks

**A 2s poll per open tab.** The 204 path is an integer comparison, but it is still
a request per tab per 2s, and `middleware.Logger` (`cmd/server/main.go:112`) would
log every one — roughly 1800 lines/hour/tab. The poll route is excluded from the
logger.

**Cross-field validation is bypassed.** `validateSettingsCrossFieldInvariants`
(`form_spec.go:310-324`) and `clampPerAccountAllocations` (`:366-379`) cover only
allocation percentages, which no writable field touches. The exposure is bounded
today; the boundary must be re-checked whenever the writable set grows.

**Detached servers linger.** See *Error handling*.

**No in-app undo.** A bad write is recovered by restoring a `.bak` from
`<BackupDir>/mcp-snapshots/` by hand. Accepted deliberately; bounds on the rate
fields exist so the page cannot be wedged into a state where the UI itself is
unusable.

## Review

An independent reviewer audited the first draft against the source. Confirmed and
folded in: the `outerHTML` swap was unworkable and would have broken the container,
the charts, and ~40 unrelated swap targets; the spawn would have set
`BUDGET_DATA_DIR` one level too deep and failed its own identity check; the write
path had a lost-update race against the user's own slider; `saveInternal` covers
23 call sites rather than the 8 claimed, and is bypassed entirely by
`RenameScenario` and backup restore; snapshots in `data/settings` would have been
swept into every backup zip; `healthcare_inflation` is unreachable from the UI and
inert in the projection.

Corrected in the reviewer's favour against the first draft's claims: the "no
redundant re-fetch" property did not exist, and the "200 but revision unchanged"
error row was unreachable and has been dropped.

Rejected: the recommendation to write to a dedicated `whatif_mcp.json` scenario.
The user re-affirmed writing to the active scenario after seeing the findings,
given that the lock race and the unbounded-value hazard now have direct fixes
rather than mitigations.
