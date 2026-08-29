# T12 — Fast-first what-if results partial (async expensive analyses)

Tier 2. Checks: `tests` (checker-tests, FAMILY anthropic) + `second`
(checker-second, FAMILY adversarial). Worker: worker-coder, attempt 1.

## Problem

Every settings change on /whatif recomputes the full `retirement.RunFull`
fan-out synchronously (~7s): Monte Carlo (1000 runs), historical backtest,
sensitivity, failure points, SS comparison grid. The deterministic projection
plus the cheap post-projection analyses are milliseconds. The analysis cache
(`internal/handlers/whatif/handlers.go`, keyed by settings dep-hash, 5-min
TTL) misses on every edit, so interactive editing waits ~7s per change.
Measured: GET /whatif 5ms (cache warm), POST /whatif/calculate 6.9s,
POST /whatif/montecarlo 6.7s.

## Chosen approach (decided by lead; do not redesign)

**Fast-first render.** On a cache miss, mutating handlers render the results
partial immediately from a new cheap `retirement.RunFast` (deterministic
projection + explainability + budget fit + present value + sustainability +
RMD + tax; expensive fields nil). Expensive sections render skeleton
placeholder cards. The partial embeds a one-shot htmx loader that GETs a new
`/whatif/results-full?hash=<depHash>` endpoint, which computes the full
analysis through the existing singleflight+cache path (~7s), then swaps the
full partial in. Hash guards (checked before AND after the compute) make a
stale follow-up answer 204 so it can never clobber a newer mutation's render.
Chart JSON endpoints serve the projection from the fast path on cache miss so
charts update with the numbers instead of lagging 7s.

Explicitly rejected: serving the previous (stale) analysis with a
"recomputing" badge — stale dollar figures presented against new settings
conflicts with this project's accuracy-first rule, and it does nothing for
cold loads. Also rejected: cache-key loosening — the expensive fan-out itself
is what blocks the partial.

Unchanged behavior (do NOT touch): `POST /whatif/montecarlo` (fresh re-roll
stays blocking; it has its own spinner), `POST /whatif/tax-optimize`, all
`mcpsvc/plan` callers of `retirement.RunFull`, the 5-minute cache TTL, the
revision poll protocol (204 / HX-Trigger).

## Hard environment rules

- Work ONLY in this worktree:
  `/home/darrell/bin/ai/budget2/.claude/worktrees/fervent-nash-783b77`.
  cd there first; never touch the main checkout.
