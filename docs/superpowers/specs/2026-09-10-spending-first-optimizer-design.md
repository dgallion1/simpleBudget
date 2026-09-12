# Spending First Optimizer — Design

**Date:** 2026-09-10
**Status:** Proposed design accompanying the requested implementation plan; no implementation authorized by this document.
**Revised:** 2026-09-10, after review against engine semantics. Three findings folded in: the unpaid-obligation definition (requirement 4), steady-plan qualification and the spending frontier (requirements 13–14), and matched selection/validation sample sizes (requirement 15).
**Video follow-up:** Added an optional timed living-spending boost and a base-case income/withdrawal timeline. The minimum and validation rules remain firm; the existing portfolio-trigger rules are not relabeled Guyton–Klinger.
**User goal:** Maximize spending, understand risk, and consider spending more now in exchange for cuts later, while maintaining basic needs plus some room for fun.

## Product decision

Replace the primary “Optimize guardrails” experience with **“How much can I spend?”** Search the living-expense budget and spending rules together. Explain the additional spending through the possible reduction in future lifestyle.

The primary question becomes:

> How much more could I spend now, what cuts might follow, and would my minimum comfortable lifestyle remain funded?

The existing optimizer changes only guardrail rules. Its “Higher spending” ranking maximizes median lifetime funded living spending. Neither the starting budget nor the experience of the first few years is its primary objective. Renaming that tool alone would not satisfy this goal.

## Proposed defaults and the unresolved meaning of worst case

These defaults make the implementation concrete without asserting that the user has selected them:

- **“Now” means the first five projection years.** Show both starting monthly living budget and median actual funded monthly living over those five years. Rank by the latter so an inflated starting budget followed by immediate cuts cannot win just because of its initial label. The near-term horizon is an advanced control, 1–10 years, and must fit every simulated horizon.
- **The minimum is one monthly living amount in today's dollars**, including basic living costs plus the user's fun allowance. The app does not infer those amounts or classify transactions to invent a minimum.
- **Minimum protection means no month below it in any independently checked future.** This is a strict tested-scenario requirement, not a probability guarantee. The previous question distinguishing tested futures from dependable-income coverage remains unanswered; this plan uses tested futures and explicitly labels that scope.
- **Dependable-income coverage after portfolio exhaustion is a separate question.** A Monte Carlo pass rate does not establish it. A dedicated model would need taxes, benefit timing, inflation, survivor changes, and other obligations. The engine does reveal whether the minimum stayed funded from its first recorded depletion event through the horizon. Report: “The model recorded depletion in X of N checked futures; in Y of those, your minimum stayed funded from that event through the end.” The legacy flag records a non-temporary funding shortfall or a non-positive portfolio balance and stays set afterward. It does not prove a zero balance in every later month or that income alone funded spending. Do not attach those claims without additional cash-flow evidence.

## Requirements shared by every implementation task

