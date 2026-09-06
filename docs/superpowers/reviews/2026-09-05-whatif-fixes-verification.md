# What-if correctness and lifestyle verification

**Status: complete — all eight tasks accepted, evidence verified, no unresolved flags.** Run started September 5, 2026; verification continued September 6. No commit, push, or live-data deployment.

Worktree: `/tmp/budget2-lifestyle-build`, branch `codex/whatif-lifestyle`, base `3f20055`. Campaign implementation lives in the isolated worktree; only the campaign plan/spec have been copied back to the original checkout. The original checkout also contains pre-existing untracked files.

## Calculation evidence

L1 critical oracle and independent primary/adversarial reviews passed at attempt2. Full $12,000 emergencies now reduce the seeded one-year ending balance by $12,381.33 including lost growth, versus the original $1,031.78. Both event types together: $24,762.67. One event in each of two years: $25,153.49. A $50,000 tax-deferred shock and same-dollar planned expense both end at $964,358.75. See `.swarm/reports/L1.md` and checker-authored reports for tax-estimator and delay traces.

The delay diagnostic found $12,000 unpaid in year 0 with $0 funded withdrawals. Actual unpaid need is now observed separately from the unchanged depletion policy. No debt carry-forward policy was added.

## Synthetic integration scenarios

Command: `rtk proxy go test ./internal/services/retirement/analysis -run TestLifestyleEvidence -count=1 -v`. Durable fixture: `internal/services/retirement/analysis/lifestyle_evidence_test.go`. Raw results: `.swarm/reports/L8-simulations.log`. These are controlled synthetic tests, not the household’s retirement forecast.

| Scenario | Runs | Funded without cuts | Funded with cuts | Shortfall | Runs with cuts | Median cut months | P90 cut months |
|---|---:|---:|---:|---:|---:|---:|---:|
| no_cut_funded | 100 | 100 | 0 | 0 | 0 | 0 | 0 |
| cut_funded | 100 | 0 | 100 | 0 | 100 | 48 | 48 |
| shortfall_despite_guardrails | 100 | 0 | 0 | 100 | 100 | 10 | 11 |
| guardrails_disabled | 100 | 100 | 0 | 0 | 0 | 0 | 0 |
| zero_living | 100 | 100 | 0 | 0 | 0 | 0 | 0 |
| scheduled_phase | 100 | 100 | 0 | 0 | 0 | 0 | 0 |
| healthcare_property_heavy | 100 | 0 | 0 | 100 | 100 | 2 | 2 |
| delayed_withdrawals | 100 | 0 | 0 | 100 | 0 | 0 | 0 |
| july_start | 100 | 100 | 0 | 0 | 0 | 0 | 0 |
| full_emergency_charge=false | 300 | 65 | 169 | 66 | 235 | 216 | 324 |
| full_emergency_charge=true | 300 | 62 | 162 | 76 | 238 | 211.5 | 324 |

The last two rows use the same seeds and an all-Roth portfolio, comparing the original one-twelfth emergency amount scale with full charges. This isolates charge size; it does not replay the prior tax engine. The shared-core depletion-avoidance rate was explicitly measured as 78% at the old charge scale and 74.6666666667% with full charges; the test asserts its identity with the funding counts for this all-Roth/no-delay fixture. No pass threshold depends on a desired success probability.

Cut duration/depth statistics include only runs that actually had cuts, including failed runs. Durations stop at depletion or each simulated horizon; cuts still ongoing there are incomplete observations. The healthcare/property-heavy case reaches shortfall even though living spending is reduced. Scheduled phase changes and zero living spending register no guardrail cut.

## Browser and accessibility evidence

The disposable server was launched through `scripts/whatif-verify.sh`, with copied data and an isolated backup directory. The observed root command trace is `.swarm/reports/L8-launcher-lifecycle.log`. L7 action-by-action evidence, viewports, theme checks, screenshots, and fast/full refresh persistence are in `.swarm/reports/L7-browser.md`.

Verified initial L7 behavior: 390×844, 800×600, 1440×1000, and 720 CSS-pixel reflow; both themes; arrows/Home/End tabs; collapse state; dollar/comparison controls; Cash Flow persistence and focus across quick and completed recalculation. The measured shared navigation overflow at 800px was fixed by retaining the mobile menu until 1280px.

The final browser matrix and copied-scenario interaction/tax checks are in `.swarm/reports/final-browser.md`. Populated NIIT was verified inside total taxes in the Cash Flow table; the copied baseline had no nonzero IRMAA, so populated IRMAA treatment is checked by independent rendered fixtures rather than claimed as a live-browser result. Initial independent a11y review found invisible dark focus outlines. Explicit accent outlines are implemented; L7 attempt-2 independent recheck passed, as did the L2 disclosure and L6 searched-limit copy. Final L5 attempt-3 display recheck passed. The actual-render axe audits recorded zero violations and 29 passes per theme, with unresolved contrast nodes and pre-existing accessibility observations listed in the browser report; this is not a sitewide accessibility certification. Existing unrelated accessibility observations remain separately recorded in the L7 report.

