# What-If MCP server — follow-ups

Captured from the `feat/whatif-mcp` execution ledger and its whole-branch review
before that scratch workspace was deleted. Nothing here blocks the branch; the
first section is the only part that is wrong *today* in shipped code.

## 1. Wrong caveats already on master — fix these first

Both were introduced earlier the same day, outside this branch, and both were
caught by reviewing `internal/services/whatifmcp/assumptions.md` against the
engine. The assumptions resource was corrected; these were not.

**`engine/loop_helpers.go`, `MedicareEligibleAdultCountAtYear` (commit `d29f78e`).**
The comment says a mid-year Medicare start is billed for the whole year. It is
the reverse: the function counts a person only when `year*12 >= MedicareStartMonth`,
so a mid-year start **skips** that year and **understates** IRMAA.

**`engine/rmd.go`, `olderBirthYear` (commit `bc97a1c`) and the RMD-card caveat in
`web/templates/components/whatif/rmd.html`.** Both say the older member drives the
life-expectancy divisor. False whenever the spouse-sole-beneficiary Joint Life
Table II applies (`RMDLifeFactor` → `jointLifeFactor(ownerAge, spouseAge)`), where
the divisor depends on **both** ages and is larger. The start year *is* driven by
the older member; only the divisor claim is wrong.

The HTML one is user-facing and currently renders on the RMD card, so it is
telling the reader something untrue about their own plan.

## 2. Spec items the shaped view does not yet expose

The design spec lists these under `get_analysis`; they were not implemented and
the omission was not deliberate (unlike `get_tax_optimizer`, which the spec
defers on purpose).

- **Per-year RMD schedule.** `RMDAnalysis.Projections` exists; `RMDView` is four
  scalars. The tool description was corrected to stop promising a schedule.
- **Per-year tax summary.** `TaxAnalysis.YearlyTaxSummary` exists; `TaxView` omits it.
- **NIIT** per year. `ProjectionYearSummary.NIIT` exists; `YearView` omits it.
- **Depletion year and age** in the headline — only `depletion_month` is exposed,
  so a model must divide by 12 and cannot get the age at all.
- **`RMDAnalysis.UsesJointLifeTable`.** `assumptions.md` explains that the divisor
  switches to Joint Life Table II under certain conditions, but nothing exposes
  whether it actually applied to *this* plan — the model is told about a
  conditional it cannot resolve.

## 3. Data-directory resolution

- **Encryption is detected in the wrong directory.** `cmd/whatif-mcp` passes the
  settings dir to `storage.New`, which looks for the `.encrypted` marker there;
  `cmd/server` passes the data dir, so it finds `data/.encrypted`. For an
  encrypted user the MCP server reads age ciphertext as JSON and reports a parse
  error instead of "storage is locked". Latent until encryption is enabled.
- **`BUDGET_DATA_DIR` is not honored.** The spec says the binary resolves the data
  directory the way `cmd/server` does; `cmd/server` goes through `config.Load()`,
  `resolveDataDir` only reads the flag and hardcodes `./data/settings`. With a
  custom data dir plus a stale `./data/settings`, the server answers about the
  wrong plan.

## 4. `active` is unknowable from a separate process

`list_scenarios` reports `active`, but the active filename is in-process state
initialized to `whatif.json` and never persisted, so a separate MCP process always
reports `whatif.json` regardless of the web UI's selection. Either rename the
field to `default` and reword the description, or persist the active scenario.

## 5. Smaller deferred findings

- `Source.names()` swallows the `ListScenarios` error, so a broken listing renders
  as `(available: )` — indistinguishable from having no scenarios.
- `Apply`'s base-untouched test asserts only a scalar; it cannot detect aliasing of
  the `RothConversion` / `SocialSecurity` / `TaxConfig` pointer fields.
- The "no social_security configuration" error path is advertised to callers as a
  contract but has no test.
- Setting only `RothConversionStart`/`End` on a scenario with conversions disabled
  is a silent no-op (the inverted-window case is now rejected; this one is not).
- The panic test registers a synthetic tool wired like the production handlers
  rather than driving a real one — it tests the pattern, not the four call sites.
- `server.go` hand-rolls the load→prepare→run pipeline in two handlers. A
  `func (s *Source) prepared(name) (prepare.PreparedSettings, string, error)`
  helper would remove the duplication — that duplication is exactly where the
  `get_months` missing-hooks bug entered.
- Error-message conventions are mixed: `scenarios.go` namespaces its errors
  (`"list scenarios: …"`), `validate()` and `MonthWindow` return bare messages.
- `go.sum` gained `golang-jwt/jwt/v5` and `x/oauth2` via the SDK's HTTP-transport
  stack. Linked but unreachable — the binary only constructs `mcp.StdioTransport`.
- `.mcp.json` runs `go run ./cmd/whatif-mcp`, which triggers `go mod download` on a
  fresh clone — real network egress at first launch, from the toolchain rather
  than the server. Worth a sentence in the README.

## 6. Manual verification not yet performed

The plan's manual-verification section — driving the four tools from a real Claude
Code session and confirming the figures match the what-if page — needs an
interactive session and has not been done. Step 3 of that list is the meaningful
one: if `get_analysis` disagrees with the UI for the same scenario, the shaping or
the run path is wrong.
