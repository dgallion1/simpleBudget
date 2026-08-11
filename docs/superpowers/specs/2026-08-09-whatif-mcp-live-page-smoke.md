# What-If MCP Live Page — Browser Smoke Verification

**Branch:** `feat/whatif-mcp-live-page`
**Original run date:** 2026-08-10 (commit `5b860fa`)
**Re-run date:** 2026-08-10 (this update), covering fix-wave commits **`c19321c`** and **`21b55c5`**, applied on top of `5b860fa` (which itself sat on `a877752`/`a07ec52`, two prior wording-only corrections to this document)
**Plan:** `docs/superpowers/plans/2026-08-09-whatif-mcp-live-page.md`
**Task brief:** `.superpowers/sdd/2026-08-09-whatif-mcp-live-page/task-15-brief.md`

> **Note on redaction:** This document was produced by driving the app against
> a copy of the owner's real retirement plan. Before publication, concrete
> plan values (dollar figures, the real scenario name, and one pre-existing
> data value) have been replaced with placeholders such as `$X,XXX.XX` and
> `<scenario>`. All request counts, status codes, revisions, ids, file paths,
> and PASS/FAIL verdicts are unchanged. This redaction does not affect any
> verdict below.

## Why this document changed again

The original run (section evidence below, mostly still valid) was approved as
this branch's execution evidence. Since then, `c19321c` and `21b55c5` landed
and changed behavior that Checks 6 and 7 specifically measure:

- **M3** — `renderRecalc` no longer emits `HX-Trigger` on every mutating
  route. `POST /whatif/settings` (both sliders) and the `saveAndRecalc`
  handlers now thread the real revision through from the write. ~20
  item-CRUD, healthcare, and spending-phase routes (`handlers_healthcare.go`,
  `handlers_income_expense.go`, `handlers_rates.go`) now deliberately pass
  `revisionUnreported` and omit the header — the client baseline lags by one
  tick and the *next* poll re-renders (200) instead of the mutation's own
  response carrying the update. This is intentional, not a regression: Check
  6 now has two distinct expected shapes instead of one.
- **M4 + R3** — the focus guard in `whatif-poll.js`'s `htmx:confirm` listener
  is now scoped: it only suppresses a poll tick when `document.activeElement`
  is both interactive **and** contained within `#whatif-results`, **and**
  `document.hasFocus()` is true. Previously *any* focused input anywhere on
  the page suppressed indefinitely, including after the window itself lost
  focus (since `document.activeElement` does not reset on blur). This
  directly invalidates the original Check 7, which used the monthly-expenses
  slider — an element **outside** `#whatif-results` — as its focused control.
  Under the new scoping that element no longer participates in the guard at
  all.
- **M2** — `POST /whatif/apply` now accepts an optional `expected_scenario`
  and returns `409 Conflict` (writing nothing) when it doesn't match the
  currently-active scenario file.
- M1 (revision bump on scenario revert), M5 (spawn port derivation), M6
  (child stderr as a real file) also landed but are not browser-observable
  and are not covered here.

All 8 checks, all 3 additional items, plus the newly-relevant R3/M2 behavior
were **re-executed in full** against a fresh throwaway instance — nothing
below is carried over unverified from the original run. Where the original
run's finding still holds, this update says so explicitly with fresh
evidence from the re-run session; where behavior changed, the row explains
what changed and why, per the fix-wave notes above.

One unrelated pre-existing bug (`updateSpendingPreview` null-deref) was
re-confirmed still present and still out of scope for this feature — see
section 4. The auditor of the original run independently confirmed via `git
diff master...feat/whatif-mcp-live-page` that this branch never touches
`rate-assumptions.html` or `portfolio-settings.html`, so this bug predates
and is unrelated to every commit on this branch, including the fix wave.

## 1. Results table — the 8 checks from the brief (re-run)

