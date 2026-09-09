# Guardrail optimizer — implementation contract

Status: Verified. All four tasks accepted by the mechanical gate; live app not deployed.
Run prefix: GO. Application repository: `/home/darrell/bin/ai/budget2`.
Keep this run separate from the existing care-cost run and ledger.

## Agreed product behavior

Optimize for more lifetime spending and steadier spending subject to a
user-selected risk limit. Minimum monthly living spending defaults to $7,500
in today's dollars ($90,000/year). The visible control is Chance of maintaining my minimum spending, with 90%, 95%, 99%, and Custom choices. Require an explicit target selection; do not infer the user's preference from the presets. Internally maximum floor-shortfall risk equals 100 minus the selected success target.

The floor applies to the Monthly Living Expenses category. Healthcare,
taxes, and other separately modeled expenses remain additional obligations.
Explain this next to the floor input. Inflate the floor using the same
cumulative CPI factor used to express simulation spending in today's dollars;
spending decline and retirement phase reductions must not lower this floor.

Risk means the estimated fraction of simulated paths where the selected
policy cannot fund the floor in at least one month of the simulated horizon.
Do not substitute depletion probability or months below the original plan.
Display simulation count, horizon assumptions, shortfall path count, and an
uncertainty interval. Describe results as simulated estimates, not guarantees.

## Search and comparison

Use a bounded, reproducible search over cut/raise trigger thresholds,
cut/raise sizes, and maximum spending multiplier. Retain the user's starting
living expenses; this first version optimizes subsequent guardrail changes.
The real-dollar floor overrides reductions below it. A starting living
expense below the floor is a validation error, not a silent budget increase.

Return non-dominated choices on lifetime real living spending, spending-cut
severity/frequency, and estimated floor shortfall risk. Show up to three
distinct alternatives: higher spending, smoother spending, lower risk.
Candidates must meet the selected risk limit on an independent validation
sample. If none qualify, show that outcome and the measured results without
relaxing the floor or risk limit. No weighted score hides the tradeoffs.

Compare current guardrails and disabled guardrails on identical scenario
draws. Clearly identify any existing adaptive-spending behavior in the
disabled-guardrails comparison; do not call it fixed spending if it cuts
discretionary expenses. Use independent, reproducible search and validation
seeds. Revalidate the final shortlist on held-out scenarios and label it as
the best choices found in the searched settings, not a global optimum.

Report median lifetime funded real living spending, the lower-tail spending
outcome, worst annual real spending cut, cut frequency, floor shortfall risk,
depletion risk, and median real ending balance. Never count unfunded spending
requests as enjoyed spending. Policies must use only information available
at the current simulation month, without foresight of future returns.

## Architecture and user flow

Reuse `engine.ProjectionState.StepMonth` and the Monte Carlo scenario model.
Keep optimizer scoring/search in `internal/services/retirement/analysis`.
Use shared model types for floor, risk inputs, and result metrics. Centralize
floor enforcement and funded-spending measurement; enumerate canonical
projection, Monte Carlo, backtest, scenario-chain, and UI consumers before
implementation. Check symbol callers before edits as required by app AGENTS.

Add an Optimize guardrails panel to the existing what-if guardrail UI.
Inputs: minimum monthly living spending in today's dollars and chance of maintaining my minimum spending. Run explicitly; show progress/cancellation, inline
validation, and result comparisons. Never run a costly search on page load.
Results are previews. Applying a selected policy is a separate explicit user
action, persists its settings, and refreshes the ordinary projection.
Changed inputs invalidate old results; stale results cannot be applied.

Follow the app's existing typography, palette, currency formatter, templates,
and HTMX conventions. Copy is direct and descriptive. No new page or chart
is required; a comparison table is the primary result surface.

## Tasks and acceptance criteria

