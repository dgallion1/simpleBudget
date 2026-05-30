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

## Architecture pointers

- `internal/services/retirement/engine` — projection simulation loop (Monte
  Carlo, backtest, canonical). High blast radius; the analysis layer reads from
  here. Touch with care.
- `internal/services/retirement/analysis` — derived analyses (budget fit,
  sensitivity, score, present value) computed from engine output.
- `internal/handlers/whatif` — HTTP handlers + render tests for the what-if UI.
- `web/templates/components/whatif` — Go `html/template` views for the planner.
