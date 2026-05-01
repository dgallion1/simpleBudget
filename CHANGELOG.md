# Changelog

## Unreleased

### Major Expenses — Pin Picker UX (2026-04-30)
- **Disambiguated, sorted "Pin to…" dropdown**: the picker used to render bare `Name` per option, so 4 different "Home Improvement" entries (different merchants) collapsed to 4 identical-looking rows. New `ExpenseOption` struct + `buildExpenseOptions` helper sort options alphabetically (case-insensitive) and append ` — <first keyword>` only when a name collides with another entry, so unique names like `Cellphone` stay short while `Home Improvement — Lowe's` / `Home Improvement — The Home Depot` are distinguishable. Sorting also makes a freshly-added entry land in its alphabetical position rather than at the end of an unsorted list.
- **Native `<option title>` for clipped labels**: each option now carries `title="{{.Label}}"` so hovering reveals the full label as a browser tooltip when the popup width clips long merchant names.
- **`min-w-[14rem]` on the select**: the closed picker shows just `Pin to…` (small), but browsers size the open popup to the closed select's width on some platforms, clipping options. A 14rem floor keeps the popup readable without forcing a wide closed state.
- **Auto-open matched-transactions disclosure when pins exist**: each `ExpenseSummary` now carries `PinnedCount`. The summary line shows `· 📌 N pinned` next to `N matched`, and the per-entry `<details>` opens by default when `PinnedCount > 0` so the 📌 row + `unpin` button are visible without a click — addresses "I pinned an exception and now I can't find it."
- **Test coverage**: new `TestBuildExpenseOptions_SortsAndDisambiguates` pins the sort + dedup contract (case-insensitive collision counting, suffix only on collisions, alphabetical order). Existing render tests updated to carry the new `PinnedCount` field on the local `summary` test struct.

### Major Expenses — Cross-Page Surfacing (2026-04-30)
- **Explorer "Major Expense" column + filter**: the transaction list at `/explorer` now shows each row's matched major-expense name (indigo badge) in a new column between Category and Amount. A new "Major Expense" filter dropdown narrows the table to one expense's transactions and composes with the existing Category / Type / Date / Search filters. Same HTMX form, no extra round-trips. Both `handleExplorer` and `handleTransactionsPartial` apply the annotation via a new `annotateAndFilterByMajorExpense` helper that loads expenses + pins, runs `Match()`, builds a hash → name lookup, and optionally narrows the set when `?majorExpense=<id>` is passed. Empty-state colspan bumped from 6 to 7.
- **Insights badge on recurring payments**: each detected recurring payment in the Bills & Recurring table on `/insights` now shows a small indigo badge with its mapped major expense (if any) next to the description. New `RecurringPayment.MajorExpenseName` field; new `majorexpenses.AnnotateRecurringPayments` helper that honors pins on the first transaction and falls back to keyword/amount matching otherwise. Annotation is wired into both `calculateInsights` (full page) and `handleRecurringPartial` (HTMX refresh) via a small `annotateRecurringWithMajorExpense` wrapper that handles a nil loader gracefully so existing unit tests still run.
- **Dashboard "Spending by Major Expense" pie chart**: a new chart card on `/dashboard` next to the existing Category card. Renders user-declared expenses as slices of total outflow spending, with an "Unmatched" residual at the end so the totals add up to the period's total. New endpoint `GET /dashboard/charts/data/major-expense` and parallel builder `buildMajorExpenseChartData` that mirrors `buildCategoryChartData`'s pie-chart output shape — auto-discovered by the existing `[data-chart-url]` Plotly bootstrap.
- **Test coverage**: 6 new engine tests for `AnnotateRecurringPayments` (pin wins, fallback to keyword, empty / nil / missing-Transactions edge cases, orphan-pin fallback) and 2 new dashboard chart tests (empty-data short-circuit, all-unmatched residual = total).

### Major Expenses — Per-Transaction Pinning (2026-04-30)
- **Pin transactions to a specific expense regardless of keyword/amount**: a new `data/transaction_pins.json` (hash → expense ID) lets the user explicitly assign individual transactions to a major expense. Solves the "I have 60 Amazon orders, none of them have category-distinguishing descriptions" problem: create empty Amazon-Books / Amazon-Household / Amazon-Gifts entries (Name only, no keyword, no amount) and pin each Amazon row to the right one. Pins survive re-imports because they're keyed by the existing `Transaction.Hash` (date+description+amount).
- **Engine pin precedence**: `Match()` consults pins FIRST and only falls back to keyword/amount matching when a transaction has no pin or its pin points to a deleted expense (orphan pins are pruned automatically on `DeleteMajorExpense`). Pinned transactions are never flagged as anomalous since the user explicitly accepted them — anomaly detection skips `pinned[Hash]`.
- **UI**: each unmatched / new-merchant / anomalous row now has a "Pin to…" `<select>` dropdown listing every existing major expense; choosing one fires `POST /major-expenses/pins` with `{hash, expense_id}` and OOB-refreshes the list + exceptions panel. Click delegation ignores `select`/`option` clicks so the dropdown doesn't accidentally trigger row pre-fill. Anomalous rows keep both affordances: dropdown to "Move to existing", body click to "Move to new", expense-name link to "Edit existing".
- **Unpin from matched-transactions list**: each pinned transaction in an expense's "Show matched transactions" disclosure is prefixed with 📌 and gets a small "unpin" button that fires `DELETE /major-expenses/pins/{hash}`. Once unpinned the transaction either reverts to keyword/amount matching for that expense or returns to Unmatched.
- **Storage layer**: `LoadTransactionPins`, `SetTransactionPin`, `ClearTransactionPin`, and `PrunePinsForMissingExpenses` mirror the existing aliases.json pattern (`dl.store.ReadFile/WriteFile`, `os.IsNotExist` empty default, no mutex). `MatchOptions.Pins` and `MatchResult.PinnedHashes` thread the data through the engine.
- **Test coverage**: 11 storage tests (round-trip, overwrite, empty-hash rejection, prune-orphans), 4 engine tests (pin overrides keyword, fallback when target missing, suppresses anomaly, ignores empty hash), 4 handler tests (pin success / unknown-expense / empty-hash / unpin / delete-cascade-prunes-pins). Existing detectAnomalies tests updated to pass `nil` pinned map.