| ID | Tier | Checks | Scope and acceptance |
|---|---|---|---|
| GO1 | 3 | tests,second | Shared floor and outcome model. Real floor remains $7,500 across CPI and phase changes; healthcare/tax obligations remain included; shortfall classification distinguishes funded floor from discretionary shortfall and portfolio depletion. Existing consumers agree. Old configurations remain compatible. Oracle exercises zero assets with sufficient income, temporary illiquidity, fractional cents, zero inflation, variable inflation, phase transitions, and depletion. |
| GO2 | 3 | tests,second | Search and validation. Identical scenarios per candidate; independent validation draws; deterministic ordering; metrics use funded real spending; selected risk boundary is inclusive on unrounded counts; infeasible search returns no qualifying result; cancellation and bounded work verified; baseline and disabled guardrails included. |
| GO3 | 2 | tests,a11y,second | Inputs, results, and Apply flow. Floor defaults to 7500; presets/custom success target validated server-side (finite, greater than 0 and less than 100); success target must be selected. Displayed figures and qualification agree near rounding boundaries. No settings change on search, cancellation, or error. Explicit Apply round-trips configuration, invalidates stale results, and updates projection. All accessibility points pass. |

GO1 precedes GO2; GO3 integrates their stable interfaces. Shared financial
engine changes are Tier 3; UI is reversible Tier 2 with financial verification.
Before dispatch, author executable GO1/GO2 acceptance oracles, calibrate each
at both ends, and record exact file ownership and caller inventory. Workers
write manifests; independent checkers write verdicts. Acceptance requires the
mechanical gate, escalation scan after every verdict, and final gate done.
Run the gate smoketests and full app build/test/vet/staticcheck before commit.
Report gate statistics and record every catch by its detecting mechanism.

## Rulings and design findings

- Lead inspection: existing MonteCarloGuardrailImpact tracks funding-gap
  months and reductions relative to plan, not the requested real-dollar floor.
  These metrics cannot be relabeled as floor shortfall probability.
- Lead inspection: existing Monte Carlo can apply adaptive discretionary
  spending when guardrails are disabled. Baseline labeling must disclose it.

## Before implementation

The user approved implementation by saying continue after reviewing the design. Preserve the clean application checkout; use an
isolated implementation checkout and resolve its permitted write location.
The executable oracle must resolve exact funded-spending accounting through
the existing tax-aware monthly engine before any worker edits production code.

## Architecture review refinements

Funded living is max(0, guardrail-adjusted requested living minus total monthly shortfall), conservatively prioritizing other obligations. Disclose this assumption. MonthOutcome.LivingExpenses is pre-guardrail planned spending and cannot be scored directly. Deflate funded spending with cumulative CPI. Optimizer observations continue through the full horizon after legacy depletion; actual income-funded spending still counts. Retain existing longevity variation and display the horizon range. Measure annual real spending cuts even after raises above the original budget. Reject chained scenarios explicitly in this first optimizer version because later settings can override candidates. Rank higher-spending by median lifetime funded real spending descending; smoother by 95th-percentile worst annual percentage cut ascending then mean cut count ascending; lower-risk by observed shortfall rate ascending. Remaining ties use median spending descending then stable candidate order. Deduplicate category winners. These refinements were caught by independent explorer review before implementation.

## Latest user-approved wording

Minimum monthly spending defaults to $7,500, is editable, is entered in today's dollars, and increases with inflation. Chance of keeping that minimum throughout retirement offers 90%, 95%, 99%, and Custom. Example helper: At 95%, the target is for at least 95 out of 100 simulated futures to fund your minimum every month. This is a simulation target, not a guarantee. The existing Monte Carlo avoids depletion result remains separately labeled and must not be substituted for floor success. The user's screenshot showed 86.1% avoids depletion; this is an observed result, not an optimizer default or a floor-success estimate. No target has yet been selected.

## Approved implementation rulings (2026-09-08)