## Independent review corrections

Review rejected initial attempts on four tasks, with fixes and rechecks tracked independently: L1's zero-probability test originally also zeroed event amounts; L6 omitted an actual-search-to-render rounding oracle and produced inverted ranges for saved assumptions outside the default search interval; L7 had invisible dark-theme keyboard focus. L1 and L6 now have passing primary/adversarial evidence; L7's integrated browser recheck passed.

L5's adversarial review also found missed probability-share styling in Monte Carlo and Social Security comparisons, plus a two-cent mismatch from independently rounded funding components. Attempt 2 fixed the original examples but a second adversarial check found that IRMAA was still visibly presented as additive despite already being in expenses, and surplus adjustments were announced as funding need. L5 escalated to Tier 3 for attempt 3. Its executable oracle and primary, adversarial and accessibility reviews pass; the task gate accepted attempt 3. Its rewritten contract checks prominent current totals and every additive-looking future row, rather than selecting a convenient nested tax row. The executable oracle was calibrated red on actual attempt 2 and green on a disposable corrected prototype before dispatch. Model gaps remain authoritative; the template repair makes IRMAA explicitly informational and uses neutral cash-flow wording for the signed cent adjustment. NIIT remains included within taxes.

These are corrected implementation findings, not exceptions silently waived by the lead. Original ACCESSIBILITY.md, critical engine globs and prior ledger history are preserved. The run-scoped gate wrapper selects exactly L1–L8 and uses the existing gate and evidence rules.

## Rendered arithmetic and data isolation

The independent L5 primary review parsed actual rendered output: expenses $4,500.00 plus taxes $258.46 minus RMD $0.00 minus income $3,787.88 equals the displayed gap $970.58. The first breakdown Portfolio Out value $62,176.80 rounds to the verdict's $62,177; the engine, explainability fallback and verdict all use the same outflow, with RMD included once. The final fractional-cent repair adds a separate displayed adjustment where needed; see L5 attempt-2 checks for those cases.

The running verification process was inspected for only its three budget paths: data `/tmp/budget2-whatif-verify-8099/data`, backups `/tmp/budget2-whatif-verify-8099/backups`, listener `:8099`. The data is a real copied directory, not a link to household data. Evidence: `.swarm/reports/L8-isolation.log`. No before/after household-directory hash was captured; this verifies the server's isolated write destinations rather than claiming a retrospective hash comparison.

## Final verification results

- Final integrated verdict/lifestyle browser pass and rendered cash-flow arithmetic: PASS, including corrected visible IRMAA accounting and need/surplus adjustment wording.
- Final whole-change and L5 attempt-3 primary, adversarial and accessibility reviews: PASS. The updated whole-change report supersedes attempt 2's incomplete nested-tax evidence and records five deliberately reverted fixes, each caught by the oracle. Final documentary review: PASS (`.swarm/reports/L8-content.md`).
- Full uncached composite checks on the attempt-2 snapshot: PASS, including vet, tests, staticcheck, vulnerability check and stylesheet verification (`.swarm/reports/final-composite-repaired.log`). Attempt 3 changes only five budget-template expressions/labels plus tests; independent final build, affected handler/template/MCP suites, CSS freshness and the full L5 executable oracle pass. All engine/model/shared-helper files match the earlier full-suite snapshot (`.swarm/reports/final-primary-review.md`). Scoped completion gate: PASS; exact output and statistics follow. Combined UI detector after the final repair: no findings. External agents2 smoke suite: ALL PASS (durable rerun `.swarm/reports/final-gate-smoke.log`).
- Verification-server teardown: PASS, final copied server stopped and temporary data/backups removed; launcher session closed. Intended scope and preserved safeguards: PASS (`.swarm/reports/final-scope.log`); process destinations recorded in `L8-isolation.log`.

## Acceptance record

```text
OK: all tasks accepted, evidence verified, no unresolved flags
stats: L1 tier=3 first-attempt=1 failed (now: status=accepted attempt=2)
stats: L2 tier=2 first-attempt=1 clean (now: status=accepted attempt=1)
stats: L3 tier=3 first-attempt=1 clean (now: status=accepted attempt=1)
stats: L4 tier=2 first-attempt=1 clean (now: status=accepted attempt=1)
stats: L5 tier=3 first-attempt=1 failed (now: status=accepted attempt=3)
stats: L6 tier=2 first-attempt=1 failed (now: status=accepted attempt=2)
stats: L7 tier=2 first-attempt=1 failed (now: status=accepted attempt=2)
stats: L8 tier=1 first-attempt=1 clean (now: status=accepted attempt=1)
first-attempt clean: 4/8 (no-evidence rows: 0)
```

Implementation and coordination records remain in the uncommitted worktree. The updated plan/spec are synchronized to the original checkout.

## PR handoff

The user subsequently authorized a commit, branch push and pull request. The public PR includes source, tests, plan/spec and this summary. Detailed `.swarm` review records, raw browser captures and copied scenario data stay local; references to those records above describe the local verification session. No live-data deployment is part of the PR.
