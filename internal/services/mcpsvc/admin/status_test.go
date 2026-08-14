package admin

import (
	"strings"
	"testing"

	"budget2/internal/services/storage"
)

func TestGetStatusReportsAnUnencryptedStore(t *testing.T) {
	deps, dir := newDeps(t, nil)
	cs := connect(t, deps)

	out := decodeToolResult[statusOutput](t, call(t, cs, "get_status", map[string]any{}))

	if out.DataDir != dir {
		t.Errorf("data_dir = %q, want %q", out.DataDir, dir)
	}
	if out.Encrypted {
		t.Error("encrypted = true, want false for a plain temp dir")
	}
	if !out.Unlocked {
		t.Error("unlocked = false; an unencrypted store is always readable")
	}
	if out.AuthMethod != "" {
		t.Errorf("auth_method = %q, want empty when not encrypted", out.AuthMethod)
	}
	if out.Backup.Dir == "" {
		t.Error("backup.dir is empty, want the configured backup directory")
	}
	if out.Backup.LastBackupTS != "" {
		t.Errorf("backup.last_backup_ts = %q, want empty before any backup runs", out.Backup.LastBackupTS)
	}
	if out.UnresolvedDuplicates == nil {
		t.Fatal("unresolved_duplicates is null on an unlocked store; want a count")
	}
	if *out.UnresolvedDuplicates != 0 {
		t.Errorf("unresolved_duplicates = %d, want 0", *out.UnresolvedDuplicates)
	}
}

// TestGetStatusReportsAnEncryptedLockedStore exercises the entire reason
// get_status exists: an encrypted, locked store is exactly the state in
// which every other tool fails, and get_status must still answer -- reporting
// the lock itself, and reporting the counts it cannot read as JSON null
// rather than a lying 0. deps.Store is a concrete *storage.Storage (not an
// interface), so newDeps's real store can be encrypted and locked in place
// before connect, mirroring spend/register_test.go's
// TestDepsLoadReportsALockedStoreAsToolError.
func TestGetStatusReportsAnEncryptedLockedStore(t *testing.T) {
	deps, _ := newDeps(t, nil)
	if err := deps.Store.EnableEncryption("admin-status-test-password"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	deps.Store.Lock()

	cs := connect(t, deps)

	// decodeToolResult itself fails the test if the call came back as a tool
	// error -- asserting that success, not an error result, is the whole
	// point of this tool existing for a locked store.
	out := decodeToolResult[statusOutput](t, call(t, cs, "get_status", map[string]any{}))

	if !out.Encrypted {
		t.Error("encrypted = false, want true for a store with encryption enabled")
	}
	if out.Unlocked {
		t.Error("unlocked = true, want false immediately after Lock()")
	}
	if out.AuthMethod != string(storage.AuthMethodPassword) {
		t.Errorf("auth_method = %q, want %q", out.AuthMethod, storage.AuthMethodPassword)
	}
	if out.UnresolvedDuplicates != nil {
		t.Errorf("unresolved_duplicates = %d, want JSON null (nil pointer) on a locked store, not a count",
			*out.UnresolvedDuplicates)
	}
	if out.CSVFileCount != nil {
		t.Errorf("csv_file_count = %d, want JSON null (nil pointer) on a locked store, not a count",
			*out.CSVFileCount)
	}
	found := false
	for _, note := range out.Notes {
		if strings.Contains(note, "locked") && strings.Contains(note, "/unlock") {
			found = true
		}
	}
	if !found {
		t.Errorf("notes do not explain why the counts are null; got %v", out.Notes)
	}
}
