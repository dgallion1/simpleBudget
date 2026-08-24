package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests defend the fix for the confidentiality defect described in
// F4: every helper in migration.go published through
// s.atomicWrite(path, data, 0644), a hardcoded mode, so atomicWrite's
// chmod-before-rename silently replaced the destination's existing
// permission bits on every migration -- including a deliberate chmod 600 a
// user applied to protect the file, at the exact moment they were enabling
// encryption to protect it further.
//
// Each test below targets exactly one of the four helpers directly (not
// through the higher-level EnableEncryption/DisableEncryption entry points)
// so that undoing the fix in one helper fails only its own test, not the
// others. That is checked by mutation: reverting filePerm usage back to a
// hardcoded 0644 in any single helper, in isolation, must fail the test
// named for that helper.

func modeOfFile(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("Stat %s: %v", p, err)
	}
	return fi.Mode().Perm()
}

// TestEncryptFileWithRecipientPreservesMode defends encryptFileWithRecipient:
// it must not replace an existing file's permission bits when it rewrites
// the file as ciphertext.
func TestEncryptFileWithRecipientPreservesMode(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Get a provider without letting EnableEncryption's directory walk touch
	// the file created below (it doesn't exist yet).
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	recipient, err := s.provider.Recipient()
	if err != nil {
		t.Fatalf("Recipient: %v", err)
	}

	path := filepath.Join(dir, "target.csv")
	if err := os.WriteFile(path, []byte("account,balance\nchecking,1.00\n"), 0644); err != nil {
		t.Fatalf("WriteFile setup: %v", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatalf("Chmod setup: %v", err)
	}

	if err := s.encryptFileWithRecipient(path, recipient); err != nil {
		t.Fatalf("encryptFileWithRecipient: %v", err)
	}

	if got := modeOfFile(t, path); got != 0600 {
		t.Errorf("encryptFileWithRecipient changed mode 0600 -> %v", got)
	}
}

// TestDecryptFileWithIdentityPreservesMode defends decryptFileWithIdentity:
// it must not replace an existing file's permission bits when it rewrites
// the file back to plaintext.
func TestDecryptFileWithIdentityPreservesMode(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	recipient, err := s.provider.Recipient()
	if err != nil {
		t.Fatalf("Recipient: %v", err)
	}
	identity, err := s.provider.Identity()
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}

	path := filepath.Join(dir, "target.csv")
	encrypted, err := encryptData([]byte("account,balance\nsavings,2.00\n"), recipient)
	if err != nil {
		t.Fatalf("encryptData: %v", err)
	}
	if err := os.WriteFile(path, encrypted, 0644); err != nil {
		t.Fatalf("WriteFile setup: %v", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatalf("Chmod setup: %v", err)
	}

	if err := s.decryptFileWithIdentity(path, identity); err != nil {
		t.Fatalf("decryptFileWithIdentity: %v", err)
	}

	if got := modeOfFile(t, path); got != 0600 {
		t.Errorf("decryptFileWithIdentity changed mode 0600 -> %v", got)
	}
}

// TestRollbackEncryptionWithIdentityPreservesMode defends
// rollbackEncryptionWithIdentity: on the failure path where an
// already-encrypted file is decrypted back to plaintext, it must not
// replace the file's permission bits either.
func TestRollbackEncryptionWithIdentityPreservesMode(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	recipient, err := s.provider.Recipient()
	if err != nil {
		t.Fatalf("Recipient: %v", err)
	}
	identity, err := s.provider.Identity()
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}

	path := filepath.Join(dir, "target.csv")
	encrypted, err := encryptData([]byte("account,balance\nchecking,3.00\n"), recipient)
	if err != nil {
		t.Fatalf("encryptData: %v", err)
	}
	if err := os.WriteFile(path, encrypted, 0644); err != nil {
		t.Fatalf("WriteFile setup: %v", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatalf("Chmod setup: %v", err)
	}

	s.rollbackEncryptionWithIdentity([]string{path}, identity)

	if got := modeOfFile(t, path); got != 0600 {
		t.Errorf("rollbackEncryptionWithIdentity changed mode 0600 -> %v", got)
	}
}

// TestRollbackDecryptionWithRecipientPreservesMode defends
// rollbackDecryptionWithRecipient: on the failure path where an
// already-decrypted file is re-encrypted, it must not replace the file's
// permission bits either.
func TestRollbackDecryptionWithRecipientPreservesMode(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	recipient, err := s.provider.Recipient()
	if err != nil {
		t.Fatalf("Recipient: %v", err)
	}

	path := filepath.Join(dir, "target.csv")
	if err := os.WriteFile(path, []byte("account,balance\nsavings,4.00\n"), 0644); err != nil {
		t.Fatalf("WriteFile setup: %v", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatalf("Chmod setup: %v", err)
	}

	failed := s.rollbackDecryptionWithRecipient([]string{path}, recipient)
	if len(failed) != 0 {
		t.Fatalf("rollbackDecryptionWithRecipient reported failures: %v", failed)
	}

	if got := modeOfFile(t, path); got != 0600 {
		t.Errorf("rollbackDecryptionWithRecipient changed mode 0600 -> %v", got)
	}
}

// TestEncryptFileWithRecipientOrdinaryModeUnchanged guards against
// satisfying "preserve the mode" by hardcoding some other fixed mode (e.g.
// 0600) instead of actually reading the file's existing one: an ordinary
// 0644 file must still come back 0644.
func TestEncryptFileWithRecipientOrdinaryModeUnchanged(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	recipient, err := s.provider.Recipient()
	if err != nil {
		t.Fatalf("Recipient: %v", err)
	}

	path := filepath.Join(dir, "target.csv")
	if err := os.WriteFile(path, []byte("account,balance\nchecking,5.00\n"), 0644); err != nil {
		t.Fatalf("WriteFile setup: %v", err)
	}

	if err := s.encryptFileWithRecipient(path, recipient); err != nil {
		t.Fatalf("encryptFileWithRecipient: %v", err)
	}

	if got := modeOfFile(t, path); got != 0644 {
		t.Errorf("encryptFileWithRecipient changed mode 0644 -> %v", got)
	}
}
