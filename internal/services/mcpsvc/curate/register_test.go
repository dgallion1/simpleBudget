package curate

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubTransactions struct {
	ts  *models.TransactionSet
	err error
}

func (s stubTransactions) LoadData() (*models.TransactionSet, error) { return s.ts, s.err }

// newDeps builds Deps backed by a real DataLoader over a temp directory --
// the write tools' whole job is to change JSON files on disk, so stubbing the
// store would test nothing -- with the ledger itself stubbed, since building
// exact match groups through real CSV parsing would be indirect and brittle.
// It returns the data directory so tests can assert on the files.
func newDeps(t *testing.T, txns []models.Transaction) (Deps, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	loader := dataloader.New(dir, store)
	for i := range txns {
		if txns[i].Hash == "" {
			txns[i].Hash = txns[i].ComputeHash()
		}
	}
	return Deps{
		Transactions: stubTransactions{ts: models.NewTransactionSet(txns)},
		Expenses:     loader,
		Pins:         loader,
		Store:        store,
		Snapshots:    snapshot.New(dir, filepath.Join(dir, "snapshots")),
	}, dir
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