### Major Expenses — UX Refinements (2026-04-30)
- **Inline edit form was rounding to integers**: per-item Min/Max inputs used `%.0f` + `step="1"`, so editing any field on an existing entry overwrote stored cents. A def saved as `1580.43 / 1580.43` would silently become `1580 / 1580` on the next change event, breaking exact-amount matches against transactions like `$1580.43`. Now uses `%.2f` + `step="0.01"`. Add-form Min/Max inputs got the same treatment.
- **Anomalous rows are now click-to-reclassify**: previously a click jumped to the matching expense's edit form. Now the row click pre-fills the add form (so a `$626` check sitting in Lucid's anomalous bucket can be moved to a new "Hyundai" entry in one click). The expense-name link inside the row still scrolls to and highlights the existing entry — handled first in the document-level click handler so it doesn't fall through to the row-level pre-fill.
- **Smarter click-fill**: clicking a row used to always pre-fill `Min == Max == amount`, which silently trapped the user into AND-filter mode. Now the pre-fill is description-aware: check-like descriptions (matching `\bcheck\b` or pure digits) clear keywords and pin the exact amount; everything else pre-fills the keyword and leaves Min/Max blank, so the keyword catches every similar transaction by default. Users who want a tighter range can add it manually.
- **Per-entry matched-transactions disclosure**: each major-expense item now exposes a "Show matched transactions (N)" details element that lists every matched transaction (date, description, amount), most-recent first. New `Transactions` field on the per-expense `ExpenseSummary` carries the matched slice from handler to template.
- **How matching works help text rewrite**: the four matching modes (keyword only, exact amount only, keyword + exact amount AND filter, range) are now spelled out under the add form, including the user's "Lucid `check` / `1580.00` and Car `check` / `626.00` coexist" example.

