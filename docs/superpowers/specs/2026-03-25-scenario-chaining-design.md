# Scenario Chaining Design

## Problem

Users want to model retirement projections that span multiple life phases with different financial parameters. For example: live off a pension and taxable account from age 60-70, then start Social Security and draw down the 401k from 70 onward. Currently, each scenario is fully independent — there's no way to chain them so one runs first and another takes over at a specified age.

## Solution

Add scenario chaining to the existing What-If system. Any scenario can define an ordered chain of other scenarios with transition ages. The projection engine runs the primary scenario's settings, then swaps to the next scenario's settings at each transition age while preserving account balances.

## Data Model

### New Types

```go
// ScenarioChainLink references a scenario to transition to at a given age
type ScenarioChainLink struct {
    ScenarioFilename string `json:"scenario_filename"` // e.g. "whatif_post-ss.json"
    TransitionAge    int    `json:"transition_age"`     // Age when this scenario's settings take effect
}
```

### WhatIfSettings Addition

```go
// Ordered list of scenarios to chain after the primary scenario
ScenarioChain []ScenarioChainLink `json:"scenario_chain,omitempty"`
```

When `ScenarioChain` is nil or empty, behavior is unchanged. The chain is stored in the primary scenario's JSON file alongside all other settings.

## Projection Engine Changes

Location: `calculator.go`, `RunProjection()` method.

### Pre-Resolution of Chained Settings

The `Calculator` is a pure computation struct with no I/O dependencies. It must not load files. Instead, the handler layer pre-resolves all chained scenario settings before creating the Calculator.

**New field on Calculator:**

```go
type Calculator struct {
    Settings      *models.WhatIfSettings
    ChainSettings []*models.WhatIfSettings // Pre-loaded settings for each chain link, ordered by transition age
}
```

The handler calls `SettingsManager.LoadScenarioSettings(filename)` for each chain link and passes the resolved slice into `NewCalculator`. This keeps Calculator free of I/O and fully testable with in-memory settings.

### Projection Loop

At year boundaries (the existing `if m%12 == 0` block), after computing `currentYear`:

1. Compute the projected age: `s.CurrentAge + currentYear`
2. Check if the next pending chain transition's `TransitionAge` has been reached
3. If so, swap `c.Settings` to the corresponding pre-loaded `ChainSettings[i]`
4. **Preserve** from the running state (do NOT overwrite):
   - `taxDeferredBalance`, `rothBalance`, `taxableBalance`
   - `cumulativeInflation`
   - `depletionMonth`, `longevityYears`
5. **Overwrite on chained settings before use**: Copy the primary scenario's `CurrentAge`, `SpouseAge`, and `PhaseAgeReference` onto the chained settings object. This ensures `GetPhaseReferenceAge()` and all other age-dependent calculations continue using the primary scenario's age baseline.
6. **Recalculate** from the new settings:
   - `currentLivingExpenses` (using new base expenses + cumulative inflation + phase multiplier)
   - `monthlyRMD` (using new settings but existing tax-deferred balance)
   - Per-account asset allocation variables (`tdStock`, `tdBond`, `tdCash`, `rothStock`, `rothBond`, `rothCash`, `taxStock`, `taxBond`, `taxCash`) — refresh from the new settings' `Get*Allocation()` methods. These are cached as local variables in both `RunProjection` and `runSingleMonteCarloSimulation` and must be explicitly reassigned at transitions.
   - Investment return rates (new allocation/override via `InvestmentReturn`)
   - Healthcare costs (new person configs)
   - Income and expense sources (new lists, with time offset rebasing — see below)
   - `RothConversion` config — the chained scenario's Roth conversion strategy takes effect
   - `TaxConfig` — the chained scenario's tax config takes effect
   - `BigTicketItems` — the chained scenario's items take effect, with `item.Year` rebased by subtracting `transitionYear` (same logic as income/expense rebasing)
   - `TaxDeferredDelayYears` — ignored from chained scenarios. Once the projection is underway, this delay has either already expired or is still counting down from the primary scenario. A chained scenario cannot restart the delay.
