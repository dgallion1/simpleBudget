package spend

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/storage"

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

// TestDepsLoadReportsALockedStoreAsToolError exercises Deps.load's locked-
// store branch with a real *storage.Storage, rather than stubbing it out --
// the branch's whole job is to translate storage's own locked state into a
// clear message ("...unlock it via the budget2 web UI (/unlock) first")
// instead of letting ciphertext surface as a parse failure, so it must be
// driven by a store that is actually encrypted and actually locked. Mirrors
// the pattern in cmd/server/mcp_mount_test.go's newLockedMCPRouter.
func TestDepsLoadReportsALockedStoreAsToolError(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	if err := store.EnableEncryption("mcpsvc-spend-test-password"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	store.Lock()

	// Transactions is a live stub that would otherwise succeed: the point of
	// this test is that Deps.load must refuse before ever reaching it.
	cs := connect(t, Deps{Transactions: stubTransactions{ts: &models.TransactionSet{}}, Store: store})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_anomalies",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	msg := toolErrorText(t, res)
	if !strings.Contains(msg, "/unlock") {
		t.Errorf("locked-store error must name /unlock as the recovery path, got: %s", msg)
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

// decodeToolResult marshals a CallToolResult's StructuredContent back to
// JSON and unmarshals it into T. Copied from the deleted whatifmcp/
// server_test.go for the same reason as connect above.
func decodeToolResult[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool call returned an error result: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal StructuredContent into %T: %v", out, err)
	}
	return out
}

// toolErrorText extracts the error message text from a tool result with
// IsError set. Copied from the deleted whatifmcp/server_test.go for the same
// reason as connect above.
func toolErrorText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if !res.IsError {
		t.Fatalf("expected an error result, got: %+v", res.StructuredContent)
	}
	if len(res.Content) == 0 {
		t.Fatal("error result has no content describing the failure")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("error content = %#v, want text", res.Content[0])
	}
	return text.Text
}
