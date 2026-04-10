# SS Portfolio-Aware Claiming Age Optimizer

> For agentic workers: use `superpowers:subagent-driven-development` or `superpowers:executing-plans`. Track progress by updating the checkbox steps in this file.

## Goal

Add a synchronous portfolio-impact analysis to the Social Security claiming-age optimizer. When the user selects at least one valid claim age, run a small Monte Carlo grid across the eligible claim ages and show which selection best protects portfolio survival.

## Desired Outcome

- The existing Social Security comparison tables still render as they do today.
- A new portfolio-impact panel appears only when at least one selected claim age is eligible for portfolio analysis.
- The panel shows, per eligible claim age:
  - survival rate
  - median ending balance
  - P10 / P90 ending balances
  - delta versus the user's currently selected baseline
- The system supports:
  - primary-only analysis
  - spouse-only analysis
  - dual-person analysis when both selections are configured

## Constraints And Design Decisions

- Keep this feature synchronous. Do not introduce polling, background jobs, or async readiness flags.
- Reuse the existing SS comparison logic for monthly-benefit lookups so the new panel stays aligned with the claiming tables.
- Treat the currently selected claim-age combination as the baseline.
- Skip ages below the relevant person's current age.
- Exclude a person from portfolio analysis when their claim age equals their current age (already claiming).
- Keep the Monte Carlo budget intentionally small and centralized behind a named constant so the request stays responsive.
- Prefer deterministic or range-based tests. Do not write brittle tests that depend on specific Monte Carlo winners unless the randomness is controlled.
- Avoid brittle implementation instructions in this plan. Verify the surrounding code before editing instead of copying code snippets blindly.

## Known Current-State Findings

These are already true in the codebase as of April 9, 2026:

- `SSPortfolioEligible` currently requires a spouse and both claim ages.
- `SSPortfolioAnalysis` still includes async-oriented `Ready` and `Error` fields.
- `RunFullAnalysis` populates Social Security analysis but does not attach portfolio analysis.
- The Social Security template has no portfolio-impact section yet.