User said continue: implementation authorized. GO-R1: override earlier termination convention. When observation floor is positive, MC continues full horizon through legacy depletion, preserving first depletion metadata. Actual income with zero assets can fund floor. No-observer runs keep old termination. GO-R2: funded=max(0,adjusted-max(0,shortfall)); compare real amounts rounded half-away to cents in shared helper. Annual cuts use consecutive complete 12-month funded real totals. GO-R3: models.GuardrailConfig.MinMonthlySpendingReal; engine.MonthOutcome and models.ProjectionMonth AdjustedLivingExpenses/FundedLivingExpenses; analysis.MonteCarloConfig.MinMonthlySpendingReal observation-only; models.MonteCarloResult.FloorOutcome pointer with FloorFailed, TotalFundedLivingReal, WorstAnnualCutPct, AnnualCutCount, MonthsObserved, FinalBalanceReal. Expose effective multipliers and floor-adjusted event dollars. GO-R4: search 32 combinations drop10/20 cut5/10 rise15/25 raise5/10 cap120/150 percent; legacy min percent0 replaced by real floor, plus current configuration with floor, deduplicated. Search64 shared runs; shortlist up to6 Pareto candidates; validate each and both baselines on1000 fresh shared runs. Resolve seed once; distinct validation seed. Bounded concurrency and cancellation between runs. Inclusive count-based qualification; Wilson95 interval. GO-R5: isolated clone /tmp/budget2-guardrail-optimizer excludes foreign Makefile and web/static/vendor/htmx.min.js edits. No deployment during build.

GO-R6: GO3 UI starts in parallel once GO2 types are pinned in PLAN; acceptance and integration testing still wait for GO1/GO2. Its owned handler/template paths do not overlap engine/analysis work.

GO-R7: Monetary floor classification and cumulative funded-real totals use shared half-away monthly cent rounding. Do not emit a cut/raise event if both before and after are identical after floor clamping. Oracle fixture must distinguish policy evaluation from an actual spending change.

GO-R8: Verified isolated clone baseline is a71e8b9 (latest source commit at clone time), not the older running binary176383f8. Full baseline go test ./... passes. Gate run directory is isolated .swarm under implementation clone; use SWARM_DIR explicitly with orchestration gate.

GO-R9 (oracle calibration): GO1 baseline compiled and failed missing capability; disposable prototype passed ORACLE PASS before worker dispatch. Calibration corrected a fixture demanding no-op events and added actual locked-assets coverage. Prototype discarded; .swarm/tier3/GO1/calibration.md records scope.

GO-R10: GO2 oracle calibrated baseline capability fail/reference pass/seed reuse mutant fail before implementation. Tool agent limit requires reusing oracle author for GO2 implementation; immutable oracle and independent primary/second verification remain mandatory. Independent files may build concurrently; GO2 gate waits GO1.

GO-R11 (worker regression catch, GO2 attempt1): held-out validation can change Pareto dominance after search. Added regression TestGuardrailOptimizerRecommendationsStayParetoAfterValidation and re-filter qualifying validation candidates before category winners. Immutable oracle still passes; no failed checker attempt.

GO4 (Tier2, tests+second): promote independent GO1 second-checker recovery probe to permanent regression. Scope analysis/floor_outcomes_test.go only. Fixture first depletion at month1/12, later income month12, positive recovered final balance; full36observed months and FIRST depletion metadata remain unchanged. No production changes. This covers a distinct gap beyond oracle firstmonth-zero-assets cases.

GO-R12 (second checker catch, GO2attempt1 FAIL CONCEDED): target64.4 multiplied by1000 rounds upward and rejects644/1000 successes. Extended immutable oracle with all999tenths thresholds +fullservice64.4; baseline fails right comparison; disposable direct count-derived percentage prototype passes before attempt2 dispatch. Use direct unrounded count-derived rate comparison, never broad epsilon/display rounding.
GO-R13 (user clarification): GO3attempt2 makes Chance of maintaining my minimum spending primary; other outcomes/config/confidence/technical assumptions go under supporting disclosures. This is new user scope, not a failed verification. Worker runtime selfchecks caught/fixed post-Apply focus and seed overflow before independent verification.

GO-R14: GO4 uses lean lead-direct exception for small verbatim checker-probe promotion (agent slot limit); no production logic changes. Adds both nonzero-first-depletion recovery and actual income/unfunded/concurrency runner proof to one test file. Independent tests+second checkers still mandatory.

GO-R15 (a11y checker catch, GO3attempt2FAIL CONCEDED): noqualifying outcome plainparagraph not announced, generic Searchcomplete status violates point20. Attempt3 adds outcome-specific accessible announcement and focusedheading. Primarychecker independently noted broad defaultvalue test could matchordinaryfloorfield; strengthen optimizer-ID-specific assertion. Existing disabled spendingphasecard contrast observed on fullsitebaseline remainsoutsideGO3.

