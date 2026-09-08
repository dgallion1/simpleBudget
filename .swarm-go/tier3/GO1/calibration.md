# GO1 oracle calibration

Executable: bash .swarm/tier3/GO1/accept.sh [optional repository root]. Uses Go overlay; never installs tests into production paths.

Fail end: current featureless application, exit 1, explicit missing GuardrailConfig.MinMonthlySpendingReal / MonteCarloConfig.MinMonthlySpendingReal assertions; Go compilation succeeds. Evidence: calibration-fail.log.

Pass end: copied application to /tmp/go1-calibration, patched a disposable direct floor/funded observer prototype, ran bash /tmp/budget2-guardrail-optimizer/.swarm/tier3/GO1/accept.sh /tmp/go1-calibration, exit 0 and ORACLE PASS. Prototype and its generator removed after calibration. Evidence: calibration-pass.log.

Coverage: deterministic StepMonth zero/variable CPI and decline/phase changes; effective multiplier and any emitted event floor consistency; canonical adjusted/funded fields; actual historical backtest final-balance effect; disabled enforcement plus independent measurement; nil legacy observation; zero assets with sufficient income; discretionary shortfall above floor versus below; monthly half-away cent comparison and accumulation; continued horizon and first depletion metadata; delayed income; genuine tax-deferred delay and later access; annual funded totals/cut severity/count; zero-CPI final real balance.

Fixture calibration catches: income at $7,500 has tax obligations, so exact arithmetic income fixtures use $750 below the deduction while floor enforcement remains tested at $7,500. Tax-deferred locking requires explicit TaxDeferredDelayYears, not just age below 59.5. Suppressed no-op guardrail events are valid.

Limits delegated to permanent implementation tests: independently reproduced stochastic CPI final balance; explicit separate healthcare and tax priority fixture; scenario-chain floor replacement; event fixture guaranteed to produce an effective event above floor. Existing consumers enumerated here: engine StepMonth, canonical engine Run projection Months, historical runSingleHistoricalSequence, Monte Carlo RunSingleMonteCarloSimulation. UI and chain coverage belong to integration checks.

Build: go build ./... fails on foreign concurrent GO3 handlers awaiting absent models.GuardrailOptimizerRequest/Candidate and analysis.OptimizeGuardrails. Oracle package builds successfully on baseline and prototype. No UI files changed.
