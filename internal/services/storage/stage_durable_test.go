package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// atomicWrite and createExclusive both publish through stageDurable now, so
// the staging step is one piece of code with one set of rules. fsync is not
// observable from Go, so these tests cover what is: that the staging file
// carries the requested mode before it is published (the chmod goes through
// the open handle so the fsync covers it, which is easy to reorder wrongly),
// and that a failure never leaves a staging file behind next to real data.

// os.CreateTemp always creates 0600. A publisher asked for a wider mode has
// to widen it, or every file written through Storage comes out owner-only.
func TestStageDurableAppliesTheRequestedMode(t *testing.T) {
	dir := t.TempDir()
	for _, perm := range []os.FileMode{0600, 0644, 0640} {
		tmpPath, err := stageDurable(dir, "probe.json", []byte("x"), perm)
		if err != nil {
			t.Fatalf("stageDurable(%o): %v", perm, err)
		}
		info, err := os.Stat(tmpPath)
		if err != nil {
			t.Fatalf("stat staging file: %v", err)
		}
		if got := info.Mode().Perm(); got != perm {
			t.Errorf("staging file mode = %o, want %o", got, perm)
		}
		if !IsStagingName(filepath.Base(tmpPath)) {
			t.Errorf("staging name %q is not recognised by IsStagingName", filepath.Base(tmpPath))
		}
		os.Remove(tmpPath)
	}
}

// When the staging file cannot even be created, stageDurable reports the
// failure with an empty path — which the callers' deferred os.Remove("")
// tolerates — and nothing is left in the directory.
//
// The failures that happen after CreateTemp succeeds (a write, chmod or fsync
// hitting a disk error) return the staging path so the caller's deferred
// Remove cleans it up; that path is covered by the existing
// TestCreateExclusiveLeavesNoStagingFileBehind, which exercises the same
// deferred removal via a failed publish. Provoking a mid-write disk error
// portably would need fault injection in production code, which is why
// cleanup lives at the caller rather than in a branch only such an error
// could reach.
func TestStageDurableLeavesNothingBehindOnFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	tmpPath, err := stageDurable(dir, "probe.json", []byte("x"), 0644)
	if err == nil {
		t.Fatalf("stageDurable succeeded in a read-only directory (tmpPath=%q)", tmpPath)
	}
	if tmpPath != "" {
		t.Errorf("stageDurable returned a path (%q) alongside an error", tmpPath)
	}

	_ = os.Chmod(dir, 0700)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		t.Errorf("failed staging left %q behind", e.Name())
	}
}

// The mode the caller asks for is the mode that lands, including when
// atomicWrite is replacing a file that had a different one. Rewrite
// semantics: the destination's old mode must not survive the rename.
func TestAtomicWritePublishesWithTheRequestedMode(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(dir, "notes.json")

	if err := os.WriteFile(path, []byte("old"), 0666); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatalf("chmod seed: %v", err)
	}

	if err := s.atomicWrite(path, []byte("new"), 0640); err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0640 {
		t.Errorf("published mode = %o, want 640 (the old 666 leaked through)", got)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new" {
		t.Errorf("content = %q err = %v, want \"new\"", got, err)
	}
	assertNoStagingLeftovers(t, dir)
}

// Same for the create-if-absent publisher, which links rather than renames.
func TestCreateExclusivePublishesWithTheRequestedMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "once.json")

	if err := createExclusive(path, []byte("first"), 0640); err != nil {
		t.Fatalf("createExclusive: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0640 {
		t.Errorf("published mode = %o, want 640", got)
	}
	assertNoStagingLeftovers(t, dir)

	// A refused second create must not disturb the first, nor litter.
	if err := createExclusive(path, []byte("second"), 0600); !errors.Is(err, os.ErrExist) {
		t.Errorf("second createExclusive err = %v, want ErrExist", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "first" {
		t.Errorf("content = %q err = %v, want \"first\"", got, err)
	}
	assertNoStagingLeftovers(t, dir)
}

func assertNoStagingLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if IsStagingName(e.Name()) || strings.Contains(e.Name(), StagingSuffix) {
			t.Errorf("staging file %q left in the data directory", e.Name())
		}
	}
}
