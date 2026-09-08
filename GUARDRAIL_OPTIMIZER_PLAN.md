# Guardrail Optimizer Implementation Plan

For agentic workers: use subagent-driven-development; workspace swarm rules govern verification.
Goal: optimize lifetime funded real spending and stability against an editable floor-success target.
Architecture: shared monthly engine floor and observation; bounded analysis search; explicit UI preview/apply.
Tech stack: existing Go, html/template, HTMX.
Spec: GUARDRAIL_OPTIMIZER_SPEC.md (including latest implementation rulings).

## Global constraints
7500 default editable real monthly floor. Success presets90/95/99 plus custom, no selected default. Never conflate avoids depletion with floor success. Keep baseline policies untouched; identical random scenarios per comparison. No changed live data or deployment. Independent checkers and mechanical acceptance mandatory.

## GO1: shared floor and observation (Tier3)
Files: internal/models/whatif.go; internal/services/retirement/engine/stepper.go, month.go, new spending_floor.go; internal/services/retirement/analysis/monte_carlo.go, new floor_outcomes.go; matching test files.
Interfaces: exact model and outcome fields in spec GO-R3; funded arithmetic and cent convention GO-R2. Keep old fields additive/backward compatible.
- Calibrate .swarm/tier3/GO1/accept.sh on baseline and disposable prototype.
- Worker first adds regression tests demonstrating floor/CPI/phase/funding cases; run go test ./internal/services/retirement/engine ./internal/services/retirement/analysis.
- Implement shared floor, effective event dollars/multipliers, canonical monthly fields, full-horizon opt-in MC observer.
- Oracle then primary and second checker; escalate-scan after verdicts, gate check before accepted.

## GO2: search service (Tier3)
Files: internal/models/guardrail_optimizer.go; internal/services/retirement/analysis/guardrail_optimizer.go and tests.
Interface: OptimizeGuardrails(ctx context.Context, in engine.Input, req models.GuardrailOptimizerRequest) (*models.GuardrailOptimizerResult,error).
Request: FloorMonthlyReal float64, TargetSuccessPct float64, Seed int64.
Result: Request, Seed, ValidationSeed, SearchRuns, ValidationRuns, Candidates, Recommendations, HorizonMinYears, HorizonMaxYears. Candidate contains stable ID, labels, config, metrics, qualifies, baseline flag. Define concrete types before GO3 starts.
- Author and calibrate executable GO2 oracle before implementation dispatch.
- Tests cover bounded candidate grid, common seeds, separate validation, cancellation, invalid/nonfinite input, chain rejection, inclusive qualification, Wilson intervals, baseline preservation, deterministic ranking, no feasible result.
- Implement 32-grid plus current-floor candidate, 64 search runs and1000 validation runs, six-member shortlist, Pareto rankings from spec, no weighted score.
- Run oracle and both checker lanes, then gate.

## GO3: UI preview and explicit apply (Tier2)
Files: new internal/handlers/whatif/handlers_guardrail_optimizer.go and tests; route registration in handlers.go; web/templates/components/whatif/guardrails.html and new guardrail_optimizer.html; scoped JS if necessary.
- Use finalized GO2 result types. Add floor and target form, explicit Run, cancellation, semantic comparison table, all result metrics, uncertainty and conventions.
- Server validates inputs; hash prepared settings and request for stale-result protection. Store bounded preview results server-side; Apply accepts opaque candidate identity from a completed qualifying preview only, revalidates settings, persists config and refreshes projection. Do not trust posted candidate settings.
- Preserve new floor field in ordinary guardrail form updates; provide editable persisted floor control. Invalidate preview on input/settings changes. Announce statuses and restore focus.
- Handler/render tests assert UI wording, default floor/no target, validation, preview no writes, stale/cancelled apply blocked, explicit apply roundtrip. Run browser keyboard/axe light+dark checks.
- tests,a11y,second verdicts; escalate-scan and gate.

## Final validation
Run full go build ./..., go test ./..., go vet ./..., staticcheck ./..., orchestration bash smoketest/gate/run_tests.sh. Site-wide a11y and lead review authored files. gate done and stats. Present verified result for deployment review; never deploy during build.

## GO2 exact metrics and private test seam
Metrics GuardrailOptimizerMetrics: Runs,FloorShortfallPaths,DepletionPaths int; FloorSuccessPct,FloorSuccessCILowPct,FloorSuccessCIHighPct,DepletionRiskPct,MedianLifetimeFundedLivingReal,P10LifetimeFundedLivingReal,P95WorstAnnualCutPct,MeanAnnualCutCount,MedianEndingBalanceReal float64. private guardrailOptimizerRunner func(context.Context,engine.Input,int64,int,float64)([]models.MonteCarloResult,error); optimizeGuardrailsWithRunner(ctx,in,req,run) is same core as public service. The runner receives seed,runs,observation floor.
UI use existing handlers_roth_recommendations.go bounded opaque-token preview pattern with manager/scenario/revision/fingerprint checks and conditional save. No direct candidate config input accepted on Apply. Existing guardrail form must preserve/edit absolute floor and allow optimized legacy minpct0 when absolute floor configured.
