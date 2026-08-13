package whatifmcp

import (
	"context"
	"encoding/json"
	"testing"

	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestSource builds a Source over an empty temp directory. It is a
// trimmed stand-in for the pre-move helper that used to live in
// scenarios_test.go (now mcpsvc/plan/scenarios_test.go): insights_test.go
// only needs a settingsDir/store pair to construct a Source before
// overriding txSource -- it never exercises List/Load, so this fixture
// writes no scenario file. Deleted along with insights_test.go's use of it
// in Task 5.
func newTestSource(t *testing.T) *Source {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return NewSource(dir, store)
}

// connectInMemory connects srv and a fresh client over
// mcp.NewInMemoryTransports and registers cleanup to close both sessions.
// Copied from the deleted server_test.go for the same reason as the rest of
// this file.
func connectInMemory(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}

// decodeToolResult marshals a CallToolResult's StructuredContent back to
// JSON and unmarshals it into T. Copied from the deleted server_test.go for
// the same reason as the rest of this file.
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
// IsError set. Copied from the deleted server_test.go for the same reason
// as the rest of this file.
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
