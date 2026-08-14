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

// TestNewServerRegistersAllThirteenTools drives an in-memory client/server
// round trip to enumerate what NewServer actually registered, rather than
// merely asserting deps.Loader != nil is checked somewhere. Deps{} (a zero
// value) is deliberately NOT used here: with a nil Loader, NewServer's own
// "if deps.Loader != nil" guard skips spend.Register entirely, so a suite
// that only ever constructs NewServer(Deps{}) would stay green even if
// spend.Register were deleted from NewServer outright. A non-nil Loader (and
// the Settings/SettingsDir/SnapshotDir plan.Register needs) closes that hole.
func TestNewServerRegistersAllThirteenTools(t *testing.T) {
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
		"get_trends", "list_major_expenses",
	} {
		if !got[want] {
			t.Errorf("tool %q not registered; got %v", want, toolNames(res.Tools))
		}
	}
	if len(res.Tools) != 13 {
		t.Errorf("expected exactly 13 tools, got %d: %v", len(res.Tools), toolNames(res.Tools))
	}
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
		"COMPLETE list of all six",
		// Duplicate exclusion.
		"already resolved as duplicates",
		// Merchant-label rule: fuzzy grouping, lower-cased.
		"fuzzy grouping",
		"lower-cased",
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
