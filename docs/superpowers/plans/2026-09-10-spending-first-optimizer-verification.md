# Spending First Optimizer — integrated verification, 2026-09-10

Verification in progress on `codex/spending-first-optimizer`, based on `895d509953d72ba84b09bfac1f45efce84f31542`. No deployment or commit is part of this work. All browser changes target the repository lifecycle script's disposable data copy.

## New financial evidence

`TestSpendingIntegrationIncomeOnlyAccounting` checks a three-year prepared scenario, living $100, real minimum $90, fixed property tax $20 and zero assets. No healthcare or income tax is configured in this minimal exact-dollar oracle. The full monthly engine output and seeded Monte Carlo observer are compared to hand calculations for all 36 months, including depletion: income $115 funds $95 (flexible only); $105 funds $85 (minimum failure only); $15 funds $0 with $5 unpaid obligations (overlapping counters); $120 funds the whole $100 (both classes qualify). $109.99 also fails at real-cent precision. The MC seed is 812. Fixed property tax avoids the engine's intentional stochastic healthcare variation; healthcare and care are separately retained in stress tests.

`TestSpendingIntegrationOneCentFinalMonth` puts a single $0.01 floor shortfall in month 36 of one of 1,000 complete paths. Qualification fails even though 999 paths never fall short.

`TestSpendingIntegrationScheduleAndCuttableBoost` executes StepMonth and Monte Carlo annual captures through all 36 months. A $30 boost stops before, at, or after the month-12 benefit claim and phase transition (2026-07, 2027-01, 2027-07). The $100→$80 phase reduction crosses the $90 minimum; expiry is not a risk cut. Separately, zero assets and $110 income fund $90 base plus $20 property tax while a $20 boost is cut for its first 12 months without breaching the floor or treating it as an obligation.

`TestSpendingIntegrationStochasticObserverParity` keeps $900 Medicare healthcare and configured $1,800 care from age 85, a 45-year requested horizon, account-specific allocations, 8% inflation, volatility and guaranteed annual health/spending shock attempts and crash attempts. Seeds 281, 812 and 20260910 produce complete nonempty annual captures and full 40–50-year observations. Turning the spending observer on leaves every other Monte Carlo output exactly identical, including captured years. The existing real-runner event tests independently cover actual early crashes, high inflation, health shocks, phase changes and late depletion.

`TestSpendingIntegrationFundedNearTermAdvantage` runs actual deterministic engine futures for 120 months. A $1,800 initial portfolio, $110 income and $20 property tax support planned median funded living $90 versus flexible $111.78 over the first five years. Flexible spending is lower later, all 1,000 repeated deterministic futures have cuts, and none has floor/unpaid-obligation failures. A higher $110 minimum has visible failed candidates and no recommendation. These are deterministic scenario-runner fixtures, not 1,000 statistically independent futures.

## Preview, persistence and replay

`TestSpendingIntegrationPreviewGraphApplyReload` performs the real search, authorized graph request, Apply and disk reload. A 10-year canonical plan with 0.8 phase multiplier starts at $8,000.25 from base $8,750 plus a $1,000.25 boost. The minimum is $6,000.25 and the boost expires in 2027-09. Pension begins month 6; Social Security begins month 24. The canonical timeline reproduces every observed monthly income, withdrawal, tax, planned/funded living and healthcare field exactly after reload. Scheduled expiry is $1,000.25 and the selected planned option has zero risk cuts. Ordinary manual-rule save preserves the absolute floor and boost; advancing start past the expiry does not move or reactivate it.

Seed replay: master 20260910, selection -6884282663016313855, final -1800455987195215627. Replaying 1,000 full final-stream paths after reload reproduces cut/floor/unpaid counts 0/0/0. The focused handler integration test passed in 11.127 seconds.

## Runtime evidence

Compiled analysis test binaries run sequentially under `/usr/bin/time -v`; builds and browser searches do not overlap timing. Host: AMD Ryzen 9 7950X, Linux amd64, Go 1.26.6, GOMAXPROCS 32, at most eight simulation workers. Fixture: start 2026-09, primary age 65, $2.4m assets, $8,000 base, $900 healthcare, $1,000 boost ending 2031-09, $1,500 pension with 2% COLA and $3,000 Social Security hooks; 60/15/25% deferred/Roth/taxable, account-specific allocations, phase multiplier 0.85 at age 75. Default tax/inflation/withdrawal settings remain authoritative. Floor $7,000; near-term five years; default starting grid $7,000–$18,000 by $100 (111 positions). Search screens nine positions and policies, then refines. Samples remain 32 discovery, 1,000 selection and 1,000 independent final. Master/selection/final seeds are 20260910/-6884282663016313855/-1800455987195215627.

| Requested years | Observed years | Paths | Evaluations | Final rows | Wall time | Peak RSS |
|---|---|---:|---:|---:|---:|---:|
|10|10–15|54,792|351|23|49.23 s|59,128 KiB|
|35 (retained unchanged search/runner measurement)|30–40|46,792|343|19|111.39 s|68,132 KiB|
|45|40–50|46,792|343|19|139.03 s|82,644 KiB|

The retained corrected-code 35-year result fits the initial 180-second timeout without reducing final evidence. It is reused because Task 6 changes tests only. The active search cancellation test signals from a real running SS hook before canceling; measured latencies were 8.24–13.65 ms, returning context.Canceled and no partial result, below the two-second oracle bound. Exact eight-worker, one-active-search and preview-capacity tests remain part of the required suites.

## Commands and pending release checks