1. The minimum includes basic living costs plus a fun allowance, expressed in today's dollars.
2. Healthcare, taxes, property tax, major expenses, and other configured obligations remain additional costs; the living minimum must not double-count them.
3. Never lower the user's minimum, relax the acceptance rule, shorten the modeled horizon, or discard failed futures to produce a recommendation.
4. An accepted candidate has zero minimum-shortfall months and zero unpaid-obligation months across every final validation path. At real-cent precision, unpaid other obligations equal max(0, shortfall - adjusted living), following `engine.FundedLiving`'s obligations-first attribution. A shortfall absorbed by living reduces the adjusted living request; it is a below-plan cut only when funded living is below that month's planned living, and a minimum failure only when below the minimum. Unpaid other obligations imply zero funded living and therefore also a minimum failure for the positive floor. These outcome counts overlap; they are not disjoint categories.
5. Portfolio depletion is reported separately; dependable income may still fund all obligations after depletion.
6. Rank spending by funded living in the first five years by default; requested starting spending and lifetime totals are separate measures.
7. Scheduled spending phases and decline assumptions remain in force. A requested early-spending boost is added separately; its scheduled expiry and existing phase reductions are disclosed separately from below-plan cuts.
8. Show observed failures and sample sizes. Never label simulation results “guaranteed,” “risk-free,” or an exhaustive worst case.
9. Preview, graph inspection, cancellation, and failed requests never change saved settings.
10. Apply saves the selected base living-expense setting, optional timed living-spending boost, and complete guardrail configuration atomically against the preview's scenario and revision.
11. Keep the existing tax, inflation, withdrawal, and guardrail engines authoritative; do not introduce parallel financial formulas. The timed boost extends the shared planned-living schedule before existing guardrails and the floor are applied.
12. Preserve input hooks, reproducible scenario draws, full-horizon observations after depletion, and independent final validation.
13. “Follow planned spending” additionally requires zero below-plan months in every final validation path. “More spending with flexibility” permits below-plan months above the minimum. Depletion by itself disqualifies neither class: if all planned living remains funded, the planned class may still pass. Without the zero-cut rule, the planned class could accept arbitrarily high requests that were never actually funded whenever post-depletion resources cover the minimum.
14. Every budget measured at selection scale is re-measured on the final validation stream and shown as a frontier row, one table per option class, in ascending budget order. The headline options are the highest-ranked qualifying rows. Discovery-only results are never displayed. Every qualifying final frontier row can be inspected and applied; the two headlines highlight choices without restricting the user to them.
15. Selection and final validation each use 1,000 paths to assess the same class-specific acceptance rule. All selection-measured candidates, including selection failures, advance to final measurement so the complete tested frontier remains visible. Selection feasibility guides refinement; final counts determine displayed qualification. A candidate can change status in either direction on fresh draws. Freeze its base budget, boost schedule, and policy before final measurement and never re-tune on final-stage results.
16. The optional boost is a monthly living amount in today's dollars with a fixed calendar stop month. It is cuttable living, included in the objective and subject to the same minimum; it is not an additional protected obligation. Its amount and stop month are fixed across a search, while the actual current-plan baseline retains its saved schedule.
17. Show a separately labeled base-case timeline of Social Security, other configured income including pensions, portfolio withdrawals, taxes, and funded living. Preserve the engine's accounting and observed date range; do not double-count transfers or taxes, stitch simulated percentiles into cash flow, or imply dependable-income coverage from this illustration.
18. Describe the implemented rules as portfolio-trigger spending rules. Full Guyton–Klinger is a separate strategy requiring its own implementation and validation; advertised withdrawal rates, portfolio examples, and generic healthcare estimates are not optimizer defaults or acceptance thresholds.

## Interpreting the evidence

Matching aggregate minimum-failure and depletion counts does not establish that they describe the same paths, months, or cause. Minimum failures may precede depletion or follow planned spending reductions. Use paired path/month observations, including the event month, to report minimum funding after a depletion event. Zero minimum failures with nonzero depletion is evidence about those tested paths, not proof of dependable-income coverage in every future.

For independent simulated paths with underlying model failure probability p, the probability of zero observed failures in N paths is (1-p)^N. Equal sample sizes make the screens comparable; they do not eliminate sampling variation. Do not promise a coin-flip pass rate or only one grid step of budget variation without measured evidence. Replaying the same seed must reproduce results; fresh seeds may change the chosen budget.

## Experience

### Inputs

The initial form starts with **Minimum comfortable monthly living budget**. Help text explains “Include your basic living costs and a fun allowance. Healthcare, taxes, and other separately entered expenses are additional.” Show a saved absolute floor when available; otherwise leave the required input empty. Remove the arbitrary $7,500 default from the new surface.

Use **Compare spending options** as the action. The minimum's funding rule is stated next to it: **“Recommendations must fund this minimum in every checked future.”** Do not lead with a percentage target that implies acceptable failure of basic needs.

Add an optional **Extra spending early** disclosure with **Extra monthly living spending** and **Extra spending stops in** (calendar month). Leave it disabled unless a schedule is already saved. For example, a September 2026 start and September 2036 stop funds the requested boost for 120 months; September 2036 is the first month without it. Show the active dates and amount before comparison. Store that concrete stop month so moving the plan's start forward does not restart another ten years. Never infer a travel allowance from the video's examples.

The boost is added to the existing phase/decline-adjusted base living request, inflated with each path's CPI, and then subjected to the existing spending rules and absolute floor. It is not phase-multiplied or reduced by the base spending-decline assumption a second time. Do not model it as an `ExpenseSource`: those expenses sit outside guardrail cuts, which would give optional travel priority over the living minimum. The boost's expiry lowers the planned schedule; only funding below that new schedule counts as a below-plan cut.

An advanced disclosure contains the near-term scoring horizon and search spending range. Scoring the first five years does not itself schedule a five-year boost. These are search controls, not promises of affordability. Display all living amounts in today's dollars by default.

### Results

Show the actual current plan as a comparison, then up to two useful alternatives:

| Option | Meaning |
|---|---|
| Follow planned spending | Highest-ranked tested budget the plan follows in full in every checked future: no month below the planned schedule, the minimum, or obligations. Existing phases and the scheduled end of extra spending may still reduce spending. |
| More spending with flexibility | Higher near-term funded spending, with below-plan cuts permitted above the minimum. |

Only call the second option “More spending” if its measured near-term spending exceeds the first. If only one option qualifies, show one. If no feasible comparison exists, show the available result without inventing an uplift. If the current plan fails, keep it visible with its observed failure counts.

