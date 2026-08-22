package ledger

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/mcpsvc/confirm"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newDeps builds Deps over a temp directory with a real DataLoader, Storage
// and accounts sidecar, plus a confirm registry. Returns the data directory
// so tests can seed CSVs and assert on files.
func newDeps(t *testing.T) (Deps, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	loader := dataloader.New(dir, store)
	deps := Deps{
		Transactions: loader,
		Accounts:     NewAccountStore(store),
		Transfers:    loader,
		Store:        store,
		Snapshots:    snapshot.New(dir, filepath.Join(dir, "snapshots")),
		Confirm:      confirm.NewRegistry(5 * time.Minute),
	}
	return deps, dir
}

// seedAccounts writes the given accounts to the sidecar through the accounts
// service, so the tool reads what the /accounts page would.
func seedAccounts(t *testing.T, deps Deps, accts []models.Account) {
	t.Helper()
	if err := accounts.Save(deps.Store, accts); err != nil {
		t.Fatalf("accounts.Save: %v", err)
	}
}

// writeCSV writes a CSV file into the data directory.
func writeCSV(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// connect wires an assembled server to a client over the SDK's in-memory
// transport.
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

// connectAsking wires a client whose elicitation handler returns answer, and
// asserts mustSay appears in the prompt the human would see.
func connectAsking(t *testing.T, deps Deps, answer *mcp.ElicitResult, mustSay string) *mcp.ClientSession {
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

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			if !strings.Contains(req.Params.Message, mustSay) {
				t.Errorf("approval prompt does not mention %q: %q", mustSay, req.Params.Message)
			}
			return answer, nil
		},
	})
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

func approve() *mcp.ElicitResult {
	return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return res
}

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

// toolErrorText asserts res is an error result and returns its message.
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

// approve is used by tests that need the approved elicitation result; keep the
// helper referenced so the compiler does not drop it when a test is skipped.
var _ = approve