Focused commands passed: `rtk proxy go test ./internal/services/retirement/analysis -run '^TestSpending(Integration|OptimizerSeedsAndTieBreaks)' -count=1 -v` (0.077 s), and `rtk proxy go test ./internal/handlers/whatif -run '^TestSpendingIntegration' -count=1 -v` (11.127 s). The initial test compile exposed EndMonth's pointer type; the first executable run exposed the test's annual-average unit and zero-volatility return assumptions. These were test corrections, with no production logic change.

The first full suite stopped at `TestHandleWhatIfGuardrailPlanFloorHint_Present`: it expected the retired main-page `guardrail-optimizer-floor`. The retained partial was still correct. The test now preserves its exact calculated $5,000/No-Go/year-1 hint and aria linkage on that partial, and checks the main page's new explicit minimum and associated instructions. Both present/absent hint tests passed (0.048 s). No legacy test was removed or financial assertion weakened. After this test-only migration, the failed full-test gate was repeated successfully.

| Command | Exit | Observed result |
|---|---:|---|
|`rtk proxy go build ./...`|0|0.851 s wall|
|`rtk proxy go vet ./...`|0|0.480 s wall|
|`rtk proxy go test ./...` (first run)|1|62.663 s wall; sole legacy main-page hint failure|
|`rtk proxy go test ./...` (after correction)|0|50.470 s wall; whatif 49.667 s; other previously green packages cached|
|`rtk proxy staticcheck ./...`|0|1.274 s wall|
|`rtk proxy go test -race ./internal/services/retirement/analysis ./internal/handlers/whatif`|0|150.763 s wall; analysis 57.249 s, whatif 147.704 s|
|`rtk git diff --check`|0|No whitespace errors|
|`rtk git diff --stat` / `rtk git diff`|0|Inspected scoped source changes; existing generated CSS dominates the full diff|

Full command output was retained without filtering, and the runner stopped at the failed gate before staticcheck/race. Build/vet were not repeated for a test-only assertion correction. No Go source changed after the green full suite. Copied-data browser checks remain pending. The baseline Task 5 accessibility review passed; Task 6 adds no production UI markup, styles or interaction behavior.


## Acceptance coverage and retained evidence

| Release contract | Evidence |
|---|---|
|Starting budget and policy jointly searched; funded five-year ranking|`TestSpendingOptimizerFrontierStagesAndBaseline`, `TestSpendingOptimizerDiscoveryChoosesFundedPolicyAndFallback`, new deterministic engine advantage test|
|Explicit inflation-adjusted minimum; never silently relaxed|New income-only, single-cent final-month and phase-crossing tests; retained `TestFloorRegressionSharedMonthlyFloor`, `TestSpendingOptimizerFormMinimumRequiresSavedFloor` and render form tests|
|Measured comparison; no invented uplift|Retained displayed-comparison cent, unavailable/rounded-zero and render-no-unconditional-more tests|
|Correct conditional cut denominators, timing/depth/duration and overlapping failures|New exact accounting; retained `TestSpendingRiskAggregatesCompletePaths`, depletion coverage, worst-path ranking, and independent-summary render/browser tests|
|Every month including depletion; no percentile acceptance oracle|New last-cent final-month/1,000-path fixture and full-horizon schedule/stress tests; retained incomplete evidence rejection|
|Independent discovery/selection/final; frozen finalists|Retained `TestSpendingOptimizerFrontierStagesAndBaseline`, `RefinementFreezesEveryMeasuredCandidate`, `FinalChangesEitherStatus`; new persisted final-seed replay|
|Planned class zero cuts; unpaid means shortfall beyond living|Exact $100/$90/$20 accounting and real deterministic search|
|All selection rows final-measured at 1,000; complete frontier|Retained frontier/refinement tests and real preview integration; measured benchmark row/path counts|
|Phase/boost starting dollars, graphs and saved base reconcile|New actual preview graph/Apply/reload test; retained non-unit phase mapping tests|
|Boost cuttable, CPI-adjusted, fixed calendar expiry and engine parity|New schedule/cuttable-boost tests; retained `TestLivingSpendingBoostProjectionLoopParity`, historical CPI and canonical persistence tests|
|Income/withdrawal timing, tax accounting and unavailable later months|New 120-month exact replay; retained literal accounting/partial-year tests in `spending_funding_test.go`, truncated graph test and accessible monthly browser fixture|
|Portfolio-trigger rules and honest observed-sample claims|Retained form/results render tests and Task 5 browser/a11y review; no financial policy changes in Task 6|
|Atomic complete Apply; stale/cancel/replay/tamper rejection|New real preview replay plus retained `TestSpendingApplyRejections`, `ConcurrentAndSaveWindowGuards`, `RetryableSaveFailure`, `StalePreviewConsumed`, graph rejection, blocked-delivery tests|
|Eight workers, one active manager/scenario search, bounded storage|Retained `TestSpendingRunnerWorkerBoundAtOneAndEight`, cancellation, preview-token lifecycle and chain/capacity tests; fresh active-cancellation latency|
|Required checks, desktop/mobile/theme and accessibility|Go/staticcheck results above; race passed; browser pending; unchanged Task 5 scoped accessibility PASS|

Retained evidence pointers: Task 3 report, “Benchmark fixture and raw evidence,” corrected full run (111.384018111 s/op, 111.39 s elapsed, 68,132 KiB RSS); Task 4 report, “Fix round 1 — blocked response delivery and unchosen minimum,” documents exact cancellation/Apply locking oracles and race pass; Task 4A report, “Accounting and scope” and “Graph lifecycle and parity,” covers literal nine-field cash-flow accounting and truncation; Task 5 report and fix-round section cover monthly table, independent summaries, theme contrast and DOM cleanup. Final Task 5 accessibility verdict: `.swarm/verdicts/spending-ui.2.checker-a11y.verdict` and `task-5-fix-1-a11y-review.md` (PASS). These financial/lifecycle tests also ran in the fresh full Go suite.
