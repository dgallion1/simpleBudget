// Package mcpsvc assembles the budget2 MCP server from its per-domain
// subpackages. It is served by cmd/server at /mcp rather than by a separate
// process, so tools share the running server's settings manager, data loader
// and locks.
package mcpsvc

import (
	"path/filepath"
	"time"

	"budget2/internal/services/backup"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/mcpsvc/admin"
	"budget2/internal/services/mcpsvc/confirm"
	"budget2/internal/services/mcpsvc/curate"
	"budget2/internal/services/mcpsvc/plan"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/mcpsvc/spend"
	"budget2/internal/services/restore"
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
	Backups     *backup.Service

	// Restores is the SAME *restore.Service the /restore HTTP handler uses,
	// handed over by handlers/backup.Initialize rather than constructed here:
	// a second instance over the same directories would work, but only as
	// long as nobody gave the two different gates or dirs. Nil disables
	// restore_backup's ability to act (the tool still registers and still
	// reports why).
	Restores *restore.Service

	// Shutdown stops the server process. Nil disables shutdown_server's
	// ability to act (the tool still registers and still reports why).
	Shutdown func()
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
	"the .bak copy taken before its first change of a session, when there was prior data on disk to " +
	"protect -- a write with nothing there yet to back up has no .bak, but also nothing to lose. " +
	"In these tools a per-expense `total` is NET SPEND and normally " +
	"POSITIVE (a refund reduces it, and a total can go negative), while a per-transaction `amount` is " +
	"SIGNED as stored, negative for a purchase -- the same split the spending tools use. Transactions " +
	"are addressed by `hash`, derived from date + lower-cased description + amount, so two " +
	"identical-looking transactions share one hash and are pinned together. Only outflows are matched " +
	"against major expenses; income never is. Pages other than the what-if planner do not refresh " +
	"themselves, so a curation write leaves an already-open Major Expenses tab showing stale data." +
	" Finally, nine HOUSEKEEPING tools describe the app itself rather than the money in it. get_status is " +
	"the one to call FIRST when another tool fails inexplicably: if the user's data is encrypted and " +
	"currently locked, every ledger-reading tool fails and get_status is the only one that still answers. " +
	"list_data_files inventories the bank exports on disk; its per-file row counts are raw and do NOT sum " +
	"to search_transactions' totals. list_duplicates is the queue of transaction pairs that look like one " +
	"payment recorded twice -- while a pair is unresolved BOTH sides are counted, so an unresolved queue " +
	"means the spending totals are inflated by those amounts, and saying so is often more useful than the " +
	"totals themselves. resolve_duplicates and undo_resolve WRITE TO THE USER'S DATA; confirm with the " +
	"user before calling either, and never invent a pair_key -- call list_duplicates and use one from " +
	"there. undo_resolve reverses a resolve_duplicates decision, but not by the same mechanism both ways: " +
	"undoing kept_winner makes the suppressed transaction live again, while undoing kept_both only " +
	"re-flags the pair for review, since kept_both never suppressed anything to begin with. run_backup " +
	"adds a zip to the backup directory and changes nothing else, so it is safe to call before suggesting " +
	"anything the user might want to walk back. list_backups reads that directory back, newest first, and " +
	"is the only place a restore_backup name may come from." +
	" Two tools are guarded, and both take two calls: the first returns what would happen plus a single-use " +
	"confirm_token, the second must echo that token. Calling one twice yourself is NOT the user agreeing; " +
	"show them the first call's answer and wait for a real answer. shutdown_server stops the server, and " +
	"after it runs nothing in this session can undo that -- every tool stops answering and only the user can " +
	"start the server again. restore_backup overwrites the whole data directory from an archive AND DELETES " +
	"every file that archive does not contain, so anything imported or decided since it was taken is lost; " +
	"it takes a safety snapshot first, which is the only route back and only via another restore."

// NewServer builds the MCP server. A nil Loader disables spend's, curate's
// and admin's tools; registration itself never touches a dependency. Other
// nil fields are not load-bearing at registration time but will fail
// individual tool calls that need them -- notably a nil SettingsDir/
// SnapshotDir still registers apply_changes (via an always-constructed
// Snapshotter), which then fails at call time rather than being absent from
// the tool list. A nil Backups degrades get_status's backup section (it
// reports "no backup service is configured" instead of a snapshot record)
// and disables nothing else: run_backup still registers and still gets
// called, it just fails that call with the same "not configured" error. A
// nil Backups is a supported configuration -- though it also disables
// list_backups and restore_backup, which have no way to name an archive
// without it. Likewise, a nil Shutdown is a supported configuration:
// shutdown_server still registers, but every call --
// including the no-argument preview -- fails fast with "no shutdown path is
// configured on this server" instead of stopping the process, because a
// server that cannot shut down should not mint a token no redeem could ever
// honor. A nil Restores behaves the same way for restore_backup.
//
// deps.Settings, by contrast, is not a supported nil configuration in
// production: cmd/server/main.go constructs it unconditionally, and
// plan.Register calls its methods without a nil check, so a nil Settings
// reaching a real tool call is a programming error, not a degraded mode.
// Tests may still construct NewServer with a nil Settings deliberately (see
// TestNewServerExposesTheAssumptionsResource), as long as the test never
// calls a tool that touches it.
//
// deps.Settings must be either a genuinely nil *retirement.SettingsManager or
// a fully constructed one -- never a typed-nil value manufactured some other
// way. plan and spend take it as that concrete type, so a nil pointer is
// harmless there, but it is also assigned into admin.Deps.Settings, which is
// an interface: a typed-nil *retirement.SettingsManager stored in an
// interface is a non-nil interface value that panics on first method call.
// admin's own nil check (deps.Settings != nil) cannot see through that, so
// this is a caller contract, not something get_status defends against.
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
		adminDeps := admin.Deps{
			Transactions: deps.Loader,
			Files:        deps.Loader,
			Duplicates:   deps.Loader,
			Decisions:    deps.Loader,
			Store:        deps.Store,
			Settings:     deps.Settings,
			// The same data-directory snapshot destination curate uses: both
			// write sidecar JSON files that live in the data dir, and a
			// restore is a hand-copy either way.
			Snapshots: snapshot.New(deps.Loader.CSVDirectory, filepath.Join(deps.SnapshotDir, "data")),
		}
		// admin.Deps.Backups is an INTERFACE and deps.Backups is a concrete
		// pointer, so assigning a nil *backup.Service unconditionally would
		// produce a non-nil interface holding a nil pointer -- admin's own
		// `Backups == nil` guards would then all take the wrong branch and
		// get_status and run_backup would panic instead of reporting the
		// service as absent. Only assign when there is really a service.
		if deps.Backups != nil {
			adminDeps.Backups = deps.Backups
		}
		// Same interface-holding-a-nil-pointer hazard as Backups above: assign
		// only when there is really a service, or restore_backup's own nil
		// check would take the wrong branch.
		if deps.Restores != nil {
			adminDeps.Restores = deps.Restores
		}
		// The registry is constructed per server, so tokens never outlive
		// the process.
		adminDeps.Confirm = confirm.NewRegistry(5 * time.Minute)
		if deps.Shutdown != nil {
			adminDeps.Shutdown = deps.Shutdown
		}
		admin.Register(s, adminDeps)
	}
	return s
}
