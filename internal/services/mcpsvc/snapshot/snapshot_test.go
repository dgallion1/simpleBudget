package snapshot

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnapshotter_CopiesOncePerScenario(t *testing.T) {
	settingsDir := t.TempDir()
	snapDir := t.TempDir()
	content := []byte(`{"monthly_living_expenses":4000}`)
	if err := os.WriteFile(filepath.Join(settingsDir, "whatif.json"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(settingsDir, snapDir)
	now := time.Date(2026, 8, 9, 14, 22, 3, 0, time.UTC)

	first, err := s.Ensure("whatif.json", now)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	got, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("reading snapshot: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("snapshot is not byte-equal to the source")
	}
	if strings.Contains(filepath.Base(first), ":") {
		t.Errorf("filename %q contains a colon; it breaks extraction on Windows and exFAT", filepath.Base(first))
	}

	second, err := s.Ensure("whatif.json", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if second != first {
		t.Errorf("snapshotted the same scenario twice: %q then %q", first, second)
	}
}

func TestSnapshotter_SnapshotsEachScenarioSeparately(t *testing.T) {
	settingsDir := t.TempDir()
	snapDir := t.TempDir()
	for _, name := range []string{"whatif.json", "whatif-alt.json"} {
		if err := os.WriteFile(filepath.Join(settingsDir, name), []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := New(settingsDir, snapDir)
	now := time.Now()
	// Switching scenarios mid-conversation must back up the second plan too.
	a, err := s.Ensure("whatif.json", now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Ensure("whatif-alt.json", now)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two scenarios shared one snapshot")
	}
}

func TestSnapshotter_WritesOutsideTheSettingsDir(t *testing.T) {
	settingsDir := t.TempDir()
	snapDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(settingsDir, "whatif.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := New(settingsDir, snapDir).Ensure("whatif.json", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// backup.SkipPredicate does not exclude .bak, so a snapshot inside the data
	// directory would be swept into every backup zip from then on.
	rel, err := filepath.Rel(settingsDir, path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rel, "..") {
		t.Fatalf("snapshot %q is inside the settings dir %q", path, settingsDir)
	}
}

func TestSnapshotter_RejectsTraversalInScenarioName(t *testing.T) {
	for _, name := range []string{"../../etc/passwd", "..", "sub/whatif.json", ""} {
		t.Run(name, func(t *testing.T) {
			settingsDir := t.TempDir()
			snapDir := t.TempDir()

			_, err := New(settingsDir, snapDir).Ensure(name, time.Now())
			if err == nil {
				t.Fatalf("expected an error for scenario name %q", name)
			}

			entries, rerr := os.ReadDir(snapDir)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if len(entries) != 0 {
				t.Fatalf("snapshot dir should still be empty after a rejected scenario name, found: %v", entries)
			}
		})
	}
}

func TestSnapshotter_FailsWhenSourceMissing(t *testing.T) {
	settingsDir := t.TempDir()
	snapDir := t.TempDir()
	// No scenario file written: Ensure must fail so the caller aborts before
	// the POST rather than writing unbacked.
	if _, err := New(settingsDir, snapDir).Ensure("whatif.json", time.Now()); err == nil {
		t.Fatal("expected an error for a missing source file")
	}
}

// TestSnapshotter_FailsWhenSourceUnreadable covers the half of Ensure's
// abort-before-write guarantee the missing-file test cannot: the source
// EXISTS but its bytes cannot be read. Ensure must surface that error and
// leave no snapshot behind, so the caller refuses the write instead of
// proceeding on a backup that was never taken. (An encrypted store is NOT
// this case — ciphertext reads fine, and copying it verbatim is Ensure's
// documented behavior.)
func TestSnapshotter_FailsWhenSourceUnreadable(t *testing.T) {
	settingsDir := t.TempDir()
	snapDir := t.TempDir()

	// Swap the source file for an empty directory of the same name instead
	// of chmod 0o000. Ensure's first move is os.ReadFile(src); a directory
	// there fails that call with EISDIR at any uid, including root -- unlike
	// chmod 0000, which root's CAP_DAC_OVERRIDE lets it read straight
	// through. The path genuinely exists on disk throughout, matching the
	// test's premise that the source "exists but cannot be read," as
	// distinct from TestSnapshotter_FailsWhenSourceMissing above.
	srcPath := filepath.Join(settingsDir, "whatif.json")
	if err := os.Mkdir(srcPath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := New(settingsDir, snapDir).Ensure("whatif.json", time.Now())
	if err == nil {
		t.Fatal("expected an error for an unreadable source file")
	}
	if !strings.Contains(err.Error(), "whatif.json") {
		t.Errorf("error %q should name the unreadable source", err)
	}

	entries, rerr := os.ReadDir(snapDir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(entries) != 0 {
		t.Fatalf("snapshot dir should still be empty after a failed read, found: %v", entries)
	}
}
