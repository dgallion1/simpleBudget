package admin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListDataFilesReportsEachCSV(t *testing.T) {
	deps, dir := newDeps(t, nil)
	csv := "Date,Description,Amount\n" +
		"2024-01-05,GROCERY STORE,-42.10\n" +
		"2024-03-20,HARDWARE STORE,-88.00\n"
	if err := os.WriteFile(filepath.Join(dir, "checking.csv"), []byte(csv), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[filesOutput](t, call(t, cs, "list_data_files", map[string]any{}))

	if out.Count != 1 {
		t.Fatalf("count = %d, want 1; files = %+v", out.Count, out.Files)
	}
	f := out.Files[0]
	if f.Name != "checking.csv" {
		t.Errorf("name = %q, want checking.csv", f.Name)
	}
	if !f.Enabled {
		t.Error("enabled = false; with no explicit selection every file is loaded")
	}
	if f.Transactions != 2 {
		t.Errorf("transactions = %d, want 2", f.Transactions)
	}
	if f.MinDate != "2024-01-05" || f.MaxDate != "2024-03-20" {
		t.Errorf("date coverage = %s..%s, want 2024-01-05..2024-03-20", f.MinDate, f.MaxDate)
	}
	if f.SizeBytes != int64(len(csv)) {
		t.Errorf("size_bytes = %d, want %d", f.SizeBytes, len(csv))
	}
}

func TestListDataFilesOnAnEmptyDirectoryIsNotAnError(t *testing.T) {
	deps, _ := newDeps(t, nil)
	cs := connect(t, deps)

	out := decodeToolResult[filesOutput](t, call(t, cs, "list_data_files", map[string]any{}))

	if out.Count != 0 {
		t.Errorf("count = %d, want 0", out.Count)
	}
	if out.Note == "" {
		t.Error("note is empty; an empty inventory must explain itself rather than look like a failure")
	}
}
