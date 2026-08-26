package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()

	// No config file should return nil, nil
	config, err := loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if config != nil {
		t.Error("expected nil config for missing file")
	}

	// Valid config file
	cfg := EncryptionConfig{
		Method:     AuthMethodPassword,
		SSHKeyPath: "/some/path",
	}
	data, _ := json.Marshal(cfg)
	configPath := filepath.Join(dir, configFile)
	os.WriteFile(configPath, data, 0600)

	config, err = loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if config == nil {
		t.Fatal("expected non-nil config")
	}
	if config.Method != AuthMethodPassword {
		t.Errorf("expected %s, got %s", AuthMethodPassword, config.Method)
	}
	if config.SSHKeyPath != "/some/path" {
		t.Errorf("expected /some/path, got %s", config.SSHKeyPath)
	}

	// Invalid JSON
	os.WriteFile(configPath, []byte("not json"), 0600)
	_, err = loadConfig(dir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSaveConfig(t *testing.T) {
	dir := t.TempDir()

	cfg := &EncryptionConfig{
		Method:          AuthMethodAge,
		AgeIdentityPath: "/path/to/key",
	}

	if err := saveConfig(dir, cfg); err != nil {
		t.Fatalf("saveConfig failed: %v", err)
	}

	// Verify it was saved
	loaded, err := loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if loaded.Method != AuthMethodAge {
		t.Errorf("expected %s, got %s", AuthMethodAge, loaded.Method)
	}
	if loaded.AgeIdentityPath != "/path/to/key" {
		t.Errorf("expected /path/to/key, got %s", loaded.AgeIdentityPath)
	}
}

func TestRemoveConfig(t *testing.T) {
	dir := t.TempDir()

	// Remove when file doesn't exist should not error
	if err := removeConfig(dir); err != nil {
		t.Fatalf("removeConfig failed for missing file: %v", err)
	}

	// Create and then remove
	cfg := &EncryptionConfig{Method: AuthMethodPassword}
	saveConfig(dir, cfg)

	configPath := filepath.Join(dir, configFile)
	if _, err := os.Stat(configPath); err != nil {
		t.Fatal("config file should exist after save")
	}

	if err := removeConfig(dir); err != nil {
		t.Fatalf("removeConfig failed: %v", err)
	}

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("config file should be removed")
	}
}

// TestSaveConfigStagingModeAndNoLeftover defends saveConfig's staging
// rework (T5): the published config must land at 0600, and nothing named
// by the StagingSuffix convention (or any other name) may remain in
// baseDir once the save completes.
func TestSaveConfigStagingModeAndNoLeftover(t *testing.T) {
	dir := t.TempDir()
	cfg := &EncryptionConfig{Method: AuthMethodPassword}
	if err := saveConfig(dir, cfg); err != nil {
		t.Fatalf("saveConfig failed: %v", err)
	}

	configPath := filepath.Join(dir, configFile)
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("Stat config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("config mode = %o, want 0600", perm)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != configFile {
		t.Fatalf("expected baseDir to contain only %q after save, got %v", configFile, entries)
	}
	for _, e := range entries {
		if IsStagingName(e.Name()) {
			t.Errorf("staging leftover after a completed save: %s", e.Name())
		}
	}
}

