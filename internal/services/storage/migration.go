package storage

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

// EnableEncryption encrypts all data files with the given password (default method)
func (s *Storage) EnableEncryption(password string) error {
	provider, err := NewPasswordProviderWithCredentials(password)
	if err != nil {
		return err
	}
	config := &EncryptionConfig{Method: AuthMethodPassword}
	return s.EnableEncryptionWithProvider(provider, config)
}

// EnableEncryptionWithProvider encrypts all data files using the given provider
func (s *Storage) EnableEncryptionWithProvider(provider AuthProvider, config *EncryptionConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.encrypted {
		return fmt.Errorf("encryption is already enabled")
	}

	// Get recipient and identity from provider
	recipient, err := provider.Recipient()
	if err != nil {
		return fmt.Errorf("failed to get recipient: %w", err)
	}

	identity, err := provider.Identity()
	if err != nil {
		return fmt.Errorf("failed to get identity: %w", err)
	}

	// Create verification file first
	verifyPath := filepath.Join(s.baseDir, verifyFile)
	encrypted, err := encryptData([]byte(verifyMagic), recipient)
	if err != nil {
		return fmt.Errorf("failed to encrypt verification file: %w", err)
	}
	if err := os.WriteFile(verifyPath, encrypted, 0644); err != nil {
		return fmt.Errorf("failed to write verification file: %w", err)
	}

	// Collect files to encrypt
	var filesToEncrypt []string
	err = filepath.Walk(s.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Skip files that shouldn't be encrypted
		if s.shouldSkipEncryption(path) {
			return nil
		}

		// Only encrypt CSV and JSON files
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".csv" || ext == ".json" {
			filesToEncrypt = append(filesToEncrypt, path)
		}

		return nil
	})
	if err != nil {
		// Cleanup verification file on error
		os.Remove(verifyPath)
		return fmt.Errorf("failed to scan files: %w", err)
	}

	// Encrypt each file
	for _, path := range filesToEncrypt {
		if err := s.encryptFileWithRecipient(path, recipient); err != nil {
			// Attempt to rollback encrypted files (best effort)
			s.rollbackEncryptionWithIdentity(filesToEncrypt, identity)
			os.Remove(verifyPath)
			return fmt.Errorf("failed to encrypt %s: %w", filepath.Base(path), err)
		}
	}

	// Save configuration
	if err := saveConfig(s.baseDir, config); err != nil {
		// Rollback on config save failure
		s.rollbackEncryptionWithIdentity(filesToEncrypt, identity)
		os.Remove(verifyPath)
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Create marker file
	markerPath := filepath.Join(s.baseDir, markerFile)
	if err := os.WriteFile(markerPath, []byte("encrypted"), 0644); err != nil {
		return fmt.Errorf("failed to create marker file: %w", err)
	}

	// Update storage state
	s.encrypted = true
	s.provider = provider
	s.config = config

	return nil
}

// DisableEncryption decrypts all data files using the current provider's credentials
func (s *Storage) DisableEncryption(credentials string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.encrypted {
		return fmt.Errorf("encryption is not enabled")
	}

	// Create provider based on config
	provider, err := s.createProviderUnlocked()
	if err != nil {
		return err
	}

	// Unlock if needed
	if provider.NeedsUnlock() {
		if err := provider.Unlock(credentials); err != nil {
			return err
		}
	}

	// Get identity for decryption
	identity, err := provider.Identity()
	if err != nil {
		return fmt.Errorf("failed to get identity: %w", err)
	}

	// Verify credentials
	verifyPath := filepath.Join(s.baseDir, verifyFile)
	encrypted, err := os.ReadFile(verifyPath)
	if err != nil {
		return fmt.Errorf("failed to read verification file: %w", err)
	}

	decrypted, err := decryptData(encrypted, identity)
	if err != nil {
		return ErrIncorrectCredentials
	}

	if string(decrypted) != verifyMagic {
		return ErrIncorrectCredentials
	}

	// Collect files to decrypt
	var filesToDecrypt []string
	err = filepath.Walk(s.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Check if file is encrypted
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip unreadable files
		}

		if isAgeEncrypted(data) {
			filesToDecrypt = append(filesToDecrypt, path)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to scan files: %w", err)
	}

	// Decrypt each file
	for _, path := range filesToDecrypt {
		if err := s.decryptFileWithIdentity(path, identity); err != nil {
			return fmt.Errorf("failed to decrypt %s: %w", filepath.Base(path), err)
		}
	}

	// Remove marker, verification, and config files
	os.Remove(filepath.Join(s.baseDir, markerFile))
	os.Remove(verifyPath)
	removeConfig(s.baseDir)

	// Update storage state
	s.encrypted = false
	s.provider = nil
	s.config = nil

	return nil
}

// createProviderUnlocked creates a provider without holding the mutex (caller must hold it)
func (s *Storage) createProviderUnlocked() (AuthProvider, error) {
	if s.config == nil {
		return NewPasswordProvider(), nil
	}

	switch s.config.Method {
	case AuthMethodPassword:
		return NewPasswordProvider(), nil
	case AuthMethodAge:
		return NewAgeProvider(s.config.AgeIdentityPath)
	case AuthMethodSSH:
		return NewSSHProvider(s.config.SSHKeyPath)
	case AuthMethodYubiKey:
		if s.config.YubiKeyRecipient != "" {
			return NewYubiKeyProviderWithRecipient(s.config.YubiKeyIdentity, s.config.YubiKeyRecipient)
		}
		return NewYubiKeyProvider(s.config.YubiKeyIdentity)
	default:
		return nil, fmt.Errorf("unknown auth method: %s", s.config.Method)
	}
}

// encryptFileWithRecipient encrypts a single file in place using any age.Recipient
func (s *Storage) encryptFileWithRecipient(path string, recipient age.Recipient) error {
	// Read original file
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Skip if already encrypted
	if isAgeEncrypted(data) {
		return nil
	}

	// Encrypt
	encrypted, err := encryptData(data, recipient)
	if err != nil {
		return err
	}

	// Write to temp file then atomic rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, encrypted, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

// decryptFileWithIdentity decrypts a single file in place using any age.Identity
func (s *Storage) decryptFileWithIdentity(path string, identity age.Identity) error {
	// Read encrypted file
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Skip if not encrypted
	if !isAgeEncrypted(data) {
		return nil
	}

	// Decrypt
	decrypted, err := decryptData(data, identity)
	if err != nil {
		return err
	}

	// Write to temp file then atomic rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, decrypted, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

// rollbackEncryptionWithIdentity attempts to decrypt files that were encrypted during a failed migration
func (s *Storage) rollbackEncryptionWithIdentity(files []string, identity age.Identity) {
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		if !isAgeEncrypted(data) {
			continue
		}

		decrypted, err := decryptData(data, identity)
		if err != nil {
			continue
		}

		if err := os.WriteFile(path, decrypted, 0644); err != nil {
			log.Printf("rollback: failed to restore %s: %v", path, err)
		}
	}
}
