# Tier 3 divergence report — A3

| worktree | oracle exit |
|----------|-------------|
| wt-glm   | 0 |
| wt-local | 0 |

## No behavioral divergence
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

<!-- Boss: after adjudicating, append a line starting 'RESOLUTION:' -->

## Adjudication

Both implementations pass all 12 oracle checks with byte-identical output. They
are NOT equivalent, and the oracle could not tell them apart — the same shape
as A1, and the second time in this run that a green oracle hid a real
divergence.

**wt-local wins on correctness.** `buildCumulativeChartData`
(`internal/handlers/dashboard/handlers.go:1044`) is the only income/expense
consumer in the application that is not a `TransactionSet.FilterByType` call.
It reads:

```go
if t.TransactionType == models.Income { dayTotal += math.Abs(t.Amount) }
else                                  { dayTotal -= math.Abs(t.Amount) }
```

A `Transfer` row is not `Income`, so it lands in the `else` and is
**subtracted**. wt-glm left this untouched. For the probe's own fixture — a
$2,000 Schwab→USAA move — the debit leg subtracts 2000 and the credit leg
subtracts another 2000, so the dashboard's cumulative cash-flow line is off by
**$4,000 on a transfer whose true net effect is zero**. Silent: no error, no
log line, and every other surface is correct because everything else filters by
type.

wt-local added an explicit skip with a comment naming exactly this reasoning,
and shipped `TestBuildCumulativeChartData_SkipsTransfers` to hold it.

Why the oracle passed both: check 6 asserts on `metrics.Calculate`, which both
implementations handle correctly. Ruling 2026-08-16f required asserting on a
consumer, and this oracle did — but on *one* consumer. The lesson A3 adds is
that a task changing the meaning of a shared field must enumerate **every**
consumer, and an oracle asserting on a single one still leaves room for exactly
this bug. Recorded as ruling 2026-08-16h.

wt-local is also broader where it matters: it carries
`internal/services/metrics/metrics_test.go` and
`internal/handlers/dashboard/handlers_test.go` — regression cover at the two
consumer sites — plus `classifier_test.go`. Its worker enumerated the
consumers it checked and cleared explicitly (explorer totals, insights trends
and recurring, the mcpsvc spend tools, majorexpenses' `!= Outflow` guard,
anomalies, pricecreep, whatif, near-duplicates) rather than stopping at the
first one it fixed.

Its mutation evidence is also stronger: removing only the `Transfer` skip from
`ClassifyTransactions` still logged
`Classified 3 transactions as transfers (2 paired, 1 external)` while
`TotalIncome` became 2012.50 and `TotalExpenses` 3584.12 — data relabelled,
consumers unchanged, reproducing ruling 2026-08-16f verbatim. Its fixture is
deliberately hostile: the credit leg is described `TRANSFER IN FROM SCHWAB`,
which hits `IncomeKeywords`, so a leak lands in Total Income rather than
quietly cancelling against the debit leg.

One genuine design insight from wt-local worth carrying forward: **ambiguity
has to be contagious within the auto-pair pass.** A row withdrawn to the review
queue by a tie must also be withdrawn as a *candidate*, or it becomes some
other row's "unique" candidate and pairs by default. Its own tie test caught
that.

Nothing was grafted from wt-glm. Its distinctive choice was the missing
consumer fix, and that was resolved against it.

Note on process: wt-glm was killed mid-implementation by an API spend cap and
resumed in a later session, so it had one more editing pass than wt-local. That
asymmetry favours wt-glm if anything, and it still missed the consumer.

RESOLUTION: wt-local adopted wholesale, no synthesis. Chosen on correctness: wt-glm leaves buildCumulativeChartData subtracting every transfer leg from cumulative cash flow ($4,000 error on a $2,000 transfer, twice-subtracted for a paired one), while wt-local skips transfers there and ships regression cover at both consumer sites. Both passed the oracle 12/12 and it could not distinguish them.
