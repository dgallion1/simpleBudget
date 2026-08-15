package admin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"budget2/internal/models"
	backupsvc "budget2/internal/services/backup"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var errNoLedger = errors.New("ledger unavailable")

type stubTransactions struct {
	ts  *models.TransactionSet
	err error
}

func (s stubTransactions) LoadData() (*models.TransactionSet, error) { return s.ts, s.err }

// newDeps builds Deps over a temp directory with a real DataLoader, Storage,
// SettingsManager and backup Service. Nothing here is stubbed except the
// ledger itself: these tools report on-disk state, so a fake store would test
// nothing. Returns the data directory so tests can assert on files.
func newDeps(t *testing.T, txns []models.Transaction) (Deps, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	loader := dataloader.New(dir, store)
	settingsDir := filepath.Join(dir, "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings: %v", err)
	}
	backupDir := filepath.Join(dir, "backups")
	svc, err := backupsvc.New(backupsvc.Config{BackupDir: backupDir, DataDir: dir, Store: store})
	if err != nil {
		t.Fatalf("backup.New: %v", err)
	}
	for i := range txns {
		if txns[i].Hash == "" {
			txns[i].Hash = txns[i].ComputeHash()
		}
	}
	return Deps{
		Transactions: stubTransactions{ts: models.NewTransactionSet(txns)},
		Files:        loader,
		Duplicates:   loader,
		Decisions:    loader,
		Store:        store,
		Settings:     retirement.NewSettingsManager(settingsDir, store),
		Backups:      svc,
		Snapshots:    snapshot.New(dir, filepath.Join(dir, "snapshots")),
	}, dir
}

// newLiveDeps builds Deps whose TransactionSource is the SAME real
// *dataloader.DataLoader as Duplicates, over a CSV containing one bill-pay →
// posted-check pair. The duplicate tools read state that only a real LoadData
// stamps, so a stubbed ledger cannot exercise them.
func newLiveDeps(t *testing.T) (Deps, string) {
	t.Helper()
	deps, dir := newDeps(t, nil)
	csv := "Date,Description,Amount,Status\n" +
		"2024-02-03,ACME INSURANCE BILL PAY,-250.00,Scheduled\n" +
		"2024-02-06,CHECK #1042,-250.00,Posted\n" +
		"2024-02-10,GROCERY STORE,-42.10,\n"
	if err := os.WriteFile(filepath.Join(dir, "checking.csv"), []byte(csv), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	loader, ok := deps.Files.(*dataloader.DataLoader)
	if !ok {
		t.Fatalf("Files is %T, want *dataloader.DataLoader", deps.Files)
	}
	deps.Transactions = loader
	return deps, dir
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

// busyBackups reports every snapshot as already in flight, the condition the
// real service signals when the scheduler tick and a manual run collide.
type busyBackups struct{ inner BackupService }

func (b busyBackups) BackupDir() string                  { return b.inner.BackupDir() }
func (b busyBackups) DataDir() string                    { return b.inner.DataDir() }
func (b busyBackups) Enabled() bool                      { return b.inner.Enabled() }
func (b busyBackups) Meta() (backupsvc.Meta, error)      { return b.inner.Meta() }
func (b busyBackups) Snapshot(ctx context.Context) error { return backupsvc.ErrSnapshotInProgress }

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
