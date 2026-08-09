# Engine assumptions and known limitations

These are properties of the projection engine, not of any one scenario. A
recommendation that depends on something in this list is not supported by the
model and should say so.

- **No mortality modeling.** Both members are assumed alive for the full
  horizon. There is no survivor's penalty: no drop to single-filer brackets, no
  loss of the smaller Social Security benefit.
- **Filing status is frozen** for the whole projection.
- **Tax-deferred savings are one household pool** driven by the older member's
  age for both the RMD start year and the life-expectancy divisor. Account
  ownership is not modeled, so a household whose tax-deferred balance belongs to
  the younger spouse will see RMDs start earlier and larger than reality.
- **IRMAA is annual.** Eligibility turns on the plan anniversary rather than the
  birthday, and a mid-year Medicare start is billed for the whole year.
- **IRMAA surcharge dollars** grow at an assumed 5.5% Medicare per-capita cost
  rate; the MAGI thresholds grow at the plan's CPI assumption.
- **No QCDs, no tax-exempt municipal interest, no itemized deductions, no
  enhanced senior deduction.**
- **The reported marginal rate is the statutory bracket.** It excludes the §86
  Social Security phase-in and the IRMAA cliff, so it is not the rate a
  conversion decision actually turns on.
- **Lifetime tax figures exclude IRMAA.**
- **Monte Carlo is stochastic and auto-seeded**, so success rates differ
  slightly between two runs of the same scenario. `run_scenario` therefore omits
  Monte Carlo entirely; only `get_analysis` reports it.
