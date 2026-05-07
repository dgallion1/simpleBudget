# Full Guyton-Klinger guardrails — follow-up ticket

**Status:** Open. Filed during whatif fix campaign PR 8 (F-063); gated on user demand.

## Background

The whatif page exposes a "guardrails" toggle. Pre-2026-05-06 the UI labeled
this as Guyton-Klinger guardrails. The code implements a simpler
portfolio-drop/rise rule. PR 8 of the fix campaign renamed the UI label to
"Drop/rise guardrails (simple)" and updated the help text. This ticket
tracks implementing the actual Guyton-Klinger rules if the user later
requests them.

## Scope

Implement Guyton & Klinger (2006), *Decision Rules and Maximum Initial
Withdrawal Rates*, Journal of Financial Planning, March 2006. Four rules:

1. **Capital Preservation Rule (CPR):** if current withdrawal rate exceeds
   initial rate × 1.20, cut withdrawal by 10%.
2. **Prosperity Rule (PR):** if current withdrawal rate falls below initial
   rate × 0.80, raise withdrawal by 10%.
3. **Inflation Rule:** if last year was a down year, skip the inflation
   raise on withdrawals.
4. **Withdrawal Rule:** even in good years, don't apply full inflation
   raise unless portfolio return exceeded inflation.

The current code's drop/rise rule maps loosely to CPR + PR using portfolio
levels rather than withdrawal rates. The full G-K paper additionally
specifies #3 and #4 (inflation moderation) which the current code lacks.

## Estimated effort

2-3 days of TDD: rule logic in `guardrails.go`, integration tests,
scenario regression to compare new behavior to current simple guardrails,
UI surfacing of the per-rule trigger reasons.

## Trigger to start

Either:
- A user request explicitly asks for actual Guyton-Klinger.
- Telemetry shows non-trivial usage of the simple guardrails toggle (signal
  that users care about this feature).

If neither materializes, the simple guardrails are sufficient — leave them.
