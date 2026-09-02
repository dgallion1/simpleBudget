# CB2 — close the refund-dominant defect class: the five sibling abs sites

Follow-up to CB1 (PR #80), authorized by the user 2026-09-02 ("run CB2").
Contract decisions applied from precedent rather than fresh sign-off:
KD ruling (month figures are SIGNED; refund-dominant months negative;
totals = sum of signed rows) and CB1's signed-negated-net contract.

## The five sites (post-#80 line numbers may shift; identify by pattern)
- internal/services/metrics/metrics.go trend loop:
  expAmt / hcAmt / livingMonth = math.Abs(<month bucket>.SumAmount())
  feeding ExpensesTrend, HealthcareTrend, LivingExpensesTrend — and the
  DERIVED savingsTrend = incAmt - expAmt, which under abs SUBTRACTS a
  refund month instead of adding it (double distortion).
- internal/handlers/dashboard/handlers.go budget-vs-actual bar traces:
  hcAmt / livingMonth = math.Abs(<month bucket>.SumAmount()) feeding
  livingValues / healthcareValues.

## Task CB2 — Tier 3, checks: tests,second
Fix: all five sites become the signed negated net (-bucket.SumAmount()),
matching CB1's walk contract. savingsTrend needs NO code change — it
derives correctly once expAmt is signed; the spec calls this out so the
worker does not "fix" it twice. Update the SY-2026-08-30d comment blocks
at the living sites (they justify the abs) and the field docs for the
three trend series in models/dashboard.go (signed contract, refund months
negative). Add committed regression tests: a refund-dominant month must
yield a NEGATIVE trend value and a savings value of income PLUS the
refund; two-month fixtures (KD lesson).

## Consumers (enumerated in Phase 0; checker-second re-enumerates)
- kpis.html sparklines (expenses/living/healthcare) via toJSON →
  charts.js renderSparkline (Plotly, invisible auto-scaled axes —
  negatives render as dips; no clamping found).
- Budget-vs-actual bar chart traces via buildBudgetVsActualChartData →
  Plotly bars (negatives render downward).
- NOT consumers: mcpsvc/spend get_trends (CategoryTrend, separate
  computation); KPI card headline numbers (range-level totals, correct).
- CB1's cumulative walk: must be UNCHANGED (its equality test and
  regression test must pass untouched).

## Out of scope
- Range-level math.Abs totals (same as CB1's precondition).
- Any threshold/color rule keyed to trend values (worker reports if one
  exists; changing classification is a spec change — STOP and report).
- charts.js changes: none expected; if negative rendering is broken in
  Plotly config, STOP and report.

## Oracle (.swarm/tier3/CB2/accept.sh) — both-ends validated pre-dispatch
Probe over a two-month fixture with a refund-dominant living month:
A. ExpensesTrend / LivingExpensesTrend value for the refund month is
   exactly the negated net (negative); healthcare analog for an
   hc-refund fixture.
B. savingsTrend for that month == income + |refund net| (the derived
   double-distortion healed).
C. buildBudgetVsActualChartData's living trace for the same fixture
   carries the same signed value (both surfaces move together).
D. CB1 non-regression: the CB1 oracle probe assertions still hold and
   TestChartCumulativeWalk_AgreesWithMetricsCombinedCumulativeBalance +
   TestCalculateMetrics_CombinedCumulativeBalance_RefundDominantMonthEntersAsCredit
   pass by name.
E. Full metrics + dashboard package suites green.

## Acceptance
gate.sh check CB2 exit 0: ORACLE PASS at current attempt + dual-lane
PASS (checker-tests primary; checker-second adversarial with a real-data
differential — expected byte-identical everywhere given zero
refund-dominant months exist today, and that zero-diff must again be
PROVEN correct, not assumed).

## AMENDMENTS from Phase 0 validation (lead, 2026-09-02, pre-dispatch)

### The class is SEVEN sites, not five
Phase-0 grep found two more the CB1 checkers' backlog list missed:
- handlers.go ~524 (handleKPIDetail): per-month expAmt feeding the KPI
  monthly stats — Expenses, DERIVED Savings, DERIVED savings Rate.
- handlers.go ~893 (handleKPIExport): the same arithmetic in CSV export.
Smoking gun: the SAME loop already uses KD-signed monthlyLivingTotals /
monthlyHealthcareTotals (ruling KD-2026-08-30d cited in its comment) two
lines below the abs — two regimes in one function.

### Ruling CB2-2026-09-02a — calibrations that encode the bug
Two committed tests PIN the abs expectation and MUST be updated (value
3000 → -3000 plus their doc comments), not deleted:
- metrics: TestCalculateMetrics_PlanExclusions_RemainderNetsRefundMonthInLivingTrend
- dashboard: TestBuildBudgetVsActualChartData_RemainderNetsRefundLivingEqualsAbsRemainder
Their "out of scope per ruling SY-2026-08-30d" note is SUPERSEDED for
this class by the user-authorized CB2 run (2026-09-02: "run CB2"); the
SY ruling's set-exclusion shape (abs directly on the bucket, never
arithmetic subtraction) remains in force — only the abs-vs-signed part
changes. The remainder test's long NOTE about the master-native walk
invariant break is now FIXED by CB1 — the worker should update that
stale paragraph too.

