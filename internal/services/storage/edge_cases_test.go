package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filippo.io/age"
)

func TestUnlockVerificationMagicMismatch(t *testing.T) {
	dir := t.TempDir()

	// Enable encryption
	s, _ := New(dir)
	s.EnableEncryption("testpassword123")
	s.Lock()

	// Tamper with the verify file - encrypt something other than verifyMagic
	// using the same password
	p := NewPasswordProvider()
	p.Unlock("testpassword123")
	recipient, _ := p.Recipient()
	wrongData, _ := encryptData([]byte("wrong magic"), recipient)
	verifyPath := filepath.Join(dir, verifyFile)
	os.WriteFile(verifyPath, wrongData, 0644)

	// Now try to unlock
	s2, _ := New(dir)
	err := s2.Unlock("testpassword123")
	if err == nil {
		t.Error("expected error for magic mismatch")
	}
}

func TestUnlockCannotReadVerifyFile(t *testing.T) {
	dir := t.TempDir()

	// Enable encryption
	s, _ := New(dir)
	s.EnableEncryption("testpassword123")
	s.Lock()

	// Remove verify file
	os.Remove(filepath.Join(dir, verifyFile))

	s2, _ := New(dir)
	err := s2.Unlock("testpassword123")
	if err == nil {
		t.Error("expected error when verify file missing")
	}
}

func TestUnlockCreateProviderFailure(t *testing.T) {
	dir := t.TempDir()

	// Create marker but with config pointing to nonexistent age identity
	os.WriteFile(filepath.Join(dir, markerFile), []byte("encrypted"), 0644)
	saveConfig(dir, &EncryptionConfig{
		Method:          AuthMethodAge,
		AgeIdentityPath: "/nonexistent/key.txt",
	})

	s, _ := New(dir)
	err := s.Unlock("")
	if err == nil {
		t.Error("expected error when provider creation fails")
	}
}

func TestUnlockUnknownMethod(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, markerFile), []byte("encrypted"), 0644)
	saveConfig(dir, &EncryptionConfig{Method: "unknown"})

	s, _ := New(dir)
	err := s.Unlock("")
	if err == nil {
		t.Error("expected error for unknown method")
	}
}

func TestReadFileDecryptionError(t *testing.T) {
	dir := t.TempDir()

	s, _ := New(dir)
	s.EnableEncryption("testpassword123")

	// Write a file that looks encrypted but isn't valid
	badFile := filepath.Join(dir, "bad.csv")
	// Write something that starts with age header but is corrupt
	os.WriteFile(badFile, []byte("age-encryption.org/v1\ncorrupt data here"), 0644)

	_, err := s.ReadFile(badFile)
	if err == nil {
		t.Error("expected error for corrupt encrypted file")
	}
}

func TestWriteFileRecipientError(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Set up encrypted state with a provider that will fail on Recipient()
	s.encrypted = true
	p := NewPasswordProvider()
	p.Unlock("testpassword")
	s.provider = p

	// Lock the provider to make Recipient() fail
	p.Lock()
	// But mark as having a (broken) provider
	s.provider = p

	testFile := filepath.Join(dir, "test.csv")
	// Provider is locked so IsUnlocked() returns false - won't try to encrypt
	// This tests the condition where encrypted=true but provider not unlocked
	err := s.WriteFile(testFile, []byte("data"), 0644)
	if err != nil {
		t.Fatalf("WriteFile should succeed without encryption: %v", err)
	}
}

func TestAtomicWriteCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Write to a path where the directory doesn't exist yet
	nestedPath := filepath.Join(dir, "new", "deep", "file.txt")
	err := s.atomicWrite(nestedPath, []byte("data"), 0644)
	if err != nil {
		t.Fatalf("atomicWrite failed: %v", err)
	}

	data, _ := os.ReadFile(nestedPath)
	if string(data) != "data" {
		t.Errorf("expected 'data', got %q", data)
	}
}

func TestEnableEncryptionWithProviderAlreadyEncrypted(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	s.EnableEncryption("testpassword123")

	// Generate age provider
	idPath := filepath.Join(t.TempDir(), "key.txt")
	ageProvider, _ := GenerateAgeIdentity(idPath)
	config := &EncryptionConfig{Method: AuthMethodAge, AgeIdentityPath: idPath}

	err := s.EnableEncryptionWithProvider(ageProvider, config)
	if err == nil {
		t.Error("expected error when already encrypted")
	}
}

