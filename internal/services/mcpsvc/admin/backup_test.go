package admin

import (
	"path/filepath"
	"testing"
)

func TestRunBackupWritesAnArchive(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)

	out := decodeToolResult[runBackupOutput](t, call(t, cs, "run_backup", map[string]any{}))

	if !out.Ran {
		t.Fatalf("ran = false, note = %q", out.Note)
	}
	if out.TS == "" {
		t.Error("ts is empty; a successful backup records its timestamp")
	}
	if out.FileCount == 0 {
		t.Error("file_count = 0; the data directory had at least one CSV")
	}
	matches, err := filepath.Glob(filepath.Join(dir, "backups", "budget_backup_*.zip"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("archives on disk = %d, want 1", len(matches))
	}
	if out.Dir != filepath.Join(dir, "backups") {
		t.Errorf("dir = %q, want %q", out.Dir, filepath.Join(dir, "backups"))
	}
}

func TestRunBackupReportsAnInProgressSnapshot(t *testing.T) {
	deps, _ := newLiveDeps(t)
	deps.Backups = busyBackups{inner: deps.Backups}
	cs := connect(t, deps)

	out := decodeToolResult[runBackupOutput](t, call(t, cs, "run_backup", map[string]any{}))

	if out.Ran {
		t.Error("ran = true, want false when a snapshot is already in flight")
	}
	if out.Note == "" {
		t.Error("note is empty; a skipped backup must say why")
	}
}
