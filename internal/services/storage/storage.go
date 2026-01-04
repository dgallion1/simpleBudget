package storage

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"filippo.io/age"
)

const (
	// ageHeader is the prefix of Age-encrypted files
	ageHeader = "age-encryption.org"

	// markerFile indicates encryption is enabled
	markerFile = ".encrypted"

	// verifyFile is used to validate the password
	verifyFile = ".encryption-verify"

	// verifyMagic is the expected content in the verify file
	verifyMagic = `{"magic":"budget2-encryption-verify","version":1}`
)

// Storage provides transparent encrypted/unencrypted file access
type Storage struct {
	baseDir   string
	encrypted bool
	identity  *age.ScryptIdentity
	recipient *age.ScryptRecipient
	mu        sync.RWMutex
}

// New creates a new Storage instance for the given base directory
func New(baseDir string) (*Storage, error) {
	s := &Storage{
		baseDir: baseDir,
	}

	// Check if encryption is enabled
	markerPath := filepath.Join(baseDir, markerFile)
	if _, err := os.Stat(markerPath); err == nil {
		s.encrypted = true
	}

	return s, nil
}

// BaseDir returns the base directory
func (s *Storage) BaseDir() string {
	return s.baseDir
}

// IsEncrypted returns true if the data directory is encrypted
func (s *Storage) IsEncrypted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.encrypted
}

// IsUnlocked returns true if encryption is enabled and unlocked
func (s *Storage) IsUnlocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.encrypted || s.identity != nil
}

// Unlock decrypts the storage with the given password
func (s *Storage) Unlock(password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.encrypted {
		return nil // Nothing to unlock
	}

	// Create identity from password
	identity, err := age.NewScryptIdentity(password)
	if err != nil {
		return fmt.Errorf("failed to create identity: %w", err)
	}

	// Verify password by decrypting the verification file
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
		return fmt.Errorf("incorrect password (verification failed)")
	}

	// Password verified, store identity
	s.identity = identity
	s.recipient, _ = age.NewScryptRecipient(password)

	return nil
}

// Lock clears the encryption key from memory
func (s *Storage) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.identity = nil
	s.recipient = nil
}

// ReadFile reads and optionally decrypts a file
func (s *Storage) ReadFile(path string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Check if file is encrypted
	if isAgeEncrypted(data) {
		if s.identity == nil {
			return nil, fmt.Errorf("file is encrypted but storage is locked")
		}
		return decryptData(data, s.identity)
	}

	return data, nil
}

// WriteFile writes and optionally encrypts a file
func (s *Storage) WriteFile(path string, data []byte, perm os.FileMode) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Skip encryption for certain files
	if s.shouldSkipEncryption(path) {
		return s.atomicWrite(path, data, perm)
	}

	// Encrypt if enabled and unlocked
	if s.encrypted && s.recipient != nil {
		encrypted, err := encryptData(data, s.recipient)
		if err != nil {
			return fmt.Errorf("failed to encrypt: %w", err)
		}
		data = encrypted
	}

	return s.atomicWrite(path, data, perm)
}

// OpenFile returns a reader for a potentially encrypted file
func (s *Storage) OpenFile(path string) (io.ReadCloser, error) {
	data, err := s.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// atomicWrite writes data to a file atomically using a temp file
func (s *Storage) atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Write to temp file
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, perm); err != nil {
		return err
	}

	// Atomic rename
	return os.Rename(tmpPath, path)
}

// shouldSkipEncryption returns true for files that shouldn't be encrypted
func (s *Storage) shouldSkipEncryption(path string) bool {
	base := filepath.Base(path)

	// Skip marker and verify files
	if base == markerFile || base == verifyFile {
		return true
	}

	// Skip cache directory (e.g., plotly.min.js)
	if strings.Contains(path, "/cache/") || strings.Contains(path, "\\cache\\") {
		return true
	}

	return false
}

// isAgeEncrypted checks if data starts with the Age encryption header
func isAgeEncrypted(data []byte) bool {
	return len(data) > len(ageHeader) && string(data[:len(ageHeader)]) == ageHeader
}

// Stat returns file info, useful for checking existence
func (s *Storage) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// Glob returns files matching a pattern
func (s *Storage) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

// MkdirAll creates a directory and all parents
func (s *Storage) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// Remove removes a file
func (s *Storage) Remove(path string) error {
	return os.Remove(path)
}
