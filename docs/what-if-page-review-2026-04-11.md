# Code Review: What-If Page

Date: 2026-04-11

## Scope

- Section 1: Page shell, render boundaries, scenario lifecycle, and client-side hydration
- Section 2: Core inputs and CRUD flows
- Section 3: Strategy modules
- Section 4: Results stack
- Section 5: Scenario persistence and orchestration

## Findings

1. High: `internal/handlers/whatif/handlers.go:507` makes `GET /whatif` mutate saved scenario data. On every page load, if `len(settings.IncomeSources) == 0`, the handler auto-syncs from the dashboard and immediately saves. That means a deliberate zero-income scenario or newly created blank scenario cannot stay blank after being opened. This should be an explicit user action, or at minimum limited to first-run bootstrap of the default scenario.

2. Medium: the left column uses inconsistent HTMX refresh boundaries, so some server-derived UI goes stale after submit. The `whatif-results` partial only OOB-refreshes a small subset of left-column cards in `web/templates/pages/whatif.html:103`, but the Social Security config card posts to `#whatif-results` while also rendering analysis-dependent helper text in `web/templates/components/whatif/social-security.html:8` and `web/templates/components/whatif/social-security.html:55`. After submit, the right-side results update, but the left-side Social Security helper text and warnings can remain stale. The page needs one rule: either all server-derived left cards get OOB refreshes, or those cards stop depending on server-rendered analysis.

3. Medium: the projection chart is fetched twice on initial load and twice after every `#whatif-results` swap. `updateProjectionDisplayMode()` already calls `loadChart()` in `web/static/js/charts.js:262`, but the page also calls `loadAllCharts()` after `initWhatIfProjectionCards(document)` in `web/static/js/charts.js:312`, and repeats the same pattern after HTMX settles in `web/static/js/charts.js:283`. Because the chart endpoint reruns analysis work in `internal/handlers/whatif/handlers.go:1361`, this adds avoidable server load and user-visible latency.

4. Medium: the portfolio value slider loses its intended range configuration after any partial refresh that re-renders the portfolio card. The `whatif-results` partial replaces `#whatif-portfolio-settings-card` out-of-band in `web/templates/pages/whatif.html:106`, but the slider is rendered with a hardcoded `max="20000000"` and `step="100000"` in `web/templates/components/whatif/portfolio-settings.html:44`. The only code that normalizes those attributes to the selected range runs once on `DOMContentLoaded` in `web/templates/components/whatif/portfolio-settings.html:115`. After an HTMX refresh, the select can show `$0-500K` while the recreated slider still behaves like the `$0-20M` control until the user manually changes the range again.

5. Medium: validation failures in the add-income, add-expense, and add-healthcare flows destroy user input and replace the full results column with a standalone error fragment. All three forms target `#whatif-results`, but the income and expense forms unconditionally call `this.reset()` after every request in `web/templates/components/whatif/income-sources-list.html:77` and `web/templates/components/whatif/expense-sources-list.html:83`, while the healthcare form unconditionally hides itself via `toggleAddHealthcare()` in `web/templates/components/whatif/healthcare-card.html:24`. On the server side, `renderError()` returns only a red error box in `internal/handlers/whatif/handlers.go:321`. A normal 400 validation error therefore wipes out the right-column analytics and clears or hides the form, forcing the user to re-enter their values.

6. Low: changing the portfolio range dropdown likely triggers duplicate `/whatif/settings` recalculations. The form listens for `change` events at `web/templates/components/whatif/portfolio-settings.html:27`, the range selector calls `updatePortfolioRange()` on change at `web/templates/components/whatif/portfolio-settings.html:33`, and that helper dispatches a synthetic `change` event on the slider at `web/templates/components/whatif/portfolio-settings.html:103`. The natural select change and the synthetic slider change both bubble to the form, so one UI action can kick off two analysis requests.

7. Medium: the rate-assumptions card contains invalid nested forms for the glide-path module. The main card opens a `/whatif/settings` form at `web/templates/components/whatif/rate-assumptions.html:7`, then starts a second `/whatif/glide-path` form inside it at `web/templates/components/whatif/rate-assumptions.html:287`, with the outer form not closing until `web/templates/components/whatif/rate-assumptions.html:406`. Nested forms are invalid HTML, so browser recovery rules determine which controls belong to which submission. That makes the glide-path controls and the remaining settings controls structurally fragile and vulnerable to browser- or HTMX-dependent mis-submission.

8. Medium: the spending-phases module allows users to create more phases than the update handler can persist. The UI keeps exposing `Add Phase` with no visible cap in `web/templates/components/whatif/spending-phases.html:88`, and `handleWhatIfAddPhase()` appends unboundedly in `internal/handlers/whatif/handlers.go:1915`. But `handleWhatIfSpendingPhases()` only rebuilds phases for indices `0..19` in `internal/handlers/whatif/handlers.go:1807`. Once a scenario has more than 20 phases, the next normal save silently drops everything after phase 20.