## Files In Scope

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/services/retirement/social_security.go` | Modify | Eligibility rules and portfolio-analysis computation |
| `internal/services/retirement/social_security_test.go` | Modify | Unit tests for eligibility, grid shape, baseline deltas, and ranking helpers |
| `internal/models/whatif.go` | Modify | Remove async-only fields from `SSPortfolioAnalysis` if no callers still need them |
| `internal/services/retirement/calculator.go` | Modify | Attach portfolio analysis during full analysis |
| `internal/handlers/whatif/handlers_test.go` | Modify | Integration coverage for the SS handler response |
| `web/templates/components/whatif/social-security.html` | Modify | Render the portfolio-impact panel |

## Task 0: Pre-Flight Validation

Objective: confirm the implementation path before changing behavior.

- [ ] Inspect the current Social Security analysis flow end to end:
  - `SSPortfolioEligible`
  - `RunSSAnalysis`
  - `RunFullAnalysis`
  - the SS handler path
  - the SS results template
- [ ] Confirm how Monte Carlo success-rate, median, and percentile stats are exposed so the new analysis can reuse the existing types.
- [ ] Check whether there is already a safe cloning or copy pattern for `WhatIfSettings`.
- [ ] Search for all reads of `SSPortfolioAnalysis.Ready` and `.Error` before removing them.
- [ ] Record the chosen Monte Carlo run count in one constant, not scattered literals.

Acceptance criteria:

- The worker understands the actual call graph before changing it.
- Any removal of model fields is proven safe by code search, not assumption.

## Task 1: Relax Portfolio Eligibility

Objective: allow portfolio analysis when at least one configured person has a valid selected claim age and usable FRA benefit.

Files:

- `internal/services/retirement/social_security.go`
- `internal/services/retirement/social_security_test.go`

Implementation notes:

- Primary eligibility should be evaluated independently from spouse eligibility.
- Spouse eligibility must still require a spouse, spouse age, spouse FRA benefit, and a valid spouse claim age.
- Primary-only and spouse-only cases should both work.
- Ages below the person's current age must remain ineligible.

Steps:

- [ ] Add unit tests for:
  - nil settings
  - missing SS config
  - no selected claim ages
  - primary-only eligible
  - spouse-only eligible
  - both eligible
  - claim age below current age
  - single-person primary-only
  - zero FRA benefit for an otherwise selected person
- [ ] Update `SSPortfolioEligible` to return true when either the primary path or spouse path is independently eligible.
- [ ] Run the retirement service tests and confirm no regressions.

Acceptance criteria:

- `SSPortfolioEligible` no longer requires both people to be configured.
- Existing invalid cases remain invalid.

## Task 2: Remove Async-Only Model Fields

Objective: simplify `SSPortfolioAnalysis` for synchronous rendering.

Files:

- `internal/models/whatif.go`

Steps:

- [ ] Remove `Ready` and `Error` from `SSPortfolioAnalysis` only after confirming no live references remain.
- [ ] Verify JSON output still matches the synchronous rendering model.
- [ ] Run a full build after the change.

Acceptance criteria:

- `SSPortfolioAnalysis` contains only fields required by the new synchronous UI.
- The project builds cleanly after the struct change.

## Task 3: Implement Portfolio Analysis Computation

Objective: compute portfolio outcomes for each eligible claim age while holding the other person's selected age fixed.

Files:

- `internal/services/retirement/social_security.go`
- `internal/services/retirement/social_security_test.go`

Implementation notes:

- Add a method on `Calculator`, for example `RunSSPortfolioAnalysis()`.
- Reuse `RunSSAnalysis()` results to populate monthly-benefit values shown in the portfolio panel.
- If only one person is selected, vary only that axis.
- If both are selected, produce both primary and spouse option tables using the currently selected paired baseline.
- Keep the Monte Carlo run count behind a dedicated constant such as `ssPortfolioMonteCarloRuns`.
- Prefer an explicit copy helper for settings. If a JSON round-trip is the only practical option, document why it is safe for this path.

Recommended decomposition:

- [ ] Add or extract a helper that determines the valid age range for each active person.
- [ ] Add a helper that clones settings and overrides selected claim ages for a single simulation cell.
- [ ] Add a helper that runs one reduced Monte Carlo cell and maps the result into `SSPortfolioOption`.
- [ ] Add a pure helper for ranking options and choosing the optimal age so tie-breaking can be unit-tested without depending on stochastic output.
- [ ] Compute `BaselineSurvivalRate` from the user's selected current combination.
- [ ] Populate `DeltaSurvivalRate` for every returned option.

Testing guidance:

- [ ] Write unit tests that verify:
  - ineligible input returns `nil`
  - spouse-only runs produce spouse options and no primary options
  - primary-only runs produce primary options and no spouse options
  - option counts respect current-age minimums
  - baseline delta is zero for the selected age
  - monthly benefits match the corresponding SS comparison table entries
- [ ] Test tie-break logic in a pure helper with fixed inputs instead of relying on Monte Carlo collisions.
- [ ] Keep stochastic assertions coarse-grained:
  - rates are in range
  - returned ages are in range
  - expected tables are non-empty when eligible

Acceptance criteria:

- The analysis returns stable data shapes for all supported configurations.
- Tests do not depend on random Monte Carlo winners.
- The implementation keeps request latency reasonable by using the reduced run-count constant.

## Task 4: Wire Portfolio Analysis Into Full Analysis

Objective: include portfolio analysis in the normal Social Security what-if flow.

Files:

- `internal/services/retirement/calculator.go`
- `internal/handlers/whatif/handlers_test.go`

Implementation notes:

- Attach the portfolio analysis only after the normal SS analysis is available.
- Avoid recursive or duplicated work that would accidentally trigger analysis loops.
- The handler should not need special-case logic if `RunFullAnalysis()` already returns the enriched SS analysis payload.

Steps:

- [ ] Update `RunFullAnalysis()` to attach `ssAnalysis.Portfolio` when the settings are portfolio-eligible.
- [ ] Add an integration test covering the Social Security POST flow and assert the response payload includes non-nil portfolio analysis for an eligible request.
- [ ] Run the handler package tests and at least one broader project test pass afterward.

Acceptance criteria:

- The existing handler flow returns portfolio analysis without a separate endpoint or polling model.
- Ineligible submissions still render correctly without portfolio data.

## Task 5: Add The Portfolio Impact Panel

Objective: render the new analysis beneath the current Social Security comparison content.

Files:

- `web/templates/components/whatif/social-security.html`

Implementation notes:

- Render the panel only when `.Analysis.SocialSecurity.Portfolio` is present.
- Keep the existing claiming-comparison tables unchanged.
- Make it obvious that the new panel is a portfolio view, not a replacement for the cumulative-benefit tables.
- If both people are active, show separate primary and spouse portfolio tables.
- Highlight the portfolio-optimal age in each table.
- Clearly label the baseline context so the delta column is understandable.

Suggested UI content:

- [ ] Intro copy explaining that this panel reflects portfolio survival, not just cumulative SS payouts.
- [ ] A short summary banner that names the portfolio-optimal selection and compares it with the currently selected baseline.
- [ ] A table for each active person with columns for:
  - age
  - monthly benefit
  - survival
  - median ending balance
  - P10 / P90 ending balance
  - delta vs baseline
- [ ] A short footnote stating the Monte Carlo run count used for the grid.

Verification:

- [ ] Build the app and confirm templates parse.
- [ ] Manually exercise:
  - spouse-only selection
  - primary-only selection
  - both selected
  - no selected claim ages
- [ ] Confirm the panel is absent when ineligible and present when eligible.

Acceptance criteria:

- The UI cleanly separates cumulative-benefit guidance from portfolio-survival guidance.
- The template handles all eligible and ineligible states without rendering errors.

## Task 6: Regression, Performance, And Cleanup

Objective: finish with a reviewable, low-risk change set.

Steps:

- [ ] Run targeted tests for the retirement service and what-if handler.
- [ ] Run `go test ./...` and `go build ./...`.
- [ ] Run `go vet ./...` and any project-standard static analysis that is already available.
- [ ] Review the diff for unintended behavior changes.
- [ ] Capture any follow-up work that should not block this feature, such as:
  - caching or memoization if the sync grid proves slow
  - richer explanation copy in the UI
  - future two-dimensional optimization if the current design still varies one axis at a time

Acceptance criteria:

- The branch is shippable without async infrastructure.
- Tests, build, and static checks pass.
- Any known limitations are documented explicitly instead of being left implicit.

## Definition Of Done

- Portfolio analysis is available from the existing Social Security what-if flow.
- Single-person and spouse-only cases both work.
- The results model and template are synchronous-only.
- The UI shows baseline-relative portfolio results with clear copy.
- The final change set has automated coverage for eligibility, computation shape, and handler integration.
