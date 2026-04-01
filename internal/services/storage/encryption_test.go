package storage

import (
	"testing"

	"filippo.io/age"
)

func TestEncryptDecryptData(t *testing.T) {
	// Generate an X25519 identity for testing
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate identity: %v", err)
	}

	original := []byte("test data for encryption")
	encrypted, err := encryptData(original, identity.Recipient())
	if err != nil {
		t.Fatalf("encryptData failed: %v", err)
	}

	if !isAgeEncrypted(encrypted) {
		t.Error("encrypted data should have age header")
	}

	decrypted, err := decryptData(encrypted, identity)
	if err != nil {
		t.Fatalf("decryptData failed: %v", err)
	}

	if string(decrypted) != string(original) {
		t.Errorf("roundtrip mismatch: got %q, want %q", decrypted, original)
	}
}

func TestDecryptDataWithWrongIdentity(t *testing.T) {
	identity1, _ := age.GenerateX25519Identity()
	identity2, _ := age.GenerateX25519Identity()

	original := []byte("secret")
	encrypted, _ := encryptData(original, identity1.Recipient())

	_, err := decryptData(encrypted, identity2)
	if err == nil {
		t.Error("expected error decrypting with wrong identity")
	}
}

func TestDecryptDataInvalidInput(t *testing.T) {
	identity, _ := age.GenerateX25519Identity()

	_, err := decryptData([]byte("not encrypted data"), identity)
	if err == nil {
		t.Error("expected error for non-encrypted data")
	}
}

func TestEncryptDataEmptyInput(t *testing.T) {
	identity, _ := age.GenerateX25519Identity()

	encrypted, err := encryptData([]byte{}, identity.Recipient())
	if err != nil {
		t.Fatalf("encryptData failed for empty input: %v", err)
	}

	decrypted, err := decryptData(encrypted, identity)
	if err != nil {
		t.Fatalf("decryptData failed: %v", err)
	}

	if len(decrypted) != 0 {
		t.Errorf("expected empty, got %q", decrypted)
	}
}

func TestEncryptDataLargeInput(t *testing.T) {
	identity, _ := age.GenerateX25519Identity()

	// Large data
	original := make([]byte, 1024*1024)
	for i := range original {
		original[i] = byte(i % 256)
	}

	encrypted, err := encryptData(original, identity.Recipient())
	if err != nil {
		t.Fatalf("encryptData failed: %v", err)
	}

	decrypted, err := decryptData(encrypted, identity)
	if err != nil {
		t.Fatalf("decryptData failed: %v", err)
	}

	if len(decrypted) != len(original) {
		t.Errorf("length mismatch: got %d, want %d", len(decrypted), len(original))
	}
}
