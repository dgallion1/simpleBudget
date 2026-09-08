# GO2 oracle calibration

Attempt: 1. This artifact author made no production edits and writes no checker verdict.

- `./.swarm/tier3/GO2/accept.sh` against featureless application: exit 1, explicit optimizer-capability absence (`calibration.baseline.log`). No compile error used as the defect.
- Same complete oracle against a disposable specification prototype, with synthetic runner outputs and minimal additive GO1 model types: exit 0, `ORACLE PASS` (`calibration.prototype.log`).
- Deliberately changed held-out seed to search seed in that prototype: exit 1, specifically `validation reused search seed` (`calibration.mutant.log`).
- `go build ./internal/services/retirement/analysis` in the disposable prototype: exit 0.
- Disposable prototype tree removed after calibration; no prototype code entered application.

The oracle uses a Go overlay and never edits production test files. It covers fixed 64/1000 defaults, grid/current dedup, scenario seed provenance at runner boundary, unchanged baseline, funded outcomes, Wilson95 interval, count-based inclusive qualification, deterministic/ranked/deduplicated Pareto recommendations, invalid requests, cancellation and seed reproduction. Calibration corrected overly exact floating-point mean comparison and Wilson reference constants before production dispatch.

Limitations: synthetic runner calibration establishes aggregation/search contract, not engine sampling fidelity. Production worker acceptance must include a small real engine runner test proving seeded per-run repeatability, observation floor and unchanged baseline policy through actual StepMonth, plus cancellation between real runs. GO1 independently verifies funded/CPI accounting. Lead explicitly assigned actual production integration tests to worker acceptance so this oracle remains inexpensive. No public run-count knobs are added.
