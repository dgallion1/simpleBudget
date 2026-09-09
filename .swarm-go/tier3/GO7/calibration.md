# GO7 oracle calibration

Calibrated before production backend implementation. Source snapshot created with
`git archive HEAD` into `/tmp/go7-oracle-prototype`; no concurrent UI working-tree
edits copied. Oracle lives outside that snapshot and is injected with Go overlay.

- Fail end: `bash .swarm-go/tier3/GO7/accept.sh` on featureless shared tree exited 1
  with `ORACLE FAIL: simulated annual bands capability absent`, before compilation.
  Evidence: calibration-fail.log.
- Pass end: disposable prototype supplied additive types, independent sorted
  interpolation, monthly capture, and runner/evaluate connection. Executed
  `bash .swarm-go/tier3/GO7/accept.sh /tmp/go7-oracle-prototype`; all oracle tests
  and full analysis/engine/models suites passed; final line `ORACLE PASS`.
  Evidence: calibration-pass.log. Prototype removed after calibration.
- Mutation: changed prototype P10 from interpolated q=.1 to minimum q=0. Same
  command exited 1 on explicit `got 0 want 20`, not a harness/compile error.
  Evidence: calibration-mutant.log.

Oracle covers independently sorted four measures, zero paths, unequal horizons,
contributor counts, snake_case JSON, invalid annual values and incomplete years,
legacy absence and mixed absence, seeded capture on/off equality, real spending
total and last portfolio reconciliation, no-floor opt-in behavior, held-out
validation provenance for every final candidate and both baselines, production
runner reproducibility, and an actual optimizer search with a short horizon.

Prototype compatibility changes exposed two required existing-test updates:
models/guardrail_floor_test.go struct `!=` becomes DeepEqual because Years is a
slice; analysis/guardrail_optimizer_test.go seeded runner expected config must
enable CaptureSpendingYears. No production assertions were bypassed. Initial
prototype run discovered these before successful final calibration.

Limits: oracle tests are a bounded acceptance contract, not exhaustive proof of
all possible settings. UI consumers are GO8's independent scope; backend suites
cover existing consumers. No production implementation or verdict was written.

Lead review extended the oracle with independent inflation RNG replay: annual nominal monthly mean and each intermediate real portfolio CPI division, under nonzero inflation/no shocks. Entire extended oracle re-calibrated at featureless fail and disposable prototype pass ends. A second mutant substituted real spending for nominal capture and failed the new assertion (1000 vs1054.1271858270). Final prototype removed.
