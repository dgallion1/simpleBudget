package main

import (
	"budget2/internal/config"
	"budget2/internal/services/storage"
	"budget2/internal/testutil"
	"context"
	"crypto/sha256"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDI5SavedDataUnchanged(t *testing.T) {
	dataRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataRoot, "synthetic.csv"), []byte("Date,Description,Amount,Category\n2026-08-28,Synthetic Grocer,-10,Groceries\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root := testutil.ProjectRoot()
	cfg := &config.Config{ListenAddr: ":0", Debug: true, DataDirectory: dataRoot, UploadsDirectory: filepath.Join(dataRoot, "uploads"), SettingsDirectory: filepath.Join(dataRoot, "settings"), TemplatesDirectory: filepath.Join(root, "web/templates"), StaticDirectory: filepath.Join(root, "web/static"), BackupDir: t.TempDir(), ImportDirectory: t.TempDir()}
	var err error
	store, err = storage.New(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetupDependencies(cfg); err != nil {
		t.Fatal(err)
	}
	router := SetupRouter()
	snapshot := func() map[string][32]byte {
		result := map[string][32]byte{}
		err := filepath.WalkDir(dataRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[path] = sha256.Sum256(data)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	if len(before) == 0 {
		t.Fatal("empty saved-data fixture")
	}
	st, ct := mcp.NewInMemoryTransports()
	ss, err := mcpServer.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "checker", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for i := 0; i < 3; i++ {
		result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "refresh_pages", Arguments: map[string]any{}})
		if err != nil || result.IsError {
			t.Fatalf("call %v %v", result, err)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/api/ui-refresh", nil))
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("saved data changed")
	}
	t.Logf("saved-data SHA256 and file inventory unchanged across 3 actual MCP refresh calls and route reads: %d files", len(before))
}
