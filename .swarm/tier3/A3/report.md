# Tier 3 divergence report — A3

| worktree | oracle exit |
|----------|-------------|
| wt-glm   | 1 |
| wt-local | 0 |

## Divergence
### wt-glm output
```
CHECK 1 build: PASS
CHECK 2 vet: PASS
CHECK 3 existing-tests: PASS
CHECK 4 probe-compiles: PASS
CHECK 5 clean-pair-auto-pairs: FAIL
CHECK 6 METRICS-EXCLUDE-TRANSFERS: PASS
CHECK 7 transfers-remain-visible: PASS
CHECK 8 coincidence-never-auto-pairs: PASS
CHECK 9 coincidence-is-suggested: FAIL
CHECK 10 external-leg-classified: PASS
CHECK 11 confirm-decision-persists: FAIL
CHECK 12 reject-not-resuggested: FAIL
SUMMARY: 8 passed, 4 failed
```
### wt-local output
```
CHECK 1 build: PASS
CHECK 2 vet: PASS
CHECK 3 existing-tests: PASS
CHECK 4 probe-compiles: PASS
CHECK 5 clean-pair-auto-pairs: PASS
CHECK 6 METRICS-EXCLUDE-TRANSFERS: PASS
CHECK 7 transfers-remain-visible: PASS
CHECK 8 coincidence-never-auto-pairs: PASS
CHECK 9 coincidence-is-suggested: PASS
CHECK 10 external-leg-classified: PASS
CHECK 11 confirm-decision-persists: PASS
CHECK 12 reject-not-resuggested: PASS
SUMMARY: 12 passed, 0 failed
```
### diff (glm vs local)
```diff
5c5
< CHECK 5 clean-pair-auto-pairs: FAIL
---
> CHECK 5 clean-pair-auto-pairs: PASS
9c9
< CHECK 9 coincidence-is-suggested: FAIL
---
> CHECK 9 coincidence-is-suggested: PASS
11,13c11,13
< CHECK 11 confirm-decision-persists: FAIL
< CHECK 12 reject-not-resuggested: FAIL
< SUMMARY: 8 passed, 4 failed
---
> CHECK 11 confirm-decision-persists: PASS
> CHECK 12 reject-not-resuggested: PASS
> SUMMARY: 12 passed, 0 failed
```

<!-- Boss: after adjudicating, append a line starting 'RESOLUTION:' -->

## STATUS: NOT A VALID N-VERSION COMPARISON — no RESOLUTION line follows

The `wt-glm` worker was killed mid-implementation by an OpenRouter **403 "Key
limit exceeded (total limit)"** — the key's $20 cap, not the account balance
(which still held ~$14.69). Its four failing checks are code it never got to
write, not a considered alternative, so the divergence above carries no
information about design. Adjudicating it would be adjudicating a budget
event.

Both sides are preserved as commits on their tier3 branches (`tier3/A3/glm`,
`tier3/A3/local`) rather than left in `/tmp`, which does not survive a reboot.

**To complete A3 as a genuine Tier 3**, raise the key limit at
openrouter.ai/settings/keys, then either resume the GLM worker against
`tier3/A3/glm` (its `transfers/` package and `transfer_decisions.go` already
exist) or re-dispatch it fresh. Then re-run `tier3-compare.sh A3`, adjudicate,
merge, and run the Tier-2 dual-family verification — which is blocked by the
same cap, since `checker-second` is also GLM.

## What the surviving implementation found (recorded now so it is not lost)

`wt-local` scores 12/12 and its report is worth keeping regardless of how the
comparison is eventually resolved:

**It found the one consumer the oracle would have missed.**
`buildCumulativeChartData` (`internal/handlers/dashboard/handlers.go:1044`) is
the only income/expense consumer in the app that is NOT a `FilterByType` call
— it does `if Income { += } else { -= }`, so every transfer leg would have been
**subtracted** from the cumulative cash-flow line, and a paired transfer
subtracted twice. Oracle check 6 asserts on `metrics.Calculate`, which was
already correct, so the oracle would have passed a build with this bug in it.
Ruling 2026-08-16f says assert on a consumer; this is the reminder that there
can be more than one, and the worker enumerated the rest explicitly rather
than stopping at the first.

**Its mutation test reproduced ruling 2026-08-16f verbatim.** Removing only
the `Transfer` skip from `ClassifyTransactions` still logged
`Classified 3 transactions as transfers (2 paired, 1 external)` while
`TotalIncome` became 2012.50 and `TotalExpenses` 3584.12 — data relabelled
correctly, consumers unchanged, no error anywhere.

**Its fixture is deliberately hostile:** the credit leg is described
`TRANSFER IN FROM SCHWAB`, which hits `IncomeKeywords`, so a leak lands in
Total Income rather than quietly cancelling against the debit leg.

**A real bug its own tie test caught:** ambiguity has to be contagious within
the auto-pair pass — a row withdrawn to review must also be withdrawn as a
*candidate*, or it becomes some other row's "unique" candidate and pairs by
default.