func TestDisableEncryptionWithAgeProviderRestart(t *testing.T) {
	dir := t.TempDir()

	csvFile := filepath.Join(dir, "data.csv")
	os.WriteFile(csvFile, []byte("a,b\n1,2"), 0644)

	s, _ := New(dir)

	idPath := filepath.Join(t.TempDir(), "key.txt")
	ageProvider, _ := GenerateAgeIdentity(idPath)
	config := &EncryptionConfig{Method: AuthMethodAge, AgeIdentityPath: idPath}
	s.EnableEncryptionWithProvider(ageProvider, config)

	// Re-create to simulate restart
	s2, _ := New(dir)

	if err := s2.DisableEncryption(""); err != nil {
		t.Fatalf("DisableEncryption failed: %v", err)
	}

	raw, _ := os.ReadFile(csvFile)
	if string(raw) != "a,b\n1,2" {
		t.Errorf("content mismatch: got %q", raw)
	}
}

func TestDisableEncryptionIncorrectCredentials(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "test.csv"), []byte("data"), 0644)

	s, _ := New(dir)
	s.EnableEncryption("correctpass123")

	s2, _ := New(dir)
	err := s2.DisableEncryption("wrongpass123")
	if err == nil {
		t.Error("expected error for wrong credentials")
	}
}

func TestEnableEncryptionNoFiles(t *testing.T) {
	// Enable encryption on empty directory
	dir := t.TempDir()
	s, _ := New(dir)

	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption on empty dir failed: %v", err)
	}

	if !s.IsEncrypted() {
		t.Error("should be encrypted")
	}
}

func TestDisableEncryptionUnknownMethod(t *testing.T) {
	dir := t.TempDir()

	// Set up encrypted state with unknown method
	os.WriteFile(filepath.Join(dir, markerFile), []byte("encrypted"), 0644)
	saveConfig(dir, &EncryptionConfig{Method: "bogus"})

	// Create a dummy verify file
	os.WriteFile(filepath.Join(dir, verifyFile), []byte("dummy"), 0644)

	s, _ := New(dir)
	err := s.DisableEncryption("")
	if err == nil {
		t.Error("expected error for unknown method")
	}
}

