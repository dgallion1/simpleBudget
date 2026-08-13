# App-wide MCP — Phase 1 (transport) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve MCP from inside `cmd/server` at `/mcp`, with today's eight what-if tools migrated onto the server's own settings manager and data loader, and retire the standalone `cmd/whatif-mcp` process.

**Architecture:** A new `internal/services/mcpsvc` package assembles an `*mcp.Server` from per-domain subpackages (`plan`, `spend`), each declaring its own narrow `Deps` struct. `mcpsvc` imports the subpackages; they never import it, so there is no cycle. Tools call `*retirement.SettingsManager` and `*dataloader.DataLoader` directly — the same instances the HTTP handlers use — so the MCP view and the page view cannot disagree. `cmd/server` mounts `mcp.NewStreamableHTTPHandler` at `/mcp`, outside the lock-check middleware group.

**Tech Stack:** Go 1.26, `github.com/modelcontextprotocol/go-sdk` v1.7.0 (already required), chi v5, stdlib `net/http` cross-origin protection.

**Spec:** `docs/superpowers/specs/2026-08-12-app-wide-mcp-design.md`

## Global Constraints

- Go 1.26 / toolchain go1.26.5. No new module dependencies — `go-sdk` v1.7.0 is already in `go.mod`.
- MCP server identity: `&mcp.Implementation{Name: "budget2", Version: "v0.2.0"}`.
- Mount path is exactly `/mcp`. It MUST be registered outside the `r.Group(lockCheckMiddleware)` block: that middleware answers `307 → /unlock`, which a JSON-RPC client cannot follow meaningfully. Locked storage is reported by the tools as an error string instead.
- `DisableLocalhostProtection` stays false (SDK default DNS-rebinding protection).
- Every tool handler keeps the `defer recoverToError("<tool>", &err)` first line. The go-sdk dispatches each call on its own goroutine with no recover of its own.
- Per `CLAUDE.md`: before editing any function/type, run `LSP` `incomingCalls`/`findReferences` and report the blast radius. Never rename by find-and-replace.
- Verify with `go build ./... && go vet ./... && go test ./... && staticcheck ./...`. Run tests **bare** — never pipe through `grep`/`head` without `set -o pipefail`.
- Pre-commit runs `make check`; never bypass with `--no-verify`.
- Tool descriptions are load-bearing (they are the model's only documentation). When a tool moves, its `Description` string moves verbatim unless a step says otherwise.

## File Structure

**Created:**
- `internal/services/mcpsvc/server.go` — `Deps`, `NewServer`, server instructions.
- `internal/services/mcpsvc/server_test.go` — assembly-level tests.
- `internal/services/mcpsvc/plan/register.go` — `Deps`, `Register`, the six what-if tools, `recoverToError`, assumptions resource.
- `internal/services/mcpsvc/plan/assumptions.md` — moved.
- `internal/services/mcpsvc/plan/{view,months,overrides,scenarios,snapshot}.go` — moved.
- `internal/services/mcpsvc/spend/register.go` — `Deps`, `Register`, `get_anomalies`, `get_price_creep`.
- `internal/services/mcpsvc/spend/insights.go` — moved (transaction-backed row builders).
- `cmd/server/mcp_mount_test.go` — router-level mount tests.

**Modified:**
- `cmd/server/main.go` — build the MCP server in `SetupDependencies`, mount in `SetupRouter`.
- `.mcp.json` — stdio command → HTTP URL.
- `README.md` — MCP section.

**Deleted:**
- `internal/services/whatifmcp/` (whole package, including `live.go` and its tests).
- `cmd/whatif-mcp/`.

---

### Task 1: `mcpsvc` skeleton and the `plan` package's assumptions resource

Establishes the two-package shape and proves an assembled server round-trips over the SDK's in-memory transport, before any tool moves.

**Files:**
- Create: `internal/services/mcpsvc/server.go`
- Create: `internal/services/mcpsvc/server_test.go`
- Create: `internal/services/mcpsvc/plan/register.go`
- Move: `internal/services/whatifmcp/assumptions.md` → `internal/services/mcpsvc/plan/assumptions.md`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `mcpsvc.Deps{Settings *retirement.SettingsManager; Loader *dataloader.DataLoader; Store *storage.Storage; SettingsDir string; SnapshotDir string; BaseURL string}`
  - `mcpsvc.NewServer(deps Deps) *mcp.Server`
  - `plan.Deps{Settings *retirement.SettingsManager; BaseURL string}` (grows a `Snapshots` field in Task 4)
  - `plan.Register(s *mcp.Server, deps Deps)`
  - `plan.recoverToError(tool string, err *error)`

- [ ] **Step 1: Move the assumptions file**

```bash
mkdir -p internal/services/mcpsvc/plan
git mv internal/services/whatifmcp/assumptions.md internal/services/mcpsvc/plan/assumptions.md
```

Leave `internal/services/whatifmcp/server.go`'s `//go:embed assumptions.md` broken for now — the whole package is deleted in Task 7. To keep the tree building in the meantime, copy the file back as well:

```bash
cp internal/services/mcpsvc/plan/assumptions.md internal/services/whatifmcp/assumptions.md
```

- [ ] **Step 2: Write the failing test**

Create `internal/services/mcpsvc/server_test.go`:

```go
package mcpsvc

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect wires an assembled server to a client over the SDK's in-memory
// transport. *mcp.Server exposes no public lister, so enumerating what was
// registered requires a connected *mcp.ClientSession.
func connect(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

func TestNewServerExposesTheAssumptionsResource(t *testing.T) {
	cs := connect(t, NewServer(Deps{}))

	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "whatif://assumptions"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("got %d contents, want 1", len(res.Contents))
	}
	if !strings.Contains(res.Contents[0].Text, "No mortality modeling") {
		t.Errorf("assumptions resource does not look like assumptions.md: %.80q", res.Contents[0].Text)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/services/mcpsvc/ -run TestNewServerExposesTheAssumptionsResource -v`
Expected: FAIL — the package does not compile (`undefined: Deps`, `undefined: NewServer`).

- [ ] **Step 4: Write `plan/register.go`**

```go
// Package plan serves the what-if retirement planner over MCP. It reads and
// runs saved scenarios through the server's own settings manager, so its
// answers and the what-if page's answers come from one place.
package plan

import (
	_ "embed"
	"context"
	"fmt"

	"budget2/internal/services/retirement"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed assumptions.md
var assumptionsMD string

// Deps is what the planner tools need. The settings manager is the server's
// own instance, not a second one opened on the same directory: it owns the
// active-scenario selection, the settings cache, and the write lock.
type Deps struct {
	Settings *retirement.SettingsManager
	BaseURL  string
}

// recoverToError converts a panic into an error so a bad scenario fails one
// tool call instead of terminating the session. The go-sdk dispatches every
// tool call on its own goroutine with no recover of its own, so this must run
// via a defer inside each handler closure.
func recoverToError(tool string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panicked: %v", tool, r)
	}
}

// Register adds the planner tools and the assumptions resource to s.
func Register(s *mcp.Server, deps Deps) {
	s.AddResource(&mcp.Resource{
		URI:         "whatif://assumptions",
		Name:        "Engine assumptions and limitations",
		Description: "What the projection engine does and does not model. Read before drawing conclusions from any analysis.",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "whatif://assumptions",
				MIMEType: "text/markdown",
				Text:     assumptionsMD,
			}},
		}, nil
	})
}
```

- [ ] **Step 5: Write `mcpsvc/server.go`**

```go
// Package mcpsvc assembles the budget2 MCP server from its per-domain
// subpackages. It is served by cmd/server at /mcp rather than by a separate
// process, so tools share the running server's settings manager, data loader
// and locks.
package mcpsvc

import (
	"budget2/internal/services/dataloader"
	"budget2/internal/services/mcpsvc/plan"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Deps carries the running server's services. Subpackages declare their own
// narrower Deps structs; NewServer maps onto them. Dependencies flow one way
// (mcpsvc -> subpackages), so no subpackage may import this one.
type Deps struct {
	Settings    *retirement.SettingsManager
	Loader      *dataloader.DataLoader
	Store       *storage.Storage
	SettingsDir string
	SnapshotDir string
	BaseURL     string
}

// serverInstructions is returned to the client on initialize. It is the
// closest thing this design has to a system prompt for the model consuming
// these tools, so it names the grounding rule directly rather than leaving it
// to be inferred from individual tool descriptions.
const serverInstructions = "These tools read and re-run a personal retirement projection for one " +
	"household. Ground every answer in the figures the tools actually return — " +
	"do not estimate or recompute by hand. Before drawing conclusions, read the " +
	"whatif://assumptions resource: it lists what the projection engine does not " +
	"model (mortality, market timing, and more), and a figure it never accounted " +
	"for should not be presented as settled. apply_changes writes to the saved " +
	"plan; run_scenario does not. Prefer run_scenario while exploring, and " +
	"apply_changes only when the user has settled on a change."

// NewServer builds the MCP server. A nil field in deps disables only the tools
// that need it; registration itself never touches a dependency.
func NewServer(deps Deps) *mcp.Server {
	s := mcp.NewServer(
		&mcp.Implementation{Name: "budget2", Version: "v0.2.0"},
		&mcp.ServerOptions{Instructions: serverInstructions},
	)
	plan.Register(s, plan.Deps{Settings: deps.Settings, BaseURL: deps.BaseURL})
	return s
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/services/mcpsvc/... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/services/mcpsvc internal/services/whatifmcp/assumptions.md
git commit -m "feat(mcp): add mcpsvc skeleton with the plan assumptions resource"
```

---

### Task 2: Manager-backed `plan.Source` and the four read-only tools

Moves the scenario reading, shaping, and override code, replacing `Source`'s private settings manager with the server's.

**Files:**
- Move: `internal/services/whatifmcp/{view,months,overrides,scenarios}.go` → `internal/services/mcpsvc/plan/`
- Move: `internal/services/whatifmcp/{view,months,overrides,scenarios}_test.go` → `internal/services/mcpsvc/plan/`
- Modify: `internal/services/mcpsvc/plan/scenarios.go` (Source loses `live`, `settingsDir`, `store`, `txSource`)
- Modify: `internal/services/mcpsvc/plan/register.go`
- Create: `internal/services/mcpsvc/plan/register_test.go`

**Interfaces:**
- Consumes: `plan.Deps`, `plan.recoverToError` (Task 1).
- Produces:
  - `plan.NewSource(sm *retirement.SettingsManager) *Source`
  - `(*plan.Source).List() ([]ScenarioInfo, error)`
  - `(*plan.Source).Load(name string) (*models.WhatIfSettings, string, error)`
  - `plan.ShapeAnalysis(a *models.WhatIfAnalysis, includeMonteCarlo bool) AnalysisView`
  - `plan.MonthWindow(p *models.ProjectionResult, from, to int) ([]MonthRow, error)`
  - `plan.RunWithOverrides(base *models.WhatIfSettings, o Overrides) (AnalysisView, error)`
  - `plan.Overrides` (alias of `overrides.Overrides`)
  - Tools: `list_scenarios`, `get_analysis`, `get_months`, `run_scenario`

- [ ] **Step 1: Move the files**

```bash
git mv internal/services/whatifmcp/view.go internal/services/mcpsvc/plan/view.go
git mv internal/services/whatifmcp/view_test.go internal/services/mcpsvc/plan/view_test.go
git mv internal/services/whatifmcp/months.go internal/services/mcpsvc/plan/months.go
git mv internal/services/whatifmcp/months_test.go internal/services/mcpsvc/plan/months_test.go
git mv internal/services/whatifmcp/overrides.go internal/services/mcpsvc/plan/overrides.go
git mv internal/services/whatifmcp/overrides_test.go internal/services/mcpsvc/plan/overrides_test.go
git mv internal/services/whatifmcp/scenarios.go internal/services/mcpsvc/plan/scenarios.go
git mv internal/services/whatifmcp/scenarios_test.go internal/services/mcpsvc/plan/scenarios_test.go
```

In each moved file change the package clause from `package whatifmcp` to `package plan`. Change nothing else in `view.go`, `months.go`, `overrides.go` or their tests — this is a move, not a rewrite.

The old package still references the moved symbols and will not build until Task 7. That is expected and contained: `make check` is only required to pass at each commit, so this task's commit comes after Step 7 restores the build by deleting the now-duplicated declarations from `whatifmcp` — see Step 7.

- [ ] **Step 2: Write the failing test**

Create `internal/services/mcpsvc/plan/register_test.go`:

```go
package plan

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestManager writes one scenario file and returns a settings manager
// rooted at it, mirroring how cmd/server constructs the real one.
func newTestManager(t *testing.T) *retirement.SettingsManager {
	t.Helper()
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := map[string]any{
		"name":             "Base",
		"portfolio_value":  1000000.0,
		"projection_years": 30,
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "whatif.json"), data, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return retirement.NewSettingsManager(settingsDir, store)
}

func connect(t *testing.T, deps Deps) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0.0.0"}, nil)
	Register(srv, deps)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

func TestListScenariosReadsTheManagersScenarios(t *testing.T) {
	cs := connect(t, Deps{Settings: newTestManager(t)})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_scenarios",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_scenarios returned an error: %+v", res.Content)
	}

	var out listOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if len(out.Scenarios) != 1 {
		t.Fatalf("got %d scenarios, want 1: %+v", len(out.Scenarios), out.Scenarios)
	}
	if out.Scenarios[0].Filename != "whatif.json" {
		t.Errorf("filename = %q, want whatif.json", out.Scenarios[0].Filename)
	}
	if out.Scenarios[0].PortfolioValue != 1000000 {
		t.Errorf("portfolio_value = %v, want 1000000", out.Scenarios[0].PortfolioValue)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	return b
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/services/mcpsvc/plan/ -run TestListScenariosReadsTheManagersScenarios -v`
Expected: FAIL — `undefined: listOutput`, and `Register` registers no `list_scenarios` tool.

- [ ] **Step 4: Rewrite `Source` onto the injected manager**

In `internal/services/mcpsvc/plan/scenarios.go`, replace the `Source` struct, its constructor, and `resolveActiveFilename` with:

```go
// Source reads saved what-if scenarios through the running server's settings
// manager. It writes nothing -- it exposes no method that does.
//
// The manager is injected rather than constructed here. A second manager on
// the same directory would have its own cache and its own idea of which
// scenario is active; sharing the server's instance is what makes the MCP
// answers and the page's answers the same answers.
type Source struct {
	sm *retirement.SettingsManager
}

func NewSource(sm *retirement.SettingsManager) *Source {
	return &Source{sm: sm}
}
```

and in `Load`, replace the `resolveActiveFilename()` call with the manager's own answer:

```go
	if name == "" {
		name = s.sm.ActiveFilename()
	}
```

Delete `resolveActiveFilename` entirely, and drop the now-unused `context` and `storage` imports. `List` and `names` are unchanged.

- [ ] **Step 5: Register the four read-only tools**

Add to `internal/services/mcpsvc/plan/register.go` — the input/output types and the four `mcp.AddTool` calls, with `Description` strings copied verbatim from `internal/services/whatifmcp/server.go`:

```go
type listInput struct{}

type listOutput struct {
	Scenarios []ScenarioInfo `json:"scenarios"`
}

type analysisInput struct {
	Scenario string `json:"scenario,omitempty" jsonschema:"saved scenario filename from list_scenarios; omit for the active scenario"`
}

type analysisOutput struct {
	Scenario string       `json:"scenario"`
	Analysis AnalysisView `json:"analysis"`
}

type monthsInput struct {
	Scenario  string `json:"scenario,omitempty" jsonschema:"saved scenario filename; omit for the active scenario"`
	FromMonth int    `json:"from_month" jsonschema:"first projection month, 0-based, inclusive"`
	ToMonth   int    `json:"to_month" jsonschema:"last projection month, inclusive; at most 120 months per call"`
}

type monthsOutput struct {
	Scenario string     `json:"scenario"`
	Months   []MonthRow `json:"months"`
}

type runInput struct {
	Scenario  string    `json:"scenario,omitempty" jsonschema:"saved scenario filename; omit for the active scenario"`
	Overrides Overrides `json:"overrides" jsonschema:"settings to change before running; omitted fields keep the scenario's value"`
}

type runOutput struct {
	Scenario          string       `json:"scenario"`
	Applied           Overrides    `json:"applied_overrides"`
	Analysis          AnalysisView `json:"analysis"`
	MonteCarloOmitted bool         `json:"monte_carlo_omitted"`
}
```

Inside `Register`, before the `AddResource` call, add:

```go
	src := NewSource(deps.Settings)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_scenarios",
		Description: "List the saved what-if retirement scenarios with a one-line summary of each. " +
			"Call this first to find out which plans exist and which one is active.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listInput) (res *mcp.CallToolResult, out listOutput, err error) {
		defer recoverToError("list_scenarios", &err)
		list, err := src.List()
		if err != nil {
			return nil, listOutput{}, err
		}
		return nil, listOutput{Scenarios: list}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_analysis",
		Description: "Get the full analysis for a saved scenario: headline balances, per-year projection, " +
			"budget fit, RMD start age/timing/tax-deferred value (not a year-by-year schedule), tax totals " +
			"and Monte Carlo success rate. Per-year detail only — use get_months for month-by-month figures. " +
			"Read the whatif://assumptions resource before drawing conclusions; several real-world effects " +
			"are not modeled.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in analysisInput) (res *mcp.CallToolResult, out analysisOutput, err error) {
		defer recoverToError("get_analysis", &err)
		settings, name, err := src.Load(in.Scenario)
		if err != nil {
			return nil, analysisOutput{}, err
		}
		prepared, err := prepare.From(settings)
		if err != nil {
			return nil, analysisOutput{}, fmt.Errorf("prepare %s: %w", name, err)
		}
		a := retirement.RunFull(engine.New(), engine.Input{Prepared: prepared})
		return nil, analysisOutput{Scenario: name, Analysis: ShapeAnalysis(a, true)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_months",
		Description: "Get month-by-month projection detail for an inclusive month range, for explaining " +
			"why a particular year behaves the way it does. At most 120 months per call. Read the " +
			"whatif://assumptions resource before drawing conclusions; several real-world effects are not modeled.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in monthsInput) (res *mcp.CallToolResult, out monthsOutput, err error) {
		defer recoverToError("get_months", &err)
		settings, name, err := src.Load(in.Scenario)
		if err != nil {
			return nil, monthsOutput{}, err
		}
		prepared, err := prepare.From(settings)
		if err != nil {
			return nil, monthsOutput{}, fmt.Errorf("prepare %s: %w", name, err)
		}
		proj := engine.New().Run(engine.Input{Prepared: prepared, Hooks: retirement.DefaultHooks()})
		rows, err := MonthWindow(proj, in.FromMonth, in.ToMonth)
		if err != nil {
			return nil, monthsOutput{}, err
		}
		return nil, monthsOutput{Scenario: name, Months: rows}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "run_scenario",
		Description: "Re-run a saved scenario with changed assumptions and return the resulting analysis, " +
			"without modifying the saved plan. Use this to check a claim before making it. " +
			"Monte Carlo is omitted from the result because it is stochastic and would make two " +
			"identical runs disagree; compare the deterministic figures instead. Read the " +
			"whatif://assumptions resource before drawing conclusions; several real-world effects are not modeled.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in runInput) (res *mcp.CallToolResult, out runOutput, err error) {
		defer recoverToError("run_scenario", &err)
		settings, name, err := src.Load(in.Scenario)
		if err != nil {
			return nil, runOutput{}, err
		}
		view, err := RunWithOverrides(settings, in.Overrides)
		if err != nil {
			return nil, runOutput{}, err
		}
		return nil, runOutput{Scenario: name, Applied: in.Overrides, Analysis: view, MonteCarloOmitted: true}, nil
	})
```

Add the imports `budget2/internal/services/retirement/engine` and `budget2/internal/services/retirement/prepare` to `register.go`.

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/services/mcpsvc/... -v`
Expected: PASS, including the moved `view`/`months`/`overrides`/`scenarios` tests now running under `package plan`.

Note: `go build ./...` still fails in `internal/services/whatifmcp` (its `server.go` references symbols that moved). Step 7 fixes that.

- [ ] **Step 7: Restore the old package's build**

The moved code cannot exist in both packages. Reduce `internal/services/whatifmcp` to a compiling shell for one more task by deleting its now-orphaned server:

```bash
git rm internal/services/whatifmcp/server.go internal/services/whatifmcp/server_test.go
git rm internal/services/whatifmcp/assumptions.md
```

Then delete `cmd/whatif-mcp` too, since it exists only to call `whatifmcp.NewServer`:

```bash
git rm -r cmd/whatif-mcp
```

`live.go`, `insights.go`, `snapshot.go` and their tests stay until Tasks 4, 5 and 7 have taken what they need.

- [ ] **Step 8: Verify the whole tree builds and passes**

Run: `go build ./... && go vet ./... && go test ./... && staticcheck ./...`
Expected: all green. `.mcp.json` still points at the deleted `cmd/whatif-mcp` — Task 7 fixes that; nothing in the test suite reads it.

- [ ] **Step 9: Commit**

```bash
git add -A internal/services/mcpsvc internal/services/whatifmcp cmd/whatif-mcp
git commit -m "refactor(mcp): move planner reads onto the server's settings manager"
```

---

### Task 3: `open_page`

The server is now the process serving the request, so there is nothing to spawn and nothing to verify.

**Files:**
- Modify: `internal/services/mcpsvc/plan/register.go`
- Modify: `internal/services/mcpsvc/plan/register_test.go`

**Interfaces:**
- Consumes: `plan.Deps` (Task 1), `plan.NewSource` (Task 2).
- Produces: tool `open_page` returning `openPageOutput{URL string; Active string; Revision int}`.

- [ ] **Step 1: Write the failing test**

Append to `internal/services/mcpsvc/plan/register_test.go`:

```go
func TestOpenPageReturnsTheWhatIfURLAndActiveScenario(t *testing.T) {
	cs := connect(t, Deps{Settings: newTestManager(t), BaseURL: "http://localhost:8080"})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "open_page",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("open_page returned an error: %+v", res.Content)
	}

	var out openPageOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.URL != "http://localhost:8080/whatif" {
		t.Errorf("url = %q, want http://localhost:8080/whatif", out.URL)
	}
	if out.Active != "whatif.json" {
		t.Errorf("active = %q, want whatif.json", out.Active)
	}
}

