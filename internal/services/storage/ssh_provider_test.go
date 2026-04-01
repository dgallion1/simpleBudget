package storage

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// createTestSSHKeyPair creates an unencrypted ed25519 SSH key pair for testing
func createTestSSHKeyPair(t *testing.T, dir string) string {
	t.Helper()

	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}

	// Marshal private key to OpenSSH format
	privPEM, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		t.Fatalf("failed to marshal private key: %v", err)
	}

	privPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(privPath, pem.EncodeToMemory(privPEM), 0600); err != nil {
		t.Fatalf("failed to write private key: %v", err)
	}

	// Create public key
	pubKey, err := ssh.NewPublicKey(privKey.Public())
	if err != nil {
		t.Fatalf("failed to create public key: %v", err)
	}

	pubPath := privPath + ".pub"
	pubData := ssh.MarshalAuthorizedKey(pubKey)
	if err := os.WriteFile(pubPath, pubData, 0644); err != nil {
		t.Fatalf("failed to write public key: %v", err)
	}

	return privPath
}

// createTestEncryptedSSHKey creates an SSH key with ENCRYPTED marker for testing IsSSHKeyEncrypted
func createTestEncryptedSSHKey(t *testing.T, dir string) string {
	t.Helper()
	keyPath := filepath.Join(dir, "id_encrypted")
	content := `-----BEGIN OPENSSH PRIVATE KEY-----
ENCRYPTED
b3BlbnNzaC1rZXktdjEAAAAACmFlczI1Ni1jdHIAAAAGYmNyeXB0AAAAGAAA
-----END OPENSSH PRIVATE KEY-----
`
	if err := os.WriteFile(keyPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}
	return keyPath
}

func TestNewSSHProvider(t *testing.T) {
	dir := t.TempDir()
	keyPath := createTestSSHKeyPair(t, dir)

	p, err := NewSSHProvider(keyPath)
	if err != nil {
		t.Fatalf("NewSSHProvider failed: %v", err)
	}

	if p.Method() != AuthMethodSSH {
		t.Errorf("expected %s, got %s", AuthMethodSSH, p.Method())
	}

	// Should not be unlocked initially (needs Unlock to load private key)
	if p.IsUnlocked() {
		t.Error("should not be unlocked before Unlock")
	}

	if !p.NeedsUnlock() {
		t.Error("SSH provider should need unlock")
	}
}

func TestNewSSHProviderEmptyPath(t *testing.T) {
	_, err := NewSSHProvider("")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestNewSSHProviderNonexistentKey(t *testing.T) {
	_, err := NewSSHProvider("/nonexistent/id_ed25519")
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
}

func TestNewSSHProviderMissingPubKey(t *testing.T) {
	dir := t.TempDir()
	// Write only private key, no .pub
	privPath := filepath.Join(dir, "id_test")
	os.WriteFile(privPath, []byte("fake private key"), 0600)

	_, err := NewSSHProvider(privPath)
	if err == nil {
		t.Error("expected error when public key is missing")
	}
}

func TestSSHProviderUnlockAndLock(t *testing.T) {
	dir := t.TempDir()
	keyPath := createTestSSHKeyPair(t, dir)

	p, err := NewSSHProvider(keyPath)
	if err != nil {
		t.Fatalf("NewSSHProvider failed: %v", err)
	}

	// Unlock with empty passphrase (unencrypted key)
	if err := p.Unlock(""); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	if !p.IsUnlocked() {
		t.Error("should be unlocked")
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

	// Lock
	p.Lock()
	if p.IsUnlocked() {
		t.Error("should be locked")
	}

	_, err = p.Identity()
	if err == nil {
		t.Error("expected error when locked")
	}
}

func TestSSHProviderLockedRecipient(t *testing.T) {
	p := &SSHProvider{}
	_, err := p.Recipient()
	if err == nil {
		t.Error("expected error for nil recipient")
	}
}

func TestSSHProviderDisplayInfo(t *testing.T) {
	dir := t.TempDir()
	keyPath := createTestSSHKeyPair(t, dir)

	p, err := NewSSHProvider(keyPath)
	if err != nil {
		t.Fatalf("NewSSHProvider failed: %v", err)
	}

	info := p.DisplayInfo()
	if info == "" {
		t.Error("DisplayInfo should not be empty")
	}
}

func TestSSHProviderEncryptDecryptRoundtrip(t *testing.T) {
	dir := t.TempDir()
	keyPath := createTestSSHKeyPair(t, dir)

	p, err := NewSSHProvider(keyPath)
	if err != nil {
		t.Fatalf("NewSSHProvider failed: %v", err)
	}

	if err := p.Unlock(""); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	recipient, _ := p.Recipient()
	identity, _ := p.Identity()

	original := []byte("secret ssh data")
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

func TestIsSSHKeyEncrypted(t *testing.T) {
	dir := t.TempDir()

	// Unencrypted key
	keyPath := createTestSSHKeyPair(t, dir)
	encrypted, err := IsSSHKeyEncrypted(keyPath)
	if err != nil {
		t.Fatalf("IsSSHKeyEncrypted failed: %v", err)
	}
	if encrypted {
		t.Error("unencrypted key should not be detected as encrypted")
	}

	// Encrypted key marker
	encPath := createTestEncryptedSSHKey(t, dir)
	encrypted, err = IsSSHKeyEncrypted(encPath)
	if err != nil {
		t.Fatalf("IsSSHKeyEncrypted failed: %v", err)
	}
	if !encrypted {
		t.Error("encrypted key should be detected as encrypted")
	}

	// Nonexistent file
	_, err = IsSSHKeyEncrypted("/nonexistent/key")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestDecryptSSHKey(t *testing.T) {
	// decryptSSHKey is not implemented, should return error
	_, err := decryptSSHKey([]byte("data"), []byte("pass"))
	if err == nil {
		t.Error("expected error from unimplemented decryptSSHKey")
	}
}

func TestSSHProviderUnlockWithPassphrase(t *testing.T) {
	dir := t.TempDir()
	// Create an encrypted SSH key (fake one that will trigger the encrypted path)
	encPath := createTestEncryptedSSHKey(t, dir)

	// Also need a pub key for NewSSHProvider to work
	// Create a valid pub key
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	pubKey, _ := ssh.NewPublicKey(privKey.Public())
	pubData := ssh.MarshalAuthorizedKey(pubKey)
	os.WriteFile(encPath+".pub", pubData, 0644)

	p, err := NewSSHProvider(encPath)
	if err != nil {
		t.Fatalf("NewSSHProvider failed: %v", err)
	}

	// Unlock with passphrase should fail since decryptSSHKey is not implemented
	err = p.Unlock("somepassphrase")
	if err == nil {
		t.Error("expected error for encrypted key with unimplemented decryption")
	}
}
