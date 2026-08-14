// Package snapshot copies a file to a backup directory before an MCP tool
// first writes to it, so an unwanted tool-driven change has a recovery path.
// It is a leaf package: mcpsvc/plan and mcpsvc/curate each construct their
// own instance pointed at their own source directory, and neither imports
// mcpsvc itself.
package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// snapshotTimeLayout deliberately avoids RFC3339: its colons survive Linux but
// break extraction on Windows and exFAT.
const snapshotTimeLayout = "2006-01-02T15-04-05Z"

// Snapshotter copies a file out of sourceDir before this process first writes
// to it.
//
// Once per (process, name), not once per process: a session that writes to
// two different files must back up both, and a session that writes to the
// same file twice keeps the copy of its ORIGINAL state rather than
// overwriting it with the state after the first change.
type Snapshotter struct {
	sourceDir   string
	snapshotDir string

	mu   sync.Mutex
	done map[string]string // name -> snapshot path
}

func New(sourceDir, snapshotDir string) *Snapshotter {
	return &Snapshotter{
		sourceDir:   sourceDir,
		snapshotDir: snapshotDir,
		done:        make(map[string]string),
	}
}

// Ensure copies sourceDir/name into the snapshot directory if this process
// has not already done so, returning the snapshot path.
//
// It copies the file's raw bytes with os.ReadFile rather than hard-linking
// it, so the backup is a point-in-time copy that a later in-place rewrite of
// the source cannot alter. When the store is encrypted the bytes are
// ciphertext, which is what a hand-restore back into that same store needs.
//
// A missing source file is an error, and the caller must abort its write on
// it: every caller snapshots BEFORE writing precisely so that a failure here
// prevents an unrecoverable change.
//
// Precondition: name must be a bare filename with no path separators or ".."
// segments (filepath.Base(name) == name, and it must not contain ".."). This
// is the only recovery path for an unwanted write to the user's real data, so
// it validates its own input rather than trusting the caller: name is
// rejected, and nothing is read or written, before any file I/O happens. This
// mirrors retirement.SettingsManager.scenarioPath's validation.
func (s *Snapshotter) Ensure(name string, now time.Time) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if path, ok := s.done[name]; ok {
		return path, nil
	}

	src := filepath.Join(s.sourceDir, name)
	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("cannot snapshot %s before writing: %w", name, err)
	}

	if err := os.MkdirAll(s.snapshotDir, 0o755); err != nil {
		return "", fmt.Errorf("creating snapshot directory: %w", err)
	}

	dst := filepath.Join(s.snapshotDir,
		fmt.Sprintf("%s.%s.bak", name, now.UTC().Format(snapshotTimeLayout)))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", fmt.Errorf("writing snapshot %s: %w", dst, err)
	}

	s.done[name] = dst
	return dst, nil
}

// validateName rejects anything that is not a bare filename, so a
// malicious or mistaken name value can't escape sourceDir on the read
// side or snapshotDir on the write side via "../" traversal (or, on Windows,
// a "\"-separated equivalent).
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("scenario name is required")
	}
	if filepath.Base(name) != name || strings.Contains(name, "..") {
		return fmt.Errorf("invalid scenario name: %q", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid scenario name: %q", name)
	}
	return nil
}
