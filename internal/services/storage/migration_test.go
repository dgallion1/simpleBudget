package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnableEncryptionAlreadyEnabled(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("first enable failed: %v", err)
	}

	// Second enable should fail
	err := s.EnableEncryption("testpassword123")
	if err == nil {
		t.Error("expected error when already encrypted")
	}
}

func TestDisableEncryptionNotEnabled(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	err := s.DisableEncryption("password")
	if err == nil {
		t.Error("expected error when not encrypted")
	}
}

func TestEnableEncryptionWithAgeProvider(t *testing.T) {
	dir := t.TempDir()

	// Create test files
	csvFile := filepath.Join(dir, "data.csv")
	jsonFile := filepath.Join(dir, "config.json")
	txtFile := filepath.Join(dir, "notes.txt")
	os.WriteFile(csvFile, []byte("col1,col2\n1,2\n"), 0644)
	os.WriteFile(jsonFile, []byte(`{"key":"value"}`), 0644)
	os.WriteFile(txtFile, []byte("plain text"), 0644)

	s, _ := New(dir)

	// Generate age identity
	idPath := filepath.Join(dir, "key.txt")
	ageProvider, err := GenerateAgeIdentity(idPath)
	if err != nil {
		t.Fatalf("GenerateAgeIdentity failed: %v", err)
	}

	config := &EncryptionConfig{
		Method:          AuthMethodAge,
		AgeIdentityPath: idPath,
	}

	if err := s.EnableEncryptionWithProvider(ageProvider, config); err != nil {
		t.Fatalf("EnableEncryptionWithProvider failed: %v", err)
	}

	if !s.IsEncrypted() {
		t.Error("should be encrypted")
	}

	// CSV and JSON should be encrypted on disk
	rawCSV, _ := os.ReadFile(csvFile)
	if !isAgeEncrypted(rawCSV) {
		t.Error("CSV file should be encrypted")
	}

	rawJSON, _ := os.ReadFile(jsonFile)
	if !isAgeEncrypted(rawJSON) {
		t.Error("JSON file should be encrypted")
	}

	// TXT should NOT be encrypted (only csv and json are encrypted)
	rawTXT, _ := os.ReadFile(txtFile)
	if isAgeEncrypted(rawTXT) {
		t.Error("TXT file should not be encrypted")
	}

	// ReadFile should return decrypted content
	data, err := s.ReadFile(csvFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "col1,col2\n1,2\n" {
		t.Errorf("content mismatch: got %q", data)
	}
}

func TestDisableEncryptionWithAgeProvider(t *testing.T) {
	dir := t.TempDir()

	csvFile := filepath.Join(dir, "data.csv")
	os.WriteFile(csvFile, []byte("a,b\n1,2\n"), 0644)

	s, _ := New(dir)

	// Generate and enable
	idPath := filepath.Join(dir, "key.txt")
	ageProvider, _ := GenerateAgeIdentity(idPath)
	config := &EncryptionConfig{Method: AuthMethodAge, AgeIdentityPath: idPath}
	s.EnableEncryptionWithProvider(ageProvider, config)

	// Disable (age doesn't need credentials)
	if err := s.DisableEncryption(""); err != nil {
		t.Fatalf("DisableEncryption failed: %v", err)
	}

	if s.IsEncrypted() {
		t.Error("should not be encrypted")
	}

	// File should be plain on disk
	raw, _ := os.ReadFile(csvFile)
	if isAgeEncrypted(raw) {
		t.Error("file should be decrypted")
	}
	if string(raw) != "a,b\n1,2\n" {
		t.Errorf("content mismatch: got %q", raw)
	}

	// Marker, verify, config should be removed
	if _, err := os.Stat(filepath.Join(dir, markerFile)); !os.IsNotExist(err) {
		t.Error("marker file should be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, verifyFile)); !os.IsNotExist(err) {
		t.Error("verify file should be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, configFile)); !os.IsNotExist(err) {
		t.Error("config file should be removed")
	}
}

func TestEnableEncryptionWithSSHProvider(t *testing.T) {
	dir := t.TempDir()

	csvFile := filepath.Join(dir, "test.csv")
	os.WriteFile(csvFile, []byte("x,y\n3,4\n"), 0644)

	s, _ := New(dir)

	// Create SSH keys
	sshDir := t.TempDir()
	keyPath := createTestSSHKeyPair(t, sshDir)

	sshProvider, err := NewSSHProvider(keyPath)
	if err != nil {
		t.Fatalf("NewSSHProvider failed: %v", err)
	}
	sshProvider.Unlock("")

	config := &EncryptionConfig{
		Method:     AuthMethodSSH,
		SSHKeyPath: keyPath,
	}

	if err := s.EnableEncryptionWithProvider(sshProvider, config); err != nil {
		t.Fatalf("EnableEncryptionWithProvider failed: %v", err)
	}

	if !s.IsEncrypted() {
		t.Error("should be encrypted")
	}

	// ReadFile should decrypt
	data, err := s.ReadFile(csvFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "x,y\n3,4\n" {
		t.Errorf("content mismatch: got %q", data)
	}
}

func TestEncryptFileAlreadyEncrypted(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Enable encryption to get a provider
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption failed: %v", err)
	}

	recipient, _ := s.provider.Recipient()

	// Write an already-encrypted file
	testFile := filepath.Join(dir, "already.csv")
	plainData := []byte("plain content")
	encrypted, _ := encryptData(plainData, recipient)
	os.WriteFile(testFile, encrypted, 0644)

	// encryptFileWithRecipient should skip it
	if err := s.encryptFileWithRecipient(testFile, recipient); err != nil {
		t.Fatalf("encryptFileWithRecipient failed: %v", err)
	}

	// File should still be decryptable
	identity, _ := s.provider.Identity()
	raw, _ := os.ReadFile(testFile)
	dec, err := decryptData(raw, identity)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if string(dec) != string(plainData) {
		t.Errorf("content changed after encrypt of already encrypted file")
	}
}

func TestDecryptFileNotEncrypted(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	testFile := filepath.Join(dir, "plain.csv")
	os.WriteFile(testFile, []byte("not encrypted"), 0644)

	// Enable to get an identity
	s.EnableEncryption("testpassword123")
	identity, _ := s.provider.Identity()

	// Should be a no-op
	if err := s.decryptFileWithIdentity(testFile, identity); err != nil {
		t.Fatalf("decryptFileWithIdentity failed: %v", err)
	}

	raw, _ := os.ReadFile(testFile)
	if string(raw) != "not encrypted" {
		t.Error("plain file should be unchanged")
	}
}

func TestRollbackEncryptionWithIdentity(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Enable encryption
	s.EnableEncryption("testpassword123")
	recipient, _ := s.provider.Recipient()
	identity, _ := s.provider.Identity()

	// Create some encrypted files
	file1 := filepath.Join(dir, "f1.csv")
	file2 := filepath.Join(dir, "f2.csv")
	file3 := filepath.Join(dir, "f3.csv") // nonexistent

	enc1, _ := encryptData([]byte("data1"), recipient)
	os.WriteFile(file1, enc1, 0644)

	// file2 is not encrypted
	os.WriteFile(file2, []byte("data2"), 0644)

	files := []string{file1, file2, file3}
	s.rollbackEncryptionWithIdentity(files, identity)

	// file1 should be decrypted
	raw1, _ := os.ReadFile(file1)
	if string(raw1) != "data1" {
		t.Errorf("expected 'data1', got %q", raw1)
	}

	// file2 should be unchanged
	raw2, _ := os.ReadFile(file2)
	if string(raw2) != "data2" {
		t.Errorf("expected 'data2', got %q", raw2)
	}
}

func TestCreateProviderUnlocked(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// nil config defaults to password
	s.config = nil
	p, err := s.createProviderUnlocked()
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if p.Method() != AuthMethodPassword {
		t.Errorf("expected password, got %s", p.Method())
	}

	// Password config
	s.config = &EncryptionConfig{Method: AuthMethodPassword}
	p, err = s.createProviderUnlocked()
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if p.Method() != AuthMethodPassword {
		t.Errorf("expected password, got %s", p.Method())
	}

	// Age config
	idDir := t.TempDir()
	idPath, _ := createTestAgeIdentityFile(t, idDir)
	s.config = &EncryptionConfig{Method: AuthMethodAge, AgeIdentityPath: idPath}
	p, err = s.createProviderUnlocked()
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if p.Method() != AuthMethodAge {
		t.Errorf("expected age, got %s", p.Method())
	}

	// SSH config
	sshDir := t.TempDir()
	keyPath := createTestSSHKeyPair(t, sshDir)
	s.config = &EncryptionConfig{Method: AuthMethodSSH, SSHKeyPath: keyPath}
	p, err = s.createProviderUnlocked()
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if p.Method() != AuthMethodSSH {
		t.Errorf("expected ssh, got %s", p.Method())
	}

	// Unknown
	s.config = &EncryptionConfig{Method: "bogus"}
	_, err = s.createProviderUnlocked()
	if err == nil {
		t.Error("expected error for unknown method")
	}
}

func TestUnlockWithAgeProvider(t *testing.T) {
	dir := t.TempDir()

	// Create data
	csvFile := filepath.Join(dir, "test.csv")
	os.WriteFile(csvFile, []byte("a,b"), 0644)

	s, _ := New(dir)

	// Generate and enable age encryption
	idPath := filepath.Join(dir, "key.txt")
	ageProvider, _ := GenerateAgeIdentity(idPath)
	config := &EncryptionConfig{Method: AuthMethodAge, AgeIdentityPath: idPath}
	s.EnableEncryptionWithProvider(ageProvider, config)

	// Lock
	s.Lock()

	// Re-create storage from scratch (simulates restart)
	s2, err := New(dir)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if !s2.IsEncrypted() {
		t.Error("should be encrypted")
	}

	// Unlock (age doesn't need credentials)
	if err := s2.Unlock(""); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	if !s2.IsUnlocked() {
		t.Error("should be unlocked")
	}

	// Should be able to read
	data, err := s2.ReadFile(csvFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "a,b" {
		t.Errorf("content mismatch: got %q", data)
	}
}

func TestUnlockWithSSHProvider(t *testing.T) {
	dir := t.TempDir()
	sshDir := t.TempDir()

	csvFile := filepath.Join(dir, "test.csv")
	os.WriteFile(csvFile, []byte("x,y"), 0644)

	s, _ := New(dir)

	keyPath := createTestSSHKeyPair(t, sshDir)
	sshProvider, _ := NewSSHProvider(keyPath)
	sshProvider.Unlock("")

	config := &EncryptionConfig{Method: AuthMethodSSH, SSHKeyPath: keyPath}
	s.EnableEncryptionWithProvider(sshProvider, config)

	s.Lock()

	// Re-create
	s2, _ := New(dir)

	// Unlock with empty passphrase (unencrypted SSH key)
	if err := s2.Unlock(""); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	data, err := s2.ReadFile(csvFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "x,y" {
		t.Errorf("content mismatch: got %q", data)
	}
}

func TestDisableEncryptionWithPassword(t *testing.T) {
	dir := t.TempDir()

	csvFile := filepath.Join(dir, "test.csv")
	os.WriteFile(csvFile, []byte("data"), 0644)

	s, _ := New(dir)
	s.EnableEncryption("testpassword123")

	// Re-create (simulates restart)
	s2, _ := New(dir)

	if err := s2.DisableEncryption("testpassword123"); err != nil {
		t.Fatalf("DisableEncryption failed: %v", err)
	}

	if s2.IsEncrypted() {
		t.Error("should not be encrypted")
	}

	raw, _ := os.ReadFile(csvFile)
	if string(raw) != "data" {
		t.Errorf("content mismatch: got %q", raw)
	}
}

func TestDisableEncryptionWrongPassword(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "t.csv"), []byte("d"), 0644)

	s, _ := New(dir)
	s.EnableEncryption("testpassword123")

	s2, _ := New(dir)
	err := s2.DisableEncryption("wrongpassword")
	if err == nil {
		t.Error("expected error with wrong password")
	}
}

func TestEnableEncryptionWithSubdirectories(t *testing.T) {
	dir := t.TempDir()

	// Create files in subdirectories
	subDir := filepath.Join(dir, "sub")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "nested.csv"), []byte("nested"), 0644)
	os.WriteFile(filepath.Join(dir, "top.json"), []byte(`{"top":true}`), 0644)

	// Cache dir
	cacheDir := filepath.Join(dir, "cache")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "skip.js"), []byte("js"), 0644)

	s, _ := New(dir)
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption failed: %v", err)
	}

	// Nested CSV should be encrypted
	raw, _ := os.ReadFile(filepath.Join(subDir, "nested.csv"))
	if !isAgeEncrypted(raw) {
		t.Error("nested CSV should be encrypted")
	}

	// Top JSON should be encrypted
	raw, _ = os.ReadFile(filepath.Join(dir, "top.json"))
	if !isAgeEncrypted(raw) {
		t.Error("top JSON should be encrypted")
	}

	// Cache should not be encrypted
	raw, _ = os.ReadFile(filepath.Join(cacheDir, "skip.js"))
	if isAgeEncrypted(raw) {
		t.Error("cache file should not be encrypted")
	}
}

func TestWriteFileWhenUnlockedEncrypts(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	s.EnableEncryption("testpassword123")

	newFile := filepath.Join(dir, "new.json")
	s.WriteFile(newFile, []byte(`{"new":true}`), 0644)

	raw, _ := os.ReadFile(newFile)
	if !isAgeEncrypted(raw) {
		t.Error("new file should be encrypted")
	}
}

func TestWriteFileWhenLockedNoEncryption(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	s.encrypted = true
	// No provider set

	newFile := filepath.Join(dir, "plain.csv")
	s.WriteFile(newFile, []byte("plain"), 0644)

	raw, _ := os.ReadFile(newFile)
	if isAgeEncrypted(raw) {
		t.Error("file should not be encrypted when provider is nil")
	}
}
