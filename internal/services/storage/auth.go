package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"filippo.io/age"
)

// AuthMethod represents the type of authentication being used
type AuthMethod string

const (
	AuthMethodPassword AuthMethod = "password"
	AuthMethodSSH      AuthMethod = "ssh"
	AuthMethodAge      AuthMethod = "age"
	AuthMethodYubiKey  AuthMethod = "yubikey"

	// configFile stores encryption configuration
	configFile = ".encryption-config.json"
)

// EncryptionConfig stores the encryption configuration
type EncryptionConfig struct {
	Method      AuthMethod `json:"method"`
	RecipientID string     `json:"recipient_id"` // Public key / recipient identifier

	// Method-specific configuration
	SSHKeyPath       string `json:"ssh_key_path,omitempty"`
	AgeIdentityPath  string `json:"age_identity_path,omitempty"`
	YubiKeyIdentity  string `json:"yubikey_identity,omitempty"`
	YubiKeyRecipient string `json:"yubikey_recipient,omitempty"` // age1yubikey1... recipient
}

// AuthProvider abstracts different authentication methods
type AuthProvider interface {
	// Method returns the authentication method type
	Method() AuthMethod

	// Identity returns the age.Identity for decryption
	Identity() (age.Identity, error)

	// Recipient returns the age.Recipient for encryption
	Recipient() (age.Recipient, error)

	// NeedsUnlock returns true if this method requires a password/passphrase
	NeedsUnlock() bool

	// IsUnlocked returns true if the provider is ready for encryption/decryption
	IsUnlocked() bool

	// Unlock provides credentials for methods that need them
	Unlock(credentials string) error

	// Lock clears any cached credentials
	Lock()

	// DisplayInfo returns human-readable info about the auth method
	DisplayInfo() string
}

// loadConfig reads the encryption configuration from disk
func loadConfig(baseDir string) (*EncryptionConfig, error) {
	configPath := filepath.Join(baseDir, configFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No config file
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config EncryptionConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

// saveConfig writes the encryption configuration to disk.
//
// Staging follows the same convention as atomicWrite/createExclusive in
// storage.go — a uniquely named temp file under StagingSuffix, written,
// chmod'd, then renamed over the destination — but this function cannot
// call atomicWrite itself, because atomicWrite is a *Storage method that
// writes through Storage's encrypting path. This file, the encryption
// config, is the one file that must never be encrypted (it is what makes
// decryption possible in the first place), and it must be writable before
// a *Storage even finishes constructing (EnableEncryptionWithProvider
// calls saveConfig while still assembling encryption state). Duplicating
// the staging convention here rather than routing through Storage keeps
// this file out of the encrypting path while still producing a staging
// name that the regime's orphan recognition (IsStagingName) and backup
// exclusion (backup.SkipPredicate, which walks baseDir/DataDir) know
// about — unlike the old fixed ".tmp" name this replaces.
func saveConfig(baseDir string, config *EncryptionConfig) error {
	configPath := filepath.Join(baseDir, configFile)
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Staged in baseDir (same filesystem as configPath, so the rename below
	// is atomic) under a unique name so concurrent saves cannot collide on
	// the staging file itself — mirrors atomicWrite/createExclusive (see
	// StagingSuffix's doc comment in storage.go). The staging file is
	// fsync'd before the rename, and baseDir itself is fsync'd after, via
	// the same fileSync/syncDir seams atomicWrite and createExclusive use
	// (storage.go) — this file is the encryption config, and losing it to a
	// crash makes every other encrypted file unreadable.
	f, err := os.CreateTemp(baseDir, configFile+StagingSuffix+"*")
	if err != nil {
		return fmt.Errorf("failed to create staging file: %w", err)
	}
	tmpPath := f.Name()
	// If the rename below succeeds this is a no-op (nothing left at
	// tmpPath); if we return early on an error this cleans up the staging
	// file so a failed save doesn't litter baseDir (mirrors atomicWrite's
	// error hygiene).
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to write config: %w", err)
	}
	if err := fileSync(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to write config: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	// os.CreateTemp already creates the file at 0600, but chmod explicitly
	// to match atomicWrite's approach rather than relying on the default.
	if err := os.Chmod(tmpPath, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	if err := os.Rename(tmpPath, configPath); err != nil {
		return err
	}
	return syncDir(baseDir)
}

// removeConfig deletes the encryption configuration file
func removeConfig(baseDir string) error {
	configPath := filepath.Join(baseDir, configFile)
	err := os.Remove(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// GetAuthMethod returns the current authentication method or empty if not encrypted
func (s *Storage) GetAuthMethod() AuthMethod {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.config != nil {
		return s.config.Method
	}
	return ""
}

// GetConfig returns a copy of the current encryption configuration
func (s *Storage) GetConfig() *EncryptionConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.config == nil {
		return nil
	}

	// Return a copy
	config := *s.config
	return &config
}