- Do NOT start the app or anything on port :8080 (user's live server).
  Verification is `go build ./... && go vet ./... && go test ./... &&
  staticcheck ./...` plus `gofmt -l` on touched files. Never pipe test output
  through grep/head without `set -o pipefail`.
- Before editing any function, run LSP `incomingCalls`/`findReferences` on it
  and account for every caller (there are callers in `mcpsvc/plan` and
  multiple handler files — do not miss `handlers_live.go`,
  `handlers_rates.go`, and every test file). Never rename by find-and-replace.
- Do not modify anything under `internal/services/retirement/engine/**`
  (critical glob — would escalate the task tier).
- Workers never commit. When done, write the manifest
  `.swarm/manifests/T12.1.files` (repo-relative paths of every touched file,
  one per line, tests included).

## Implementation spec

### 1. `internal/services/retirement/orchestrator.go` — add RunFast

Extract the hooks auto-fill and the cheap block so RunFast and RunFull share
one code path (no duplicated math):

```go
// fillDefaultHooks returns in with DefaultHooks auto-filled when the caller
// passed zero-valued hooks — RunFull's historical convention, shared by
// RunFast so both entry points resolve hooks identically.
func fillDefaultHooks(in engine.Input) engine.Input {
	if in.Hooks.SocialSecurityProjectionActive == nil &&
		in.Hooks.ProjectedSocialSecurityIncome == nil &&
		in.Hooks.ResolveChainTransition == nil {
		in.Hooks = DefaultHooks()
	}
	return in
}

// RunFast executes only the deterministic projection and the cheap
// post-projection analyses. The expensive fields — Sensitivity,
// FailurePoints, MonteCarlo, HistoricalBacktest, SocialSecurity — stay nil.
// The what-if handlers use it to render the results partial immediately on a
// cache miss while the full analysis loads asynchronously behind it.
func RunFast(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
	in = fillDefaultHooks(in)
	return fastAnalysis(in, eng.Run(in))
}

// fastAnalysis assembles the cheap analyses derived from the single baseline
// projection.
func fastAnalysis(in engine.Input, proj <baseline projection type>) *models.WhatIfAnalysis {
	budgetFit := analysis.BudgetFit(in, proj)
	return &models.WhatIfAnalysis{
		Settings:                 in.Prepared.Settings(),
		Projection:               proj,
		ProjectionExplainability: analysis.BuildExplainability(proj, in),
		BudgetFit:                budgetFit,
		PresentValue:             analysis.PresentValue(in, proj),
		Sustainability:           analysis.Score(budgetFit.RequiredRate, proj.Survives),
		RMD:                      analysis.BuildRMD(proj, in),
		Tax:                      analysis.BuildTax(proj, in),
	}
}
```

(Resolve `proj`'s exact type via LSP hover on `eng.Run`.)

Refactor `runFullWithSeed` to: `in = fillDefaultHooks(in)`, run the
projection, `a := fastAnalysis(in, proj)`, then keep the existing expensive
fan-out EXACTLY as it is today (same branches, same `ParallelIndexed` call,
same SS-portfolio join, same backtest/MC join), reading `settings :=
a.Settings` and `budgetFit := a.BudgetFit`, and assigning the results into
`a`'s fields before returning `a`. This is a pure refactor of RunFull —
byte-identical outputs. Note: `in.Prepared.Settings()` is called once inside
`fastAnalysis`; today's code also calls it once. Keep it once.

### 2. `internal/handlers/whatif/handlers.go` — fast-or-cached path

a. Test seam next to `runFullFn`:
```go
// runFastFn indirects retirement.RunFast so tests can count or stub the
// fast-path analysis. Production never reassigns it.
var runFastFn = retirement.RunFast
```

b. `runFastRecovered(in engine.Input)` — mirror of `runFullRecovered`
   (same recover/log/errAnalysisPanicked contract) calling
   `runFastFn(getEngine(), in)`.

c. `analysisFastOrCached`:
```go
// analysisFastOrCached returns the cached full analysis when fresh, else a
// RunFast analysis plus the dep-hash the client needs to fetch the full
// analysis asynchronously. pendingHash == "" means the analysis is full.
func analysisFastOrCached(settings *models.WhatIfSettings) (*models.WhatIfAnalysis, string, error) {
	in, depHash, err := buildEngineInput(settings)
	if err != nil {
		return nil, "", err
	}
	if cached, ok := cachedAnalysis(depHash); ok {
		return cached, "", nil
	}
	a, err := runFastRecovered(in)
	if err != nil {
		return nil, "", err
	}
	return a, depHash, nil
}
```

d. `renderRecalc` switches from `runAnalysisWithCache` to
   `analysisFastOrCached`; keeps the HX-Trigger revision logic verbatim; the
   render tail passes the pendingHash through (see f). `runAnalysisWithCache`
   itself stays exactly as-is (it is now the engine of `/whatif/results-full`
   and remains correct for any residual callers).

e. New endpoint + route `r.Get("/whatif/results-full", handleWhatIfResultsFull)`:
```go
// handleWhatIfResultsFull serves the full (expensive) analysis for the async
// loader a pending results render embeds. The hash parameter is the dep-hash
// that pending render was built from; a mismatch with the CURRENT settings
// hash means a newer mutation owns the results panel, so answer 204 (htmx: no
// swap) instead of clobbering it with figures for superseded settings. The
// check runs again after the multi-second compute because settings can change
// while the flight runs; the late 204 keeps the newer render on screen.
func handleWhatIfResultsFull(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.LoadContext(r.Context())
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_, depHash, err := buildEngineInput(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("hash") != depHash {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	analysis, err := runAnalysisWithCache(r.Context(), settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if cur, curErr := retirementMgr.Load(); curErr == nil {
		if _, curHash, hashErr := buildEngineInput(cur); hashErr == nil && curHash != depHash {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	renderWhatIfResultsOnly(w, settings, analysis)
}
```
No OOB swaps and no HX-Trigger here: the follow-up must never rewrite the
left column or advance the revision baseline.

f. Pending flag plumbing. `renderWhatIfResults` and `renderWhatIfResultsOnly`
   gain a `pendingHash string` parameter (update EVERY caller — enumerate via
   LSP findReferences; callers passing a full analysis pass `""`).
   `renderResultsTemplate` likewise, and after `buildResultsPartialData` it
   sets:
```go
partialData["AnalysisPending"] = pendingHash != ""
partialData["AsyncHash"] = pendingHash
```
   (Set in `renderResultsTemplate`, not `buildResultsPartialData`, so the
   direct test callers of `buildResultsPartialData` keep today's shape; a
   missing `AnalysisPending` key is falsy in templates — verify with the
   existing tabs render test.)

g. `handleWhatIf` (GET page): switch to `analysisFastOrCached`; add the same
   two keys to `pageData` (`"AnalysisPending": pendingHash != ""`,
   `"AsyncHash": pendingHash`). Cold page load then paints in ms with
   skeletons and self-fills.

h. Chart endpoints `handleWhatIfProjectionChart` and
   `handleWhatIfIncomeChart`: replace `runAnalysisWithCache` with
   `analysisFastOrCached`, using only `analysis.Projection` (they must not
   block ~7s behind the flight, and must NOT start one).
   `handleWhatIfProjectionChartNoGuardrails` already does a bare run — leave
   it alone.

### 3. `internal/handlers/whatif/handlers_live.go` — poll

`handleWhatIfPoll`: replace `runAnalysisWithCache` with
`analysisFastOrCached`; keep the HX-Trigger revision header verbatim; render
via `renderWhatIfResultsOnly(w, settings, analysis, pendingHash)`. A pending
poll render carries the loader div, which fetches the full partial exactly
like a mutation's render does.

### 4. Templates

`web/templates/pages/whatif.html`, inside `{{define "whatif-results"}}`:

- At the top:
```html
{{if .AnalysisPending}}
<div id="whatif-async-loader"
     hx-get="/whatif/results-full?hash={{.AsyncHash}}"
     hx-trigger="load"
     hx-target="#whatif-results"
     hx-swap="innerHTML"></div>
{{end}}
```
- Overview panel: wrap failure points:
  `{{if .AnalysisPending}}{{template "whatif-pending-card" "Failure Thresholds"}}{{else}}{{template "whatif-failure-points" .}}{{end}}`
- Risk panel: same wrap for `whatif-sensitivity` ("Sensitivity Analysis"),
  `whatif-monte-carlo` ("Monte Carlo Simulation"),
  `whatif-historical-backtest` ("Historical Backtesting").
  `whatif-guardrail-events` derives from the projection — leave it unwrapped.
- Strategies panel: wrap SS results; skeleton only when the SS card would
  render at all:
```html
{{if .AnalysisPending}}
  {{if and .Settings.SocialSecurity (gt .Settings.SocialSecurity.FRABenefit 0.0)}}
  {{template "whatif-pending-card" "Social Security Claiming Comparison"}}
  {{end}}
{{else}}{{template "whatif-social-security-results" .}}{{end}}
```
  (`whatif-tax-optimizer-results` is driven by its own endpoint — untouched.)
- New define (same file, after `whatif-results-with-oob`), a skeleton card
  taking its title as the dot; reuse the spinner SVG markup from
  `monte-carlo.html`'s `#mc-loading` (without the htmx-indicator class):
```html
{{define "whatif-pending-card"}}
<div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4" data-wf-pending>
    <h3 class="text-lg font-semibold text-gray-800 dark:text-gray-100 mb-4">{{.}}</h3>
    <div class="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-300">
        <svg class="animate-spin h-4 w-4" viewBox="0 0 24 24"><!-- spinner circle+path as in monte-carlo.html --></svg>
        Computing…
    </div>
    <div class="mt-3 space-y-2 animate-pulse" aria-hidden="true">
        <div class="h-3 bg-gray-100 dark:bg-gray-700 rounded w-3/4"></div>
        <div class="h-3 bg-gray-100 dark:bg-gray-700 rounded w-1/2"></div>
    </div>
</div>
{{end}}
```
- `web/templates/components/whatif/verdict-bar.html`: when `.AnalysisPending`,
  append a small neutral-gray chip (e.g. `<span … >updating risk metrics…</span>`)
  so a verdict that later gains the Monte Carlo gate doesn't look like a
  glitch when it shifts. Neutral styling — this is not a cost item.

Why the interplay is safe (context, verify, don't re-derive): the loader div
targets `#whatif-results` and is not `#whatif-poll`, so
`web/static/js/whatif-poll.js` counts its request as a mutation-in-flight and
suppresses the 2s poll while the full analysis computes; `htmx:afterRequest`
clears the flag on every outcome. The hash guards close the
mutation-during-flight race server-side. `whatif-tabs.js` restores the active
tab on `htmx:afterSettle`, so the full swap keeps the user's tab.

### 5. Tests (all in existing files' style; use `setupTestEnvWithRenderer`
where HTML is asserted, reset the package cache the way existing tests do)

`internal/services/retirement` (place next to the existing seeded-run helper —
find it via grep for `runFullWithSeed` in tests):
- `TestRunFastMatchesRunFullCheapFields`: for a representative settings
  fixture, `RunFast` returns nil `Sensitivity`/`FailurePoints`/`MonteCarlo`/
  `HistoricalBacktest`/`SocialSecurity`, and its `Projection`, `BudgetFit`,
  `PresentValue`, `Sustainability`, `RMD`, `Tax`,
  `ProjectionExplainability`, `Settings` are `reflect.DeepEqual` to the same
  fields from a seeded full run. This is the refactor's no-drift oracle.

`internal/handlers/whatif`:
- `TestCalculateCacheMissRendersFastWithLoader` (renderer env, cache reset):
  stub `runFullFn` with a counter; POST /whatif/calculate; assert 200, body
  contains `id="whatif-async-loader"`, `/whatif/results-full?hash=`, and
  `data-wf-pending`; body does NOT contain `Monte Carlo Simulation` heading
  outside the pending card (assert absence of a string unique to the real MC
  card, e.g. `scenarios with year-by-year`); `runFullFn` count == 0.
- `TestCalculateCacheHitRendersFull`: seed the cache directly (compute
  `buildEngineInput` hash, store a full analysis built as
  `retirement.RunFast(...)` + hand-filled minimal `MonteCarlo` with `Stats`,
  `FailurePoints{BaselineSurvives: true}`; nil backtest/SS are template-
  guarded); POST /whatif/calculate; assert no `whatif-async-loader`, real MC
  marker present.
- `TestResultsFullStaleHashNoContent`: GET /whatif/results-full?hash=deadbeef
  → 204, `runFullFn` count 0.
- `TestResultsFullRendersFullPartial`: stub `runFullFn` (fake full analysis, as
  in singleflight tests); GET with the correct current hash → 200; body has
  the MC marker, no `whatif-async-loader`, no `hx-swap-oob`.
- `TestResultsFullDetectsSettingsChangeDuringCompute`: stub `runFullFn` with a
  fn that first mutates settings through the manager (change any numeric
  field) then returns a fake analysis; GET with the pre-change hash → 204.
- `TestChartEndpointsDoNotBlockOnCacheMiss`: cache reset, `runFullFn` counter;
  GET /whatif/chart/projection and /whatif/chart/income → 200 JSON,
  count == 0.
- `TestPollCacheMissRendersPending` (renderer env): revision baseline
  mismatch, empty cache → 200 containing `whatif-async-loader`.

Repoint — do not weaken — the four singleflight tests in
`singleflight_test.go` whose entry was `handleWhatIfCalculate`
(`…RunsFullAnalysisOnce`, `…ErrorResultNotCached`,
`…PanicFailsAllCoalescedRequests`, `…WaiterHonorsContextCancellation`): the
coalescing path is now `handleWhatIfResultsFull`, so drive them through GET
`/whatif/results-full?hash=<current hash>` (compute the hash in the test via
`buildEngineInput` on loaded settings). Every property they guard transfers
unchanged. `TestMonteCarloRerollCoalesces…` is untouched.

Run the FULL package suites. Any other pre-existing test that relied on a
mutating endpoint blocking for the full analysis must be routed through the
new endpoint or a seeded cache — preserving exactly what it guards, never
deleting an assertion. If a change to a pre-existing test's meaning seems
required, STOP and report instead of changing it.

### Acceptance criteria (checkers verify these; the build/vet/test/staticcheck
gate is table stakes)

1. `retirement.RunFull` output is unchanged by the refactor
   (`TestRunFastMatchesRunFullCheapFields` plus the pre-existing retirement
   suite green).
2. On a cache miss, POST /whatif/calculate (and every recalcAndRender
   mutation) returns without invoking `RunFull`, contains the loader div +
   pending skeletons, and still carries the OOB left-column swaps.
3. GET /whatif/results-full with the current hash returns the full partial
   (no OOB, no loader); with a stale hash returns 204 before computing; a
   settings change during the compute yields 204 after it.
4. Chart JSON endpoints return on a cache miss without invoking `RunFull`.
5. Cache-hit behavior is byte-identical to today (full render, no loader).
6. Singleflight coalescing / error-not-cached / panic / ctx-cancel guarantees
   hold on the new blocking endpoint.
7. `gofmt -l` clean on touched files; full `go build ./... && go vet ./... &&
   go test ./... && staticcheck ./...` green; manifest written.