func TestOpenPageSwitchesToTheRequestedScenario(t *testing.T) {
	sm := newTestManager(t)
	if _, err := sm.CreateScenario("Later Retirement"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	// CreateScenario switches to the new file; switch back so the test's
	// request is a real change rather than a no-op.
	if err := sm.SwitchScenario("whatif.json"); err != nil {
		t.Fatalf("SwitchScenario: %v", err)
	}

	scenarios, err := sm.ListScenarios()
	if err != nil {
		t.Fatalf("ListScenarios: %v", err)
	}
	var target string
	for _, sc := range scenarios {
		if sc.Filename != "whatif.json" {
			target = sc.Filename
		}
	}
	if target == "" {
		t.Fatal("no second scenario was created")
	}

	cs := connect(t, Deps{Settings: sm, BaseURL: "http://localhost:8080"})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "open_page",
		Arguments: map[string]any{"scenario": target},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("open_page returned an error: %+v", res.Content)
	}

	var out openPageOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Active != target {
		t.Errorf("active = %q, want %q", out.Active, target)
	}
	if got := sm.ActiveFilename(); got != target {
		t.Errorf("manager active = %q, want %q", got, target)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/services/mcpsvc/plan/ -run TestOpenPage -v`
Expected: FAIL — `undefined: openPageOutput`.

- [ ] **Step 3: Register `open_page`**

Add the types to `register.go`:

```go
type openPageInput struct {
	Scenario string `json:"scenario,omitempty" jsonschema:"saved scenario filename to switch to first; omit to use the active one"`
}

// openPageOutput drops the "started" field the standalone MCP process
// reported: the server answering this call is the server being linked to, so
// there is never anything to start.
type openPageOutput struct {
	URL      string `json:"url"`
	Active   string `json:"active"`
	Revision int    `json:"revision"`
}
```

and the tool inside `Register`:

```go
	mcp.AddTool(s, &mcp.Tool{
		Name: "open_page",
		Description: "Return the URL of the what-if page. Call this before apply_changes. The page " +
			"updates itself, so a tab opened from this URL will show later changes without being reloaded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in openPageInput) (res *mcp.CallToolResult, out openPageOutput, err error) {
		defer recoverToError("open_page", &err)

		if in.Scenario != "" && in.Scenario != deps.Settings.ActiveFilename() {
			if err := deps.Settings.SwitchScenario(in.Scenario); err != nil {
				return nil, openPageOutput{}, fmt.Errorf("switching to scenario %q: %w", in.Scenario, err)
			}
		}
		return nil, openPageOutput{
			URL:      deps.BaseURL + "/whatif",
			Active:   deps.Settings.ActiveFilename(),
			Revision: deps.Settings.Revision(),
		}, nil
	})
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/services/mcpsvc/plan/ -run TestOpenPage -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/mcpsvc/plan
git commit -m "feat(mcp): serve open_page from the running server's own state"
```

---

### Task 4: `apply_changes` and the snapshotter

The only planner write. The scenario-switch race it defended against over HTTP is still real in-process — a browser request can switch scenarios between the read and the write — so `ApplyOverrides`' `expectedScenario` argument still carries the guarantee.

**Files:**
- Move: `internal/services/whatifmcp/snapshot.go` → `internal/services/mcpsvc/plan/snapshot.go`
- Move: `internal/services/whatifmcp/snapshot_test.go` → `internal/services/mcpsvc/plan/snapshot_test.go`
- Modify: `internal/services/mcpsvc/plan/register.go`
- Modify: `internal/services/mcpsvc/server.go`
- Modify: `internal/services/mcpsvc/plan/register_test.go`

**Interfaces:**
- Consumes: `plan.Deps` (Task 1), `plan.NewSource` / `plan.ShapeAnalysis` (Task 2).
- Produces:
  - `plan.NewSnapshotter(settingsDir, snapshotDir string) *Snapshotter` (moved verbatim)
  - `(*plan.Snapshotter).Ensure(scenario string, now time.Time) (string, error)` (moved verbatim)
  - `plan.Deps` gains `Snapshots *Snapshotter`
  - Tool `apply_changes` returning `applyChangesOutput{Scenario, Applied, RevisionBefore, RevisionAfter, SnapshotPath, Analysis}`

- [ ] **Step 1: Move the snapshotter**

```bash
git mv internal/services/whatifmcp/snapshot.go internal/services/mcpsvc/plan/snapshot.go
git mv internal/services/whatifmcp/snapshot_test.go internal/services/mcpsvc/plan/snapshot_test.go
```

Change the package clause in both to `package plan`. Change nothing else — including the comment about followups §3, which stays accurate: `Ensure` reads rather than links, so a locked store still fails the snapshot instead of copying ciphertext.

- [ ] **Step 2: Write the failing test**

Append to `internal/services/mcpsvc/plan/register_test.go`:

```go
func TestApplyChangesWritesSnapshotsAndReportsBothRevisions(t *testing.T) {
	sm := newTestManager(t)
	snapDir := t.TempDir()
	deps := Deps{
		Settings:  sm,
		Snapshots: NewSnapshotter(sm.SettingsDir(), snapDir),
		BaseURL:   "http://localhost:8080",
	}
	before := sm.Revision()

	cs := connect(t, deps)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "apply_changes",
		Arguments: map[string]any{
			"overrides": map[string]any{"projection_years": 35},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("apply_changes returned an error: %+v", res.Content)
	}

	var out applyChangesOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Scenario != "whatif.json" {
		t.Errorf("scenario = %q, want whatif.json", out.Scenario)
	}
	if out.RevisionBefore != before {
		t.Errorf("revision_before = %d, want %d", out.RevisionBefore, before)
	}
	if out.RevisionAfter <= out.RevisionBefore {
		t.Errorf("revision_after = %d, want greater than %d", out.RevisionAfter, out.RevisionBefore)
	}
	if _, err := os.Stat(out.SnapshotPath); err != nil {
		t.Errorf("snapshot %q not written: %v", out.SnapshotPath, err)
	}

	saved, err := sm.LoadScenarioSettings("whatif.json")
	if err != nil {
		t.Fatalf("LoadScenarioSettings: %v", err)
	}
	if saved.ProjectionYears != 35 {
		t.Errorf("projection_years = %d, want 35 — the override was not persisted", saved.ProjectionYears)
	}
}

// A failed snapshot must abort the write. Pointing the snapshot directory at
// a path that cannot be created is the cheapest way to force that failure.
func TestApplyChangesDoesNotWriteWhenTheSnapshotFails(t *testing.T) {
	sm := newTestManager(t)
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	deps := Deps{
		Settings:  sm,
		Snapshots: NewSnapshotter(sm.SettingsDir(), filepath.Join(blocker, "snapshots")),
	}

	cs := connect(t, deps)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "apply_changes",
		Arguments: map[string]any{
			"overrides": map[string]any{"projection_years": 35},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("apply_changes should have failed when the snapshot could not be written")
	}

	saved, err := sm.LoadScenarioSettings("whatif.json")
	if err != nil {
		t.Fatalf("LoadScenarioSettings: %v", err)
	}
	if saved.ProjectionYears == 35 {
		t.Error("the scenario was written despite the snapshot failing")
	}
}
```

Add `"os"` and `"path/filepath"` to the test file's imports if they are not already present.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/services/mcpsvc/plan/ -run TestApplyChanges -v`
Expected: FAIL — `undefined: applyChangesOutput`, and `Deps` has no `Snapshots` field.

- [ ] **Step 4: Register `apply_changes`**

Add `Snapshots *Snapshotter` to `plan.Deps`. Add the types:

```go
type applyChangesInput struct {
	Scenario  string    `json:"scenario,omitempty" jsonschema:"saved scenario filename; omit for the active one"`
	Overrides Overrides `json:"overrides" jsonschema:"settings to change and save; omitted fields keep their current value"`
}

type applyChangesOutput struct {
	Scenario       string       `json:"scenario"`
	Applied        Overrides    `json:"applied"`
	RevisionBefore int          `json:"revision_before"`
	RevisionAfter  int          `json:"revision_after"`
	SnapshotPath   string       `json:"snapshot_path"`
	Analysis       AnalysisView `json:"analysis"`
}
```

and the tool inside `Register`:

```go
	mcp.AddTool(s, &mcp.Tool{
		Name: "apply_changes",
		Description: "Save changed assumptions to the retirement plan and return the resulting analysis. " +
			"THIS MODIFIES THE SAVED PLAN — use run_scenario to check a claim without writing. " +
			"An open what-if page picks the change up within about two seconds. A copy of the scenario " +
			"is saved before this session's first change to that scenario — later changes in the same " +
			"session are not separately recoverable; recovering from an unwanted change means restoring " +
			"that .bak file by hand. Note two behaviors: roth_conversion_amount of 0 DISABLES " +
			"conversions, and healthcare_inflation cannot be saved (preview it with run_scenario). " +
			"Read the whatif://assumptions resource before drawing conclusions.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in applyChangesInput) (res *mcp.CallToolResult, out applyChangesOutput, err error) {
		defer recoverToError("apply_changes", &err)

		if in.Scenario != "" && in.Scenario != deps.Settings.ActiveFilename() {
			if err := deps.Settings.SwitchScenario(in.Scenario); err != nil {
				return nil, applyChangesOutput{}, fmt.Errorf("switching to scenario %q: %w", in.Scenario, err)
			}
		}

		active := deps.Settings.ActiveFilename()
		revisionBefore := deps.Settings.Revision()

		// Before the write, never after: a failed snapshot must abort it.
		snapPath, err := deps.Snapshots.Ensure(active, time.Now())
		if err != nil {
			return nil, applyChangesOutput{}, err
		}

		// active is the scenario the snapshot above covers. Passing it as the
		// expectation moves the read-vs-write comparison inside the manager's
		// write lock, where a browser-driven scenario switch racing this call
		// can still PREVENT the write instead of being reported after it.
		settings, written, revisionAfter, err := deps.Settings.ApplyOverrides(in.Overrides, active)
		if err != nil {
			return nil, applyChangesOutput{}, err
		}

		prepared, err := prepare.From(settings)
		if err != nil {
			return nil, applyChangesOutput{}, fmt.Errorf("prepare %s: %w", written, err)
		}
		a := retirement.RunFull(engine.New(), engine.Input{Prepared: prepared})

		return nil, applyChangesOutput{
			Scenario:       written,
			Applied:        in.Overrides,
			RevisionBefore: revisionBefore,
			RevisionAfter:  revisionAfter,
			SnapshotPath:   snapPath,
			Analysis:       ShapeAnalysis(a, true),
		}, nil
	})
```

Add `"time"` to `register.go`'s imports.

- [ ] **Step 5: Wire the snapshotter in `mcpsvc`**

In `internal/services/mcpsvc/server.go`, extend the `plan.Register` call:

```go
	plan.Register(s, plan.Deps{
		Settings:  deps.Settings,
		Snapshots: plan.NewSnapshotter(deps.SettingsDir, deps.SnapshotDir),
		BaseURL:   deps.BaseURL,
	})
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/services/mcpsvc/... -v`
Expected: PASS, including the moved snapshot tests.

- [ ] **Step 7: Commit**

```bash
git add -A internal/services/mcpsvc internal/services/whatifmcp
git commit -m "feat(mcp): apply planner changes through the server's settings manager"
```

---

### Task 5: The `spend` package with the two insight tools

`get_anomalies` and `get_price_creep` already read the whole ledger rather than a scenario, so they belong in `spend` from the start. In-server they take the server's `*dataloader.DataLoader` directly, which deletes the settings-dir-shape derivation and its guard entirely.

**Files:**
- Move: `internal/services/whatifmcp/insights.go` → `internal/services/mcpsvc/spend/insights.go`
- Move: `internal/services/whatifmcp/insights_test.go` → `internal/services/mcpsvc/spend/insights_test.go`
- Create: `internal/services/mcpsvc/spend/register.go`
- Create: `internal/services/mcpsvc/spend/register_test.go`
- Modify: `internal/services/mcpsvc/server.go`

**Interfaces:**
- Consumes: `mcpsvc.Deps` (Task 1).
- Produces:
  - `spend.TransactionSource` — `interface{ LoadData() (*models.TransactionSet, error) }`
  - `spend.Deps{Transactions TransactionSource; Store *storage.Storage}`
  - `spend.Register(s *mcp.Server, deps Deps)`
  - Tools `get_anomalies`, `get_price_creep`

- [ ] **Step 1: Move the insight code**

```bash
mkdir -p internal/services/mcpsvc/spend
git mv internal/services/whatifmcp/insights.go internal/services/mcpsvc/spend/insights.go
git mv internal/services/whatifmcp/insights_test.go internal/services/mcpsvc/spend/insights_test.go
```

Change both package clauses to `package spend`. In `insights.go`, delete `func (s *Source) Transactions()` entirely along with the `dataDirFromSettingsDir` call it made — the loader is now injected. Keep the row builders (`anomalyRows`, `priceCreepRows`), the input/output types, `parseWindowDate`, and `nilableString` exactly as they are.

`insights.go` also uses `round0`, which moved to `plan/view.go` in Task 2. Add a copy to `spend/insights.go` rather than exporting it across packages — it is one line and cross-package coupling for `math.Round` is not worth it:

```go
func round0(v float64) float64 { return math.Round(v) }
```

- [ ] **Step 2: Write the failing test**

Create `internal/services/mcpsvc/spend/register_test.go`:

```go
package spend

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubTransactions struct {
	ts  *models.TransactionSet
	err error
}

func (s stubTransactions) LoadData() (*models.TransactionSet, error) { return s.ts, s.err }

func connect(t *testing.T, deps Deps) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0.0.0"}, nil)
	Register(srv, deps)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

func TestGetPriceCreepReadsTheInjectedLoader(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: &models.TransactionSet{}}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_price_creep",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_price_creep returned an error: %+v", res.Content)
	}

	var out priceCreepOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Count != 0 {
		t.Errorf("count = %d, want 0 for an empty ledger", out.Count)
	}
}

func TestGetAnomaliesReportsALoadFailureAsAToolError(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{err: errors.New("storage is locked")}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_anomalies",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("get_anomalies should have reported the load failure as a tool error")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	return b
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/services/mcpsvc/spend/ -v`
Expected: FAIL — `undefined: Deps`, `undefined: Register`.

- [ ] **Step 4: Write `spend/register.go`**

```go
// Package spend serves spending analysis over MCP: what the ledger says, as
// opposed to what the retirement projection assumes.
package spend

import (
	"context"
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TransactionSource loads the full transaction history. *dataloader.DataLoader
// satisfies it via its existing LoadData method, so no adapter is needed in
// production. The interface exists so tests can substitute a canned
// models.TransactionSet directly -- constructing exact peer groups and planted
// anomalies through real CSV parsing, classification, and near-duplicate
// detection would be indirect and brittle.
type TransactionSource interface {
	LoadData() (*models.TransactionSet, error)
}

// Deps is what the spending tools need. Store is optional and used only to
// turn a locked store into a clear message instead of a parse failure.
type Deps struct {
	Transactions TransactionSource
	Store        *storage.Storage
}

func recoverToError(tool string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panicked: %v", tool, r)
	}
}

// load returns the full ledger, reporting a locked store as such rather than
// letting ciphertext surface as a parse error.
func (d Deps) load() (*models.TransactionSet, error) {
	if d.Store != nil && d.Store.IsEncrypted() && !d.Store.IsUnlocked() {
		return nil, fmt.Errorf(
			"cannot load transaction history: storage is encrypted and locked; unlock it via the budget2 web UI (/unlock) first")
	}
	return d.Transactions.LoadData()
}

// Register adds the spending tools to s.
func Register(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_anomalies",
		Description: "Flag unusual expense transactions: amounts far outside a merchant's or category's " +
			"typical range (mad_merchant, mad_category), or an outsized first-ever charge from a brand-new " +
			"merchant (new_merchant). Detection ALWAYS runs over the COMPLETE transaction history -- peer-group " +
			"baselines and each merchant's first-ever occurrence never change with the window -- start_date and " +
			"end_date only filter which already-detected flags are RETURNED, so a narrow window will not " +
			"chronically re-flag a long-standing recurring bill as \"new\" merely because its true first " +
			"occurrence predates the window. Only expenses are considered (outflows: TransactionType == Outflow " +
			"AND Amount < 0); the returned amount is the transaction's signed amount, so expenses are negative. " +
			"Both date params are optional, inclusive, YYYY-MM-DD; an invalid date is a tool error.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in anomaliesInput) (res *mcp.CallToolResult, out anomaliesOutput, err error) {
		defer recoverToError("get_anomalies", &err)

		start, err := parseWindowDate("start_date", in.StartDate)
		if err != nil {
			return nil, anomaliesOutput{}, err
		}
		end, err := parseWindowDate("end_date", in.EndDate)
		if err != nil {
			return nil, anomaliesOutput{}, err
		}

		ts, err := deps.load()
		if err != nil {
			return nil, anomaliesOutput{}, err
		}

		rows := anomalyRows(ts, start, end)
		return nil, anomaliesOutput{
			Count:     len(rows),
			Window:    anomaliesWindow{Start: nilableString(in.StartDate), End: nilableString(in.EndDate)},
			Anomalies: rows,
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_price_creep",
		Description: "Find recurring merchant charges whose amount has drifted upward over their full history: " +
			"for each merchant with at least 6 occurrences, compares the median of its first 3 charges to the " +
			"median of its last 3 and reports it when the increase exceeds 5%. Always runs over the COMPLETE " +
			"transaction history -- there is no window parameter, because the whole point is a merchant's amount " +
			"across its full lifetime, not one period. Only expenses are considered (outflows: TransactionType " +
			"== Outflow AND Amount < 0); the returned amounts are absolute (positive) dollar figures.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ priceCreepInput) (res *mcp.CallToolResult, out priceCreepOutput, err error) {
		defer recoverToError("get_price_creep", &err)

		ts, err := deps.load()
		if err != nil {
			return nil, priceCreepOutput{}, err
		}

		rows := priceCreepRows(ts)
		return nil, priceCreepOutput{Count: len(rows), Items: rows}, nil
	})
}
```

- [ ] **Step 5: Wire `spend` into `mcpsvc`**

In `internal/services/mcpsvc/server.go`, add the import and the registration:

```go
	spend.Register(s, spend.Deps{Transactions: deps.Loader, Store: deps.Store})
```

Guard it so a nil loader does not register tools that must fail:

```go
	if deps.Loader != nil {
		spend.Register(s, spend.Deps{Transactions: deps.Loader, Store: deps.Store})
	}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/services/mcpsvc/... -v`
Expected: PASS, including the moved insight tests.

- [ ] **Step 7: Delete the emptied `whatifmcp` package**

Everything still in it is now either moved or dead (`live.go` and its client existed only for the cross-process write path):

```bash
git rm -r internal/services/whatifmcp
```

- [ ] **Step 8: Verify the whole tree**

Run: `go build ./... && go vet ./... && go test ./... && staticcheck ./...`
Expected: all green.

- [ ] **Step 9: Commit**

```bash
git add -A internal/services
git commit -m "feat(mcp): move the insight tools into a spend package on the shared loader"
```

---

### Task 6: Mount `/mcp` in `cmd/server`

**Files:**
- Modify: `cmd/server/main.go`
- Create: `cmd/server/mcp_mount_test.go`

**Interfaces:**
- Consumes: `mcpsvc.NewServer` / `mcpsvc.Deps` (Tasks 1–5).
- Produces: a package-level `mcpServer *mcp.Server` in `main`, and `/mcp` on the router.

- [ ] **Step 1: Write the failing test**

Create `cmd/server/mcp_mount_test.go`:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"budget2/internal/config"
	"budget2/internal/services/storage"
	"budget2/internal/testutil"

	"github.com/go-chi/chi/v5"
)

// initializeBody is a minimal MCP initialize request. A mounted streamable
// handler answers it; an unmounted path 404s.
const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{` +
	`"protocolVersion":"2025-06-18","capabilities":{},` +
	`"clientInfo":{"name":"mount-test","version":"v0.0.0"}}}`

// newMCPRouter mirrors the setup helper at the top of main_test.go: the
// package's dependencies are globals, so a router is only meaningful after
// SetupDependencies has run against a test config.
func newMCPRouter(t *testing.T) chi.Router {
	t.Helper()
	root := testutil.ProjectRoot()
	cfg := &config.Config{
		ListenAddr:         ":0",
		Debug:              true,
		DataDirectory:      testutil.TestDataDir(),
		UploadsDirectory:   testutil.TestDataDir() + "/uploads",
		SettingsDirectory:  testutil.TestDataDir() + "/settings",
		TemplatesDirectory: root + "/web/templates",
		StaticDirectory:    root + "/web/static",
		BackupDir:          t.TempDir(),
	}

	var err error
	store, err = storage.New(cfg.DataDirectory)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	if err := SetupDependencies(cfg); err != nil {
		t.Fatalf("SetupDependencies: %v", err)
	}
	return SetupRouter()
}

func newMCPRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	return req
}

func TestMCPEndpointIsMounted(t *testing.T) {
	r := newMCPRouter(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, newMCPRequest(t, initializeBody))

	if rec.Code == http.StatusNotFound {
		t.Fatal("/mcp is not mounted")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("initialize returned %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "budget2") {
		t.Errorf("initialize result does not name the budget2 server: %s", rec.Body.String())
	}
}

// The lock-check middleware answers 307 -> /unlock, which a JSON-RPC client
// cannot follow. /mcp must sit outside that group; its tools report a locked
// store as an error string instead.
func TestMCPEndpointIsNotBehindTheLockRedirect(t *testing.T) {
	r := newMCPRouter(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, newMCPRequest(t, initializeBody))

	if rec.Code == http.StatusTemporaryRedirect {
		t.Fatalf("/mcp redirected to %q; it must not sit behind lockCheckMiddleware", rec.Header().Get("Location"))
	}
}

// Cross-origin protection must reject a browser-issued cross-site POST: a
// page on any origin could otherwise drive these tools against local data.
func TestMCPEndpointRejectsCrossOriginRequests(t *testing.T) {
	r := newMCPRouter(t)

	req := newMCPRequest(t, initializeBody)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("a cross-site POST to /mcp was accepted")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/server/ -run TestMCPEndpoint -v`
