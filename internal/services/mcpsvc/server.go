// Package mcpsvc assembles the budget2 MCP server from its per-domain
// subpackages. It is served by cmd/server at /mcp rather than by a separate
// process, so tools share the running server's settings manager, data loader
// and locks.
package mcpsvc

import (
	"budget2/internal/services/dataloader"
	"budget2/internal/services/mcpsvc/plan"
	"budget2/internal/services/mcpsvc/spend"
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

// NewServer builds the MCP server. A nil Loader disables spend's tools;
// registration itself never touches a dependency. Other nil fields are not
// load-bearing at registration time but will fail individual tool calls that
// need them -- notably a nil SettingsDir/SnapshotDir still registers
// apply_changes (via an always-constructed Snapshotter), which then fails at
// call time rather than being absent from the tool list.
func NewServer(deps Deps) *mcp.Server {
	s := mcp.NewServer(
		&mcp.Implementation{Name: "budget2", Version: "v0.2.0"},
		&mcp.ServerOptions{Instructions: serverInstructions},
	)
	plan.Register(s, plan.Deps{
		Settings:  deps.Settings,
		Snapshots: plan.NewSnapshotter(deps.SettingsDir, deps.SnapshotDir),
		BaseURL:   deps.BaseURL,
	})
	if deps.Loader != nil {
		spend.Register(s, spend.Deps{Transactions: deps.Loader, Store: deps.Store})
	}
	return s
}