9. Medium: the Roth conversion handler accepts invalid schedules without server-side validation, and negative annual amounts can leak into tax calculations. `handleWhatIfRothConversion()` blindly stores parsed numeric fields in `internal/handlers/whatif/handlers.go:2156` without rejecting negatives or `end_year < start_year`. The calculator then returns `math.Min(s.RothConversion.AnnualAmount, availableTaxDeferred)` in `internal/services/retirement/calculator.go:415`, which means a negative annual amount stays negative. That raw value is consumed in tax snapshot paths such as `internal/services/retirement/calculator.go:1406` and `internal/services/retirement/calculator.go:1524`, so malformed input can distort displayed tax and steady-state outputs rather than failing fast.

10. Low: the big-ticket handler silently coerces malformed or negative years to `0` instead of rejecting the request. The UI presents year as a bounded non-negative field in `web/templates/components/whatif/bigticket-card.html:111`, but `handleWhatIfAddBigTicket()` falls back to `year = 0` on parse failure and also rewrites negative years to `0` in `internal/handlers/whatif/handlers.go:2218`. A bad submission therefore becomes “apply immediately” rather than returning a validation error. (Note: negative amounts are now properly rejected with HTTP 400 at lines 2208–2215; only the year coercion remains.)

11. Medium: the budget-analysis steady-state slider can anchor to the wrong minimum year when Social Security is being projected by the optimizer instead of entered as a regular income source. The budget panel derives `MinSteadyStateYear` from `findSteadyStateMonth()` in `internal/services/retirement/calculator.go:1477`, but that helper only scans `Settings.IncomeSources` in `internal/services/retirement/calculator.go:1573`. Actual monthly income calculations also include projected Social Security through `calculateMonthlyIncomeBreakdown()` in `internal/services/retirement/calculator.go:369`. In optimizer-driven scenarios, the UI can therefore offer a “steady state” year before projected Social Security has started, which makes the panel’s “all income active” framing incorrect.

12. Low: the budget-analysis card contradicts itself about whether steady-state values are nominal or inflation-adjusted. The panel intro says, “Steady-state values are in future nominal dollars” in `web/templates/components/whatif/budget-analysis.html:6`, but the steady-state footer says, “Values shown in year-X dollars (inflation-adjusted)” in `web/templates/components/whatif/budget-analysis.html:215`. The calculator path inflates expenses and projected balances forward in nominal terms when filling the steady-state fields in `internal/services/retirement/calculator.go:1498`. Even if the math is acceptable, the card is currently self-contradictory and invites misreading.

13. Medium: the sensitivity-analysis “Higher Healthcare” scenario stops being meaningful as soon as the page uses the multi-person healthcare model. The scenario mutates only the legacy `MonthlyHealthcare` field in `internal/services/retirement/calculator.go:1698` and `internal/services/retirement/calculator.go:1715`. But expense calculations prefer `HealthcarePersons` whenever any person records exist, via `WhatIfSettings.GetTotalHealthcareCost()` in `internal/models/whatif.go:471` and `CalculateTotalExpenses()` in `internal/services/retirement/calculator.go:549`. On the current what-if page, healthcare is configured through person entries in `web/templates/components/whatif/healthcare-card.html:1`, so the sensitivity card can present a “Higher Healthcare” row that does not actually change the simulated scenario.

14. Low: the historical-backtest card hardcodes a crash-year explanation that may not match the scenario’s actual worst starting years. The UI always shows “Includes market crashes of 1929, 1966, 1973, 2000, 2008” in `web/templates/components/whatif/historical-backtest.html:58`, but the actual `WorstStartYears` list is dynamically generated from the scenario results in `internal/services/retirement/backtest.go:62`. For shorter horizons or unusual cash-flow profiles, those years may not appear in the returned list at all, so the helper text can overstate confidence and imply a causal interpretation the engine did not specifically derive.

15. Medium: invalid scenario chains are silently deleted on ordinary saves instead of being surfaced back to the user. `saveInternal()` strips `settings.ScenarioChain` whenever validation fails in `internal/services/retirement/settings.go:421`, logging only a server-side warning. The behavior is even codified in `internal/services/retirement/settings_crud_test.go:1398`, where changing age causes a previously valid chain to disappear on the next save. A user adjusting `CurrentAge` or `ProjectionYears` can therefore lose a carefully built chain with no in-product warning or recovery path.

