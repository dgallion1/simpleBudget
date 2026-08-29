# W run — make the spending-phase multiplier visible (user request 2026-08-29)

User-approved scope ("All four", 2026-08-29): the What-If page shows
"Living Expenses $8,124.60/mo" while the slider says $7,400, because the
saved base is $7,386 and the Go-Go spending phase multiplies it by 1.1 with
no visual hint anywhere. Make the multiplier visible at the output (W1), at
the input (W2), and on the dashboard target (W3) — and fix the slider's
step-snapping, which both displays a wrong number and silently re-saves it.

Branch `fix/phase-visibility` off master. Ledger prefix `W`.

Ground truth (verified against live data 2026-08-29):
- `data/settings/whatif.json`: `monthly_living_expenses = 7386`,
  `spending_phase_config.enabled = true`, phases Go-Go ×1.1 (age 0),
  Slow-Go ×0.9 (70), No-Go ×0.8 (85), `phase_age_reference = older`.
- Panel row: `internal/services/retirement/analysis/budget_fit.go:72-78`
  builds `ExpenseBreakdownItem{Name: "Living Expenses", Amount:
  engine.LivingExpensesAtMonth(s, 0)}` = 7386 × 1.1 = 8124.60. Rendered by
  `web/templates/components/whatif/budget-analysis.html:26-29`, which
  already supports an italic `({{.Note}})` suffix.
- Slider: `web/templates/components/whatif/portfolio-settings.html:52-62`,
  `min=0 max=20000 step=100`. The browser snaps a 7386 value attribute to
  7400, so (a) the on-load display can show $7,400 and (b) submitting the
  settings form for ANY change posts `monthly_living_expenses=7400` —
  silently mutating a value the user never touched. A mirror slider lives
  in `quick-adjust.html:102-105`.
- Dashboard target: `internal/handlers/dashboard/handlers.go` →
  `metrics.BudgetTargets` → `phaseAdjustedMonthlyTarget`
  (`internal/services/metrics/metrics.go:60`) — already phase-aware,
  returns only the float. Card: `web/templates/components/kpis.html`.

House invariants: `.swarm/critical.globs` — none of these files fall under
it (`retirement/engine/**` is critical; `retirement/analysis/**` is not).
Do not change engine math anywhere in this run; it is display-only plus the
W2 round-trip fix.

---

## W1 — break the phase math out in Monthly Budget Analysis

Tier 2. Checks: `tests,a11y,second`.

When spending phases are enabled AND the month-0 multiplier ≠ 1.0, the
Living Expenses row gets two indented sub-rows, mirroring the existing
"Includes State Tax" sub-row styling in the same template:

```
Living Expenses                    $8,124.60/mo
    Base (slider setting)          $7,386.00/mo
    Go-Go phase ×1.1               +$738.60/mo
```

Requirements:
1. Extend `models.ExpenseBreakdownItem` with an optional
   `SubItems []ExpenseBreakdownItem` (or equivalent minimal shape) and
   render them indented in `budget-analysis.html` with the same classes the
   taxes sub-rows use. Empty/nil renders nothing — every other breakdown
   row must be byte-identical in output.
2. Populate sub-items in `budget_fit.go` only for the Living Expenses row,
   only when the multiplier ≠ 1.0: base = `s.MonthlyLivingExpenses`,
   adjustment = Amount − base (signed: a 0.9 phase renders "−$…"), label =
   "<phase name> phase ×<multiplier>" using the phase active at month 0
   (`s.GetSpendingMultiplier(s.GetPhaseReferenceAge(0))` — reuse whatever
   the file already calls; there is a phase-name accessor nearby, find it
   rather than re-deriving).
3. The sum invariant must hold to the cent: base + adjustment == Amount.
4. When `spending_decline_rate` (not phases) adjusts month-0 spending,
   do NOT invent a breakdown — sub-rows are phase-only in this task; the
   row total already reflects whatever the engine does.
5. Tests in `analysis` package: sub-items present with correct figures for
   an enabled-phases scenario (use 7386/1.1 as the fixture values), absent
   when phases disabled, absent when multiplier == 1.0, signed correctly
   for a <1.0 phase. Template rendering covered by whatever harness the
   package already uses for budget-analysis (if none exists, a handler-level
   test asserting the rendered HTML contains the sub-row labels).

## W2 — the input side: live note + kill the snap trap

Tier 2. Checks: `tests,a11y,second`.

Part A — note under the slider (`portfolio-settings.html`), shown only when
phases are enabled and the current multiplier ≠ 1.0, directly under the
existing "In today's dollars…" caption:

