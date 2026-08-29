// Package plan serves the what-if retirement planner over MCP. It reads and
// runs saved scenarios through the server's own settings manager, so its
// answers and the what-if page's answers come from one place.
package plan

import (
	_ "embed"
	"context"
	"fmt"
	"time"

	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed assumptions.md
var assumptionsMD string

// Deps is what the planner tools need. The settings manager is the server's
// own instance, not a second one opened on the same directory: it owns the
// active-scenario selection, the settings cache, and the write lock.
type Deps struct {
	Settings  *retirement.SettingsManager
	Snapshots *snapshot.Snapshotter
	BaseURL   string
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

// Register adds the planner tools and the assumptions resource to s.
func Register(s *mcp.Server, deps Deps) {
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

	mcp.AddTool(s, &mcp.Tool{
		Name: "apply_changes",
		Description: "Save changed assumptions to the retirement plan and return the resulting analysis. " +
			"THIS MODIFIES THE SAVED PLAN — use run_scenario to check a claim without writing. " +
			"An open what-if page picks the change up within about two seconds. A copy of the scenario " +
			"is saved before this session's first change to that scenario — later changes in the same " +
			"session are not separately recoverable; recovering from an unwanted change means restoring " +
			"that .bak file by hand. Note several behaviors: roth_conversion_amount of 0 DISABLES " +
			"conversions, healthcare_inflation cannot be saved (preview it with run_scenario), " +
			"social_security_fra_benefit/spouse_fra_benefit are GROSS monthly amounts at full " +
			"retirement age and require Social Security to already be configured in the UI, and " +
			"healthcare_monthly_cost is today's total household cost, distributed proportionally " +
			"across any configured healthcare persons. Read the whatif://assumptions resource " +
			"before drawing conclusions.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in applyChangesInput) (res *mcp.CallToolResult, out applyChangesOutput, err error) {
		defer recoverToError("apply_changes", &err)

		// expected is the caller's own scenario name when it named one, not a
		// re-read of ActiveFilename() after the switch below: a second
		// scenario switch racing this call between that switch and a re-read
		// could otherwise snapshot and write a plan the caller never named.
		// Using the caller's own intent instead means such a race makes
		// ApplyOverrides refuse the write below, rather than silently
		// retargeting it.
		expected := in.Scenario
		if expected == "" {
			expected = deps.Settings.ActiveFilename()
		}

		if in.Scenario != "" && in.Scenario != deps.Settings.ActiveFilename() {
			if err := deps.Settings.SwitchScenario(in.Scenario); err != nil {
				return nil, applyChangesOutput{}, fmt.Errorf("switching to scenario %q: %w", in.Scenario, err)
			}
		}

		revisionBefore := deps.Settings.Revision()

		// Before the write, never after: a failed snapshot must abort it.
		snapPath, err := deps.Snapshots.Ensure(expected, time.Now())
		if err != nil {
			return nil, applyChangesOutput{}, err
		}

		// expected is also the scenario the snapshot above covers. Passing it
		// as the expectation moves the read-vs-write comparison inside the
		// manager's write lock, where a browser-driven scenario switch racing
		// this call can still PREVENT the write instead of being reported
		// after it.
		settings, written, revisionAfter, err := deps.Settings.ApplyOverrides(in.Overrides, expected)
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
