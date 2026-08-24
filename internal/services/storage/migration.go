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
		_ = os.Remove(verifyPath)
		return fmt.Errorf("failed to scan files: %w", err)
	}

	// Encrypt each file
	for _, path := range filesToEncrypt {
		if err := s.encryptFileWithRecipient(path, recipient); err != nil {
			// Attempt to rollback encrypted files (best effort)
			s.rollbackEncryptionWithIdentity(filesToEncrypt, identity)
			_ = os.Remove(verifyPath)
			return fmt.Errorf("failed to encrypt %s: %w", filepath.Base(path), err)
		}
	}

	// Save configuration
	if err := saveConfig(s.baseDir, config); err != nil {
		// Rollback on config save failure
		s.rollbackEncryptionWithIdentity(filesToEncrypt, identity)
		_ = os.Remove(verifyPath)
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

	// Remove marker, verification, and config files (best effort)
	_ = os.Remove(filepath.Join(s.baseDir, markerFile))
	_ = os.Remove(verifyPath)
	_ = removeConfig(s.baseDir)

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

	// Invalidate before encrypting, not merely before staging. path is about
	// to stop being the plaintext ReadFileContext may have cached; this is the
	// confidentiality half of invalidateCache (see its doc), not the staleness
	// half -- the point is to shrink the window in which a decrypted copy of a
	// migrated file can be read back out of s.cache, not to protect a reader
	// from stale bytes.
	//
	// Above encryptData rather than below it, which is what the two blind arms
	// disagreed about: encryptData can fail, and on that path the later
	// placement returns with the plaintext still resident -- exactly the
	// residency this change exists to remove, at exactly the moment the
	// migration is going wrong. Costs one re-read on a path that is already
	// failing.
	s.invalidateCache(path)

	// Encrypt
	encrypted, err := encryptData(data, recipient)
	if err != nil {
		return err
	}

	// Stage beside the destination and publish by rename, via the same
	// atomicWrite routine ordinary writes use (see StagingSuffix's doc
	// comment on why consumers derive the staging name from one place
	// instead of each spelling out their own fixed ".tmp" convention).
	err = s.atomicWrite(path, encrypted, 0644)

	// Invalidate again now that the rename has published (or failed to
	// publish) the new bytes. Unconditional, same reasoning as
	// writeFileLocked: a failed rename leaves the file's state unproven, and
	// this is also what guarantees the pre-migration plaintext is evicted
	// from the map rather than left to be missed later by mtime/size.
	s.invalidateCache(path)
	return err
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

	// Invalidate before decrypting, for the same reason as
	// encryptFileWithRecipient: decryptData can fail, and the entry being
	// evicted already holds plaintext, so a failure below must not leave it
	// resident.
	s.invalidateCache(path)

	// Decrypt
	decrypted, err := decryptData(data, identity)
	if err != nil {
		return err
	}

	// Stage beside the destination and publish by rename, same convention as
	// encryptFileWithRecipient above.
	err = s.atomicWrite(path, decrypted, 0644)

	// Invalidate again after the rename lands (or fails). Unconditional, same
	// reasoning as encryptFileWithRecipient's second call.
	s.invalidateCache(path)
	return err
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

		// Same invalidate-before/invalidate-after bracket as
		// decryptFileWithIdentity, and above decryptData for the same reason:
		// a rollback is already the failure path, and a decrypt that fails
		// here must not leave the plaintext resident.
		s.invalidateCache(path)

		decrypted, err := decryptData(data, identity)
		if err != nil {
			continue
		}

		// Publish by rename via atomicWrite rather than writing the
		// destination directly. Renaming over path needs write permission on
		// its directory, not on path itself, so this also fixes rollback's
		// previous inability to publish (and silent swallowing of that
		// failure) when the destination file itself was not writable.
		if err := s.atomicWrite(path, decrypted, 0644); err != nil {
			log.Printf("rollback: failed to restore %s: %v", path, err)
		}

		s.invalidateCache(path)
	}
}
