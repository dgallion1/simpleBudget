# Engine assumptions and known limitations

These are properties of the projection engine, not of any one scenario. A
recommendation that depends on something in this list is not supported by the
model and should say so.

- **No mortality modeling.** Both members are assumed alive for the full
  horizon. There is no survivor's penalty: no drop to single-filer brackets, no
  loss of the smaller Social Security benefit.
- **Filing status is frozen** for the whole projection.
- **Tax-deferred savings are one household pool.** Account ownership is not
  modeled, so a household whose tax-deferred balance actually belongs to the
  younger spouse will see RMDs start on the wrong schedule. The RMD start year
  is always driven by the older member's age. The life-expectancy divisor is
  usually also keyed to the older member's age alone (the Uniform Lifetime
  Table) — except when the spouse is the sole beneficiary and more than 10 years
  younger, in which case the engine switches to the IRS Joint and Last Survivor Table
  (Table II), whose divisor depends on both ages and produces a smaller RMD
  than the single-age table would.
- **IRMAA eligibility turns on the plan anniversary rather than the birthday,
  and a mid-year Medicare start skips that year's surcharge entirely.** The
  engine only counts someone as Medicare-eligible for IRMAA purposes starting
  from the first full projection year at or after their Medicare start month —
  the partial first year is never billed. IRMAA is therefore understated, not
  overstated, for anyone starting Medicare mid-year.
- **IRMAA surcharge dollars** grow at an assumed 5.5% Medicare per-capita cost
  rate; the MAGI thresholds grow at the plan's CPI assumption.
- **The age-65 additional standard deduction is modeled** (applied per
  qualifying filer, per projection year). What is **not** modeled: the
  temporary 2025-2028 enhanced/bonus senior deduction, QCDs, tax-exempt
  municipal interest, and itemized deductions.
- **No marginal tax rate is exposed by these tools.** `get_analysis` and
  `run_scenario` report effective rates and totals only (`average_effective_rate`
  in the tax section); no per-year or point-in-time marginal bracket is
  returned, so do not state a marginal rate when discussing a scenario.
- **Lifetime tax figures exclude IRMAA.**
- **Monte Carlo is stochastic and auto-seeded**, so success rates differ
  slightly between two runs of the same scenario. `run_scenario` therefore omits
  Monte Carlo entirely; only `get_analysis` reports it.