> Engine spends $8,124.60/mo now (Go-Go ×1.1); ×0.9 from age 70.

1. Server-rendered initial text (handler passes current phase name,
   multiplier, engine month-0 living expense, and the next phase transition
   if any: "×<mult> from age <age>"; omit that clause when no later phase).
2. Live update: dragging the slider updates the dollar figure
   (value × current multiplier) via the existing `updateSpendingPreview`
   / oninput path — put the multiplier in a data attribute; no new
   endpoint. The phase name/multiplier text does not change while dragging.
3. Hidden entirely when phases are disabled or multiplier == 1.0.

Part B — snap trap. Requirements, stated observably:
1. On load, the value displayed under the slider equals the SAVED value to
   the dollar ($7,386, not $7,400) — including after any quick-adjust JS
   init that currently syncs displays from the (snapped) input value.
2. Submitting the settings form without touching the slider round-trips the
   saved value unchanged: load a plan with 7386, apply an unrelated change,
   the file still holds 7386. (Recommended mechanism: the range input keeps
   step=100 for drag feel, but the submitted value comes from a hidden
   input initialized to the exact saved value and updated only by an actual
   `input` event on the slider. Equivalent mechanisms fine.)
3. Dragging still snaps to $100 increments and saves what was displayed.
4. Apply the same fix to the quick-adjust mirror slider — the two inputs
   must not fight (quick-adjust sync must propagate the exact value, not
   the snapped one, when untouched).
5. Handler-level test proving the round-trip invariant (post the settings
   form built from a 7386 plan without a slider event — asserting the
   persisted value stays 7386). JS behavior asserted as far as the
   package's existing test conventions allow; where they can't reach,
   document the manual check in the manifest task notes.

## W3 — dashboard target provenance

Tier 1. Checks: `tests,a11y`.

The Monthly Living Expenses card's "Target $8,124.60" gets provenance:
1. `metrics` exposes the pieces (base, weighted multiplier over the range,
   phase name for the range start — extend `BudgetTargets` or add a
   sibling; do not duplicate the phase-walk logic).
2. `kpis.html`: the Target text gets a `title` attribute AND an
   `aria-label` (or visually-hidden text) reading e.g.
   "Target from What-If plan: $7,386 base × 1.1 (Go-Go phase)". When phases
   are disabled or multiplier == 1.0 the attribute is absent (target
   simply equals the plan value, saying so adds noise).
3. When the selected dashboard range straddles a phase transition the
   multiplier shown is the weighted average `phaseAdjustedMonthlyTarget`
   already computes — label it "avg ×1.05" in that case.
4. Tests: provenance struct correctness for enabled/disabled/straddling
   ranges; template shows/hides the attribute per rule 2.

---

Acceptance for every task: `go build ./... && go vet ./... && go test
./...` green; manifests `.swarm/manifests/W<n>.<attempt>.files`; workers
never commit. `checker-a11y` runs on W1/W2/W3 because all three touch
markup (contrast of new sub-row/note text, aria on the tooltip).

## W4 — spendable-headroom callout in the budget banner (user request, approved "Both, explained" 2026-08-29)

Tier 2. Checks: `tests,a11y,second`. DISPATCH ONLY AFTER W3's worker is done
— shared files (dashboard handler, metrics, kpis/banner templates).

User goal: "see that current spend is less than budget and how much the
underage has accumulated, so I know if I can spend extra." The number
exists (BUDGET STATUS banner: "$9,660.18 under budget over 7.8 months") but
it conflates two buckets: on the live data, LIVING is $1,804.41 OVER its
target while HEALTHCARE is $11,464.60 UNDER — the cushion is entirely
healthcare premiums that never materialized. The display must not tell the
user they underspent when they didn't.

Required behavior — the existing banner gains, under its headline:
1. A decomposition line: "Living: $1,804.41 over · Healthcare: $11,464.60
   under" (signed per bucket, dollars as the banner already formats them).
   Data already exists in `models.DashboardMetrics` (living and healthcare
   cumulative deltas) — no new arithmetic in templates.
2. A plain-English verdict sentence, exactly one of:
   - both buckets at-or-under target: "You could spend up to $<total>
     extra this period and stay on plan."
   - total under but living over (the live case): "Your $<total> of room
     comes from healthcare running under plan — living spending is already
     $<living-over> over its target."
   - total over: "No headroom — $<total-over> over plan for this period."
   Boundary: a bucket within $1 of target counts as at-target, not over.
3. Applies to the selected dashboard date range, like the banner headline;
   no forward-projection ("per remaining month") claims.
