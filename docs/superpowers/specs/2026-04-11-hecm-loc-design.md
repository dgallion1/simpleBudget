# HECM Line of Credit — Buffer Asset Design (WIP)

Status: **In Progress** — brainstorming paused after 3 questions

## Concept

Model a HECM (Home Equity Conversion Mortgage) reverse mortgage line of credit as a **standby buffer asset** in the retirement projection. The LOC sits outside the portfolio and is drawn strategically during market downturns to avoid selling equities at a loss, letting the portfolio recover.

This is the "coordinated strategy" from Pfau/Salter research on standby reverse mortgages.

## Decisions Made

### 1. Draw trigger: Coordinated strategy (C)

Draw from HECM during down markets (when portfolio is below its high-water mark), use portfolio in up markets. This is the classic standby reverse mortgage approach — not always-on, not purely decline-triggered.

### 2. Setup inputs: Hybrid (C)

User provides the LOC amount directly (common path for those who have quotes). An optional estimator helps users who don't know their LOC — takes home value + age to approximate the principal limit using HUD factor tables.

### 3. Guardrails interaction: TBD

Open question with three options:
- **A. Independent** — guardrails and HECM don't coordinate; both could trigger simultaneously
- **B. HECM first, then guardrails** — draw HECM to cover gap; only cut spending if LOC exhausted
- **C. Guardrails first, then HECM** — cut spending first, then use HECM for remaining gap

## Key Architecture Notes (from codebase exploration)

- Follows existing feature config pattern: pointer struct on `WhatIfSettings` with `Enabled` flag
- Integration point: before portfolio withdrawal in `executePortfolioCashFlowWithTaxableState()` (calculator.go ~line 780)
- Withdrawal priority currently: RMD → Taxable → Roth → Tax-deferred (with penalty)
- HECM would be the first explicit buffer asset outside the portfolio structure
- LOC balance grows over time at the effective rate (independent of housing market)
- Non-recourse: loan balance capped at home value at settlement
- Draws are not taxable income — no MAGI/tax impact

## Remaining Questions

- Guardrails interaction model (A/B/C above)
- LOC growth rate modeling (fixed vs. variable, how to handle MIP)
- Repayment tracking (for estate value / net worth display)
- What to show in the UI (LOC balance over time, draws per year, cumulative usage)
- Whether to show a "with HECM vs. without HECM" comparison
- How HECM affects Monte Carlo / failure analysis