// TestSaveConfigOrphanRecognitionAndSweep pins the coverage claim in the T5
// report: after this change, saveConfig's staging name is one
// storage.IsStagingName recognises, so a crash-orphaned staging file left
// by this function is distinguishable from real data by any consumer
// walking baseDir -- notably backup.SkipPredicate, which walks DataDir
// (== baseDir in production; see cmd/server/main.go, where both
// storage.New and backupsvc.New are handed cfg.DataDirectory) to exclude
// staging leftovers from backup zips and to protect them from restore's
// stale-file prune. This package cannot import backup directly to call
// SkipPredicate itself (backup already imports storage, so that would be
// an import cycle), so this test exercises the same recognition primitive
// SkipPredicate is built on -- IsStagingName -- and performs the sweep a
// consumer would with it: plant both the new convention's name and the
// pre-fix legacy ".tmp" name (still recognised, for orphans that predate
// this change) and confirm both are identified and removed while the real
// config survives.
func TestSaveConfigOrphanRecognitionAndSweep(t *testing.T) {
	dir := t.TempDir()

	// A real, saved config -- must never be treated as staging.
	if err := saveConfig(dir, &EncryptionConfig{Method: AuthMethodPassword}); err != nil {
		t.Fatalf("saveConfig failed: %v", err)
	}

	// Plant two orphans: one in this function's new convention, one in the
	// pre-fix fixed name it replaces.
	newOrphan := filepath.Join(dir, configFile+StagingSuffix+"1234567890")
	if err := os.WriteFile(newOrphan, []byte("stale"), 0600); err != nil {
		t.Fatalf("plant new-convention orphan: %v", err)
	}
	legacyOrphan := filepath.Join(dir, configFile+".tmp")
	if err := os.WriteFile(legacyOrphan, []byte("stale"), 0600); err != nil {
		t.Fatalf("plant legacy orphan: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if !IsStagingName(filepath.Base(newOrphan)) {
		t.Errorf("IsStagingName(%q) = false, want true (new convention)", filepath.Base(newOrphan))
	}
	if !IsStagingName(filepath.Base(legacyOrphan)) {
		t.Errorf("IsStagingName(%q) = false, want true (legacy convention)", filepath.Base(legacyOrphan))
	}
	if IsStagingName(configFile) {
		t.Errorf("IsStagingName(%q) = true, want false for the real config name", configFile)
	}

	// Simulate the sweep a real consumer (backup.SkipPredicate's caller)
	// performs with IsStagingName: remove anything it recognises.
	for _, e := range entries {
		if IsStagingName(e.Name()) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				t.Errorf("sweep remove %s: %v", e.Name(), err)
			}
		}
	}

	if _, err := os.Stat(newOrphan); !os.IsNotExist(err) {
		t.Errorf("new-convention orphan survived the sweep: err=%v", err)
	}
	if _, err := os.Stat(legacyOrphan); !os.IsNotExist(err) {
		t.Errorf("legacy orphan survived the sweep: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, configFile)); err != nil {
		t.Errorf("real config did not survive the sweep: %v", err)
	}
}

// TestSaveConfigStagingCreateFailureLeavesNoLitter forces saveConfig's
// os.CreateTemp call to fail with a kernel-enforced ENAMETOOLONG rather
// than a permission bit, so the failure is identical for uid 0 (this
// suite's root container) and an ordinary user -- there is nothing here
// for root's CAP_DAC_OVERRIDE to bypass (mirrors
// TestRollbackDecryptionReportsPathOnAtomicWriteFailure in
// migration_decrypt_rollback_test.go, which does the same thing to
// atomicWrite's staging name via an overlong destination basename).
// configFile is a fixed package constant this test cannot lengthen, so
// instead of blowing NAME_MAX on one path component, this test blows
// PATH_MAX on the whole path: baseDir itself is built long enough (but
// still under PATH_MAX, so it is creatable) that appending
// configFile+StagingSuffix+<random suffix> pushes the full staging path
// over PATH_MAX.
func TestSaveConfigStagingCreateFailureLeavesNoLitter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH_MAX arithmetic below assumes POSIX path length limits; windows differs")
	}

	const pathMax = 4096
	// target sits between two thresholds: above pathMax-len(configFile)-1
	// (4072), so appending plain configFile still fits, the destination
	// path stays statable, and a would-be leftover config would still be
	// checkable -- but above pathMax-len(configFile+StagingSuffix)-10-1
	// (4057; the 10 is os.CreateTemp's random decimal suffix), so
	// appending configFile+StagingSuffix+<random suffix> does not fit and
	// os.CreateTemp fails with ENAMETOOLONG.
	const target = 4065

	root := t.TempDir()
	dir := root
	for len(dir) < target {
		remaining := target - len(dir) - 1 // -1 for the path separator filepath.Join adds
		if remaining <= 0 {
			break
		}
		segLen := remaining
		if segLen > 200 {
			segLen = 200
		}
		dir = filepath.Join(dir, strings.Repeat("a", segLen))
	}
	if len(dir) >= pathMax {
		t.Fatalf("test bug: baseDir itself is %d bytes, want < %d so it is creatable", len(dir), pathMax)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll setup failed (unexpected on this filesystem): %v", err)
	}
	stagingLen := len(dir) + 1 + len(configFile+StagingSuffix) + 10
	if stagingLen <= pathMax {
		t.Fatalf("test bug: computed staging path length %d does not exceed PATH_MAX %d", stagingLen, pathMax)
	}

	err := saveConfig(dir, &EncryptionConfig{Method: AuthMethodPassword})
	if err == nil {
		t.Fatal("expected saveConfig to fail when the staging path exceeds PATH_MAX")
	}

	configPath := filepath.Join(dir, configFile)
	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Errorf("destination config must not exist after a failed save: stat err=%v", statErr)
	}

	entries, rdErr := os.ReadDir(dir)
	if rdErr != nil {
		t.Fatalf("ReadDir: %v", rdErr)
	}
	for _, e := range entries {
		t.Errorf("staging litter left behind after a failed save: %s", e.Name())
	}
}