## Final verification record

All tasks accepted, evidence verified, no unresolved flags. Final evidence is isolated in .swarm-go (set SWARM_DIR=.swarm-go with orchestration gate). First-attempt clean:2/4; second checker caught decimal qualification and a11y checker caught no-result announcement; both fixed and reverified. Full make check, go build ./..., and orchestration gate smoketests passed. GO3 scoped accessibility passed; preexisting disabled spending-phase contrast issues remain documented in its verdict. Preview uses synthetic data only.


## GO5 — Select an optimizer option and view its graph (Tier 2)
User request: I should be able to select one of them and see the graph results.
Scope: add a read-only graph preview to every returned candidate, including below-target options and both baselines. Keep saved settings and Apply eligibility unchanged.
Architecture: retain server-side candidate tokens separately from Apply authorization; validate request/manager/scenario/revision/fingerprint/expiry for graph access. Clone current validated settings, set the exact retained candidate config (including nil baseline), run the canonical engine with DefaultHooks and existing buildEngineInput, and reuse buildProjectionChartData. No simulation/search/financial formula changes.
UI: each row has View graph. Show an inline portfolio projection near the comparison, clearly labeled selected policy, target status, and Preview — not saved. State this is the base projection, not a Monte Carlo success graph. Include today-dollar/nominal choice, Close preview, keyboard focus/status feedback, and an accessible numeric alternative. Preserve comparison results for selecting another option; input changes, cancellation and new search clear graph. Rapid selections must not paint stale responses. Errors announced without losing comparison rows.
Acceptance: all returned rows can preview exact candidate settings; forged/expired/cancelled/stale/wrong-request tokens rejected; below-target preview cannot enable Apply; graph request never saves or changes revision; canonical output matches independent expected engine chart for selected config and dollar mode; browser switching updates graph and label together, handles errors/closing/invalidation; existing optimizer tests pass; accessible in both themes/narrow viewport.
Verification: tests, second, a11y; manifest and verdicts in .swarm-go; gate check GO5 before acceptance. User request authorizes this bounded preview addition; no change to optimizer search or saved risk target.

## GO6 — Retain real-search token regression (Tier 2)
Test-only promotion of primary checker probe: every row returned by the actual optimizer has a distinct graph token retaining exactly that candidate, and graph tokens differ from Apply tokens. Extend existing real-service search test. No production edits. Checks: tests,second.
GO6 also retains the second-checker browser probe in .swarm-go/probes/guardrail-graph-browser.cjs, with explicit synthetic GUARDRAIL_TEST_URL and optional runtime environment paths. It checks every graph token rejects Apply, mode switching, reordered Plotly completion, close while loading, error retention and input invalidation.

## GO7 — Simulated annual spending and portfolio bands (Tier 3)
Territory: models whatif.go/guardrail_optimizer.go/new guardrail_simulation.go; analysis monte_carlo.go/floor_outcomes.go/guardrail_optimizer.go/new guardrail_simulation.go and relevant tests.
Add opt-in MonteCarloConfig.CaptureSpendingYears bool. Only positive-floor observation plus opt-in captures years; old metrics/math/RNG and legacy depletion behavior unchanged. Add MonteCarloFloorOutcome.Years []GuardrailPathYear (json years,omitempty). GuardrailPathYear: Year int; LivingReal,LivingNominal,PortfolioReal,PortfolioNominal float64, snake_case JSON. Complete projection years only, 1-based; living is mean of12 actual funded monthly living values in that basis, real each month uses existing RoundLivingCents(funded/CPI). Portfolio is year-end max(0,balance), real divides by actual year-end CPI. Include full-horizon outcomes after depletion, including zero paths. Never impute after each path horizon. Existing floor/cut classification unchanged.
Models GuardrailPercentiles {P10,P50,P90 float64}; GuardrailSimulationYear {Year,Paths int; LivingReal,LivingNominal,PortfolioReal,PortfolioNominal GuardrailPercentiles}; snake_case JSON. Add candidate SimulationYears []GuardrailSimulationYear json simulation_years,omitempty.
Expose analysis.SummarizeGuardrailSimulationYears(rows []models.MonteCarloResult) ([]models.GuardrailSimulationYear,error): group complete year; sort each measure independently; interpolate q*(n-1); Paths counts contributors incl zeros. Exclude ended paths, no padding/carryforward. Reject nonfinite/invalid/inconsistent annual inputs. All nil Years returns nil for custom-runner compatibility; mixed capture absence rejected.
Real optimizer runner enables capture. FINAL candidate SimulationYears derive from SAME held-out validation rows/seed as Metrics, including both baselines. No search/classification change, no raw paths retained in response, no graph reruns. Custom runners returning no years continue to work.
Acceptance oracle: explicit percentile fixtures with variable horizons and zero depleted paths, invalid values; seeded capture on/off metric equality; capture reconciles floor total/final balance; real optimizer all candidates use held-out years and reproducible seed. Every existing consumer covered by relevant full analysis/engine/model regressions. Oracle .swarm-go/tier3/GO7/accept.sh must be calibrated both ends before implementation; tests+second after oracle.
User authorized simulation ranges by continue after agreed direction.

