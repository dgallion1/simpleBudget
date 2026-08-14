// Package mcpsvc assembles the budget2 MCP server from its per-domain
// subpackages. It is served by cmd/server at /mcp rather than by a separate
// process, so tools share the running server's settings manager, data loader
// and locks.
package mcpsvc

import (
	"path/filepath"

	"budget2/internal/services/dataloader"
	"budget2/internal/services/mcpsvc/curate"
	"budget2/internal/services/mcpsvc/plan"
	"budget2/internal/services/mcpsvc/snapshot"
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
const serverInstructions = "These tools cover two things for one household: a personal retirement " +
	"projection (the plan tools) and the actual transaction ledger behind it (the spending tools). " +
	"Ground every answer in the figures the tools actually return — do not estimate or recompute by " +
	"hand. The ledger is what was actually spent; the projection is a model of the future. Never " +
	"present one as evidence for the other without saying which is which. Before drawing conclusions " +
	"from a projection, read the whatif://assumptions resource: it lists what the projection engine " +
	"does not model (mortality, market timing, and more), and a figure it never accounted for should " +
	"not be presented as settled. apply_changes writes to the saved plan; run_scenario does not. " +
	"Prefer run_scenario while exploring, and apply_changes only when the user has settled on a " +
	"change. Expense amounts are not signed the same way across the spending tools -- this is the " +
	"COMPLETE list of all six SPENDING tools, not a sample: signed in search_transactions (expenses " +
	"negative) and get_anomalies (expenses negative); positive in get_price_creep and get_recurring; and MIXED in " +
	"get_trends (current_amount/previous_amount are positive; change_amount/change_percent are " +
	"signed) and summarize_spending (total_expenses is always non-negative, but its by_category/ " +
	"by_merchant/by_month breakdown rows are normally positive and can go NEGATIVE when refunds " +
	"outweigh spending in that row) — read each field's own description rather than assuming a " +
	"convention. A \"merchant\" " +
	"in these tools is a fuzzy grouping of similar transaction descriptions, not a verified " +
	"counterparty, and merchant labels are lower-cased, so they will not match a transaction's " +
	"description verbatim. All spending tools exclude transactions the user has already resolved as " +
	"duplicates." +
	" There are also five curation tools covering the user's declared \"major expenses\" -- their own " +
	"labels for spending they already understand. list_major_expenses and list_exceptions read; " +
	"pin_transactions, upsert_major_expense and delete_major_expense WRITE TO THE USER'S DATA, so " +
	"confirm with the user before calling one. pin_transactions unpins when unpin is true -- but unpin " +
	"only REMOVES the pin, returning the transaction to ordinary keyword and amount matching; it does " +
	"NOT restore whatever the transaction was pinned to before, so repinning over an existing pin is " +
	"not reversible this way. delete_major_expense restores a deleted expense when restore is true, " +
	"bringing the definition and its captured pins back. upsert_major_expense does NOT reverse -- an " +
	"edit overwrites the previous definition, and the only way back for any of these three writes is " +
	"the .bak copy each takes before its first change of a session. " +
	"In these tools a per-expense `total` is NET SPEND and normally " +
	"POSITIVE (a refund reduces it, and a total can go negative), while a per-transaction `amount` is " +
	"SIGNED as stored, negative for a purchase -- the same split the spending tools use. Transactions " +
	"are addressed by `hash`, derived from date + lower-cased description + amount, so two " +
	"identical-looking transactions share one hash and are pinned together. Only outflows are matched " +
	"against major expenses; income never is. Pages other than the what-if planner do not refresh " +
	"themselves, so a curation write leaves an already-open Major Expenses tab showing stale data."

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
		Snapshots: snapshot.New(deps.SettingsDir, deps.SnapshotDir),
		BaseURL:   deps.BaseURL,
	})
	if deps.Loader != nil {
		spend.Register(s, spend.Deps{
			Transactions:  deps.Loader,
			Store:         deps.Store,
			Settings:      deps.Settings,
			MajorExpenses: deps.Loader,
		})
		// The data directory comes off the loader rather than a new Deps
		// field so the files curate snapshots are, by construction, the same
		// files the loader writes.
		curate.Register(s, curate.Deps{
			Transactions: deps.Loader,
			Expenses:     deps.Loader,
			Pins:         deps.Loader,
			Store:        deps.Store,
			// A separate snapshot subdirectory: plan snapshots files from the
			// settings dir and curate from the data dir, and nothing stops
			// the two directories from holding a file of the same name.
			Snapshots: snapshot.New(deps.Loader.CSVDirectory, filepath.Join(deps.SnapshotDir, "data")),
		})
	}
	return s
}
