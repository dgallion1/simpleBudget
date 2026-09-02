# CB run — refund-dominant months in the combined cumulative-balance walk

DRAFT for user sign-off (Phase 0). Prefix CB; no ledger collision (checked).

## The defect (MASTER-NATIVE, recorded in NEXT.md SY section)

`metrics.Calculate`'s CombinedCumulativeBalance walk computes each month's
spend as `math.Abs(bucket.SumAmount())` over the month's non-excluded
outflow bucket. For a refund-dominant month — one whose outflow-typed rows
NET POSITIVE (a cruise refund, a returned purchase larger than the month's
spending) — the abs() flips the sign: a net +$500 refund month is charged
as $500 SPENT instead of credited −$500. Consequences:

- The documented invariant on models.DashboardMetrics (last element ==
  −CombinedCumulativeDelta) breaks for any range containing such a month.
- The budget-balance sparkline/chart understates how far ahead of budget
  the user is, by 2×|net refund| per refund-dominant month. Wrong money
  figure on screen.
- SY4 made the dashboard chart walk AGREE with metrics.go; both share the
  wrong assumption (proven identical with planExclusions=nil by both
  lanes' probes, SY4 attempts 3–4).

House precedent: KD ruling — month rows are SIGNED; refund-dominant months
render negative; totals are the sum of signed rows.

## Task CB1 — Tier 3 (money figure, multi-surface; dataloader-adjacent)
`checks: tests,second` — Tier 3 is exempt from the lean experiment.

Fix: the walk's spend term becomes the SIGNED outflow net (credit months
reduce cumulative spend / raise the balance), in ONE shared source consumed
by every surface — the split-classification rule. Surfaces to enumerate
(checker must enumerate, not trust the diff):
1. metrics.go walk (the shared computation).
2. The dashboard chart walk (plan_exclusions_chart_walk_test guards
   chart-vs-metrics equality — must still hold AFTER the fix, i.e. both
   move together).
3. The models/dashboard.go field doc + invariant text (update to state the
   signed contract).
4. Any tooltip/legend copy that says "spent" for a credit month (worker
   reports; content decision escalates to lead if wording is ambiguous).

Out of scope (explicitly): the flat-monthTarget vs day-prorated accrual
difference (separate NEXT.md item, untested today, unchanged); PerMonth/
Cumulative delta bases other than the walk unless the invariant forces
them (if it does, STOP and report — that is a spec change).

## Oracle (.swarm/tier3/CB1/accept.sh) — written and both-ends validated
before dispatch
- Two-month fixture, one refund-dominant (KD lesson: single-month fixtures
  cannot discriminate per-month-abs from signed arithmetic).
- Assert: walk invariant holds (last point == −CombinedCumulativeDelta
  within float noise) WITH the refund-dominant month present.
- Assert on the observable output of every consumer: metrics.Calculate
  result AND the rendered dashboard chart JSON for the same fixture agree
  point-for-point.
- Real-data check: on the live ledger (read-only copy), the walk's points
  change ONLY in months that are refund-dominant; all other points
  byte-identical.
- Fail-end validation: current master must FAIL the invariant check on the
  fixture; pass-end: a throwaway signed-spend prototype must pass, then be
  discarded.

## Acceptance
- `gate.sh check CB1` exit 0 (oracle log ORACLE PASS at current attempt +
  dual-lane PASSes).
- Both existing test suites green: plan_exclusions_chart_walk_test and
  the metrics package (calibrations may need updating — updating a
  calibration that encoded the bug is expected; deleting a guard is not).

## Oracle validation record (lead, 2026-09-02, pre-dispatch)
- FAIL end (pristine master 040166a): probe failed on exactly the defect —
  Feb step 879.8768 vs wanted 1879.8768 (2x the 500 refund) and invariant
  off by exactly 1000; the Jan harness-validity guard did NOT fire.
- PASS end, first attempt: a metrics.go-only signed-spend prototype FAILED
  the oracle at the dashboard chart-walk equality suite — MECHANICAL PROOF
  of the split-classification prediction (handlers.go:1091 replicates the
  walk arithmetic). Prototype extended to both sites → ORACLE PASS.
  Prototype discarded (git checkout); only .swarm files remained.
- Ruling CB-2026-09-02a: the enumeration of consuming surfaces is
  therefore {metrics.go walk, handlers.go:1091 chart walk} PROVEN, plus
  doc surfaces; sibling per-month abs sites (~1051, ~1060) are explicitly
  observation-only for CB1.

## Ruling CB-2026-09-02b (lead, on checker-tests F3)
The spec's oracle section promised a live-ledger differential check;
accept.sh implements only the fixture checks A/B/C. The lead delegated
the real-data differential to checker-second's lane at dispatch (ND1
precedent: checker-second diffed full real-data sets) but failed to
amend this spec — checker-tests F3 caught the drift. DECISION: the
delegation stands (the differential is a one-time verification of THIS
change, not a repeatable acceptance property; the oracle's fixture
checks are the durable part), and CB1's acceptance additionally
requires checker-second's real-data differential to PASS explicitly.
Catch attribution: primary lane, against the LEAD's artifact.

## Checker-tests findings disposition (attempt 1)
- F1 stale "ONE Abs" comment (handlers.go:1026, chart_walk_test.go:94):
  worker touch-up ordered within attempt 1 (comment-only), oracle re-run
  after.
- F2 docs/api/internal/models.md stale on master already: backlog.
- F4 manifest's sibling-site list extended to all five abs sites: worker
  touch-up with F1.
- F5 uncommitted-by-design (lead commits after gate): no action.
