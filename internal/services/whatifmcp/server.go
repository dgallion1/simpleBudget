package whatifmcp

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed assumptions.md
var assumptionsMD string

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

type openPageInput struct {
	Scenario string `json:"scenario,omitempty" jsonschema:"saved scenario filename to switch to first; omit to use the active one"`
}

type openPageOutput struct {
	URL      string `json:"url"`
	Started  bool   `json:"started"`
	Active   string `json:"active"`
	Revision int    `json:"revision"`
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

// recoverToError converts a panic into an error so a bad scenario fails one
// tool call instead of terminating the stdio session. The go-sdk dispatches
// every tool call on its own goroutine with no recover of its own, so this
// must run via a defer inside each handler closure — a recover in main
// cannot catch a panic on a goroutine main didn't spawn.
func recoverToError(tool string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panicked: %v", tool, r)
	}
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

// NewServer builds the MCP server. list_scenarios, get_analysis, get_months,
// run_scenario, get_anomalies and get_price_creep are read-only with respect
// to the data directory: scenarios and transaction history are loaded and
// copied, never written. open_page and apply_changes are the exception --
// apply_changes saves changed assumptions to the running server's active
// scenario file.
func NewServer(src *Source, live *Client, snaps *Snapshotter) *mcp.Server {
	src.live = live

	s := mcp.NewServer(&mcp.Implementation{Name: "whatif", Version: "v0.1.0"},
		&mcp.ServerOptions{Instructions: serverInstructions})

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
		Name: "open_page",
		Description: "Return the URL of the what-if page, starting the budget2 web server first if " +
			"nothing is running. Call this before apply_changes. The page updates itself, so a tab " +
			"opened from this URL will show later changes without being reloaded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in openPageInput) (res *mcp.CallToolResult, out openPageOutput, err error) {
		defer recoverToError("open_page", &err)

		state, started, err := live.EnsureServer(ctx)
		if err != nil {
			return nil, openPageOutput{}, err
		}
		if in.Scenario != "" && in.Scenario != state.Active {
			if err := live.SwitchScenario(ctx, in.Scenario); err != nil {
				return nil, openPageOutput{}, err
			}
			if state, err = live.State(ctx); err != nil {
				return nil, openPageOutput{}, err
			}
		}
		return nil, openPageOutput{
			URL:      live.BaseURL() + "/whatif",
			Started:  started,
			Active:   state.Active,
			Revision: state.Revision,
		}, nil
	})

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

		state, _, err := live.EnsureServer(ctx)
		if err != nil {
			return nil, applyChangesOutput{}, err
		}
		if in.Scenario != "" && in.Scenario != state.Active {
			if err := live.SwitchScenario(ctx, in.Scenario); err != nil {
				return nil, applyChangesOutput{}, err
			}
			if state, err = live.State(ctx); err != nil {
				return nil, applyChangesOutput{}, err
			}
		}

		// Before the POST, never after: a failed snapshot must abort the write.
		snapPath, err := snaps.Ensure(state.Active, time.Now())
		if err != nil {
			return nil, applyChangesOutput{}, err
		}

		// state.Active is the scenario the snapshot above covers. Sending it
		// as the expectation moves the read-vs-write comparison inside the
		// server's write lock, where a mismatch can still PREVENT the write
		// (409) instead of merely reporting it afterwards.
		result, err := live.Apply(ctx, in.Overrides, state.Active)
		if err != nil {
			return nil, applyChangesOutput{}, err
		}

		// Defence in depth behind the expectation sent above. If a future
		// server ignores expected_scenario, or answers with a different file
		// than it was asked to write, the snapshot this call took no longer
		// covers the write -- and there is no way to roll it back from here,
		// so it must be reported rather than returned as a success.
		if result.Scenario != state.Active {
			return nil, applyChangesOutput{}, fmt.Errorf(
				"apply_changes wrote to %q, but the snapshot taken for this call covers %q (snapshot: %s); "+
					"the active scenario changed between the read and the write, likely a scenario switch in "+
					"the browser during this call -- the write to %q cannot be undone automatically and is not "+
					"covered by that snapshot",
				result.Scenario, state.Active, snapPath, result.Scenario)
		}

		settings, name, err := src.Load(result.Scenario)
		if err != nil {
			return nil, applyChangesOutput{}, err
		}
		prepared, err := prepare.From(settings)
		if err != nil {
			return nil, applyChangesOutput{}, fmt.Errorf("prepare %s: %w", name, err)
		}
		a := retirement.RunFull(engine.New(), engine.Input{Prepared: prepared})

		return nil, applyChangesOutput{
			Scenario:       result.Scenario,
			Applied:        result.Applied,
			RevisionBefore: state.Revision,
			RevisionAfter:  result.Revision,
			SnapshotPath:   snapPath,
			Analysis:       ShapeAnalysis(a, true),
		}, nil
	})

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

		ts, err := src.Transactions()
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

		ts, err := src.Transactions()
		if err != nil {
			return nil, priceCreepOutput{}, err
		}

		rows := priceCreepRows(ts)
		return nil, priceCreepOutput{Count: len(rows), Items: rows}, nil
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

	return s
}