func TestEncryptFileWithRecipientNonexistent(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	s.EnableEncryption("testpassword123")
	recipient, _ := s.provider.Recipient()

	err := s.encryptFileWithRecipient(filepath.Join(dir, "nonexistent"), recipient)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestDecryptFileWithIdentityNonexistent(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	s.EnableEncryption("testpassword123")
	identity, _ := s.provider.Identity()

	err := s.decryptFileWithIdentity(filepath.Join(dir, "nonexistent"), identity)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestNewSSHProviderBadPublicKey(t *testing.T) {
	dir := t.TempDir()

	privPath := filepath.Join(dir, "id_test")
	os.WriteFile(privPath, []byte("private key data"), 0600)
	os.WriteFile(privPath+".pub", []byte("not a valid ssh public key"), 0644)

	_, err := NewSSHProvider(privPath)
	if err == nil {
		t.Error("expected error for invalid public key")
	}
}

func TestLockWithNilProvider(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Should not panic
	s.Lock()
}

func TestCacheStaleAfterFileModification(t *testing.T) {
	// Note: timestamps are set explicitly with os.Chtimes rather than relying
	// on os.WriteFile to advance mtime — some filesystems (e.g. WSL2 9p, the
	// CI sandbox) have coarse or effectively frozen mtime granularity, so two
	// rapid rewrites can share an mtime. Setting it deterministically makes
	// these cases reproducible regardless of the host filesystem.
	t.Run("re-reads when mtime changes", func(t *testing.T) {
		dir := t.TempDir()
		s, _ := New(dir)
		testFile := filepath.Join(dir, "change.txt")

		os.WriteFile(testFile, []byte("original"), 0644)
		old := time.Unix(1_000_000_000, 0)
		if err := os.Chtimes(testFile, old, old); err != nil {
			t.Fatalf("chtimes: %v", err)
		}

		if data, _ := s.ReadFile(testFile); string(data) != "original" {
			t.Fatalf("unexpected initial read: %q", data)
		}

		// External edit (bypassing WriteFile) with a strictly later mtime.
		os.WriteFile(testFile, []byte("changed!"), 0644)
		newer := old.Add(time.Hour)
		if err := os.Chtimes(testFile, newer, newer); err != nil {
			t.Fatalf("chtimes: %v", err)
		}

		if data, _ := s.ReadFile(testFile); string(data) != "changed!" {
			t.Errorf("expected 'changed!', got %q", data)
		}
	})

	// Even when mtime is unchanged (coarse/frozen-granularity filesystem), a
	// size change must invalidate the cache — mtime alone is not a reliable
	// staleness signal.
	t.Run("re-reads when size changes even if mtime is unchanged", func(t *testing.T) {
		dir := t.TempDir()
		s, _ := New(dir)
		testFile := filepath.Join(dir, "change.txt")

		os.WriteFile(testFile, []byte("original"), 0644)
		frozen := time.Unix(1_000_000_000, 0)
		if err := os.Chtimes(testFile, frozen, frozen); err != nil {
			t.Fatalf("chtimes: %v", err)
		}

		if data, _ := s.ReadFile(testFile); string(data) != "original" {
			t.Fatalf("unexpected initial read: %q", data)
		}

		// Different-length content, but pin the SAME mtime to simulate a
		// filesystem whose timestamp granularity didn't catch the edit.
		os.WriteFile(testFile, []byte("modified-and-longer"), 0644)
		if err := os.Chtimes(testFile, frozen, frozen); err != nil {
			t.Fatalf("chtimes: %v", err)
		}

		if data, _ := s.ReadFile(testFile); string(data) != "modified-and-longer" {
			t.Errorf("expected size change to invalidate cache, got %q", data)
		}
	})
}

func TestEncryptDecryptFileWithRecipientIdentity(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Generate age identity for clean encrypt/decrypt
	idPath := filepath.Join(dir, "key.txt")
	ageProvider, _ := GenerateAgeIdentity(idPath)

	recipient, _ := ageProvider.Recipient()
	identity, _ := ageProvider.Identity()

	// Create a plain file
	testFile := filepath.Join(dir, "roundtrip.csv")
	original := []byte("col1,col2\nval1,val2\n")
	os.WriteFile(testFile, original, 0644)

	// Encrypt it
	if err := s.encryptFileWithRecipient(testFile, recipient); err != nil {
		t.Fatalf("encryptFileWithRecipient failed: %v", err)
	}

	// Verify it's encrypted on disk
	raw, _ := os.ReadFile(testFile)
	if !isAgeEncrypted(raw) {
		t.Error("file should be encrypted after encryptFileWithRecipient")
	}

	// Decrypt it
	if err := s.decryptFileWithIdentity(testFile, identity); err != nil {
		t.Fatalf("decryptFileWithIdentity failed: %v", err)
	}

	// Verify it's decrypted
	raw, _ = os.ReadFile(testFile)
	if string(raw) != string(original) {
		t.Errorf("content mismatch: got %q, want %q", raw, original)
	}
}

func TestDecryptFileWithIdentityCorruptData(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	idPath := filepath.Join(dir, "key.txt")
	ageProvider, _ := GenerateAgeIdentity(idPath)
	identity, _ := ageProvider.Identity()

	// Write corrupt encrypted data
	testFile := filepath.Join(dir, "corrupt.csv")
	os.WriteFile(testFile, []byte("age-encryption.org/v1\ncorrupt"), 0644)

	err := s.decryptFileWithIdentity(testFile, identity)
	if err == nil {
		t.Error("expected error for corrupt encrypted file")
	}
}

func TestRollbackEncryptionWithBadDecrypt(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Use one identity to encrypt, another to try rollback (should fail silently)
	id1Path := filepath.Join(dir, "key1.txt")
	id2Path := filepath.Join(dir, "key2.txt")
	prov1, _ := GenerateAgeIdentity(id1Path)
	prov2, _ := GenerateAgeIdentity(id2Path)

	recipient1, _ := prov1.Recipient()
	identity2, _ := prov2.Identity()

	// Encrypt a file with prov1
	testFile := filepath.Join(dir, "encrypted.csv")
	enc, _ := encryptData([]byte("data"), recipient1)
	os.WriteFile(testFile, enc, 0644)

	// Rollback with wrong identity should silently fail
	s.rollbackEncryptionWithIdentity([]string{testFile}, identity2)

	// File should still be encrypted (rollback couldn't decrypt it)
	raw, _ := os.ReadFile(testFile)
	if !isAgeEncrypted(raw) {
		t.Error("file should still be encrypted after failed rollback")
	}
}

// mockBrokenProvider is a provider where IsUnlocked() is true but Recipient() fails
type mockBrokenProvider struct{}

func (m *mockBrokenProvider) Method() AuthMethod                     { return AuthMethodPassword }
func (m *mockBrokenProvider) Identity() (age.Identity, error)        { return nil, fmt.Errorf("broken") }
func (m *mockBrokenProvider) Recipient() (age.Recipient, error)      { return nil, fmt.Errorf("broken") }
func (m *mockBrokenProvider) NeedsUnlock() bool                      { return false }
func (m *mockBrokenProvider) IsUnlocked() bool                       { return true }
func (m *mockBrokenProvider) Unlock(credentials string) error        { return nil }
func (m *mockBrokenProvider) Lock()                                  {}
func (m *mockBrokenProvider) DisplayInfo() string                    { return "broken" }

func TestWriteFileWithRecipientError(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	s.encrypted = true
	s.provider = &mockBrokenProvider{}

	testFile := filepath.Join(dir, "fail.csv")
	err := s.WriteFile(testFile, []byte("data"), 0644)
	if err == nil {
		t.Error("expected error when recipient fails")
	}
}

func TestEnableEncryptionWithProviderGetIdentityError(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Provider where Recipient works but Identity fails
	provider := &mockIdentityOnlyBroken{}
	config := &EncryptionConfig{Method: AuthMethodPassword}

	err := s.EnableEncryptionWithProvider(provider, config)
	if err == nil {
		t.Error("expected error when identity fails")
	}
}

func TestEnableEncryptionWithProviderGetRecipientError(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Provider where Recipient fails
	provider := &mockRecipientOnlyBroken{}
	config := &EncryptionConfig{Method: AuthMethodPassword}

	err := s.EnableEncryptionWithProvider(provider, config)
	if err == nil {
		t.Error("expected error when recipient fails")
	}
}

// mockIdentityOnlyBroken has working Recipient but broken Identity
type mockIdentityOnlyBroken struct{}

func (m *mockIdentityOnlyBroken) Method() AuthMethod { return AuthMethodPassword }
func (m *mockIdentityOnlyBroken) Identity() (age.Identity, error) {
	return nil, fmt.Errorf("broken identity")
}
func (m *mockIdentityOnlyBroken) Recipient() (age.Recipient, error) {
	id, _ := age.GenerateX25519Identity()
	return id.Recipient(), nil
}
func (m *mockIdentityOnlyBroken) NeedsUnlock() bool       { return false }
func (m *mockIdentityOnlyBroken) IsUnlocked() bool        { return true }
func (m *mockIdentityOnlyBroken) Unlock(string) error     { return nil }
func (m *mockIdentityOnlyBroken) Lock()                   {}
func (m *mockIdentityOnlyBroken) DisplayInfo() string     { return "broken" }

// mockRecipientOnlyBroken has working Identity but broken Recipient
type mockRecipientOnlyBroken struct{}

func (m *mockRecipientOnlyBroken) Method() AuthMethod { return AuthMethodPassword }
func (m *mockRecipientOnlyBroken) Identity() (age.Identity, error) {
	id, _ := age.GenerateX25519Identity()
	return id, nil
}
func (m *mockRecipientOnlyBroken) Recipient() (age.Recipient, error) {
	return nil, fmt.Errorf("broken recipient")
}
func (m *mockRecipientOnlyBroken) NeedsUnlock() bool       { return false }
func (m *mockRecipientOnlyBroken) IsUnlocked() bool        { return true }
func (m *mockRecipientOnlyBroken) Unlock(string) error     { return nil }
func (m *mockRecipientOnlyBroken) Lock()                   {}
func (m *mockRecipientOnlyBroken) DisplayInfo() string     { return "broken" }

func TestDisableEncryptionVerifyFileMissing(t *testing.T) {
	dir := t.TempDir()

	// Set up encrypted state
	os.WriteFile(filepath.Join(dir, markerFile), []byte("encrypted"), 0644)
	saveConfig(dir, &EncryptionConfig{Method: AuthMethodPassword})
	// NO verify file

	s, _ := New(dir)
	err := s.DisableEncryption("testpassword123")
	if err == nil {
		t.Error("expected error when verify file missing")
	}
}

func TestDisableEncryptionWithSSHProvider(t *testing.T) {
	dir := t.TempDir()
	sshDir := t.TempDir()

	csvFile := filepath.Join(dir, "data.csv")
	os.WriteFile(csvFile, []byte("ssh,data"), 0644)

	s, _ := New(dir)

	keyPath := createTestSSHKeyPair(t, sshDir)
	sshProvider, _ := NewSSHProvider(keyPath)
	sshProvider.Unlock("")

	config := &EncryptionConfig{Method: AuthMethodSSH, SSHKeyPath: keyPath}
	s.EnableEncryptionWithProvider(sshProvider, config)

	// Re-create to simulate restart
	s2, _ := New(dir)

	if err := s2.DisableEncryption(""); err != nil {
		t.Fatalf("DisableEncryption failed: %v", err)
	}

	if s2.IsEncrypted() {
		t.Error("should not be encrypted")
	}

	raw, _ := os.ReadFile(csvFile)
	if string(raw) != "ssh,data" {
		t.Errorf("content mismatch: got %q", raw)
	}
}

func TestDisableEncryptionWithMultipleFiles(t *testing.T) {
	dir := t.TempDir()

	// Create multiple files including subdirectories
	subDir := filepath.Join(dir, "sub")
	os.MkdirAll(subDir, 0755)

	files := map[string]string{
		filepath.Join(dir, "a.csv"):    "csv content",
		filepath.Join(dir, "b.json"):   `{"json":true}`,
		filepath.Join(subDir, "c.csv"): "nested csv",
	}

	for path, content := range files {
		os.WriteFile(path, []byte(content), 0644)
	}

	s, _ := New(dir)
	s.EnableEncryption("testpassword123")

	// Verify files are encrypted
	for path := range files {
		raw, _ := os.ReadFile(path)
		if !isAgeEncrypted(raw) {
			t.Errorf("file %s should be encrypted", path)
		}
	}

	// Disable
	s2, _ := New(dir)
	if err := s2.DisableEncryption("testpassword123"); err != nil {
		t.Fatalf("DisableEncryption failed: %v", err)
	}

	// Verify all files are decrypted
	for path, expected := range files {
		raw, _ := os.ReadFile(path)
		if string(raw) != expected {
			t.Errorf("file %s content mismatch: got %q, want %q", path, raw, expected)
		}
	}
}

func TestDisableEncryptionProviderUnlockError(t *testing.T) {
	dir := t.TempDir()
	sshDir := t.TempDir()

	csvFile := filepath.Join(dir, "data.csv")
	os.WriteFile(csvFile, []byte("data"), 0644)

	s, _ := New(dir)
	keyPath := createTestSSHKeyPair(t, sshDir)
	sshProvider, _ := NewSSHProvider(keyPath)
	sshProvider.Unlock("")
	config := &EncryptionConfig{Method: AuthMethodSSH, SSHKeyPath: keyPath}
	s.EnableEncryptionWithProvider(sshProvider, config)

	// Remove the private key to cause Unlock failure
	os.Remove(keyPath)

	s2, _ := New(dir)
	err := s2.DisableEncryption("")
	if err == nil {
		t.Error("expected error when SSH key missing")
	}
}

func TestDisableEncryptionProviderCreateError(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, markerFile), []byte("encrypted"), 0644)
	saveConfig(dir, &EncryptionConfig{Method: "bogus"})
	os.WriteFile(filepath.Join(dir, verifyFile), []byte("dummy"), 0644)

	s, _ := New(dir)
	err := s.DisableEncryption("")
	if err == nil {
		t.Error("expected error for unknown auth method")
	}
}

