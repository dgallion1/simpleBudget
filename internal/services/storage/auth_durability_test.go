package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The fsync calls saveConfig makes are not observable from Go — there is no
// portable way to ask whether a write reached stable storage. What these
// tests pin down is everything around them that a reader can observe, and
// that a naive "add an f.Sync()" edit could get wrong: the staging file is
// published rather than left beside the config, its permissions do not leak
// in from a crash leftover, and a failure to publish leaves the previous
// config intact.

// saveConfig must leave nothing behind: the staging file is renamed onto the
// config, so a directory listing afterwards shows the config and no
// <config>.tmp. Drop the rename (or the deferred Remove) and this fails.
func TestSaveConfigLeavesNoStagingFile(t *testing.T) {
	dir := t.TempDir()

	if err := saveConfig(dir, &EncryptionConfig{Method: AuthMethodPassword, RecipientID: "age1test"}); err != nil {
		t.Fatalf("saveConfig failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, configFile+legacyStagingSuffix)); !os.IsNotExist(err) {
		t.Errorf("staging file still present after saveConfig (stat err = %v)", err)
	}

	got, err := loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if got == nil || got.RecipientID != "age1test" {
		t.Errorf("config did not round-trip: %+v", got)
	}
}

// The published config must be 0600 even when a staging file orphaned by an
// earlier crash is sitting there under a looser mode. os.OpenFile's perm
// argument only applies on create, so without the explicit Chmod in
// writeFileSync the rename would publish a world-readable file naming the
// recipient the data is encrypted to.
func TestSaveConfigTightensLeftoverStagingPerms(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, configFile+legacyStagingSuffix)

	if err := os.WriteFile(tmpPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("planting staging file: %v", err)
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		t.Fatalf("chmod staging file: %v", err)
	}

	if err := saveConfig(dir, &EncryptionConfig{Method: AuthMethodAge, RecipientID: "age1leftover"}); err != nil {
		t.Fatalf("saveConfig failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, configFile))
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("config perm = %o, want 600", perm)
	}
}

// The staging leftover saveConfig can produce is named <config>.tmp, which is
// the legacy staging form. IsStagingName has to recognise it, or a crashed
// save would put a stray file into every backup snapshot and have restore's
// prune treat it as user data.
func TestSaveConfigStagingNameIsRecognisedAsStaging(t *testing.T) {
	if !IsStagingName(configFile + legacyStagingSuffix) {
		t.Errorf("IsStagingName(%q) = false, want true", configFile+legacyStagingSuffix)
	}
}

// A save that cannot publish must not damage the config already on disk. The
// rename is made to fail by turning the destination into a non-empty
// directory, which is the one case rename refuses to replace.
func TestSaveConfigFailedPublishKeepsPreviousConfig(t *testing.T) {
	dir := t.TempDir()

	original := &EncryptionConfig{Method: AuthMethodPassword, RecipientID: "age1original"}
	if err := saveConfig(dir, original); err != nil {
		t.Fatalf("initial saveConfig failed: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, configFile))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	// Replace the config with a non-empty directory of the same name: the
	// bytes are gone, but rename onto it now fails with ENOTEMPTY, which is
	// the publish failure this test is after.
	configPath := filepath.Join(dir, configFile)
	if err := os.Remove(configPath); err != nil {
		t.Fatalf("removing config: %v", err)
	}
	if err := os.Mkdir(configPath, 0755); err != nil {
		t.Fatalf("mkdir over config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configPath, "occupant"), before, 0600); err != nil {
		t.Fatalf("occupying config dir: %v", err)
	}

	if err := saveConfig(dir, &EncryptionConfig{Method: AuthMethodAge, RecipientID: "age1replacement"}); err == nil {
		t.Fatal("saveConfig succeeded despite an unpublishable destination")
	}

	// The failed attempt must not have littered the data directory.
	if _, err := os.Stat(filepath.Join(dir, configFile+legacyStagingSuffix)); !os.IsNotExist(err) {
		t.Errorf("staging file survived a failed publish (stat err = %v)", err)
	}

	// And the bytes it would have replaced are untouched.
	after, err := os.ReadFile(filepath.Join(configPath, "occupant"))
	if err != nil {
		t.Fatalf("reading occupant: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("previous config bytes changed by a failed save")
	}

	var parsed EncryptionConfig
	if err := json.Unmarshal(before, &parsed); err != nil {
		t.Fatalf("previous config is not valid JSON: %v", err)
	}
	if parsed.RecipientID != "age1original" {
		t.Errorf("previous config recipient = %q, want age1original", parsed.RecipientID)
	}
}
