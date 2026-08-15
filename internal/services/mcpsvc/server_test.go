package mcpsvc

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/services/dataloader"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"

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

// TestNewServerRegistersAllTwentyThreeTools drives an in-memory client/server
// round trip to enumerate what NewServer actually registered, rather than
// merely asserting deps.Loader != nil is checked somewhere. Deps{} (a zero
// value) is deliberately NOT used here: with a nil Loader, NewServer's own
// "if deps.Loader != nil" guard skips spend.Register, curate.Register and
// admin.Register entirely, so a suite that only ever constructs
// NewServer(Deps{}) would stay green even if spend.Register were deleted
// from NewServer outright. A non-nil Loader (and the
// Settings/SettingsDir/SnapshotDir plan.Register needs) closes that hole.
func TestNewServerRegistersAllTwentyThreeTools(t *testing.T) {
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, "settings")
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := retirement.NewSettingsManager(settingsDir, store)
	loader := dataloader.New(dir, store)

	cs := connect(t, NewServer(Deps{
		Settings:    sm,
		Loader:      loader,
		Store:       store,
		SettingsDir: settingsDir,
		SnapshotDir: filepath.Join(dir, "mcp-snapshots"),
		BaseURL:     "http://localhost:8080",
	}))

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{
		"list_scenarios", "get_analysis", "get_months", "run_scenario", "open_page", "apply_changes",
		"get_anomalies", "get_price_creep", "search_transactions", "summarize_spending", "get_recurring",
		"get_trends", "list_major_expenses", "list_exceptions", "pin_transactions", "upsert_major_expense",
		"delete_major_expense", "get_status", "list_data_files", "list_duplicates", "resolve_duplicates",
		"undo_resolve", "run_backup",
	} {
		if !got[want] {
			t.Errorf("tool %q not registered; got %v", want, toolNames(res.Tools))
		}
	}
	if len(res.Tools) != 23 {
		t.Errorf("expected exactly 23 tools, got %d: %v", len(res.Tools), toolNames(res.Tools))
	}
}

// TestNilBackupsDegradesRatherThanPanics guards the typed-nil seam between
// Deps.Backups (a concrete *backup.Service) and admin.Deps.Backups (an
// interface). Assigning a nil pointer straight across yields a NON-nil
// interface holding a nil pointer, which sails past admin's own
// `Backups == nil` guards and turns both tools into nil-pointer panics --
// worst for get_status, the tool the instructions bill as "the only one that
// still answers" when everything else is broken. The doc comment on NewServer
// promises a nil Backups is a supported degraded mode; this is what holds it
// to that.
func TestNilBackupsDegradesRatherThanPanics(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	cs := connect(t, NewServer(Deps{
		Settings:    retirement.NewSettingsManager(filepath.Join(dir, "settings"), store),
		Loader:      dataloader.New(dir, store),
		Store:       store,
		SettingsDir: filepath.Join(dir, "settings"),
		SnapshotDir: filepath.Join(dir, "mcp-snapshots"),
		Backups:     nil,
	}))
	ctx := context.Background()

	status, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_status"})
	if err != nil {
		t.Fatalf("CallTool get_status: %v", err)
	}
	if status.IsError {
		t.Fatalf("get_status failed with a nil backup service instead of degrading: %+v", status.Content)
	}
	if !strings.Contains(toolText(t, status), "no backup service is configured") {
		t.Errorf("get_status did not report the absent backup service; content = %s", toolText(t, status))
	}

	// run_backup cannot degrade -- there is nothing to run -- but it must fail
	// with its own named error, not a recovered panic.
	backup, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "run_backup"})
	if err != nil {
		t.Fatalf("CallTool run_backup: %v", err)
	}
	if !backup.IsError {
		t.Fatal("run_backup claimed success with no backup service configured")
	}
	msg := toolText(t, backup)
	if strings.Contains(msg, "panicked") {
		t.Errorf("run_backup panicked instead of reporting the missing service: %s", msg)
	}
	if !strings.Contains(msg, "no backup service is configured") {
		t.Errorf("run_backup error %q does not name the missing dependency", msg)
	}
}

