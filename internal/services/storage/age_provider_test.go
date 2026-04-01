package storage

import (
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
)

func createTestAgeIdentityFile(t *testing.T, dir string) (string, *age.X25519Identity) {
	t.Helper()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate identity: %v", err)
	}

	idPath := filepath.Join(dir, "key.txt")
	f, err := os.Create(idPath)
	if err != nil {
		t.Fatalf("failed to create identity file: %v", err)
	}
	defer f.Close()

	f.WriteString("# created: test\n")
	f.WriteString("# public key: " + identity.Recipient().String() + "\n")
	f.WriteString(identity.String() + "\n")

	return idPath, identity
}

func TestNewAgeProvider(t *testing.T) {
	dir := t.TempDir()
	idPath, _ := createTestAgeIdentityFile(t, dir)

	p, err := NewAgeProvider(idPath)
	if err != nil {
		t.Fatalf("NewAgeProvider failed: %v", err)
	}

	if p.Method() != AuthMethodAge {
		t.Errorf("expected %s, got %s", AuthMethodAge, p.Method())
	}

	if !p.IsUnlocked() {
		t.Error("age provider should be unlocked after creation")
	}

	if p.NeedsUnlock() {
		t.Error("age provider should not need unlock")
	}
}

func TestNewAgeProviderEmptyPath(t *testing.T) {
	_, err := NewAgeProvider("")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestNewAgeProviderNonexistentFile(t *testing.T) {
	_, err := NewAgeProvider("/nonexistent/path/key.txt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestNewAgeProviderInvalidFile(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "bad.txt")
	os.WriteFile(badPath, []byte("not a valid identity"), 0600)

	_, err := NewAgeProvider(badPath)
	if err == nil {
		t.Error("expected error for invalid identity file")
	}
}

func TestNewAgeProviderEmptyIdentityFile(t *testing.T) {
	dir := t.TempDir()
	emptyPath := filepath.Join(dir, "empty.txt")
	// Write only comments, no identity line
	os.WriteFile(emptyPath, []byte("# just a comment\n"), 0600)

	_, err := NewAgeProvider(emptyPath)
	if err == nil {
		t.Error("expected error for empty identity file")
	}
}

func TestAgeProviderIdentityAndRecipient(t *testing.T) {
	dir := t.TempDir()
	idPath, _ := createTestAgeIdentityFile(t, dir)

	p, err := NewAgeProvider(idPath)
	if err != nil {
		t.Fatalf("NewAgeProvider failed: %v", err)
	}

	id, err := p.Identity()
	if err != nil {
		t.Fatalf("Identity failed: %v", err)
	}
	if id == nil {
		t.Error("identity should not be nil")
	}

	r, err := p.Recipient()
	if err != nil {
		t.Fatalf("Recipient failed: %v", err)
	}
	if r == nil {
		t.Error("recipient should not be nil")
	}
}

func TestAgeProviderLockUnlock(t *testing.T) {
	dir := t.TempDir()
	idPath, _ := createTestAgeIdentityFile(t, dir)

	p, err := NewAgeProvider(idPath)
	if err != nil {
		t.Fatalf("NewAgeProvider failed: %v", err)
	}

	// Lock
	p.Lock()

	if p.IsUnlocked() {
		t.Error("should be locked")
	}

	_, err = p.Identity()
	if err == nil {
		t.Error("expected error when locked")
	}

	_, err = p.Recipient()
	if err == nil {
		t.Error("expected error when locked")
	}

	// Unlock is a no-op for age
	if err := p.Unlock(""); err != nil {
		t.Errorf("Unlock should be no-op: %v", err)
	}
}

func TestAgeProviderDisplayInfo(t *testing.T) {
	dir := t.TempDir()
	idPath, _ := createTestAgeIdentityFile(t, dir)

	p, err := NewAgeProvider(idPath)
	if err != nil {
		t.Fatalf("NewAgeProvider failed: %v", err)
	}

	info := p.DisplayInfo()
	if info == "" {
		t.Error("DisplayInfo should not be empty")
	}
}

func TestAgeProviderGetPublicKey(t *testing.T) {
	dir := t.TempDir()
	idPath, _ := createTestAgeIdentityFile(t, dir)

	p, err := NewAgeProvider(idPath)
	if err != nil {
		t.Fatalf("NewAgeProvider failed: %v", err)
	}

	pubKey := p.GetPublicKey()
	if pubKey == "" {
		t.Error("GetPublicKey should not be empty")
	}

	// After lock, should be empty
	p.Lock()
	pubKey = p.GetPublicKey()
	if pubKey != "" {
		t.Error("GetPublicKey should be empty when locked")
	}
}

func TestAgeProviderEncryptDecryptRoundtrip(t *testing.T) {
	dir := t.TempDir()
	idPath, _ := createTestAgeIdentityFile(t, dir)

	p, err := NewAgeProvider(idPath)
	if err != nil {
		t.Fatalf("NewAgeProvider failed: %v", err)
	}

	recipient, _ := p.Recipient()
	identity, _ := p.Identity()

	original := []byte("secret age data")
	encrypted, err := encryptData(original, recipient)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	decrypted, err := decryptData(encrypted, identity)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if string(decrypted) != string(original) {
		t.Errorf("mismatch: got %q, want %q", decrypted, original)
	}
}

func TestGenerateAgeIdentity(t *testing.T) {
	dir := t.TempDir()
	idPath := filepath.Join(dir, "subdir", "newkey.txt")

	p, err := GenerateAgeIdentity(idPath)
	if err != nil {
		t.Fatalf("GenerateAgeIdentity failed: %v", err)
	}

	if p == nil {
		t.Fatal("provider should not be nil")
	}

	if !p.IsUnlocked() {
		t.Error("generated provider should be unlocked")
	}

	pubKey := p.GetPublicKey()
	if pubKey == "" {
		t.Error("generated provider should have public key")
	}

	// File should exist
	if _, err := os.Stat(idPath); err != nil {
		t.Errorf("identity file should exist: %v", err)
	}

	// Trying to generate again should fail (file exists)
	_, err = GenerateAgeIdentity(idPath)
	if err == nil {
		t.Error("expected error when file already exists")
	}
}

func TestAgeProviderLoadIdentityNoIdentities(t *testing.T) {
	dir := t.TempDir()
	// File with only comments/blank lines - no actual identity
	emptyPath := filepath.Join(dir, "empty_id.txt")
	os.WriteFile(emptyPath, []byte("# just a comment\n# another\n"), 0600)

	_, err := NewAgeProvider(emptyPath)
	if err == nil {
		t.Error("expected error for file with no identities")
	}
}

func TestGenerateAgeIdentityCanBeLoaded(t *testing.T) {
	dir := t.TempDir()
	idPath := filepath.Join(dir, "gen.txt")

	gen, err := GenerateAgeIdentity(idPath)
	if err != nil {
		t.Fatalf("GenerateAgeIdentity failed: %v", err)
	}

	// Load it back
	loaded, err := NewAgeProvider(idPath)
	if err != nil {
		t.Fatalf("NewAgeProvider failed: %v", err)
	}

	if gen.GetPublicKey() != loaded.GetPublicKey() {
		t.Error("public keys should match")
	}
}
