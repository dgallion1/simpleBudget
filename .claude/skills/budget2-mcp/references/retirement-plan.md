# Retirement plan — "will I be OK?"

The what-if engine projects the saved retirement plan forward. Five reads,
one write. A `whatif://assumptions` MCP resource describes what the engine
does **not** model — read it before making claims about what a projection
proves.

## list_scenarios (read)

The saved what-if scenarios with a one-line summary of each. Scenario
filenames from here are what every other planner tool's `scenario` param
takes; omitting `scenario` always means the active scenario.

## get_analysis (read)

The full analysis for a scenario: headline balances, per-year projection,
and the derived figures the what-if page shows.

Params: `scenario` (optional).

## get_months (read)

Month-by-month projection detail for an inclusive month range — for
explaining *why* a year looks the way it does (which flows hit in which
month).

Params: `from_month`, `to_month` (0-based, inclusive, at most 120 months per
call), `scenario`.

## run_scenario (read — nothing is saved)

Re-runs a scenario with changed assumptions and returns the resulting
analysis. This is the "what if" tool: use it to answer hypotheticals without
touching the plan. `overrides` changes only the fields you pass; omitted
fields keep the scenario's value.

Params: `scenario`, `overrides`.

### Spending phases multiply living expenses — the #1 "numbers don't match" trap

`monthly_living_expenses` (in the scenario file, the UI slider, and the
`overrides` field) is the **pre-phase base**. When the scenario has
`spending_phase_config.enabled`, every figure the engine reports —
`get_analysis`'s `budget.monthly_expenses`, the what-if page's Monthly
Budget Analysis, the dashboard's budget **target** — uses base × the
active phase multiplier (e.g. Go-Go ×1.1 now, Slow-Go ×0.9 at 70, keyed
to `phase_age_reference`). So a $7,386 base legitimately reports as
$8,124.60/mo of living expense today. Before telling the user two
living-expense numbers disagree, check the phase config; before setting
`monthly_living_expenses` via `run_scenario`/`apply_changes`, remember the
engine will spend multiplier × your value, not your value. (Since
2026-08-29 the UI breaks this out — sub-rows in the budget panel, a note
under the slider, provenance on the dashboard target — quote those rather
than re-deriving.)

### overrides fields worth knowing the gotchas for

Most `overrides` fields are self-explanatory scalars (`monthly_living_expenses`,
`inflation_rate`, claim ages, etc.). Three are less obvious:

- `healthcare_monthly_cost` — the household's **current** total monthly
  healthcare cost in dollars, i.e. what is paid today, not the Medicare-era
  cost. It never touches Medicare cost, ACA-after-employer cost, or any
  inflation field. If the plan has multiple healthcare persons configured,
  this total is **distributed across them proportionally to their existing
  individual costs** (split evenly if those are all currently zero); with no
  healthcare persons configured, it sets the legacy single scalar instead.
- `social_security_fra_benefit` / `spouse_fra_benefit` — the primary/spouse
  **GROSS** monthly Social Security benefit at full retirement age (FRA),
  i.e. *before* Medicare premium deductions and tax withholding — the engine
  computes those itself, so do not pre-net them. Both require the scenario to
  already have a `social_security` configuration (set once in the UI); if it
  doesn't, the call fails with an error naming the missing configuration
  rather than fabricating one.

## open_page (read)

Returns the URL of the what-if page, switching the active scenario first if
you name one. **Call this before `apply_changes`** and give the user the
URL — the page is where they see what the write did.

Params: `scenario` (optional, switches to it).

## apply_changes ✏️ (write → the saved plan)

Saves changed assumptions to the plan and returns the resulting analysis.
Same `overrides` shape as `run_scenario`: omitted fields keep their current
value.

- Before its first write to a scenario in a session, the scenario is
  snapshotted to a `.bak` under `<backup-dir>/mcp-snapshots`. **There is no
  in-app undo** — restoring that file by hand is the only recovery path, so
  tell the user which fields you changed and to what.
- Prefer `run_scenario` for exploration; reach for `apply_changes` only when
  the user has said they want the change kept.