16. Medium: scenario CRUD handlers map normal user errors to HTTP 500s, so the UI treats validation and missing-file cases as server failures. `handleCreateScenario()`, `handleSwitchScenario()`, `handleDeleteScenario()`, and `handleRenameScenario()` all wrap manager errors as “Failed to …” and return `http.StatusInternalServerError` in `internal/handlers/whatif/handlers.go:2350` and `internal/handlers/whatif/handlers.go:2407`. That includes common cases like missing files, invalid filenames, deleting the default scenario, or deleting a referenced scenario. The current tests even assert those 500s in `internal/handlers/whatif/handlers_test.go:3658`, which means the bad status behavior is locked in. (Note: empty-name validation correctly returns 400; only service-layer errors are mis-mapped to 500.)

17. Low: scenario names are not trimmed, so whitespace-only names are accepted and can produce blank-looking entries in the scenario switcher. The handlers only reject `name == ""` in `internal/handlers/whatif/handlers.go:2343` and `internal/handlers/whatif/handlers.go:2399`, while `CreateScenario()` persists the raw display name in `internal/services/retirement/settings.go:1229`. `slugify()` falls back to a generic filename like `whatif_scenario.json` in `internal/services/retirement/settings.go:1061`, but `readScenarioName()` returns the whitespace name as-is in `internal/services/retirement/settings.go:1084`, so the UI can show an effectively blank scenario label.

18. Low: cache behavior is inconsistent across what-if mutations because some expensive handlers bypass the shared analysis cache entirely. Most flows use `runAnalysisWithCache()`, but `handleWhatIfSocialSecurity()`, `handleWhatIfGlidePath()`, and `handleWhatIfGuardrails()` save settings and then call `buildCalculator()` plus `RunFullAnalysis()` directly in `internal/handlers/whatif/handlers.go:2556`, `internal/handlers/whatif/handlers.go:2617`, and `internal/handlers/whatif/handlers.go:2694`. Functionally that still works, but it defeats the documented 5-minute reuse path for repeated submissions of identical settings and makes performance depend on which card the user edited.

## Notes

- This document is intentionally append-only while the review is in progress.
- Later sections should be added under `## Findings` with continuing numbering so fixes can be tracked in one place.

## Implementation Status

Applied on 2026-04-11:

- Finding 1 fixed. `GET /whatif` no longer auto-syncs and saves dashboard data on page load.
- Finding 5 fixed. Add-income, add-expense, add-healthcare, and big-ticket validation errors now retarget to inline form error regions instead of wiping results, and the add forms only reset/hide after successful requests.
- Finding 8 fixed. Spending-phase saves now preserve submitted phases beyond index 19.
- Finding 9 fixed. Roth conversion updates now reject negative amounts, negative years, and `end_year < start_year`.
- Finding 10 fixed. Big-ticket adds now reject malformed and negative years instead of coercing them to year `0`.
- Finding 11 fixed. Steady-state timing now includes optimizer-driven Social Security claim starts.
- Finding 13 fixed. The `Higher Healthcare` sensitivity scenario now varies person-based healthcare costs when `HealthcarePersons` is populated.
- Finding 2 fixed. The Social Security config card now refreshes out-of-band with `whatif-results`, so analysis-derived helper text and warnings stay in sync with the right-column results.
- Finding 3 fixed. Projection chart initialization no longer double-fetches on first load or after `#whatif-results` swaps.
- Finding 4 fixed. Portfolio slider range normalization now reruns after HTMX result swaps, so min/max/step stay aligned with the selected range after OOB card refreshes.
- Finding 6 fixed. Changing the portfolio range selector now suppresses the selector's bubbled `change` event and triggers only the canonical slider change, avoiding duplicate `/whatif/settings` recalculations.
- Finding 7 fixed. The rate-assumptions card no longer nests the glide-path form inside the main settings form.
- Finding 12 fixed. Budget-analysis steady-state copy now consistently describes those values as nominal dollars at the selected future year.
- Finding 14 fixed. Historical-backtest helper text now uses neutral scenario-specific language instead of hardcoded crash-year claims.
- Finding 15 fixed. Invalid scenario chains now fail save validation instead of being silently stripped.
- Finding 16 fixed. Scenario CRUD handlers now map user-correctable manager errors to `400`/`404`/`409` instead of `500`.
- Finding 17 fixed. Scenario create/rename paths now trim names and reject whitespace-only values, and blankish persisted names fall back to filenames in the switcher.
- Finding 18 fixed. Social Security, glide path, and guardrails mutations now use the shared `runAnalysisWithCache()` path instead of bypassing the what-if analysis cache.
- Related hardening: failed `Save` calls now clear the in-memory settings cache so invalid unsaved state does not leak into later reads.

All 18 findings from this review are now resolved.

Verification for the implementation pass:

- `go test ./internal/handlers/whatif`
- `go test ./internal/services/retirement`
- `go test ./...`

## Fix Plan