## GO8 — Simulation preview charts (Tier 2)
Territory: whatif graph/optimizer handlers/tests, optimizer template, optional JS/CSS only. Depends GO7 contract. Existing retained candidate tokens carry SimulationYears.
View graph defaults Simulated ranges: separate living spending and portfolio charts, P50 median and P10–P90 shaded range from candidate.simulation_years exactly. X projection year; living average funded monthly spending in that year, portfolio year-end. Real default/nominal updates all charts/labels/tables atomically. Real spending chart reference selected minimum; omit nominal reference rather than invent deterministic CPI. Keep base-case separately selectable/labeled.
Retained success percentage/classification/seed/runs unchanged. Explain middle80% pointwise range is not guaranteed bounds or one future. Accessible tables include Paths each year; explain longevity reduces later counts. All candidates including belowtarget/baselines inspectable; no Apply permission or settings change. Stale/expired/cancelled protection retained; async race/close/error behavior preserved; no bands shows truthful unavailable state not fake simulated graph. Zero bands valid. Both themes, keyboard, reflow, table values equal graph. Tests include stochastic differences despite same base-case, source fidelity, all modes, sample counts, race/invalidation. Checks tests,second,a11y.

### GO8 attempt2 ruling — CONCEDE accessibility finding
Mechanism: checker-a11y, prompted by lead code review. Theme changes refetched an expired result and retained light colors on a dark background, violating standard7/12. Fix uses cached data to retheme locally without request or focus/scroll change, including when graph endpoint is unavailable. Also fix fixed-width SVG clipping on viewport resize. Preserve all GO8 behaviors; add permanent browser regressions for expired/error theme toggle and resize. Reverify tests,second,a11y; GO7 code/oracle unchanged. Worker visual self-review also corrected short-horizon ticks and legend spacing before attempt1.

## GO9 — Retain every-candidate validation replay (Tier 2)
Test-only promotion of second checker probe. Independently rerun every final policy including baselines on retained validation seed/runs; aggregate bands AND metrics must equal retained candidate, and fixture has different stochastic bands. No production changes or user figures changed. Lead direct small test task; checks tests (primary did not author probe). UI extra probes promoted in GO8attempt2 harness.

## GO10 — Retain reviewed HTMX maintenance update (Tier 2)
User approved continuing with keeping/committing Makefile and vendored HTMX. Only production changes: pin official HTMX2.0.10 instead of2.0.4, download from jsDelivr with curl-f to fail HTTP errors. Bundled file must match official release bytes and version. No custom library edits, financial logic, or templates. Verify full app check/build, actual browser runs2.0.10 and HTMX swaps/forms/navigation/cancellation work on isolated synthetic data; accessibility regression audit observes unchanged legacy issues. Checks tests,a11y (dependency mechanics; no money/classification logic changes). No new code tests mirroring a version string necessary; direct integrity and real interaction evidence required. Preserve existing latest master changes; commit these two reviewed files plus run evidence only.