### Fixture gotcha for committed tests
"refund" (also "rebate", "cashback") are classifier IncomeKeywords — a
CSV/HTTP-level refund fixture must use a description avoiding them
(e.g. "Carnival Cruise Lines" +800) to classify Outflow. Direct
models.Transaction fixtures set TransactionType explicitly.

### Oracle validation record
- FAIL end (pristine 6b7c840): all seven probe failures are the defect
  (500 vs -500, savings 4500 vs 5500), zero harness errors.
- PASS end: prototype = the seven sites signed + ONLY the two named
  calibration updates → ORACLE PASS. Prototype discarded.

## Amendment CB2-c (lead, 2026-09-02, post-worker-report): NINE sites
The worker's point-7 observations found sites 8-9, confirmed in-class
(month-bucket abs) by lead enumeration of ALL math.Abs in both packages:
- 8: handlers.go ~738 handleKPIMonthDetail expenseTotal — the "Total
  Spent" tile, now inconsistent with the KD-signed living/healthcare
  kinds in the SAME handler.
- 9: handlers.go ~1409 buildSpendingTrendChartData monthlyTotals — feeds
  MoM %-change bars; signed curr gives -127.78% on the oracle fixture
  (vs abs -72.22%); the existing prev>0 guard stays (a refund-month BASE
  renders 0% — honest degradation).
Oracle extended (TestCB2Oracle_MonthDetailAndSpendingTrendSigned) and
RE-VALIDATED both ends: fail end fails exactly on the two new
assertions; pass end (two-line prototype, then reverted) ORACLE PASS.

## Classified OUT of CB2 — distinct per-transaction-abs class (CB3
candidates, need their own contracts; do not fix here):
- handlers.go ~383: modal transaction-list total += |amount| per txn.
- handlers.go ~1482: merchantTotals += |amount| (refund inflates a
  merchant's "spend").
- handlers.go ~1554-1556: cumulative cash-flow day walk does
  dayTotal -= |amount| for outflow-typed rows — an outflow-typed REFUND
  SUBTRACTS from cash flow (wrong direction; looks like a real bug worth
  its own run).
- metrics.go ~729 PercentChange's |previous| denominator — %-change
  semantics against signed bases is a design question.

## Checker findings ledger (attempt 1, both lanes PASS)
- checker-tests: nine sites individually mutation-killed by committed
  tests; diff = exactly nine executable lines; derived formulas
  byte-identical. Observations O1-O3 (signed customdata hover; green 0%
  guard case pinned; CB3 cash-flow site).
- checker-second: real-data differential 298/298 byte-identical AND
  refund-dominance disproven per-bucket for all nine sites; tile Total
  == negated sum of modal rows (KD contract extended). NEW backlog:
  internal/services/insights/trends.go has the same abs-over-bucket
  shape (separate package, un-enumerated — CB3 candidate list grows);
  kpi-month-detail.html colors the Total tile unconditionally red
  regardless of sign (pre-existing, inert today).
- Tooling: rtk falsely reports differing files as identical to plain
  diff — checkers should use python3 difflib (checker-tests caveat).
