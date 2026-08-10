package whatifmcp

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

	s := NewSnapshotter(settingsDir, snapDir)
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

	s := NewSnapshotter(settingsDir, snapDir)
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

	path, err := NewSnapshotter(settingsDir, snapDir).Ensure("whatif.json", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// backup.SkipPredicate does not exclude .bak, so a snapshot inside the data
	// directory would be swept into every backup zip from then on.
	rel, err := filepath.Rel(settingsDir, path)
	if err == nil && !strings.HasPrefix(rel, "..") {
		t.Fatalf("snapshot %q is inside the settings dir %q", path, settingsDir)
	}
}

func TestSnapshotter_FailsWhenSourceUnreadable(t *testing.T) {
	settingsDir := t.TempDir()
	snapDir := t.TempDir()
	// No scenario file written: Ensure must fail so the caller aborts before
	// the POST rather than writing unbacked.
	if _, err := NewSnapshotter(settingsDir, snapDir).Ensure("whatif.json", time.Now()); err == nil {
		t.Fatal("expected an error for a missing source file")
	}
}
