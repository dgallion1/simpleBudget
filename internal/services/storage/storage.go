package storage

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// cacheEntry holds cached decrypted file content
type cacheEntry struct {
	data    []byte
	modTime int64
}

// Storage provides transparent encrypted/unencrypted file access
type Storage struct {
	baseDir   string
	encrypted bool
	provider  AuthProvider      // Current auth provider (nil if locked or not encrypted)
	config    *EncryptionConfig // Configuration for the auth method
	mu        sync.RWMutex
	cache     map[string]*cacheEntry
	cacheMu   sync.RWMutex
}

// New creates a new Storage instance for the given base directory
func New(baseDir string) (*Storage, error) {
	s := &Storage{
		baseDir: baseDir,
		cache:   make(map[string]*cacheEntry),
	}

	// Check if encryption is enabled
	markerPath := filepath.Join(baseDir, markerFile)
	if _, err := os.Stat(markerPath); err == nil {
		s.encrypted = true

		// Load encryption configuration
		config, err := loadConfig(baseDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load encryption config: %w", err)
		}

		// Default to password if no config exists (backward compatibility)
		if config == nil {
			config = &EncryptionConfig{Method: AuthMethodPassword}
		}
		s.config = config
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

// IsUnlocked returns true if encryption is not enabled or provider is unlocked
func (s *Storage) IsUnlocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.encrypted || (s.provider != nil && s.provider.IsUnlocked())
}

// Unlock decrypts the storage with the given credentials
// For password method, credentials is the password
// For SSH with passphrase, credentials is the passphrase
// For age identity and YubiKey, credentials may be empty
func (s *Storage) Unlock(credentials string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.encrypted {
		return nil // Nothing to unlock
	}

	// Create or get provider based on config
	provider, err := s.createProvider()
	if err != nil {
		return err
	}

	// Unlock the provider with credentials
	if provider.NeedsUnlock() {
		if err := provider.Unlock(credentials); err != nil {
			return err
		}
	}

	// Get identity for verification
	identity, err := provider.Identity()
	if err != nil {
		log.Printf("Storage unlock failed: could not get identity from provider: %v", err)
		return fmt.Errorf("failed to get identity: %w", err)
	}

	// Verify credentials by decrypting the verification file
	verifyPath := filepath.Join(s.baseDir, verifyFile)
	encrypted, err := os.ReadFile(verifyPath)
	if err != nil {
		log.Printf("Storage unlock failed: could not read verification file: %v", err)
		return fmt.Errorf("failed to read verification file: %w", err)
	}

	decrypted, err := decryptData(encrypted, identity)
	if err != nil {
		log.Printf("Storage unlock failed: incorrect credentials")
		provider.Lock()
		return fmt.Errorf("incorrect credentials")
	}

	if string(decrypted) != verifyMagic {
		log.Printf("Storage unlock failed: verification magic mismatch")
		provider.Lock()
		return fmt.Errorf("incorrect credentials (verification failed)")
	}

	// Credentials verified, store provider
	log.Printf("Storage unlocked successfully")
	s.provider = provider
	return nil
}

// createProvider creates an AuthProvider based on the current configuration
func (s *Storage) createProvider() (AuthProvider, error) {
	if s.config == nil {
		// Default to password for backward compatibility
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

// Lock clears the encryption key and cached data from memory
func (s *Storage) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.provider != nil {
		s.provider.Lock()
		s.provider = nil
	}

	// Clear cache for security - don't keep decrypted data in memory
	s.cacheMu.Lock()
	s.cache = make(map[string]*cacheEntry)
	s.cacheMu.Unlock()
}

// ReadFile reads and optionally decrypts a file, using cache when possible
func (s *Storage) ReadFile(path string) ([]byte, error) {
	// Get file modification time
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	modTime := info.ModTime().UnixNano()

	// Check cache first
	s.cacheMu.RLock()
	if entry, ok := s.cache[path]; ok && entry.modTime == modTime {
		// Cache hit - return copy to prevent mutation
		data := make([]byte, len(entry.data))
		copy(data, entry.data)
		s.cacheMu.RUnlock()
		return data, nil
	}
	s.cacheMu.RUnlock()

	// Cache miss - read from disk
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Check if file is encrypted
	if isAgeEncrypted(data) {
		if s.provider == nil || !s.provider.IsUnlocked() {
			return nil, fmt.Errorf("file is encrypted but storage is locked")
		}
		identity, err := s.provider.Identity()
		if err != nil {
			return nil, fmt.Errorf("failed to get identity: %w", err)
		}
		data, err = decryptData(data, identity)
		if err != nil {
			log.Printf("Warning: failed to decrypt %s: %v", path, err)
			return nil, fmt.Errorf("decryption failed for %s: %w", path, err)
		}
	}

	// Store in cache
	s.cacheMu.Lock()
	s.cache[path] = &cacheEntry{data: data, modTime: modTime}
	s.cacheMu.Unlock()

	// Return a copy to prevent mutation
	result := make([]byte, len(data))
	copy(result, data)
	return result, nil
}

// WriteFile writes and optionally encrypts a file
func (s *Storage) WriteFile(path string, data []byte, perm os.FileMode) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Invalidate cache for this path
	s.cacheMu.Lock()
	delete(s.cache, path)
	s.cacheMu.Unlock()

	// Skip encryption for certain files
	if s.shouldSkipEncryption(path) {
		return s.atomicWrite(path, data, perm)
	}

	// Encrypt if enabled and unlocked
	if s.encrypted && s.provider != nil && s.provider.IsUnlocked() {
		recipient, err := s.provider.Recipient()
		if err != nil {
			return fmt.Errorf("failed to get recipient: %w", err)
		}
		encrypted, err := encryptData(data, recipient)
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

// Remove removes a file and invalidates its cache entry
func (s *Storage) Remove(path string) error {
	s.cacheMu.Lock()
	delete(s.cache, path)
	s.cacheMu.Unlock()
	return os.Remove(path)
}
