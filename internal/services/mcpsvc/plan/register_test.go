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

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	return b
}