// TestSaveConfigRenameFailureLeavesNoLitter forces saveConfig's staging
// file to be created, written, and chmod'd successfully, and only the
// final os.Rename to fail: the destination is pre-created as a
// directory, and os.Rename can never replace an existing directory with a
// regular file (EISDIR/ENOTDIR) -- a kernel invariant, not a permission
// check, so it fails identically for uid 0 and an ordinary user. This
// exercises the error-path cleanup that TestSaveConfigStagingCreateFailure
// LeavesNoLitter cannot: that test fails before a staging file ever
// exists, so it cannot catch a dropped `defer os.Remove(tmpPath)` --
// this one can, because here the staging file is created and only its
// cleanup after the failed rename is in question.
func TestSaveConfigRenameFailureLeavesNoLitter(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, configFile)
	if err := os.Mkdir(configPath, 0700); err != nil {
		t.Fatalf("Mkdir setup: %v", err)
	}

	err := saveConfig(dir, &EncryptionConfig{Method: AuthMethodPassword})
	if err == nil {
		t.Fatal("expected saveConfig to fail when the destination is a directory")
	}

	info, statErr := os.Stat(configPath)
	if statErr != nil || !info.IsDir() {
		t.Fatalf("destination must remain the untouched directory after a failed rename: stat err=%v, isDir=%v", statErr, info != nil && info.IsDir())
	}

	entries, rdErr := os.ReadDir(dir)
	if rdErr != nil {
		t.Fatalf("ReadDir: %v", rdErr)
	}
	for _, e := range entries {
		if e.Name() == configFile {
			continue // the pre-existing directory itself, untouched
		}
		t.Errorf("staging litter left behind after a failed rename: %s", e.Name())
	}
}

func TestGetAuthMethod(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Not encrypted, should return empty
	if m := s.GetAuthMethod(); m != "" {
		t.Errorf("expected empty, got %s", m)
	}

	// Set config
	s.config = &EncryptionConfig{Method: AuthMethodSSH}
	if m := s.GetAuthMethod(); m != AuthMethodSSH {
		t.Errorf("expected %s, got %s", AuthMethodSSH, m)
	}
}

func TestGetConfig(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// No config
	if c := s.GetConfig(); c != nil {
		t.Error("expected nil config")
	}

	// With config
	s.config = &EncryptionConfig{
		Method:     AuthMethodAge,
		SSHKeyPath: "/test",
	}

	c := s.GetConfig()
	if c == nil {
		t.Fatal("expected non-nil config")
	}

	// Should be a copy
	c.SSHKeyPath = "/modified"
	if s.config.SSHKeyPath != "/test" {
		t.Error("GetConfig should return a copy")
	}
}
