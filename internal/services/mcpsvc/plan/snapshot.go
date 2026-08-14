package plan

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

// Snapshotter copies a scenario before this process first writes to it.
//
// Once per (process, scenario), not once per process: switching scenarios in
// the UI mid-conversation must back up the second plan too.
type Snapshotter struct {
	settingsDir string
	snapshotDir string

	mu   sync.Mutex
	done map[string]string // scenario -> snapshot path
}

func NewSnapshotter(settingsDir, snapshotDir string) *Snapshotter {
	return &Snapshotter{
		settingsDir: settingsDir,
		snapshotDir: snapshotDir,
		done:        make(map[string]string),
	}
}

// Ensure copies scenario to the snapshot directory if this process has not
// already done so, returning the snapshot path.
//
// It READS the source rather than linking it: followups §3 records that this
// server detects encryption in the wrong directory, so a blind copy of
// ciphertext would "succeed" and the caller's abort-before-write guarantee
// would not fire.
//
// Precondition: scenario must be a bare filename with no path separators or
// ".." segments (filepath.Base(scenario) == scenario, and it must not
// contain ".."). This is the only recovery path for an unwanted write to the
// user's real plan, so it validates its own input rather than trusting the
// caller: scenario is rejected, and nothing is read or written, before any
// file I/O happens. This mirrors
// retirement.SettingsManager.scenarioPath's validation.
func (s *Snapshotter) Ensure(scenario string, now time.Time) (string, error) {
	if err := validateScenarioName(scenario); err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if path, ok := s.done[scenario]; ok {
		return path, nil
	}

	src := filepath.Join(s.settingsDir, scenario)
	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("cannot snapshot %s before writing: %w", scenario, err)
	}

	if err := os.MkdirAll(s.snapshotDir, 0o755); err != nil {
		return "", fmt.Errorf("creating snapshot directory: %w", err)
	}

	dst := filepath.Join(s.snapshotDir,
		fmt.Sprintf("%s.%s.bak", scenario, now.UTC().Format(snapshotTimeLayout)))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", fmt.Errorf("writing snapshot %s: %w", dst, err)
	}

	s.done[scenario] = dst
	return dst, nil
}

// validateScenarioName rejects anything that is not a bare filename, so a
// malicious or mistaken scenario value can't escape settingsDir on the read
// side or snapshotDir on the write side via "../" traversal (or, on Windows,
// a "\"-separated equivalent).
func validateScenarioName(scenario string) error {
	if scenario == "" {
		return fmt.Errorf("scenario name is required")
	}
	if filepath.Base(scenario) != scenario || strings.Contains(scenario, "..") {
		return fmt.Errorf("invalid scenario name: %q", scenario)
	}
	if strings.ContainsAny(scenario, `/\`) {
		return fmt.Errorf("invalid scenario name: %q", scenario)
	}
	return nil
}
