package whatifmcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/services/storage"
)

// TestSource_Transactions_ProductionWiringReadsRealCSVDirectory proves the
// production (txSource == nil) path actually reads CSVs, not just that the
// mock path works: it lays out <dir>/data/settings (the settings dir, same
// shape cmd/whatif-mcp/main.go's resolveDataDir produces) alongside
// <dir>/data/one.csv (the sibling CSV directory cmd/server's own DataLoader
// reads), builds a Source the same way NewSource always is, and asserts
// Transactions() loads the CSV's row -- confirming the settingsDir-parent
// relationship this package relies on elsewhere (live.go's spawnArgs derives
// BUDGET_DATA_DIR the same way).
func TestSource_Transactions_ProductionWiringReadsRealCSVDirectory(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	settingsDir := filepath.Join(dataDir, "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}

	csv := "Date,Description,Amount,Category\n2024-01-15,Coffee Shop,-4.50,Dining\n"
	if err := os.WriteFile(filepath.Join(dataDir, "one.csv"), []byte(csv), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	store, err := storage.New(settingsDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	src := NewSource(settingsDir, store)

	ts, err := src.Transactions()
	if err != nil {
		t.Fatalf("Transactions() error: %v", err)
	}
	if ts.Len() != 1 {
		t.Fatalf("Transactions() loaded %d rows, want 1: %+v", ts.Len(), ts.Transactions)
	}
	if ts.Transactions[0].Description != "Coffee Shop" {
		t.Errorf("Description = %q, want %q", ts.Transactions[0].Description, "Coffee Shop")
	}
}

// TestSource_Transactions_MissingDataDirectoryIsAClearError asserts a
// settings dir whose parent does not exist on disk fails with a message
// naming the missing directory, rather than dataloader.LoadData's own
// silent "no CSV files found" empty-result behavior (which would otherwise
// mask a genuinely broken deployment as "no anomalies here").
func TestSource_Transactions_MissingDataDirectoryIsAClearError(t *testing.T) {
	root := t.TempDir()
	// settingsDir's parent (root/does-not-exist) is never created.
	settingsDir := filepath.Join(root, "does-not-exist", "settings")

	store, err := storage.New(settingsDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	src := NewSource(settingsDir, store)

	_, err = src.Transactions()
	if err == nil {
		t.Fatal("Transactions() with a missing data directory should error, got nil")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should name the missing directory, got: %v", err)
	}
}

// TestSource_Transactions_NonSettingsBasenameIsAClearError is the
// regression case for the demonstrated defect: a settingsDir NOT named
// ".../settings" (e.g. a custom -data flag value) must never let Transactions
// silently guess a parent directory. Before the fix, filepath.Dir(settingsDir)
// on "/tmp/x/notsettings" resolves to "/tmp/x", which exists but holds no
// CSVs -- dataloader.LoadData then returns an empty, non-error result, so
// get_anomalies/get_price_creep would confidently report "count: 0" instead
// of surfacing the misconfiguration. This asserts an error instead, naming
// the offending settingsDir, exactly as spawnArgs already refuses the
// identical shape for a different purpose (deriving BUDGET_DATA_DIR).
func TestSource_Transactions_NonSettingsBasenameIsAClearError(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "notsettings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settingsDir: %v", err)
	}
	// The parent DOES exist and even has an unrelated CSV in it, so a
	// pre-fix filepath.Dir(settingsDir) guess would "succeed" with an empty
	// or wrong result instead of erroring -- this is what makes the defect
	// silent rather than obviously broken.
	if err := os.WriteFile(filepath.Join(root, "unrelated.csv"), []byte("Date,Description,Amount\n2024-01-01,X,-1\n"), 0o644); err != nil {
		t.Fatalf("write unrelated csv: %v", err)
	}

	store, err := storage.New(settingsDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	src := NewSource(settingsDir, store)

	_, err = src.Transactions()
	if err == nil {
		t.Fatal("Transactions() with a settingsDir not named \"settings\" should error, got nil")
	}
	if !strings.Contains(err.Error(), settingsDir) {
		t.Errorf("error should name the offending settings dir %q, got: %v", settingsDir, err)
	}
	if !strings.Contains(err.Error(), "settings") {
		t.Errorf("error should explain the expected <data-dir>/settings shape, got: %v", err)
	}
}

// TestSource_Transactions_StandardSettingsShapeStillWorks is the happy-path
// companion to the regression test above: the ordinary <dataDir>/settings
// layout must keep working after the basename guard is added.
func TestSource_Transactions_StandardSettingsShapeStillWorks(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	settingsDir := filepath.Join(dataDir, "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "one.csv"), []byte("Date,Description,Amount\n2024-01-01,X,-1\n"), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	store, err := storage.New(settingsDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	src := NewSource(settingsDir, store)

	ts, err := src.Transactions()
	if err != nil {
		t.Fatalf("Transactions() error: %v", err)
	}
	if ts.Len() != 1 {
		t.Fatalf("Transactions() loaded %d rows, want 1", ts.Len())
	}
}

// TestSource_Transactions_EmptyDataDirectoryReturnsEmptySetNoError asserts a
// data directory that exists but holds no CSV files is NOT an error -- it is
// indistinguishable from "no transactions yet", matching dataloader.LoadData's
// own documented behavior for that case.
func TestSource_Transactions_EmptyDataDirectoryReturnsEmptySetNoError(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	settingsDir := filepath.Join(dataDir, "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}

	store, err := storage.New(settingsDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	src := NewSource(settingsDir, store)

	ts, err := src.Transactions()
	if err != nil {
		t.Fatalf("Transactions() error: %v", err)
	}
	if ts.Len() != 0 {
		t.Errorf("Transactions() = %d rows, want 0", ts.Len())
	}
}
