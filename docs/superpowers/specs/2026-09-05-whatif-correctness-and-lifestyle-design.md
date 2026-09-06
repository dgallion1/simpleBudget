# What-if correctness and full-lifestyle outcomes — review draft

**Status:** Implementation authorized by the user after review; the corrected companion plan controls the bounded tax-classification remedy and separate withdrawal-delay diagnostic.
**User direction:** Full planned lifestyle is the target; spending circuit breakers are the fallback.
**Companion plan:** `docs/superpowers/plans/2026-09-05-whatif-correctness-and-lifestyle.md`.

## Problem and verified evidence

The September 5 review reproduced a Monte Carlo defect: spending and health emergency amounts are divided by twelve, charged in the year-boundary month, and cleared in other months. A fixed $12,000 event reduced a one-year all-Roth/cash ending portfolio by $1,031.78, including lost growth, for each emergency type. Existing tests across eight what-if/retirement packages passed. A temporary independent diagnostic failed as expected.

The review also established misleading interpretation risks: a green verdict at 70% simulated survival or without simulation results; future RMD-driven surplus presented alongside an unexplained zero additional withdrawal rate; a cash-buffer heuristic described as safe; rounded search limits presented as exact failure thresholds; and clipped mobile chart controls/incomplete tab semantics.

Full evidence is archived at `.impeccable/critique/2026-09-05T18-50-04Z__web-templates-components-whatif.md`. The durable plan does not depend on that untracked archive: the reproduction and acceptance criteria are repeated in the companion plan.

## Chosen approach for review

Ship three independently reviewable phases: calculation integrity; lifestyle and fallback reporting; presentation/accessibility. Preserve the Go engine/analysis/handler separation and incumbent UI. Instrument existing simulations rather than introduce a second retirement model.

Alternatives considered:

1. Copy-only corrections: inexpensive but leaves the shock defect and missing cut statistics. Insufficient.
2. Correct the defect and report the existing strategy honestly: recommended scope below.
3. Replace guardrails and build a cash-reserve optimizer: substantially broader, changes planning policy, and is outside this campaign.

## Requirements

1. Charge each sampled emergency in full once at the existing event month. Preserve the existing event probabilities, ranges, random draw order, and shock timing convention. Do not silently redesign the stochastic model.
2. Do not describe the current buffer formula as safe, sufficient, optimized, or simulation-tested. Retain it only as a collapsed, clearly labeled arithmetic illustration with its exclusions. Building a calibrated reserve strategy is a separate project.
3. Describe the base projection separately from simulated outcomes. A deterministic path is not the Monte Carlo median.
4. Partition completed simulation runs into: funded without below-plan living-expense cuts; funded with such cuts; funding shortfall. These are observed outcomes under the configured strategy, not causal claims about whether cuts were necessary.
5. Show cut frequency, depth, total duration, longest continuous duration, and whether cuts are ongoing at the end of observed simulation. State denominators and duration censoring explicitly.
6. Full lifestyle means the configured spending plan, including user-selected age phases and expense schedules. A scheduled phase reduction is not a circuit-breaker cut. A reduction from an earlier raise that stays at or above the planned living-expense baseline is not a below-plan cut.
7. Existing guardrails affect the living-expense line. They do not constitute a separately protected essentials budget and do not automatically apply to healthcare, property tax, or all additional expenses. State this beside guardrail settings and outcomes. Keep current strategy semantics unchanged.
8. Keep today's cash-flow funding gap visible. RMDs are portfolio distributions, not independent income. Label additional withdrawal rate explicitly; show actual projected portfolio outflow separately with a defined denominator and period.
9. Do not invent a new acceptable failure-probability threshold. Until a risk target is agreed, show numerical outcome shares with neutral aggregate styling. Use amber for a base projection with cuts, red for a base projection shortfall, and conditional wording for a funded base projection. Missing/pending metrics are never zero risk.
10. Label threshold results approximate and bounded. A search endpoint at which the portfolio survives is a tested bound, not a proven failure point. State one-variable-at-a-time scope and nominal/today's-dollar bases.
11. Make chart controls readable at 390px and tabs usable with standard keyboard and assistive-technology semantics. Preserve per-scenario tab state and partial-refresh behavior.
12. Verify using copied data only. Do not alter household settings, guardrail thresholds, real ledger files, or real backups.

## Scope boundaries

No tax-law overhaul, full Guyton–Klinger implementation, reserve optimizer, new essentials classification, redesign, publishing, or automatic commits. Existing seeded Social Security and tax-strategy comparisons must remain internally consistent after the shock correction. A changed simulation success rate is an expected consequence of correcting the model, not grounds to weaken a regression test.

## Review decisions

- Confirm one-time full shock charging at the existing year-boundary event month.
- Confirm that clearly downgrading the buffer to an illustration is the first-release remedy; a tested reserve recommendation requires a separate specification.
- Confirm observed lifestyle outcome categories without an additional paired Monte Carlo counterfactual. The UI must say “funded with cuts,” not “funded only because of cuts.”
- Confirm neutral aggregate risk styling until the user chooses a risk target.
- Confirm cut depth is reported for the living-expense budget, with dollar impact also shown in today's dollars.

These choices were accepted for implementation in the user’s request to update the plan and start building.

Execution evidence: a withdrawal-delay month can have unpaid spending without triggering the legacy depletion flag. Funding classifications must inspect actual observed shortfall. Add observational metadata only; do not introduce debt carry-forward or change withdrawal policy. A run with any unpaid spending is a funding-shortfall outcome even if the existing Survives flag remains true. Label the legacy metric as avoiding modeled depletion and explain why it can differ from funded lifestyle outcomes.
