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

// saveConfig writes the encryption configuration to disk
func saveConfig(baseDir string, config *EncryptionConfig) error {
	configPath := filepath.Join(baseDir, configFile)
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write atomically
	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return os.Rename(tmpPath, configPath)
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
