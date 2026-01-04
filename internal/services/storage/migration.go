package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

// EnableEncryption encrypts all data files with the given password
func (s *Storage) EnableEncryption(password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.encrypted {
		return fmt.Errorf("encryption is already enabled")
	}

	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}

	// Create recipient and identity from password
	recipient, err := age.NewScryptRecipient(password)
	if err != nil {
		return fmt.Errorf("failed to create recipient: %w", err)
	}

	identity, err := age.NewScryptIdentity(password)
	if err != nil {
		return fmt.Errorf("failed to create identity: %w", err)
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
		if err := s.encryptFile(path, recipient); err != nil {
			// Attempt to rollback encrypted files (best effort)
			s.rollbackEncryption(filesToEncrypt, identity)
			os.Remove(verifyPath)
			return fmt.Errorf("failed to encrypt %s: %w", filepath.Base(path), err)
		}
	}

	// Create marker file
	markerPath := filepath.Join(s.baseDir, markerFile)
	if err := os.WriteFile(markerPath, []byte("encrypted"), 0644); err != nil {
		return fmt.Errorf("failed to create marker file: %w", err)
	}

	// Update storage state
	s.encrypted = true
	s.identity = identity
	s.recipient = recipient

	return nil
}

// DisableEncryption decrypts all data files (requires current password)
func (s *Storage) DisableEncryption(password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.encrypted {
		return fmt.Errorf("encryption is not enabled")
	}

	// Verify password
	identity, err := age.NewScryptIdentity(password)
	if err != nil {
		return fmt.Errorf("failed to create identity: %w", err)
	}

	verifyPath := filepath.Join(s.baseDir, verifyFile)
	encrypted, err := os.ReadFile(verifyPath)
	if err != nil {
		return fmt.Errorf("failed to read verification file: %w", err)
	}

	decrypted, err := decryptData(encrypted, identity)
	if err != nil {
		return fmt.Errorf("incorrect password")
	}

	if string(decrypted) != verifyMagic {
		return fmt.Errorf("incorrect password")
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
		if err := s.decryptFile(path, identity); err != nil {
			return fmt.Errorf("failed to decrypt %s: %w", filepath.Base(path), err)
		}
	}

	// Remove marker and verification files
	os.Remove(filepath.Join(s.baseDir, markerFile))
	os.Remove(verifyPath)

	// Update storage state
	s.encrypted = false
	s.identity = nil
	s.recipient = nil

	return nil
}

// encryptFile encrypts a single file in place
func (s *Storage) encryptFile(path string, recipient *age.ScryptRecipient) error {
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

// decryptFile decrypts a single file in place
func (s *Storage) decryptFile(path string, identity *age.ScryptIdentity) error {
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

// rollbackEncryption attempts to decrypt files that were encrypted during a failed migration
func (s *Storage) rollbackEncryption(files []string, identity *age.ScryptIdentity) {
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

		os.WriteFile(path, decrypted, 0644)
	}
}
