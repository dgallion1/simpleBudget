package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// enableAgeEncryptionForTest sets up age-based encryption on s using a freshly
// generated identity file. The storage is left in the unlocked state.
func enableAgeEncryptionForTest(t *testing.T, s *Storage) {
	t.Helper()
	idPath, _ := createTestAgeIdentityFile(t, s.baseDir)
	provider, err := NewAgeProvider(idPath)
	if err != nil {
		t.Fatalf("NewAgeProvider failed: %v", err)
	}
	config := &EncryptionConfig{Method: AuthMethodAge, AgeIdentityPath: idPath}
	if err := s.EnableEncryptionWithProvider(provider, config); err != nil {
		t.Fatalf("EnableEncryptionWithProvider failed: %v", err)
	}
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Write unencrypted file
	testFile := filepath.Join(dir, "test.csv")
	original := []byte("Date,Description,Amount\n2024-01-01,Test,100.00\n")

	if err := store.WriteFile(testFile, original, 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Verify unencrypted content
	read, err := store.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if string(read) != string(original) {
		t.Errorf("Content mismatch before encryption")
	}

	// Enable encryption
	password := "testpassword123"
	if err := store.EnableEncryption(password); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	if !store.IsEncrypted() {
		t.Error("Expected IsEncrypted() to return true")
	}

	// Verify file is encrypted on disk
	rawData, _ := os.ReadFile(testFile)
	if !isAgeEncrypted(rawData) {
		t.Error("File should be encrypted on disk")
	}

	// Read should still return original content (decrypted)
	read, err = store.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read encrypted file: %v", err)
	}
	if string(read) != string(original) {
		t.Errorf("Content mismatch after encryption: got %q, want %q", string(read), string(original))
	}

	// Lock and unlock
	store.Lock()
	if err := store.Unlock(password); err != nil {
		t.Fatalf("Failed to unlock: %v", err)
	}

	// Read again after unlock
	read, err = store.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read after unlock: %v", err)
	}
	if string(read) != string(original) {
		t.Errorf("Content mismatch after unlock")
	}

	// Disable encryption
	if err := store.DisableEncryption(password); err != nil {
		t.Fatalf("Failed to disable encryption: %v", err)
	}

	if store.IsEncrypted() {
		t.Error("Expected IsEncrypted() to return false after disable")
	}

	// Verify file is decrypted on disk
	rawData, _ = os.ReadFile(testFile)
	if isAgeEncrypted(rawData) {
		t.Error("File should be decrypted on disk")
	}
	if string(rawData) != string(original) {
		t.Errorf("Raw content mismatch after decryption")
	}
}

func TestWrongPassword(t *testing.T) {
	dir := t.TempDir()
	store, _ := New(dir)

	// Write a test file
	testFile := filepath.Join(dir, "test.json")
	if err := store.WriteFile(testFile, []byte(`{"test": true}`), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Enable encryption
	if err := store.EnableEncryption("correctpassword"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	// Lock
	store.Lock()

	// Try wrong password
	err := store.Unlock("wrongpassword")
	if err == nil {
		t.Error("Expected error with wrong password")
	}
}

func TestPasswordTooShort(t *testing.T) {
	dir := t.TempDir()
	store, _ := New(dir)

	err := store.EnableEncryption("short")
	if err == nil {
		t.Error("Expected error for short password")
	}
}

func TestSkipCacheFiles(t *testing.T) {
	dir := t.TempDir()
	store, _ := New(dir)

	// Create cache directory
	cacheDir := filepath.Join(dir, "cache")
	os.MkdirAll(cacheDir, 0755)

	// Write a cache file
	cacheFile := filepath.Join(cacheDir, "plotly.min.js")
	content := []byte("// plotly.js content")
	if err := store.WriteFile(cacheFile, content, 0644); err != nil {
		t.Fatalf("Failed to write cache file: %v", err)
	}

	// Enable encryption
	if err := store.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	// Cache file should NOT be encrypted
	rawData, _ := os.ReadFile(cacheFile)
	if isAgeEncrypted(rawData) {
		t.Error("Cache file should not be encrypted")
	}
	if string(rawData) != string(content) {
		t.Error("Cache file content should be unchanged")
	}
}

func TestNewFilesEncrypted(t *testing.T) {
	dir := t.TempDir()
	store, _ := New(dir)

	// Enable encryption first
	if err := store.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	// Write a new file - should be encrypted
	newFile := filepath.Join(dir, "new.csv")
	content := []byte("Date,Amount\n2024-01-01,100\n")
	if err := store.WriteFile(newFile, content, 0644); err != nil {
		t.Fatalf("Failed to write new file: %v", err)
	}

	// Verify it's encrypted on disk
	rawData, _ := os.ReadFile(newFile)
	if !isAgeEncrypted(rawData) {
		t.Error("New file should be encrypted on disk")
	}

	// But ReadFile should return decrypted content
	read, err := store.ReadFile(newFile)
	if err != nil {
		t.Fatalf("Failed to read new file: %v", err)
	}
	if string(read) != string(content) {
		t.Errorf("Content mismatch: got %q, want %q", string(read), string(content))
	}
}

func TestWriteFile_PassesThroughAgeEncryptedBytes(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	enableAgeEncryptionForTest(t, s)

	// Build a payload that is ALREADY age-encrypted using the package's
	// internal encryptData with the provider's recipient.
	plaintext := []byte("hello round-trip")
	recipient, err := s.provider.Recipient()
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := encryptData(plaintext, recipient)
	if err != nil {
		t.Fatal(err)
	}

	// Write the already-encrypted bytes via WriteFile.
	target := filepath.Join(dir, "round_trip.bin")
	if err := s.WriteFile(target, encrypted, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Read raw bytes off disk; they should equal the encrypted payload, NOT
	// be a re-encryption of it.
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, encrypted) {
		t.Fatalf("WriteFile re-encrypted age-encrypted bytes (double encryption)")
	}

	// Sanity: ReadFile should still give us the plaintext.
	got, err := s.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, plaintext)
	}
}

func TestShouldSkipEncryption_IncludesEncryptionConfig(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".encryption-config.json")
	if !s.shouldSkipEncryption(path) {
		t.Fatalf(".encryption-config.json must be skipped from encryption")
	}
}

func TestIsAgeEncryptedData_Exported(t *testing.T) {
	plaintext := []byte("not encrypted")
	if IsAgeEncryptedData(plaintext) {
		t.Fatalf("plaintext misdetected as age-encrypted")
	}
	header := []byte("age-encryption.org/v1\n")
	if !IsAgeEncryptedData(header) {
		t.Fatalf("age header not detected")
	}
}