func TestUnlockProviderUnlockError(t *testing.T) {
	dir := t.TempDir()

	// Set up encrypted storage with SSH key that will fail on unlock
	sshDir := t.TempDir()
	keyPath := createTestSSHKeyPair(t, sshDir)

	s, _ := New(dir)
	sshProvider, _ := NewSSHProvider(keyPath)
	sshProvider.Unlock("")
	config := &EncryptionConfig{Method: AuthMethodSSH, SSHKeyPath: keyPath}
	s.EnableEncryptionWithProvider(sshProvider, config)

	// Now delete the private key so unlock will fail
	os.Remove(keyPath)

	s2, _ := New(dir)
	err := s2.Unlock("")
	if err == nil {
		t.Error("expected error when private key is missing")
	}
}

func TestYubiKeyProviderWithoutPlugin(t *testing.T) {
	if IsYubiKeyPluginInstalled() {
		t.Skip("YubiKey plugin is installed")
	}

	// Test NewYubiKeyProvider
	_, err := NewYubiKeyProvider("AGE-PLUGIN-YUBIKEY-1test")
	if err == nil {
		t.Error("expected error")
	}

	// Test NewYubiKeyProviderWithRecipient
	_, err = NewYubiKeyProviderWithRecipient("AGE-PLUGIN-YUBIKEY-1test", "age1yubikey1test")
	if err == nil {
		t.Error("expected error")
	}

	// Test empty identity
	_, err = NewYubiKeyProvider("")
	if err == nil {
		t.Error("expected error for empty identity")
	}

	_, err = NewYubiKeyProviderWithRecipient("", "age1yubikey1test")
	if err == nil {
		t.Error("expected error for empty identity")
	}
}
