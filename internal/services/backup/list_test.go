package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newListService builds a Service over an empty temp backup dir.
func newListService(t *testing.T) (*Service, string) {
	t.Helper()
	backupDir := t.TempDir()
	dataDir := t.TempDir()
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc, backupDir
}

// writeArchive plants a file named the way a real snapshot would name it.
func writeArchive(t *testing.T, dir string, ts time.Time, body string) string {
	t.Helper()
	name := backupNamePrefix + ts.UTC().Format(archiveStampLayout) + backupNameSuffix
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write archive %s: %v", name, err)
	}
	return name
}

// Newest-first is the contract, not an accident of the filesystem: a caller
// asking for "the most recent backup" takes element zero.
func TestListReturnsArchivesNewestFirst(t *testing.T) {
	svc, dir := newListService(t)
	oldest := writeArchive(t, dir, time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC), "old")
	middle := writeArchive(t, dir, time.Date(2026, 6, 1, 3, 0, 0, 0, time.UTC), "mid")
	newest := writeArchive(t, dir, time.Date(2026, 8, 1, 3, 0, 0, 0, time.UTC), "new!!")

	got, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List returned %d archives, want 3: %+v", len(got), got)
	}
	want := []string{newest, middle, oldest}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("archive[%d].Name = %q, want %q", i, got[i].Name, w)
		}
	}
	if got[0].Bytes != int64(len("new!!")) {
		t.Errorf("Bytes = %d, want %d", got[0].Bytes, len("new!!"))
	}
	if want := time.Date(2026, 8, 1, 3, 0, 0, 0, time.UTC); !got[0].TS.Equal(want) {
		t.Errorf("TS = %v, want %v", got[0].TS, want)
	}
	// The name is what a restore caller passes back, so it must carry no
	// directory component.
	if filepath.Base(got[0].Name) != got[0].Name {
		t.Errorf("Name = %q, want a bare filename", got[0].Name)
	}
}

func TestListIgnoresFilesThatAreNotArchives(t *testing.T) {
	svc, dir := newListService(t)
	real := writeArchive(t, dir, time.Date(2026, 8, 1, 3, 0, 0, 0, time.UTC), "z")
	for _, junk := range []string{
		"notes.txt",
		"budget_backup_NOT_A_DATE.zip",
		"budget_backup_20260801_030000.zip.tmp", // an in-flight snapshot
		"some_other_backup.zip",
	} {
		if err := os.WriteFile(filepath.Join(dir, junk), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", junk, err)
		}
	}

	got, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != real {
		t.Fatalf("List = %+v, want only %s", got, real)
	}
}

func TestListOnAnEmptyDirectoryIsEmptyNotAnError(t *testing.T) {
	svc, _ := newListService(t)
	got, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List = %+v, want empty", got)
	}
}

func TestValidArchiveName(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"budget_backup_20260801_030000.zip", true},
		{"", false},
		{"budget_backup_20260801_030000.zip.tmp", false},
		{"budget_backup_NOT_A_DATE.zip", false},
		{"budget_backup_20260801_030000.txt", false},
		{"anything_else.zip", false},
		// Traversal, in every shape a caller could send it.
		{"../budget_backup_20260801_030000.zip", false},
		{"../../etc/passwd", false},
		{"sub/budget_backup_20260801_030000.zip", false},
		{"/tmp/budget_backup_20260801_030000.zip", false},
		{`..\budget_backup_20260801_030000.zip`, false},
		{".", false},
		{"..", false},
	} {
		if got := ValidArchiveName(tc.name); got != tc.want {
			t.Errorf("ValidArchiveName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