7. **Ignore** from the chained scenario:
   - `CurrentAge`, `SpouseAge`, `PhaseAgeReference` — overwritten with primary's values (step 5)
   - `ProjectionYears` — the original scenario's projection duration governs
   - `PortfolioValue` and account split percentages — balances continue from current state
   - `ScenarioChain` — nested chains are not followed (only the primary scenario's chain is used)
   - `TaxDeferredDelayYears` — see step 6

Chain links are sorted by `TransitionAge` at projection start. The engine tracks the index of the next pending transition.

### Income/Expense Source Time Rebasing

Income sources use `StartMonth` and expense sources use `StartYear` as offsets from projection start (month 0 / year 0). When a chained scenario's sources are swapped in mid-projection, these offsets must be rebased so they are relative to the transition point.

At transition time (transition month `T`):
- For each income source: `source.StartMonth = max(0, source.StartMonth - T)`. If the source's original `StartMonth` < `T`, it means the source was meant to be active from the start of that scenario, so it becomes active immediately (offset 0). End months are rebased similarly.
- For each expense source: `source.StartYear = max(0, source.StartYear - transitionYear)`. Same logic.
- COLA/inflation adjustments: Sources that become immediately active at the transition should NOT have accumulated inflation from month 0. The engine already applies inflation based on the number of months since `StartMonth`, so rebasing the offset to 0 means inflation accrues only from the transition point forward. This is the correct behavior.

This rebasing is done on copies of the chained settings (the pre-loaded `ChainSettings` are not mutated — clone them before rebasing).

### Monte Carlo Simulation

`runSingleMonteCarloSimulation` has its own complete projection loop (independent of `RunProjection`). The chain transition logic must be duplicated in both loops — same transition checks, same settings swap, same rebasing, same allocation variable refresh. Both loops must produce consistent behavior for chained scenarios.

Each simulation run applies the same chain transitions at the same ages.

**Asset return generation**: The existing `generateAssetReturns()` pre-generates per-asset-class returns (stock, bond, cash) for the entire projection using historical distributions. These raw per-class returns are allocation-independent. When computing the blended portfolio return for a given month, the engine uses whichever scenario's asset allocation is active at that point. This means:
- Pre-generate stock/bond/cash return series for the full projection (unchanged)
- At each month, compute the blended return using the current scenario's `StockPercent`/`CashPercent` (or per-account allocations)
- When a chain transition changes the allocation, the blended return changes but the underlying asset-class returns remain the same for that simulation path

## Settings Manager Changes

### New Method

```go
// LoadScenarioSettings loads a scenario's settings without switching the active scenario.
// This is a read-only operation used by the projection engine during chain transitions.
func (sm *SettingsManager) LoadScenarioSettings(filename string) (*models.WhatIfSettings, error)
```

This reads and returns the `WhatIfSettings` from the specified scenario JSON file. It does not change which scenario is "active."

### Chain Validation

When saving settings that include a `ScenarioChain`, validate:

1. **Ascending ages**: Each transition age must be strictly greater than the previous
2. **File existence**: Each referenced scenario file must exist
3. **No self-reference**: A scenario cannot chain to itself
4. **Age bounds**: Transition ages must be greater than or equal to `CurrentAge` (a transition at `CurrentAge` means "start with this scenario immediately") and strictly less than `CurrentAge + ProjectionYears` (a transition at the last year would never fire)

Note: Circular chains (A chains to B, B chains to A) are not a runtime concern since nested chains are ignored. Validation only checks self-reference.

Validation errors are returned to the UI for display.

## UI Changes

### New Card: Scenario Chain

Add a collapsible card to the left column of the What-If page, placed after the existing settings cards (below "Additional Expenses"). The card contains:

**Empty state** (no chain links):
- Header: "Scenario Chain"
- Brief text: "Chain other scenarios to run sequentially"
- "+ Add Step" button

**With chain links** (ordered list):
- Each link shows:
  - Scenario dropdown (populated from `ListScenarios()`, excluding the current scenario)
  - "at age" number input for `TransitionAge`
  - Remove button (x icon)
- Links displayed in age order
- "+ Add Step" button at the bottom

**Behavior:**
- Adding/changing/removing a link posts to the server via HTMX
- Server saves the updated chain, re-runs the projection, and returns updated results
- The scenario dropdown reuses the same scenario list already passed to the template

### Projection Chart

No chart changes needed initially. The projection chart already renders month-by-month data — chained scenarios will produce a continuous series. A future enhancement could add vertical markers at transition ages, but that's out of scope.

## Handler Routes

Two new endpoints, registered alongside existing scenario routes:

```go
r.Post("/whatif/chain", handleUpdateChain)           // Save chain config, re-run analysis
r.Delete("/whatif/chain/{index}", handleDeleteChainLink) // Remove a link by index, re-run analysis
```

### `POST /whatif/chain`

Accepts form data with parallel arrays:
- `chain_scenario[]` — scenario filenames
- `chain_age[]` — transition ages

Parses into `[]ScenarioChainLink`, validates, saves to current scenario settings, invalidates cache, re-renders results.

### `DELETE /whatif/chain/{index}`

Removes the chain link at the given index, saves, invalidates cache, re-renders results.

## Edge Cases

- **Deleted scenario in chain**: If a referenced scenario file is deleted, the chain validation catches it on next save. During projection, if a file is missing, skip that transition and log a warning.
- **Empty chain**: No-op. Projection runs as it does today.
- **Transition at current age**: The chained scenario takes effect immediately (year 0). This is valid — it means "start with this other scenario's settings from the beginning."
- **Multiple transitions in same year**: Not allowed. Validation requires strictly ascending ages.
- **Nested chains ignored**: If scenario B has its own chain, it's ignored when B is loaded as a chain link. Only the primary scenario's chain drives transitions.

## Testing Strategy

- **Unit tests**: `calculator_test.go` — test projection with 2-link and 3-link chains, verify balances carry over, settings swap correctly
- **Validation tests**: `settings_crud_test.go` — test chain validation rules (circular, self-ref, ascending ages, missing files)
- **Integration**: Manual testing via the UI — create two scenarios with different settings, chain them, verify the projection chart shows the expected transition
