package whatifmcp

import (
	"context"
	_ "embed"
	"fmt"

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
	Scenario   string       `json:"scenario"`
	Applied    Overrides    `json:"applied_overrides"`
	Analysis   AnalysisView `json:"analysis"`
	Stochastic bool         `json:"monte_carlo_omitted"`
}

// NewServer builds the MCP server. Every tool is read-only with respect to the
// data directory: scenarios are loaded and copied, never written.
func NewServer(src *Source) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "whatif", Version: "v0.1.0"}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_scenarios",
		Description: "List the saved what-if retirement scenarios with a one-line summary of each. " +
			"Call this first to find out which plans exist and which one is active.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listInput) (*mcp.CallToolResult, listOutput, error) {
		list, err := src.List()
		if err != nil {
			return nil, listOutput{}, err
		}
		return nil, listOutput{Scenarios: list}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_analysis",
		Description: "Get the full analysis for a saved scenario: headline balances, per-year projection, " +
			"budget fit, RMD schedule, tax totals and Monte Carlo success rate. Per-year detail only — " +
			"use get_months for month-by-month figures. Read the whatif://assumptions resource before " +
			"drawing conclusions; several real-world effects are not modeled.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in analysisInput) (*mcp.CallToolResult, analysisOutput, error) {
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
			"why a particular year behaves the way it does. At most 120 months per call.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in monthsInput) (*mcp.CallToolResult, monthsOutput, error) {
		settings, name, err := src.Load(in.Scenario)
		if err != nil {
			return nil, monthsOutput{}, err
		}
		prepared, err := prepare.From(settings)
		if err != nil {
			return nil, monthsOutput{}, fmt.Errorf("prepare %s: %w", name, err)
		}
		proj := engine.New().Run(engine.Input{Prepared: prepared})
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
			"identical runs disagree; compare the deterministic figures instead.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in runInput) (*mcp.CallToolResult, runOutput, error) {
		settings, name, err := src.Load(in.Scenario)
		if err != nil {
			return nil, runOutput{}, err
		}
		view, err := RunWithOverrides(settings, in.Overrides)
		if err != nil {
			return nil, runOutput{}, err
		}
		return nil, runOutput{Scenario: name, Applied: in.Overrides, Analysis: view, Stochastic: true}, nil
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
