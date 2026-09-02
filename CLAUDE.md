# simpleBudget — Code Intelligence

A pure-Go retirement/budget planner. Use first-party Go semantic analysis to
understand code, assess impact, and navigate safely — driven through the
built-in `LSP` tool (backed by `gopls`). No external index to keep fresh.

> Tooling: `gopls` (`~/go/bin/gopls`), `staticcheck`, and the Go 1.26 toolchain
> are installed and on `PATH`. If the `LSP` tool reports no server, run
> `go install golang.org/x/tools/gopls@latest`.

## Planning log — read first for any planning question

`PLANNING_LOG.md` at the repo root is the append-only record of the user's
household facts, corrections, and planning decisions across sessions. Before
answering ANY retirement/tax/conversion planning question, read it — its
facts override test fixtures (`testdata/`) and stale docs. When the user
states a personal fact, corrects one, or makes a decision in conversation,
append a dated `## YYYY-MM-DD — <topic>` entry (never rewrite history; add a
correction entry instead) and commit it.

## Always Do

- **Assess impact before editing a symbol.** Before modifying a function,
  method, or type, run `LSP` `incomingCalls` (and `findReferences` for vars/
  consts) on it, and report the blast radius — direct callers and the files
  they live in — to the user. For a wide blast radius, chase callers
  transitively with repeated `incomingCalls`.
- **Warn the user** when the blast radius is large or crosses package
  boundaries (e.g. an exported symbol with many callers, or anything in
  `internal/services/retirement/engine`) before proceeding.
- **Verify before committing.** Run `go build ./... && go vet ./... &&
  go test ./... && staticcheck ./...` and confirm the diff only touches what
  you intended (`git diff`).
- When exploring unfamiliar code, prefer `LSP` `workspaceSymbol` to locate a
  symbol, then `goToDefinition` / `outgoingCalls` to read how it works, instead
  of blind grepping.
- For full context on a symbol — its callers, callees, and signature — combine
  `incomingCalls`, `outgoingCalls`, and `hover`.

## Never Do

- NEVER edit a function, method, or type without first checking its callers
  (`incomingCalls` / `findReferences`).
- NEVER rename a symbol with find-and-replace. Use `LSP` `findReferences` to
  enumerate every use first, then change them all (gopls understands the call
  graph; text search does not).
- NEVER commit without a green `go build` + `go test` + `go vet` run.
- NEVER filter test output through a pipeline like `go test ./... 2>&1 | grep
  FAIL | head` — the pipe reports the LAST command's exit code, so a red suite
  reads as exit 0 and failures get re-run for hours (this happened). Run tests
  bare; if output must be trimmed, prefix `set -o pipefail;` so the test's own
  exit code survives.

## LSP tool quick reference

| Question | `LSP` operation |
|----------|-----------------|
| What breaks if I change X? (blast radius) | `incomingCalls`, `findReferences` |
| What does X depend on? | `outgoingCalls` |
| Where is X defined? | `goToDefinition` |
| Where is X used? | `findReferences` |
| Find a symbol by name across the repo | `workspaceSymbol` |
| What implements this interface? | `goToImplementation` |
| Type / doc of X | `hover` |
| All symbols in a file | `documentSymbol` |

`incomingCalls`/`outgoingCalls` need the cursor on the function *name* (e.g.
`func BudgetFit(...)` → the `BudgetFit` token), with 1-based line/character.

## Gotchas (this codebase)

Testing:
- Analysis-package tests build inputs with `runProj(t, s)` / `engineInput(t, s)`
  (`analysis/helpers_test.go`) via `prepare.MustFrom` — never the retirement
  `Calculator` (the analysis package must not import its parent).
- `prepare.From` recomputes `CurrentAge`/`SpouseAge` from each `Person.BirthMonth`
  + `StartDate`, overriding any `s.CurrentAge` you set. Set age in tests via
  `s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, age)`; a spouse
  needs a `PersonRoleSpouse` entry.