Expected: FAIL with "/mcp is not mounted" (404).

- [ ] **Step 3: Build the MCP server in `SetupDependencies`**

Add to the `var (...)` block in `cmd/server/main.go`:

```go
	mcpServer     *mcp.Server
```

and at the end of `SetupDependencies`, after `backup.Initialize(...)`:

```go
	// The MCP server shares these exact instances -- not a second manager or
	// loader on the same directory -- so a tool call and a page request cannot
	// report different figures for the same plan.
	mcpServer = mcpsvc.NewServer(mcpsvc.Deps{
		Settings:    retirementMgr,
		Loader:      loader,
		Store:       store,
		SettingsDir: settingsDir,
		SnapshotDir: filepath.Join(cfg.BackupDir, "mcp-snapshots"),
		BaseURL:     "http://localhost" + cfg.ListenAddr,
	})
```

Add the imports:

```go
	"budget2/internal/services/mcpsvc"

	"github.com/modelcontextprotocol/go-sdk/mcp"
```

`settingsDir` is already a local in `SetupDependencies`; reuse it rather than recomputing.

- [ ] **Step 4: Mount the handler in `SetupRouter`**

In `cmd/server/main.go`, immediately after `r.Get("/api/version", handleVersion)` and **before** the `r.Group(func(r chi.Router) {` block:

```go
	// MCP endpoint. Deliberately outside the lock-check group below: that
	// middleware answers 307 -> /unlock, which a JSON-RPC client cannot
	// follow. A locked store is reported by the tools themselves.
	if mcpServer != nil {
		mcpHandler := mcp.NewStreamableHTTPHandler(
			func(*http.Request) *mcp.Server { return mcpServer },
			&mcp.StreamableHTTPOptions{SessionTimeout: 30 * time.Minute},
		)
		r.Handle("/mcp", http.NewCrossOriginProtection().Handler(mcpHandler))
	}
```

`time` is already imported in `main.go`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./cmd/server/ -run TestMCPEndpoint -v`
Expected: PASS.

- [ ] **Step 6: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... && staticcheck ./...`
Expected: all green.

- [ ] **Step 7: Commit**

```bash
git add cmd/server
git commit -m "feat(mcp): mount the MCP endpoint at /mcp in cmd/server"
```

---

### Task 7: Client config and documentation

**Files:**
- Modify: `.mcp.json`
- Modify: `README.md`

**Interfaces:**
- Consumes: the `/mcp` mount (Task 6).
- Produces: nothing code-facing.

- [ ] **Step 1: Point `.mcp.json` at the HTTP endpoint**

Replace the whole file with:

```json
{
  "mcpServers": {
    "budget2": {
      "type": "http",
      "url": "http://localhost:8080/mcp"
    }
  }
}
```