// toolText flattens a tool result's content to a single string.
func toolText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// TestServerInstructionsCarryLoadBearingClaims pins the presence of every
// claim serverInstructions makes about tool behavior -- it is the model's
// closest thing to a system prompt, read on every connection, and nothing
// else catches it drifting out of sync with what the tools actually do (this
// is how the peer_group casing claim went stale in the first place). This is
// not a wording freeze: reword freely. The point is that an edit to a tool's
// actual behavior which invalidates one of these claims fails a test instead
// of silently misleading the model.
func TestServerInstructionsCarryLoadBearingClaims(t *testing.T) {
	for _, want := range []string{
		// apply_changes writes; run_scenario doesn't.
		"apply_changes writes to the saved plan",
		"run_scenario does not",
		// All six spend tools' sign conventions, by name -- the list is
		// claimed COMPLETE, so a seventh spend tool or a changed convention
		// must update this text (and this test) too.
		"search_transactions (expenses negative)",
		"get_anomalies (expenses negative)",
		"positive in get_price_creep and get_recurring",
		"MIXED in",
		"get_trends (current_amount/previous_amount are positive",
		"summarize_spending (total_expenses is always non-negative",
		"COMPLETE list of all six SPENDING tools",
		// Duplicate exclusion.
		"already resolved as duplicates",
		// Merchant-label rule: fuzzy grouping, lower-cased.
		"fuzzy grouping",
		"lower-cased",
		// The five curation tools: which read, which write, and the sign
		// split that differs from the spending tools' own.
		"five curation tools",
		"pin_transactions, upsert_major_expense and delete_major_expense WRITE",
		"`total` is NET SPEND and normally POSITIVE",
		"`amount` is SIGNED as stored",
		"two identical-looking transactions share one hash",
		"Only outflows are matched against major expenses",
		// The reversal asymmetry: unpin removes a pin but does not restore
		// whatever the transaction was pinned to before; delete_major_expense
		// restores a deleted expense; upsert never reverses. A model told
		// otherwise either refuses an undo it can perform or promises one it
		// cannot.
		"pin_transactions unpins when unpin is true",
		"only REMOVES the pin",
		"NOT restore whatever the transaction was pinned to before",
		"delete_major_expense restores a deleted expense when restore is true",
		"upsert_major_expense does NOT reverse",
		// The .bak recovery path is conditional, not guaranteed: a write
		// with nothing on disk yet has no prior state to protect, so no
		// backup is taken for it. A model told the backup is unconditional
		// could promise a recovery path that does not exist.
		"the .bak copy taken before its first change of a session, when there was prior data on disk to",
		"a write with nothing there yet to back up has no .bak, but also nothing to lose",
		// The six housekeeping tools: which read, which write, and the two
		// claims a behavior change would silently falsify.
		"six HOUSEKEEPING tools",
		"get_status is the one to call FIRST",
		"the only one that still answers",
		"do NOT sum to search_transactions' totals",
		"while a pair is unresolved BOTH sides are counted",
		"resolve_duplicates and undo_resolve WRITE TO THE USER'S DATA",
		"never invent a pair_key",
		// The reversal asymmetry between the two resolve_duplicates outcomes:
		// undoing kept_winner restores the suppressed transaction, but
		// undoing kept_both has nothing to restore because kept_both never
		// suppressed anything. A model told this is a uniform "exact inverse"
		// (the claim admin/undo.go itself walked back in 5c1325d) could
		// promise a restoration that kept_both never took away.
		"undoing kept_winner makes the suppressed transaction live again",
		"undoing kept_both only",
		"run_backup adds a zip to the backup directory and changes nothing else",
	} {
		if !strings.Contains(serverInstructions, want) {
			t.Errorf("serverInstructions no longer contains %q -- a tool's behavior may have changed "+
				"underneath a claim this text makes to the model; update serverInstructions (and this "+
				"test) to match", want)
		}
	}
}

func toolNames(tools []*mcp.Tool) []string {
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	return names
}
