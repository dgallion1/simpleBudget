package whatifmcp

import (
	"fmt"
	"os"
	"path/filepath"
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
func (s *Snapshotter) Ensure(scenario string, now time.Time) (string, error) {
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