Below the headline, show the **spending frontier**: one table per option class, one row per final-validated starting budget in ascending order, with the near-term funded median, futures below plan, futures below the minimum, unpaid-obligation futures, portfolio-depletion futures, and (flexible only) typical first-cut year. Mark qualifying rows; the headline options are the highest-ranked qualifying rows. The user's real decision is where on this table to sit, which the two-option headline cannot express on its own. All rows come from the same final validation stream and carry the same sample size. Each qualifying row has an Apply action; failed rows remain inspectable.

For each option show:

- Starting monthly living budget and change from the current plan, including any active boost. Separately show the base schedule, boost amount/stop month, and scheduled living request immediately after expiry; this post-expiry request is not a forecast of actual funding.
- Median monthly funded living over the chosen near-term period, plus a lower outcome (P10).
- Chance of falling below the planned living budget, and chance of doing so during the first five years (or chosen near-term period).
- Among futures with cuts: typical first cut timing, typical and severe cut depth in dollars and percent, and total/longest time below plan. State the denominator and whether a cut remains ongoing at the horizon.
- Lowest observed monthly funded living, alongside the selected minimum; months and paths below that minimum if any.
- Unpaid-obligation paths and portfolio-depletion paths as distinct outcomes, and among depletion paths how many kept the minimum funded from the first recorded event through the horizon, including the event month. No income-only attribution is made.

A plain-language comparison uses real computed values:

> This option funds $X more per month on average in the first five years. Y of N checked futures fall below the planned budget; among those, the median first cut occurs in year Z.

Never combine independently ranked percentiles into an invented single future. “Cut by $X,” “down to $Y,” and “for Z years” require either separate labeled distributions or the same observed path.

### Graphs

Lead with living spending over time and the inflation-adjusted minimum. Portfolio balance is secondary. Reuse existing P10/P50/P90 annual summaries, with their pointwise and annual-average caveats.

Add a **Worst observed spending path** view: one coherent validation path selected by lowest funded month, ties by most below-minimum months and then scenario order. Show that path's annual spending plus its lowest-month amount and month number. An annual average above the minimum must not conceal a monthly failure. Call it an observed path, not a guaranteed bound.

Keep the base case separately labeled. Preview graphs and Apply must use precisely the same candidate base budget, boost schedule, and guardrails that produced the result. Mark planned phase changes and the boost's end alongside observed below-plan cuts.

Add **Income and withdrawals over time — base case**, with an accessible monthly table and annual summaries. Show Social Security, other configured income (including pensions), portfolio withdrawals by account, taxes paid, planned/funded living, and healthcare using the existing engine. Highlight benefit starts and the end of the boost so the user can see how the portfolio supports early retirement before benefits begin. Label gross income and taxes separately; do not describe gross withdrawals as spendable after-tax living. Show other modeled income only where existing outputs identify it correctly, without treating taxable gains or conversions as new cash.

This is an illustration of the selected plan's base case, not another risk estimate or a complete cash-flow reconciliation. Do not stack account transfers, RMD subsets, investment tax items, and withdrawal totals as independent income sources. Do not add state tax again to total taxes. The canonical base-case projection currently stops at its depletion event: end the timeline there, mark the missing future as unavailable, and state the covered dates. The separate Monte Carlo minimum checks must still continue through the full horizon. A truncated base case cannot prove or disprove income-only coverage after depletion.

### No result and incomplete result

- No passing candidate: “No tested option met your minimum. Your minimum was not reduced.” Show the best measured failures and explain the amount/duration of shortfalls.
- Search range exhausted: “Best option found within the tested range.” Expose range, resolution, and whether a leading result sits on a boundary. Do not call a bounded search a global maximum.
- Final validation failure: retain the failed candidate as inspection-only; do not re-tune using final validation draws.
- Missing observations, a canceled request, or a timeout: no recommendation or Apply authorization. Report an incomplete analysis; never interpret missing observations as zero risk.

## Architecture

Add a spending optimizer in `internal/services/retirement/analysis`, reusing the existing guardrail policy generator, Monte Carlo simulation, funded-living definition, and yearly summaries. Keep the legacy guardrail optimizer and routes operational during migration, but replace its primary template inclusion with the new experience. Keep manual guardrail controls available.

Use new request/result types in `internal/models/spending_optimizer.go`. Add optional spending-experience observations to Monte Carlo results, enabled only for this analysis. A dedicated observer consumes existing monthly engine outputs; it does not alter simulation behavior. Separately, add one optional living-boost schedule to settings and the shared engine living calculation. Keep the cumulative base-living state free of the boost to prevent compounding it twice, and verify parity across canonical, Monte Carlo, backtest, and deterministic expense helpers. Existing scenarios without a boost retain their behavior.

