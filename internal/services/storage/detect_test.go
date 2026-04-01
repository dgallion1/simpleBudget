package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectAgeIdentities(t *testing.T) {
	// This function scans ~/.config/age/keys.txt, ~/.age/key.txt, ~/.age/keys.txt
	// We can't control the user's home directory, but we can at least call it
	// to cover the code path. It should not error even if no identities exist.
	identities, err := DetectAgeIdentities()
	if err != nil {
		t.Fatalf("DetectAgeIdentities failed: %v", err)
	}
	// identities may be empty or non-empty depending on the system
	_ = identities
}

func TestDetectSSHKeys(t *testing.T) {
	// This scans ~/.ssh, we can at least call it
	keys, err := DetectSSHKeys()
	if err != nil {
		t.Fatalf("DetectSSHKeys failed: %v", err)
	}
	_ = keys
}

func TestIsSSHKeyEncryptedVariants(t *testing.T) {
	dir := t.TempDir()

	// RSA encrypted key
	rsaPath := filepath.Join(dir, "id_rsa")
	rsaContent := `-----BEGIN RSA PRIVATE KEY-----
Proc-Type: 4,ENCRYPTED
DEK-Info: AES-128-CBC,xxx
base64data
-----END RSA PRIVATE KEY-----
`
	os.WriteFile(rsaPath, []byte(rsaContent), 0600)

	encrypted, err := IsSSHKeyEncrypted(rsaPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !encrypted {
		t.Error("RSA key with ENCRYPTED should be detected")
	}

	// Non-encrypted key
	plainPath := filepath.Join(dir, "id_plain")
	os.WriteFile(plainPath, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\ndata\n-----END OPENSSH PRIVATE KEY-----\n"), 0600)

	encrypted, err = IsSSHKeyEncrypted(plainPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if encrypted {
		t.Error("plain key should not be detected as encrypted")
	}
}

func TestIsYubiKeyPluginInstalled(t *testing.T) {
	// Just call it - will return true or false depending on system
	_ = IsYubiKeyPluginInstalled()
}

func TestSetupYubiKey(t *testing.T) {
	result, err := SetupYubiKey()
	if err == nil {
		t.Error("expected error from SetupYubiKey")
	}
	if result != nil {
		t.Error("expected nil result from SetupYubiKey")
	}
}

func TestListYubiKeySlots(t *testing.T) {
	// This requires the plugin; if not installed, should return nil
	if !IsYubiKeyPluginInstalled() {
		slots, err := ListYubiKeySlots()
		if err != nil {
			t.Errorf("should not error when plugin not installed: %v", err)
		}
		if slots != nil {
			t.Error("should return nil when plugin not installed")
		}
	}
}

func TestDetectYubiKeyIdentities(t *testing.T) {
	if !IsYubiKeyPluginInstalled() {
		identities, err := DetectYubiKeyIdentities()
		if err != nil {
			t.Errorf("should not error when plugin not installed: %v", err)
		}
		if identities != nil {
			t.Error("should return nil when plugin not installed")
		}
	}
}

func TestNewYubiKeyProviderWithoutPlugin(t *testing.T) {
	if IsYubiKeyPluginInstalled() {
		t.Skip("YubiKey plugin is installed, skipping negative test")
	}

	_, err := NewYubiKeyProvider("AGE-PLUGIN-YUBIKEY-test")
	if err == nil {
		t.Error("expected error when plugin not installed")
	}

	_, err = NewYubiKeyProviderWithRecipient("AGE-PLUGIN-YUBIKEY-test", "age1yubikey1test")
	if err == nil {
		t.Error("expected error when plugin not installed")
	}
}

func TestDetectYubiKeysWithoutPlugin(t *testing.T) {
	if IsYubiKeyPluginInstalled() {
		t.Skip("YubiKey plugin is installed, skipping negative test")
	}

	_, err := DetectYubiKeys()
	if err == nil {
		t.Error("expected error when plugin not installed")
	}
}

func TestGetYubiKeyIdentityForRecipientWithoutPlugin(t *testing.T) {
	if IsYubiKeyPluginInstalled() {
		t.Skip("YubiKey plugin is installed, skipping negative test")
	}

	_, err := GetYubiKeyIdentityForRecipient("age1yubikey1test")
	if err == nil {
		t.Error("expected error when plugin not installed")
	}
}

func TestNewAgeProviderHomeDirExpansion(t *testing.T) {
	// Test with ~/nonexistent path - should fail but exercise the home dir expansion
	_, err := NewAgeProvider("~/nonexistent-age-key-file-12345")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestNewSSHProviderHomeDirExpansion(t *testing.T) {
	// Test with ~/nonexistent path - should fail but exercise the home dir expansion
	_, err := NewSSHProvider("~/nonexistent-ssh-key-12345")
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
}

func TestGenerateAgeIdentityHomeDirExpansion(t *testing.T) {
	// Use a path under home that definitely won't exist
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot get home dir")
	}

	testPath := filepath.Join(home, ".budget2-test-identity-temp-12345")
	defer os.Remove(testPath)

	p, err := GenerateAgeIdentity("~/.budget2-test-identity-temp-12345")
	if err != nil {
		t.Fatalf("GenerateAgeIdentity failed: %v", err)
	}

	if p == nil {
		t.Fatal("provider should not be nil")
	}

	// Clean up
	os.Remove(testPath)
}

func TestSSHProviderLoadIdentityEncryptedKey(t *testing.T) {
	dir := t.TempDir()

	// Create a key file that looks encrypted to agessh
	encKeyPath := filepath.Join(dir, "id_enc")
	// Write openssh format but encrypted
	os.WriteFile(encKeyPath, []byte(`-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAACmFlczI1Ni1jdHIAAAAGYmNyeXB0AAAAGAAAABDT
-----END OPENSSH PRIVATE KEY-----`), 0600)

	// Need public key for NewSSHProvider
	sshDir := t.TempDir()
	keyPath := createTestSSHKeyPair(t, sshDir)

	// Use the valid public key but with the encrypted private key
	pubData, _ := os.ReadFile(keyPath + ".pub")
	os.WriteFile(encKeyPath+".pub", pubData, 0644)

	p, err := NewSSHProvider(encKeyPath)
	if err != nil {
		t.Fatalf("NewSSHProvider failed: %v", err)
	}

	// Unlock with empty passphrase should fail for encrypted key
	err = p.Unlock("")
	if err == nil {
		t.Error("expected error unlocking encrypted key without passphrase")
	}
}