- [ ] **Step 2: Rewrite the existing README section**

`README.md` already has a `## Talking to your plan (MCP)` section (currently starting at line 312 and running to the `## Project Structure` heading). It describes `cmd/whatif-mcp`, stdio, `BUDGET_SERVER_URL`, server-spawning, and settings-dir verification — all of which this phase deleted. Replace the whole section, from the `## Talking to your plan (MCP)` heading down to but not including `## Project Structure`, with:

```markdown
## Talking to your plan (MCP)

The running server exposes its tools over MCP at `http://localhost:8080/mcp`.
The repo ships a `.mcp.json` pointing there, so Claude Code picks it up from the
repo root — you can ask questions about a plan, have the engine re-run to check
an answer, and look at spending patterns. **Start `budget2` first:** the tools
come from the running server, so if nothing is listening when a Claude Code
session starts, they will not be available. There is no separate MCP process.

Six planner tools: `list_scenarios`, `get_analysis`, `get_months`, and
`run_scenario` are read-only; `open_page` returns the what-if page URL,
switching the active scenario if you name one; and `apply_changes` **writes to
the saved plan**. Before its first write to a scenario in a session,
`apply_changes` snapshots that scenario to a `.bak` file under
`<backup-dir>/mcp-snapshots`. There is no in-app undo, so restoring that file by
hand is the recovery path for an unwanted change. A `whatif://assumptions`
resource describes what the engine does not model.