Add separate spending-preview handlers following the existing scenario/revision/fingerprint/token contract. Long-running analysis and graph rendering happen outside registry locks. Recheck ownership and validity before publishing results. Do not undertake a general preview framework refactor as part of this feature.

The UI uses small Go templates and an external JavaScript controller consistent with the existing app. The existing visual design, dark theme, HTMX behavior, and accessibility standard remain authoritative.

## Search and evidence contract

Search actual starting monthly living amounts on a bounded grid, subtracting the active month-zero boost before dividing by the prepared plan's month-zero base-living factor. Derive that factor with the boost disabled. Require a positive nonboost base. Retain the base, total starting value, and boost schedule. The user-selected boost is fixed across the search, so it adds no policy or budget-search dimension. The first-month amount shown must be verified against the canonical projection after applying the candidate.

Compare a no-extra-cuts policy (enabled guardrails with multiplier fixed at 1, plus the absolute floor) and the existing enabled policy grid with that same floor. With no extra cuts, planned phase reductions still apply, stopping at the floor. The actual current-plan comparison retains its original settings and receives the floor as an observation threshold only.

Use three recorded scenario streams: discovery, selection, and final validation. Every policy within a stage receives identical scenario draws. Discovery is a small screen (32 shared futures per budget/policy pair) used only to choose the best flexible policy per budget; it never establishes feasibility and its metrics are never displayed. Selection and final validation each use 1,000 shared futures. Refinement evaluates at most two midpoint budgets per class between the highest passing selected budget and its next higher failing selected position. Align to the request's actual step; skip when there is no bracket or untested interior point. A flexible midpoint inherits the policy of its lower passing endpoint, without another policy search. This is a bounded local heuristic, not a monotonicity proof. Freeze every selection-measured candidate plus the current plan as the frontier before final validation; final validation only measures and rejects. Do not reuse it for another search. All displayed risk comparisons use the same final-validation stream.

The release search is bounded by documented counts and cancellation. The review reports 3.6 ms per surviving 35-year path and 0.3 ms per early-depleted path, but no reproducible command, fixture, or raw output accompanies those figures here. Treat them as sizing estimates until reproduced. The bounded search is about 55,000 paths. Allow up to eight workers subject to available CPUs; measure actual scaling and full-horizon observations after depletion before setting runtime expectations. Never reduce evidence silently when the machine is slow.

Acceptance uses integer counts, not displayed rounded percentages, and applies requirement 13's class rule: the steady class also requires zero below-plan paths. The primary score is the median of each path's near-term funded living sum divided by its observed near-term months. Near-term samples must be complete; do not reward early termination. Tie-break by fewer cut paths, lower severe cut depth, shorter below-plan duration, lower requested starting budget, then stable candidate ID. Lifetime spending and ending wealth remain secondary disclosures.

## Scope and verification

This is one coordinated feature spanning models, analysis, handlers, and the existing What-If surface. It adds the timed living schedule and income/withdrawal display; it does not change asset allocation, income sources, tax strategies, lifespan settings, or guardrail trigger/cut formulas. It does not introduce guaranteed-income products or claim a mathematical maximum.

Keep current healthcare and care assumptions visible and test adverse expenses; do not replace them with generic costs from the video. Full Guyton–Klinger, benefit-claiming optimization, guaranteed-income products, and additional retirement-date optimization remain separate work. The video supports comparing spending paths; its portfolio balances and ending-wealth examples are not evidence that a particular user budget qualifies.

Evidence must cover boost expiry and CPI/guardrail interactions, saved stop-date stability, timeline source/tax accounting and truncation, exact cent boundaries, shortfalls after depletion (both absorbed by living and exceeding it), inflation, phase transitions, input-hook preservation, joint budget/policy search, independent validation, all-or-nothing Apply, stale/canceled previews, browser arithmetic, and accessibility. Financial metrics need independently hand-calculated fixtures as well as seeded engine integration tests.

### Research context for the video follow-up

[Guyton and Klinger's original research](https://www.financialplanningassociation.org/article/journal/MAR06-decision-rules-and-maximum-initial-withdrawal-rates) evaluates a specific combination of withdrawal and portfolio rules under stated assumptions. It is not interchangeable with this app's portfolio-drop/rise policy. [Morningstar's retirement-income research](https://www.morningstar.com/retirement/morningstars-retirement-income-research-finding-your-safe-withdrawal-rate) also distinguishes fixed real spending from flexible approaches. Neither justifies calling a withdrawal percentage universally safe or making the comfortable minimum negotiable.

No application code is changed by this design or its companion plan.
