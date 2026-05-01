package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"budget2/internal/services/storage"
)

// ErrSnapshotInProgress is returned by Snapshot when another snapshot is
// already running. Callers (the scheduler, the shutdown hook) treat it as
// a no-op rather than an error.
var ErrSnapshotInProgress = errors.New("backup: snapshot already in progress")

// Config is the dependency-injection bundle for the backup service.
type Config struct {
	BackupDir string
	DataDir   string
	Store     *storage.Storage // may be nil (storage not encrypted) — only used at restore time
	Clock     Clock            // optional; defaults to wall clock
}

// Service owns automatic backup snapshot, retention, and scheduling.
type Service struct {
	cfg   Config
	mu    sync.Mutex // serializes Snapshot
	clock Clock

	enabledMu sync.RWMutex
	enabled   bool
}

// New constructs a Service. It loads the persisted enabled flag from
// <DataDir>/settings/auto_backup.json (defaulting to true if absent).
func New(cfg Config) (*Service, error) {
	if cfg.BackupDir == "" {
		return nil, fmt.Errorf("backup: BackupDir required")
	}
	if cfg.DataDir == "" {
		return nil, fmt.Errorf("backup: DataDir required")
	}
	clk := cfg.Clock
	if clk == nil {
		clk = realClock{}
	}
	s := &Service{cfg: cfg, clock: clk, enabled: true}
	if err := s.loadEnabled(); err != nil {
		return nil, fmt.Errorf("backup: load enabled flag: %w", err)
	}
	return s, nil
}

func (s *Service) BackupDir() string { return s.cfg.BackupDir }
func (s *Service) DataDir() string   { return s.cfg.DataDir }

func (s *Service) Enabled() bool {
	s.enabledMu.RLock()
	defer s.enabledMu.RUnlock()
	return s.enabled
}

// SetEnabled persists the user's auto-backup toggle.
func (s *Service) SetEnabled(v bool) error {
	s.enabledMu.Lock()
	s.enabled = v
	s.enabledMu.Unlock()
	return s.saveEnabled(v)
}

type enabledFile struct {
	Enabled bool `json:"enabled"`
}

func (s *Service) enabledPath() string {
	return filepath.Join(s.cfg.DataDir, "settings", "auto_backup.json")
}

func (s *Service) loadEnabled() error {
	data, err := os.ReadFile(s.enabledPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil // default true
		}
		return err
	}
	var ef enabledFile
	if err := json.Unmarshal(data, &ef); err != nil {
		return err
	}
	s.enabledMu.Lock()
	s.enabled = ef.Enabled
	s.enabledMu.Unlock()
	return nil
}

func (s *Service) saveEnabled(v bool) error {
	if err := os.MkdirAll(filepath.Dir(s.enabledPath()), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(enabledFile{Enabled: v}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.enabledPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.enabledPath())
}

// tryLock attempts a non-blocking acquire of the snapshot mutex. Returns
// false if another snapshot is in flight.
func (s *Service) tryLock() bool {
	return s.mu.TryLock()
}

func (s *Service) unlock() { s.mu.Unlock() }