Two spending tools read the transaction history rather than a scenario.
`get_anomalies` flags unusual expense transactions (outflows only,
TransactionType == Outflow AND Amount < 0) by three methods: amounts far outside
a merchant's or category's typical range (mad_merchant, mad_category, using a
robust z-score against absolute amounts), or an outsized first-ever charge from
a brand-new merchant (new_merchant). Detection always runs over the complete
transaction history — peer-group baselines and each merchant's first-ever
occurrence never change with the window — and accepts optional `start_date` and
`end_date` (YYYY-MM-DD) to filter which already-detected flags are returned.
`get_price_creep` finds recurring merchant charges whose amounts have drifted
upward: for each merchant with at least 6 occurrences it compares the median of
the first 3 charges to the median of the last 3 and reports when the increase
exceeds 5%; decreases and single outliers never report.

Locked or encrypted storage surfaces as a clear error from the tool rather than
a parse failure — unlock via `/unlock` in the web UI first. The endpoint is
reachable only from this machine: requests arriving at a localhost address with
a non-localhost `Host` header are rejected, and cross-site browser requests are
refused.
```

- [ ] **Step 3: Verify the endpoint by hand**

```bash
go run ./cmd/server &
sleep 3
curl -s -X POST http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"v0"}}}'
```

Expected: a JSON-RPC result naming `budget2`. Stop the server afterwards.

- [ ] **Step 4: Confirm the tools work from a real session**

Restart Claude Code so it picks up the new `.mcp.json`, with `budget2` running, and call `list_scenarios` followed by `get_analysis`. Compare the headline figures against the what-if page for the same scenario. A disagreement means the shaping or the run path is wrong — this is the manual verification step the original MCP plan deferred (followups §6).

- [ ] **Step 5: Commit**

```bash
git add .mcp.json README.md
git commit -m "docs(mcp): point .mcp.json at the in-server endpoint"
```

---

## Definition of done

- `go build ./... && go vet ./... && go test ./... && staticcheck ./...` all green.
- `internal/services/whatifmcp/` and `cmd/whatif-mcp/` no longer exist.
- `POST /mcp` answers `initialize` with the `budget2` implementation name, is not redirected when storage is locked, and refuses cross-site requests.
- All eight tools are callable from a real Claude Code session, and `get_analysis` agrees with the what-if page for the same scenario.
- No test was deleted to make the suite pass. Every test that moved still runs.

## Follow-ups this phase deliberately leaves open

- The spec places `Snapshotter` in `mcpsvc` as shared write infrastructure; this plan leaves it in `plan`, the only package that uses it in Phase 1. Move it up to `mcpsvc` in Phase 3, when `curate`'s writes need it too.
- `serverInstructions` still describes a retirement-only toolset. It needs rewriting when `spend` grows real spending tools in Phase 2.
- `spend` tools read the ledger through `dataloader.LoadData` on every call, with no caching. Fine at two tools; revisit if Phase 2's tools make it hot.
- The stale-tab limitation from the spec is unaddressed by design: only the what-if page polls.