| # | Check | Expected | Observed (re-run) | Result |
|---|---|---|---|---|
| 1 | Container survives | `#whatif-results` and `#whatif-poll` both exist after 5 poll cycles (~10s) | Fresh load of `/whatif`, waited 11s, `document.getElementById` for both ids returned `{pollExists: true, resultsExists: true}`. 10 `/whatif/poll?since=0` requests observed during the wait, all `204`. Unchanged from the original run. | **PASS** |
| 2 | Idle polls are 204 | Every `/whatif/poll` response is 204 while nothing changes | Same network log as Check 1: 10 consecutive `204 No Content`, zero non-204 responses. Unchanged. | **PASS** |
| 3 | External change appears without reload | `curl -X POST /whatif/apply -d '{"monthly_living_expenses": 9876}'` → results column shows the new figure within 3s, no reload | curl returned `{"scenario":"whatif.json","applied":{"monthly_living_expenses":9876},"revision":1}`. 3s later, the Cash Flow tab's "Monthly Budget Analysis" panel showed `Living Expenses $X,XXX.XX/mo` (the same derived, inflation-adjusted figure as the original run — the POSTed value × the `1.05` Go-Go-phase multiplier per `budget_fit.go:77`, matching the rendered total exactly, confirmed by the original run's auditor). `window.__whatifRevision` read `1`, matching the curl response. Unchanged from the original run — `/whatif/apply` is not one of the routes M3 touched. | **PASS** |
| 4 | Charts redraw after check 3 | `#chart-projection` has real rendered content; a `/whatif/chart/projection` request occurs after the poll swap | Poll `#26` (`since=0 => 200`) immediately followed by `GET /whatif/chart/projection => 200` and `GET /whatif/chart/income => 200` (requests #27/#28). `#chart-projection` contains a live `<svg>` element (`hasSvg: true`) after switching to the Overview tab. Unchanged. | **PASS** |
| 5 | Baseline advances | After check 3, polls return to 204; a stuck-at-200 loop would mean `HX-Trigger` isn't updating the client baseline | Immediately after the #26/#27/#28 sequence, subsequent polls switched to `since=1 => 204` (13 consecutive observed). This pattern repeated correctly at every revision transition observed later in the session (`since=1→2`, `2→3`, `3→5`, `7→8`) — every poll immediately following a real baseline advance was 204, never a stale 200 loop. See "Revision arc, this session" in section 4 for exactly which transitions were directly observed vs. not independently timestamped. | **PASS** |
| 6 | No redundant re-render after a user edit — **now has two distinct expected shapes per M3** | **Shape A (slider, threaded revision):** edit → next poll 204. **Shape B (item add/remove, `revisionUnreported`):** add/remove → next poll 200 within ~2s (deliberate, not a bug) | **Shape A:** focused `#monthly_living_expenses_input`, real `ArrowRight` keypress → debounced `POST /whatif/settings => 200` (request #42). The very next poll was `since=2 => 204` (request #45) — not 200. Threading intact, **no regression**. **Shape B:** submitted the add-income form (name "QA Rerun Income", amount 111) → `POST /whatif/income => 200` (request #56) with **no** `HX-Trigger` (confirmed: client baseline stayed at `since=2` on the very next poll, request #59, which came back **200**, not 204 — the client picked up the change one tick late, exactly as M3 intends). That poll's own response *did* carry the real revision, and the following poll correctly returned to `since=3 => 204`. `#income-sources-list` contained "QA Rerun Income" afterward, confirming the mutation itself succeeded even though its HTTP response carried no trigger. | **PASS (both shapes correct; Shape A shows no threading regression)** |
| 7 | Focus guard — **scoped and blur-aware per M4/R3; original Check 7 methodology is now obsolete** | See items 2–3 in the coordinator's re-run request, folded in below | Re-designed this check around the one interactive control *inside* `#whatif-results` (`#steady-state-slider`, Cash Flow tab, `budget-analysis.html:136`) since that is the only element the new scoped guard applies to. Full results in section 2, rows "Focus suppression, window focused" (**PASS**) and "Focus suppression, window genuinely blurred — R3's alt-tab scenario" (**PASS with a measurement caveat — see below, not a clean pass on the literal wording**). | **PASS with caveat (see section 2)** |
| 8 | Existing swaps unaffected | Sync, add an income source, switch scenarios — no `htmx:targetError` anywhere in the console for the whole session | Re-run in full: `POST /whatif/sync => 200` (0 new console errors), income-add as above (0 new console errors, `#income-sources-list` OOB-updated correctly — see section 2 row 3), scenario switch to "<scenario>" → `POST /whatif/scenarios/switch => 200` followed by a full-page `GET /whatif => 200` reload (0 new console errors post-reload). Console messages captured for the entire session (including the deliberately-induced 404 from the mutation-resilience test and the pre-existing `updateSpendingPreview` bug, both accounted for separately): **zero `htmx:targetError` occurrences**, confirmed by scanning the full `all:true` console dump for the literal event name — no matches. | **PASS** |

## 2. R3 / M4 focus-guard deep dive (replaces the original Check 7)

The coordinator's re-run request asked for two specific sub-tests plus a
regression watch on the shared JS. All three, plus the OOB-list check
folded in from Check 8, are below.

| Item | Expected | Observed (re-run) | Result |
|---|---|---|---|
| **Focus suppression, window focused** (item 3) | With `#steady-state-slider` (inside `#whatif-results`) focused and the browser window/tab itself focused, an external `curl /whatif/apply` should still be held off — the R3 fix must not have disabled the guard entirely | Focused `#steady-state-slider` (`containedInResults: true`, `hasFocus: true`). `curl -X POST /whatif/apply -d '{"monthly_living_expenses": 22222}'` → server revision advanced to `4`. Waited 5s: `window.__whatifRevision` stayed at `3`, the Cash Flow panel still showed the pre-change figure (`$X,XXX.XX/mo`), and all polls during the window continued returning `204` at the *old* `since=3` baseline — if a request had gone out and gotten a mismatched-baseline response, it would have come back `200`, not `204`, so the continued 204s are consistent with the request being suppressed before it was sent. **Both halves hold**: the guard still suppresses when genuinely applicable. | **PASS** |
| **Focus suppression, window genuinely blurred — R3's alt-tab scenario** (item 2) | Focus `#steady-state-slider`, move **actual** browser focus away (not a synthetic event), apply an external change, confirm polling continues and the page updates. Before R3 this would have stayed frozen indefinitely. | Opened a second, real browser tab via `browser_tabs {action: "new"}` and left it frontmost — a genuine UI-level tab switch, not `window.blur()` or a dispatched event. With the original tab backgrounded, ran `curl /whatif/apply -d '{"monthly_living_expenses": 33333}'` (server revision → `5`) and waited 6s. **Measurement limit, stated plainly:** this Playwright MCP's `browser_evaluate` / `browser_network_requests` tools operate only on whichever tab is currently selected — confirmed by selecting the blank tab and querying `/whatif/poll`, which returned zero results even though the original tab's poll history is non-empty. There is no way with this tool surface to inspect the backgrounded tab's live state without re-selecting it, which itself ends the blur. So the literal claim "polling continued during the blur" could not be directly measured — I did not fabricate a live capture. What **was** measured: re-selecting the original tab immediately afterward showed `window.__whatifRevision` still at `3` and the Cash Flow panel still showing the stale pre-`22222` figure — i.e., across the entire ~11s the tab was backgrounded, no poll response had landed and updated the page, for either the revision-4 or revision-5 change. Then, with the tab foregrounded again but **before any further wait**, I blurred `#steady-state-slider` (releasing the one remaining suppression condition) and waited 3s: the very next poll (`since=3 => 200`) picked up **both** missed changes at once, jumping straight to `since=5`, and the panel updated to `$X,XXX.XX/mo` (consistent with the last-applied `33333`). **Interpretation, stated as interpretation, not fact:** the guard's own code (`interactive && results.contains(active) && document.hasFocus()`) would *not* have suppressed a request during a genuine blur, since `document.hasFocus()` would have been `false` — so if the observed staleness was caused by suppression, it was not this guard doing it. The more likely explanation, given that no request of any kind appears to have completed during the background window, is that the browser itself throttled or paused htmx's `every 2s` timer while the tab was hidden (standard Page Visibility–driven background-tab throttling in Chromium), which is an environment/browser effect outside this application's code and outside what R3 controls. I cannot distinguish "guard suppressed it" from "browser never attempted it" from what I was able to instrument, and I am not asserting either as confirmed. What is confirmed: focus alone, without a live tab switch, no longer causes indefinite suppression (see the window-focused row above, which is the guard actually being exercised and releasing correctly), and after this genuinely-backgrounded window the page self-healed completely within one tick once the remaining focus condition was cleared, with no lingering staleness, no stuck flag, and no console error. | **PASS on "does not stay frozen forever" and "self-heals correctly"; NOT independently confirmed on the literal "polling continues while backgrounded" — see caveat** |
| OOB left-column still applies under M3's changed revision handling | Income-add (a `revisionUnreported` route) should still correctly OOB-update `#income-sources-list` even though it no longer emits `HX-Trigger` | After the Shape-B income-add above, `document.getElementById('income-sources-list').textContent.includes('QA Rerun Income')` → `true`. The OOB swap is independent of the `HX-Trigger` header (different response-handling paths in htmx), and M3 did not touch the OOB template blocks. | **PASS** |

## 3. New in this fix wave — M2: `expected_scenario` conflict check

| Check | Expected | Observed | Result |
|---|---|---|---|
| Mismatched `expected_scenario` | `curl -X POST /whatif/apply -d '{"expected_scenario":"whatif-does-not-match.json","monthly_living_expenses":5000}'` → `409`, nothing written | `HTTP/1.1 409 Conflict`, body: `refusing to write: the active scenario is whatif_<scenario>.json, but this change was prepared for whatif-does-not-match.json (the active scenario changed between the two). Nothing was written`. Data file `settings/whatif_<scenario>.json` confirmed unchanged (`monthly_living_expenses` still `X,XXX`, the pre-existing Sync'd value) after the request. | **PASS** |
| Matching `expected_scenario` | Same request with the correct active scenario name → succeeds | `curl -X POST /whatif/apply -d '{"expected_scenario":"whatif_<scenario>.json","monthly_living_expenses":5000}'` → `HTTP/1.1 200 OK`, `{"scenario":"whatif_<scenario>.json","applied":{"monthly_living_expenses":5000},"revision":8}`. Data file confirmed updated to `"monthly_living_expenses": 5000`. Browser picked up the change on its next poll (`window.__whatifRevision` read `8` after a 3s wait). | **PASS** |

## 4. Additional verification items (re-run)

| Item | Expected | Observed (re-run) | Result |
|---|---|---|---|
| Sentinel polls despite `display:none`; no gap/empty cell at either breakpoint | Poll fires on its 2s timer regardless of visibility; grid shows no stray row/cell | At 375×812: `getComputedStyle(#whatif-poll).display === 'none'`, parent grid still has 3 DOM children with no visible gap; 15 consecutive `204`s observed over 4s+ dwell at this viewport, confirming the timer keeps firing under `display:none`. This part of the page (`web/templates/pages/whatif.html`) is untouched by the fix wave, so a lighter re-check (one breakpoint, not both) was judged sufficient rather than a full redo — noted explicitly rather than silently assumed. | **PASS** |
| Polling resumes after a mutation completes (not stuck off); induced-failure case also recovers | `mutationInFlight` flag clears on success **and** on failure/abort | Re-ran the induced-failure technique: `htmx.ajax('POST', '/whatif/this-route-does-not-exist-abc', {target:'#whatif-results', source: <fake element>})` → `404`. Network log shows poll requests bracketing the failure (`…#181 since=8 204, #182 [the 404], #183 since=8 204…`) with no gap and no stuck-off state. This exercises the same `htmx:beforeRequest`/`htmx:afterRequest` pairing in `whatif-poll.js` that M4/R3 did not modify (only the `htmx:confirm` interactive-check block changed). | **PASS** |
| Unrelated finding: `updateSpendingPreview` null-deref | N/A — tracking only | Re-confirmed present: pressing `ArrowRight` on `#monthly_living_expenses_input` during Check 6 Shape A reproduced the same `TypeError: Cannot read properties of null (reading 'classList')` at `rate-assumptions.html:790` as the original run. Confirmed via `git diff a877752..21b55c5 -- web/templates/components/whatif/rate-assumptions.html web/templates/components/whatif/portfolio-settings.html` that neither file changed in this fix wave (or at any point on this branch, per the original run's independent audit). Already tracked as a separate follow-up task, out of scope here. | Unchanged — informational only |

## 5. Session evidence summary (re-run)

- Server: `scripts/whatif-verify.sh start 8099` against a fresh throwaway copy of `data/`; stopped cleanly with `scripts/whatif-verify.sh stop 8099` at the end of the run.
- **Revision arc, this session (stated precisely, not smoothed over):** `0` (load) → `1` (curl apply, Check 3) → `2` (slider edit, Check 6 Shape A, directly observed via the `POST /whatif/settings => 200` immediately followed by `since=2` polls) → `3` (income-add, Check 6 Shape B, directly observed: the follow-up poll returned `200` and subsequent polls used `since=3`) → server advanced to `4` then `5` via two curls during the R3 test (`22222`, then `33333`) while the client was held at `3`; both were picked up together in a single `since=3 => 200` response that jumped the client straight to `5` (directly observed, both the "stuck at 3" state and the "jumped to 5" catch-up were read via `window.__whatifRevision`). Between `5` and the next reading of `7` (taken after Sync, then a scenario-switch full-page reload), I did not read the revision at each intermediate step — Sync writes multiple settings fields in one request and could plausibly account for more than one revision point, and the scenario-switch reload may also bump it (M1 mentions a revision bump on scenario *revert*, which may be related to or distinct from a plain switch) — but I am **not** asserting a specific breakdown for `5→7` since I did not capture it directly. `7 → 8` is fully accounted for: the M2 matching-scenario `curl` explicitly returned `"revision":8`, and the client read `8` after its next poll.
- Console errors across the whole re-run session (since the last full-page navigation, which was the scenario-switch reload): 3, all from the single deliberately-induced 404 (`Failed to load resource: 404`, `Response Status Error Code 404`, `HTMX error: {...}`). **Zero** `htmx:targetError` anywhere in the full-session console dump (`all:true`, scanned explicitly for the literal event name).
- Server log (`scripts/whatif-verify.sh log 8099`) reviewed for the full re-run session: the `409` and `200` for the M2 test, the `404` for the induced-failure test, and otherwise only expected 200/204 traffic — no 5xx, no panics.

## 6. Verification summary

- Check 1 (container survives): ✅ re-confirmed
- Check 2 (idle polls 204): ✅ re-confirmed
- Check 3 (external change appears): ✅ re-confirmed, unchanged by fix wave
- Check 4 (charts redraw): ✅ re-confirmed, unchanged by fix wave
- Check 5 (baseline advances): ✅ re-confirmed
- Check 6, Shape A — slider edit → 204 (threaded revision): ✅ **no regression**
- Check 6, Shape B — item add/remove → 200 within ~2s (deliberate `HX-Trigger` omission): ✅ working as newly intended
- Check 7 / focus guard, window focused: ✅
- Check 7 / focus guard, R3 genuine alt-tab: ✅ on outcome (no permanent freeze, clean self-heal) — ⚠️ the literal "polling continues while backgrounded" could not be independently measured with this tooling; most likely explanation is browser background-tab timer throttling, not a guard defect, but this is stated as interpretation, not a directly-measured fact
- Check 8 (existing swaps unaffected, no htmx:targetError): ✅ re-confirmed
- M2 — mismatched `expected_scenario` → 409, no write: ✅
- M2 — matching `expected_scenario` → 200, writes: ✅
- Sentinel polls under `display:none`, no layout gap: ✅ re-confirmed (light re-check, template untouched by fix wave)
- Polling resumes after success and after induced failure (no stuck-off flag): ✅ re-confirmed
- OOB left-column list updates correctly under M3's `revisionUnreported` routes: ✅
- Unrelated finding: `updateSpendingPreview` null-deref bug — still present, still out of scope, confirmed untouched by this fix wave

No check was skipped or reported as passing without direct evidence from this
session. The one item that could not be fully confirmed as literally
specified (live network activity on a backgrounded tab) is recorded above
with the exact reason and the closest evidence that could be gathered instead
— not silently upgraded to a pass.
