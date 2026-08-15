package restore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// plantArchive writes zip bytes into the service's backup directory under a
// real archive name and returns that name.
func plantArchive(t *testing.T, backupDir, stamp string, content []byte) string {
	t.Helper()
	name := "budget_backup_" + stamp + ".zip"
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, name), content, 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	return name
}

func TestFromArchiveRestoresANamedArchive(t *testing.T) {
	s, dir := newService(t)
	if err := os.WriteFile(filepath.Join(dir, "stale.csv"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed stale: %v", err)
	}
	name := plantArchive(t, s.deps.BackupDir, "20260801_030000", zipOf(t, map[string]string{
		"fresh.csv": "Date,Amount\n2024-01-01,-1.00\n",
	}))

	res, err := s.FromArchive(context.Background(), name)
	if err != nil {
		t.Fatalf("FromArchive: %v", err)
	}
	if res.Restored != 1 {
		t.Errorf("Restored = %d, want 1", res.Restored)
	}
	if res.Pruned != 1 {
		t.Errorf("Pruned = %d, want 1 (stale.csv)", res.Pruned)
	}
	got, err := os.ReadFile(filepath.Join(dir, "fresh.csv"))
	if err != nil {
		t.Fatalf("fresh.csv not restored: %v", err)
	}
	if string(got) != "Date,Amount\n2024-01-01,-1.00\n" {
		t.Errorf("fresh.csv = %q", got)
	}
}

// The name is the only attacker-controlled input on this path, so every
// shape that could address a file outside the backup directory is refused
// before anything is opened.
func TestFromArchiveRejectsNamesThatAreNotArchives(t *testing.T) {
	for _, name := range []string{
		"",
		"../../etc/passwd",
		"../budget_backup_20260801_030000.zip",
		"sub/budget_backup_20260801_030000.zip",
		"/etc/passwd",
		"notes.txt",
		"budget_backup_20260801_030000.zip.tmp",
	} {
		t.Run(name, func(t *testing.T) {
			s, dir := newService(t)
			_, err := s.FromArchive(context.Background(), name)
			if !errors.Is(err, ErrBadArchiveName) {
				t.Fatalf("err = %v, want ErrBadArchiveName", err)
			}
			entries, rerr := os.ReadDir(dir)
			if rerr != nil {
				t.Fatalf("readdir: %v", rerr)
			}
			if len(entries) != 0 {
				t.Errorf("data dir was touched by a rejected name: %v", entries)
			}
		})
	}
}

// A well-formed name for an archive that is not there is a different answer
// from a malformed one: the model can fix the first by re-listing.
func TestFromArchiveReportsAMissingArchive(t *testing.T) {
	s, _ := newService(t)
	_, err := s.FromArchive(context.Background(), "budget_backup_20260801_030000.zip")
	if !errors.Is(err, ErrNoSuchArchive) {
		t.Fatalf("err = %v, want ErrNoSuchArchive", err)
	}
}

func TestFromArchiveReportsAnUnreadableArchive(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode 0000 is still readable")
	}
	s, _ := newService(t)
	name := plantArchive(t, s.deps.BackupDir, "20260801_030000", []byte("whatever"))
	if err := os.Chmod(filepath.Join(s.deps.BackupDir, name), 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	_, err := s.FromArchive(context.Background(), name)
	if !errors.Is(err, ErrArchiveUnreadable) {
		t.Fatalf("err = %v, want ErrArchiveUnreadable", err)
	}
}

// A corrupt file under a valid name must surface as a bad archive, not as a
// successful no-op restore.
func TestFromArchiveRejectsAnArchiveThatIsNotAZip(t *testing.T) {
	s, _ := newService(t)
	name := plantArchive(t, s.deps.BackupDir, "20260801_030000", []byte("not a zip"))
	if _, err := s.FromArchive(context.Background(), name); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("err = %v, want ErrInvalidArchive", err)
	}
}

// An unconfigured backup directory must not resolve the name against the
// process working directory.
func TestFromArchiveWithoutABackupDirRefuses(t *testing.T) {
	s, _ := newService(t)
	s.deps.BackupDir = ""
	_, err := s.FromArchive(context.Background(), "budget_backup_20260801_030000.zip")
	if !errors.Is(err, ErrNoBackupDir) {
		t.Fatalf("err = %v, want ErrNoBackupDir", err)
	}
}