4. When no budget target is configured (metrics sentinel 0), the banner is
   byte-identical to today — no headroom text at all.
5. Colors: reuse the banner's existing green/amber/red token classes only;
   the verdict sentence inherits the banner's current severity coloring
   (verdict.go thresholds unchanged).
6. Tests: sentence selection for all three cases plus the $1 boundary and
   the no-target case, at handler/render level per existing dashboard test
   conventions; decomposition figures asserted against a fixture ledger.

## Rulings

- **2026-08-29a** — a threshold applied to a figure (the W4 $1 dead-band)
  must live in ONE classification source consumed by every surface that
  renders the same figure. The spec put the dead-band only in the banner
  while the KPI card kept bare sign comparisons; checker-second showed the
  same $0.60 delta rendered "on target" and red-"over" in one view. Spec
  defect, lead's. Attempt 2 wires the card's per-bucket rows to the shared
  classification.
- **2026-08-29b** — a "sum must hold" display requirement means the
  RENDERED strings, not the floats. W1's guard was a tautology
  (adjustment defined as total−base) and independent %.2f formatting broke
  the displayed identity on fractional-cent bases. Attempt 2 computes the
  adjustment from cent-rounded operands and tests assert on rendered
  output with a fractional-cent fixture.

## Attempt 3 (tier 3) — W1, W2, W4

Escalated by the gate (two-consecutive-fails). Oracles written and validated
at both ends 2026-08-29: `.swarm/tier3/W{1,2,4}/accept.sh`. The oracles are
the authoritative acceptance statement for attempt 3; the blocks below are
the fix intent.

- **W1**: one rounding path for all three displayed figures — extract
  integer cents from the SAME decimal rounding `%.2f` performs (format then
  parse), adjustment = totalCents − baseCents, sub-item amounts =
  cents/100. Row Amount stays raw.
- **W2**: one whole-dollar rule (half-away-from-zero, commas) — Go template
  func `formatWholeDollars` for every server-rendered whole-dollar string
  derived from MonthlyLivingExpenses (both aria-valuetexts, both display
  spans, phase-dollar labels in quick-adjust.html AND spending-phases.html);
  a single JS `formatWholeDollars` in quick-adjust-scripts.html used by all
  JS whole-dollar recomputations (no bare `.toLocaleString()` left in
  portfolio-settings.html). Plus the two structural gaps checkers proved:
  a quick-adjust mirror drag must also update the primary range's `.value`
  (thumb + implicit aria-valuenow parity), and the phase-dollar JS +
  updateSpendingPreview must read the canonical exact value, not the
  snapped range value.
- **W4**: card bucket rows gated on the verdict's `Configured` flags (no
  phantom $0.00 rows); on-target row class `text-gray-600
  dark:text-gray-400`; `room <= 0` clamp so "$-0.00" is unreachable; fix the
  vacuous byte-identity test anchor (assert on `</div>\n    </div>`, the
  level where the attempt-1 artifact actually sat).

- **2026-08-29c (user ruling)** — W2 and W4 hit the three-attempt hard stop;
  the user explicitly reopened both for ONE narrowly-scoped attempt 4:
  W2 = locale-pin every JS dollar formatter ('en-US' or equivalent fixed
  locale; includes the phase-note call and rate-assumptions.html's inline
  expression) + oracle checks forbidding locale-less toLocaleString + the
  missing spending-phases phase-dollar regression guard;
  W4 = the Budget card's container tint AND headline consume the shared
  BudgetVerdict classification (closing the last two independent
  classifiers of CombinedCumulativeDelta — an inconsistency that pre-exists
  on master) + the five kpis.html trim markers. Nothing else.
- **2026-08-29d (judge panel, 3-0 OVERRULE)** — W4 attempt 4's adversarial
  FAIL (Budget-card sparkline colors "the same figure" bare-sign) was set
  aside by all three judges: the later, specific user ruling 2026-08-29c
  ("Nothing else") governs over 29a's general language; charts.js has zero
  diff on the branch and is in no manifest; and judge-claude showed the
  FAIL's factual premise wrong — the sparkline tail is a DIFFERENT basis
  (whole transaction-months capped at six) from CombinedCumulativeDelta
  (fractional MonthsInRange), crossing zero at ~+$375 on live-scale ranges.
  Re-characterized defect (pre-existing, real, bigger than the dead band):
  the Budget sparkline can contradict its own card headline by hundreds of
  dollars either way — its own future task on metrics.go/charts.js, not a
  fifth W4 attempt.