### Phase 1: Data integrity and correctness

Address these first because they mutate saved scenarios, silently drop user configuration, or make result panels materially wrong.

1. Stop `GET /whatif` from mutating scenario data.
   - Remove the automatic dashboard sync from `handleWhatIf()`.
   - If bootstrap behavior is still wanted, limit it to explicit user action or one-time initialization of the default scenario only.
   - Re-test opening blank scenarios and deliberate zero-income scenarios.

2. Make invalid scenario chains visible instead of silently stripping them.
   - Change `saveInternal()` to return a validation error for invalid chains rather than deleting them.
   - Surface that error in the chain UI and preserve the submitted chain values where possible.
   - Add/update tests for age changes and projection-year changes that invalidate an existing chain.

3. Fix the 20-phase persistence cap.
   - Either enforce a hard UI/server cap of 20 phases or update the save handler to round-trip arbitrary phase counts.
   - Add a regression test that saves more than 20 phases and verifies no truncation.

4. Add server-side validation for Roth conversion schedules.
   - Reject negative annual amounts.
   - Reject `end_year < start_year`.
   - Add tests covering malformed and negative inputs and verify tax outputs stay stable.

5. Correct the budget steady-state minimum-year calculation.
   - Include optimizer-driven Social Security timing when deriving `MinSteadyStateYear`.
   - Add a targeted calculator test where Social Security is projected but not present in `IncomeSources`.

6. Fix healthcare sensitivity so it affects active scenarios.
   - If `HealthcarePersons` is in use, vary person costs instead of the legacy `MonthlyHealthcare` field.
   - Otherwise suppress the row until a meaningful scenario variant exists.
   - Add a test proving the “Higher Healthcare” scenario changes outcomes under the multi-person model.

### Phase 2: UX and request-flow correctness

These do not corrupt core data, but they create stale UI, lost form state, or misleading error handling.

1. Normalize HTMX refresh boundaries across the page.
   - Decide which left-column cards are server-derived and refresh all of them consistently with OOB swaps.
   - At minimum, include the Social Security config card in the OOB refresh set if it continues rendering analysis-derived helper text.

2. Preserve form state on validation errors.
   - Stop resetting/hiding add forms on every response.
   - Return error fragments in a way that does not replace the full results column.
   - Re-test income, expense, and healthcare add flows with invalid values.

3. Fix scenario CRUD HTTP status mapping.
   - Return 400/404/409-style responses for user-correctable errors instead of 500.
   - Update handler tests accordingly.

4. Trim scenario names before create/rename.
   - Reject whitespace-only names.
   - Normalize saved display names with `strings.TrimSpace`.
   - Add tests for create/rename with blank and whitespace-only values.

5. Stop coercing malformed big-ticket years to zero.
   - Return validation errors for parse failures or negative years. (Negative amounts already reject with 400.)
   - Keep “year 0” only for explicit valid user input.

6. Clarify the budget-analysis copy.
   - Decide whether steady-state values are nominal or inflation-adjusted.
   - Make the intro and footer use the same terminology.

7. Remove the hardcoded crash-year claim from historical backtest.
   - Either derive the helper text from actual `WorstStartYears` or replace it with neutral explanatory copy.

### Phase 3: Structural cleanup and performance

These can follow once correctness issues are handled.

1. Eliminate duplicate projection-chart fetches.
   - Ensure chart initialization happens once per load/swap.
   - Re-test initial page load and `#whatif-results` refresh behavior.

2. Re-initialize portfolio slider behavior after HTMX refreshes.
   - Move range normalization into a reusable init function called on both `DOMContentLoaded` and HTMX swaps.
   - Verify min/max/step match the selected range after any settings change.

3. Remove duplicate portfolio-settings recalculations.
   - Prevent the range dropdown change from also causing a second bubbled slider-triggered submission.

4. Replace invalid nested forms in the rate-assumptions card.
   - Split the glide-path form out of the main settings form, or unify them into one valid submission boundary.

5. Make analysis caching consistent.
   - Route Social Security, glide path, and guardrails through `runAnalysisWithCache()` unless there is a clear reason not to.
   - Keep the async optimizer job state separate from the normal analysis cache.

### Suggested execution order

If we fix this incrementally, the safest order is:

1. Findings 1, 8, 9, 11, 13, 15.
2. Findings 5, 16, 17, 10.
3. Findings 2, 7, 3, 4, 6, 12, 14, 18.

### Good first PR split

1. Scenario integrity.
   - Findings 1, 15, 16, 17.

2. Strategy persistence and validation.
   - Findings 8, 9, 10.

3. Results correctness.
   - Findings 11, 12, 13, 14.

4. HTMX/UI refresh cleanup.
   - Findings 2, 3, 4, 5, 6, 7, 18.
