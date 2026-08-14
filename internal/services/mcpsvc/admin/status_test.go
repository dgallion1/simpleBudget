package admin

import "testing"

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
