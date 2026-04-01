package storage

import (
	"testing"
)

func TestPasswordProviderMethod(t *testing.T) {
	p := NewPasswordProvider()
	if p.Method() != AuthMethodPassword {
		t.Errorf("expected %s, got %s", AuthMethodPassword, p.Method())
	}
}

func TestPasswordProviderLockedState(t *testing.T) {
	p := NewPasswordProvider()

	// Should not be unlocked initially
	if p.IsUnlocked() {
		t.Error("new provider should not be unlocked")
	}

	// NeedsUnlock should always be true
	if !p.NeedsUnlock() {
		t.Error("password provider should always need unlock")
	}

	// Identity should fail when locked
	_, err := p.Identity()
	if err == nil {
		t.Error("expected error getting identity when locked")
	}

	// Recipient should fail when locked
	_, err = p.Recipient()
	if err == nil {
		t.Error("expected error getting recipient when locked")
	}
}

func TestPasswordProviderUnlockAndLock(t *testing.T) {
	p := NewPasswordProvider()

	if err := p.Unlock("testpassword"); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	if !p.IsUnlocked() {
		t.Error("should be unlocked after Unlock")
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

	p.Lock()

	if p.IsUnlocked() {
		t.Error("should be locked after Lock")
	}

	_, err = p.Identity()
	if err == nil {
		t.Error("expected error after Lock")
	}

	_, err = p.Recipient()
	if err == nil {
		t.Error("expected error after Lock")
	}
}

func TestPasswordProviderDisplayInfo(t *testing.T) {
	p := NewPasswordProvider()
	info := p.DisplayInfo()
	if info == "" {
		t.Error("DisplayInfo should not be empty")
	}
}

func TestNewPasswordProviderWithCredentials(t *testing.T) {
	// Valid password
	p, err := NewPasswordProviderWithCredentials("longenoughpassword")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !p.IsUnlocked() {
		t.Error("should be unlocked")
	}

	// Too short
	_, err = NewPasswordProviderWithCredentials("short")
	if err == nil {
		t.Error("expected error for short password")
	}
}

func TestPasswordProviderEncryptDecryptRoundtrip(t *testing.T) {
	p := NewPasswordProvider()
	if err := p.Unlock("roundtrippassword"); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	recipient, err := p.Recipient()
	if err != nil {
		t.Fatalf("Recipient failed: %v", err)
	}

	identity, err := p.Identity()
	if err != nil {
		t.Fatalf("Identity failed: %v", err)
	}

	original := []byte("secret data for password provider")
	encrypted, err := encryptData(original, recipient)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	decrypted, err := decryptData(encrypted, identity)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if string(decrypted) != string(original) {
		t.Errorf("roundtrip mismatch: got %q, want %q", decrypted, original)
	}
}