### Major Expenses — New Page (2026-04-30)
- **New page at `/major-expenses`** for declaring expenses you understand (rent, mortgage, gym, fixed-amount checks, etc.) so the app can group similar transactions and call out exceptions. Persisted as `data/major_expenses.json` alongside `aliases.json`; first-run is intentionally blank — no auto-seed from recurring detection.
- **Three exception buckets** computed against the full imported transaction set: unmatched outflows over $100, matched-but-anomalous-amount (transactions in a declared expense's group whose amount falls outside its expected range), and new merchants whose normalized description was not seen before the trailing 30-day window. Anomaly check runs only against keyword-matched transactions; range-only and exact-amount-only matches are in-range by definition. New merchants are deduped to first occurrence and exclude income transactions.
- **Four matching modes** in one model: (1) keyword only — case-insensitive substring on description / display-name; first-def wins; range, if set, is anomaly-only. (2) Min == Max only — match transactions of that exact dollar value within ±$0.01 (useful when descriptions vary, like checks). (3) **Keyword + Min == Max — AND filter**: both must match. Lets users disambiguate transactions that share a generic keyword like "check" by pinning each definition to its own dollar amount (e.g., Lucid `check`/`1580.00` and Car `check`/`626.00` coexist). (4) Min < Max range without a keyword — match anything in the range. Validation requires either keywords OR a Min/Max pair.
- **Click-to-pre-fill from exception rows**: clicking a row in Unmatched or New Merchants seeds the add form with min/max set to the exact transaction amount (cents preserved) and focuses the Name field; check-like descriptions auto-clear the keyword field so the user falls back to amount-only matching. Anomalous rows scroll to and highlight the matching list entry on the left for in-place range adjustment.
- **Searchable exceptions panel**: client-side filter across all three buckets matches description, major-expense name (for anomalous rows), amount (`625` finds `$625.35`), and date (`2025-12` finds that month). Auto-expands buckets that contain matches and shows `X of Y` count. Survives HTMX swaps via document-level event delegation.
- **HTMX-driven CRUD** with OOB swap on every mutation so a single response refreshes both the list card and the exceptions panel; full-page `GET /major-expenses` and partial `GET /major-expenses/exceptions` for polling/refresh. Inline edit on each list entry with `change delay:500ms` debounced PUT. Add form sits at the top of the left card with collapsible "How matching works" help text.
- **Test coverage**: 29 engine tests (matching, exact-amount tolerance, anomaly detection, new-merchant cutoff/dedup/normalization, OR-with-keyword), 11 storage round-trip + timestamp tests, 10 handler tests (CRUD success + 5-case validation table including amount-only-accepted), and 3 template render tests asserting click-fill / jump / OOB / search plumbing. New `internal/services/majorexpenses/` engine package, new `internal/handlers/majorexpenses/` handler package, new `web/templates/pages/major-expenses.html`. Nav entry added between Insights and File Manager.

### What-If Planner — Bug Fixes (2026-04-30)
- **Failure Thresholds: hide Investment Return card in allocation mode**: When `InvestmentReturn == 0` (the sentinel that tells the projector to derive returns from per-account asset allocation), `findReturnThreshold` was running its binary search against the literal 0 and rendering a meaningless card with `Current: 0.0%`, `Fails if below: 0.0%`, `Safety margin: 0.0 pts`. The threshold now returns nil in allocation mode so the card is omitted; the inflation, expenses, and portfolio thresholds still render as before.
- **Success-rate color tiers: 5-tier gradient instead of 90/75 binary**: Monte Carlo and Historical Backtest cards used a two-threshold scheme that painted everything below 75% red — so a 72.6% rate (planner-marginal) looked identical to a 30% rate (failing plan). New tiers track common retirement-planning convention: ≥90 green, ≥80 lime, ≥70 yellow, ≥60 orange, <60 red. Logic moved to two new template helpers (`successRateTextClass`, `successRateBarClass`) so the same mapping is reused across both cards.
- **Monte Carlo risk-event labels: consistent "% of runs" framing**: The card displayed three different units under the same `runs` suffix — `1.6/run` (avg crashes per simulation), `909 runs` (count of runs with ≥1 spending shock), `784 runs` (count of runs with ≥1 health event). All three now lead with `% of runs` so the denominator is consistent with the success rate above. Market Crashes additionally shows the avg-count-per-run as a secondary line (`avg 1.5 each`) since the count is the more interesting datum when most simulations see at least one crash. `percentOf` template helper widened to accept any numeric type so int counts can be passed directly.
- **Historical Backtesting: failures-first table order, survival count, clearer Failed label**: The "View All N Sequences" table sorted by Final Balance descending, so users scrolled past the best survivors before reaching the failed sequences that explain the sub-100% success rate. The table now puts failures first (sorted by depletion year, quickest first), then survivors ascending — reading top-to-bottom matches the "Worst → Best" mental model and the failures called out in the chips above. Added a `X of Y survived` subtitle under the historical-success percentage so the headline answers "did they all survive?" at a glance. Renamed the per-row label from `Failed (1966)` to `Failed in 1966` (with a `title` tooltip clarifying it's the depletion year) since the parenthetical was being misread as the start year. New `SurvivedCount` field on `HistoricalBacktestAnalysis`.

### Testing — Coverage Expansion (2026-04-30)
- **whatif**: 84.6% → 98.6% (+14.0 pts). ~85 new error-path tests covering ParseForm failures (real `%ZZ` percent-encoding, since the existing multipart-no-boundary trick is silently accepted by modern Go), `Save()` failures (chmod-based), `Load()` failures, and `runAnalysisWithCache` failures (corrupt scenario chain — a `whatif_corrupt.json` file referenced from the active scenario's chain that parses on file-existence but fails on Load). 11 handlers went from 70-78% to 100%: RothConversion, GlidePath, Guardrails, SocialSecurity, ResetPhases, UpdateChain, DeleteChainLink, plus all Delete/Restore handlers.
- **retirement**: 94.6% → 97.2% (+2.6 pts). New tests for the four scenario error types (`ScenarioChainValidationError`, `ScenarioValidationError`, `ScenarioNotFoundError`, `ScenarioConflictError` — `Error()`/`Unwrap()` 0% → 100%), `findPreparedScenarioPerson` (0% → 100%), `ensurePrimaryPerson`/`ensureSpousePerson` (42-57% → 100%), `UpdateSettingsWithPersons` (0% → 90.9%), plus coverage for `normalizeStartDate`, `inferHealthcarePersonLink`, `decodeSettings`, `loadInternal`, and `removeSpousePersons`.
- **New test infrastructure** (whatif): `setupTestEnvWithDir` (returns settings dir for chmod tests), `makeSaveFail` (chmod 0o500 with cleanup), `setupItemsThenBreakChain` (corrupt JSON via dangling chain link), `badEncodedRequest` (real ParseForm failures).
- **Remaining ceilings** (intentional): storage 85.3% / backup 87.4% (YubiKey hardware paths), cmd/validate 86.5% (`main()` ceiling), whatif's last 1.4% (`getSettingsHash` json.Marshal-of-struct uncoverable, `handleListScenarios` filepath.Glob err unreachable from hardcoded pattern, `handleSwitchScenario` Stat-only path not breakable via chmod).

### What-If Planner — Hardening (2026-04-29)
- **Cost styling consistency**: Healthcare per-person, ACA, Medicare, total healthcare, and active expense amounts now render in red (`text-red-600 dark:text-red-400`) rather than neutral gray, matching the project rule that cost numbers are visually distinct from neutral and income figures.
- **Restore-path safety**: `RestoreIncomeSource`, `RestoreExpenseSource`, and `RestoreBigTicketItem` now reject restoring an ID that already exists in the active list (HTTP 409) and surface a 404 when the ID is not in the removed list, instead of silently no-oping or producing duplicate active entries from a hand-edited file.
- **Icon-button accessibility**: Decorative SVGs in the income, expense, big-ticket, and scenario-chain list cards now carry `aria-hidden="true"`, and the icon-only delete/restore buttons gained `aria-label` so screen readers announce a meaningful action.
- **`handleWhatIfSettings` refactor**: The 363-line super-handler is now 41 lines orchestrating a declarative `fieldSpec` table (`internal/handlers/whatif/form_spec.go`). Per-field parse, bounds, and enum validation live in the spec; cross-field invariants (`tax_deferred + roth ≤ 100`, `stock + cash ≤ 100`) and per-account allocation clamping run as discrete post-passes. All error messages preserved byte-for-byte; existing 22 `TestHandleWhatIfSettings_*` tests pass unchanged.
- **`handlers.go` file split**: The 2,797-line monolith was split by domain into `handlers.go` (shared helpers, page handler, sync), `handlers_income_expense.go`, `handlers_healthcare.go`, `handlers_scenarios.go`, and `handlers_rates.go`. Pure file move — no exported APIs or behavior changed; `RegisterRoutes` still wires every route from one place.

### Documentation
- **What-If hardening note**: Added `docs/what-if-hardening-2026-04-29.md` documenting the structural refactor, file split, restore-path fix, a11y pass, and three Phase-1 over-claims that were investigated and dismissed (Load() TOCTOU, DeleteScenario race, glide-path/guardrails clamping).
- **What-If retirement verification**: Added a verification note documenting the Current Plan inputs, live page values, calculator cross-check, and test command used to confirm the retirement numbers shown on the What-If page.

### What-If Planner — New Features
- **Social Security claiming age optimizer**: New analysis panel lets users enter their PIA (monthly benefit at FRA) and compare claiming ages 62–70 with adjusted monthly benefits, cumulative totals at ages 80/85/90 with COLA, and breakeven ages between adjacent options. Supports spouse comparison. Selected primary and spouse claim ages now feed directly into retirement projections, and manual Social Security-like income sources are excluded while optimizer-driven projection is active to prevent double counting.
- **Glide path (time-based allocation shift)**: Asset allocation can now shift linearly from a start stock% to an end stock% over a configurable number of years. Applied uniformly to all account types across deterministic, Monte Carlo, and historical backtest projections.
- **Spending guardrails**: Portfolio-performance-based spending rules that automatically cut spending when the portfolio drops from its peak (floor) and raise spending when it rises above baseline (ceiling), with configurable min/max caps. Works in deterministic projection, Monte Carlo, and backtesting. Replaces adaptive spending when enabled.

### What-If Planner
- **Canonical people and start month**: What-If settings now persist `start_date` plus a canonical `persons` list instead of saving scenario-level age fields. Working ages are derived in memory from each person's `birth_month`, keeping chained scenarios and healthcare projections aligned to one age baseline.
- **Linked healthcare people**: Healthcare rows can now link to a canonical person with automatic name and age derivation, while manual healthcare entries remain supported for one-off cases.
- **What-If settings UI**: Replaced the old primary/spouse age inputs with a projection start month, editable person rows, derived age previews, and spouse-aware phase-reference normalization in the What-If settings card.
- **IRMAA lookback correction**: Current and near-term what-if projections now respect IRMAA's 2-year MAGI lookback instead of charging Medicare surcharges immediately from the current modeled income year. Budget-fit now surfaces an explicit current IRMAA estimate from modeled MAGI, while steady-state IRMAA is derived from a two-year-earlier MAGI estimate so the budget card stays aligned with the projection engine.
- **IRMAA baseline consistency**: Rebased the Medicare surcharge table onto the planner's 2024 tax baseline and applied the same year-based IRMAA inflation logic across deterministic projections, Monte Carlo runs, and historical backtests so equivalent scenarios no longer disagree by engine.
- **Projection assumptions summary**: Added a compact assumptions panel to the portfolio longevity card covering after-tax cash flow, average-cost taxable sales, annual Roth conversion timing, and the active monthly cash-flow timing mode.
- **Reconciliation table readability**: Improved the year-by-year projection table with sticky headers, alternating row emphasis, tabular numeric alignment, clearer `Gross Cash In` / `Portfolio Out` terminology, and a more visually distinct real-balance sublabel.
- **Tax breakdown labeling**: Renamed the yearly projection tax columns to `Taxes Incl. NIIT` and `NIIT Portion` so the explainability table makes clear that NIIT is already included in the total tax number.
- **Template coverage**: Added render tests covering the new projection assumptions summary and the updated reconciliation table copy.

## v1.9.1 - Test Coverage Expansion

### Testing
- **config**: Added 18 tests — 100% coverage (DefaultConfig, Load, ensureDirectories, LoadUserSettings, SaveUserSettings)
- **classifier**: Added tests — 100% coverage (ClassifyTransactions, IsInternalTransfer, IsPotentialIncome, containsAny)
- **metrics**: Added tests — 100% coverage (CalculateMetrics, CalculateComparison, PercentChange)
- **templates**: Added tests — 100% coverage (all 21 helper functions + render infrastructure)
- **models**: Added tests for transactions, healthcare, whatif, income sources, user profile — 99.5% coverage (2 unreachable dead-code guards)
- **version**: Added 12 tests — 89.7% coverage (remaining is VCS metadata only available at build time)
- **dataloader**: Added 40+ tests — 99.6% coverage (remaining is unreachable json.MarshalIndent on map[string]string)
- **dashboard**: Added 60+ HTTP handler tests — 100% coverage (all chart types, KPIs, drilldowns, exports)
- **insights**: Added 62+ tests — 98.2% coverage (all 6 handlers + recurring detection, trends, velocity)
- **retirement**: Added coverage gap tests — 98.3% coverage (income calc, tax estimation, Roth conversion, withdrawals, big-ticket expenses, chain settings, budget fit, threshold analysis)
- **explorer**: Added 63 tests — 99.0% coverage (all handlers, sort, pagination, file ops, alias CRUD)
- **whatif**: Added 222 tests — 84.8% coverage (all settings, income/expense/healthcare CRUD, scenarios, chains, spending phases)
- **backup**: Added 55 tests — 82.0% coverage (backup/restore, encryption enable/disable, auth methods, key detection)
- **storage**: Added 126 tests — 72.3% coverage (all providers except YubiKey, encryption roundtrips, migrations, caching)
- **cmd/server**: Added 30 tests — 81.2% coverage (middleware, setup, version, kill previous instance)
- **cmd/validate**: Added tests — 86.5% coverage (endpoint validation, error paths, subprocess exit tests)
- **internal/http**: Added 11 tests — 100% coverage (render, error response, date range parsing)
- **Refactoring for testability**: extracted `run()` from `cmd/server/main()`, injected `exitFunc` in backup, injected `readBuildInfo` in version, removed 2 unreachable dead-code guards in models
- **retirement (SS/NIIT/IRMAA)**: Added 43 tests for new tax features — SS provisional income thresholds, NIIT with non-qualified dividends, IRMAA MFS tiers, buildProjectionExplainability 42%→100%
- Overall project coverage: 44.5% → 93.6%

## v1.9.0 - Projection Realism & Explainability

### Breaking Changes
- **Monthly compounding**: Inflation, COLA, and healthcare cost escalation now compound monthly instead of stair-stepping annually. Existing projections will shift slightly — expenses accrue more smoothly within each year rather than jumping at year boundaries.

### New Features
- **Tax-aware projections**: Projection loop now estimates federal + state taxes monthly using a YTD accumulator with iterative convergence. Withdrawals from tax-deferred accounts, RMDs, dividends, and capital gains distributions all generate realistic tax drag.
- **Long-term capital gains tax rates**: Qualified dividends and long-term capital gains are taxed at preferential LTCG rates (0%/15%/20%) instead of ordinary income rates.
- **Taxable account modeling**: New settings for dividend yield, qualified dividend share, and capital gains distribution rate. Taxable account withdrawals use average cost basis to determine realized gains.
- **Projection timing**: New setting controls whether portfolio growth is applied before (start of month), around (mid-month), or after (end of month) monthly cash flows.
- **Real-dollar tracking**: Projection months now include cumulative inflation and real (inflation-adjusted) portfolio balance, expenses, and income.
- **Projection explainability**: New year-by-year breakdown table showing starting balance, growth, income, taxes, expenses, withdrawals, and ending balance for each projection year.
- **Chart event annotations**: Projection chart now marks key life events (Social Security start, Medicare transitions, RMD start, scenario chain transitions) as diamond markers.
- **Real/nominal chart toggle**: Projection chart supports switching between nominal and inflation-adjusted (today's dollars) views.

### Bug Fixes
- **Budget analysis subtitle**: Corrected subtitle to clarify that steady-state values are in future nominal dollars, not today's dollars.
- **Qualified dividend migration**: Settings migration now checks for JSON field presence instead of zero-value sentinel, preventing silent override when user intentionally sets qualified share to 0%.
- **Dashboard monthly chart wrong totals**: Monthly Income vs Expenses chart used `MonthlyTotals()` which summed raw transaction amounts then applied `math.Abs` to the monthly total. Mixed-sign outflow transactions within a month cancelled out, showing ~$5,500 instead of ~$27,800. Switched to `GroupByMonth()` + `SumAbsAmount()` to match KPI calculations. Same fix applied to Spending Trend chart.
- **Inflation disclaimer**: Added helper text to Monthly Living Expenses slider and Retirement Spending Phases card clarifying values are in today's dollars and inflation is applied during projection.

## v1.8.1 - Code Review Fixes

### Bug Fixes
- **Cumulative balance chart wrong**: Chart used raw `Amount` sign to determine income vs expense, but some bank CSVs export outflows as positive numbers. Now uses `TransactionType` field to determine direction, so outflows are always subtracted regardless of amount sign
- **Backtest depletion sort**: `yearsUntilDepletion` returned raw calendar year instead of 0 when `DepletionYear < StartYear`, causing wildly wrong sort ordering in edge cases
- **URL parameter double-append**: Chart loading in `charts.js` and `dashboard.js` used string concatenation (`url + '?' + params`) which produced malformed URLs when the base URL already contained a query string; now uses `URL`/`URLSearchParams` for safe merging
- **Chart fetch error handling**: `loadChart` and `refreshCharts` now check `response.ok` before parsing JSON, preventing silent failures that left "Loading chart..." displayed forever
- **Explorer redirect loop**: Filter persistence restore could loop infinitely if saved params were invalid; added a one-shot guard via sessionStorage
- **Dead `min()` function**: Removed user-defined `min(a, b int)` in `backtest.go` that shadowed the Go builtin

### Security Hardening
- **Alias input validation**: `handleAlias` now validates hash length (max 128) and display name length (max 200) to prevent storage bloat from malicious input
- **File delete path traversal**: `handleFileDelete` now reuses `sanitizeUploadFilename` instead of duplicated inline path-traversal checks

### Accessibility
- Projection chart toggle group now has `aria-label` and buttons track `aria-pressed` state
- Explorer step-back/forward buttons now have `aria-label` for screen reader users

### Code Quality
- Removed dead code: `portfolioBalances`, `monthlyAccountReturns`, `applyPortfolioGrowth`, `executePortfolioCashFlow`, `reinvestRequiredRMD`, `unrealizedGains` — superseded by tax-aware equivalents
- Renamed `mergeSimlarGroups` → `mergeSimilarGroups` (typo fix)
- Fixed import grouping in `render.go` (stdlib before third-party)
- Added trailing newlines to `expense-sources-list.html` and `income-sources-list.html`
- Alias endpoint returns `204 No Content` instead of empty `200 OK`

## v1.8.0 - Dashboard Redesign

### Changes

#### Dashboard
- Removed spending alerts panel (noisy, not actionable)
- Removed daily cash flow chart (too noisy) and weekly spending pattern chart (low value)
- Added **Spending Trend** chart: month-over-month spending change as percentage bars (green = decreased, red = increased) with hover showing actual dollar amounts for current and prior month
- Cumulative Balance chart now spans full width for better readability
- Charts now use direct fetch() for reliable updates on date range and preset changes (replaced fragile HTMX hx-swap="none" pattern)
- Removed `SpendingAlert` model type and `/dashboard/alerts` endpoint

#### Bug Fixes
- **Insights recurring freshness**: Active recurring payments now stay visible relative to the selected dataset end date instead of disappearing when imported data is old relative to wall-clock time
- **Insights historical windows**: Recurring detection now ignores transactions after the selected end date, so past views do not leak in subscriptions that started later
- **Historical backtest worst-year ranking**: Failed historical sequences are now ranked by years until depletion relative to their own start year instead of by absolute calendar year
- **Make fuzz target**: `make fuzz` now runs valid per-package fuzz commands and exits cleanly with guidance when the repo has no `Fuzz*` tests yet

## v1.7.0 - Scenario Chaining & Bug Fixes

### New Features

#### Scenario Chaining
- Chain multiple retirement scenarios to run sequentially with different assumptions at each life phase
- Example: run "Early Retirement" plan from age 60-70, then switch to "Post Social Security" plan from 70 onward
- Portfolio balances carry over between phases — only assumptions change (expenses, income, allocation, healthcare)
- Configure chains in the new "Scenario Chain" card on the What-If page: pick a scenario and a transition age
- Unlimited chain links with ascending transition ages
- Chain-aware across all analysis outputs: projection chart, Monte Carlo, historical backtest, sensitivity, and failure-point analysis
- Budget-fit, present-value, and RMD panels show a note when chain is active (chain support coming in a future release)
- Referential integrity: scenarios referenced in a chain cannot be deleted
- Chain validation on every save: if changing your current age invalidates a chain, it is automatically removed

#### Spending Phase Dollar Amounts
- Spending phase sliders now show both percentage and equivalent monthly dollar amount (e.g., "70% $6,440/mo")
- Updates live as the slider moves

### Bug Fixes
- **Projection depletion during tax-deferred delay**: A temporary shortfall where accessible accounts couldn't cover expenses but locked accounts still had funds was incorrectly treated as permanent depletion, stopping the projection early. Now only true depletion (total balance <= 0) stops the projection.
- **Invalid allocation blocking settings**: If per-account stocks + cash exceeded 100%, all settings changes were rejected with a 400 error. Now values are clamped automatically instead of blocking.
- **Dashboard date filter**: KPIs and alerts now update when the date range is changed
- **Insights date filter**: Date inputs now trigger page refresh on change
- **Recurring payment detection**: Recurring payments are now detected from full transaction history, so short date ranges no longer show $0

## v1.6.0 - Explorer Enhancements

### New Features

#### Transaction Renaming
- Double-click any transaction description to assign a custom display name
- Useful for cryptic entries like "Check #996574" - rename to "Plumber repair"
- Aliases stored in `aliases.json`, encrypted when encryption is enabled
- Original description shown in parentheses next to the alias
- Search matches both original description and custom name
- Clear the name to revert to the original description

#### Date Range Stepping
- Back/forward arrow buttons next to the 3M/6M/12M/All quick-range buttons
- Steps the entire date window forward or backward by its current duration
- Clamped to the min/max bounds of your data

#### Filter Persistence
- Explorer filter state (dates, search, category, sort) persists across tab changes
- Uses sessionStorage so settings survive navigation to other pages and back
- "Clear Filters" resets both the filters and the saved state

## v1.5.0 - Scenarios, Subscriptions & Budget Transparency

### New Features

#### What-If Scenarios
- Named scenarios let you explore different retirement plans without losing your current setup
- Create "Job Loss", "Early Retirement", etc. - each starts as a copy of the active plan
- Switch between scenarios instantly via dropdown at the top of the What-If page
- Rename or delete non-default scenarios; "Current Plan" is always preserved

#### Subscription Tracking (Insights)
- New dedicated Subscriptions section on the Insights page
- Automatically classifies recurring payments as subscriptions vs bills/retail
- Shows monthly subscription total with per-service breakdown
- KPI card added showing subscription cost at a glance

#### Monthly Budget Snapshot (What-If)
- Budget analysis now shows itemized expense and income breakdowns
- Each expense source listed with amount and notes (e.g., "ends year 3", "employer covered")
- Each income source listed with amount and start year
- Net cash flow summary at bottom

#### Healthcare Coverage Type Editing
- Coverage type (Employer/ACA/Medicare) is now a dropdown you can change directly
- Previously was a static label requiring removal and re-adding the person

### Improvements

- Recurring payment detection now uses fuzzy vendor matching — transactions with similar names (e.g., "Lucid" and "Lucidmotors.com") are merged into a single vendor group, so payments aren't missed due to inconsistent bank descriptions
- Amount-based recurring payment detection — payments with identical amounts at regular intervals are detected even when descriptions differ (e.g., check payments and direct bill pay to the same vendor)
- Go version updated to 1.26
- Income source number inputs now update results as you type (not just on blur)
- CSV upload merges new rows into existing files instead of overwriting (prevents data loss)

---

## v1.4.0 - Bug Fixes & Spouse Age Tracking

### Bug Fixes

#### Fixed Monthly Return Calculation
- **Critical fix**: Corrected compound interest calculation for monthly portfolio returns
- Old calculation used simple division (annual/12) instead of geometric conversion
- This was inflating projected returns by ~1.4%/year, compounding to 60% higher final balances
- Now uses correct formula: `monthly = (1 + annual)^(1/12) - 1`

#### RMD Calculations for Couples
- RMD calculations now correctly use the older spouse's age for joint accounts
- Fixed template type mismatch in healthcare timeline comparison

#### Per-Account Allocation Improvements
- Fixed issue where explicit 0% stock allocations were ignored
- Sensitivity analysis now uses effective return rate when in allocation mode
- Asset allocation changes now properly update projection charts

### New Features

#### Spouse Age Tracking
- Added spouse age tracking for retirement spending phases
- Enables more accurate RMD and healthcare cost projections for couples

### UI Improvements
- Show dollar amounts next to account types in asset allocation section
- Moved Income Sources card higher in what-if sidebar for better visibility
- Improved chart loading when settings change

---

## v1.3.0 - Per-Account Asset Allocation

### New Features

#### Per-Account Asset Allocation
- **Independent allocation per account type**: Tax-Deferred, Roth, and Taxable accounts can each have different stock/bond/cash allocations
- Example: Conservative 60/40 for Tax-Deferred, aggressive 90/10 for Taxable brokerage account
- Returns derived from historical means (~10.5% stocks, ~5.2% bonds, ~3.5% cash)
- Auto-rebalancing maintains target allocation each year
- Investment Return slider now acts as override (if set, applies flat rate to all accounts)

#### Enhanced Monte Carlo
- Monte Carlo simulation now uses per-account allocation
- Generates separate stock/bond/cash return sequences, then blends per account
- More realistic modeling of diversified portfolios with different risk profiles

### UI Improvements
- New "Asset Allocation by Account" section replaces single global allocation
- Each account shows Stocks/Cash inputs with calculated Bonds display
- Bond percentage updates dynamically as you adjust

---

## v1.2.0 - Enhanced Backtesting & Asset Allocation

### New Features

#### Configurable Asset Allocation
- **Stock/Bond/Cash allocation**: Set your own portfolio mix instead of fixed 60/40
- Stocks use S&P 500 historical returns
- Bonds use 10-year Treasury yields
- Cash uses 3-month T-bill rates
- Bond percentage computed automatically (100% - stocks - cash)

#### Historical T-Bill Returns
- Added 97 years of cash/money market returns (1928-2024)
- Source: NYU Stern/Damodaran historical returns database
- Enables accurate modeling of conservative portfolios

#### Inflation-Adjusted Results
- Backtesting now shows both nominal and real (inflation-adjusted) balances
- Real balance represents purchasing power in start-year dollars
- Cumulative inflation tracked throughout each simulation

#### Monte Carlo Asset Allocation
- Monte Carlo simulation now uses your stock/bond/cash allocation
- Separate return generation for each asset class with realistic volatility
- Stocks: ~11.7% mean, 19% standard deviation
- Bonds: ~5% mean, 8% standard deviation
- Cash: ~3.3% mean (low volatility)
- Flight-to-safety behavior: bonds rally during stock crashes

### UI Improvements
- New Asset Allocation section in Rate Assumptions card
- "Final (Real)" column in historical backtest results table
- Explanatory notes about inflation-adjusted values

---

## v1.1.0 - Multi-Account Tax Support

### New Features

#### 3-Bucket Portfolio Model
- **Roth Account Support**: Portfolio now tracks Tax-Deferred, Roth, and Taxable accounts separately
- Configurable allocation percentages for each account type
- Tax-efficient withdrawal ordering: RMD → Taxable → Roth → Tax-Deferred

#### Tax Bracket Modeling
- Full 2024 federal tax brackets for all filing statuses (Single, Married Filing Jointly, etc.)
- Inflation-adjusted brackets for multi-year projections
- State income tax rate configuration
- Marginal and effective tax rate calculations

#### Roth Conversion Strategy
- Model annual Roth conversions with configurable amounts
- Set start/end years for conversion window
- Conversions automatically move funds from Tax-Deferred to Roth bucket
- Track tax impact of conversions

#### Big Ticket Items
- Add one-time financial events (inheritance, home sale, large purchase)
- Configure as income or expense with year of occurrence
- Tax treatment options: None, Ordinary Income, Capital Gains
- Integrated into both standard projection and Monte Carlo simulation

#### Historical Backtesting
- Test your retirement plan against 97 years of actual market history (1928-2024)
- Uses real S&P 500 returns, bond yields, and inflation rates
- Identifies worst starting years (1929, 1966, 1973, 2000, 2008)
- Compare historical success rate vs Monte Carlo simulation
- Expandable table showing all historical sequences

### Technical Details
- New `tax.go` with TaxCalculator for federal/state tax calculations
- New `historical_data.go` with embedded market data
- New `backtest.go` for historical sequence testing
- Added comprehensive test coverage for tax and backtesting

---

## v1.0.0 - Initial Release

SimpleBudget is a local-first personal finance dashboard and retirement planning tool. All data stays on your computer - no cloud, no accounts, complete privacy.

### Features

#### Dashboard
- Real-time KPI tracking: income, expenses, net savings, savings rate with sparklines
- Interactive charts: monthly income vs expenses, spending by category, spending trend, top spending, cumulative balance
- Category breakdown with drill-down analysis
- Month-over-month spending trend with hover details (current/prior amounts, percentage change)
- CSV export of financial metrics

#### Data Explorer
- Search and filter transactions by date, category, amount, description
- Multi-file CSV import with automatic deduplication
- Flexible parsing handles most bank export formats
- File management with toggle inclusion and deletion

#### What-If Retirement Planner
- 30-year portfolio projections with Monte Carlo simulation (1000 runs)
- Sustainability scoring (0-100) with success probability
- Multiple income sources with COLA adjustments
- Multiple expense categories with inflation tracking
- Healthcare cost modeling for multiple household members
- RMD (Required Minimum Distribution) calculations
- Sequence risk and failure point analysis
- Go-Go/Slow-Go/No-Go spending phase modeling
- Auto-sync income patterns from transaction data

#### Insights
- Automatic recurring payment detection (subscriptions, bills)
- Income pattern analysis (weekly, biweekly, monthly)
- Category spending trends
- Spending velocity tracking

#### Security & Encryption
- **Password** - Simple scrypt-based encryption
- **Age Identity** - X25519 key pair for advanced users
- **SSH Key** - Use existing ed25519 or RSA keys
- **YubiKey** - Hardware security key support

All transaction files and settings are encrypted at rest.

#### Backup & Restore
- Create downloadable ZIP backups
- Restore from previous backups
- Test data for learning the interface

### Technical Details

- Single binary with embedded web assets
- No external database required (CSV/JSON file storage)
- Built with Go, HTMX, Tailwind CSS, Plotly.js
- Cross-platform: Linux, macOS (Intel & Apple Silicon), Windows

### Supported Platforms

| Platform | Architecture | Download |
|----------|--------------|----------|
| Linux | x64 | `budget2_1.0.0_linux_amd64.tar.gz` |
| Linux | ARM64 | `budget2_1.0.0_linux_arm64.tar.gz` |
| macOS | Intel | `budget2_1.0.0_darwin_amd64.tar.gz` |
| macOS | Apple Silicon | `budget2_1.0.0_darwin_arm64.tar.gz` |
| Windows | x64 | `budget2_1.0.0_windows_amd64.zip` |

### Getting Started

1. Download the appropriate archive for your platform
2. Extract and run `budget2` (or `budget2.exe` on Windows)
3. Open http://localhost:8080 in your browser
4. Import your bank's CSV transaction export

### Privacy

- 100% local - no data leaves your computer
- No telemetry, analytics, or external connections
- Standard file formats (CSV/JSON) - no vendor lock-in
