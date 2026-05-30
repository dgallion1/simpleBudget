# simpleBudget — Code Intelligence

A pure-Go retirement/budget planner. Use first-party Go semantic analysis to
understand code, assess impact, and navigate safely — driven through the
built-in `LSP` tool (backed by `gopls`). No external index to keep fresh.

> Tooling: `gopls` (`~/go/bin/gopls`), `staticcheck`, and the Go 1.26 toolchain
> are installed and on `PATH`. If the `LSP` tool reports no server, run
> `go install golang.org/x/tools/gopls@latest`.

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
- Three projection loops build `PortfolioMonthInput` independently —
  `engine/month.go` (canonical), `analysis/monte_carlo.go`, `analysis/backtest.go`.
  Per-month tax/IRMAA input changes must be replicated across all three (or
  centralized in `ExecuteTaxAwarePortfolioMonth`).

## Architecture pointers

- `internal/services/retirement/engine` — projection simulation loop (Monte
  Carlo, backtest, canonical). High blast radius; the analysis layer reads from
  here. Touch with care.
- `internal/services/retirement/analysis` — derived analyses (budget fit,
  sensitivity, score, present value) computed from engine output.
- `internal/handlers/whatif` — HTTP handlers + render tests for the what-if UI.
- `web/templates/components/whatif` — Go `html/template` views for the planner.
