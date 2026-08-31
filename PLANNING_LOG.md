# Planning Log — retirement / Roth conversion decisions

Append-only, newest entry last. Every Claude session working on this plan
reads this file FIRST and appends a dated entry for any fact, correction,
or decision established in conversation. Facts here override test fixtures
and stale docs.

---

## Household facts (corrections override anything in testdata/)

- **Birth year: 1958** (user-stated 2026-08-31; the `testdata/settings/whatif.json`
  fixture says 1961-04 — that is synthetic test data, NOT the user).
  - Age 67–68 in 2026; past 59½ long ago.
  - SECURE 2.0 RMD applicable age: **73 → first RMD year 2031**.
  - ⚠ OPEN: verify the live saved settings (`birth_month` in the local
    file-backed whatif settings) say 1958, not 1961. If wrong, every
    projection delays RMDs to 2036 and understates the tax bomb.
- No existing Roth IRA per current plan (`roth_percent: 0`).
  - ⚠ OPEN: confirm with tax accountants whether ANY Roth was ever funded
    in 2021 or earlier. If yes, the qualified-distribution clock is already
    satisfied and the 5-year earnings rule is moot entirely.

## Standing analysis (2026-08-31 session, Roth 5-year rules)

- **Per-conversion 5-year rule (10% penalty): does not apply.** Only for
  under-59½. Large conversions do NOT each carry a lockup at this age;
  converted principal is withdrawable next day, tax- and penalty-free.
- **Qualified-distribution (earnings) 5-year rule: one clock, one time.**
  First-ever Roth funding starts it from Jan 1 of that tax year. First
  conversion in 2026 → all Roth earnings tax-free **Jan 1, 2031**. Later
  conversions never restart it. Before 2031, withdrawals draw basis first
  (Pub 590-B ordering); only earnings withdrawn early would be ordinary
  income (no penalty). Practical impact on the plan: near zero — Roth is
  the last-touched account and basis vastly exceeds any plausible draw.
- **What actually prices the conversions:** IRMAA two-year lookback
  (2026 conversion → 2028 premiums) + bracket fill — see
  `docs/rmd-tax-bomb-gap-analysis-2026-08-08.md` (the accountant report's
  all-in marginal cost framework). Not the 5-year rules.
- **Conversion window is short: 2026–2030** (five tax years before RMDs
  begin at 73 in 2031). This argues for larger annual conversions than a
  born-1961 plan would.
- App engine models the earnings rule exactly
  (`RothQualifiedDistributionClockSatisfied`, basis/earnings split in all
  three projection loops). Set `roth_first_funded_year` in the whatif
  settings to the true first-funding year (2026 if the first conversion is
  the first funding); unset = engine conservatively treats the clock as
  never satisfied.

## How to use this log (instructions for Claude sessions)

1. Read this file before answering any planning question.
2. Append a dated `## YYYY-MM-DD — <topic>` entry when the user states a
   personal fact, corrects one, or makes a decision. Never rewrite history;
   strike through with a correction entry instead.
3. Resolve the ⚠ OPEN items above when the user provides the answer, by
   appending the resolution with its date.