- `PresentValue` / `BudgetFit` read `in.Prepared.Settings()` (deep-copied +
  normalized), so compute test oracles from the prepared settings, not raw input.
- This WSL2 sandbox reports a frozen/coarse file `mtime`: `os.WriteFile` doesn't
  reliably advance it. Tests needing distinct timestamps must set them with
  `os.Chtimes`.

Retirement math:
- Tax/IRMAA bracket inflation must key off the plan's calendar year via
  `engine.YearsFromTaxBase(s, currentYear)`, NOT the raw projection-year offset
  (`taxBaseYear=2024`, `irmaaBaseYear=2026`).
- Rate units differ: `engine.PresentValueAnnuity` takes discount/growth as
  **percent**; `IncomeSource.COLARate` is a **decimal** (0.02 = 2%).
- All three projection loops — `engine/month.go` (canonical),
  `analysis/monte_carlo.go`, `analysis/backtest.go` — advance through
  `engine.ProjectionState.StepMonth` (`engine/stepper.go`), which owns the
  year-boundary pass, expense assembly, and the `PortfolioMonthInput` build.
  Per-month tax/IRMAA input changes go in the stepper (or
  `ExecuteTaxAwarePortfolioMonth`), not in the loops. Loops inject only a
  `MonthReturns`; its units are fixed at the seam: monthly **decimal** rates
  for tax-deferred/Roth, taxable annual return in **percent**, annual
  inflation as **decimal**. Monte Carlo must draw all its per-month RNG
  inside the `returnsFor` callback, in the legacy order, or seeded runs
  change.
- Roth bracket-fill conversion sizing (`analysis/tax_optimizer_strategies.go`)
  must mirror the engine's tax model: `bracketFillConversion` binary-searches
  `taxableOrdinaryIncome` for the conversion that hits the inflated bracket
  ceiling — never `ceiling − other` (the conversion itself raises §86
  provisional income, making more SS taxable). Ordinary income + RMD +
  non-qualified dividends fill the bracket; SS taxable portion, qualified
  dividends, and cap-gains distributions (LTCG) are §86-provisional-only. Add
  any new taxable component to `bracketFillIncomeForYear` to match the engine.
  Non-qualified Roth EARNINGS withdrawals (`TaxableRothEarnings`) are also
  ordinary income, but they depend on the conversions themselves (a conversion
  adds Roth basis that Pub 590-B basis-first ordering consumes before earnings).
  So `scoreCandidate` sizes iteratively: size → run engine → fold the per-year
  `TaxableRothEarnings` back into `bracketFillIncomeForYear` via the `feedback`
  map → re-size to convergence. This only changes sizing in the corner where a
  small Roth is drained past basis under 59½ during the conversion window; with
  no such earnings it converges in one engine run (behavior unchanged).

Live server staleness:
- Before comparing the running app on :8080 (screenshots, browser-driving,
  MCP answers) against repo code, check the build fingerprint:
  `curl -s localhost:8080/api/health` → `.commit`, or the `X-Budget2-Build`
  response header, vs `git describe --always --dirty` in your checkout.
  A mismatch means the server predates (or postdates) the code you are
  reading — say so instead of debugging phantom differences.
- `commit` is stamped by `make build` ldflags. "unknown" = built without
  make. Do NOT trust `go version -m <binary>`'s vcs.revision for binaries
  built under `.claude/worktrees/*` — Go stamps the PARENT checkout's HEAD
  there.

## Architecture pointers

- `internal/services/retirement/engine` — projection simulation loop (Monte
  Carlo, backtest, canonical). High blast radius; the analysis layer reads from
  here. Touch with care.
- `internal/services/retirement/analysis` — derived analyses (budget fit,
  sensitivity, score, present value) computed from engine output.
- `internal/handlers/whatif` — HTTP handlers + render tests for the what-if UI.
- `web/templates/components/whatif` — Go `html/template` views for the planner.
