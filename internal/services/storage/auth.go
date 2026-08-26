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

// saveConfig writes the encryption configuration to disk, atomically and
// durably.
//
// Atomic: the bytes land in a staging file that is renamed over the
// destination, so a concurrent reader never observes a half-written config.
//
// Durable: the staging file is fsynced before the rename, and the containing
// directory is fsynced after it. Both matter. os.WriteFile returns once the
// data is in the page cache, so a crash between the rename and the kernel's
// own flush can publish a config file of the right name and zero length —
// and this is the one file whose loss makes every encrypted file in the data
// directory unreadable, since it names the recipient the data was encrypted
// to. The directory fsync is what makes the rename itself survive the same
// crash.
//
// A staging file left behind by a crash is named <destination>.tmp, which
// IsStagingName already recognises (see legacyStagingSuffix), so a leftover
// is excluded from snapshots and restore-time pruning rather than mistaken
// for real data.
func saveConfig(baseDir string, config *EncryptionConfig) error {
	configPath := filepath.Join(baseDir, configFile)
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	tmpPath := configPath + legacyStagingSuffix
	if err := writeFileSync(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	// A staging file that never gets published would otherwise sit next to
	// the real config holding a recipient that was never adopted.
	defer func() { _ = os.Remove(tmpPath) }()

	if err := os.Rename(tmpPath, configPath); err != nil {
		return fmt.Errorf("failed to publish config: %w", err)
	}
	syncDir(baseDir)
	return nil
}

// writeFileSync writes data to path with the given permissions and flushes it
// to stable storage before returning. Unlike os.WriteFile it also chmods an
// existing file to perm, so a staging file orphaned by an earlier crash under
// a looser mode cannot silently widen the permissions of what replaces the
// config.
func writeFileSync(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
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
